package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	if err := db.AutoMigrate(&types.WikiPage{}, &types.WikiPageVersion{}, &types.WikiChangeSet{}, &types.WikiChangeItem{}, &types.WikiReview{}, &types.WikiPackage{}, &types.WikiPackagePage{}, &types.WikiScenario{}, &types.WikiGovernanceMetricEvent{}); err != nil {
		t.Fatal(err)
	}
	return &wikiGovernanceRepository{db: db}, db
}

func TestReviewUpdateSeedsOldAndPublishedVersions(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	oldPage := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-versioned", Title: "versioned", Content: "old claim", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(oldPage).Error; err != nil {
		t.Fatal(err)
	}
	newPage := *oldPage
	newPage.Content = "corrected claim"
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryCorrection, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", PageID: oldPage.ID, PageSlug: oldPage.Slug, ExpectedVersion: 1, Before: snapshotForTest(t, oldPage), After: snapshotForTest(t, &newPage), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved}); err != nil {
		t.Fatal(err)
	}
	var versions []types.WikiPageVersion
	if err := db.Where("page_id = ?", oldPage.ID).Order("version ASC").Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].State != types.WikiPageVersionHistory || versions[1].State != types.WikiPageVersionPublished {
		t.Fatalf("versions=%+v, want old history and corrected published versions", versions)
	}
	var oldSnapshot, publishedSnapshot types.WikiPage
	if err := json.Unmarshal(versions[0].Snapshot, &oldSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(versions[1].Snapshot, &publishedSnapshot); err != nil {
		t.Fatal(err)
	}
	if oldSnapshot.Content != "old claim" || publishedSnapshot.Content != "corrected claim" {
		t.Fatalf("snapshot contents=%q/%q", oldSnapshot.Content, publishedSnapshot.Content)
	}
}

func snapshotForTest(t *testing.T, page *types.WikiPage) types.JSON {
	t.Helper()
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	return types.JSON(b)
}

func TestCreateChangeSetNormalizesEmptyJSONFields(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	set := &types.WikiChangeSet{
		ID:              uuid.NewString(),
		KnowledgeBaseID: "kb-1",
		Status:          types.WikiChangeSetPending,
		ReviewLevel:     types.WikiReviewLevelL1,
		CreatedAt:       now,
		UpdatedAt:       now,
		Items: []types.WikiChangeItem{{
			ID:        uuid.NewString(),
			Operation: "create",
			PageSlug:  "card/question-empty-json",
			CreatedAt: now,
		}},
	}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}

	var stored types.WikiChangeItem
	if err := db.First(&stored, "id = ?", set.Items[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Before.ToString() != "{}" || stored.After.ToString() != "{}" || stored.EvidenceExcerpts.ToString() != "{}" {
		t.Fatalf("JSON objects were not normalized: before=%s after=%s evidence=%s", stored.Before, stored.After, stored.EvidenceExcerpts)
	}
	if stored.ChangedFields == nil || stored.EvidenceChunkIDs == nil {
		t.Fatalf("JSON arrays were not normalized: changed=%v evidence=%v", stored.ChangedFields, stored.EvidenceChunkIDs)
	}
}

func TestCreateChangeSetNormalizesLegacyL2ToL1(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL2, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiChangeSet
	if err := db.First(&stored, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReviewLevel != types.WikiReviewLevelL1 || stored.ChangeCategory != types.WikiChangeCategoryUpdate {
		t.Fatalf("legacy change set normalized to level/category=%s/%s, want L1/update", stored.ReviewLevel, stored.ChangeCategory)
	}
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

func TestPendingGraphCardStaysIsolatedAndPublishesLatestGraph(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	page := &types.WikiPage{TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/question-graph", Title: "graph", Content: "# graph\n\nclaim\n", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeQuestion, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1}
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryConflict, Reasons: types.StringArray{"cross_page_claim_conflict"}, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", ChangeCategory: types.WikiChangeCategoryConflict, PageSlug: page.Slug, After: snapshotForTest(t, page), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}

	var publishedCount int64
	if err := db.Model(&types.WikiPage{}).Where("knowledge_base_id = ?", "kb-1").Count(&publishedCount).Error; err != nil {
		t.Fatal(err)
	}
	if publishedCount != 0 {
		t.Fatalf("pending candidate leaked into wiki_pages: count=%d", publishedCount)
	}
	candidates, err := repo.ListPendingGraphCards(context.Background(), "kb-1", 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("pending graph candidates=%d err=%v, want 1", len(candidates), err)
	}
	graphPage := candidates[0].Page
	graphPage.Content += "\n## 关联知识\n- [[card/knowledge-target|target]]（answers）\n"
	graphPage.OutLinks = types.StringArray{"card/knowledge-target"}
	graphPage.PageMetadata = types.JSON(`{"relationships":[{"target_slug":"card/knowledge-target","relation_type":"answers"}]}`)
	governance := types.WikiPendingGraphGovernance{SyncReviewEnvelope: true, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryAddition, Reasons: types.StringArray{"new_reviewable_card"}}
	updated, err := repo.UpdatePendingGraphCard(context.Background(), "kb-1", candidates[0].ChangeItemID, graphPage, governance)
	if err != nil || !updated {
		t.Fatalf("update pending graph updated=%v err=%v", updated, err)
	}
	var reclassified types.WikiChangeSet
	if err := db.Preload("Items").First(&reclassified, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reclassified.ChangeCategory != types.WikiChangeCategoryAddition || reclassified.Items[0].ChangeCategory != types.WikiChangeCategoryAddition {
		t.Fatalf("graph convergence left stale categories set=%s item=%s", reclassified.ChangeCategory, reclassified.Items[0].ChangeCategory)
	}
	if len(reclassified.Reasons) != 1 || reclassified.Reasons[0] != "new_reviewable_card" {
		t.Fatalf("graph governance was not synchronized: reasons=%v", reclassified.Reasons)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewApproved}); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.Where("knowledge_base_id = ? AND slug = ?", "kb-1", page.Slug).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored.OutLinks) != 1 || stored.OutLinks[0] != "card/knowledge-target" || !strings.Contains(stored.Content, "关联知识") {
		t.Fatalf("approval did not publish latest candidate graph: out=%v content=%q", stored.OutLinks, stored.Content)
	}
	if updated, err := repo.UpdatePendingGraphCard(context.Background(), "kb-1", candidates[0].ChangeItemID, graphPage, governance); err != nil || updated {
		t.Fatalf("applied snapshot must reject candidate graph writes: updated=%v err=%v", updated, err)
	}
}

func TestPendingGraphCardPreservesNonGraphReviewEnvelope(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	first := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-feedback-a", Title: "feedback a", Content: "old a", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, Version: 1}
	second := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-feedback-b", Title: "feedback b", Content: "old b", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, Version: 1}
	set := &types.WikiChangeSet{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending,
		ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryCorrection,
		Reasons: types.StringArray{"feedback_signal", "incorrect", "pending_feedback_conflict"},
		ModelID: "feedback-loop", PromptVersion: "feedback-candidate-v2-full-page", CandidateFingerprint: "feedback-key",
		CreatedAt: now, UpdatedAt: now,
		Items: []types.WikiChangeItem{
			{ID: uuid.NewString(), Operation: "update", ChangeCategory: types.WikiChangeCategoryCorrection, PageID: first.ID, PageSlug: first.Slug, Before: snapshotForTest(t, first), After: snapshotForTest(t, first), CreatedAt: now},
			{ID: uuid.NewString(), Operation: "update", ChangeCategory: types.WikiChangeCategoryCorrection, PageID: second.ID, PageSlug: second.Slug, Before: snapshotForTest(t, second), After: snapshotForTest(t, second), CreatedAt: now},
		},
	}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}

	graphPage := *first
	graphPage.Content = "old a\n\n## 关联知识\n- [[card/knowledge-feedback-b|feedback b]]（related）\n"
	graphPage.OutLinks = types.StringArray{second.Slug}
	graphPage.PageMetadata = types.JSON(`{"relationships":[{"target_slug":"card/knowledge-feedback-b","relation_type":"related"}]}`)
	updated, err := repo.UpdatePendingGraphCard(context.Background(), "kb-1", set.Items[0].ID, &graphPage, types.WikiPendingGraphGovernance{})
	if err != nil || !updated {
		t.Fatalf("update pending feedback graph updated=%v err=%v", updated, err)
	}

	var stored types.WikiChangeSet
	if err := db.Preload("Items").First(&stored, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReviewLevel != types.WikiReviewLevelL1 || stored.ChangeCategory != types.WikiChangeCategoryCorrection {
		t.Fatalf("feedback review envelope changed to %s/%s", stored.ReviewLevel, stored.ChangeCategory)
	}
	if strings.Join(stored.Reasons, ",") != "feedback_signal,incorrect,pending_feedback_conflict" {
		t.Fatalf("feedback reasons changed: %v", stored.Reasons)
	}
	for _, item := range stored.Items {
		if item.ChangeCategory != types.WikiChangeCategoryCorrection {
			t.Fatalf("feedback item %s category changed to %s", item.ID, item.ChangeCategory)
		}
	}
	if !strings.Contains(stored.Items[0].After.ToString(), "关联知识") {
		t.Fatalf("graph-owned candidate snapshot was not updated: %s", stored.Items[0].After.ToString())
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
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: 1, Before: snapshotForTest(t, current), After: snapshotForTest(t, &candidate), CreatedAt: now}}}
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

func TestConflictReviewAdoptsCandidateAndArchivesOldVersionWithAudit(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	old := &types.WikiPage{
		ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "concept/refund-three-days", Title: "退款三日到账",
		PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, Content: "退款必须在3个工作日到账",
		ReviewStatus: types.WikiReviewApproved, Version: 2, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]any{"cross_page_assessments": []map[string]any{{
		"related_slug": old.Slug, "relation": "conflicting", "candidate_claim": "退款必须在5个工作日到账",
		"existing_claim": "退款必须在3个工作日到账", "reason": "到账时限不同", "confidence": 0.99, "applicability_overlap": true,
	}}})
	candidate := &types.WikiPage{
		KnowledgeBaseID: "kb-1", Slug: "card/rule-refund-five-days", Title: "退款五日到账", PageType: types.WikiPageTypeCard,
		KnowledgeType: types.WikiKnowledgeTypeRule, MaturityStatus: types.WikiMaturityPendingReview, ReviewStatus: types.WikiReviewPending,
		Status: types.WikiPageStatusDraft, Content: "退款必须在5个工作日到账", Summary: "五日到账规则", ChunkRefs: types.StringArray{"chunk-new"},
		PageMetadata: types.JSON(metadata), Version: 1,
	}
	set := &types.WikiChangeSet{
		ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1,
		ChangeCategory: types.WikiChangeCategoryConflict, Reasons: types.StringArray{"cross_page_claim_conflict"}, CreatedAt: now, UpdatedAt: now,
		Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", ChangeCategory: types.WikiChangeCategoryConflict, PageSlug: candidate.Slug, After: snapshotForTest(t, candidate), CreatedAt: now}},
	}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, Resolution: types.WikiConflictAdoptCandidate, RetainedClaim: "退款必须在5个工作日到账", Comment: "新制度已生效"}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", decision); err != nil {
		t.Fatal(err)
	}

	var archived types.WikiPage
	if err := db.First(&archived, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if archived.Status != types.WikiPageStatusArchived || archived.MaturityStatus != types.WikiMaturityOutdated || archived.EffectiveTo == nil {
		t.Fatalf("old page was not retired: status=%s maturity=%s effective_to=%v", archived.Status, archived.MaturityStatus, archived.EffectiveTo)
	}
	var published types.WikiPage
	if err := db.First(&published, "knowledge_base_id = ? AND slug = ?", "kb-1", candidate.Slug).Error; err != nil {
		t.Fatal(err)
	}
	if published.Status != types.WikiPageStatusPublished || published.ReviewStatus != types.WikiReviewApproved {
		t.Fatalf("candidate was not published: status=%s review=%s", published.Status, published.ReviewStatus)
	}
	var itemCount int64
	if err := db.Model(&types.WikiChangeItem{}).Where("change_set_id = ?", set.ID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 {
		t.Fatalf("change items=%d, want candidate plus retired version", itemCount)
	}
	var review types.WikiReview
	if err := db.First(&review, "change_set_id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if review.Resolution != types.WikiConflictAdoptCandidate || review.RetainedClaim != decision.RetainedClaim || !strings.Contains(review.DiscardedClaims.ToString(), "3个工作日") {
		t.Fatalf("incomplete conflict audit: resolution=%s retained=%q discarded=%s", review.Resolution, review.RetainedClaim, review.DiscardedClaims)
	}
}

func TestConflictReviewKeepExistingRejectsCandidateAndKeepsOldPage(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	old := &types.WikiPage{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "card/rule-current", Title: "现行规则", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeRule, MaturityStatus: types.WikiMaturityVerified, ReviewStatus: types.WikiReviewApproved, Status: types.WikiPageStatusPublished, ChunkRefs: types.StringArray{"chunk-old"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &types.WikiPage{KnowledgeBaseID: "kb-1", Slug: "card/rule-candidate", Title: "候选规则", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeRule, PageMetadata: types.JSON(`{"cross_page_assessments":[{"related_slug":"card/rule-current","relation":"conflicting","candidate_claim":"新说法","existing_claim":"旧说法","reason":"直接冲突","confidence":1,"applicability_overlap":true}]}`)}
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryConflict, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: candidate.Slug, After: snapshotForTest(t, candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", &types.WikiReviewDecision{Decision: types.WikiReviewRejected, Resolution: types.WikiConflictKeepExisting, Comment: "旧制度仍有效"}); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.WikiPageStatusPublished || stored.Version != old.Version {
		t.Fatalf("existing page changed unexpectedly: status=%s version=%d", stored.Status, stored.Version)
	}
}

func TestConflictReviewAppliesIndependentChoicesAndKeepsAudit(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	existing := &types.WikiPage{
		ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "entity/mai-jia", Title: "买家报价时长",
		PageType: types.WikiPageTypeEntity, Status: types.WikiPageStatusPublished,
		Content:      "手机商品报价窗口为30分钟；其他多品类商品报价窗口为2小时。",
		ReviewStatus: types.WikiReviewApproved, Version: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(existing).Error; err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]any{"cross_page_assessments": []map[string]any{
		{"related_slug": existing.Slug, "relation": "conflicting", "candidate_claim": "手机商品报价窗口为8分钟", "existing_claim": "手机商品报价窗口为30分钟", "reason": "手机报价时长不同", "confidence": 1, "applicability_overlap": true},
		{"related_slug": existing.Slug, "relation": "conflicting", "candidate_claim": "其他多品类商品报价窗口为30分钟", "existing_claim": "其他多品类商品报价窗口为2小时", "reason": "多品类报价时长不同", "confidence": 1, "applicability_overlap": true},
	}})
	candidate := &types.WikiPage{
		KnowledgeBaseID: "kb-1", Slug: "card/rule-quote-window", Title: "报价窗口", PageType: types.WikiPageTypeCard,
		KnowledgeType: types.WikiKnowledgeTypeRule, MaturityStatus: types.WikiMaturityPendingReview,
		ReviewStatus: types.WikiReviewPending, Status: types.WikiPageStatusDraft,
		Content: "手机商品报价窗口为8分钟；其他多品类商品报价窗口为30分钟。", Summary: "报价窗口规则",
		ChunkRefs: types.StringArray{"chunk-new"}, PageMetadata: types.JSON(metadata), Version: 1,
	}
	itemID := uuid.NewString()
	set := &types.WikiChangeSet{
		ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1,
		ChangeCategory: types.WikiChangeCategoryConflict, CreatedAt: now, UpdatedAt: now,
		Items: []types.WikiChangeItem{{ID: itemID, Operation: "create", ChangeCategory: types.WikiChangeCategoryConflict, PageSlug: candidate.Slug, After: snapshotForTest(t, candidate), CreatedAt: now}},
	}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	decision := &types.WikiReviewDecision{
		Decision: types.WikiReviewApproved, Resolution: types.WikiConflictPerClaim, Comment: "逐项选择",
		ConflictChoices: []types.WikiConflictChoice{
			{ItemID: itemID, AssessmentIndex: 0, Resolution: types.WikiConflictAdoptCandidate},
			{ItemID: itemID, AssessmentIndex: 1, Resolution: types.WikiConflictKeepExisting},
		},
	}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", decision); err != nil {
		t.Fatal(err)
	}

	var published types.WikiPage
	if err := db.First(&published, "knowledge_base_id = ? AND slug = ?", "kb-1", candidate.Slug).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(published.Content, "手机商品报价窗口为8分钟") || !strings.Contains(published.Content, "其他多品类商品报价窗口为2小时") || strings.Contains(published.Content, "其他多品类商品报价窗口为30分钟") {
		t.Fatalf("candidate did not reflect mixed choices: %q", published.Content)
	}
	var corrected types.WikiPage
	if err := db.First(&corrected, "id = ?", existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if corrected.Status != types.WikiPageStatusPublished || corrected.Version != 4 || !strings.Contains(corrected.Content, "手机商品报价窗口为8分钟") || !strings.Contains(corrected.Content, "其他多品类商品报价窗口为2小时") {
		t.Fatalf("existing page was not selectively corrected: status=%s version=%d content=%q", corrected.Status, corrected.Version, corrected.Content)
	}
	var review types.WikiReview
	if err := db.First(&review, "change_set_id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review.ConflictChoices.ToString(), "手机报价时长不同") || !strings.Contains(review.DiscardedClaims.ToString(), "30分钟") {
		t.Fatalf("per-conflict audit is incomplete: choices=%s discarded=%s", review.ConflictChoices, review.DiscardedClaims)
	}
}

func TestConflictReviewRequiresEveryConflictChoice(t *testing.T) {
	page := &types.WikiPage{PageMetadata: types.JSON(`{"cross_page_assessments":[{"related_slug":"entity/a","relation":"conflicting","candidate_claim":"new-a","existing_claim":"old-a"},{"related_slug":"entity/a","relation":"conflicting","candidate_claim":"new-b","existing_claim":"old-b"}]}`)}
	items := []types.WikiChangeItem{{ID: "item-1", After: snapshotForTest(t, page)}}
	set := &types.WikiChangeSet{ChangeCategory: types.WikiChangeCategoryConflict}
	decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, Resolution: types.WikiConflictPerClaim, ConflictChoices: []types.WikiConflictChoice{{ItemID: "item-1", AssessmentIndex: 0, Resolution: types.WikiConflictAdoptCandidate}}}
	err := validateConflictResolution(set, items, decision)
	if err == nil || !strings.Contains(err.Error(), "every conflict position must be selected") {
		t.Fatalf("error=%v, want incomplete-selection validation", err)
	}
}

func TestAutomaticCorrectionReviewPreservesDiscardedClaim(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	current := &types.WikiPage{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Slug: "card/rule-response-time", Title: "响应时限", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeRule, Content: "响应时限为3个工作日", Summary: "3日响应", Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, ChunkRefs: types.StringArray{"chunk-old"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(current).Error; err != nil {
		t.Fatal(err)
	}
	candidate := *current
	candidate.Content, candidate.Summary = "响应时限现更正为5个工作日", "5日响应"
	candidate.ChunkRefs = types.StringArray{"chunk-new"}
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL0, ChangeCategory: types.WikiChangeCategoryCorrection, Reasons: types.StringArray{"authoritative_explicit_correction", "card_conclusion_changed"}, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", ChangeCategory: types.WikiChangeCategoryCorrection, PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: current.Version, Before: snapshotForTest(t, current), After: snapshotForTest(t, &candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, Resolution: types.WikiCorrectionAutoApplied, RetainedClaim: candidate.Summary, Comment: "automatic correction"}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "system", decision); err != nil {
		t.Fatal(err)
	}
	var review types.WikiReview
	if err := db.First(&review, "change_set_id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if review.ReviewerID != "system" || review.Resolution != types.WikiCorrectionAutoApplied || review.RetainedClaim != "5日响应" || !strings.Contains(review.DiscardedClaims.ToString(), "3日响应") {
		t.Fatalf("automatic correction audit incomplete: reviewer=%s resolution=%s retained=%q discarded=%s", review.ReviewerID, review.Resolution, review.RetainedClaim, review.DiscardedClaims)
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Version != 2 || stored.Summary != "5日响应" {
		t.Fatalf("automatic correction not applied: version=%d summary=%q", stored.Version, stored.Summary)
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
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "create", PageSlug: page.Slug, After: snapshotForTest(t, page), CreatedAt: now}}}
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
	target := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-target", Title: "target", Content: "existing content", Summary: "existing summary", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, SourceRefs: types.StringArray{"doc-1"}, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(target).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &types.WikiPage{TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-duplicate", Title: "duplicate title", Content: "candidate content", Summary: "candidate summary", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, SourceRefs: types.StringArray{"doc-2"}, ChunkRefs: types.StringArray{"chunk-2"}, Version: 1}
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
	if stored.Version != 2 || len(stored.SourceRefs) != 2 || len(stored.ChunkRefs) != 2 || stored.Content != "existing content" || stored.Summary != "existing summary" {
		t.Fatalf("merged target version=%d sources=%v chunks=%v content=%q summary=%q", stored.Version, stored.SourceRefs, stored.ChunkRefs, stored.Content, stored.Summary)
	}
	var item types.WikiChangeItem
	if err := db.Where("change_set_id = ?", set.ID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Operation != "update" || item.PageSlug != target.Slug {
		t.Fatalf("audit item operation=%s slug=%s", item.Operation, item.PageSlug)
	}
}

func TestReviewMergeDuplicateAppliesChosenContentAndRecordsVersion(t *testing.T) {
	repo, db := newGovernanceTestRepo(t)
	now := time.Now()
	target := &types.WikiPage{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-target-content", Title: "target", Content: "old content", Summary: "old summary", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusPublished, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, SourceRefs: types.StringArray{"doc-1"}, ChunkRefs: types.StringArray{"chunk-1"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(target).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &types.WikiPage{TenantID: 1, KnowledgeBaseID: "kb-1", Slug: "card/knowledge-duplicate-content", Title: "duplicate title", Content: "candidate content", Summary: "candidate summary", PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeKnowledge, Status: types.WikiPageStatusDraft, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityPendingReview, SourceRefs: types.StringArray{"doc-2"}, ChunkRefs: types.StringArray{"chunk-2"}, Version: 1}
	itemID := uuid.NewString()
	set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryMergeDuplicate, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: itemID, Operation: "create", ChangeCategory: types.WikiChangeCategoryMergeDuplicate, PageSlug: candidate.Slug, After: snapshotForTest(t, candidate), CreatedAt: now}}}
	if err := repo.CreateChangeSet(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	override, _ := json.Marshal(map[string]any{"content": "reviewer chosen content", "summary": "reviewer chosen summary"})
	decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, MergeIntoSlug: target.Slug, ItemOverrides: map[string]types.JSON{itemID: types.JSON(override)}}
	if err := repo.ReviewChangeSet(context.Background(), "kb-1", set.ID, "reviewer-1", decision); err != nil {
		t.Fatal(err)
	}
	var stored types.WikiPage
	if err := db.First(&stored, "id = ?", target.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != "reviewer chosen content" || stored.Summary != "reviewer chosen summary" || stored.Version != 2 {
		t.Fatalf("merged target content=%q summary=%q version=%d", stored.Content, stored.Summary, stored.Version)
	}
	var versions []types.WikiPageVersion
	if err := db.Where("page_id = ?", target.ID).Order("version ASC").Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].State != types.WikiPageVersionHistory || versions[1].State != types.WikiPageVersionPublished {
		t.Fatalf("versions=%+v, want old history and merged published versions", versions)
	}
	var oldSnapshot, publishedSnapshot types.WikiPage
	if err := json.Unmarshal(versions[0].Snapshot, &oldSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(versions[1].Snapshot, &publishedSnapshot); err != nil {
		t.Fatal(err)
	}
	if oldSnapshot.Content != "old content" || publishedSnapshot.Content != "reviewer chosen content" {
		t.Fatalf("version contents=%q/%q", oldSnapshot.Content, publishedSnapshot.Content)
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
	set := &types.WikiChangeSet{ID: uuid.NewString(), KnowledgeBaseID: "kb-1", Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, CreatedAt: now, UpdatedAt: now, Items: []types.WikiChangeItem{{ID: uuid.NewString(), Operation: "update", PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: current.Version, Before: snapshotForTest(t, current), After: snapshotForTest(t, &candidate), CreatedAt: now}}}
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
