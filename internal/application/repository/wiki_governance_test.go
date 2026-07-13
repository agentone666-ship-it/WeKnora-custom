package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newGovernanceTestRepo(t *testing.T) (*wikiGovernanceRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.WikiPage{}, &types.WikiChangeSet{}, &types.WikiChangeItem{}, &types.WikiReview{}, &types.WikiPackage{}, &types.WikiPackagePage{}, &types.WikiScenario{}, &types.WikiGovernanceMetricEvent{}); err != nil {
		t.Fatal(err)
	}
	return &wikiGovernanceRepository{db: db}, db
}

func snapshotForTest(t *testing.T, page *types.WikiPage) types.JSON {
	t.Helper()
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	return types.JSON(b)
}

func TestReviewChangeSetPublishesCardAtomically(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	page := &types.WikiPage{KnowledgeBaseID: "kb-1", Slug: "card/question-test", Title: "test", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeQuestion, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1}
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: page.Slug, After: snapshotForTest(t, page), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved}); err != nil {
		t.Fatal(err)
	}

	var stored types.WikiPage
	if err := db.Where("knowledge_base_id = ? AND slug = ?", "kb-1", page.Slug).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.WikiPageStatusPublished || stored.ReviewStatus != types.WikiReviewApproved || stored.MaturityStatus != types.WikiMaturityVerified {
		t.Fatalf("unexpected published state: status=%s review=%s maturity=%s", stored.Status, stored.ReviewStatus, stored.MaturityStatus)
	}
	var reviewCount int64
	if err := db.Model(&types.WikiReview{}).Where("change_set_id = ?", set.ID).Count(&reviewCount).Error; err != nil {
		t.Fatal(err)
	}
	if reviewCount != 1 {
		t.Fatalf("reviews=%d, want 1", reviewCount)
	}
	var packageCount int64
	if err := db.Model(&types.WikiPackage{}).Where("knowledge_base_id = ?", "kb-1").Count(&packageCount).Error; err != nil {
		t.Fatal(err)
	}
	if packageCount != 1 {
		t.Fatalf("managed packages=%d, want global package", packageCount)
	}
}

func TestReviewChangeSetRejectsStaleVersion(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	current := &types.WikiPage{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "card/knowledge-test", Title: "current", Content: "current", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, ChunkRefs: types.StringArray{"chunk-1"}, Version: 2, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(current).Error; err != nil {
		t.Fatal(err)
	}
	candidate := *current
	candidate.Content = "stale candidate"
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL2, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: 1, Before: snapshotForTest(t, current), After: snapshotForTest(t, &candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved})
	if !errors.Is(err, ErrWikiChangeSetConflict) {
		t.Fatalf("error=%v, want version conflict", err)
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != "current" {
		t.Fatalf("stale review overwrote page: %q", stored.Content)
	}
	var storedSet types.WikiChangeSet
	if err := db.First(&storedSet, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedSet.Status != types.WikiChangeSetConflict {
		t.Fatalf("change set status=%s, want conflict", storedSet.Status)
	}
}

func TestReviewCardWithoutEvidenceStaysDraft(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	page := &types.WikiPage{KnowledgeBaseID: "kb-1", Slug: "card/question-no-evidence", Title: "unresolved", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeQuestion, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, Version: 1}
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: page.Slug, After: snapshotForTest(t, page), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved}); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.Where("slug = ?", page.Slug).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.MaturityStatus != types.WikiMaturityDraft {
		t.Fatalf("maturity=%s, want draft", stored.MaturityStatus)
	}
}

func TestReviewCreatesScenarioAndManagedPackages(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	metadata, _ := json.Marshal(map[string]any{"scenario_names": []string{"Checkout recovery"}})
	page := &types.WikiPage{TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/procedure-recovery", Title: "recovery", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeProcedure, BusinessLine: "commerce", Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, ChunkRefs: types.StringArray{"chunk-1"}, PageMetadata: types.JSON(metadata), Version: 1}
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL2, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: page.Slug, After: snapshotForTest(t, page), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved}); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.Where("slug = ?", page.Slug).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored.ScenarioIDs) != 1 {
		t.Fatalf("scenario_ids=%v, want one approved scenario", stored.ScenarioIDs)
	}
	var scenarioCount, packageCount, linkCount int64
	if err := db.Model(&types.WikiScenario{}).Count(&scenarioCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&types.WikiPackage{}).Count(&packageCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&types.WikiPackagePage{}).Where("page_id = ?", stored.ID).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if scenarioCount != 1 || packageCount != 3 || linkCount != 3 {
		t.Fatalf("scenario=%d packages=%d links=%d, want 1/3/3", scenarioCount, packageCount, linkCount)
	}
}

func TestReviewCanMergeDuplicateCandidateIntoExistingCard(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	target := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-target", Title: "target", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, SourceRefs: types.StringArray{"doc-1"}, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(target).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &types.WikiPage{TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-duplicate", Title: "duplicate title", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, SourceRefs: types.StringArray{"doc-2"}, ChunkRefs: types.StringArray{"chunk-2"}, Version: 1}
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: candidate.Slug, After: snapshotForTest(t, candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved, MergeIntoSlug: target.Slug}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&types.WikiPage{}).Where("slug = ?", candidate.Slug).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("merge unexpectedly created duplicate card")
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", target.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Version != 2 || len(stored.SourceRefs) != 2 || len(stored.ChunkRefs) != 2 {
		t.Fatalf("merged target version=%d sources=%v chunks=%v", stored.Version, stored.SourceRefs, stored.ChunkRefs)
	}
	var item types.WikiChangeItem
	if err := db.Where("change_set_id = ?", set.ID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Operation != "update" || item.PageSlug != target.Slug {
		t.Fatalf("audit item operation=%s slug=%s", item.Operation, item.PageSlug)
	}
}

func TestRejectedChangeSetDoesNotModifyPublishedPage(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	current := &types.WikiPage{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "card/rule-current", Title: "current", Content: "current rule", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeRule, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, ChunkRefs: types.StringArray{"chunk-1"}, Version: 4, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(current).Error; err != nil {
		t.Fatal(err)
	}
	candidate := *current
	candidate.Content = "replacement rule"
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL2, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: current.Version, Before: snapshotForTest(t, current), After: snapshotForTest(t, &candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewRejected, Comment: "source is not authoritative"}); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != current.Content || stored.Version != current.Version {
		t.Fatalf("rejection modified page: content=%q version=%d", stored.Content, stored.Version)
	}
	var storedSet types.WikiChangeSet
	if err := db.First(&storedSet, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedSet.Status != types.WikiChangeSetRejected {
		t.Fatalf("status=%s, want rejected", storedSet.Status)
	}
}
