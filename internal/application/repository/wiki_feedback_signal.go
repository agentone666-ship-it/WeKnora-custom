package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func feedbackCandidateContent(page *types.WikiPage, in *types.WikiFeedbackSignalInput) string {
	correction := strings.TrimSpace(in.SuggestedCorrection)
	if correction == "" {
		return page.Content
	}
	excerpt := strings.TrimSpace(in.AnswerExcerpt)
	if excerpt != "" && strings.Contains(page.Content, excerpt) {
		return strings.Replace(page.Content, excerpt, correction, 1)
	}
	// Never append feedback to the page as if it were knowledge content. If
	// the original answer excerpt cannot be located in this recalled page, keep
	// the page unchanged and leave the item for manual attribution/editing.
	// This keeps feedback as review context instead of polluting the Wiki page.
	return page.Content
}

// generateFeedbackCandidateContent produces a complete replacement page from
// the page and the context of the original answer. Feedback is treated as an
// instruction to revise the existing article, never as text to append to it.
// The deterministic fallback keeps the review flow usable when a model is not
// configured or temporarily unavailable.
func (r *wikiGovernanceRepository) generateFeedbackCandidateContent(ctx context.Context, kbID string, page *types.WikiPage, in *types.WikiFeedbackSignalInput) string {
	fallback := feedbackCandidateContent(page, in)
	if r.modelService == nil || r.kbService == nil {
		return fallback
	}
	kb, err := r.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil || kb == nil || strings.TrimSpace(kb.SummaryModelID) == "" {
		return fallback
	}
	model, err := r.modelService.GetChatModel(ctx, kb.SummaryModelID)
	if err != nil || model == nil {
		return fallback
	}
	prompt := fmt.Sprintf(`请修正一篇 Wiki 页面，并只输出修正后的完整 Markdown 正文。

要求：
1. 以原页面为基础，保留没有被反馈影响的标题、结构、事实和列表。
2. 结合原问题、系统原回答、用户反馈和建议修正，真正改写页面中受影响的内容。
3. 不要追加“用户反馈”“反馈修正候选”“修正说明”等元信息。
4. 不要编造上下文无法确认的事实；无法确认的部分保留原文。
5. 不要输出 Markdown 代码围栏或解释文字。

【原 Wiki 页面】
%s

【用户原问题】
%s

【系统原回答】
%s

【用户反馈】
%s

【建议修正】
%s`, page.Content, in.OriginalQuestion, in.AnswerExcerpt, in.FeedbackText, in.SuggestedCorrection)
	resp, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: "你是严谨的 Wiki 内容修正器。"}, {Role: "user", Content: prompt}}, &chat.ChatOptions{Temperature: 0.1, MaxTokens: 8192})
	if err != nil || resp == nil {
		return fallback
	}
	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```markdown")
	content = strings.TrimPrefix(content, "```md")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	if content == "" {
		return fallback
	}
	return content
}

func feedbackRisk(in *types.WikiFeedbackSignalInput) string {
	text := strings.ToLower(in.FeedbackText + " " + in.SuggestedCorrection)
	for _, word := range []string{"退款", "赔付", "合同", "合规", "隐私", "安全", "权限", "价格", "refund", "contract", "privacy", "security"} {
		if strings.Contains(text, word) {
			return "high"
		}
	}
	return "medium"
}

func feedbackKey(kbID string, in *types.WikiFeedbackSignalInput) string {
	if v := strings.TrimSpace(in.IdempotencyKey); v != "" {
		return v
	}
	s := sha256.Sum256([]byte(kbID + "\x00" + in.RelatedRequestID + "\x00" + in.FeedbackText + "\x00" + strings.Join(in.TargetNodeIDs, ",")))
	return hex.EncodeToString(s[:])
}

func feedbackCandidateNodeIDs(in *types.WikiFeedbackSignalInput) []string {
	if len(in.TargetNodeIDs) > 0 {
		return append([]string(nil), in.TargetNodeIDs...)
	}
	return append([]string(nil), in.RecalledNodeIDs...)
}

func feedbackTextContains(text, term string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	term = strings.ToLower(strings.TrimSpace(term))
	return term != "" && strings.Contains(text, term)
}

func appendFeedbackReason(reasons map[string][]string, pageID, reason string) {
	for _, existing := range reasons[pageID] {
		if existing == reason {
			return
		}
	}
	reasons[pageID] = append(reasons[pageID], reason)
}

// feedbackAffectedPages attributes feedback only within the pages recalled for
// the original question. Explicit targets are authoritative. Without explicit
// targets, the feedback/question context may narrow the recalled set, but it
// must never expand into unrelated pages elsewhere in the knowledge base.
func feedbackAffectedPages(recalledPages []types.WikiPage, targetIDs types.StringArray, in *types.WikiFeedbackSignalInput) ([]types.WikiPage, map[string][]string) {
	selected := make(map[string]bool, len(targetIDs))
	reasons := make(map[string][]string)
	for _, id := range targetIDs {
		selected[id] = true
		appendFeedbackReason(reasons, id, "explicit_attribution")
	}

	if len(targetIDs) == 0 {
		contextText := strings.Join([]string{in.FeedbackText, in.OriginalQuestion, in.AnswerExcerpt, in.SuggestedCorrection}, "\n")
		for i := range recalledPages {
			page := &recalledPages[i]
			haystack := strings.Join([]string{page.Title, page.Summary, page.Content}, "\n")
			if strings.TrimSpace(in.AnswerExcerpt) != "" && feedbackTextContains(haystack, in.AnswerExcerpt) {
				selected[page.ID] = true
				appendFeedbackReason(reasons, page.ID, "answer_excerpt_match")
			}
			for _, term := range append(types.StringArray{page.Title}, page.Aliases...) {
				term = strings.TrimSpace(term)
				if len([]rune(term)) >= 2 && feedbackTextContains(contextText, term) {
					selected[page.ID] = true
					appendFeedbackReason(reasons, page.ID, "recalled_page_mention:"+term)
				}
			}
		}
	}

	affected := make([]types.WikiPage, 0, len(selected))
	for i := range recalledPages {
		if selected[recalledPages[i].ID] {
			affected = append(affected, recalledPages[i])
		}
	}
	return affected, reasons
}

func (r *wikiGovernanceRepository) SubmitFeedbackSignal(ctx context.Context, tenantID uint64, kbID, actor string, in *types.WikiFeedbackSignalInput) (*types.WikiFeedbackSignal, error) {
	if in == nil || strings.TrimSpace(in.FeedbackText) == "" {
		return nil, errors.New("feedback_text is required")
	}
	key := feedbackKey(kbID, in)
	var existing types.WikiFeedbackSignal
	if err := r.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// Keep query IDs as a plain []string. types.StringArray implements
	// driver.Valuer for JSON columns; passing it to GORM's `IN ?` predicate
	// makes PostgreSQL bind the whole JSON value as one parameter and produces
	// invalid SQL such as `id IN $3` instead of expanding the IDs.
	candidateNodeIDs := feedbackCandidateNodeIDs(in)
	confidence := 1.0
	if len(in.TargetNodeIDs) == 0 && len(candidateNodeIDs) > 0 {
		confidence = 0.7
	}
	if len(candidateNodeIDs) == 0 {
		confidence = 0.3
	}
	now := time.Now()
	signal := &types.WikiFeedbackSignal{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, Source: strings.TrimSpace(in.Source), SignalType: strings.TrimSpace(in.SignalType), FeedbackText: strings.TrimSpace(in.FeedbackText), OriginalQuestion: in.OriginalQuestion, AnswerExcerpt: in.AnswerExcerpt, SuggestedCorrection: in.SuggestedCorrection, RelatedRequestID: in.RelatedRequestID, SessionID: in.SessionID, AgentID: in.AgentID, RecalledNodeIDs: in.RecalledNodeIDs, TargetNodeIDs: in.TargetNodeIDs, Status: types.WikiFeedbackReceived, Strategy: "manual_review", RiskLevel: feedbackRisk(in), Confidence: confidence, IdempotencyKey: key, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if signal.Source == "" {
		signal.Source = "manual_correction"
	}
	if signal.SignalType == "" {
		signal.SignalType = "other"
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var recalledPages []types.WikiPage
		if len(candidateNodeIDs) > 0 {
			if err := tx.Where("knowledge_base_id = ? AND status <> ? AND id IN ?", kbID, types.WikiPageStatusArchived, candidateNodeIDs).Find(&recalledPages).Error; err != nil {
				return err
			}
		}
		pages, attributionReasons := feedbackAffectedPages(recalledPages, in.TargetNodeIDs, in)
		if len(pages) == 0 {
			signal.Strategy = "manual_attribution"
			signal.ConflictSummary = "未能在原问题召回节点中确认具体修正目标，反馈已保留待人工归因"
			return tx.Create(signal).Error
		}
		pageIDs := make([]string, 0, len(pages))
		for i := range pages {
			pageIDs = append(pageIDs, pages[i].ID)
		}
		var pendingCount int64
		if err := tx.Model(&types.WikiChangeItem{}).
			Joins("JOIN wiki_change_sets ON wiki_change_sets.id = wiki_change_items.change_set_id").
			Where("wiki_change_sets.knowledge_base_id = ? AND wiki_change_sets.status = ? AND wiki_change_items.page_id IN ?", kbID, types.WikiChangeSetPending, pageIDs).
			Count(&pendingCount).Error; err != nil {
			return err
		}
		set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryCorrection, Reasons: types.StringArray{"feedback_signal", in.SignalType}, ModelID: "feedback-loop", PromptVersion: "feedback-candidate-v2-full-page", CandidateFingerprint: key, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if pendingCount > 0 {
			signal.ConflictSummary = "目标节点已有待审核变更，请审核人对照现有候选后再发布"
			set.Reasons = append(set.Reasons, "pending_feedback_conflict")
		}
		for i := range pages {
			page := pages[i]
			after := page
			after.Content = r.generateFeedbackCandidateContent(ctx, kbID, &page, in)
			beforeJSON, _ := json.Marshal(&page)
			afterJSON, _ := json.Marshal(&after)
			evidence, _ := json.Marshal(map[string]any{"feedback_signal_id": signal.ID, "feedback_text": in.FeedbackText, "suggested_correction": in.SuggestedCorrection, "related_request_id": in.RelatedRequestID, "attribution_reasons": attributionReasons[page.ID], "candidate_generation": "full_page_revision"})
			set.Items = append(set.Items, types.WikiChangeItem{ID: uuid.NewString(), Operation: "update", ChangeCategory: types.WikiChangeCategoryCorrection, PageID: page.ID, PageSlug: page.Slug, ExpectedVersion: page.Version, Before: types.JSON(beforeJSON), After: types.JSON(afterJSON), ChangedFields: types.StringArray{"content"}, EvidenceChunkIDs: page.ChunkRefs, EvidenceExcerpts: types.JSON(evidence), CreatedAt: now})
			signal.AttributedNodeIDs = append(signal.AttributedNodeIDs, page.ID)
		}
		items := set.Items
		set.Items = nil
		if err := tx.Create(set).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].ChangeSetID = set.ID
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		signal.ChangeSetID, signal.Status = set.ID, types.WikiFeedbackPendingReview
		return tx.Create(signal).Error
	})
	if err != nil {
		return nil, fmt.Errorf("create feedback candidate: %w", err)
	}
	return signal, nil
}

func (r *wikiGovernanceRepository) ListFeedbackSignals(ctx context.Context, req types.WikiFeedbackSignalListRequest) ([]*types.WikiFeedbackSignal, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.WikiFeedbackSignal{}).Where("knowledge_base_id = ?", req.KnowledgeBaseID)
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []*types.WikiFeedbackSignal
	err := q.Order("created_at DESC").Limit(limit).Offset(req.Offset).Find(&rows).Error
	return rows, total, err
}

func (r *wikiGovernanceRepository) GetFeedbackSignal(ctx context.Context, kbID, id string) (*types.WikiFeedbackSignal, error) {
	var row types.WikiFeedbackSignal
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ? AND id = ?", kbID, id).First(&row).Error
	return &row, err
}
