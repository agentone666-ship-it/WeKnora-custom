package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"gorm.io/gorm"
)

const (
	traceStatusVerified = "verified"
	traceStatusUntraced = "untraced"
	traceStatusMismatch = "trace_mismatch"
)

var supportedFeedbackTypes = map[string]bool{
	"incorrect": true, "outdated": true, "incomplete": true, "irrelevant": true,
	"wrong_reference": true, "missing_knowledge": true, "suggestion": true, "other": true,
}

type recalledNode struct {
	NodeID          string   `json:"node_id"`
	KnowledgeID     string   `json:"knowledge_id,omitempty"`
	KnowledgeBaseID string   `json:"knowledge_base_id,omitempty"`
	ParentNodeID    string   `json:"parent_node_id,omitempty"`
	SubNodeIDs      []string `json:"sub_node_ids,omitempty"`
	KnowledgeTitle  string   `json:"knowledge_title,omitempty"`
	Rank            int      `json:"rank"`
	Score           float64  `json:"score"`
	MatchType       string   `json:"match_type,omitempty"`
	ContentExcerpt  string   `json:"content_excerpt,omitempty"`
	ContentHash     string   `json:"content_hash,omitempty"`
}

type knowledgeAnswer struct {
	Answer          string         `json:"answer"`
	RequestID       string         `json:"request_id"`
	SessionID       string         `json:"session_id"`
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	RecalledNodes   []recalledNode `json:"recalled_nodes"`
}

type feedbackInput struct {
	FeedbackText        string   `json:"feedback_text"`
	OriginalQuestion    string   `json:"original_question,omitempty"`
	AnswerExcerpt       string   `json:"answer_excerpt,omitempty"`
	SuggestedCorrection string   `json:"suggested_correction,omitempty"`
	FeedbackType        string   `json:"feedback_type,omitempty"`
	RelatedRequestID    string   `json:"related_request_id,omitempty"`
	KnowledgeBaseID     string   `json:"knowledge_base_id,omitempty"`
	RecalledNodeIDs     []string `json:"recalled_node_ids,omitempty"`
	TargetNodeIDs       []string `json:"target_node_ids,omitempty"`
}

type feedbackReceipt struct {
	Accepted         bool     `json:"accepted"`
	FeedbackID       string   `json:"feedback_id"`
	TraceStatus      string   `json:"trace_status"`
	TraceMessage     string   `json:"trace_message,omitempty"`
	RelatedRequestID string   `json:"related_request_id,omitempty"`
	KnowledgeBaseID  string   `json:"knowledge_base_id,omitempty"`
	RecalledNodeIDs  []string `json:"recalled_node_ids"`
	TargetNodeIDs    []string `json:"target_node_ids"`
}

type mcpCallReference struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	RequestID       string    `json:"request_id" gorm:"size:64;not null;uniqueIndex:idx_mcp_call_reference"`
	SessionID       string    `json:"session_id" gorm:"size:64;index"`
	NodeID          string    `json:"node_id" gorm:"size:128;not null;uniqueIndex:idx_mcp_call_reference"`
	KnowledgeID     string    `json:"knowledge_id" gorm:"size:128;index"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"size:128;index"`
	ParentNodeID    string    `json:"parent_node_id" gorm:"size:128"`
	SubNodeIDs      []string  `json:"sub_node_ids" gorm:"serializer:json;type:text"`
	KnowledgeTitle  string    `json:"knowledge_title" gorm:"type:text"`
	Rank            int       `json:"rank"`
	Score           float64   `json:"score"`
	MatchType       string    `json:"match_type" gorm:"size:64"`
	ContentExcerpt  string    `json:"content_excerpt" gorm:"type:text"`
	ContentHash     string    `json:"content_hash" gorm:"size:80"`
	CreatedAt       time.Time `json:"created_at" gorm:"index"`
}

type mcpFeedback struct {
	ID                  string    `json:"id" gorm:"primaryKey;size:64"`
	MemberID            *uint     `json:"member_id" gorm:"index"`
	MemberName          string    `json:"member_name" gorm:"size:120;index"`
	FeedbackType        string    `json:"feedback_type" gorm:"size:32;index"`
	FeedbackText        string    `json:"feedback_text" gorm:"type:text;not null"`
	OriginalQuestion    string    `json:"original_question" gorm:"type:text"`
	AnswerExcerpt       string    `json:"answer_excerpt" gorm:"type:text"`
	SuggestedCorrection string    `json:"suggested_correction" gorm:"type:text"`
	RelatedRequestID    string    `json:"related_request_id" gorm:"size:64;index"`
	KnowledgeBaseID     string    `json:"knowledge_base_id" gorm:"size:128;index"`
	TraceStatus         string    `json:"trace_status" gorm:"size:32;index"`
	TraceMessage        string    `json:"trace_message" gorm:"type:text"`
	CreatedAt           time.Time `json:"created_at" gorm:"index"`
}

type mcpFeedbackNode struct {
	ID          uint   `json:"id" gorm:"primaryKey"`
	FeedbackID  string `json:"feedback_id" gorm:"size:64;not null;uniqueIndex:idx_feedback_node_role"`
	NodeID      string `json:"node_id" gorm:"size:128;not null;uniqueIndex:idx_feedback_node_role"`
	Role        string `json:"role" gorm:"size:20;not null;uniqueIndex:idx_feedback_node_role"`
	ReferenceID *uint  `json:"reference_id" gorm:"index"`
	Verified    bool   `json:"verified"`
}

type feedbackView struct {
	mcpFeedback
	RecalledNodeIDs []string           `json:"recalled_node_ids"`
	TargetNodeIDs   []string           `json:"target_node_ids"`
	References      []mcpCallReference `json:"references,omitempty"`
}

type feedbackQuery struct {
	Page, PageSize int
	FeedbackType   string
	TraceStatus    string
}

func contentExcerpt(content string, maxRunes int) string {
	content = strings.TrimSpace(content)
	if maxRunes <= 0 || utf8.RuneCountInString(content) <= maxRunes {
		return content
	}
	runes := []rune(content)
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeIDs(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (s *adminStore) saveRecallSnapshot(answer knowledgeAnswer) error {
	if strings.TrimSpace(answer.RequestID) == "" {
		return errors.New("request_id is required to save recall snapshot")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("request_id = ?", answer.RequestID).Delete(&mcpCallReference{}).Error; err != nil {
			return err
		}
		for _, node := range answer.RecalledNodes {
			if strings.TrimSpace(node.NodeID) == "" {
				continue
			}
			row := mcpCallReference{
				RequestID: answer.RequestID, SessionID: answer.SessionID, NodeID: node.NodeID,
				KnowledgeID: node.KnowledgeID, KnowledgeBaseID: node.KnowledgeBaseID,
				ParentNodeID: node.ParentNodeID, SubNodeIDs: node.SubNodeIDs,
				KnowledgeTitle: node.KnowledgeTitle, Rank: node.Rank, Score: node.Score,
				MatchType: node.MatchType, ContentExcerpt: node.ContentExcerpt, ContentHash: node.ContentHash,
				CreatedAt: time.Now(),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *adminStore) referencesByRequestID(requestID string) ([]mcpCallReference, error) {
	var rows []mcpCallReference
	err := s.db.Where("request_id = ?", strings.TrimSpace(requestID)).Order("rank ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (s *adminStore) submitFeedback(ctx context.Context, input feedbackInput) (*feedbackReceipt, error) {
	input.FeedbackText = strings.TrimSpace(input.FeedbackText)
	if input.FeedbackText == "" {
		return nil, errors.New("feedback_text is required")
	}
	input.FeedbackType = strings.TrimSpace(input.FeedbackType)
	if input.FeedbackType == "" {
		input.FeedbackType = "other"
	}
	if !supportedFeedbackTypes[input.FeedbackType] {
		return nil, fmt.Errorf("unsupported feedback_type %q", input.FeedbackType)
	}
	input.RelatedRequestID = strings.TrimSpace(input.RelatedRequestID)
	input.KnowledgeBaseID = strings.TrimSpace(input.KnowledgeBaseID)
	input.RecalledNodeIDs = normalizeIDs(input.RecalledNodeIDs)
	input.TargetNodeIDs = normalizeIDs(input.TargetNodeIDs)

	traceStatus := traceStatusUntraced
	traceMessage := "No related_request_id was provided; feedback was stored without a verified recall snapshot."
	var refs []mcpCallReference
	refByNode := map[string]mcpCallReference{}
	if input.RelatedRequestID != "" {
		var err error
		refs, err = s.referencesByRequestID(input.RelatedRequestID)
		if err != nil {
			return nil, err
		}
		if len(refs) == 0 {
			traceStatus = traceStatusMismatch
			traceMessage = "No saved recall snapshot matches related_request_id; node bindings were not silently accepted."
		} else {
			for _, ref := range refs {
				refByNode[ref.NodeID] = ref
			}
			if len(input.RecalledNodeIDs) == 0 {
				for _, ref := range refs {
					input.RecalledNodeIDs = append(input.RecalledNodeIDs, ref.NodeID)
				}
			}
			mismatches := make([]string, 0)
			for _, nodeID := range input.RecalledNodeIDs {
				if _, ok := refByNode[nodeID]; !ok {
					mismatches = append(mismatches, "recalled node "+nodeID+" was not in the saved call")
				}
			}
			recalledSet := make(map[string]bool, len(input.RecalledNodeIDs))
			for _, nodeID := range input.RecalledNodeIDs {
				recalledSet[nodeID] = true
			}
			for _, nodeID := range input.TargetNodeIDs {
				if !recalledSet[nodeID] {
					mismatches = append(mismatches, "target node "+nodeID+" is not a subset of recalled_node_ids")
				} else if _, ok := refByNode[nodeID]; !ok {
					mismatches = append(mismatches, "target node "+nodeID+" was not in the saved call")
				}
			}
			if input.KnowledgeBaseID == "" {
				for _, ref := range refs {
					if ref.KnowledgeBaseID != "" {
						input.KnowledgeBaseID = ref.KnowledgeBaseID
						break
					}
				}
			} else {
				for _, ref := range refs {
					if ref.KnowledgeBaseID != "" && ref.KnowledgeBaseID != input.KnowledgeBaseID {
						mismatches = append(mismatches, "knowledge_base_id does not match the saved recall snapshot")
						break
					}
				}
			}
			if len(mismatches) == 0 {
				traceStatus = traceStatusVerified
				traceMessage = "Recall nodes were verified against the saved call snapshot."
			} else {
				traceStatus = traceStatusMismatch
				traceMessage = strings.Join(mismatches, "; ")
			}
		}
	}

	feedback := &mcpFeedback{
		ID: uuid.NewString(), FeedbackType: input.FeedbackType, FeedbackText: input.FeedbackText,
		OriginalQuestion: strings.TrimSpace(input.OriginalQuestion), AnswerExcerpt: strings.TrimSpace(input.AnswerExcerpt),
		SuggestedCorrection: strings.TrimSpace(input.SuggestedCorrection), RelatedRequestID: input.RelatedRequestID,
		KnowledgeBaseID: input.KnowledgeBaseID, TraceStatus: traceStatus, TraceMessage: traceMessage, CreatedAt: time.Now(),
	}
	if member, ok := ctx.Value(authMemberKey{}).(*mcpMember); ok {
		feedback.MemberID, feedback.MemberName = &member.ID, member.Name
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(feedback).Error; err != nil {
			return err
		}
		for _, roleAndIDs := range []struct {
			role string
			ids  []string
		}{{"recalled", input.RecalledNodeIDs}, {"target", input.TargetNodeIDs}} {
			for _, nodeID := range roleAndIDs.ids {
				row := mcpFeedbackNode{FeedbackID: feedback.ID, NodeID: nodeID, Role: roleAndIDs.role}
				if ref, ok := refByNode[nodeID]; ok {
					row.ReferenceID, row.Verified = &ref.ID, true
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &feedbackReceipt{Accepted: true, FeedbackID: feedback.ID, TraceStatus: traceStatus, TraceMessage: traceMessage, RelatedRequestID: input.RelatedRequestID, KnowledgeBaseID: input.KnowledgeBaseID, RecalledNodeIDs: input.RecalledNodeIDs, TargetNodeIDs: input.TargetNodeIDs}, nil
}

func (s *adminStore) listFeedback(q feedbackQuery) ([]feedbackView, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 50
	}
	db := s.db.Model(&mcpFeedback{})
	if q.FeedbackType != "" {
		db = db.Where("feedback_type = ?", q.FeedbackType)
	}
	if q.TraceStatus != "" {
		db = db.Where("trace_status = ?", q.TraceStatus)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []mcpFeedback
	if err := db.Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	views := make([]feedbackView, 0, len(rows))
	for _, row := range rows {
		view, err := s.feedbackView(row, false)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *view)
	}
	return views, total, nil
}

func (s *adminStore) getFeedback(id string) (*feedbackView, error) {
	var row mcpFeedback
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	return s.feedbackView(row, true)
}

func (s *adminStore) feedbackView(row mcpFeedback, includeReferences bool) (*feedbackView, error) {
	var nodes []mcpFeedbackNode
	if err := s.db.Where("feedback_id = ?", row.ID).Order("id ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	view := &feedbackView{mcpFeedback: row, RecalledNodeIDs: []string{}, TargetNodeIDs: []string{}}
	for _, node := range nodes {
		if node.Role == "target" {
			view.TargetNodeIDs = append(view.TargetNodeIDs, node.NodeID)
		} else {
			view.RecalledNodeIDs = append(view.RecalledNodeIDs, node.NodeID)
		}
	}
	if includeReferences && row.RelatedRequestID != "" {
		refs, err := s.referencesByRequestID(row.RelatedRequestID)
		if err != nil {
			return nil, err
		}
		view.References = refs
	}
	sort.Strings(view.TargetNodeIDs)
	return view, nil
}

func (s *adminStore) feedbackCounts() (total, mismatches int64, err error) {
	if err = s.db.Model(&mcpFeedback{}).Count(&total).Error; err != nil {
		return
	}
	err = s.db.Model(&mcpFeedback{}).Where("trace_status = ?", traceStatusMismatch).Count(&mismatches).Error
	return
}

func (s *adminStore) feedbackTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input feedbackInput
	if err := request.BindArguments(&input); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	receipt, err := s.submitFeedback(ctx, input)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fallback := fmt.Sprintf("Feedback accepted (feedback_id=%s, trace_status=%s).", receipt.FeedbackID, receipt.TraceStatus)
	return mcp.NewToolResultStructured(receipt, fallback), nil
}
