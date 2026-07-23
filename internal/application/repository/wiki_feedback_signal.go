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
	return strings.TrimSpace(page.Content) + "\n\n## 反馈修正候选\n\n" + correction
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

// feedbackAffectedPages expands the explicitly attributed nodes to every
// directly related page that can repeat or depend on the disputed claim. The
// expansion uses the wiki graph plus titles/aliases mentioned in the feedback;
// this avoids silently fixing only the page returned by the original answer.
func feedbackAffectedPages(allPages []types.WikiPage, seedIDs types.StringArray, in *types.WikiFeedbackSignalInput) ([]types.WikiPage, map[string][]string) {
	selected := make(map[string]bool, len(seedIDs))
	reasons := make(map[string][]string)
	seedSlugs := make(map[string]bool)
	pageBySlug := make(map[string]*types.WikiPage, len(allPages))
	for i := range allPages {
		pageBySlug[allPages[i].Slug] = &allPages[i]
	}
	for _, id := range seedIDs {
		selected[id] = true
		appendFeedbackReason(reasons, id, "explicit_attribution")
	}
	for i := range allPages {
		if selected[allPages[i].ID] {
			seedSlugs[allPages[i].Slug] = true
		}
	}

	// Add both directions of every direct wiki relation around a seed.
	neighborSlugs := make(map[string]bool)
	for i := range allPages {
		page := &allPages[i]
		if selected[page.ID] {
			for _, slug := range append(append(types.StringArray(nil), page.OutLinks...), page.InLinks...) {
				neighborSlugs[slug] = true
			}
		}
		for _, slug := range append(append(types.StringArray(nil), page.OutLinks...), page.InLinks...) {
			if seedSlugs[slug] {
				selected[page.ID] = true
				appendFeedbackReason(reasons, page.ID, "direct_wiki_relation")
			}
		}
	}
	for slug := range neighborSlugs {
		if page := pageBySlug[slug]; page != nil {
			selected[page.ID] = true
			appendFeedbackReason(reasons, page.ID, "direct_wiki_relation")
		}
	}

	contextText := strings.Join([]string{in.FeedbackText, in.OriginalQuestion, in.AnswerExcerpt, in.SuggestedCorrection}, "\n")
	terms := make(map[string]bool)
	// Titles and aliases are the wiki's own vocabulary. If one is explicitly
	// mentioned by the feedback, every page repeating it is affected.
	for i := range allPages {
		page := &allPages[i]
		for _, term := range append(types.StringArray{page.Title}, page.Aliases...) {
			term = strings.TrimSpace(term)
			if len([]rune(term)) >= 2 && feedbackTextContains(contextText, term) {
				terms[term] = true
			}
		}
	}
	for i := range allPages {
		page := &allPages[i]
		haystack := strings.Join([]string{page.Title, page.Summary, page.Content}, "\n")
		if strings.TrimSpace(in.AnswerExcerpt) != "" && feedbackTextContains(haystack, in.AnswerExcerpt) {
			selected[page.ID] = true
			appendFeedbackReason(reasons, page.ID, "answer_excerpt_match")
		}
		for term := range terms {
			if feedbackTextContains(haystack, term) {
				selected[page.ID] = true
				appendFeedbackReason(reasons, page.ID, "mentioned_wiki_term:"+term)
			}
		}
	}

	affected := make([]types.WikiPage, 0, len(selected))
	for i := range allPages {
		if selected[allPages[i].ID] {
			affected = append(affected, allPages[i])
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
	nodeIDs := append(types.StringArray(nil), in.TargetNodeIDs...)
	confidence := 1.0
	if len(nodeIDs) == 0 {
		nodeIDs = append(nodeIDs, in.RecalledNodeIDs...)
		confidence = 0.7
	}
	if len(nodeIDs) == 0 {
		return nil, errors.New("feedback cannot be attributed: target_node_ids or recalled_node_ids is required")
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
		var allPages []types.WikiPage
		if err := tx.Where("knowledge_base_id = ? AND status <> ?", kbID, types.WikiPageStatusArchived).Find(&allPages).Error; err != nil {
			return err
		}
		pages, attributionReasons := feedbackAffectedPages(allPages, nodeIDs, in)
		if len(pages) == 0 {
			return errors.New("no matching wiki nodes were found")
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
		set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryCorrection, Reasons: types.StringArray{"feedback_signal", in.SignalType}, ModelID: "feedback-loop", PromptVersion: "feedback-candidate-v1", CandidateFingerprint: key, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if pendingCount > 0 {
			signal.ConflictSummary = "目标节点已有待审核变更，请审核人对照现有候选后再发布"
			set.Reasons = append(set.Reasons, "pending_feedback_conflict")
		}
		for i := range pages {
			page := pages[i]
			after := page
			after.Content = feedbackCandidateContent(&page, in)
			beforeJSON, _ := json.Marshal(&page)
			afterJSON, _ := json.Marshal(&after)
			evidence, _ := json.Marshal(map[string]any{"feedback_signal_id": signal.ID, "feedback_text": in.FeedbackText, "suggested_correction": in.SuggestedCorrection, "related_request_id": in.RelatedRequestID, "attribution_reasons": attributionReasons[page.ID]})
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
