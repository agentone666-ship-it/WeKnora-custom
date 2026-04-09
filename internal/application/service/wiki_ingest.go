package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	Language        string `json:"language,omitempty"`
	// Fallback for Lite mode (no Redis)
	LiteOps []WikiPendingOp `json:"lite_ops,omitempty"`
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

const (
	WikiOpIngest  = "ingest"
	WikiOpRetract = "retract"
)

// WikiPendingOp represents a single operation in the Redis pending queue
type WikiPendingOp struct {
	Op          string `json:"op"`
	KnowledgeID string `json:"knowledge_id"`
	// Ingest fields
	Language string `json:"language,omitempty"`
	// Retract fields
	DocTitle   string   `json:"doc_title,omitempty"`
	DocSummary string   `json:"doc_summary,omitempty"`
	PageSlugs  []string `json:"page_slugs,omitempty"`
}

// wikiIngestService handles the LLM-powered wiki generation pipeline
type wikiIngestService struct {
	wikiService  interfaces.WikiPageService
	kbService    interfaces.KnowledgeBaseService
	knowledgeSvc interfaces.KnowledgeService
	chunkRepo    interfaces.ChunkRepository
	modelService interfaces.ModelService
	task         interfaces.TaskEnqueuer
	redisClient  *redis.Client // nil in Lite mode (no Redis)
}

// NewWikiIngestService creates a new wiki ingest service
func NewWikiIngestService(
	wikiService interfaces.WikiPageService,
	kbService interfaces.KnowledgeBaseService,
	knowledgeSvc interfaces.KnowledgeService,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	svc := &wikiIngestService{
		wikiService:  wikiService,
		kbService:    kbService,
		knowledgeSvc: knowledgeSvc,
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

	payload := WikiIngestPayload{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		Language:        lang,
	}

	// Push to Redis pending list (if Redis available)
	if redisClient != nil {
		pendingKey := wikiPendingKeyPrefix + kbID
		op := WikiPendingOp{
			Op:          WikiOpIngest,
			KnowledgeID: knowledgeID,
			Language:    lang,
		}
		opBytes, _ := json.Marshal(op)
		redisClient.RPush(ctx, pendingKey, string(opBytes))
		redisClient.Expire(ctx, pendingKey, wikiPendingTTL)
	} else {
		// Fallback for Lite mode (no Redis)
		payload.LiteOps = []WikiPendingOp{{
			Op:          WikiOpIngest,
			KnowledgeID: knowledgeID,
			Language:    lang,
		}}
	}

	payloadBytes, _ := json.Marshal(payload)

	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes,
		asynq.Queue("low"),
		asynq.MaxRetry(10), // Increased from 3 to 10 to ensure it can outlast the 5-minute active lock TTL
		asynq.Timeout(60*time.Minute),
		asynq.ProcessIn(wikiIngestDelay),
	)
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to enqueue task: %v", err)
	}
}

// EnqueueWikiRetract enqueues an async wiki content retraction task
func EnqueueWikiRetract(ctx context.Context, task interfaces.TaskEnqueuer, redisClient *redis.Client, payload WikiRetractPayload) {
	ingestPayload := WikiIngestPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		Language:        payload.Language,
	}

	op := WikiPendingOp{
		Op:          WikiOpRetract,
		KnowledgeID: payload.KnowledgeID,
		DocTitle:    payload.DocTitle,
		DocSummary:  payload.DocSummary,
		PageSlugs:   payload.PageSlugs,
		Language:    payload.Language,
	}

	if redisClient != nil {
		pendingKey := wikiPendingKeyPrefix + payload.KnowledgeBaseID
		opBytes, _ := json.Marshal(op)
		redisClient.RPush(ctx, pendingKey, string(opBytes))
		redisClient.Expire(ctx, pendingKey, wikiPendingTTL)
	} else {
		// Fallback for Lite mode (no Redis)
		ingestPayload.LiteOps = []WikiPendingOp{op}
	}

	payloadBytes, _ := json.Marshal(ingestPayload)
	t := asynq.NewTask(types.TypeWikiIngest, payloadBytes,
		asynq.Queue("low"),
		asynq.MaxRetry(10), // Increased from 3 to 10 to outlast the active lock TTL
		asynq.Timeout(60*time.Minute),
		asynq.ProcessIn(5*time.Second), // Retract can trigger the batch quickly
	)
	if _, err := task.Enqueue(t); err != nil {
		logger.Warnf(ctx, "wiki retract: failed to enqueue task: %v", err)
	}
}

// Handle implements interfaces.TaskHandler for asynq task processing.
// Wiki ingest tasks are debounced via asynq.Unique + ProcessIn, so at most
// one ingest task runs per KB at a time. No distributed lock needed.
func (s *wikiIngestService) Handle(ctx context.Context, t *asynq.Task) error {
	return s.ProcessWikiIngest(ctx, t)
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
	summaryContentByKnowledgeID := make(map[string]string)
	for _, p := range allPages {
		if p.PageType != types.WikiPageTypeIndex && p.PageType != types.WikiPageTypeLog && p.Status != types.WikiPageStatusArchived {
			slugTitleMap[p.Slug] = p.Title
		}
		if p.PageType == types.WikiPageTypeSummary && p.Content != "" {
			for _, ref := range p.SourceRefs {
				kid := ref
				if pipeIdx := strings.Index(ref, "|"); pipeIdx > 0 {
					kid = ref[:pipeIdx]
				}
				summaryContentByKnowledgeID[kid] = p.Content
			}
		}
	}

	for _, slug := range pageSlugs {
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, slug)
		if err != nil || page == nil {
			continue
		}

		var remainingSourcesContent strings.Builder
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
			if content, ok := summaryContentByKnowledgeID[refKnowledgeID]; ok {
				fmt.Fprintf(&remainingSourcesContent, "<source title=%q>\n%s\n</source>\n\n", refTitle, content)
			} else {
				fmt.Fprintf(&remainingSourcesContent, "<source title=%q>\n(summary not available)\n</source>\n\n", refTitle)
			}
		}
		if remainingSourcesContent.Len() == 0 {
			remainingSourcesContent.WriteString("(no remaining sources)")
		}

		var relatedSlugs strings.Builder
		for _, outSlug := range page.OutLinks {
			if title, ok := slugTitleMap[outSlug]; ok {
				fmt.Fprintf(&relatedSlugs, "- %s (%s)\n", outSlug, title)
			}
		}

		updatedContent, err := s.generateWithTemplate(ctx, chatModel, agent.WikiPageRetractPrompt, map[string]string{
			"ExistingContent":         page.Content,
			"DeletedDocTitle":         docTitle,
			"DeletedDocContent":       docContent,
			"RemainingSourcesContent": remainingSourcesContent.String(),
			"AvailableSlugs":          relatedSlugs.String(),
			"Language":                lang,
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
	ingestPagesCount := 0
	retractPagesCount := 0
	totalPagesAffected := 0
	docPreview := make([]string, 0, 6)
	defer func() {
		logger.Infof(
			ctx,
			"wiki ingest stats: kb=%s tenant=%d retry=%d/%d status=%s elapsed=%s mode=%s lock_acquired=%v pending_ops=%d ops(ingest=%d,retract=%d) ingest(success=%d,failed=%d) retract_handled=%d pages(ingest=%d,retract=%d,total=%d) index(rebuild_attempted=%v,rebuild_succeeded=%v) followup=%v preview=%s",
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
			ingestPagesCount,
			retractPagesCount,
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
		// Use a 5-minute initial TTL
		acquired, err := s.redisClient.SetNX(ctx, activeKey, "1", 5*time.Minute).Result()
		if err != nil {
			logger.Warnf(ctx, "wiki ingest: redis SetNX failed: %v", err)
			// Proceed anyway — better to risk brief overlap than drop documents
		} else if !acquired {
			exitStatus = "active_lock_conflict"
			// Another batch is actively processing this KB.
			// By returning an error, we leverage Asynq's built-in exponential backoff to retry this task later.
			// This ensures that if the active task crashes (leaking the lock until its TTL expires),
			// this task will eventually retry and process the remaining items in the queue, preventing a stalled queue.
			logger.Infof(ctx, "wiki ingest: another batch active for KB %s, deferring to asynq retry", payload.KnowledgeBaseID)
			return fmt.Errorf("concurrent wiki task active, please retry")
		}
		lockAcquired = acquired

		// Create a context to cancel the keep-alive goroutine when we're done
		lockCtx, cancelLock := context.WithCancel(context.Background())

		// We own the flag — make sure to release it when done
		defer func() {
			cancelLock()
			// Use context.Background() to ensure release even if ctx is cancelled
			s.redisClient.Del(context.Background(), activeKey)
		}()

		// Keep-alive goroutine to extend lock TTL while the task is running
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

	// Get KB and validate
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

	// Get synthesis model
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

	// Peek Redis pending list to get all operations queued for this KB without removing them
	pendingOps, peekedCount := s.peekPendingList(ctx, payload.KnowledgeBaseID)
	pendingOpsCount = len(pendingOps)
	if len(pendingOps) == 0 {
		if s.redisClient != nil {
			// Redis mode: list was already drained — nothing to do
			exitStatus = "no_pending_ops"
			logger.Infof(ctx, "wiki ingest: no pending operations for KB %s", payload.KnowledgeBaseID)
			return nil
		}
		// Lite mode (no Redis): use LiteOps from payload
		if len(payload.LiteOps) > 0 {
			pendingOps = payload.LiteOps
			peekedCount = len(pendingOps)
			pendingOpsCount = len(pendingOps)
		} else {
			exitStatus = "no_lite_ops"
			return nil
		}
	}

	logger.Infof(ctx, "wiki ingest: batch processing %d ops for KB %s",
		len(pendingOps), payload.KnowledgeBaseID)

	// Process each operation
	var ingestPagesAffected []string
	var retractPagesAffected []string
	var docResults []*docIngestResult
	var retractChangeDesc strings.Builder

	for _, op := range pendingOps {
		if op.Op == WikiOpRetract {
			retractOps++
			logger.Infof(ctx, "wiki ingest: retracting document '%s' (%s)", op.DocTitle, op.KnowledgeID)
			s.retractPagesContent(ctx, chatModel, payload.KnowledgeBaseID, op.DocTitle, op.DocSummary, op.PageSlugs, op.Language)
			retractPagesAffected = append(retractPagesAffected, op.PageSlugs...)
			retractPagesCount += len(op.PageSlugs)
			retractHandled++
			docPreview = append(docPreview,
				fmt.Sprintf("retract[%s]: %s", previewText(op.KnowledgeID, 24), previewText(op.DocTitle, 48)))
			fmt.Fprintf(&retractChangeDesc, "Removed document '%s': %s\n", op.DocTitle, op.DocSummary)
			s.appendLogEntry(ctx, WikiIngestPayload{
				TenantID:        payload.TenantID,
				KnowledgeBaseID: payload.KnowledgeBaseID,
			}, "retract", op.KnowledgeID, op.DocTitle, op.PageSlugs, "")
		} else {
			ingestOps++
			// Default to ingest
			logger.Infof(ctx, "wiki ingest: processing document '%s' (%s)", op.DocTitle, op.KnowledgeID)
			result, err := s.processOneDocument(ctx, chatModel, payload, op.KnowledgeID, op.Language)
			if err != nil {
				ingestFailed++
				logger.Warnf(ctx, "wiki ingest: failed to process knowledge %s: %v", op.KnowledgeID, err)
				continue
			}
			if result != nil {
				ingestSucceeded++
				ingestPagesAffected = append(ingestPagesAffected, result.Pages...)
				ingestPagesCount += len(result.Pages)
				docResults = append(docResults, result)
				docPreview = append(docPreview,
					fmt.Sprintf(
						"ingest[%s]: title=%s summary=%s pages=%s",
						previewText(result.KnowledgeID, 24),
						previewText(result.DocTitle, 40),
						previewText(result.Summary, 64),
						previewStringSlice(result.Pages, 4),
					))
			}
		}
	}

	allPagesAffected := append(ingestPagesAffected, retractPagesAffected...)
	totalPagesAffected = len(allPagesAffected)

	// Batch post-processing (once for the whole batch, not per-doc)

	// Build change description from processed documents
	var changeDesc strings.Builder
	if len(docResults) > 0 {
		fmt.Fprintf(&changeDesc, "Added %d documents:\n", len(docResults))
		for _, r := range docResults {
			fmt.Fprintf(&changeDesc, "- %s: %s\n", r.DocTitle, r.Summary)
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
			docPreview = append(docPreview,
				fmt.Sprintf("index_change=%s", previewText(changeDesc.String(), 160)))
		} else {
			indexRebuildSucceeded = true
			docPreview = append(docPreview,
				fmt.Sprintf("index_change=%s", previewText(changeDesc.String(), 160)))
		}
	}

	// Append log entry for ingests
	if len(docResults) > 0 {
		s.appendLogEntry(ctx, payload, "ingest", "",
			fmt.Sprintf("%d documents", len(docResults)),
			ingestPagesAffected, "")
	}

	// Clean dead links (needed after retracts)
	if len(retractPagesAffected) > 0 {
		logger.Infof(ctx, "wiki ingest: cleaning dead links")
		s.cleanDeadLinks(ctx, payload.KnowledgeBaseID)
	}

	if len(allPagesAffected) > 0 {
		// Cross-link injection
		logger.Infof(ctx, "wiki ingest: injecting cross links")
		s.injectCrossLinks(ctx, payload.KnowledgeBaseID, allPagesAffected)

		// Publish all draft pages
		logger.Infof(ctx, "wiki ingest: publishing draft pages")
		s.publishDraftPages(ctx, payload.KnowledgeBaseID, allPagesAffected)
	}

	// Trim the pending list now that processing is complete
	s.trimPendingList(ctx, payload.KnowledgeBaseID, peekedCount)

	logger.Infof(ctx, "wiki ingest: batch completed for KB %s, %d ops, %d pages affected",
		payload.KnowledgeBaseID, len(pendingOps), len(allPagesAffected))

	// After clearing active flag (via defer above), check for follow-up work.
	// Note: this runs BEFORE defer, but defer runs LIFO so active flag is still set here.
	// We need to clear it first, then check. Use a closure:
	followUpScheduled = s.scheduleFollowUp(ctx, payload)

	return nil
}

// scheduleFollowUp checks if documents arrived in the pending list during batch processing.
// Called right before the active flag is released (via defer). Enqueues a new task with
// minimal delay so the next batch picks up new docs promptly.
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

// peekPendingList gets up to wikiMaxDocsPerBatch entries from the Redis pending list
// WITHOUT removing them. It returns the unique ops and the actual number of items peeked.
func (s *wikiIngestService) peekPendingList(ctx context.Context, kbID string) ([]WikiPendingOp, int) {
	if s.redisClient == nil {
		return nil, 0
	}
	pendingKey := wikiPendingKeyPrefix + kbID

	result, err := s.redisClient.LRange(ctx, pendingKey, 0, wikiMaxDocsPerBatch-1).Result()
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to peek pending list: %v", err)
		return nil, 0
	}

	var ops []WikiPendingOp
	for _, item := range result {
		if !strings.HasPrefix(item, "{") {
			// Backward compatibility: raw knowledgeID string
			ops = append(ops, WikiPendingOp{
				Op:          WikiOpIngest,
				KnowledgeID: item,
			})
			continue
		}
		var op WikiPendingOp
		if err := json.Unmarshal([]byte(item), &op); err == nil {
			ops = append(ops, op)
		}
	}

	// Deduplicate by KnowledgeID, keeping only the *last* operation for each document.
	// This optimizes out redundant sequences (e.g., upload then immediate delete: [ingest, retract] -> [retract]).
	seen := make(map[string]bool)
	var reversedUnique []WikiPendingOp
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		if !seen[op.KnowledgeID] {
			seen[op.KnowledgeID] = true
			reversedUnique = append(reversedUnique, op)
		}
	}

	// Reverse back to maintain chronological order
	var unique []WikiPendingOp
	for i := len(reversedUnique) - 1; i >= 0; i-- {
		unique = append(unique, reversedUnique[i])
	}

	return unique, len(result)
}

// trimPendingList removes the first `count` items from the Redis pending list.
func (s *wikiIngestService) trimPendingList(ctx context.Context, kbID string, count int) {
	if s.redisClient == nil || count <= 0 {
		return
	}
	pendingKey := wikiPendingKeyPrefix + kbID
	if err := s.redisClient.LTrim(ctx, pendingKey, int64(count), -1).Err(); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to trim pending list: %v", err)
	}
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
	docStartedAt := time.Now()
	// Get document chunks and reconstruct content
	chunks, err := s.chunkRepo.ListChunksByKnowledgeID(ctx, payload.TenantID, knowledgeID)
	if err != nil {
		return nil, fmt.Errorf("get chunks: %w", err)
	}
	if len(chunks) == 0 {
		logger.Infof(ctx, "wiki ingest: document %s has no chunks, skip", knowledgeID)
		return nil, nil
	}

	content := reconstructContent(chunks)
	rawRuneCount := len([]rune(content))
	if len([]rune(content)) > maxContentForWiki {
		content = string([]rune(content)[:maxContentForWiki])
	}
	logger.Infof(ctx,
		"wiki ingest: doc %s chunks=%d content_len(raw=%d,truncated=%d) content_preview=%q",
		knowledgeID, len(chunks), rawRuneCount, len([]rune(content)), previewText(content, 120))

	// Get document title
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

	var pagesAffected []string
	var docSummaryLine string
	sourceRef := fmt.Sprintf("%s|%s", knowledgeID, docTitle)

	// Snapshot existing page slugs for stale detection
	oldPageSlugs := s.getExistingPageSlugsForKnowledge(ctx, payload.KnowledgeBaseID, knowledgeID)

	// Build a per-doc payload for functions that still need KnowledgeID
	// (docPayload removed as WikiIngestPayload no longer holds KnowledgeID)

	// Step 1: Extract entities and concepts
	logger.Infof(ctx, "wiki ingest: extracting entities and concepts for %s", knowledgeID)
	extractedPages, slugItems, err := s.extractEntitiesAndConcepts(ctx, chatModel, content, docTitle, lang, payload, sourceRef, oldPageSlugs)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: knowledge extraction failed for %s: %v", knowledgeID, err)
	} else {
		pagesAffected = append(pagesAffected, extractedPages...)
	}

	// Step 2: Generate summary page
	logger.Infof(ctx, "wiki ingest: generating summary page for %s", knowledgeID)
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
		logger.Infof(ctx, "wiki ingest: summary preview for %s => line=%q body=%q",
			knowledgeID, previewText(sumLine, 100), previewText(sumBody, 140))

		existingSummary, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, summarySlug)
		if err == nil && existingSummary != nil {
			// Update existing summary page (idempotent for retries)
			existingSummary.Title = docTitle + " - Summary"
			existingSummary.Content = sumBody
			existingSummary.Summary = sumLine
			existingSummary.Status = types.WikiPageStatusDraft
			existingSummary.SourceRefs = appendUnique(existingSummary.SourceRefs, sourceRef)

			if _, err := s.wikiService.UpdatePage(ctx, existingSummary); err != nil {
				logger.Warnf(ctx, "wiki ingest: update summary page failed for %s: %v", knowledgeID, err)
			} else {
				pagesAffected = append(pagesAffected, summarySlug)
			}
		} else {
			// Create new summary page
			_, err = s.wikiService.CreatePage(ctx, &types.WikiPage{
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
	}

	// Retract stale pages (pages this doc previously contributed to but no longer does)
	logger.Infof(ctx, "wiki ingest: retracting stale pages for %s", knowledgeID)
	s.retractStalePages(ctx, chatModel, payload, knowledgeID, oldPageSlugs, pagesAffected, docTitle, content, lang)

	logger.Infof(ctx,
		"wiki ingest: processed knowledge %s title=%q affected_pages=%d page_preview=%s extracted_pages=%s elapsed=%s",
		knowledgeID,
		previewText(docTitle, 80),
		len(pagesAffected),
		previewStringSlice(pagesAffected, 6),
		previewStringSlice(extractedPages, 6),
		time.Since(docStartedAt).Round(time.Millisecond),
	)
	return &docIngestResult{
		KnowledgeID: knowledgeID,
		DocTitle:    docTitle,
		Summary:     docSummaryLine,
		Pages:       pagesAffected,
	}, nil
}

func previewText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	r := []rune(s)
	if maxRunes <= 0 || len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "...(truncated)"
}

func previewStringSlice(items []string, limit int) string {
	if len(items) == 0 {
		return "[]"
	}
	if limit <= 0 {
		limit = 1
	}
	n := len(items)
	if n > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, previewText(it, 48))
	}
	if n > limit {
		return fmt.Sprintf("[%s ...(+%d)]", strings.Join(out, ", "), n-limit)
	}
	return fmt.Sprintf("[%s]", strings.Join(out, ", "))
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
// Processes entity/concept/synthesis/comparison and summary pages (not index/log).
func (s *wikiIngestService) injectCrossLinks(ctx context.Context, kbID string, affectedSlugs []string) {
	// Build a title→slug lookup from ALL pages in this KB
	allPages, err := s.wikiService.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) < 2 {
		return
	}

	type pageRef struct {
		slug      string
		matchText string
	}
	var allRefs []pageRef
	for _, p := range allPages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		if p.Title != "" {
			allRefs = append(allRefs, pageRef{slug: p.Slug, matchText: p.Title})
		}
		for _, alias := range p.Aliases {
			if alias != "" {
				allRefs = append(allRefs, pageRef{slug: p.Slug, matchText: alias})
			}
		}
	}
	if len(allRefs) == 0 {
		return
	}

	// Sort by title length descending — match longer names first to avoid
	// partial matches (e.g. "北京邮电大学" before "北京")
	for i := 0; i < len(allRefs); i++ {
		for j := i + 1; j < len(allRefs); j++ {
			if len([]rune(allRefs[j].matchText)) > len([]rune(allRefs[i].matchText)) {
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
		// Skip system pages (index, log). We allow summary to be processed so it gets links to other KB entities.
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
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
			if strings.Contains(content, ref.matchText) {
				content = replaceFirstOutsideLinks(content, ref.matchText, "[["+ref.slug+"|"+ref.matchText+"]]")
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
	knowledgeID string,
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
	sourceRef := fmt.Sprintf("%s|%s", knowledgeID, docTitle)
	prefix := knowledgeID + "|"

	for _, slug := range staleSlugs {
		page, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
		if err != nil || page == nil {
			continue
		}

		// Remove this doc's source ref
		var remaining types.StringArray
		for _, ref := range page.SourceRefs {
			if ref == knowledgeID || ref == sourceRef || strings.HasPrefix(ref, prefix) {
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
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description"`
	Details     string   `json:"details"`
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
) ([]string, map[string]extractedItem, error) {
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

	extractionJSON = sanitizeJSONString(extractionJSON)

	var result combinedExtraction
	if err := json.Unmarshal([]byte(extractionJSON), &result); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to parse combined extraction JSON: %v\nRaw: %s", err, extractionJSON)
		return nil, nil, fmt.Errorf("parse combined extraction JSON: %w", err)
	}

	var affected []string

	// Deduplicate entities against existing wiki pages (LLM-based)
	logger.Infof(ctx, "wiki ingest: deduplicating %d entities", len(result.Entities))
	result.Entities = s.deduplicateItems(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, payload.KnowledgeBaseID)

	// Deduplicate concepts against existing wiki pages (LLM-based)
	logger.Infof(ctx, "wiki ingest: deduplicating %d concepts", len(result.Concepts))
	result.Concepts = s.deduplicateItems(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, payload.KnowledgeBaseID)

	// Build slug→item map for wiki-link generation in summary pages
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

	// Upsert entity pages
	logger.Infof(ctx, "wiki ingest: upserting %d entity pages", len(result.Entities))
	entitySlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Entities, types.WikiPageTypeEntity, docTitle, lang, payload, sourceRef)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: entity upsert failed: %v", err)
	} else {
		affected = append(affected, entitySlugs...)
	}

	// Upsert concept pages
	logger.Infof(ctx, "wiki ingest: upserting %d concept pages", len(result.Concepts))
	conceptSlugs, err := s.upsertExtractedPages(ctx, chatModel, result.Concepts, types.WikiPageTypeConcept, docTitle, lang, payload, sourceRef)
	if err != nil {
		logger.Warnf(ctx, "wiki ingest: concept upsert failed: %v", err)
	} else {
		affected = append(affected, conceptSlugs...)
	}

	return affected, slugItems, nil
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
			if len(item.Aliases) > 0 {
				// Merge new aliases with existing ones, deduplicating
				aliasMap := make(map[string]bool)
				for _, alias := range existing.Aliases {
					aliasMap[alias] = true
				}
				for _, newAlias := range item.Aliases {
					if !aliasMap[newAlias] {
						existing.Aliases = append(existing.Aliases, newAlias)
						aliasMap[newAlias] = true
					}
				}
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
				Aliases:         item.Aliases,
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
func (s *wikiIngestService) appendLogEntry(ctx context.Context, payload WikiIngestPayload, action, knowledgeID, docTitle string, pagesAffected []string, extra string) {
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
	if knowledgeID != "" {
		fmt.Fprintf(&sb, "- **Source**: knowledge/%s\n", knowledgeID)
	}
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
		aliases := ""
		if len(p.Aliases) > 0 {
			aliases = fmt.Sprintf(" | aliases: %s", strings.Join(p.Aliases, ", "))
		}
		fmt.Fprintf(&existingBuf, "- slug: %s | title: %s%s\n", p.Slug, p.Title, aliases)
	}

	// Build new items listing
	var newBuf strings.Builder
	for _, item := range items {
		aliases := ""
		if len(item.Aliases) > 0 {
			aliases = fmt.Sprintf(" | aliases: %s", strings.Join(item.Aliases, ", "))
		}
		fmt.Fprintf(&newBuf, "- slug: %s | name: %s%s\n", item.Slug, item.Name, aliases)
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

	dedupeJSON = sanitizeJSONString(dedupeJSON)

	var dedupeResult struct {
		Merges map[string]string `json:"merges"`
	}
	if err := json.Unmarshal([]byte(dedupeJSON), &dedupeResult); err != nil {
		logger.Warnf(ctx, "wiki ingest: failed to parse dedup JSON: %v\nRaw: %s", err, dedupeJSON)
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

	// Sort by StartAt, then ChunkIndex
	sort.Slice(textChunks, func(i, j int) bool {
		if textChunks[i].StartAt == textChunks[j].StartAt {
			return textChunks[i].ChunkIndex < textChunks[j].ChunkIndex
		}
		return textChunks[i].StartAt < textChunks[j].StartAt
	})

	var sb strings.Builder
	lastEndAt := -1
	for _, c := range textChunks {
		toAppend := c.Content

		if c.StartAt > lastEndAt || c.EndAt == 0 {
			// Non-overlapping or missing position info
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(toAppend)
			if c.EndAt > 0 {
				lastEndAt = c.EndAt
			}
		} else if c.EndAt > lastEndAt {
			// Partial overlap
			contentRunes := []rune(toAppend)
			offset := len(contentRunes) - (c.EndAt - lastEndAt)
			if offset >= 0 && offset < len(contentRunes) {
				sb.WriteString(string(contentRunes[offset:]))
			} else {
				// Fallback if offset calculation is invalid
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(toAppend)
			}
			lastEndAt = c.EndAt
		}
		// If c.EndAt <= lastEndAt, it's fully contained, so skip appending text
	}

	// Append image information at the end to avoid interrupting text flow
	var hasImages bool
	seenURLs := make(map[string]bool)
	for _, c := range textChunks {
		if c.ImageInfo != "" {
			var imageInfos []types.ImageInfo
			if err := json.Unmarshal([]byte(c.ImageInfo), &imageInfos); err == nil && len(imageInfos) > 0 {
				for _, img := range imageInfos {
					// Deduplicate images by URL to avoid printing the same image multiple times from overlapping chunks
					if img.URL != "" {
						if seenURLs[img.URL] {
							continue
						}
						seenURLs[img.URL] = true
					}

					if !hasImages {
						sb.WriteString("\n\n<images>\n")
						hasImages = true
					} else {
						sb.WriteString("\n")
					}

					sb.WriteString("<image>\n")
					if img.URL != "" {
						sb.WriteString(fmt.Sprintf("  <url>%s</url>\n", img.URL))
					}
					if img.Caption != "" {
						sb.WriteString(fmt.Sprintf("  <caption>%s</caption>\n", img.Caption))
					}
					if img.OCRText != "" {
						sb.WriteString(fmt.Sprintf("  <ocr_text>%s</ocr_text>\n", img.OCRText))
					}
					sb.WriteString("</image>\n")
				}
			}
		}
	}

	if hasImages {
		sb.WriteString("</images>\n")
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

// sanitizeJSONString sanitizes a string that is intended to be parsed as JSON,
// by properly escaping unescaped control characters (like newlines) inside string literals.
func sanitizeJSONString(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	inString := false
	escape := false
	for _, r := range s {
		if escape {
			if r == '\n' {
				buf.WriteString(`n`)
			} else if r == '\r' {
				buf.WriteString(`r`)
			} else if r == '\t' {
				buf.WriteString(`t`)
			} else {
				buf.WriteRune(r)
			}
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			buf.WriteRune(r)
			continue
		}
		if r == '"' {
			inString = !inString
			buf.WriteRune(r)
			continue
		}
		if inString {
			if r == '\n' {
				buf.WriteString(`\n`)
				continue
			}
			if r == '\r' {
				buf.WriteString(`\r`)
				continue
			}
			if r == '\t' {
				buf.WriteString(`\t`)
				continue
			}
		}
		buf.WriteRune(r)
	}
	return buf.String()
}
