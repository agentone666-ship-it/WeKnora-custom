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
	// TypeWikiIngest is the asynq task type for wiki ingest
	TypeWikiIngest = "wiki:ingest"

	// maxContentForWiki limits the document content sent to LLM for wiki generation
	maxContentForWiki = 32768
)

// WikiIngestPayload is the asynq task payload for wiki ingest
type WikiIngestPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
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
	payload := WikiIngestPayload{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		KnowledgeID:     knowledgeID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "wiki ingest: failed to marshal payload: %v", err)
		return
	}
	t := asynq.NewTask(TypeWikiIngest, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(2))
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to enqueue task: %v", err)
	}
}

// Handle implements interfaces.TaskHandler for asynq task processing
func (s *wikiIngestService) Handle(ctx context.Context, t *asynq.Task) error {
	return s.ProcessWikiIngest(ctx, t)
}

// ProcessWikiIngest processes a wiki ingest task (asynq handler)
func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, t *asynq.Task) error {
	var payload WikiIngestPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("wiki ingest: unmarshal payload: %w", err)
	}

	logger.Infof(ctx, "wiki ingest: starting for knowledge %s in KB %s", payload.KnowledgeID, payload.KnowledgeBaseID)

	// Get KB and validate it's a wiki type
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("wiki ingest: get KB: %w", err)
	}
	if kb.Type != types.KnowledgeBaseTypeWiki {
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


	// Get human-readable language name for LLM prompts
	// Reuses language mapping from middleware infrastructure (supports 9+ languages)
	// Maps locale codes like "zh", "en" to names like "Chinese (Simplified)", "English"
	lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)

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

	// Step 1: Generate summary page
	summarySlug := fmt.Sprintf("summary/%s", slugify(docTitle))
	summaryContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiSummaryPrompt, map[string]string{
		"Title":    docTitle,
		"FileName": docTitle,
		"FileType": "document",
		"Content":  content,
		"Language": lang,
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
			Status:          types.WikiPageStatusPublished,
			Content:         summaryContent,
			Summary:         truncateString(summaryContent, 200),
			SourceRefs:      types.StringArray{payload.KnowledgeID},
		})
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: create summary page failed: %v", err)
		} else {
			pagesAffected = append(pagesAffected, summarySlug)
		}
	}

	// Step 2: Extract entities and concepts in a single LLM call, then create/update pages
	extractedPages, err := s.extractEntitiesAndConcepts(ctx, chatModel, content, docTitle, lang, payload)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: knowledge extraction failed: %v", err)
	} else {
		pagesAffected = append(pagesAffected, extractedPages...)
	}

	// Step 3: Detect synthesis opportunities (no LLM call, pure heuristic)
	synthesisSuggestions = s.detectSynthesisOpportunities(ctx, payload)

	// Step 4: Rebuild index page
	if err := s.rebuildIndexPage(ctx, chatModel, payload, lang); err != nil {
		logger.Warnf(ctx, "wiki ingest: rebuild index failed: %v", err)
	}

	// Step 5: Append to log page with synthesis suggestions
	s.appendLogEntry(ctx, payload, docTitle, pagesAffected, synthesisSuggestions)

	logger.Infof(ctx, "wiki ingest: completed for knowledge %s, %d pages affected",
		payload.KnowledgeID, len(pagesAffected))

	return nil
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
// then upserts pages for each. Returns the list of affected page slugs.
func (s *wikiIngestService) extractEntitiesAndConcepts(
	ctx context.Context,
	chatModel chat.Chat,
	content, docTitle, lang string,
	payload WikiIngestPayload,
) ([]string, error) {
	// Single LLM call for both entities and concepts
	extractionJSON, err := s.generateWithTemplate(ctx, chatModel, agent.WikiKnowledgeExtractPrompt, map[string]string{
		"Title":   docTitle,
		"Content": content,
	})
	if err != nil {
		return nil, fmt.Errorf("combined extraction failed: %w", err)
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
		return nil, fmt.Errorf("parse combined extraction JSON: %w", err)
	}

	var affected []string

	// Upsert entity pages
	entitySlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, docTitle, lang, payload)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: entity upsert failed: %v", err)
	} else {
		affected = append(affected, entitySlugs...)
	}

	// Upsert concept pages
	conceptSlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, docTitle, lang, payload)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: concept upsert failed: %v", err)
	} else {
		affected = append(affected, conceptSlugs...)
	}

	return affected, nil
}

// upsertExtractedPages creates or updates wiki pages from pre-extracted items.
func (s *wikiIngestService) upsertExtractedPages(
	ctx context.Context,
	chatModel chat.Chat,
	items []extractedItem,
	pageType string,
	docTitle, lang string,
	payload WikiIngestPayload,
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
			existing.SourceRefs = appendUnique(existing.SourceRefs, payload.KnowledgeID)

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
				Status:          types.WikiPageStatusPublished,
				Content:         pageContent,
				Summary:         item.Description,
				SourceRefs:      types.StringArray{payload.KnowledgeID},
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
