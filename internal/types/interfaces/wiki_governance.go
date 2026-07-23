package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type WikiGovernanceRepository interface {
	CreateChangeSet(ctx context.Context, set *types.WikiChangeSet) error
	FindChangeSetByFingerprint(ctx context.Context, kbID, fingerprint string) (*types.WikiChangeSet, error)
	GetChangeSet(ctx context.Context, kbID, id string) (*types.WikiChangeSet, error)
	ListChangeSets(ctx context.Context, req types.WikiGovernanceListRequest) ([]*types.WikiChangeSet, int64, error)
	ListPageChangeSets(ctx context.Context, kbID, slug string, limit int) ([]*types.WikiChangeSet, error)
	ListPendingGraphCards(ctx context.Context, kbID string, limit int) ([]*types.WikiPendingGraphCard, error)
	UpdatePendingGraphCard(ctx context.Context, kbID, changeItemID string, page *types.WikiPage) (bool, error)
	ReviewChangeSet(ctx context.Context, kbID, id, reviewerID string, decision *types.WikiReviewDecision) error
	ListReviews(ctx context.Context, changeSetID string) ([]*types.WikiReview, error)
	ListPackages(ctx context.Context, kbID string) ([]*types.WikiPackage, error)
	CreatePackage(ctx context.Context, pkg *types.WikiPackage) error
	ListPackagePages(ctx context.Context, kbID, packageID string) ([]*types.WikiPage, error)
	SetPackagePages(ctx context.Context, kbID, packageID string, pageIDs []string) error
	ListScenarios(ctx context.Context, kbID string) ([]*types.WikiScenario, error)
	CreateScenario(ctx context.Context, scenario *types.WikiScenario) error
	ListScenarioPages(ctx context.Context, kbID, scenarioID string) ([]*types.WikiPage, error)
	GetGovernanceStats(ctx context.Context, kbID string) (*types.WikiGovernanceStats, error)
	RecordGovernanceMetric(ctx context.Context, event *types.WikiGovernanceMetricEvent) error
	CreateRollbackChangeSet(ctx context.Context, kbID, appliedChangeSetID, requestedBy string) (*types.WikiChangeSet, error)
	SubmitFeedbackSignal(ctx context.Context, tenantID uint64, kbID, actor string, input *types.WikiFeedbackSignalInput) (*types.WikiFeedbackSignal, error)
	ListFeedbackSignals(ctx context.Context, req types.WikiFeedbackSignalListRequest) ([]*types.WikiFeedbackSignal, int64, error)
	GetFeedbackSignal(ctx context.Context, kbID, id string) (*types.WikiFeedbackSignal, error)
}

type WikiGovernanceService interface {
	SubmitCandidate(ctx context.Context, candidate *types.WikiPage, existing *types.WikiPage, knowledgeID, modelID string) (*types.WikiChangeSet, error)
	GetChangeSet(ctx context.Context, kbID, id string) (*types.WikiChangeSet, error)
	ListChangeSets(ctx context.Context, req types.WikiGovernanceListRequest) ([]*types.WikiChangeSet, int64, error)
	ListPageChangeSets(ctx context.Context, kbID, slug string, limit int) ([]*types.WikiChangeSet, error)
	ListPendingGraphCards(ctx context.Context, kbID string, limit int) ([]*types.WikiPendingGraphCard, error)
	UpdatePendingGraphCard(ctx context.Context, kbID, changeItemID string, page *types.WikiPage) (bool, error)
	ReviewChangeSet(ctx context.Context, kbID, id, reviewerID string, decision *types.WikiReviewDecision) error
	ListReviews(ctx context.Context, changeSetID string) ([]*types.WikiReview, error)
	ListPackages(ctx context.Context, kbID string) ([]*types.WikiPackage, error)
	CreatePackage(ctx context.Context, pkg *types.WikiPackage) error
	ListPackagePages(ctx context.Context, kbID, packageID string) ([]*types.WikiPage, error)
	SetPackagePages(ctx context.Context, kbID, packageID string, pageIDs []string) error
	ListScenarios(ctx context.Context, kbID string) ([]*types.WikiScenario, error)
	CreateScenario(ctx context.Context, scenario *types.WikiScenario) error
	ListScenarioPages(ctx context.Context, kbID, scenarioID string) ([]*types.WikiPage, error)
	GetGovernanceStats(ctx context.Context, kbID string) (*types.WikiGovernanceStats, error)
	RecordGovernanceMetric(ctx context.Context, event *types.WikiGovernanceMetricEvent) error
	CreateRollbackChangeSet(ctx context.Context, kbID, appliedChangeSetID, requestedBy string) (*types.WikiChangeSet, error)
	SubmitFeedbackSignal(ctx context.Context, tenantID uint64, kbID, actor string, input *types.WikiFeedbackSignalInput) (*types.WikiFeedbackSignal, error)
	ListFeedbackSignals(ctx context.Context, req types.WikiFeedbackSignalListRequest) ([]*types.WikiFeedbackSignal, int64, error)
	GetFeedbackSignal(ctx context.Context, kbID, id string) (*types.WikiFeedbackSignal, error)
}
