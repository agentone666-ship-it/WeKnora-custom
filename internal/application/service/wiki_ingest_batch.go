package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"golang.org/x/sync/errgroup"
)

func (s *wikiIngestService) scheduleFollowUp(ctx context.Context, payload WikiIngestPayload) bool {
	if s.redisClient == nil {
		return false
	}
	pendingKey := wikiPendingKeyPrefix + payload.KnowledgeBaseID
	count, err := s.redisClient.LLen(ctx, pendingKey).Result()
	if err != nil || count == 0 {
		return false
	}

	logger.Infof(ctx, "wiki ingest: %d more documents pending for KB %s, scheduling follow-up", count, payload.KnowledgeBaseID)

	payloadBytes, _ := json.Marshal(payload)
	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes,
		asynq.Queue("low"),
		asynq.MaxRetry(10), // Increased from 3 to 10 to outlast the active lock TTL
		asynq.Timeout(60*time.Minute),
		asynq.ProcessIn(5*time.Second), // short delay — active flag will be released by then
	)
	if _, err := s.task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki ingest: follow-up enqueue failed: %v", err)
		return false
	}
	return true
}

func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, t *asynq.Task) error {
	taskStartedAt := time.Now()
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)

	var payload WikiIngestPayload
	exitStatus := "success"
	mode := "redis"
	lockAcquired := false
	pendingOpsCount := 0
	ingestOps := 0
	retractOps := 0
	ingestSucceeded := 0
	ingestFailed := 0
	retractHandled := 0
	indexRebuildAttempted := false
	indexRebuildSucceeded := false
	followUpScheduled := false
	totalPagesAffected := 0
	docPreview := make([]string, 0, 6)

	defer func() {
		logger.Infof(
			ctx,
			"wiki ingest stats: kb=%s tenant=%d retry=%d/%d status=%s elapsed=%s mode=%s lock_acquired=%v pending_ops=%d ops(ingest=%d,retract=%d) ingest(success=%d,failed=%d) retract_handled=%d pages(total=%d) index(rebuild_attempted=%v,rebuild_succeeded=%v) followup=%v preview=%s",
			payload.KnowledgeBaseID,
			payload.TenantID,
			retryCount,
			maxRetry,
			exitStatus,
			time.Since(taskStartedAt).Round(time.Millisecond),
			mode,
			lockAcquired,
			pendingOpsCount,
			ingestOps,
			retractOps,
			ingestSucceeded,
			ingestFailed,
			retractHandled,
			totalPagesAffected,
			indexRebuildAttempted,
			indexRebuildSucceeded,
			followUpScheduled,
			previewStringSlice(docPreview, 6),
		)
	}()

	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		exitStatus = "invalid_payload"
		return fmt.Errorf("wiki ingest: unmarshal payload: %w", err)
	}

	// Inject context
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// Try to acquire the "active batch" flag (non-blocking)
	if s.redisClient != nil {
		activeKey := wikiActiveKeyPrefix + payload.KnowledgeBaseID
		acquired, err := s.redisClient.SetNX(ctx, activeKey, "1", 5*time.Minute).Result()
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: redis SetNX failed: %v", err)
		} else if !acquired {
			exitStatus = "active_lock_conflict"
			logger.Infof(ctx, "wiki ingest: another batch active for KB %s, deferring to asynq retry", payload.KnowledgeBaseID)
			return fmt.Errorf("concurrent wiki task active, please retry")
		}
		lockAcquired = acquired

		lockCtx, cancelLock := context.WithCancel(context.Background())
		defer func() {
			cancelLock()
			s.redisClient.Del(context.Background(), activeKey)
		}()

		go func() {
			ticker := time.NewTicker(2 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-lockCtx.Done():
					return
				case <-ticker.C:
					s.redisClient.Expire(context.Background(), activeKey, 5*time.Minute)
				}
			}
		}()
	} else {
		mode = "lite"
	}

	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil {
		exitStatus = "get_kb_failed"
		return fmt.Errorf("wiki ingest: get KB: %w", err)
	}
	if !kb.IsWikiEnabled() {
		exitStatus = "kb_not_wiki_enabled"
		return fmt.Errorf("wiki ingest: KB %s is not wiki type", kb.ID)
	}
	if kb.WikiConfig == nil || !kb.WikiConfig.AutoIngest {
		exitStatus = "auto_ingest_disabled"
		logger.Infof(ctx, "wiki ingest: auto_ingest disabled for KB %s, skipping", kb.ID)
		return nil
	}

	synthesisModelID := kb.WikiConfig.SynthesisModelID
	if synthesisModelID == "" {
		synthesisModelID = kb.SummaryModelID
	}
	if synthesisModelID == "" {
		exitStatus = "missing_synthesis_model"
		return fmt.Errorf("wiki ingest: no synthesis model configured for KB %s", kb.ID)
	}
	chatModel, err := s.modelService.GetChatModel(ctx, synthesisModelID)
	if err != nil {
		exitStatus = "get_chat_model_failed"
		return fmt.Errorf("wiki ingest: get chat model: %w", err)
	}

	lang := types.LanguageNameFromContext(ctx)

	pendingOps, peekedCount := s.peekPendingList(ctx, payload.KnowledgeBaseID)
	pendingOpsCount = len(pendingOps)
	if len(pendingOps) == 0 {
		if s.redisClient != nil {
			exitStatus = "no_pending_ops"
			logger.Infof(ctx, "wiki ingest: no pending operations for KB %s", payload.KnowledgeBaseID)
			return nil
		}
		if len(payload.LiteOps) > 0 {
			pendingOps = payload.LiteOps
			peekedCount = len(pendingOps)
			pendingOpsCount = len(pendingOps)
		} else {
			exitStatus = "no_lite_ops"
			return nil
		}
	}

	logger.Infof(ctx, "wiki ingest: batch processing %d ops for KB %s", len(pendingOps), payload.KnowledgeBaseID)

	// Fetch all existing pages to pass to the Map-Reduce phases
	allPages, _ := s.wikiService.ListAllPages(ctx, payload.KnowledgeBaseID)
	batchCtx := &WikiBatchContext{
		SlugTitleMap:                make(map[string]string),
		SummaryContentByKnowledgeID: make(map[string]string),
	}
	for _, p := range allPages {
		if p.PageType != types.WikiPageTypeIndex && p.PageType != types.WikiPageTypeLog && p.Status != types.WikiPageStatusArchived {
			batchCtx.SlugTitleMap[p.Slug] = p.Title
		}
		if p.PageType == types.WikiPageTypeSummary && p.Content != "" {
			for _, ref := range p.SourceRefs {
				kid := ref
				if pipeIdx := strings.Index(ref, "|"); pipeIdx > 0 {
					kid = ref[:pipeIdx]
				}
				batchCtx.SummaryContentByKnowledgeID[kid] = p.Content
			}
		}
	}

	// 1. MAP PHASE (Parallel extraction and generation of updates)
	var mapMu sync.Mutex
	slugUpdates := make(map[string][]SlugUpdate)
	var docResults []*docIngestResult
	var retractChangeDesc strings.Builder

	eg, mapCtx := errgroup.WithContext(ctx)
	eg.SetLimit(10) // Map phase limit

	for _, op := range pendingOps {
		op := op
		eg.Go(func() error {
			if op.Op == WikiOpRetract {
				mapMu.Lock()
				retractOps++
				retractHandled++
				docPreview = append(docPreview, fmt.Sprintf("retract[%s]: %s", previewText(op.KnowledgeID, 24), previewText(op.DocTitle, 48)))
				fmt.Fprintf(&retractChangeDesc, "<document_removed>\n<title>%s</title>\n<summary>%s</summary>\n</document_removed>\n\n", op.DocTitle, op.DocSummary)

				for _, slug := range op.PageSlugs {
					slugUpdates[slug] = append(slugUpdates[slug], SlugUpdate{
						Slug:              slug,
						Type:              "retract",
						RetractDocContent: op.DocSummary,
						DocTitle:          op.DocTitle,
						KnowledgeID:       op.KnowledgeID,
						Language:          op.Language,
					})
				}
				mapMu.Unlock()
				return nil
			}

			// Ingest
			mapMu.Lock()
			ingestOps++
			mapMu.Unlock()

			logger.Infof(mapCtx, "wiki ingest: processing document '%s' (%s)", op.DocTitle, op.KnowledgeID)
			result, updates, err := s.mapOneDocument(mapCtx, chatModel, payload, op, batchCtx)
			if err != nil {
				mapMu.Lock()
				ingestFailed++
				mapMu.Unlock()
				logger.Warnf(mapCtx, "wiki ingest: failed to map knowledge %s: %v", op.KnowledgeID, err)
				return nil // Don't fail the whole batch
			}

			if result != nil {
				mapMu.Lock()
				ingestSucceeded++
				docResults = append(docResults, result)
				docPreview = append(docPreview, fmt.Sprintf("ingest[%s]: title=%s summary=%s", previewText(result.KnowledgeID, 24), previewText(result.DocTitle, 40), previewText(result.Summary, 64)))
				for _, u := range updates {
					slugUpdates[u.Slug] = append(slugUpdates[u.Slug], u)
				}
				mapMu.Unlock()
			}
			return nil
		})
	}
	_ = eg.Wait()

	// 2. REDUCE PHASE (Parallel upserting grouped by Slug)
	egReduce, reduceCtx := errgroup.WithContext(ctx)
	egReduce.SetLimit(10) // Reduce phase limit (LLM + DB concurrent connections)

	var reduceMu sync.Mutex
	var allPagesAffected []string
	var ingestPagesAffected []string
	var retractPagesAffected []string

	for slug, updates := range slugUpdates {
		slug := slug
		updates := updates
		egReduce.Go(func() error {
			changed, affectedType, err := s.reduceSlugUpdates(reduceCtx, chatModel, payload.KnowledgeBaseID, slug, updates, payload.TenantID, batchCtx)
			if err != nil {
				logger.Warnf(reduceCtx, "wiki ingest: reduce failed for slug %s: %v", slug, err)
			}
			if changed {
				reduceMu.Lock()
				allPagesAffected = append(allPagesAffected, slug)
				if affectedType == "ingest" {
					ingestPagesAffected = append(ingestPagesAffected, slug)
				} else if affectedType == "retract" {
					retractPagesAffected = append(retractPagesAffected, slug)
				}
				reduceMu.Unlock()
			}
			return nil
		})
	}
	_ = egReduce.Wait()

	totalPagesAffected = len(allPagesAffected)

	// Append log entry for retracts (since retracts aren't in docResults)
	for _, op := range pendingOps {
		if op.Op == WikiOpRetract {
			s.appendLogEntry(ctx, WikiIngestPayload{
				TenantID:        payload.TenantID,
				KnowledgeBaseID: payload.KnowledgeBaseID,
			}, "retract", op.KnowledgeID, op.DocTitle, op.PageSlugs, "")
		}
	}

	// Build change description for the Index Intro LLM prompt
	var changeDesc strings.Builder
	if len(docResults) > 0 {
		for _, r := range docResults {
			fmt.Fprintf(&changeDesc, "<document_added>\n<title>%s</title>\n<summary>%s</summary>\n</document_added>\n\n", r.DocTitle, r.Summary)
		}
	}
	if retractChangeDesc.Len() > 0 {
		changeDesc.WriteString(retractChangeDesc.String())
	}

	// Rebuild index page
	if changeDesc.Len() > 0 {
		indexRebuildAttempted = true
		logger.Infof(ctx, "wiki ingest: rebuilding index page")
		if err := s.rebuildIndexPage(ctx, chatModel, payload, changeDesc.String(), lang); err != nil {
			logger.Warnf(ctx, "wiki ingest: rebuild index failed: %v", err)
			docPreview = append(docPreview, fmt.Sprintf("index_change=%s", previewText(changeDesc.String(), 160)))
		} else {
			indexRebuildSucceeded = true
			docPreview = append(docPreview, fmt.Sprintf("index_change=%s", previewText(changeDesc.String(), 160)))
		}
	}

	// Append log entry for ingests
	if len(docResults) > 0 {
		s.appendLogEntry(ctx, payload, "ingest", "", fmt.Sprintf("%d documents", len(docResults)), ingestPagesAffected, "")
	}

	if len(retractPagesAffected) > 0 {
		logger.Infof(ctx, "wiki ingest: cleaning dead links")
		s.cleanDeadLinks(ctx, payload.KnowledgeBaseID)
	}

	if len(allPagesAffected) > 0 {
		logger.Infof(ctx, "wiki ingest: injecting cross links")
		s.injectCrossLinks(ctx, payload.KnowledgeBaseID, allPagesAffected)

		logger.Infof(ctx, "wiki ingest: publishing draft pages")
		s.publishDraftPages(ctx, payload.KnowledgeBaseID, allPagesAffected)
	}

	s.trimPendingList(ctx, payload.KnowledgeBaseID, peekedCount)

	logger.Infof(ctx, "wiki ingest: batch completed for KB %s, %d ops, %d pages affected", payload.KnowledgeBaseID, len(pendingOps), len(allPagesAffected))

	followUpScheduled = s.scheduleFollowUp(ctx, payload)
	return nil
}

func (s *wikiIngestService) mapOneDocument(
	ctx context.Context,
	chatModel chat.Chat,
	payload WikiIngestPayload,
	op WikiPendingOp,
	batchCtx *WikiBatchContext,
) (*docIngestResult, []SlugUpdate, error) {
	docStartedAt := time.Now()
	knowledgeID := op.KnowledgeID
	lang := op.Language

	chunks, err := s.chunkRepo.ListChunksByKnowledgeID(ctx, payload.TenantID, knowledgeID)
	if err != nil {
		return nil, nil, fmt.Errorf("get chunks: %w", err)
	}
	if len(chunks) == 0 {
		logger.Infof(ctx, "wiki ingest: document %s has no chunks, skip", knowledgeID)
		return nil, nil, nil
	}

	content := reconstructContent(chunks)
	rawRuneCount := len([]rune(content))
	if len([]rune(content)) > maxContentForWiki {
		content = string([]rune(content)[:maxContentForWiki])
	}
	logger.Infof(ctx, "wiki ingest: doc %s chunks=%d content_len(raw=%d,truncated=%d)", knowledgeID, len(chunks), rawRuneCount, len([]rune(content)))

	docTitle := knowledgeID
	if kn, err := s.knowledgeSvc.GetKnowledgeByIDOnly(ctx, knowledgeID); err == nil && kn != nil && kn.Title != "" {
		docTitle = kn.Title
	} else {
		for _, ch := range chunks {
			if ch.Content != "" {
				lines := strings.SplitN(ch.Content, "\n", 2)
				if len(lines) > 0 && len(lines[0]) > 0 && len(lines[0]) < 200 {
					docTitle = strings.TrimPrefix(strings.TrimSpace(lines[0]), "# ")
					break
				}
			}
		}
	}

	sourceRef := fmt.Sprintf("%s|%s", knowledgeID, docTitle)
	oldPageSlugs := s.getExistingPageSlugsForKnowledge(ctx, payload.KnowledgeBaseID, knowledgeID)

	logger.Infof(ctx, "wiki ingest: extracting entities and concepts for %s", knowledgeID)
	extractedEntities, extractedConcepts, slugItems, err := s.extractEntitiesAndConceptsNoUpsert(ctx, chatModel, content, docTitle, lang, payload, oldPageSlugs)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: knowledge extraction failed for %s: %v", knowledgeID, err)
		return nil, nil, err
	}

	var extractedPages []string
	for slug := range slugItems {
		extractedPages = append(extractedPages, slug)
	}

	// Summary
	summarySlug := fmt.Sprintf("summary/%s", slugify(docTitle))
	var slugListing string
	for _, slug := range extractedPages {
		if item, ok := slugItems[slug]; ok {
			aliases := ""
			if len(item.Aliases) > 0 {
				aliases = fmt.Sprintf(" (Aliases: %s)", strings.Join(item.Aliases, ", "))
			}
			slugListing += fmt.Sprintf("- [[%s]] = %s%s\n", slug, item.Name, aliases)
		} else {
			slugListing += fmt.Sprintf("- [[%s]]\n", slug)
		}
	}

	var docSummaryLine string
	summaryContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiSummaryPrompt, map[string]string{
		"Title":          docTitle,
		"FileName":       docTitle,
		"FileType":       "document",
		"Content":        content,
		"Language":       lang,
		"ExtractedSlugs": slugListing,
	})

	var updates []SlugUpdate

	if err != nil {
		logger.Errorf(ctx, "wiki ingest: generate summary failed for %s: %v", knowledgeID, err)
	} else {
		sumLine, sumBody := splitSummaryLine(summaryContent)
		if sumBody == "" {
			sumBody = summaryContent
		}
		if sumLine == "" {
			sumLine = docTitle
		}
		docSummaryLine = sumLine
		updates = append(updates, SlugUpdate{
			Slug:        summarySlug,
			Type:        types.WikiPageTypeSummary,
			DocTitle:    docTitle,
			KnowledgeID: knowledgeID,
			SourceRef:   sourceRef,
			Language:    lang,
			SummaryLine: sumLine,
			SummaryBody: sumBody,
		})
		extractedPages = append(extractedPages, summarySlug)
	}

	// Entities
	for _, item := range extractedEntities {
		if item.Slug != "" {
			updates = append(updates, SlugUpdate{
				Slug:        item.Slug,
				Type:        types.WikiPageTypeEntity,
				Item:        item,
				DocTitle:    docTitle,
				KnowledgeID: knowledgeID,
				SourceRef:   sourceRef,
				Language:    lang,
			})
		}
	}

	// Concepts
	for _, item := range extractedConcepts {
		if item.Slug != "" {
			updates = append(updates, SlugUpdate{
				Slug:        item.Slug,
				Type:        types.WikiPageTypeConcept,
				Item:        item,
				DocTitle:    docTitle,
				KnowledgeID: knowledgeID,
				SourceRef:   sourceRef,
				Language:    lang,
			})
		}
	}

	// Stale Pages
	for oldSlug := range oldPageSlugs {
		found := false
		for _, newSlug := range extractedPages {
			if oldSlug == newSlug {
				found = true
				break
			}
		}
		if !found {
			updates = append(updates, SlugUpdate{
				Slug:              oldSlug,
				Type:              "retractStale",
				RetractDocContent: content,
				DocTitle:          docTitle,
				KnowledgeID:       knowledgeID,
				Language:          lang,
			})
		}
	}

	logger.Infof(ctx, "wiki ingest: mapped knowledge %s title=%q generated_updates=%d elapsed=%s",
		knowledgeID, previewText(docTitle, 80), len(updates), time.Since(docStartedAt).Round(time.Millisecond))

	return &docIngestResult{
		KnowledgeID: knowledgeID,
		DocTitle:    docTitle,
		Summary:     docSummaryLine,
	}, updates, nil
}

func (s *wikiIngestService) extractEntitiesAndConceptsNoUpsert(
	ctx context.Context,
	chatModel chat.Chat,
	content, docTitle, lang string,
	payload WikiIngestPayload,
	oldPageSlugs map[string]bool,
) ([]extractedItem, []extractedItem, map[string]extractedItem, error) {
	var prevSlugsText string
	if len(oldPageSlugs) > 0 {
		var sb strings.Builder
		for slug := range oldPageSlugs {
			fmt.Fprintf(&sb, "- %s\n", slug)
		}
		prevSlugsText = sb.String()
	} else {
		prevSlugsText = "(none — this is a new document)"
	}

	extractionJSON, err := s.generateWithTemplate(ctx, chatModel, agent.WikiKnowledgeExtractPrompt, map[string]string{
		"Title":         docTitle,
		"Content":       content,
		"Language":      lang,
		"PreviousSlugs": prevSlugsText,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("combined extraction failed: %w", err)
	}

	extractionJSON = strings.TrimSpace(extractionJSON)
	extractionJSON = strings.TrimPrefix(extractionJSON, "```json")
	extractionJSON = strings.TrimPrefix(extractionJSON, "```")
	extractionJSON = strings.TrimSuffix(extractionJSON, "```")
	extractionJSON = strings.TrimSpace(extractionJSON)
	extractionJSON = sanitizeJSONString(extractionJSON)

	var result combinedExtraction
	if err := json.Unmarshal([]byte(extractionJSON), &result); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to parse combined extraction JSON: %v\nRaw: %s", err, extractionJSON)
		return nil, nil, nil, fmt.Errorf("parse combined extraction JSON: %w", err)
	}

	result.Entities = s.deduplicateItems(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, payload.KnowledgeBaseID)
	result.Concepts = s.deduplicateItems(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, payload.KnowledgeBaseID)

	slugItems := make(map[string]extractedItem)
	for _, item := range result.Entities {
		if item.Slug != "" && item.Name != "" {
			slugItems[item.Slug] = item
		}
	}
	for _, item := range result.Concepts {
		if item.Slug != "" && item.Name != "" {
			slugItems[item.Slug] = item
		}
	}

	return result.Entities, result.Concepts, slugItems, nil
}

func (s *wikiIngestService) reduceSlugUpdates(
	ctx context.Context,
	chatModel chat.Chat,
	kbID string,
	slug string,
	updates []SlugUpdate,
	tenantID uint64,
	batchCtx *WikiBatchContext,
) (bool, string, error) {
	page, err := s.wikiService.GetPageBySlug(ctx, kbID, slug)
	exists := (err == nil && page != nil)

	if !exists {
		hasAdditions := false
		for _, u := range updates {
			if u.Type == types.WikiPageTypeEntity || u.Type == types.WikiPageTypeConcept || u.Type == "summary" {
				hasAdditions = true
				break
			}
		}
		if !hasAdditions {
			return false, "", nil
		}

		page = &types.WikiPage{
			ID:              uuid.New().String(),
			TenantID:        tenantID,
			KnowledgeBaseID: kbID,
			Slug:            slug,
			Status:          types.WikiPageStatusDraft,
			SourceRefs:      types.StringArray{},
			Aliases:         types.StringArray{},
		}
	}

	changed := false
	affectedType := "ingest"

	var summaryUpdate *SlugUpdate
	var retracts []SlugUpdate
	var additions []SlugUpdate

	for i, u := range updates {
		if u.Type == "summary" {
			summaryUpdate = &updates[i]
		} else if u.Type == "retract" || u.Type == "retractStale" {
			retracts = append(retracts, u)
			affectedType = "retract"
		} else if u.Type == types.WikiPageTypeEntity || u.Type == types.WikiPageTypeConcept {
			additions = append(additions, u)
			affectedType = "ingest" // Additions override retracts type
		}
	}

	if summaryUpdate != nil {
		page.Title = summaryUpdate.DocTitle + " - Summary"
		page.Content = summaryUpdate.SummaryBody
		page.Summary = summaryUpdate.SummaryLine
		page.PageType = types.WikiPageTypeSummary
		page.SourceRefs = appendUnique(page.SourceRefs, summaryUpdate.SourceRef)
		changed = true

		if exists {
			_, err = s.wikiService.UpdatePage(ctx, page)
		} else {
			_, err = s.wikiService.CreatePage(ctx, page)
		}
		return changed, affectedType, err
	}

	var remainingSourcesContent strings.Builder
	var deletedContent strings.Builder
	var relatedSlugs strings.Builder
	var newContentBuilder strings.Builder
	var docTitles []string
	var language string

	if len(retracts) > 0 {
		language = retracts[0].Language

		for _, r := range retracts {
			fmt.Fprintf(&deletedContent, "<document>\n<title>%s</title>\n<content>\n%s\n</content>\n</document>\n\n", r.DocTitle, r.RetractDocContent)
		}

		retractKIDs := make(map[string]bool)
		for _, r := range retracts {
			retractKIDs[r.KnowledgeID] = true
		}

		for _, ref := range page.SourceRefs {
			pipeIdx := strings.Index(ref, "|")
			var refKnowledgeID, refTitle string
			if pipeIdx > 0 {
				refKnowledgeID = ref[:pipeIdx]
				refTitle = ref[pipeIdx+1:]
			} else {
				refKnowledgeID = ref
				refTitle = ref
			}

			if retractKIDs[refKnowledgeID] {
				continue
			}

			if content, ok := batchCtx.SummaryContentByKnowledgeID[refKnowledgeID]; ok {
				fmt.Fprintf(&remainingSourcesContent, "<document>\n<title>%s</title>\n<content>\n%s\n</content>\n</document>\n\n", refTitle, content)
			} else {
				fmt.Fprintf(&remainingSourcesContent, "<document>\n<title>%s</title>\n<content>\n(summary not available)\n</content>\n</document>\n\n", refTitle)
			}
		}
		if remainingSourcesContent.Len() == 0 {
			remainingSourcesContent.WriteString("(no remaining sources)")
		}

		newRefs := types.StringArray{}
		for _, ref := range page.SourceRefs {
			pipeIdx := strings.Index(ref, "|")
			refKnowledgeID := ref
			if pipeIdx > 0 {
				refKnowledgeID = ref[:pipeIdx]
			}
			if !retractKIDs[refKnowledgeID] {
				newRefs = append(newRefs, ref)
			}
		}
		page.SourceRefs = newRefs
	}

	if len(additions) > 0 {
		language = additions[0].Language
		for _, add := range additions {
			fmt.Fprintf(&newContentBuilder, "<document>\n<title>%s</title>\n<content>\n**%s**: %s\n\n%s\n</content>\n</document>\n\n",
				add.DocTitle, add.Item.Name, add.Item.Description, add.Item.Details)
			docTitles = appendUnique(docTitles, add.DocTitle)

			for _, alias := range add.Item.Aliases {
				page.Aliases = appendUnique(page.Aliases, alias)
			}
			page.SourceRefs = appendUnique(page.SourceRefs, add.SourceRef)

			if page.Title == "" {
				page.Title = add.Item.Name
			}
			if page.PageType == "" {
				page.PageType = add.Type
			}
		}
	}

	if len(additions) > 0 || len(retracts) > 0 {
		for _, outSlug := range page.OutLinks {
			if title, ok := batchCtx.SlugTitleMap[outSlug]; ok {
				fmt.Fprintf(&relatedSlugs, "- %s (%s)\n", outSlug, title)
			}
		}

		existingContent := page.Content
		if !exists || existingContent == "" {
			existingContent = "(New page)"
		}

		hasAdditionsStr := ""
		if len(additions) > 0 {
			hasAdditionsStr = "1"
		}
		hasRetractionsStr := ""
		if len(retracts) > 0 {
			hasRetractionsStr = "1"
		}

		updatedContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiPageModifyPrompt, map[string]string{
			"HasAdditions":            hasAdditionsStr,
			"HasRetractions":          hasRetractionsStr,
			"ExistingContent":         existingContent,
			"NewContent":              newContentBuilder.String(),
			"DeletedContent":          deletedContent.String(),
			"RemainingSourcesContent": remainingSourcesContent.String(),
			"AvailableSlugs":          relatedSlugs.String(),
			"Language":                language,
		})

		if err == nil && updatedContent != "" {
			updatedSummary, updatedBody := splitSummaryLine(updatedContent)
			if updatedBody != "" {
				page.Content = updatedBody
			} else {
				page.Content = updatedContent
			}
			if updatedSummary != "" {
				page.Summary = updatedSummary
			}
			changed = true
		} else if err != nil {
			logger.Warnf(ctx, "wiki ingest: update/retract failed for slug %s: %v", slug, err)
		}
	}

	if changed {
		if exists {
			_, err = s.wikiService.UpdatePage(ctx, page)
		} else {
			_, err = s.wikiService.CreatePage(ctx, page)
		}
		return true, affectedType, err
	}

	return false, "", nil
}
