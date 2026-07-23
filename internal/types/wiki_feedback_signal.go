package types

import "time"

const (
	WikiFeedbackReceived      = "received"
	WikiFeedbackPendingReview = "pending_review"
	WikiFeedbackPublished     = "published"
	WikiFeedbackRejected      = "rejected"
	WikiFeedbackConflict      = "conflict"
)

// WikiFeedbackSignal is the durable link between an external feedback event
// and the governed change set generated from it.
type WikiFeedbackSignal struct {
	ID                  string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID            uint64      `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID     string      `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	Source              string      `json:"source" gorm:"type:varchar(32);index"`
	SignalType          string      `json:"signal_type" gorm:"type:varchar(32);index"`
	FeedbackText        string      `json:"feedback_text" gorm:"type:text"`
	OriginalQuestion    string      `json:"original_question" gorm:"type:text"`
	AnswerExcerpt       string      `json:"answer_excerpt" gorm:"type:text"`
	SuggestedCorrection string      `json:"suggested_correction" gorm:"type:text"`
	RelatedRequestID    string      `json:"related_request_id" gorm:"type:varchar(64);index"`
	SessionID           string      `json:"session_id" gorm:"type:varchar(64);index"`
	AgentID             string      `json:"agent_id" gorm:"type:varchar(64);index"`
	RecalledNodeIDs     StringArray `json:"recalled_node_ids" gorm:"type:json"`
	TargetNodeIDs       StringArray `json:"target_node_ids" gorm:"type:json"`
	AttributedNodeIDs   StringArray `json:"attributed_node_ids" gorm:"type:json"`
	Status              string      `json:"status" gorm:"type:varchar(32);index"`
	Strategy            string      `json:"strategy" gorm:"type:varchar(32);index"`
	RiskLevel           string      `json:"risk_level" gorm:"type:varchar(16);index"`
	Confidence          float64     `json:"confidence"`
	ConflictSummary     string      `json:"conflict_summary" gorm:"type:text"`
	ChangeSetID         string      `json:"change_set_id" gorm:"type:varchar(36);index"`
	IdempotencyKey      string      `json:"idempotency_key" gorm:"type:varchar(128);uniqueIndex"`
	CreatedBy           string      `json:"created_by" gorm:"type:varchar(255)"`
	ReviewedBy          string      `json:"reviewed_by" gorm:"type:varchar(255)"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

func (WikiFeedbackSignal) TableName() string { return "wiki_feedback_signals" }

type WikiFeedbackSignalInput struct {
	Source              string      `json:"source"`
	SignalType          string      `json:"signal_type"`
	FeedbackText        string      `json:"feedback_text" binding:"required"`
	OriginalQuestion    string      `json:"original_question"`
	AnswerExcerpt       string      `json:"answer_excerpt"`
	SuggestedCorrection string      `json:"suggested_correction"`
	RelatedRequestID    string      `json:"related_request_id"`
	SessionID           string      `json:"session_id"`
	AgentID             string      `json:"agent_id"`
	RecalledNodeIDs     StringArray `json:"recalled_node_ids"`
	TargetNodeIDs       StringArray `json:"target_node_ids"`
	IdempotencyKey      string      `json:"idempotency_key"`
}

type WikiFeedbackSignalListRequest struct {
	KnowledgeBaseID string
	Status          string
	Limit           int
	Offset          int
}
