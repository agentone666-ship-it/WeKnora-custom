package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	// maxContentForWiki limits the document content sent to LLM for wiki generation
	maxContentForWiki = 32768

	// wikiPendingKeyPrefix is the Redis key prefix for pending wiki ingest document lists.
	// Key format: wiki:pending:{kbID} → Redis List of knowledge IDs.
	wikiPendingKeyPrefix = "wiki:pending:"

	// wikiActiveKeyPrefix is the Redis key for the "batch in progress" flag.
	// Key format: wiki:active:{kbID} → "1" with TTL. Prevents concurrent batches.
	wikiActiveKeyPrefix = "wiki:active:"

	// wikiIngestDelay is how long to wait after a document is added before
	// the batch task fires. Debounces rapid uploads.
	wikiIngestDelay = 30 * time.Second

	// wikiPendingTTL prevents stale pending lists from accumulating.
	wikiPendingTTL = 24 * time.Hour

	// wikiActiveTTL is the max time the "batch in progress" flag stays set.
	// Safety net — auto-expires if a batch crashes without cleanup.
	wikiActiveTTL = 60 * time.Minute

	// wikiMaxDocsPerBatch limits how many documents a single batch processes.
	// Prevents unbounded execution time. Remaining docs stay in the pending list
	// and are picked up by the follow-up task.
	wikiMaxDocsPerBatch = 5
)

// WikiIngestPayload is the asynq task payload for wiki ingest batch trigger.
// The actual document IDs are stored in a Redis list (wiki:pending:{kbID}).
// KnowledgeID is only used as fallback in Lite mode (no Redis).
type WikiIngestPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id,omitempty"` // Lite mode only
	Language        string `json:"language,omitempty"`
}

// WikiRetractPayload is the asynq task payload for wiki content retraction
type WikiRetractPayload struct {
	TenantID        uint64   `json:"tenant_id"`
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	KnowledgeID     string   `json:"knowledge_id"`
	DocTitle        string   `json:"doc_title"`
	DocSummary      string   `json:"doc_summary,omitempty"` // one-line summary of the deleted document
	Language        string   `json:"language,omitempty"`
	PageSlugs       []string `json:"page_slugs"`
}

// wikiIngestService handles the LLM-powered wiki generation pipeline
type wikiIngestService struct {
	wikiService  interfaces.WikiPageService
	kbService    interfaces.KnowledgeBaseService
	chunkRepo    interfaces.ChunkRepository
	modelService interfaces.ModelService
	task         interfaces.TaskEnqueuer
	redisClient  *redis.Client // nil in Lite mode (no Redis)
}

// NewWikiIngestService creates a new wiki ingest service
func NewWikiIngestService(
	wikiService interfaces.WikiPageService,
	kbService interfaces.KnowledgeBaseService,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	svc := &wikiIngestService{
		wikiService:  wikiService,
		kbService:    kbService,
		chunkRepo:    chunkRepo,
		modelService: modelService,
		task:         task,
		redisClient:  redisClient,
	}
	return svc
}

// EnqueueWikiIngest adds a document to the wiki ingest queue.
//
// Architecture: each document upload pushes its knowledgeID to a Redis pending list,
// then schedules a delayed asynq task. When the task fires, it atomically drains the
// entire list and processes ALL pending documents in one batch.
//
// If multiple uploads happen within the delay window (30s), each one schedules a task,
// but the FIRST task to fire drains the list and processes everything. Subsequent tasks
// fire, find an empty list, and exit immediately (no-op). This gives us natural batching
// without any locks or task deduplication.
//
//	t=0s   doc1 → RPush + Enqueue(delay=30s, id=random1)
//	t=5s   doc2 → RPush + Enqueue(delay=30s, id=random2)
//	t=10s  doc3 → RPush + Enqueue(delay=30s, id=random3)
//	t=30s  random1 fires → drain [doc1,doc2,doc3] → process all
//	t=35s  random2 fires → drain [] → no-op return
//	t=40s  random3 fires → drain [] → no-op return
//
// In Lite mode (no Redis), falls back to immediate per-document execution.
func EnqueueWikiIngest(ctx context.Context, task interfaces.TaskEnqueuer, redisClient *redis.Client, tenantID uint64, kbID, knowledgeID string) {
	lang, _ := types.LanguageFromContext(ctx)

	// Push to Redis pending list (if Redis available)
	if redisClient != nil {
		pendingKey := wikiPendingKeyPrefix + kbID
		redisClient.RPush(ctx, pendingKey, knowledgeID)
		redisClient.Expire(ctx, pendingKey, wikiPendingTTL)
	}

	payload := WikiIngestPayload{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		KnowledgeID:     knowledgeID, // fallback for Lite mode
		Language:        lang,
	}
	payloadBytes, _ := json.Marshal(payload)

	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes,
		asynq.Queue("low"),
		asynq.MaxRetry(3),
		asynq.Timeout(60*time.Minute),
		asynq.ProcessIn(wikiIngestDelay),
	)
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to enqueue task: %v", err)
	}
}

// EnqueueWikiRetract enqueues an async wiki content retraction task
func EnqueueWikiRetract(ctx context.Context, task interfaces.TaskEnqueuer, payload WikiRetractPayload) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "wiki retract: failed to marshal payload: %v", err)
		return
	}
	t := asynq.NewTask(types.TypeWikiRetract, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3), asynq.Timeout(60*time.Minute))
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki retract: failed to enqueue task: %v", err)
	}
}

// Handle implements interfaces.TaskHandler for asynq task processing.
// Wiki ingest tasks are debounced via asynq.Unique + ProcessIn, so at most
// one ingest task runs per KB at a time. No distributed lock needed.
func (s *wikiIngestService) Handle(ctx context.Context, t *asynq.Task) error {
	switch t.Type() {
	case types.TypeWikiRetract:
		return s.ProcessWikiRetract(ctx, t)
	default:
		return s.ProcessWikiIngest(ctx, t)
	}
}

// ProcessWikiRetract uses LLM to remove deleted document's contributions from wiki pages
func (s *wikiIngestService) ProcessWikiRetract(ctx context.Context, t *asynq.Task) error {
	var payload WikiRetractPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("wiki retract: unmarshal payload: %w", err)
	}

	logger.Infof(ctx, "wiki retract: starting for knowledge %s (%s), %d pages",
		payload.KnowledgeID, payload.DocTitle, len(payload.PageSlugs))

	// Inject context values
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// Get KB and synthesis model
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("wiki retract: get KB: %w", err)
	}
	synthesisModelID := kb.WikiConfig.SynthesisModelID
	if synthesisModelID == "" {
		synthesisModelID = kb.SummaryModelID
	}
	if synthesisModelID == "" {
		return fmt.Errorf("wiki retract: no synthesis model for KB %s", kb.ID)
	}
	chatModel, err := s.modelService.GetChatModel(ctx, synthesisModelID)
	if err != nil {
		return fmt.Errorf("wiki retract: get chat model: %w", err)
	}

	lang := types.LanguageNameFromContext(ctx)

	s.retractPagesContent(ctx, chatModel, payload.KnowledgeBaseID, payload.DocTitle, payload.DocSummary, payload.PageSlugs, lang)

	// Rebuild index page to reflect changes
	retractChangeDesc := fmt.Sprintf("Removed document '%s': %s", payload.DocTitle, payload.DocSummary)
	if err := s.rebuildIndexPage(ctx, chatModel, WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
	}, retractChangeDesc, lang); err != nil {
		logger.Warnf(ctx, "wiki retract: rebuild index failed: %v", err)
	}

	// Append log entry
	s.appendLogEntry(ctx, WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
	}, "retract", payload.DocTitle, payload.PageSlugs, "")

	// Clean up dead links pointing to archived/deleted pages
	s.cleanDeadLinks(ctx, payload.KnowledgeBaseID)

	return nil
}

// retractPagesContent uses LLM to remove a deleted/stale document's contributions
// from the given wiki pages. docContent is the deleted document's content (or summary)
// so the LLM can accurately identify which parts to remove. It does NOT rebuild the
// index or append log entries — callers are responsible for those post-processing steps.
func (s *wikiIngestService) retractPagesContent(
	ctx context.Context,
	chatModel chat.Chat,
	kbID, docTitle, docContent string,
	pageSlugs []string,
	lang string,
) {
	allPages, _ := s.wikiService.ListAllPages(ctx, kbID)
	slugTitleMap := make(map[string]string)
	for _, p := range allPages {
		if p.PageType != types.WikiPageTypeIndex && p.PageType != types.WikiPageTypeLog && p.Status != types.WikiPageStatusArchived {
			slugTitleMap[p.Slug] = p.Title
		}
	}

	for _, slug := range pageSlugs {
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, slug)
		if err != nil || page == nil {
			continue
		}

		var remainingSources string
		for _, ref := range page.SourceRefs {
			pipeIdx := strings.Index(ref, "|")
			if pipeIdx > 0 {
				remainingSources += "- " + ref[pipeIdx+1:] + "\n"
			} else {
				remainingSources += "- " + ref + "\n"
			}
		}
		if remainingSources == "" {
			remainingSources = "(no other sources)"
		}

		var relatedSlugs strings.Builder
		for _, outSlug := range page.OutLinks {
			if title, ok := slugTitleMap[outSlug]; ok {
				fmt.Fprintf(&relatedSlugs, "- %s (%s)\n", outSlug, title)
			}
		}

		updatedContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiPageRetractPrompt, map[string]string{
			"ExistingContent":   page.Content,
			"DeletedDocTitle":   docTitle,
			"DeletedDocContent": docContent,
			"RemainingSources":  remainingSources,
			"AvailableSlugs":    relatedSlugs.String(),
			"Language":          lang,
		})
		if err != nil {
			logger.Warnf(ctx, "wiki retract: LLM call failed for page %s: %v", slug, err)
			continue
		}

		retractedSummary, retractedBody := splitSummaryLine(updatedContent)
		if retractedBody != "" {
			page.Content = retractedBody
		} else {
			page.Content = updatedContent
		}
		if retractedSummary != "" {
			page.Summary = retractedSummary
		}
		if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
			logger.Warnf(ctx, "wiki retract: failed to update page %s: %v", slug, err)
		} else {
			logger.Infof(ctx, "wiki retract: updated page %s after removing content from '%s'", slug, docTitle)
		}
	}
}

// ProcessWikiIngest processes a batch wiki ingest task.
//
// Concurrency model (Redis mode):
//  1. Try to set "wiki:active:{kbID}" flag via SetNX. If already set, another batch
//     is running → return nil (no-op). Documents are safe in the pending list.
//  2. Atomically drain the pending list → process all documents sequentially.
//  3. After processing, clear the active flag.
//  4. Check if more documents arrived during processing. If so, enqueue a follow-up
//     task (no delay — docs are already waiting). This is safe because we just
//     cleared the active flag.
//
// This ensures: one batch per KB at a time, no locks, no blocking, no timeouts.
func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, t *asynq.Task) error {
	var payload WikiIngestPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
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
		acquired, err := s.redisClient.SetNX(ctx, activeKey, "1", wikiActiveTTL).Result()
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: redis SetNX failed: %v", err)
			// Proceed anyway — better to risk brief overlap than drop documents
		} else if !acquired {
			// Another batch is actively processing this KB — bail out.
			// Our documents are safe in the pending list; the active batch will
			// pick them up via its follow-up check, or a future task will.
			logger.Infof(ctx, "wiki ingest: another batch active for KB %s, skipping (docs safe in pending list)", payload.KnowledgeBaseID)
			return nil
		}
		// We own the flag — make sure to release it when done
		defer s.redisClient.Del(ctx, activeKey)
	}

	// Get KB and validate
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("wiki ingest: get KB: %w", err)
	}
	if !kb.IsWikiEnabled() {
		return fmt.Errorf("wiki ingest: KB %s is not wiki type", kb.ID)
	}
	if kb.WikiConfig == nil || !kb.WikiConfig.AutoIngest {
		logger.Infof(ctx, "wiki ingest: auto_ingest disabled for KB %s, skipping", kb.ID)
		return nil
	}

	// Get synthesis model
	synthesisModelID := kb.WikiConfig.SynthesisModelID
	if synthesisModelID == "" {
		synthesisModelID = kb.SummaryModelID
	}
	if synthesisModelID == "" {
		return fmt.Errorf("wiki ingest: no synthesis model configured for KB %s", kb.ID)
	}
	chatModel, err := s.modelService.GetChatModel(ctx, synthesisModelID)
	if err != nil {
		return fmt.Errorf("wiki ingest: get chat model: %w", err)
	}

	lang := types.LanguageNameFromContext(ctx)

	// Drain Redis pending list to get all documents queued for this KB
	knowledgeIDs := s.drainPendingList(ctx, payload.KnowledgeBaseID)
	if len(knowledgeIDs) == 0 {
		if s.redisClient != nil {
			// Redis mode: list was already drained — nothing to do
			return nil
		}
		// Lite mode (no Redis): use the single KnowledgeID from payload
		if payload.KnowledgeID != "" {
			knowledgeIDs = []string{payload.KnowledgeID}
		} else {
			return nil
		}
	}

	logger.Infof(ctx, "wiki ingest: batch processing %d documents for KB %s: %v",
		len(knowledgeIDs), payload.KnowledgeBaseID, knowledgeIDs)

	// Process each document
	var allPagesAffected []string
	var docResults []*docIngestResult
	for _, knowledgeID := range knowledgeIDs {
		result, err := s.processOneDocument(ctx, chatModel, payload, knowledgeID, lang)
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: failed to process knowledge %s: %v", knowledgeID, err)
			continue
		}
		if result != nil {
			allPagesAffected = append(allPagesAffected, result.Pages...)
			docResults = append(docResults, result)
		}
	}

	// Batch post-processing (once for the whole batch, not per-doc)

	// Build change description from processed documents
	var changeDesc strings.Builder
	fmt.Fprintf(&changeDesc, "Added %d documents:\n", len(docResults))
	for _, r := range docResults {
		fmt.Fprintf(&changeDesc, "- %s: %s\n", r.DocTitle, r.Summary)
	}

	// Rebuild index page
	if err := s.rebuildIndexPage(ctx, chatModel, payload, changeDesc.String(), lang); err != nil {
		logger.Warnf(ctx, "wiki ingest: rebuild index failed: %v", err)
	}

	// Append log entry
	s.appendLogEntry(ctx, payload, "ingest",
		fmt.Sprintf("%d documents", len(knowledgeIDs)),
		allPagesAffected, "")

	// Cross-link injection
	s.injectCrossLinks(ctx, payload.KnowledgeBaseID, allPagesAffected)

	// Publish all draft pages
	s.publishDraftPages(ctx, payload.KnowledgeBaseID, allPagesAffected)

	logger.Infof(ctx, "wiki ingest: batch completed for KB %s, %d docs, %d pages affected",
		payload.KnowledgeBaseID, len(knowledgeIDs), len(allPagesAffected))

	// After clearing active flag (via defer above), check for follow-up work.
	// Note: this runs BEFORE defer, but defer runs LIFO so active flag is still set here.
	// We need to clear it first, then check. Use a closure:
	s.scheduleFollowUp(ctx, payload)

	return nil
}

// scheduleFollowUp checks if documents arrived in the pending list during batch processing.
// Called right before the active flag is released (via defer). Enqueues a new task with
// minimal delay so the next batch picks up new docs promptly.
func (s *wikiIngestService) scheduleFollowUp(ctx context.Context, payload WikiIngestPayload) {
	if s.redisClient == nil {
		return
	}
	pendingKey := wikiPendingKeyPrefix + payload.KnowledgeBaseID
	count, err := s.redisClient.LLen(ctx, pendingKey).Result()
	if err != nil || count == 0 {
		return
	}

	logger.Infof(ctx, "wiki ingest: %d more documents pending for KB %s, scheduling follow-up", count, payload.KnowledgeBaseID)

	payloadBytes, _ := json.Marshal(payload)
	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes,
		asynq.Queue("low"),
		asynq.MaxRetry(3),
		asynq.Timeout(60*time.Minute),
		asynq.ProcessIn(5*time.Second), // short delay — active flag will be released by then
	)
	if _, err := s.task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki ingest: follow-up enqueue failed: %v", err)
	}
}

// drainPendingList atomically pops up to wikiMaxDocsPerBatch entries from the
// Redis pending list. Remaining entries stay in the list for the follow-up batch.
func (s *wikiIngestService) drainPendingList(ctx context.Context, kbID string) []string {
	if s.redisClient == nil {
		return nil
	}
	pendingKey := wikiPendingKeyPrefix + kbID

	// Atomically pop up to N items: LRANGE + LTRIM in a single Lua script
	script := redis.NewScript(`
		local items = redis.call("LRANGE", KEYS[1], 0, tonumber(ARGV[1]) - 1)
		redis.call("LTRIM", KEYS[1], tonumber(ARGV[1]), -1)
		return items
	`)
	result, err := script.Run(ctx, s.redisClient, []string{pendingKey}, wikiMaxDocsPerBatch).StringSlice()
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to drain pending list: %v", err)
		return nil
	}

	// Deduplicate (same doc could be pushed multiple times if re-uploaded)
	seen := make(map[string]bool)
	var unique []string
	for _, id := range result {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

// docIngestResult captures per-document info for batch post-processing.
type docIngestResult struct {
	KnowledgeID string
	DocTitle    string
	Summary     string   // one-line summary of the document (from summary page)
	Pages       []string // affected page slugs
}

// processOneDocument handles wiki ingest for a single document.
func (s *wikiIngestService) processOneDocument(
	ctx context.Context,
	chatModel chat.Chat,
	payload WikiIngestPayload,
	knowledgeID string,
	lang string,
) (*docIngestResult, error) {
	// Get document chunks and reconstruct content
	chunks, err := s.chunkRepo.ListChunksByKnowledgeID(ctx, payload.TenantID, knowledgeID)
	if err != nil {
		return nil, fmt.Errorf("get chunks: %w", err)
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	content := reconstructContent(chunks)
	if len([]rune(content)) > maxContentForWiki {
		content = string([]rune(content)[:maxContentForWiki])
	}

	// Get document title
	docTitle := knowledgeID
	for _, ch := range chunks {
		if ch.Content != "" {
			lines := strings.SplitN(ch.Content, "\n", 2)
			if len(lines) > 0 && len(lines[0]) > 0 && len(lines[0]) < 200 {
				docTitle = strings.TrimPrefix(strings.TrimSpace(lines[0]), "# ")
				break
			}
		}
	}

	var pagesAffected []string
	var docSummaryLine string
	sourceRef := fmt.Sprintf("%s|%s", knowledgeID, docTitle)

	// Snapshot existing page slugs for stale detection
	oldPageSlugs := s.getExistingPageSlugsForKnowledge(ctx, payload.KnowledgeBaseID, knowledgeID)

	// Build a per-doc payload for functions that still need KnowledgeID
	docPayload := WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     knowledgeID,
		Language:        payload.Language,
	}

	// Step 1: Extract entities and concepts
	extractedPages, slugNames, err := s.extractEntitiesAndConcepts(ctx, chatModel, content, docTitle, lang, docPayload, sourceRef, oldPageSlugs)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: knowledge extraction failed for %s: %v", knowledgeID, err)
	} else {
		pagesAffected = append(pagesAffected, extractedPages...)
	}

	// Step 2: Generate summary page
	summarySlug := fmt.Sprintf("summary/%s", slugify(docTitle))
	var slugListing string
	for _, slug := range extractedPages {
		if name, ok := slugNames[slug]; ok {
			slugListing += fmt.Sprintf("- [[%s]] = %s\n", slug, name)
		} else {
			slugListing += fmt.Sprintf("- [[%s]]\n", slug)
		}
	}
	summaryContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiSummaryPrompt, map[string]string{
		"Title":          docTitle,
		"FileName":       docTitle,
		"FileType":       "document",
		"Content":        content,
		"Language":       lang,
		"ExtractedSlugs": slugListing,
	})
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
		_, err := s.wikiService.CreatePage(ctx, &types.WikiPage{
			ID:              uuid.New().String(),
			TenantID:        payload.TenantID,
			KnowledgeBaseID: payload.KnowledgeBaseID,
			Slug:            summarySlug,
			Title:           docTitle + " - Summary",
			PageType:        types.WikiPageTypeSummary,
			Status:          types.WikiPageStatusDraft,
			Content:         sumBody,
			Summary:         sumLine,
			SourceRefs:      types.StringArray{sourceRef},
		})
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: create summary page failed for %s: %v", knowledgeID, err)
		} else {
			pagesAffected = append(pagesAffected, summarySlug)
		}
	}

	// Retract stale pages (pages this doc previously contributed to but no longer does)
	s.retractStalePages(ctx, chatModel, docPayload, oldPageSlugs, pagesAffected, docTitle, content, lang)

	logger.Infof(ctx, "wiki ingest: processed knowledge %s, %d pages affected", knowledgeID, len(pagesAffected))
	return &docIngestResult{
		KnowledgeID: knowledgeID,
		DocTitle:    docTitle,
		Summary:     docSummaryLine,
		Pages:       pagesAffected,
	}, nil
}

// cleanDeadLinks removes [[wiki-links]] that point to archived or deleted pages.
// Scans all published pages, checks each out_link, and removes references to
// pages that no longer exist or are archived. No LLM call — pure text cleanup.
func (s *wikiIngestService) cleanDeadLinks(ctx context.Context, kbID string) {
	allPages, err := s.wikiService.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) == 0 {
		return
	}

	// Build set of live (non-archived, non-system) slugs
	liveSlugs := make(map[string]bool)
	for _, p := range allPages {
		if p.Status != types.WikiPageStatusArchived {
			liveSlugs[p.Slug] = true
		}
	}

	var cleaned int
	for _, p := range allPages {
		if p.Status == types.WikiPageStatusArchived {
			continue
		}
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}

		content := p.Content
		changed := false

		// Find all [[slug]] references and remove dead ones
		for _, outSlug := range p.OutLinks {
			if liveSlugs[outSlug] {
				continue // link is alive
			}
			// Dead link — remove the [[slug]] from content, keep the display text if any
			linkPattern := "[[" + outSlug + "]]"
			if strings.Contains(content, linkPattern) {
				// Replace [[dead-slug]] with just the slug's readable part
				parts := strings.Split(outSlug, "/")
				readableName := parts[len(parts)-1]
				readableName = strings.ReplaceAll(readableName, "-", " ")
				content = strings.ReplaceAll(content, linkPattern, readableName)
				changed = true
			}
		}

		if changed {
			p.Content = content
			if _, err := s.wikiService.UpdatePage(ctx, p); err != nil {
				logger.Warnf(ctx, "wiki: failed to clean dead links in page %s: %v", p.Slug, err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		logger.Infof(ctx, "wiki: cleaned dead links in %d pages", cleaned)
	}
}

// injectCrossLinks scans affected pages and injects [[wiki-links]] for mentions
// of other wiki page titles in the content. Pure text replacement, no LLM call.
// Only processes entity/concept/synthesis/comparison pages (not index/log/summary).
func (s *wikiIngestService) injectCrossLinks(ctx context.Context, kbID string, affectedSlugs []string) {
	// Build a title→slug lookup from ALL pages in this KB
	allPages, err := s.wikiService.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) < 2 {
		return
	}

	type pageRef struct {
		slug  string
		title string
	}
	var allRefs []pageRef
	for _, p := range allPages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		if p.Title != "" {
			allRefs = append(allRefs, pageRef{slug: p.Slug, title: p.Title})
		}
	}
	if len(allRefs) == 0 {
		return
	}

	// Sort by title length descending — match longer names first to avoid
	// partial matches (e.g. "北京邮电大学" before "北京")
	for i := 0; i < len(allRefs); i++ {
		for j := i + 1; j < len(allRefs); j++ {
			if len([]rune(allRefs[j].title)) > len([]rune(allRefs[i].title)) {
				allRefs[i], allRefs[j] = allRefs[j], allRefs[i]
			}
		}
	}

	// Process only the pages we just created/updated
	affectedSet := make(map[string]bool, len(affectedSlugs))
	for _, s := range affectedSlugs {
		affectedSet[s] = true
	}

	var updated int
	for _, p := range allPages {
		if !affectedSet[p.Slug] {
			continue
		}
		// Skip system pages and summary (summary already has links from the prompt)
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog || p.PageType == types.WikiPageTypeSummary {
			continue
		}

		content := p.Content
		changed := false

		for _, ref := range allRefs {
			if ref.slug == p.Slug {
				continue
			}
			// Skip if already linked with this slug (either [[slug]] or [[slug|...]])
			if strings.Contains(content, "[["+ref.slug+"|") || strings.Contains(content, "[["+ref.slug+"]]") {
				continue
			}
			if strings.Contains(content, ref.title) {
				content = replaceFirstOutsideLinks(content, ref.title, "[["+ref.slug+"|"+ref.title+"]]")
				changed = true
			}
		}

		if changed {
			p.Content = content
			if _, err := s.wikiService.UpdatePage(ctx, p); err != nil {
				logger.Warnf(ctx, "wiki ingest: cross-link injection failed for %s: %v", p.Slug, err)
			} else {
				updated++
			}
		}
	}

	if updated > 0 {
		logger.Infof(ctx, "wiki ingest: injected cross-links in %d pages", updated)
	}
}

// replaceFirstOutsideLinks replaces the first occurrence of `old` with `new` in s,
// but only if it's not already inside a [[...]] wiki link.
func replaceFirstOutsideLinks(s, old, newStr string) string {
	idx := 0
	for {
		pos := strings.Index(s[idx:], old)
		if pos < 0 {
			return s // not found
		}
		absPos := idx + pos

		// Check if this occurrence is inside a [[ ... ]] link
		// Look backwards for [[ without encountering ]]
		insideLink := false
		for i := absPos - 1; i >= 0 && i >= absPos-200; i-- {
			if i > 0 && s[i-1:i+1] == "]]" {
				break // closed link before us
			}
			if i > 0 && s[i-1:i+1] == "[[" {
				insideLink = true
				break
			}
		}

		if !insideLink {
			return s[:absPos] + newStr + s[absPos+len(old):]
		}

		// Skip this match, try next
		idx = absPos + len(old)
	}
}

// getExistingPageSlugsForKnowledge returns all page slugs that currently reference
// a given knowledge ID in their source_refs. Used to snapshot state before re-ingest.
func (s *wikiIngestService) getExistingPageSlugsForKnowledge(ctx context.Context, kbID, knowledgeID string) map[string]bool {
	allPages, err := s.wikiService.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) == 0 {
		return nil
	}

	slugs := make(map[string]bool)
	prefix := knowledgeID + "|"
	for _, p := range allPages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		for _, ref := range p.SourceRefs {
			if ref == knowledgeID || strings.HasPrefix(ref, prefix) {
				slugs[p.Slug] = true
				break
			}
		}
	}
	return slugs
}

// retractStalePages handles pages that were previously linked to this document
// but are no longer produced by the updated extraction.
// - Single-source stale pages → deleted
// - Multi-source stale pages → LLM retract to clean content synchronously
func (s *wikiIngestService) retractStalePages(
	ctx context.Context,
	chatModel chat.Chat,
	payload WikiIngestPayload,
	oldSlugs map[string]bool,
	newSlugs []string,
	docTitle, docContent, lang string,
) {
	if len(oldSlugs) == 0 {
		return
	}

	// Build set of newly affected slugs (including summary)
	newSet := make(map[string]bool, len(newSlugs))
	for _, s := range newSlugs {
		newSet[s] = true
	}

	// Stale = was in old set but not in new set
	var staleSlugs []string
	for slug := range oldSlugs {
		if !newSet[slug] {
			staleSlugs = append(staleSlugs, slug)
		}
	}
	if len(staleSlugs) == 0 {
		return
	}

	logger.Infof(ctx, "wiki ingest: %d stale pages detected after document update: %v", len(staleSlugs), staleSlugs)

	var retractSlugs []string
	sourceRef := fmt.Sprintf("%s|%s", payload.KnowledgeID, docTitle)
	prefix := payload.KnowledgeID + "|"

	for _, slug := range staleSlugs {
		page, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
		if err != nil || page == nil {
			continue
		}

		// Remove this doc's source ref
		var remaining types.StringArray
		for _, ref := range page.SourceRefs {
			if ref == payload.KnowledgeID || ref == sourceRef || strings.HasPrefix(ref, prefix) {
				continue
			}
			remaining = append(remaining, ref)
		}

		if len(remaining) == 0 {
			// No other sources → delete the page
			if err := s.wikiService.DeletePage(ctx, payload.KnowledgeBaseID, slug); err != nil {
				logger.Warnf(ctx, "wiki ingest: failed to delete stale page %s: %v", slug, err)
			}
		} else {
			// Multi-source → remove ref, queue retract
			page.SourceRefs = remaining
			if err := s.wikiService.UpdatePageMeta(ctx, page); err != nil {
				logger.Warnf(ctx, "wiki ingest: failed to update stale page %s: %v", slug, err)
			} else {
				retractSlugs = append(retractSlugs, slug)
			}
		}
	}

	if len(retractSlugs) > 0 {
		s.retractPagesContent(ctx, chatModel, payload.KnowledgeBaseID, docTitle, docContent, retractSlugs, lang)
	}
}

// extractedItem represents a single extracted entity or concept
type extractedItem struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Details     string `json:"details"`
}

// combinedExtraction represents the parsed result of the combined entity+concept extraction
type combinedExtraction struct {
	Entities []extractedItem `json:"entities"`
	Concepts []extractedItem `json:"concepts"`
}

// extractEntitiesAndConcepts performs a single LLM call to extract both entities and concepts,
// then upserts pages for each. Returns the list of successfully upserted page slugs and
// a slug→display name map for building wiki-link references.
// oldPageSlugs contains slugs from the previous version of this document — passed to LLM for slug stability.
func (s *wikiIngestService) extractEntitiesAndConcepts(
	ctx context.Context,
	chatModel chat.Chat,
	content, docTitle, lang string,
	payload WikiIngestPayload,
	sourceRef string,
	oldPageSlugs map[string]bool,
) ([]string, map[string]string, error) {
	// Build previous slugs listing for the prompt
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

	// Single LLM call for both entities and concepts
	extractionJSON, err := s.generateWithTemplate(ctx, chatModel, agent.WikiKnowledgeExtractPrompt, map[string]string{
		"Title":         docTitle,
		"Content":       content,
		"Language":      lang,
		"PreviousSlugs": prevSlugsText,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("combined extraction failed: %w", err)
	}

	// Clean JSON - strip markdown code blocks if present
	extractionJSON = strings.TrimSpace(extractionJSON)
	extractionJSON = strings.TrimPrefix(extractionJSON, "```json")
	extractionJSON = strings.TrimPrefix(extractionJSON, "```")
	extractionJSON = strings.TrimSuffix(extractionJSON, "```")
	extractionJSON = strings.TrimSpace(extractionJSON)

	var result combinedExtraction
	if err := json.Unmarshal([]byte(extractionJSON), &result); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to parse combined extraction JSON: %v\nRaw: %s", err, truncateString(extractionJSON, 500))
		return nil, nil, fmt.Errorf("parse combined extraction JSON: %w", err)
	}

	var affected []string

	// Deduplicate entities against existing wiki pages (LLM-based)
	result.Entities = s.deduplicateItems(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, payload.KnowledgeBaseID)

	// Deduplicate concepts against existing wiki pages (LLM-based)
	result.Concepts = s.deduplicateItems(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, payload.KnowledgeBaseID)

	// Build slug→name map for wiki-link generation in summary pages
	slugNames := make(map[string]string)
	for _, item := range result.Entities {
		if item.Slug != "" && item.Name != "" {
			slugNames[item.Slug] = item.Name
		}
	}
	for _, item := range result.Concepts {
		if item.Slug != "" && item.Name != "" {
			slugNames[item.Slug] = item.Name
		}
	}

	// Upsert entity pages
	entitySlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, docTitle, lang, payload, sourceRef)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: entity upsert failed: %v", err)
	} else {
		affected = append(affected, entitySlugs...)
	}

	// Upsert concept pages
	conceptSlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, docTitle, lang, payload, sourceRef)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: concept upsert failed: %v", err)
	} else {
		affected = append(affected, conceptSlugs...)
	}

	return affected, slugNames, nil
}

// upsertExtractedPages creates or updates wiki pages from pre-extracted items.
func (s *wikiIngestService) upsertExtractedPages(
	ctx context.Context,
	chatModel chat.Chat,
	items []extractedItem,
	pageType string,
	docTitle, lang string,
	payload WikiIngestPayload,
	sourceRef string,
) ([]string, error) {
	var affected []string
	for _, item := range items {
		if item.Slug == "" || item.Name == "" {
			continue
		}

		// Check if page already exists
		existing, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, item.Slug)
		if err == nil && existing != nil {
			// Page exists → incremental update
			updatedContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiPageUpdatePrompt, map[string]string{
				"ExistingContent": existing.Content,
				"NewDocTitle":     docTitle,
				"NewContent":      fmt.Sprintf("**%s**: %s\n\n%s", item.Name, item.Description, item.Details),
				"Language":        lang,
			})
			if err != nil {
				logger.Warnf(ctx, "wiki ingest: update page %s failed: %v", item.Slug, err)
				continue
			}

			// LLM returns "SUMMARY: ..." on first line — use it as the authoritative summary
			updatedSummary, updatedBody := splitSummaryLine(updatedContent)
			if updatedBody != "" {
				existing.Content = updatedBody
			} else {
				existing.Content = updatedContent
			}
			if updatedSummary != "" {
				existing.Summary = updatedSummary
			}
			existing.SourceRefs = appendUnique(existing.SourceRefs, sourceRef)

			if _, err := s.wikiService.UpdatePage(ctx, existing); err != nil {
				logger.Warnf(ctx, "wiki ingest: save updated page %s failed: %v", item.Slug, err)
				continue
			}
			affected = append(affected, item.Slug)
		} else {
			// New page
			pageContent := fmt.Sprintf("# %s\n\n%s\n\n%s\n\n---\n*Source: %s*\n",
				item.Name, item.Description, item.Details, docTitle)

			if _, err := s.wikiService.CreatePage(ctx, &types.WikiPage{
				ID:              uuid.New().String(),
				TenantID:        payload.TenantID,
				KnowledgeBaseID: payload.KnowledgeBaseID,
				Slug:            item.Slug,
				Title:           item.Name,
				PageType:        pageType,
				Status:          types.WikiPageStatusDraft,
				Content:         pageContent,
				Summary:         item.Description,
				SourceRefs:      types.StringArray{sourceRef},
			}); err != nil {
				logger.Warnf(ctx, "wiki ingest: create page %s failed: %v", item.Slug, err)
				continue
			}
			affected = append(affected, item.Slug)
		}
	}

	return affected, nil
}

// rebuildIndexPage regenerates the index page.
//
// Strategy: Index = LLM-generated intro (stored in Summary field) + code-generated directory.
//   - Intro: stored in indexPage.Summary. First time: generated from document summaries.
//     Subsequent: incrementally updated with changeDescription.
//   - Directory: pure code, rebuilt every time. O(N) string concat, no LLM.
func (s *wikiIngestService) rebuildIndexPage(ctx context.Context, chatModel chat.Chat, payload WikiIngestPayload, changeDesc, lang string) error {
	indexPage, _ := s.wikiService.GetIndex(ctx, payload.KnowledgeBaseID)
	if indexPage == nil {
		return nil
	}

	// List all live pages
	allPages, err := s.wikiService.ListAllPages(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return err
	}

	typeOrder := []string{
		types.WikiPageTypeSummary, types.WikiPageTypeEntity, types.WikiPageTypeConcept,
		types.WikiPageTypeSynthesis, types.WikiPageTypeComparison,
	}
	typeLabels := map[string]string{
		types.WikiPageTypeSummary: "Summary", types.WikiPageTypeEntity: "Entity",
		types.WikiPageTypeConcept: "Concept", types.WikiPageTypeSynthesis: "Synthesis",
		types.WikiPageTypeComparison: "Comparison",
	}

	grouped := make(map[string][]*types.WikiPage)
	totalPages := 0
	for _, p := range allPages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		if p.Status == types.WikiPageStatusArchived {
			continue
		}
		grouped[p.PageType] = append(grouped[p.PageType], p)
		totalPages++
	}

	// Build document summaries listing (only summary-type pages — they represent documents)
	var docSummaries strings.Builder
	for _, p := range grouped[types.WikiPageTypeSummary] {
		fmt.Fprintf(&docSummaries, "- %s: %s\n", p.Title, p.Summary)
	}
	if docSummaries.Len() == 0 {
		docSummaries.WriteString("(no documents yet)")
	}

	// Generate or update intro
	existingIntro := indexPage.Summary
	var intro string

	if existingIntro == "" || existingIntro == "Wiki index - table of contents" {
		// First time — generate intro from scratch
		generatedIntro, genErr := s.generateWithTemplate(ctx, chatModel, agent.WikiIndexIntroPrompt, map[string]string{
			"DocumentSummaries": docSummaries.String(),
			"Language":          lang,
		})
		if genErr != nil {
			intro = "# Wiki Index\n\nThis wiki contains knowledge extracted from uploaded documents.\n"
		} else {
			intro = strings.TrimSpace(generatedIntro)
		}
	} else if changeDesc != "" {
		// Incremental update — tell LLM what changed
		updatedIntro, genErr := s.generateWithTemplate(ctx, chatModel, agent.WikiIndexIntroUpdatePrompt, map[string]string{
			"ExistingIntro":     existingIntro,
			"ChangeDescription": changeDesc,
			"DocumentSummaries": docSummaries.String(),
			"Language":          lang,
		})
		if genErr != nil {
			intro = existingIntro // keep existing on error
		} else {
			intro = strings.TrimSpace(updatedIntro)
		}
	} else {
		intro = existingIntro // no change description, keep as-is
	}

	// Build directory (pure code, no LLM)
	var dir strings.Builder
	for _, pt := range typeOrder {
		pages := grouped[pt]
		if len(pages) == 0 {
			continue
		}
		fmt.Fprintf(&dir, "\n## %s (%d)\n\n", typeLabels[pt], len(pages))
		for _, p := range pages {
			summary := p.Summary
			fmt.Fprintf(&dir, "[[%s]] — %s\n", p.Slug, summary)
		}
	}
	for pt, pages := range grouped {
		inOrder := false
		for _, o := range typeOrder {
			if o == pt {
				inOrder = true
				break
			}
		}
		if inOrder || len(pages) == 0 {
			continue
		}
		fmt.Fprintf(&dir, "\n## %s (%d)\n\n", pt, len(pages))
		for _, p := range pages {
			fmt.Fprintf(&dir, "[[%s]] — %s\n", p.Slug, p.Summary)
		}
	}
	if totalPages == 0 {
		dir.WriteString("\n*No wiki pages yet. Upload documents to get started.*\n")
	}

	indexPage.Content = intro + "\n" + dir.String()
	indexPage.Summary = intro // persist intro for next incremental update
	_, err = s.wikiService.UpdatePage(ctx, indexPage)
	return err
}

// splitSummaryLine extracts the "SUMMARY: ..." line from LLM output.
// Returns (summary, content). If no SUMMARY line found, summary is empty.
func splitSummaryLine(raw string) (summary string, content string) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "SUMMARY:") || strings.HasPrefix(raw, "SUMMARY：") {
		idx := strings.IndexByte(raw, '\n')
		if idx < 0 {
			// Only one line
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(raw, "SUMMARY:"), "SUMMARY：")), ""
		}
		summaryLine := raw[:idx]
		summaryLine = strings.TrimPrefix(summaryLine, "SUMMARY:")
		summaryLine = strings.TrimPrefix(summaryLine, "SUMMARY：")
		return strings.TrimSpace(summaryLine), strings.TrimSpace(raw[idx+1:])
	}
	return "", raw
}

// appendLogEntry appends an entry to the log page, including any synthesis suggestions
// appendLogEntry appends a structured, grep-parseable entry to the log page.
// Format: ## [2026-04-07 19:50:02] action | title
// Followed by key-value metadata lines. No sub-headings — keeps `grep "^## \[" log.md` clean.
func (s *wikiIngestService) appendLogEntry(ctx context.Context, payload WikiIngestPayload, action, docTitle string, pagesAffected []string, extra string) {
	logPage, _ := s.wikiService.GetLog(ctx, payload.KnowledgeBaseID)
	if logPage == nil {
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n## [%s] %s | %s\n",
		time.Now().UTC().Format("2006-01-02 15:04:05"),
		action,
		docTitle,
	)
	fmt.Fprintf(&sb, "- **Source**: knowledge/%s\n", payload.KnowledgeID)
	if len(pagesAffected) > 0 {
		fmt.Fprintf(&sb, "- **Pages affected**: %d (%s)\n", len(pagesAffected), strings.Join(pagesAffected, ", "))
	}
	if extra != "" {
		sb.WriteString(extra)
	}

	logPage.Content = logPage.Content + sb.String()
	if _, err := s.wikiService.UpdatePage(ctx, logPage); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to update log page: %v", err)
	}
}

// publishDraftPages transitions draft pages to published status after ingest completes.
// This ensures users don't see half-built pages during the ingest process.
func (s *wikiIngestService) publishDraftPages(ctx context.Context, kbID string, slugs []string) {
	for _, slug := range slugs {
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, slug)
		if err != nil || page == nil {
			continue
		}
		if page.Status == types.WikiPageStatusDraft {
			page.Status = types.WikiPageStatusPublished
			if err := s.wikiService.UpdatePageMeta(ctx, page); err != nil {
				logger.Warnf(ctx, "wiki ingest: failed to publish page %s: %v", slug, err)
			}
		}
	}
}

// deduplicateItems uses LLM to identify new items that refer to the same entity/concept
// as existing wiki pages, and remaps their slugs so upsertExtractedPages merges them.
func (s *wikiIngestService) deduplicateItems(
	ctx context.Context,
	chatModel chat.Chat,
	items []extractedItem,
	pageType string,
	kbID string,
) []extractedItem {
	if len(items) == 0 {
		return items
	}

	// Get existing pages of the same type
	allPages, err := s.wikiService.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) == 0 {
		return items
	}

	// Filter to the target type
	var typedPages []*types.WikiPage
	for _, p := range allPages {
		if p.PageType == pageType {
			typedPages = append(typedPages, p)
		}
	}
	if len(typedPages) == 0 {
		return items // No existing pages of this type → nothing to deduplicate against
	}

	// Build existing pages listing
	var existingBuf strings.Builder
	for _, p := range typedPages {
		fmt.Fprintf(&existingBuf, "- slug: %s | title: %s\n", p.Slug, p.Title)
	}

	// Build new items listing
	var newBuf strings.Builder
	for _, item := range items {
		fmt.Fprintf(&newBuf, "- slug: %s | name: %s\n", item.Slug, item.Name)
	}

	// Call LLM for deduplication
	dedupeJSON, err := s.generateWithTemplate(ctx, chatModel, agent.WikiDeduplicationPrompt, map[string]string{
		"NewItems":      newBuf.String(),
		"ExistingPages": existingBuf.String(),
	})
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: deduplication LLM call failed: %v", err)
		return items
	}

	// Parse response
	dedupeJSON = strings.TrimSpace(dedupeJSON)
	dedupeJSON = strings.TrimPrefix(dedupeJSON, "```json")
	dedupeJSON = strings.TrimPrefix(dedupeJSON, "```")
	dedupeJSON = strings.TrimSuffix(dedupeJSON, "```")
	dedupeJSON = strings.TrimSpace(dedupeJSON)

	var dedupeResult struct {
		Merges map[string]string `json:"merges"`
	}
	if err := json.Unmarshal([]byte(dedupeJSON), &dedupeResult); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to parse dedup JSON: %v", err)
		return items
	}

	if len(dedupeResult.Merges) == 0 {
		return items
	}

	// Remap slugs for matched items
	for i, item := range items {
		if existingSlug, ok := dedupeResult.Merges[item.Slug]; ok {
			logger.Infof(ctx, "wiki ingest: dedup merge %s → %s", item.Slug, existingSlug)
			items[i].Slug = existingSlug
		}
	}

	return items
}

// generateWithTemplate executes a prompt template and calls the LLM
func (s *wikiIngestService) generateWithTemplate(ctx context.Context, chatModel chat.Chat, promptTpl string, data map[string]string) (string, error) {
	tmpl, err := template.New("wiki").Parse(promptTpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	prompt := buf.String()
	thinking := false
	response, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "user", Content: prompt},
	}, &chat.ChatOptions{
		Temperature: 0.3,
		Thinking:    &thinking,
	})
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	return response.Content, nil
}

// --- Helpers ---

// reconstructContent rebuilds document text from chunks
func reconstructContent(chunks []*types.Chunk) string {
	var textChunks []*types.Chunk
	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeText || c.ChunkType == "" {
			textChunks = append(textChunks, c)
		}
	}

	// Sort by chunk index
	for i := 0; i < len(textChunks); i++ {
		for j := i + 1; j < len(textChunks); j++ {
			if textChunks[i].ChunkIndex > textChunks[j].ChunkIndex {
				textChunks[i], textChunks[j] = textChunks[j], textChunks[i]
			}
		}
	}

	var sb strings.Builder
	for _, c := range textChunks {
		sb.WriteString(c.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// slugify creates a URL-friendly slug from a string
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '/' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		// Keep CJK characters
		if r >= 0x4E00 && r <= 0x9FFF {
			return r
		}
		return -1
	}, s)
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// truncateString truncates a string to maxLen runes
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// appendUnique appends a string to a StringArray if not already present
func appendUnique(arr types.StringArray, s string) types.StringArray {
	for _, v := range arr {
		if v == s {
			return arr
		}
	}
	return append(arr, s)
}
