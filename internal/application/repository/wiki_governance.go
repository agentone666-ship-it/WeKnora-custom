package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrWikiChangeSetConflict = errors.New("wiki change set version conflict")

func updateFeedbackSignalForChangeSet(tx *gorm.DB, changeSetID string, values map[string]any) error {
	// Older unit-test fixtures and pre-71 databases do not have the feedback
	// table yet. A normal governance review must remain backwards compatible.
	if !tx.Migrator().HasTable(&types.WikiFeedbackSignal{}) {
		return nil
	}
	return tx.Model(&types.WikiFeedbackSignal{}).Where("change_set_id = ?", changeSetID).Updates(values).Error
}

func recordGovernancePublishedVersion(tx *gorm.DB, page *types.WikiPage, before types.JSON, operation, actor, summary string, now time.Time) error {
	// Keep governance usable while an older database is being upgraded. Once
	// migration 70 is present, every approved mutation must participate in the
	// same version lifecycle as edits made from the version-management UI.
	if !tx.Migrator().HasTable(&types.WikiPageVersion{}) {
		return nil
	}
	var current types.WikiPageVersion
	parentID := ""
	if err := tx.Where("page_id = ? AND state = ?", page.ID, types.WikiPageVersionPublished).
		Order("version DESC").First(&current).Error; err == nil {
		parentID = current.ID
		if err := tx.Model(&types.WikiPageVersion{}).Where("id = ?", current.ID).
			Update("state", types.WikiPageVersionHistory).Error; err != nil {
			return err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var maxVersion int
	if err := tx.Model(&types.WikiPageVersion{}).Where("page_id = ?", page.ID).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return err
	}
	if parentID == "" && operation != "create" && len(before) > 0 && before.ToString() != "{}" {
		var previous types.WikiPage
		if err := json.Unmarshal(before, &previous); err != nil {
			return err
		}
		previousVersion := previous.Version
		if previousVersion < 1 {
			previousVersion = 1
		}
		seed := &types.WikiPageVersion{
			ID: uuid.NewString(), TenantID: page.TenantID, KnowledgeBaseID: page.KnowledgeBaseID,
			PageID: page.ID, Version: previousVersion, State: types.WikiPageVersionHistory,
			Snapshot: before, ChangeSummary: "反馈更新前的已发布版本",
			CreatedBy: actor, PublishedBy: actor, CreatedAt: now, PublishedAt: &now,
		}
		if err := tx.Create(seed).Error; err != nil {
			return err
		}
		parentID = seed.ID
		maxVersion = previousVersion
	}
	snapshot, err := json.Marshal(page)
	if err != nil {
		return err
	}
	version := &types.WikiPageVersion{
		ID: uuid.NewString(), TenantID: page.TenantID, KnowledgeBaseID: page.KnowledgeBaseID,
		PageID: page.ID, Version: maxVersion + 1, State: types.WikiPageVersionPublished,
		ParentVersionID: parentID, Snapshot: types.JSON(snapshot), ChangeSummary: summary,
		CreatedBy: actor, PublishedBy: actor, CreatedAt: now, PublishedAt: &now,
	}
	return tx.Create(version).Error
}

type wikiGovernanceRepository struct{ db *gorm.DB }

func NewWikiGovernanceRepository(db *gorm.DB) interfaces.WikiGovernanceRepository {
	return &wikiGovernanceRepository{db: db}
}

func (r *wikiGovernanceRepository) CreateChangeSet(ctx context.Context, set *types.WikiChangeSet) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if set.ReviewLevel == types.WikiReviewLevelL2 || set.ReviewLevel == "" {
			set.ReviewLevel = types.WikiReviewLevelL1
		}
		if set.ChangeCategory == "" {
			set.ChangeCategory = types.WikiChangeCategoryUpdate
		}
		items := set.Items
		set.Items = nil
		if err := tx.Create(set).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].ChangeSetID = set.ID
			if items[i].ID == "" {
				items[i].ID = uuid.NewString()
			}
			if items[i].ChangeCategory == "" {
				items[i].ChangeCategory = set.ChangeCategory
			}
			// PostgreSQL JSONB defaults are not applied when GORM explicitly
			// writes a nil driver.Value. Normalize optional snapshots here so a
			// create item never violates the NOT NULL governance schema.
			if len(items[i].Before) == 0 {
				items[i].Before = types.JSON(`{}`)
			}
			if len(items[i].After) == 0 {
				items[i].After = types.JSON(`{}`)
			}
			if items[i].ChangedFields == nil {
				items[i].ChangedFields = types.StringArray{}
			}
			if items[i].EvidenceChunkIDs == nil {
				items[i].EvidenceChunkIDs = types.StringArray{}
			}
			if len(items[i].EvidenceExcerpts) == 0 {
				items[i].EvidenceExcerpts = types.JSON(`{}`)
			}
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		set.Items = items
		return nil
	})
}

func (r *wikiGovernanceRepository) FindChangeSetByFingerprint(ctx context.Context, kbID, fingerprint string) (*types.WikiChangeSet, error) {
	var set types.WikiChangeSet
	err := r.db.WithContext(ctx).Preload("Items").Where("knowledge_base_id = ? AND candidate_fingerprint = ?", kbID, fingerprint).Order("created_at DESC").First(&set).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &set, err
}

func (r *wikiGovernanceRepository) GetChangeSet(ctx context.Context, kbID, id string) (*types.WikiChangeSet, error) {
	var set types.WikiChangeSet
	err := r.db.WithContext(ctx).Preload("Items").Where("knowledge_base_id = ? AND id = ?", kbID, id).First(&set).Error
	if set.ReviewLevel == types.WikiReviewLevelL2 {
		set.ReviewLevel = types.WikiReviewLevelL1
	}
	return &set, err
}

func (r *wikiGovernanceRepository) ListPendingGraphCards(ctx context.Context, kbID string, limit int) ([]*types.WikiPendingGraphCard, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	var items []types.WikiChangeItem
	err := r.db.WithContext(ctx).Table("wiki_change_items").
		Select("wiki_change_items.*").
		Joins("JOIN wiki_change_sets ON wiki_change_sets.id = wiki_change_items.change_set_id").
		Where("wiki_change_sets.knowledge_base_id = ? AND wiki_change_sets.status = ?", kbID, types.WikiChangeSetPending).
		Where("wiki_change_items.operation IN ?", []string{"create", "update"}).
		Order("wiki_change_items.created_at ASC").Limit(limit).Find(&items).Error
	if err != nil {
		return nil, err
	}
	out := make([]*types.WikiPendingGraphCard, 0, len(items))
	for i := range items {
		page, decodeErr := decodePageSnapshot(items[i].After)
		if decodeErr != nil || page == nil || page.PageType != types.WikiPageTypeCard {
			continue
		}
		out = append(out, &types.WikiPendingGraphCard{ChangeSetID: items[i].ChangeSetID, ChangeItemID: items[i].ID, Page: page})
	}
	return out, nil
}

// UpdatePendingGraphCard updates only graph-owned fields in the latest
// candidate snapshot. Locking the owning change set serializes this write with
// ReviewChangeSet, so an approval can never publish a stale pre-graph snapshot.
func (r *wikiGovernanceRepository) UpdatePendingGraphCard(ctx context.Context, kbID, changeItemID string, page *types.WikiPage) (bool, error) {
	if page == nil {
		return false, errors.New("pending graph card is nil")
	}
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item types.WikiChangeItem
		// Read the owner id first, then lock in the same set -> items order as
		// ReviewChangeSet. Keeping one lock order avoids approval/graph-update
		// deadlocks under a busy review queue.
		if err := tx.Select("id", "change_set_id").Where("id = ?", changeItemID).First(&item).Error; err != nil {
			return err
		}
		var set types.WikiChangeSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND knowledge_base_id = ?", item.ChangeSetID, kbID).First(&set).Error; err != nil {
			return err
		}
		if set.Status != types.WikiChangeSetPending {
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", changeItemID).First(&item).Error; err != nil {
			return err
		}
		current, err := decodePageSnapshot(item.After)
		if err != nil {
			return err
		}
		current.Content = page.Content
		current.OutLinks = page.OutLinks
		current.PageMetadata = page.PageMetadata
		if err := tx.Model(&types.WikiChangeItem{}).Where("id = ?", item.ID).Update("after", pageJSONForRepository(current)).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

func (r *wikiGovernanceRepository) ListChangeSets(ctx context.Context, req types.WikiGovernanceListRequest) ([]*types.WikiChangeSet, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.WikiChangeSet{}).Where("knowledge_base_id = ?", req.KnowledgeBaseID)
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.ReviewLevel != "" {
		if req.ReviewLevel == types.WikiReviewLevelL1 {
			q = q.Where("review_level IN ?", []string{types.WikiReviewLevelL1, types.WikiReviewLevelL2})
		} else {
			q = q.Where("review_level = ?", req.ReviewLevel)
		}
	}
	if req.ChangeCategory != "" {
		q = q.Where("change_category = ?", req.ChangeCategory)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var sets []*types.WikiChangeSet
	err := q.Preload("Items").Order("created_at DESC").Limit(limit).Offset(req.Offset).Find(&sets).Error
	for _, set := range sets {
		if set.ReviewLevel == types.WikiReviewLevelL2 {
			set.ReviewLevel = types.WikiReviewLevelL1
		}
	}
	return sets, total, err
}

func (r *wikiGovernanceRepository) ListPageChangeSets(ctx context.Context, kbID, slug string, limit int) ([]*types.WikiChangeSet, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var sets []*types.WikiChangeSet
	err := r.db.WithContext(ctx).Model(&types.WikiChangeSet{}).Distinct("wiki_change_sets.*").
		Joins("JOIN wiki_change_items ON wiki_change_items.change_set_id = wiki_change_sets.id").
		Where("wiki_change_sets.knowledge_base_id = ? AND wiki_change_items.page_slug = ?", kbID, slug).
		Preload("Items").Order("wiki_change_sets.created_at DESC").Limit(limit).Find(&sets).Error
	return sets, err
}

func decodePageSnapshot(raw types.JSON) (*types.WikiPage, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty page snapshot")
	}
	var page types.WikiPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func validateApprovedCard(page *types.WikiPage) error {
	if page.PageType != types.WikiPageTypeCard {
		return errors.New("governance change item must contain a card page")
	}
	switch page.KnowledgeType {
	case types.WikiKnowledgeTypeKnowledge, types.WikiKnowledgeTypeExperience, types.WikiKnowledgeTypeQuestion,
		types.WikiKnowledgeTypeHypothesis, types.WikiKnowledgeTypeExperiment, types.WikiKnowledgeTypeMetric,
		types.WikiKnowledgeTypeProcedure, types.WikiKnowledgeTypeFailure, types.WikiKnowledgeTypeCase, types.WikiKnowledgeTypeRule:
	default:
		return errors.New("invalid knowledge_type in approved card")
	}
	switch page.MaturityStatus {
	case "", types.WikiMaturityDraft, types.WikiMaturityPendingReview, types.WikiMaturityVerified,
		types.WikiMaturityPartiallyVerified, types.WikiMaturityDisputed, types.WikiMaturityOutdated,
		types.WikiMaturityUnsupported, types.WikiMaturityRejected, types.WikiMaturityArchived:
	default:
		return errors.New("invalid maturity_status in approved card")
	}
	if len(page.ChunkRefs) == 0 && (page.MaturityStatus == types.WikiMaturityVerified || page.MaturityStatus == types.WikiMaturityPartiallyVerified) {
		return errors.New("a card without chunk evidence cannot be verified")
	}
	if page.AnswerStrength == types.WikiAnswerStrengthStrong && (page.KnowledgeType == types.WikiKnowledgeTypeQuestion || page.KnowledgeType == types.WikiKnowledgeTypeHypothesis || page.KnowledgeType == types.WikiKnowledgeTypeExperiment) {
		return errors.New("question, hypothesis, or experiment cards cannot have strong answer strength")
	}
	if page.EffectiveFrom != nil && page.EffectiveTo != nil && !page.EffectiveTo.After(*page.EffectiveFrom) {
		return errors.New("effective_to must be later than effective_from")
	}
	return nil
}

func ensureApprovedPageScenarios(tx *gorm.DB, page *types.WikiPage, now time.Time) error {
	metadata, err := page.PageMetadata.Map()
	if err != nil {
		return err
	}
	rawNames, _ := metadata["scenario_names"].([]any)
	for _, raw := range rawNames {
		name, ok := raw.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		var scenario types.WikiScenario
		attrs := types.WikiScenario{ID: uuid.NewString(), TenantID: page.TenantID, KnowledgeBaseID: page.KnowledgeBaseID, Name: name, BusinessLine: page.BusinessLine, TargetRoles: page.AudienceRoles, AffectedMetrics: page.AffectedMetrics, Priority: "medium", CreatedAt: now, UpdatedAt: now}
		if err := tx.Where("knowledge_base_id = ? AND business_line = ? AND name = ?", page.KnowledgeBaseID, page.BusinessLine, name).Attrs(attrs).FirstOrCreate(&scenario).Error; err != nil {
			return err
		}
		page.ScenarioIDs = appendUniqueWikiString(page.ScenarioIDs, scenario.ID)
	}
	return nil
}

func appendUniqueWikiString(values types.StringArray, value string) types.StringArray {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if value != "" {
		return append(values, value)
	}
	return values
}

func mergeWikiStringArrays(first, second types.StringArray) types.StringArray {
	out := append(types.StringArray(nil), first...)
	for _, value := range second {
		out = appendUniqueWikiString(out, value)
	}
	return out
}

func (r *wikiGovernanceRepository) mergeChangeSetIntoPage(tx *gorm.DB, set *types.WikiChangeSet, items []types.WikiChangeItem, overrides map[string]types.JSON, kbID, targetSlug, reviewerID string, now time.Time) error {
	if len(items) != 1 || items[0].Operation != "create" {
		return errors.New("merge is only supported for a single new-card candidate")
	}
	after := items[0].After
	if override, ok := overrides[items[0].ID]; ok {
		var err error
		after, err = mergeOverride(after, override)
		if err != nil {
			return err
		}
	}
	candidate, err := decodePageSnapshot(after)
	if err != nil {
		return err
	}
	if err := ensureApprovedPageScenarios(tx, candidate, now); err != nil {
		return err
	}
	var target types.WikiPage
	if err := tx.Where("knowledge_base_id = ? AND slug = ? AND page_type = ? AND review_status = ?", kbID, targetSlug, types.WikiPageTypeCard, types.WikiReviewApproved).First(&target).Error; err != nil {
		return err
	}
	if target.KnowledgeType != candidate.KnowledgeType {
		return errors.New("candidate and merge target must have the same knowledge_type")
	}
	before := pageJSONForRepository(&target)
	target.SourceRefs = mergeWikiStringArrays(target.SourceRefs, candidate.SourceRefs)
	target.ChunkRefs = mergeWikiStringArrays(target.ChunkRefs, candidate.ChunkRefs)
	target.ScenarioIDs = mergeWikiStringArrays(target.ScenarioIDs, candidate.ScenarioIDs)
	target.AudienceRoles = mergeWikiStringArrays(target.AudienceRoles, candidate.AudienceRoles)
	target.AffectedMetrics = mergeWikiStringArrays(target.AffectedMetrics, candidate.AffectedMetrics)
	target.Aliases = mergeWikiStringArrays(target.Aliases, types.StringArray{candidate.Title})
	target.ReviewStatus, target.ReviewedBy, target.ReviewedAt = types.WikiReviewApproved, reviewerID, &now
	expectedVersion := target.Version
	target.Version++
	target.UpdatedAt = now
	result := tx.Model(&types.WikiPage{}).Where("id = ? AND knowledge_base_id = ? AND version = ?", target.ID, kbID, expectedVersion).Updates(map[string]any{"source_refs": target.SourceRefs, "chunk_refs": target.ChunkRefs, "scenario_ids": target.ScenarioIDs, "audience_roles": target.AudienceRoles, "affected_metrics": target.AffectedMetrics, "aliases": target.Aliases, "review_status": target.ReviewStatus, "reviewed_by": reviewerID, "reviewed_at": now, "version": target.Version, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWikiChangeSetConflict
	}
	if err := syncGovernedPagePackages(tx, &target, now); err != nil {
		return err
	}
	changed := types.StringArray{"source_refs", "chunk_refs", "scenario_ids", "audience_roles", "affected_metrics", "aliases"}
	if err := tx.Model(&types.WikiChangeItem{}).Where("id = ?", items[0].ID).Updates(map[string]any{"operation": "update", "page_id": target.ID, "page_slug": target.Slug, "expected_version": expectedVersion, "before": before, "after": pageJSONForRepository(&target), "changed_fields": changed}).Error; err != nil {
		return err
	}
	return tx.Model(set).Updates(map[string]any{"status": types.WikiChangeSetApplied, "reviewed_by": reviewerID, "review_comment": "Merged candidate into " + targetSlug, "reviewed_at": now, "updated_at": now}).Error
}

func syncGovernedPagePackages(tx *gorm.DB, page *types.WikiPage, now time.Time) error {
	if page.ID == "" {
		return errors.New("cannot package a card without page id")
	}
	var managedIDs []string
	if err := tx.Model(&types.WikiPackage{}).Where("knowledge_base_id = ? AND package_type IN ?", page.KnowledgeBaseID, []string{"global", "business_line", "scenario"}).Pluck("id", &managedIDs).Error; err != nil {
		return err
	}
	if len(managedIDs) > 0 {
		if err := tx.Where("page_id = ? AND package_id IN ?", page.ID, managedIDs).Delete(&types.WikiPackagePage{}).Error; err != nil {
			return err
		}
	}
	if page.Status == types.WikiPageStatusArchived {
		return nil
	}
	type packageSpec struct{ Name, Type, BusinessLine string }
	specs := []packageSpec{{Name: "Global", Type: "global"}}
	if page.BusinessLine != "" {
		specs = append(specs, packageSpec{Name: page.BusinessLine, Type: "business_line", BusinessLine: page.BusinessLine})
	}
	if len(page.ScenarioIDs) > 0 {
		var scenarios []types.WikiScenario
		if err := tx.Where("knowledge_base_id = ? AND id IN ?", page.KnowledgeBaseID, []string(page.ScenarioIDs)).Find(&scenarios).Error; err != nil {
			return err
		}
		for _, scenario := range scenarios {
			specs = append(specs, packageSpec{Name: scenario.Name, Type: "scenario", BusinessLine: scenario.BusinessLine})
		}
	}
	for _, spec := range specs {
		var pkg types.WikiPackage
		attrs := types.WikiPackage{ID: uuid.NewString(), TenantID: page.TenantID, KnowledgeBaseID: page.KnowledgeBaseID, Name: spec.Name, PackageType: spec.Type, BusinessLine: spec.BusinessLine, LoadingPolicy: "on_demand", CreatedAt: now, UpdatedAt: now}
		if err := tx.Where("knowledge_base_id = ? AND package_type = ? AND business_line = ? AND name = ?", page.KnowledgeBaseID, spec.Type, spec.BusinessLine, spec.Name).Attrs(attrs).FirstOrCreate(&pkg).Error; err != nil {
			return err
		}
		link := types.WikiPackagePage{PackageID: pkg.ID, PageID: page.ID, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergeOverride(raw types.JSON, override types.JSON) (types.JSON, error) {
	if len(override) == 0 {
		return raw, nil
	}
	base := map[string]any{}
	patch := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &base); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(override, &patch); err != nil {
		return nil, err
	}
	for k, v := range patch {
		base[k] = v
	}
	b, err := json.Marshal(base)
	return types.JSON(b), err
}

type governedConflictAssessment struct {
	RelatedSlug          string  `json:"related_slug"`
	Relation             string  `json:"relation"`
	CandidateClaim       string  `json:"candidate_claim"`
	ExistingClaim        string  `json:"existing_claim"`
	Reason               string  `json:"reason"`
	Confidence           float64 `json:"confidence"`
	ApplicabilityOverlap bool    `json:"applicability_overlap"`
}

func conflictAssessmentsFromPage(page *types.WikiPage) []governedConflictAssessment {
	if page == nil || len(page.PageMetadata) == 0 {
		return nil
	}
	var metadata struct {
		Assessments []governedConflictAssessment `json:"cross_page_assessments"`
	}
	if json.Unmarshal(page.PageMetadata, &metadata) != nil {
		return nil
	}
	return metadata.Assessments
}

func actionableConflictAssessments(page *types.WikiPage) []governedConflictAssessment {
	out := make([]governedConflictAssessment, 0)
	for _, assessment := range conflictAssessmentsFromPage(page) {
		if assessment.Relation == "conflicting" || assessment.Relation == "supersedes" {
			out = append(out, assessment)
		}
	}
	return out
}

func conflictChoiceKey(itemID string, assessmentIndex int) string {
	return fmt.Sprintf("%s:%d", itemID, assessmentIndex)
}

func validatedConflictChoices(items []types.WikiChangeItem, decision *types.WikiReviewDecision) (map[string]types.WikiConflictChoice, error) {
	expected := map[string]bool{}
	for _, item := range items {
		page, err := decodePageSnapshot(item.After)
		if err != nil {
			return nil, err
		}
		for index := range actionableConflictAssessments(page) {
			expected[conflictChoiceKey(item.ID, index)] = true
		}
	}
	if len(expected) == 0 {
		return nil, errors.New("conflict change set has no actionable conflict positions")
	}
	choices := map[string]types.WikiConflictChoice{}
	for _, choice := range decision.ConflictChoices {
		key := conflictChoiceKey(choice.ItemID, choice.AssessmentIndex)
		if !expected[key] {
			return nil, fmt.Errorf("unknown conflict position %s", key)
		}
		if _, duplicate := choices[key]; duplicate {
			return nil, fmt.Errorf("duplicate conflict choice for %s", key)
		}
		if choice.Resolution != types.WikiConflictKeepExisting && choice.Resolution != types.WikiConflictAdoptCandidate {
			return nil, fmt.Errorf("invalid per-conflict resolution %q", choice.Resolution)
		}
		choices[key] = choice
	}
	if len(choices) != len(expected) {
		return nil, fmt.Errorf("every conflict position must be selected: got %d of %d", len(choices), len(expected))
	}
	return choices, nil
}

func validateConflictResolution(set *types.WikiChangeSet, items []types.WikiChangeItem, decision *types.WikiReviewDecision) error {
	if set.ChangeCategory != types.WikiChangeCategoryConflict {
		return nil
	}
	switch decision.Resolution {
	case types.WikiConflictPerClaim:
		choices, err := validatedConflictChoices(items, decision)
		if err != nil {
			return err
		}
		anyCandidate := false
		for _, choice := range choices {
			anyCandidate = anyCandidate || choice.Resolution == types.WikiConflictAdoptCandidate
		}
		if anyCandidate && decision.Decision != types.WikiReviewApproved {
			return errors.New("selecting any candidate claim requires an approved decision")
		}
		if !anyCandidate && decision.Decision != types.WikiReviewRejected {
			return errors.New("keeping every existing claim requires a rejected decision")
		}
	case types.WikiConflictKeepExisting:
		if decision.Decision != types.WikiReviewRejected {
			return errors.New("keep_existing requires a rejected decision")
		}
	case types.WikiConflictAdoptCandidate, types.WikiConflictEditCandidate, types.WikiConflictSplitScope:
		if decision.Decision != types.WikiReviewApproved {
			return errors.New("the selected conflict resolution requires an approved decision")
		}
		if decision.Resolution == types.WikiConflictSplitScope && len(decision.ItemOverrides) == 0 {
			return errors.New("split_scope requires an applicability override")
		}
		if decision.Resolution == types.WikiConflictEditCandidate && len(decision.ItemOverrides) == 0 {
			return errors.New("edit_candidate requires a content override")
		}
	case types.WikiConflictDefer:
		if decision.Decision != types.WikiReviewDeferred {
			return errors.New("defer resolution requires a deferred decision")
		}
	default:
		return errors.New("a conflict resolution is required")
	}
	return nil
}

func conflictAuditClaims(items []types.WikiChangeItem, decision *types.WikiReviewDecision) (string, types.JSON) {
	retained := strings.TrimSpace(decision.RetainedClaim)
	discarded := make([]map[string]any, 0)
	choices, _ := validatedConflictChoices(items, decision)
	retainedClaims := make([]string, 0)
	for _, item := range items {
		if decision.Resolution == types.WikiCorrectionAutoApplied {
			before, err := decodePageSnapshot(item.Before)
			if err == nil {
				claim := strings.TrimSpace(before.Summary)
				if claim == "" {
					claim = strings.TrimSpace(before.Content)
				}
				if claim != "" {
					discarded = append(discarded, map[string]any{"claim": claim, "source": before.Slug, "reason": "replaced_by_authoritative_explicit_correction"})
				}
			}
		}
		page, err := decodePageSnapshot(item.After)
		if err != nil {
			continue
		}
		for index, assessment := range actionableConflictAssessments(page) {
			resolution := decision.Resolution
			if decision.Resolution == types.WikiConflictPerClaim {
				resolution = choices[conflictChoiceKey(item.ID, index)].Resolution
			}
			switch resolution {
			case types.WikiConflictKeepExisting:
				retainedClaims = append(retainedClaims, assessment.ExistingClaim)
				discarded = append(discarded, map[string]any{"claim": assessment.CandidateClaim, "source": item.PageSlug, "reason": assessment.Reason})
			case types.WikiConflictAdoptCandidate, types.WikiConflictEditCandidate:
				retainedClaims = append(retainedClaims, assessment.CandidateClaim)
				discarded = append(discarded, map[string]any{"claim": assessment.ExistingClaim, "source": assessment.RelatedSlug, "reason": assessment.Reason})
			case types.WikiCorrectionAutoApplied:
				if retained == "" {
					retained = assessment.CandidateClaim
				}
				discarded = append(discarded, map[string]any{"claim": assessment.ExistingClaim, "source": assessment.RelatedSlug, "reason": assessment.Reason})
			}
		}
	}
	if retained == "" {
		retained = strings.Join(retainedClaims, "\n")
	}
	raw, _ := json.Marshal(discarded)
	return retained, types.JSON(raw)
}

func conflictChoiceAudit(items []types.WikiChangeItem, decision *types.WikiReviewDecision) types.JSON {
	if decision.Resolution != types.WikiConflictPerClaim {
		return types.JSON(`[]`)
	}
	choices, err := validatedConflictChoices(items, decision)
	if err != nil {
		return types.JSON(`[]`)
	}
	audit := make([]map[string]any, 0, len(choices))
	for _, item := range items {
		page, decodeErr := decodePageSnapshot(item.After)
		if decodeErr != nil {
			continue
		}
		for index, assessment := range actionableConflictAssessments(page) {
			choice := choices[conflictChoiceKey(item.ID, index)]
			audit = append(audit, map[string]any{
				"item_id": item.ID, "assessment_index": index, "related_slug": assessment.RelatedSlug,
				"resolution": choice.Resolution, "candidate_claim": assessment.CandidateClaim,
				"existing_claim": assessment.ExistingClaim, "reason": assessment.Reason,
			})
		}
	}
	raw, _ := json.Marshal(audit)
	return types.JSON(raw)
}

func appendPageMetadata(page *types.WikiPage, values map[string]any) error {
	metadata, err := page.PageMetadata.Map()
	if err != nil {
		return err
	}
	for key, value := range values {
		metadata[key] = value
	}
	raw, err := json.Marshal(metadata)
	page.PageMetadata = types.JSON(raw)
	return err
}

func replaceReviewedClaim(page *types.WikiPage, from, to string) bool {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return from == to && from != ""
	}
	replaced := false
	if strings.Contains(page.Content, from) {
		page.Content = strings.ReplaceAll(page.Content, from, to)
		replaced = true
	}
	if strings.Contains(page.Summary, from) {
		page.Summary = strings.ReplaceAll(page.Summary, from, to)
		replaced = true
	}
	return replaced
}

// applyPerConflictChoices materializes a mixed human decision. Existing claims
// selected by the reviewer replace their candidate counterparts in the
// candidate page. Candidate claims selected by the reviewer replace only the
// conflicting snippets in related pages, preserving unrelated page content.
// Every touched related page is added to the same ChangeSet so normal version
// checks, snapshots, history, and rollback continue to apply atomically.
func applyPerConflictChoices(tx *gorm.DB, set *types.WikiChangeSet, items []types.WikiChangeItem, decision *types.WikiReviewDecision, now time.Time) ([]types.WikiChangeItem, error) {
	if set.ChangeCategory != types.WikiChangeCategoryConflict || decision.Resolution != types.WikiConflictPerClaim {
		return items, nil
	}
	choices, err := validatedConflictChoices(items, decision)
	if err != nil {
		return nil, err
	}
	type selectedAssessment struct {
		assessment governedConflictAssessment
		itemSlug   string
		resolution string
	}
	adoptBySlug := map[string][]selectedAssessment{}
	for itemIndex := range items {
		page, decodeErr := decodePageSnapshot(items[itemIndex].After)
		if decodeErr != nil {
			return nil, decodeErr
		}
		audit := make([]map[string]any, 0)
		for assessmentIndex, assessment := range actionableConflictAssessments(page) {
			choice := choices[conflictChoiceKey(items[itemIndex].ID, assessmentIndex)]
			audit = append(audit, map[string]any{"assessment_index": assessmentIndex, "related_slug": assessment.RelatedSlug, "resolution": choice.Resolution})
			if choice.Resolution == types.WikiConflictKeepExisting {
				if !replaceReviewedClaim(page, assessment.CandidateClaim, assessment.ExistingClaim) {
					return nil, fmt.Errorf("candidate claim %q could not be located for conflict %d", assessment.CandidateClaim, assessmentIndex+1)
				}
			} else if assessment.RelatedSlug != "" && assessment.RelatedSlug != page.Slug {
				adoptBySlug[assessment.RelatedSlug] = append(adoptBySlug[assessment.RelatedSlug], selectedAssessment{assessment: assessment, itemSlug: items[itemIndex].PageSlug, resolution: choice.Resolution})
			}
		}
		if err := appendPageMetadata(page, map[string]any{"reviewed_conflict_choices": audit, "conflict_resolution": types.WikiConflictPerClaim}); err != nil {
			return nil, err
		}
		items[itemIndex].After = pageJSONForRepository(page)
		if err := tx.Model(&types.WikiChangeItem{}).Where("id = ?", items[itemIndex].ID).Update("after", items[itemIndex].After).Error; err != nil {
			return nil, err
		}
	}

	for relatedSlug, selected := range adoptBySlug {
		var existing types.WikiPage
		if err := tx.Where("knowledge_base_id = ? AND slug = ? AND status <> ?", set.KnowledgeBaseID, relatedSlug, types.WikiPageStatusArchived).First(&existing).Error; err != nil {
			return nil, err
		}
		before := pageJSONForRepository(&existing)
		audit := make([]map[string]any, 0, len(selected))
		for _, selectedClaim := range selected {
			assessment := selectedClaim.assessment
			if !replaceReviewedClaim(&existing, assessment.ExistingClaim, assessment.CandidateClaim) {
				return nil, fmt.Errorf("existing claim %q could not be located in %s", assessment.ExistingClaim, relatedSlug)
			}
			audit = append(audit, map[string]any{"old_claim": assessment.ExistingClaim, "retained_claim": assessment.CandidateClaim, "reason": assessment.Reason, "source_candidate": selectedClaim.itemSlug})
		}
		if err := appendPageMetadata(&existing, map[string]any{"reviewed_claim_replacements": audit, "corrected_by_change_set_id": set.ID, "corrected_at": now}); err != nil {
			return nil, err
		}
		item := types.WikiChangeItem{
			ID: uuid.NewString(), ChangeSetID: set.ID, Operation: "update", ChangeCategory: types.WikiChangeCategoryCorrection,
			PageID: existing.ID, PageSlug: existing.Slug, ExpectedVersion: existing.Version, Before: before, After: pageJSONForRepository(&existing),
			ChangedFields: types.StringArray{"content", "summary", "page_metadata"}, EvidenceChunkIDs: existing.ChunkRefs,
			EvidenceExcerpts: types.JSON(`{}`), CreatedAt: now,
		}
		if err := tx.Create(&item).Error; err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// appendConflictRetirementItems turns every overlapping page displaced by an
// approved candidate into an explicit archive item. This keeps the old page
// snapshot in the same ChangeSet, lets normal history/rollback APIs find it,
// and prevents the obsolete claim from remaining visible to ordinary RAG.
func appendConflictRetirementItems(tx *gorm.DB, set *types.WikiChangeSet, items []types.WikiChangeItem, decision *types.WikiReviewDecision, reviewerID string, now time.Time) ([]types.WikiChangeItem, error) {
	manualReplacement := set.ChangeCategory == types.WikiChangeCategoryConflict && (decision.Resolution == types.WikiConflictAdoptCandidate || decision.Resolution == types.WikiConflictEditCandidate)
	automaticCorrection := set.ChangeCategory == types.WikiChangeCategoryCorrection && decision.Resolution == types.WikiCorrectionAutoApplied
	if !manualReplacement && !automaticCorrection {
		return items, nil
	}
	if len(items) == 0 {
		return nil, errors.New("conflict change set has no candidate item")
	}
	originalItemCount := len(items)
	candidateRaw := items[0].After
	if override, ok := decision.ItemOverrides[items[0].ID]; ok {
		var err error
		candidateRaw, err = mergeOverride(candidateRaw, override)
		if err != nil {
			return nil, err
		}
	}
	candidate, err := decodePageSnapshot(candidateRaw)
	if err != nil {
		return nil, err
	}
	assessments := conflictAssessmentsFromPage(candidate)
	superseded := make([]string, 0)
	seen := map[string]bool{}
	for _, assessment := range assessments {
		if seen[assessment.RelatedSlug] || assessment.RelatedSlug == "" || assessment.RelatedSlug == candidate.Slug {
			continue
		}
		if assessment.Relation != "conflicting" && assessment.Relation != "supersedes" {
			continue
		}
		if assessment.Relation == "conflicting" && (!assessment.ApplicabilityOverlap || assessment.Confidence < 0.75) {
			continue
		}
		var old types.WikiPage
		if err := tx.Where("knowledge_base_id = ? AND slug = ? AND status <> ?", set.KnowledgeBaseID, assessment.RelatedSlug, types.WikiPageStatusArchived).First(&old).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		seen[old.Slug] = true
		before := pageJSONForRepository(&old)
		old.Status = types.WikiPageStatusArchived
		old.MaturityStatus = types.WikiMaturityOutdated
		old.ReviewStatus = types.WikiReviewApproved
		old.ReviewedBy, old.ReviewedAt, old.EffectiveTo = reviewerID, &now, &now
		old.OutLinks = appendUniqueWikiString(old.OutLinks, candidate.Slug)
		if err := appendPageMetadata(&old, map[string]any{"superseded_by": candidate.Slug, "superseded_change_set_id": set.ID, "discarded_claim": assessment.ExistingClaim, "discarded_reason": assessment.Reason}); err != nil {
			return nil, err
		}
		items = append(items, types.WikiChangeItem{
			ID: uuid.NewString(), ChangeSetID: set.ID, Operation: "archive", ChangeCategory: types.WikiChangeCategoryRetirement,
			PageID: old.ID, PageSlug: old.Slug, ExpectedVersion: old.Version, Before: before, After: pageJSONForRepository(&old),
			ChangedFields: types.StringArray{"status", "maturity_status", "effective_to", "page_metadata", "out_links"}, EvidenceChunkIDs: old.ChunkRefs,
			EvidenceExcerpts: types.JSON(`{}`), CreatedAt: now,
		})
		superseded = append(superseded, old.Slug)
	}
	if len(superseded) == 0 {
		return items, nil
	}
	candidate.OutLinks = mergeWikiStringArrays(candidate.OutLinks, types.StringArray(superseded))
	if err := appendPageMetadata(candidate, map[string]any{"supersedes": superseded, "conflict_resolution": decision.Resolution}); err != nil {
		return nil, err
	}
	items[0].After = pageJSONForRepository(candidate)
	if err := tx.Model(&types.WikiChangeItem{}).Where("id = ?", items[0].ID).Update("after", items[0].After).Error; err != nil {
		return nil, err
	}
	for i := originalItemCount; i < len(items); i++ {
		if err := tx.Create(&items[i]).Error; err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *wikiGovernanceRepository) ReviewChangeSet(ctx context.Context, kbID, id, reviewerID string, decision *types.WikiReviewDecision) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var set types.WikiChangeSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("knowledge_base_id = ? AND id = ?", kbID, id).First(&set).Error; err != nil {
			return err
		}
		if set.Status != types.WikiChangeSetPending {
			return fmt.Errorf("change set is already %s", set.Status)
		}
		now := time.Now()
		var items []types.WikiChangeItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("change_set_id = ?", id).Find(&items).Error; err != nil {
			return err
		}
		if err := validateConflictResolution(&set, items, decision); err != nil {
			return err
		}
		retainedClaim, discardedClaims := conflictAuditClaims(items, decision)
		conflictChoices := conflictChoiceAudit(items, decision)
		overrideBytes, _ := json.Marshal(decision.ItemOverrides)
		review := types.WikiReview{ID: uuid.NewString(), ChangeSetID: id, ReviewerID: reviewerID, Decision: decision.Decision, Comment: decision.Comment, ItemOverrides: types.JSON(overrideBytes), Resolution: decision.Resolution, RetainedClaim: retainedClaim, DiscardedClaims: discardedClaims, ConflictChoices: conflictChoices, CreatedAt: now}
		if err := tx.Create(&review).Error; err != nil {
			return err
		}
		if decision.Decision == types.WikiReviewDeferred {
			return nil
		}
		if decision.Decision == types.WikiReviewRejected {
			if err := tx.Model(&set).Updates(map[string]any{"status": types.WikiChangeSetRejected, "reviewed_by": reviewerID, "review_comment": decision.Comment, "reviewed_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			return updateFeedbackSignalForChangeSet(tx, id, map[string]any{"status": types.WikiFeedbackRejected, "reviewed_by": reviewerID, "updated_at": now})
		}
		if decision.Decision != types.WikiReviewApproved {
			return errors.New("decision must be approved, rejected, or deferred")
		}
		if strings.TrimSpace(decision.MergeIntoSlug) != "" {
			return r.mergeChangeSetIntoPage(tx, &set, items, decision.ItemOverrides, kbID, strings.TrimSpace(decision.MergeIntoSlug), reviewerID, now)
		}
		var err error
		items, err = applyPerConflictChoices(tx, &set, items, decision, now)
		if err != nil {
			return err
		}
		items, err = appendConflictRetirementItems(tx, &set, items, decision, reviewerID, now)
		if err != nil {
			return err
		}
		for _, item := range items {
			after := item.After
			if ov, ok := decision.ItemOverrides[item.ID]; ok {
				var err error
				after, err = mergeOverride(after, ov)
				if err != nil {
					return err
				}
			}
			page, err := decodePageSnapshot(after)
			if err != nil {
				return err
			}
			page.ReviewStatus = types.WikiReviewApproved
			page.ReviewedBy = reviewerID
			page.ReviewedAt = &now
			if page.Status == "" || page.Status == types.WikiPageStatusDraft {
				page.Status = types.WikiPageStatusPublished
			}
			if page.MaturityStatus == "" || page.MaturityStatus == types.WikiMaturityPendingReview {
				if len(page.ChunkRefs) == 0 {
					page.MaturityStatus = types.WikiMaturityDraft
				} else {
					page.MaturityStatus = types.WikiMaturityVerified
				}
			}
			if item.Operation == "create" {
				if page.ID == "" {
					page.ID = uuid.NewString()
				}
				if page.Version == 0 {
					page.Version = 1
				}
				page.CreatedAt, page.UpdatedAt = now, now
			} else {
				page.Version = item.ExpectedVersion + 1
				page.UpdatedAt = now
			}
			if page.PageType == types.WikiPageTypeCard {
				if err := ensureApprovedPageScenarios(tx, page, now); err != nil {
					return err
				}
				if err := validateApprovedCard(page); err != nil {
					return err
				}
			}
			if err := tx.Model(&types.WikiChangeItem{}).Where("id = ?", item.ID).Update("after", pageJSONForRepository(page)).Error; err != nil {
				return err
			}
			switch item.Operation {
			case "create":
				if err := tx.Create(page).Error; err != nil {
					return err
				}
			case "update":
				result := tx.Model(&types.WikiPage{}).Where("id = ? AND knowledge_base_id = ? AND version = ?", page.ID, kbID, item.ExpectedVersion).Updates(map[string]any{
					"title": page.Title, "content": page.Content, "summary": page.Summary, "page_type": page.PageType,
					"knowledge_type": page.KnowledgeType, "maturity_status": page.MaturityStatus, "answer_strength": page.AnswerStrength,
					"review_status": page.ReviewStatus, "business_line": page.BusinessLine, "scenario_ids": page.ScenarioIDs,
					"audience_roles": page.AudienceRoles, "affected_metrics": page.AffectedMetrics, "applicability": page.Applicability,
					"prohibited_claims": page.ProhibitedClaims, "source_refs": page.SourceRefs, "chunk_refs": page.ChunkRefs,
					"aliases": page.Aliases, "page_metadata": page.PageMetadata,
					"out_links":   page.OutLinks,
					"reviewed_by": reviewerID, "reviewed_at": now, "effective_from": page.EffectiveFrom, "effective_to": page.EffectiveTo,
					"status":  page.Status,
					"version": item.ExpectedVersion + 1, "updated_at": now,
				})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return ErrWikiChangeSetConflict
				}
			case "archive":
				result := tx.Model(&types.WikiPage{}).Where("id = ? AND knowledge_base_id = ? AND version = ?", page.ID, kbID, item.ExpectedVersion).Updates(map[string]any{
					"status": types.WikiPageStatusArchived, "maturity_status": page.MaturityStatus, "review_status": types.WikiReviewApproved,
					"reviewed_by": reviewerID, "reviewed_at": now, "effective_to": page.EffectiveTo,
					"page_metadata": page.PageMetadata, "out_links": page.OutLinks,
					"version": item.ExpectedVersion + 1, "updated_at": now,
				})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return ErrWikiChangeSetConflict
				}
			default:
				return fmt.Errorf("unsupported change operation %q", item.Operation)
			}
			if page.PageType == types.WikiPageTypeCard {
				if err := syncGovernedPagePackages(tx, page, now); err != nil {
					return err
				}
			}
			if err := recordGovernancePublishedVersion(tx, page, item.Before, item.Operation, reviewerID, "审核通过："+set.ChangeCategory, now); err != nil {
				return err
			}
		}
		if err := tx.Model(&set).Updates(map[string]any{"status": types.WikiChangeSetApplied, "reviewed_by": reviewerID, "review_comment": decision.Comment, "reviewed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		return updateFeedbackSignalForChangeSet(tx, id, map[string]any{"status": types.WikiFeedbackPublished, "reviewed_by": reviewerID, "updated_at": now})
	})
	if errors.Is(err, ErrWikiChangeSetConflict) {
		now := time.Now()
		_ = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if updateErr := tx.Model(&types.WikiChangeSet{}).Where("knowledge_base_id = ? AND id = ? AND status = ?", kbID, id, types.WikiChangeSetPending).Updates(map[string]any{"status": types.WikiChangeSetConflict, "reviewed_by": reviewerID, "review_comment": "Page version changed during review", "reviewed_at": now, "updated_at": now}).Error; updateErr != nil {
				return updateErr
			}
			if createErr := tx.Create(&types.WikiReview{ID: uuid.NewString(), ChangeSetID: id, ReviewerID: reviewerID, Decision: types.WikiChangeSetConflict, Comment: "Page version changed during review", DiscardedClaims: types.JSON(`[]`), ConflictChoices: types.JSON(`[]`), CreatedAt: now}).Error; createErr != nil {
				return createErr
			}
			return updateFeedbackSignalForChangeSet(tx, id, map[string]any{"status": types.WikiFeedbackConflict, "conflict_summary": "Page version changed during review", "updated_at": now})
		})
	}
	return err
}

func (r *wikiGovernanceRepository) ListReviews(ctx context.Context, changeSetID string) ([]*types.WikiReview, error) {
	var out []*types.WikiReview
	err := r.db.WithContext(ctx).Where("change_set_id = ?", changeSetID).Order("created_at ASC").Find(&out).Error
	return out, err
}
func (r *wikiGovernanceRepository) ListPackages(ctx context.Context, kbID string) ([]*types.WikiPackage, error) {
	var out []*types.WikiPackage
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ?", kbID).Order("name ASC").Find(&out).Error
	return out, err
}
func (r *wikiGovernanceRepository) CreatePackage(ctx context.Context, p *types.WikiPackage) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(p).Error
}
func (r *wikiGovernanceRepository) ListPackagePages(ctx context.Context, kbID, packageID string) ([]*types.WikiPage, error) {
	var pages []*types.WikiPage
	now := time.Now()
	err := r.db.WithContext(ctx).Table("wiki_pages").
		Joins("JOIN wiki_package_pages ON wiki_package_pages.page_id = wiki_pages.id").
		Joins("JOIN wiki_packages ON wiki_packages.id = wiki_package_pages.package_id").
		Where("wiki_packages.id = ? AND wiki_packages.knowledge_base_id = ? AND wiki_pages.knowledge_base_id = ?", packageID, kbID, kbID).
		Where("wiki_pages.status <> ?", types.WikiPageStatusArchived).
		Where("wiki_pages.page_type <> ? OR (wiki_pages.review_status = ? AND wiki_pages.maturity_status IN ? AND (wiki_pages.effective_from IS NULL OR wiki_pages.effective_from <= ?) AND (wiki_pages.effective_to IS NULL OR wiki_pages.effective_to > ?))", types.WikiPageTypeCard, types.WikiReviewApproved, []string{types.WikiMaturityVerified, types.WikiMaturityPartiallyVerified}, now, now).
		Order("wiki_pages.title ASC").Find(&pages).Error
	return pages, err
}
func (r *wikiGovernanceRepository) SetPackagePages(ctx context.Context, kbID, packageID string, pageIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&types.WikiPackage{}).Where("id = ? AND knowledge_base_id = ?", packageID, kbID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		if len(pageIDs) > 0 {
			var pageCount int64
			if err := tx.Model(&types.WikiPage{}).Where("knowledge_base_id = ? AND id IN ?", kbID, pageIDs).Count(&pageCount).Error; err != nil {
				return err
			}
			if pageCount != int64(len(pageIDs)) {
				return errors.New("one or more pages do not belong to the knowledge base")
			}
		}
		if err := tx.Where("package_id = ?", packageID).Delete(&types.WikiPackagePage{}).Error; err != nil {
			return err
		}
		now := time.Now()
		links := make([]types.WikiPackagePage, 0, len(pageIDs))
		seen := map[string]bool{}
		for _, pageID := range pageIDs {
			if pageID != "" && !seen[pageID] {
				seen[pageID] = true
				links = append(links, types.WikiPackagePage{PackageID: packageID, PageID: pageID, CreatedAt: now})
			}
		}
		if len(links) > 0 {
			return tx.Create(&links).Error
		}
		return nil
	})
}
func (r *wikiGovernanceRepository) ListScenarios(ctx context.Context, kbID string) ([]*types.WikiScenario, error) {
	var out []*types.WikiScenario
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ?", kbID).Order("priority DESC, name ASC").Find(&out).Error
	return out, err
}
func (r *wikiGovernanceRepository) CreateScenario(ctx context.Context, s *types.WikiScenario) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(s).Error
}
func (r *wikiGovernanceRepository) ListScenarioPages(ctx context.Context, kbID, scenarioID string) ([]*types.WikiPage, error) {
	var scenario types.WikiScenario
	if err := r.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ?", scenarioID, kbID).First(&scenario).Error; err != nil {
		return nil, err
	}
	needle, _ := json.Marshal([]string{scenarioID})
	var pages []*types.WikiPage
	now := time.Now()
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ? AND page_type = ? AND scenario_ids @> ?::jsonb", kbID, types.WikiPageTypeCard, string(needle)).
		Where("review_status = ? AND maturity_status IN ? AND status <> ? AND (effective_from IS NULL OR effective_from <= ?) AND (effective_to IS NULL OR effective_to > ?)", types.WikiReviewApproved, []string{types.WikiMaturityVerified, types.WikiMaturityPartiallyVerified}, types.WikiPageStatusArchived, now, now).
		Order("title ASC").Find(&pages).Error
	return pages, err
}

func groupedCounts(db *gorm.DB, column string) (map[string]int64, error) {
	var rows []struct {
		Key   string
		Count int64
	}
	if err := db.Select(column + " AS key, COUNT(*) AS count").Group(column).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.Key] = row.Count
	}
	return out, nil
}

func (r *wikiGovernanceRepository) GetGovernanceStats(ctx context.Context, kbID string) (*types.WikiGovernanceStats, error) {
	db := r.db.WithContext(ctx)
	byType, err := groupedCounts(db.Model(&types.WikiPage{}).Where("knowledge_base_id = ? AND page_type = ?", kbID, types.WikiPageTypeCard), "knowledge_type")
	if err != nil {
		return nil, err
	}
	byMaturity, err := groupedCounts(db.Model(&types.WikiPage{}).Where("knowledge_base_id = ? AND page_type = ?", kbID, types.WikiPageTypeCard), "maturity_status")
	if err != nil {
		return nil, err
	}
	byLevel, err := groupedCounts(db.Model(&types.WikiChangeSet{}).Where("knowledge_base_id = ?", kbID), "review_level")
	if err != nil {
		return nil, err
	}
	if legacy := byLevel[types.WikiReviewLevelL2]; legacy > 0 {
		byLevel[types.WikiReviewLevelL1] += legacy
		delete(byLevel, types.WikiReviewLevelL2)
	}
	byStatus, err := groupedCounts(db.Model(&types.WikiChangeSet{}).Where("knowledge_base_id = ?", kbID), "status")
	if err != nil {
		return nil, err
	}
	var decisions []struct {
		Key   string
		Count int64
	}
	err = db.Table("wiki_reviews").Select("wiki_reviews.decision AS key, COUNT(*) AS count").Joins("JOIN wiki_change_sets ON wiki_change_sets.id = wiki_reviews.change_set_id").Where("wiki_change_sets.knowledge_base_id = ?", kbID).Group("wiki_reviews.decision").Scan(&decisions).Error
	if err != nil {
		return nil, err
	}
	byDecision := map[string]int64{}
	for _, row := range decisions {
		byDecision[row.Key] = row.Count
	}
	var pendingCount int64
	if err := db.Model(&types.WikiChangeSet{}).Where("knowledge_base_id = ? AND status = ?", kbID, types.WikiChangeSetPending).Count(&pendingCount).Error; err != nil {
		return nil, err
	}
	var oldest *time.Time
	var row struct{ CreatedAt time.Time }
	if pendingCount > 0 {
		if err := db.Model(&types.WikiChangeSet{}).Select("created_at").Where("knowledge_base_id = ? AND status = ?", kbID, types.WikiChangeSetPending).Order("created_at ASC").First(&row).Error; err != nil {
			return nil, err
		}
		oldest = &row.CreatedAt
	}
	var reviewedSets []types.WikiChangeSet
	if err := db.Where("knowledge_base_id = ? AND reviewed_at IS NOT NULL", kbID).Find(&reviewedSets).Error; err != nil {
		return nil, err
	}
	var reviewSeconds float64
	for _, set := range reviewedSets {
		if set.ReviewedAt != nil {
			reviewSeconds += set.ReviewedAt.Sub(set.CreatedAt).Seconds()
		}
	}
	var reviews []types.WikiReview
	if err := db.Table("wiki_reviews").Select("wiki_reviews.*").Joins("JOIN wiki_change_sets ON wiki_change_sets.id = wiki_reviews.change_set_id").Where("wiki_change_sets.knowledge_base_id = ?", kbID).Find(&reviews).Error; err != nil {
		return nil, err
	}
	var modified, typeCorrected int64
	for _, review := range reviews {
		var overrides map[string]map[string]any
		if len(review.ItemOverrides) == 0 || json.Unmarshal(review.ItemOverrides, &overrides) != nil || len(overrides) == 0 {
			continue
		}
		modified++
		for _, fields := range overrides {
			if _, ok := fields["knowledge_type"]; ok {
				typeCorrected++
				break
			}
		}
	}
	totalSets := int64(0)
	for _, count := range byLevel {
		totalSets += count
	}
	decisionTotal := byDecision[types.WikiReviewApproved] + byDecision[types.WikiReviewRejected]
	var violations int64
	if err := db.Model(&types.WikiGovernanceMetricEvent{}).Where("knowledge_base_id = ? AND metric_name = ?", kbID, "hypothesis_answered_as_fact").Select("COALESCE(SUM(value), 0)").Scan(&violations).Error; err != nil {
		return nil, err
	}
	stats := &types.WikiGovernanceStats{CardsByKnowledgeType: byType, CardsByMaturity: byMaturity, ChangeSetsByLevel: byLevel, ChangeSetsByStatus: byStatus, ReviewsByDecision: byDecision, PendingReviewCount: pendingCount, OldestPendingAt: oldest, HypothesisFactViolations: violations}
	if totalSets > 0 {
		stats.AutoPublishedRate = float64(byLevel[types.WikiReviewLevelL0]) / float64(totalSets)
	}
	if decisionTotal > 0 {
		stats.ReviewApprovalRate = float64(byDecision[types.WikiReviewApproved]) / float64(decisionTotal)
	}
	if len(reviews) > 0 {
		stats.ReviewModifiedRate = float64(modified) / float64(len(reviews))
		stats.TypeCorrectionRate = float64(typeCorrected) / float64(len(reviews))
	}
	if len(reviewedSets) > 0 {
		stats.AverageReviewSeconds = reviewSeconds / float64(len(reviewedSets))
	}
	return stats, nil
}

func (r *wikiGovernanceRepository) RecordGovernanceMetric(ctx context.Context, event *types.WikiGovernanceMetricEvent) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Value == 0 {
		event.Value = 1
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *wikiGovernanceRepository) CreateRollbackChangeSet(ctx context.Context, kbID, appliedChangeSetID, requestedBy string) (*types.WikiChangeSet, error) {
	var created *types.WikiChangeSet
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var original types.WikiChangeSet
		if err := tx.Preload("Items").Where("id = ? AND knowledge_base_id = ?", appliedChangeSetID, kbID).First(&original).Error; err != nil {
			return err
		}
		if original.Status != types.WikiChangeSetApplied {
			return errors.New("only an applied change set can be rolled back")
		}
		now := time.Now()
		set := &types.WikiChangeSet{ID: uuid.NewString(), TenantID: original.TenantID, KnowledgeBaseID: kbID, KnowledgeID: original.KnowledgeID, Status: types.WikiChangeSetPending, ReviewLevel: types.WikiReviewLevelL1, ChangeCategory: types.WikiChangeCategoryCorrection, Reasons: types.StringArray{"rollback_requested", "published_content_will_change"}, ModelID: original.ModelID, PromptVersion: original.PromptVersion, CreatedBy: requestedBy, CreatedAt: now, UpdatedAt: now}
		for _, item := range original.Items {
			var current types.WikiPage
			if err := tx.Where("knowledge_base_id = ? AND slug = ?", kbID, item.PageSlug).First(&current).Error; err != nil {
				return err
			}
			reverse := types.WikiChangeItem{ID: uuid.NewString(), ChangeSetID: set.ID, ChangeCategory: types.WikiChangeCategoryCorrection, PageID: current.ID, PageSlug: current.Slug, ExpectedVersion: current.Version, Before: pageJSONForRepository(&current), EvidenceChunkIDs: current.ChunkRefs, EvidenceExcerpts: types.JSON(`{}`), CreatedAt: now}
			switch item.Operation {
			case "create":
				reverse.Operation = "archive"
				reverse.After = pageJSONForRepository(&current)
				reverse.ChangedFields = types.StringArray{"status"}
			case "update", "archive":
				desired, err := decodePageSnapshot(item.Before)
				if err != nil {
					return err
				}
				desired.ID, desired.KnowledgeBaseID, desired.Version = current.ID, kbID, current.Version
				reverse.Operation = "update"
				reverse.After = pageJSONForRepository(desired)
				reverse.ChangedFields = changedFieldsForRollback(&current, desired)
			default:
				return fmt.Errorf("unsupported original operation %q", item.Operation)
			}
			set.Items = append(set.Items, reverse)
		}
		items := set.Items
		set.Items = nil
		if err := tx.Create(set).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		set.Items = items
		created = set
		return nil
	})
	return created, err
}

func pageJSONForRepository(page *types.WikiPage) types.JSON {
	data, _ := json.Marshal(page)
	return types.JSON(data)
}
func changedFieldsForRollback(current, desired *types.WikiPage) types.StringArray {
	fields := types.StringArray{"content", "summary", "knowledge_type", "maturity_status", "review_status", "applicability", "source_refs", "chunk_refs"}
	if current.Status != desired.Status {
		fields = append(fields, "status")
	}
	return fields
}
