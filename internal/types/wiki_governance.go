package types

import "time"

const (
	WikiReviewLevelL0 = "L0"
	WikiReviewLevelL1 = "L1"
	// WikiReviewLevelL2 is retained only for reading pre-migration records.
	// New change sets must use the two-level L0/L1 policy.
	WikiReviewLevelL2 = "L2"

	WikiChangeCategoryAddition       = "addition"
	WikiChangeCategoryUpdate         = "update"
	WikiChangeCategoryConflict       = "conflict"
	WikiChangeCategoryCorrection     = "correction"
	WikiChangeCategoryRetirement     = "retirement"
	WikiChangeCategoryMergeDuplicate = "merge_duplicate"

	WikiChangeSetPending  = "pending"
	WikiChangeSetApproved = "approved"
	WikiChangeSetRejected = "rejected"
	WikiChangeSetApplied  = "applied"
	WikiChangeSetConflict = "conflict"

	WikiReviewDeferred = "deferred"

	WikiConflictKeepExisting   = "keep_existing"
	WikiConflictAdoptCandidate = "adopt_candidate"
	WikiConflictEditCandidate  = "edit_candidate"
	WikiConflictSplitScope     = "split_scope"
	WikiConflictDefer          = "defer"
	WikiCorrectionAutoApplied  = "automatic_correction"
)

type WikiPackage struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	Name            string    `json:"name" gorm:"type:varchar(255)"`
	PackageType     string    `json:"package_type" gorm:"type:varchar(32);index"`
	BusinessLine    string    `json:"business_line" gorm:"type:varchar(128);index"`
	Description     string    `json:"description" gorm:"type:text"`
	LoadingPolicy   string    `json:"loading_policy" gorm:"type:varchar(32);default:'on_demand'"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (WikiPackage) TableName() string { return "wiki_packages" }

type WikiPackagePage struct {
	PackageID string    `json:"package_id" gorm:"type:varchar(36);primaryKey"`
	PageID    string    `json:"page_id" gorm:"type:varchar(36);primaryKey"`
	CreatedAt time.Time `json:"created_at"`
}

func (WikiPackagePage) TableName() string { return "wiki_package_pages" }

type WikiScenario struct {
	ID              string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64      `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string      `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	Name            string      `json:"name" gorm:"type:varchar(255)"`
	BusinessLine    string      `json:"business_line" gorm:"type:varchar(128);index"`
	TargetRoles     StringArray `json:"target_roles" gorm:"type:json"`
	AffectedMetrics StringArray `json:"affected_metrics" gorm:"type:json"`
	Priority        string      `json:"priority" gorm:"type:varchar(16);default:'medium'"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func (WikiScenario) TableName() string { return "wiki_scenarios" }

type WikiChangeSet struct {
	ID                   string           `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID             uint64           `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID      string           `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	KnowledgeID          string           `json:"knowledge_id,omitempty" gorm:"type:varchar(36);index"`
	Status               string           `json:"status" gorm:"type:varchar(32);index"`
	ReviewLevel          string           `json:"review_level" gorm:"type:varchar(8);index"`
	ChangeCategory       string           `json:"change_category" gorm:"type:varchar(32);index;default:'update'"`
	Reasons              StringArray      `json:"reasons" gorm:"type:json"`
	ModelID              string           `json:"model_id,omitempty" gorm:"type:varchar(64)"`
	PromptVersion        string           `json:"prompt_version,omitempty" gorm:"type:varchar(32)"`
	CandidateFingerprint string           `json:"candidate_fingerprint,omitempty" gorm:"type:varchar(64);index"`
	CreatedBy            string           `json:"created_by,omitempty" gorm:"type:varchar(36)"`
	ReviewedBy           string           `json:"reviewed_by,omitempty" gorm:"type:varchar(36)"`
	ReviewComment        string           `json:"review_comment,omitempty" gorm:"type:text"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	ReviewedAt           *time.Time       `json:"reviewed_at,omitempty"`
	Items                []WikiChangeItem `json:"items,omitempty" gorm:"foreignKey:ChangeSetID"`
}

func (WikiChangeSet) TableName() string { return "wiki_change_sets" }

type WikiChangeItem struct {
	ID               string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	ChangeSetID      string      `json:"change_set_id" gorm:"type:varchar(36);index"`
	Operation        string      `json:"operation" gorm:"type:varchar(16)"`
	ChangeCategory   string      `json:"change_category" gorm:"type:varchar(32);index;default:'update'"`
	PageID           string      `json:"page_id,omitempty" gorm:"type:varchar(36);index"`
	PageSlug         string      `json:"page_slug" gorm:"type:varchar(255);index"`
	ExpectedVersion  int         `json:"expected_version"`
	Before           JSON        `json:"before" gorm:"type:json"`
	After            JSON        `json:"after" gorm:"type:json"`
	ChangedFields    StringArray `json:"changed_fields" gorm:"type:json"`
	EvidenceChunkIDs StringArray `json:"evidence_chunk_ids" gorm:"type:json"`
	EvidenceExcerpts JSON        `json:"evidence_excerpts" gorm:"type:json"`
	CreatedAt        time.Time   `json:"created_at"`
}

func (WikiChangeItem) TableName() string { return "wiki_change_items" }

type WikiReview struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	ChangeSetID     string    `json:"change_set_id" gorm:"type:varchar(36);index"`
	ReviewerID      string    `json:"reviewer_id" gorm:"type:varchar(36);index"`
	Decision        string    `json:"decision" gorm:"type:varchar(16)"`
	Comment         string    `json:"comment" gorm:"type:text"`
	ItemOverrides   JSON      `json:"item_overrides,omitempty" gorm:"type:json"`
	Resolution      string    `json:"resolution,omitempty" gorm:"type:varchar(32)"`
	RetainedClaim   string    `json:"retained_claim,omitempty" gorm:"type:text"`
	DiscardedClaims JSON      `json:"discarded_claims,omitempty" gorm:"type:json"`
	CreatedAt       time.Time `json:"created_at"`
}

func (WikiReview) TableName() string { return "wiki_reviews" }

type WikiGovernanceListRequest struct {
	KnowledgeBaseID string
	Status          string
	ReviewLevel     string
	ChangeCategory  string
	Limit           int
	Offset          int
}

type WikiReviewDecision struct {
	Decision      string          `json:"decision" binding:"required"`
	Comment       string          `json:"comment"`
	MergeIntoSlug string          `json:"merge_into_slug,omitempty"`
	ItemOverrides map[string]JSON `json:"item_overrides,omitempty"`
	Resolution    string          `json:"resolution,omitempty"`
	RetainedClaim string          `json:"retained_claim,omitempty"`
}

type WikiGovernanceStats struct {
	CardsByKnowledgeType     map[string]int64 `json:"cards_by_knowledge_type"`
	CardsByMaturity          map[string]int64 `json:"cards_by_maturity"`
	ChangeSetsByLevel        map[string]int64 `json:"change_sets_by_level"`
	ChangeSetsByStatus       map[string]int64 `json:"change_sets_by_status"`
	ReviewsByDecision        map[string]int64 `json:"reviews_by_decision"`
	PendingReviewCount       int64            `json:"pending_review_count"`
	OldestPendingAt          *time.Time       `json:"oldest_pending_at,omitempty"`
	AutoPublishedRate        float64          `json:"auto_published_rate"`
	ReviewApprovalRate       float64          `json:"review_approval_rate"`
	ReviewModifiedRate       float64          `json:"review_modified_rate"`
	TypeCorrectionRate       float64          `json:"type_correction_rate"`
	AverageReviewSeconds     float64          `json:"average_review_seconds"`
	HypothesisFactViolations int64            `json:"hypothesis_fact_violations"`
}

type WikiGovernanceMetricEvent struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	MetricName      string    `json:"metric_name" gorm:"type:varchar(64);index"`
	Value           int64     `json:"value"`
	Metadata        JSON      `json:"metadata" gorm:"type:json"`
	CreatedAt       time.Time `json:"created_at"`
}

func (WikiGovernanceMetricEvent) TableName() string { return "wiki_governance_metric_events" }
