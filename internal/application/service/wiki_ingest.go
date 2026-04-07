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
)

const (
	// maxContentForWiki limits the document content sent to LLM for wiki generation
	maxContentForWiki = 32768
)

// WikiIngestPayload is the asynq task payload for wiki ingest
type WikiIngestPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
	Language        string `json:"language,omitempty"` // locale code, e.g. "zh-CN"
}

// WikiRetractPayload is the asynq task payload for wiki content retraction
type WikiRetractPayload struct {
	TenantID        uint64   `json:"tenant_id"`
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	KnowledgeID     string   `json:"knowledge_id"`
	DocTitle        string   `json:"doc_title"`
	Language        string   `json:"language,omitempty"`
	PageSlugs       []string `json:"page_slugs"` // pages to retract content from
}

// wikiIngestService handles the LLM-powered wiki generation pipeline
type wikiIngestService struct {
	wikiService  interfaces.WikiPageService
	kbService    interfaces.KnowledgeBaseService
	chunkRepo    interfaces.ChunkRepository
	modelService interfaces.ModelService
	task         interfaces.TaskEnqueuer
}

// NewWikiIngestService creates a new wiki ingest service
func NewWikiIngestService(
	wikiService interfaces.WikiPageService,
	kbService interfaces.KnowledgeBaseService,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
) interfaces.TaskHandler {
	return &wikiIngestService{
		wikiService:  wikiService,
		kbService:    kbService,
		chunkRepo:    chunkRepo,
		modelService: modelService,
		task:         task,
	}
}

// EnqueueWikiIngest enqueues an async wiki ingest task
func EnqueueWikiIngest(ctx context.Context, task interfaces.TaskEnqueuer, tenantID uint64, kbID, knowledgeID string) {
	lang, _ := types.LanguageFromContext(ctx)
	payload := WikiIngestPayload{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		KnowledgeID:     knowledgeID,
		Language:        lang,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "wiki ingest: failed to marshal payload: %v", err)
		return
	}
	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(2))
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
	t := asynq.NewTask(types.TypeWikiRetract, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(2))
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki retract: failed to enqueue task: %v", err)
	}
}

// Handle implements interfaces.TaskHandler for asynq task processing
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

	for _, slug := range payload.PageSlugs {
		page, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
		if err != nil || page == nil {
			continue
		}

		// Build remaining sources list
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

		// Call LLM to retract content
		updatedContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiPageRetractPrompt, map[string]string{
			"ExistingContent":  page.Content,
			"DeletedDocTitle":  payload.DocTitle,
			"RemainingSources": remainingSources,
			"Language":         lang,
		})
		if err != nil {
			logger.Warnf(ctx, "wiki retract: LLM call failed for page %s: %v", slug, err)
			continue
		}

		page.Content = updatedContent
		if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
			logger.Warnf(ctx, "wiki retract: failed to update page %s: %v", slug, err)
		} else {
			logger.Infof(ctx, "wiki retract: updated page %s after removing content from '%s'", slug, payload.DocTitle)
		}
	}

	// Rebuild index page to reflect changes
	if err := s.rebuildIndexPage(ctx, chatModel, WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
	}, lang); err != nil {
		logger.Warnf(ctx, "wiki retract: rebuild index failed: %v", err)
	}

	// Append log entry
	s.appendLogEntry(ctx, WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
	}, payload.DocTitle+" [DELETED]", payload.PageSlugs, nil)

	return nil
}

// ProcessWikiIngest processes a wiki ingest task (asynq handler)
func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, t *asynq.Task) error {
	var payload WikiIngestPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("wiki ingest: unmarshal payload: %w", err)
	}

	logger.Infof(ctx, "wiki ingest: starting for knowledge %s in KB %s", payload.KnowledgeID, payload.KnowledgeBaseID)

	// Inject tenant ID into context — asynq tasks don't have it from middleware
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)

	// Inject language into context — captured from the original HTTP request at enqueue time
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// Get KB and validate it's wiki-enabled
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
		synthesisModelID = kb.SummaryModelID // fallback
	}
	if synthesisModelID == "" {
		return fmt.Errorf("wiki ingest: no synthesis model configured for KB %s", kb.ID)
	}
	chatModel, err := s.modelService.GetChatModel(ctx, synthesisModelID)
	if err != nil {
		return fmt.Errorf("wiki ingest: get chat model: %w", err)
	}

	// Get document chunks and reconstruct content
	chunks, err := s.chunkRepo.ListChunksByKnowledgeID(ctx, payload.TenantID, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("wiki ingest: get chunks: %w", err)
	}
	if len(chunks) == 0 {
		logger.Warnf(ctx, "wiki ingest: no chunks found for knowledge %s", payload.KnowledgeID)
		return nil
	}

	// Reconstruct document content from text chunks
	content := reconstructContent(chunks)
	if len([]rune(content)) > maxContentForWiki {
		content = string([]rune(content)[:maxContentForWiki])
	}

	// Get human-readable language name for LLM prompts from middleware context
	lang := types.LanguageNameFromContext(ctx)

	// Get document title from first chunk's knowledge info
	docTitle := payload.KnowledgeID
	if len(chunks) > 0 {
		// Try to extract a title
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

	var pagesAffected []string
	var synthesisSuggestions []string

	// Format source ref as "knowledgeID|docTitle" for frontend display
	sourceRef := fmt.Sprintf("%s|%s", payload.KnowledgeID, docTitle)

	// Snapshot: existing page slugs that reference this knowledge ID (before extraction)
	// Used later to detect pages that are no longer relevant after document update
	oldPageSlugs := s.getExistingPageSlugsForKnowledge(ctx, payload.KnowledgeBaseID, payload.KnowledgeID)

	// Step 1: Extract entities and concepts FIRST (so we know the slugs for summary links)
	extractedPages, extractedSlugs, err := s.extractEntitiesAndConcepts(ctx, chatModel, content, docTitle, lang, payload, sourceRef, oldPageSlugs)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: knowledge extraction failed: %v", err)
	} else {
		pagesAffected = append(pagesAffected, extractedPages...)
	}

	// Step 2: Generate summary page (with extracted slugs for accurate [[wiki-links]])
	summarySlug := fmt.Sprintf("summary/%s", slugify(docTitle))
	var slugListing string
	for _, slug := range extractedSlugs {
		slugListing += fmt.Sprintf("- [[%s]]\n", slug)
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
		logger.Errorf(ctx, "wiki ingest: generate summary failed: %v", err)
	} else {
		_, err := s.wikiService.CreatePage(ctx, &types.WikiPage{
			ID:              uuid.New().String(),
			TenantID:        payload.TenantID,
			KnowledgeBaseID: payload.KnowledgeBaseID,
			Slug:            summarySlug,
			Title:           docTitle + " - Summary",
			PageType:        types.WikiPageTypeSummary,
			Status:          types.WikiPageStatusDraft,
			Content:         summaryContent,
			Summary:         truncateString(summaryContent, 200),
			SourceRefs:      types.StringArray{sourceRef},
		})
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: create summary page failed: %v", err)
		} else {
			pagesAffected = append(pagesAffected, summarySlug)
		}
	}

	// Step 3: Detect synthesis opportunities (no LLM call, pure heuristic)
	synthesisSuggestions = s.detectSynthesisOpportunities(ctx, payload)

	// Step 4: Rebuild index page
	if err := s.rebuildIndexPage(ctx, chatModel, payload, lang); err != nil {
		logger.Warnf(ctx, "wiki ingest: rebuild index failed: %v", err)
	}

	// Step 5: Append to log page with synthesis suggestions
	s.appendLogEntry(ctx, payload, docTitle, pagesAffected, synthesisSuggestions)

	// Step 6: Publish all draft pages created during this ingest
	s.publishDraftPages(ctx, payload.KnowledgeBaseID, pagesAffected)

	// Step 7: Handle stale pages — pages that previously referenced this document
	// but are no longer produced by the updated extraction. This handles the
	// "document updated and some entities/concepts were removed" scenario.
	s.retractStalePages(ctx, payload, oldPageSlugs, pagesAffected, docTitle, lang)

	logger.Infof(ctx, "wiki ingest: completed for knowledge %s, %d pages affected",
		payload.KnowledgeID, len(pagesAffected))

	return nil
}

// getExistingPageSlugsForKnowledge returns all page slugs that currently reference
// a given knowledge ID in their source_refs. Used to snapshot state before re-ingest.
func (s *wikiIngestService) getExistingPageSlugsForKnowledge(ctx context.Context, kbID, knowledgeID string) map[string]bool {
	resp, err := s.wikiService.ListPages(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: kbID,
		PageSize:        500,
	})
	if err != nil || resp == nil {
		return nil
	}

	slugs := make(map[string]bool)
	prefix := knowledgeID + "|"
	for _, p := range resp.Pages {
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
// - Single-source stale pages → archived
// - Multi-source stale pages → enqueue LLM retract to clean content
func (s *wikiIngestService) retractStalePages(
	ctx context.Context,
	payload WikiIngestPayload,
	oldSlugs map[string]bool,
	newSlugs []string,
	docTitle, lang string,
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
			// No other sources → archive
			page.Status = types.WikiPageStatusArchived
			page.SourceRefs = remaining
			if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
				logger.Warnf(ctx, "wiki ingest: failed to archive stale page %s: %v", slug, err)
			}
		} else {
			// Multi-source → remove ref, queue retract
			page.SourceRefs = remaining
			if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
				logger.Warnf(ctx, "wiki ingest: failed to update stale page %s: %v", slug, err)
			} else {
				retractSlugs = append(retractSlugs, slug)
			}
		}
	}

	// Enqueue LLM retraction for multi-source stale pages
	if len(retractSlugs) > 0 {
		EnqueueWikiRetract(ctx, s.task, WikiRetractPayload{
			TenantID:        payload.TenantID,
			KnowledgeBaseID: payload.KnowledgeBaseID,
			KnowledgeID:     payload.KnowledgeID,
			DocTitle:        docTitle,
			Language:        payload.Language,
			PageSlugs:       retractSlugs,
		})
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
// then upserts pages for each. Returns the list of affected page slugs and all extracted slugs (for linking).
// oldPageSlugs contains slugs from the previous version of this document — passed to LLM for slug stability.
func (s *wikiIngestService) extractEntitiesAndConcepts(
	ctx context.Context,
	chatModel chat.Chat,
	content, docTitle, lang string,
	payload WikiIngestPayload,
	sourceRef string,
	oldPageSlugs map[string]bool,
) ([]string, []string, error) {
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

	// Collect all extracted slugs (before dedup remapping) for summary linking
	var allSlugs []string
	for _, item := range result.Entities {
		if item.Slug != "" {
			allSlugs = append(allSlugs, item.Slug)
		}
	}
	for _, item := range result.Concepts {
		if item.Slug != "" {
			allSlugs = append(allSlugs, item.Slug)
		}
	}

	var affected []string

	// Deduplicate entities against existing wiki pages (LLM-based)
	result.Entities = s.deduplicateItems(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, payload.KnowledgeBaseID)

	// Deduplicate concepts against existing wiki pages (LLM-based)
	result.Concepts = s.deduplicateItems(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, payload.KnowledgeBaseID)

	// Update allSlugs after dedup (use final slugs)
	allSlugs = allSlugs[:0]
	for _, item := range result.Entities {
		if item.Slug != "" {
			allSlugs = append(allSlugs, item.Slug)
		}
	}
	for _, item := range result.Concepts {
		if item.Slug != "" {
			allSlugs = append(allSlugs, item.Slug)
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

	return affected, allSlugs, nil
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

			existing.Content = updatedContent
			existing.Summary = item.Description
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

// detectSynthesisOpportunities checks if there are enough pages to suggest synthesis.
// Pure heuristic — no LLM call, just counts pages by type and returns suggestion strings.
func (s *wikiIngestService) detectSynthesisOpportunities(ctx context.Context, payload WikiIngestPayload) []string {
	resp, err := s.wikiService.ListPages(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: payload.KnowledgeBaseID,
		PageSize:        500,
		SortBy:          "page_type",
		SortOrder:       "asc",
	})
	if err != nil {
		return nil
	}

	// Count pages by type, collect example titles
	typeCounts := make(map[string]int)
	typeExamples := make(map[string][]string)
	for _, p := range resp.Pages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		typeCounts[p.PageType]++
		if len(typeExamples[p.PageType]) < 5 {
			typeExamples[p.PageType] = append(typeExamples[p.PageType], p.Title)
		}
	}

	var suggestions []string

	if count := typeCounts[types.WikiPageTypeEntity]; count >= 3 {
		examples := strings.Join(typeExamples[types.WikiPageTypeEntity], ", ")
		suggestions = append(suggestions,
			fmt.Sprintf("Synthesis opportunity: %d entity pages (e.g. %s) could be synthesized into a comparison or overview page", count, examples))
	}

	if count := typeCounts[types.WikiPageTypeConcept]; count >= 3 {
		examples := strings.Join(typeExamples[types.WikiPageTypeConcept], ", ")
		suggestions = append(suggestions,
			fmt.Sprintf("Synthesis opportunity: %d concept pages (e.g. %s) could be synthesized into a thematic overview", count, examples))
	}

	return suggestions
}

// rebuildIndexPage regenerates the index page from all existing pages
func (s *wikiIngestService) rebuildIndexPage(ctx context.Context, chatModel chat.Chat, payload WikiIngestPayload, lang string) error {
	// List all pages
	resp, err := s.wikiService.ListPages(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: payload.KnowledgeBaseID,
		PageSize:        500,
		SortBy:          "page_type",
		SortOrder:       "asc",
	})
	if err != nil {
		return err
	}

	// Build page listing
	var listing strings.Builder
	for _, p := range resp.Pages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		listing.WriteString(fmt.Sprintf("- [%s] [[%s]] | %s — %s\n", p.PageType, p.Slug, p.Title, p.Summary))
	}

	indexContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiIndexRebuildPrompt, map[string]string{
		"PageListing": listing.String(),
		"Language":    lang,
	})
	if err != nil {
		return fmt.Errorf("generate index: %w", err)
	}

	// Get or create index page
	indexPage, _ := s.wikiService.GetIndex(ctx, payload.KnowledgeBaseID)
	if indexPage != nil {
		indexPage.Content = indexContent
		indexPage.Summary = "Wiki index - table of contents"
		if _, err := s.wikiService.UpdatePage(ctx, indexPage); err != nil {
			return fmt.Errorf("update index page: %w", err)
		}
	}

	return nil
}

// appendLogEntry appends an entry to the log page, including any synthesis suggestions
func (s *wikiIngestService) appendLogEntry(ctx context.Context, payload WikiIngestPayload, docTitle string, pagesAffected []string, suggestions []string) {
	logPage, _ := s.wikiService.GetLog(ctx, payload.KnowledgeBaseID)
	if logPage == nil {
		return
	}

	entry := fmt.Sprintf("\n## [%s] ingest | %s\n- **Source**: knowledge/%s\n- **Pages affected**: %d (%s)\n",
		time.Now().UTC().Format("2006-01-02 15:04"),
		docTitle,
		payload.KnowledgeID,
		len(pagesAffected),
		strings.Join(pagesAffected, ", "),
	)

	if len(suggestions) > 0 {
		entry += "\n### Synthesis Opportunities\n"
		for _, suggestion := range suggestions {
			entry += fmt.Sprintf("- %s\n", suggestion)
		}
	}

	logPage.Content = logPage.Content + entry
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
			if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
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
	resp, err := s.wikiService.ListPages(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: kbID,
		PageType:        pageType,
		PageSize:        2000,
	})
	if err != nil || resp == nil || len(resp.Pages) == 0 {
		return items // No existing pages → nothing to deduplicate against
	}

	// Build existing pages listing
	var existingBuf strings.Builder
	for _, p := range resp.Pages {
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
