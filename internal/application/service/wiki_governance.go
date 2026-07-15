package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

const wikiCardPromptVersion = "scenario-cards-v1"

type wikiGovernanceService struct {
	repo interfaces.WikiGovernanceRepository
}

func NewWikiGovernanceService(repo interfaces.WikiGovernanceRepository) interfaces.WikiGovernanceService {
	return &wikiGovernanceService{repo: repo}
}

var validKnowledgeTypes = map[string]bool{
	types.WikiKnowledgeTypeKnowledge: true, types.WikiKnowledgeTypeExperience: true,
	types.WikiKnowledgeTypeQuestion: true, types.WikiKnowledgeTypeHypothesis: true,
	types.WikiKnowledgeTypeExperiment: true, types.WikiKnowledgeTypeMetric: true,
	types.WikiKnowledgeTypeProcedure: true, types.WikiKnowledgeTypeFailure: true,
	types.WikiKnowledgeTypeCase: true, types.WikiKnowledgeTypeRule: true,
}

var validAnswerStrengths = map[string]bool{
	types.WikiAnswerStrengthStrong: true, types.WikiAnswerStrengthMedium: true,
	types.WikiAnswerStrengthWeak: true, types.WikiAnswerStrengthNone: true,
	types.WikiAnswerStrengthUnknown: true,
}

var allowedTypesByNature = map[string]map[string]bool{
	"official_rule":       {types.WikiKnowledgeTypeRule: true, types.WikiKnowledgeTypeKnowledge: true, types.WikiKnowledgeTypeProcedure: true},
	"business_manual":     {types.WikiKnowledgeTypeKnowledge: true, types.WikiKnowledgeTypeProcedure: true, types.WikiKnowledgeTypeQuestion: true},
	"meeting_record":      {types.WikiKnowledgeTypeQuestion: true, types.WikiKnowledgeTypeHypothesis: true, types.WikiKnowledgeTypeCase: true},
	"analysis_report":     {types.WikiKnowledgeTypeQuestion: true, types.WikiKnowledgeTypeHypothesis: true, types.WikiKnowledgeTypeMetric: true, types.WikiKnowledgeTypeExperience: true},
	"experiment_report":   {types.WikiKnowledgeTypeExperiment: true, types.WikiKnowledgeTypeExperience: true, types.WikiKnowledgeTypeFailure: true},
	"customer_feedback":   {types.WikiKnowledgeTypeQuestion: true, types.WikiKnowledgeTypeCase: true},
	"operational_data":    {types.WikiKnowledgeTypeQuestion: true, types.WikiKnowledgeTypeMetric: true, types.WikiKnowledgeTypeCase: true},
	"personal_experience": {types.WikiKnowledgeTypeExperience: true, types.WikiKnowledgeTypeCase: true},
	"proposal":            {types.WikiKnowledgeTypeHypothesis: true, types.WikiKnowledgeTypeExperiment: true, types.WikiKnowledgeTypeQuestion: true},
}

var uncertaintySignals = []string{"可能", "推测", "预计", "或许", "有待验证", "怀疑", "假设", "may ", "might ", "possibly", "hypothesis"}
var highRiskSignals = []string{"价格", "退款", "赔付", "合同", "合规", "隐私", "安全", "权限", "承诺", "password", "privacy", "refund", "contract", "compliance"}

type WikiReviewAssessment struct {
	Level   string
	Reasons []string
}

func AssessWikiCardChange(candidate, existing *types.WikiPage) WikiReviewAssessment {
	level := types.WikiReviewLevelL0
	reasons := []string{}
	escalate := func(to, reason string) {
		rank := map[string]int{types.WikiReviewLevelL0: 0, types.WikiReviewLevelL1: 1, types.WikiReviewLevelL2: 2}
		if rank[to] > rank[level] {
			level = to
		}
		for _, r := range reasons {
			if r == reason {
				return
			}
		}
		reasons = append(reasons, reason)
	}
	if candidate == nil {
		return WikiReviewAssessment{Level: types.WikiReviewLevelL2, Reasons: []string{"missing_candidate"}}
	}
	if !validKnowledgeTypes[candidate.KnowledgeType] {
		escalate(types.WikiReviewLevelL2, "invalid_knowledge_type")
	}
	if len(candidate.ChunkRefs) == 0 {
		escalate(types.WikiReviewLevelL1, "missing_chunk_evidence")
	}
	if existing == nil && strings.TrimSpace(candidate.BusinessLine) == "" {
		escalate(types.WikiReviewLevelL1, "missing_business_line")
	}
	if existing == nil && len(candidate.ScenarioIDs) == 0 {
		escalate(types.WikiReviewLevelL1, "missing_scenario")
	}
	meta, _ := candidate.PageMetadata.Map()
	nature, _ := meta["document_nature"].(string)
	if origin, _ := meta["submission_origin"].(string); origin == "manual" {
		escalate(types.WikiReviewLevelL1, "manual_candidate_submission")
	}
	if forceReview, _ := meta["force_review"].(bool); forceReview {
		escalate(types.WikiReviewLevelL1, "legacy_reclassification")
	}
	if duplicate, _ := meta["possible_duplicate_slug"].(string); duplicate != "" {
		escalate(types.WikiReviewLevelL1, "possible_duplicate")
	}
	if conflict, _ := meta["cross_page_conflict"].(bool); conflict {
		escalate(types.WikiReviewLevelL2, "cross_page_claim_conflict")
	}
	if uncertain, _ := meta["cross_page_uncertain"].(bool); uncertain {
		escalate(types.WikiReviewLevelL2, "cross_page_claim_uncertain")
	}
	if supersedes, _ := meta["cross_page_supersedes"].(bool); supersedes {
		escalate(types.WikiReviewLevelL2, "cross_page_claim_supersedes_existing")
	}
	if failed, _ := meta["cross_page_check_failed"].(bool); failed {
		escalate(types.WikiReviewLevelL2, "cross_page_conflict_check_failed")
	}
	if incomplete, _ := meta["cross_page_check_incomplete"].(bool); incomplete {
		escalate(types.WikiReviewLevelL2, "cross_page_conflict_check_incomplete")
	}
	if allowed := allowedTypesByNature[nature]; nature != "" && nature != "unknown" && !allowed[candidate.KnowledgeType] {
		escalate(types.WikiReviewLevelL2, "document_nature_type_mismatch")
	}
	text := strings.ToLower(candidate.Title + " " + candidate.Summary + " " + candidate.Content)
	if candidate.KnowledgeType == types.WikiKnowledgeTypeKnowledge || candidate.KnowledgeType == types.WikiKnowledgeTypeRule || candidate.KnowledgeType == types.WikiKnowledgeTypeExperience {
		for _, signal := range uncertaintySignals {
			if strings.Contains(text, signal) {
				escalate(types.WikiReviewLevelL2, "uncertain_language_in_asserted_card")
				break
			}
		}
	}
	for _, signal := range highRiskSignals {
		if strings.Contains(text, signal) {
			escalate(types.WikiReviewLevelL2, "high_risk_content")
			break
		}
	}
	if existing == nil {
		switch candidate.KnowledgeType {
		case types.WikiKnowledgeTypeQuestion, types.WikiKnowledgeTypeHypothesis, types.WikiKnowledgeTypeExperiment, types.WikiKnowledgeTypeExperience:
			escalate(types.WikiReviewLevelL1, "new_reviewable_card")
		case types.WikiKnowledgeTypeRule, types.WikiKnowledgeTypeProcedure:
			escalate(types.WikiReviewLevelL2, "new_authoritative_card")
		}
	} else {
		existingMeta, _ := existing.PageMetadata.Map()
		existingNature, _ := existingMeta["document_nature"].(string)
		authority := map[string]int{"official_rule": 5, "business_manual": 4, "experiment_report": 3, "analysis_report": 3, "operational_data": 3, "meeting_record": 2, "proposal": 2, "customer_feedback": 1, "personal_experience": 1, "unknown": 0}
		if (existing.Content != candidate.Content || existing.Summary != candidate.Summary) && authority[nature] < authority[existingNature] {
			escalate(types.WikiReviewLevelL2, "lower_authority_source_would_override_existing_card")
		}
		if len(existing.ChunkRefs) > 0 && len(candidate.ChunkRefs) == 0 {
			escalate(types.WikiReviewLevelL2, "published_card_lost_all_evidence")
		}
		if existing.KnowledgeType != candidate.KnowledgeType {
			escalate(types.WikiReviewLevelL2, "knowledge_type_changed")
		}
		strengthRank := map[string]int{types.WikiAnswerStrengthNone: 0, types.WikiAnswerStrengthUnknown: 0, types.WikiAnswerStrengthWeak: 1, types.WikiAnswerStrengthMedium: 2, types.WikiAnswerStrengthStrong: 3}
		if strengthRank[candidate.AnswerStrength] > strengthRank[existing.AnswerStrength] {
			escalate(types.WikiReviewLevelL2, "answer_strength_increased")
		} else if candidate.AnswerStrength != existing.AnswerStrength {
			escalate(types.WikiReviewLevelL1, "answer_strength_changed")
		}
		if existing.Content != candidate.Content || existing.Summary != candidate.Summary {
			escalate(types.WikiReviewLevelL2, "card_conclusion_changed")
		}
		if string(existing.Applicability) != string(candidate.Applicability) {
			escalate(types.WikiReviewLevelL2, "applicability_changed")
		}
		if strings.Join(existing.ProhibitedClaims, "\x00") != strings.Join(candidate.ProhibitedClaims, "\x00") {
			escalate(types.WikiReviewLevelL2, "risk_boundary_changed")
		}
		if candidate.MaturityStatus == types.WikiMaturityDisputed || candidate.MaturityStatus == types.WikiMaturityOutdated || candidate.MaturityStatus == types.WikiMaturityArchived {
			escalate(types.WikiReviewLevelL2, "knowledge_became_unreliable")
		}
		if existing.Content == candidate.Content && existing.Summary == candidate.Summary && existing.KnowledgeType == candidate.KnowledgeType { /* evidence/tag-only stays L0 */
		}
	}
	sort.Strings(reasons)
	return WikiReviewAssessment{Level: level, Reasons: reasons}
}

func normalizeCardCandidate(p *types.WikiPage) error {
	if p == nil {
		return errors.New("candidate is required")
	}
	if p.PageType == "" {
		p.PageType = types.WikiPageTypeCard
	}
	if p.PageType != types.WikiPageTypeCard {
		return errors.New("governance candidate must be a card page")
	}
	if !validKnowledgeTypes[p.KnowledgeType] {
		return errors.New("invalid knowledge_type")
	}
	if p.AnswerStrength == "" {
		p.AnswerStrength = types.WikiAnswerStrengthUnknown
	}
	if !validAnswerStrengths[p.AnswerStrength] {
		return errors.New("invalid answer_strength")
	}
	if p.AnswerStrength == types.WikiAnswerStrengthStrong && (p.KnowledgeType == types.WikiKnowledgeTypeQuestion || p.KnowledgeType == types.WikiKnowledgeTypeHypothesis || p.KnowledgeType == types.WikiKnowledgeTypeExperiment) {
		p.AnswerStrength = types.WikiAnswerStrengthWeak
	}
	if p.MaturityStatus == "" {
		p.MaturityStatus = types.WikiMaturityPendingReview
	}
	if p.ReviewStatus == "" {
		p.ReviewStatus = types.WikiReviewPending
	}
	if len(p.ChunkRefs) == 0 {
		p.MaturityStatus = types.WikiMaturityDraft
	}
	return nil
}

func pageJSON(p *types.WikiPage) types.JSON { b, _ := json.Marshal(p); return types.JSON(b) }

func candidateFingerprint(p *types.WikiPage) string {
	sorted := func(values types.StringArray) types.StringArray {
		out := append(types.StringArray(nil), values...)
		sort.Strings(out)
		return out
	}
	payload := struct {
		Slug, KnowledgeType, Content, Summary, BusinessLine  string
		MaturityStatus, AnswerStrength, Status               string
		Applicability                                        types.JSON
		SourceRefs, ChunkRefs, ScenarioIDs, ProhibitedClaims types.StringArray
		EffectiveFrom, EffectiveTo                           *time.Time
	}{p.Slug, p.KnowledgeType, p.Content, p.Summary, p.BusinessLine, p.MaturityStatus, p.AnswerStrength, p.Status, p.Applicability, sorted(p.SourceRefs), sorted(p.ChunkRefs), sorted(p.ScenarioIDs), sorted(p.ProhibitedClaims), p.EffectiveFrom, p.EffectiveTo}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func changedCardFields(a, b *types.WikiPage) types.StringArray {
	if a == nil {
		return types.StringArray{"page"}
	}
	var out types.StringArray
	checks := []struct {
		name    string
		changed bool
	}{
		{"title", a.Title != b.Title}, {"content", a.Content != b.Content}, {"summary", a.Summary != b.Summary},
		{"knowledge_type", a.KnowledgeType != b.KnowledgeType}, {"maturity_status", a.MaturityStatus != b.MaturityStatus},
		{"answer_strength", a.AnswerStrength != b.AnswerStrength}, {"business_line", a.BusinessLine != b.BusinessLine},
		{"scenario_ids", strings.Join(a.ScenarioIDs, "\x00") != strings.Join(b.ScenarioIDs, "\x00")},
		{"audience_roles", strings.Join(a.AudienceRoles, "\x00") != strings.Join(b.AudienceRoles, "\x00")},
		{"affected_metrics", strings.Join(a.AffectedMetrics, "\x00") != strings.Join(b.AffectedMetrics, "\x00")},
		{"source_refs", strings.Join(a.SourceRefs, "\x00") != strings.Join(b.SourceRefs, "\x00")},
		{"chunk_refs", strings.Join(a.ChunkRefs, "\x00") != strings.Join(b.ChunkRefs, "\x00")},
		{"applicability", string(a.Applicability) != string(b.Applicability)},
		{"prohibited_claims", strings.Join(a.ProhibitedClaims, "\x00") != strings.Join(b.ProhibitedClaims, "\x00")},
	}
	for _, c := range checks {
		if c.changed {
			out = append(out, c.name)
		}
	}
	return out
}

func (s *wikiGovernanceService) SubmitCandidate(ctx context.Context, candidate, existing *types.WikiPage, knowledgeID, modelID string) (*types.WikiChangeSet, error) {
	if candidate == nil {
		return nil, errors.New("candidate is required")
	}
	if modelID == "manual" {
		metadata, _ := candidate.PageMetadata.Map()
		metadata["submission_origin"] = "manual"
		data, _ := json.Marshal(metadata)
		candidate.PageMetadata = types.JSON(data)
	}
	candidate.ReviewStatus = types.WikiReviewPending
	if existing == nil {
		candidate.Status = types.WikiPageStatusDraft
	} else if candidate.Status == "" {
		candidate.Status = existing.Status
	}
	if err := normalizeCardCandidate(candidate); err != nil {
		return nil, err
	}
	assessment := AssessWikiCardChange(candidate, existing)
	fingerprint := candidateFingerprint(candidate)
	previous, err := s.repo.FindChangeSetByFingerprint(ctx, candidate.KnowledgeBaseID, fingerprint)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		return previous, nil
	}
	now := time.Now()
	setID := uuid.NewString()
	operation := "create"
	expectedVersion := 0
	before := types.JSON(nil)
	if existing != nil {
		operation = "update"
		if candidate.Status == types.WikiPageStatusArchived || candidate.MaturityStatus == types.WikiMaturityArchived {
			operation = "archive"
		}
		expectedVersion = existing.Version
		before = pageJSON(existing)
		candidate.ID = existing.ID
		candidate.Version = existing.Version
	}
	set := &types.WikiChangeSet{ID: setID, TenantID: candidate.TenantID, KnowledgeBaseID: candidate.KnowledgeBaseID, KnowledgeID: knowledgeID, Status: types.WikiChangeSetPending, ReviewLevel: assessment.Level, Reasons: assessment.Reasons, ModelID: modelID, PromptVersion: wikiCardPromptVersion, CandidateFingerprint: fingerprint, CreatedAt: now, UpdatedAt: now}
	metadata, _ := candidate.PageMetadata.Map()
	excerpts, _ := json.Marshal(metadata["evidence_excerpts"])
	set.Items = []types.WikiChangeItem{{ID: uuid.NewString(), ChangeSetID: setID, Operation: operation, PageID: candidate.ID, PageSlug: candidate.Slug, ExpectedVersion: expectedVersion, Before: before, After: pageJSON(candidate), ChangedFields: changedCardFields(existing, candidate), EvidenceChunkIDs: candidate.ChunkRefs, EvidenceExcerpts: types.JSON(excerpts), CreatedAt: now}}
	if err := s.repo.CreateChangeSet(ctx, set); err != nil {
		return nil, err
	}
	if assessment.Level == types.WikiReviewLevelL0 {
		decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, Comment: "Automatically approved by L0 governance policy"}
		if err := s.repo.ReviewChangeSet(ctx, candidate.KnowledgeBaseID, set.ID, "system", decision); err != nil {
			return nil, err
		}
		set.Status = types.WikiChangeSetApplied
	}
	return set, nil
}

func (s *wikiGovernanceService) GetChangeSet(ctx context.Context, kbID, id string) (*types.WikiChangeSet, error) {
	return s.repo.GetChangeSet(ctx, kbID, id)
}
func (s *wikiGovernanceService) ListChangeSets(ctx context.Context, req types.WikiGovernanceListRequest) ([]*types.WikiChangeSet, int64, error) {
	return s.repo.ListChangeSets(ctx, req)
}
func (s *wikiGovernanceService) ListPageChangeSets(ctx context.Context, kbID, slug string, limit int) ([]*types.WikiChangeSet, error) {
	return s.repo.ListPageChangeSets(ctx, kbID, slug, limit)
}
func (s *wikiGovernanceService) ReviewChangeSet(ctx context.Context, kbID, id, reviewerID string, d *types.WikiReviewDecision) error {
	return s.repo.ReviewChangeSet(ctx, kbID, id, reviewerID, d)
}
func (s *wikiGovernanceService) ListReviews(ctx context.Context, id string) ([]*types.WikiReview, error) {
	return s.repo.ListReviews(ctx, id)
}
func (s *wikiGovernanceService) ListPackages(ctx context.Context, kbID string) ([]*types.WikiPackage, error) {
	return s.repo.ListPackages(ctx, kbID)
}
func (s *wikiGovernanceService) CreatePackage(ctx context.Context, p *types.WikiPackage) error {
	return s.repo.CreatePackage(ctx, p)
}
func (s *wikiGovernanceService) ListPackagePages(ctx context.Context, kbID, packageID string) ([]*types.WikiPage, error) {
	return s.repo.ListPackagePages(ctx, kbID, packageID)
}
func (s *wikiGovernanceService) SetPackagePages(ctx context.Context, kbID, packageID string, pageIDs []string) error {
	return s.repo.SetPackagePages(ctx, kbID, packageID, pageIDs)
}
func (s *wikiGovernanceService) ListScenarios(ctx context.Context, kbID string) ([]*types.WikiScenario, error) {
	return s.repo.ListScenarios(ctx, kbID)
}
func (s *wikiGovernanceService) CreateScenario(ctx context.Context, sc *types.WikiScenario) error {
	return s.repo.CreateScenario(ctx, sc)
}
func (s *wikiGovernanceService) ListScenarioPages(ctx context.Context, kbID, scenarioID string) ([]*types.WikiPage, error) {
	return s.repo.ListScenarioPages(ctx, kbID, scenarioID)
}
func (s *wikiGovernanceService) GetGovernanceStats(ctx context.Context, kbID string) (*types.WikiGovernanceStats, error) {
	return s.repo.GetGovernanceStats(ctx, kbID)
}
func (s *wikiGovernanceService) RecordGovernanceMetric(ctx context.Context, event *types.WikiGovernanceMetricEvent) error {
	return s.repo.RecordGovernanceMetric(ctx, event)
}
func (s *wikiGovernanceService) CreateRollbackChangeSet(ctx context.Context, kbID, appliedChangeSetID, requestedBy string) (*types.WikiChangeSet, error) {
	return s.repo.CreateRollbackChangeSet(ctx, kbID, appliedChangeSetID, requestedBy)
}
