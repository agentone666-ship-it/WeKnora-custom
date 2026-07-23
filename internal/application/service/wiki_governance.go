package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const wikiCardPromptVersion = "scenario-cards-v1"

type wikiGovernanceService struct {
	repo        interfaces.WikiGovernanceRepository
	task        interfaces.TaskEnqueuer
	pendingRepo interfaces.TaskPendingOpsRepository
}

func NewWikiGovernanceService(repo interfaces.WikiGovernanceRepository, task interfaces.TaskEnqueuer, pendingRepo interfaces.TaskPendingOpsRepository) interfaces.WikiGovernanceService {
	return &wikiGovernanceService{repo: repo, task: task, pendingRepo: pendingRepo}
}

func (s *wikiGovernanceService) requestGraphRefresh(ctx context.Context, tenantID uint64, kbID string, slugs []string) {
	if s.task == nil || s.pendingRepo == nil || kbID == "" {
		return
	}
	seen := map[string]bool{}
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		rowBytes, _ := json.Marshal(wikiFinalizeRow{Slug: slug, Graph: true})
		_ = s.pendingRepo.Enqueue(ctx, &types.TaskPendingOp{TenantID: tenantID, TaskType: wikiFinalizeTaskType, Scope: wikiTaskScope, ScopeID: kbID, Op: wikiFinalizeOpGraph, DedupKey: slug, Payload: rowBytes})
	}
	if len(seen) == 0 {
		return
	}
	lang, _ := types.LanguageFromContext(ctx)
	payload := WikiIngestPayload{TenantID: tenantID, KnowledgeBaseID: kbID, Language: lang}
	b, _ := json.Marshal(payload)
	task := asynq.NewTask(types.TypeWikiFinalize, b, asynq.Queue(types.QueueWiki), asynq.MaxRetry(wikiIngestMaxRetry), asynq.Timeout(30*time.Minute), asynq.ProcessIn(wikiFinalizeDelay), asynq.TaskID("wiki-finalize-"+kbID))
	if _, err := s.task.Enqueue(task); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) && !errors.Is(err, asynq.ErrDuplicateTask) {
		// The durable row remains queued; the next Wiki update/finalize trigger
		// will pick it up. Governance must not fail after its DB commit merely
		// because the asynchronous convergence trigger was unavailable.
		return
	}
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
var explicitCorrectionSignals = []string{"明确更正", "现更正为", "修订为", "废止", "正式替代", "正式取代", "以本版本为准", "以本文件为准", "原规定不再适用", "supersedes", "replaces the previous", "is hereby amended", "corrected version"}

func governanceJSONEqual(left, right types.JSON) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return string(left) == string(right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

type WikiReviewAssessment struct {
	Level   string
	Reasons []string
}

func AssessWikiCardChange(candidate, existing *types.WikiPage) WikiReviewAssessment {
	level := types.WikiReviewLevelL0
	reasons := []string{}
	escalate := func(to, reason string) {
		rank := map[string]int{types.WikiReviewLevelL0: 0, types.WikiReviewLevelL1: 1}
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
		return WikiReviewAssessment{Level: types.WikiReviewLevelL1, Reasons: []string{"missing_candidate"}}
	}
	if !validKnowledgeTypes[candidate.KnowledgeType] {
		escalate(types.WikiReviewLevelL1, "invalid_knowledge_type")
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
		escalate(types.WikiReviewLevelL1, "cross_page_claim_conflict")
	}
	if uncertain, _ := meta["cross_page_uncertain"].(bool); uncertain {
		escalate(types.WikiReviewLevelL1, "cross_page_claim_uncertain")
	}
	if supersedes, _ := meta["cross_page_supersedes"].(bool); supersedes {
		escalate(types.WikiReviewLevelL1, "cross_page_claim_supersedes_existing")
	}
	if failed, _ := meta["cross_page_check_failed"].(bool); failed {
		escalate(types.WikiReviewLevelL1, "cross_page_conflict_check_failed")
	}
	if incomplete, _ := meta["cross_page_check_incomplete"].(bool); incomplete {
		escalate(types.WikiReviewLevelL1, "cross_page_conflict_check_incomplete")
	}
	if allowed := allowedTypesByNature[nature]; nature != "" && nature != "unknown" && !allowed[candidate.KnowledgeType] {
		escalate(types.WikiReviewLevelL1, "document_nature_type_mismatch")
	}
	text := strings.ToLower(candidate.Title + " " + candidate.Summary + " " + candidate.Content)
	if candidate.KnowledgeType == types.WikiKnowledgeTypeKnowledge || candidate.KnowledgeType == types.WikiKnowledgeTypeRule || candidate.KnowledgeType == types.WikiKnowledgeTypeExperience {
		for _, signal := range uncertaintySignals {
			if strings.Contains(text, signal) {
				escalate(types.WikiReviewLevelL1, "uncertain_language_in_asserted_card")
				break
			}
		}
	}
	for _, signal := range highRiskSignals {
		if strings.Contains(text, signal) {
			escalate(types.WikiReviewLevelL1, "high_risk_content")
			break
		}
	}
	if existing == nil {
		switch candidate.KnowledgeType {
		case types.WikiKnowledgeTypeQuestion, types.WikiKnowledgeTypeHypothesis, types.WikiKnowledgeTypeExperiment, types.WikiKnowledgeTypeExperience:
			escalate(types.WikiReviewLevelL1, "new_reviewable_card")
		case types.WikiKnowledgeTypeRule, types.WikiKnowledgeTypeProcedure:
			escalate(types.WikiReviewLevelL1, "new_authoritative_card")
		}
	} else {
		existingMeta, _ := existing.PageMetadata.Map()
		existingNature, _ := existingMeta["document_nature"].(string)
		authority := map[string]int{"official_rule": 5, "business_manual": 4, "experiment_report": 3, "analysis_report": 3, "operational_data": 3, "meeting_record": 2, "proposal": 2, "customer_feedback": 1, "personal_experience": 1, "unknown": 0}
		if (existing.Content != candidate.Content || existing.Summary != candidate.Summary) && authority[nature] < authority[existingNature] {
			escalate(types.WikiReviewLevelL1, "lower_authority_source_would_override_existing_card")
		}
		if len(existing.ChunkRefs) > 0 && len(candidate.ChunkRefs) == 0 {
			escalate(types.WikiReviewLevelL1, "published_card_lost_all_evidence")
		}
		if existing.KnowledgeType != candidate.KnowledgeType {
			escalate(types.WikiReviewLevelL1, "knowledge_type_changed")
		}
		strengthRank := map[string]int{types.WikiAnswerStrengthNone: 0, types.WikiAnswerStrengthUnknown: 0, types.WikiAnswerStrengthWeak: 1, types.WikiAnswerStrengthMedium: 2, types.WikiAnswerStrengthStrong: 3}
		if strengthRank[candidate.AnswerStrength] > strengthRank[existing.AnswerStrength] {
			escalate(types.WikiReviewLevelL1, "answer_strength_increased")
		} else if candidate.AnswerStrength != existing.AnswerStrength {
			escalate(types.WikiReviewLevelL1, "answer_strength_changed")
		}
		if existing.Content != candidate.Content || existing.Summary != candidate.Summary {
			escalate(types.WikiReviewLevelL1, "card_conclusion_changed")
		}
		if !governanceJSONEqual(existing.Applicability, candidate.Applicability) {
			escalate(types.WikiReviewLevelL1, "applicability_changed")
		}
		if strings.Join(existing.ProhibitedClaims, "\x00") != strings.Join(candidate.ProhibitedClaims, "\x00") {
			escalate(types.WikiReviewLevelL1, "risk_boundary_changed")
		}
		if candidate.MaturityStatus == types.WikiMaturityDisputed || candidate.MaturityStatus == types.WikiMaturityOutdated || candidate.MaturityStatus == types.WikiMaturityArchived {
			escalate(types.WikiReviewLevelL1, "knowledge_became_unreliable")
		}
		if existing.Content == candidate.Content && existing.Summary == candidate.Summary && existing.KnowledgeType == candidate.KnowledgeType { /* evidence/tag-only stays L0 */
		}
	}
	if isSafeAutomaticCorrection(candidate, existing, reasons) {
		level = types.WikiReviewLevelL0
		reasons = append(reasons, "authoritative_explicit_correction")
	}
	sort.Strings(reasons)
	return WikiReviewAssessment{Level: level, Reasons: reasons}
}

func metadataSourceTime(metadata map[string]any) (time.Time, bool) {
	value, _ := metadata["source_updated_at"].(string)
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func containsAnyFold(text string, signals []string) bool {
	text = strings.ToLower(text)
	for _, signal := range signals {
		if strings.Contains(text, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

func metadataStringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func metadataEvidenceText(metadata map[string]any) string {
	excerpts, _ := metadata["evidence_excerpts"].(map[string]any)
	values := make([]string, 0, len(excerpts))
	for _, value := range excerpts {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			values = append(values, text)
		}
	}
	sort.Strings(values)
	return strings.Join(values, "\n")
}

func safeCorrectionCrossPageEvidence(metadata map[string]any, existing *types.WikiPage) bool {
	conflict, _ := metadata["cross_page_conflict"].(bool)
	supersedes, _ := metadata["cross_page_supersedes"].(bool)
	if !conflict && !supersedes {
		return true
	}
	assessments, _ := metadata["cross_page_assessments"].([]any)
	evidence, _ := metadata["related_page_evidence"].(map[string]any)
	oldSources := map[string]bool{}
	for _, source := range existing.SourceRefs {
		oldSources[source] = true
	}
	checked := 0
	for _, raw := range assessments {
		assessment, _ := raw.(map[string]any)
		relation, _ := assessment["relation"].(string)
		if relation != "conflicting" && relation != "supersedes" {
			continue
		}
		confidence, _ := assessment["confidence"].(float64)
		overlap, _ := assessment["applicability_overlap"].(bool)
		slug, _ := assessment["related_slug"].(string)
		if confidence < 0.9 || !overlap || slug == "" {
			return false
		}
		related, _ := evidence[slug].(map[string]any)
		sources := metadataStringSlice(related["source_refs"])
		if len(sources) == 0 {
			return false
		}
		for _, source := range sources {
			if !oldSources[source] {
				return false
			}
		}
		checked++
	}
	return checked > 0
}

// correctionClaimOverlapThreshold is the minimum overlap-coefficient a
// correction candidate's claim must share with an existing approved card's
// text to be treated as the same rule when the LLM emits a different slug.
// It is intentionally a permissive "same subject matter" floor; the
// applicability-overlap check and isSafeAutomaticCorrection provide the
// remaining precision.
const correctionClaimOverlapThreshold = 0.3

// tokenizeClaim extracts stable comparison tokens from a free-text claim:
// CJK character bigrams (so Chinese phrases match without a word segmenter)
// and lowercased alphanumeric words. Punctuation and whitespace are dropped.
func tokenizeClaim(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	runes := []rune(text)
	var alpha strings.Builder
	flushAlpha := func() {
		s := strings.ToLower(strings.TrimSpace(alpha.String()))
		alpha.Reset()
		if s != "" {
			tokens[s] = struct{}{}
		}
	}
	for i, r := range runes {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			flushAlpha()
			if i+1 < len(runes) && runes[i+1] >= 0x4E00 && runes[i+1] <= 0x9FFF {
				tokens[string(runes[i])+string(runes[i+1])] = struct{}{}
			}
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			alpha.WriteRune(r)
		default:
			flushAlpha()
		}
	}
	flushAlpha()
	return tokens
}

// claimOverlapScore returns the overlap coefficient between the token sets of
// two free-text claims (|A∩B| / min(|A|,|B|)). It is the deterministic signal
// used to decide whether a correction candidate describes the same rule as an
// existing approved card when the LLM-generated title/slug differs. A candidate
// with fewer than three tokens is treated as too short to match reliably.
func claimOverlapScore(candidate, existing string) float64 {
	a := tokenizeClaim(candidate)
	b := tokenizeClaim(existing)
	if len(a) < 3 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	smaller := len(a)
	if len(b) < smaller {
		smaller = len(b)
	}
	return float64(intersection) / float64(smaller)
}

// applicabilityValueText concatenates every string value nested inside an
// applicability JSON blob. Comparing values (not keys) keeps the overlap check
// stable when the LLM names applicability fields differently for the same scope.
func applicabilityValueText(app types.JSON) string {
	var values []string
	var collect func(v any)
	collect = func(v any) {
		switch value := v.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				values = append(values, value)
			}
		case map[string]any:
			for _, child := range value {
				collect(child)
			}
		case []any:
			for _, child := range value {
				collect(child)
			}
		}
	}
	var parsed any
	if json.Unmarshal(app, &parsed) == nil {
		collect(parsed)
	}
	sort.Strings(values)
	return strings.Join(values, " ")
}

// correctionApplicabilityOverlaps reports whether two applicability blobs
// describe the same scope. An applicability with no string values (empty,
// "{}", "null", etc.) is treated as "undetermined" and allowed through; the
// content-overlap check and the downstream isSafeAutomaticCorrection gate
// still protect against wrong matches.
func correctionApplicabilityOverlaps(existing, candidate types.JSON) bool {
	existingText := applicabilityValueText(existing)
	candidateText := applicabilityValueText(candidate)
	if existingText == "" || candidateText == "" {
		return true
	}
	if governanceJSONEqual(existing, candidate) {
		return true
	}
	return claimOverlapScore(existingText, candidateText) >= 0.3
}

// isSafeAutomaticCorrection is intentionally a strict allow-list. A candidate
// is downgraded from human review only when the sole review reason is a changed
// conclusion and the source itself proves a newer, authoritative correction.
func isSafeAutomaticCorrection(candidate, existing *types.WikiPage, reasons []string) bool {
	if candidate == nil || existing == nil || candidate.PageType != types.WikiPageTypeCard || existing.PageType != types.WikiPageTypeCard {
		return false
	}
	allowedReasons := map[string]bool{"card_conclusion_changed": true, "cross_page_claim_conflict": true, "cross_page_claim_supersedes_existing": true}
	hasConclusionChange := false
	for _, reason := range reasons {
		if !allowedReasons[reason] {
			return false
		}
		hasConclusionChange = hasConclusionChange || reason == "card_conclusion_changed"
	}
	if !hasConclusionChange || len(candidate.ChunkRefs) == 0 {
		return false
	}
	if candidate.KnowledgeType != existing.KnowledgeType || candidate.BusinessLine != existing.BusinessLine || candidate.AnswerStrength != existing.AnswerStrength {
		return false
	}
	if !governanceJSONEqual(candidate.Applicability, existing.Applicability) || strings.Join(candidate.ProhibitedClaims, "\x00") != strings.Join(existing.ProhibitedClaims, "\x00") {
		return false
	}
	candidateMeta, candidateErr := candidate.PageMetadata.Map()
	existingMeta, existingErr := existing.PageMetadata.Map()
	if candidateErr != nil || existingErr != nil {
		return false
	}
	if origin, _ := candidateMeta["submission_origin"].(string); origin == "manual" {
		return false
	}
	if forceReview, _ := candidateMeta["force_review"].(bool); forceReview {
		return false
	}
	for _, flag := range []string{"cross_page_uncertain", "cross_page_check_failed", "cross_page_check_incomplete"} {
		if enabled, _ := candidateMeta[flag].(bool); enabled {
			return false
		}
	}
	// A deterministic content/applicability match (used when the LLM emits a
	// different title/slug for the same rule) carries its own cross-page
	// identity proof, so the LLM-confidence-based evidence gate below does not
	// apply. Every other safety check above and below still runs.
	if method, _ := candidateMeta["correction_target_method"].(string); method != "deterministic_content_match" {
		if !safeCorrectionCrossPageEvidence(candidateMeta, existing) {
			return false
		}
	}
	candidateNature, _ := candidateMeta["document_nature"].(string)
	existingNature, _ := existingMeta["document_nature"].(string)
	if candidateNature != "official_rule" && candidateNature != "business_manual" {
		return false
	}
	authority := map[string]int{"official_rule": 5, "business_manual": 4, "experiment_report": 3, "analysis_report": 3, "operational_data": 3, "meeting_record": 2, "proposal": 2, "customer_feedback": 1, "personal_experience": 1, "unknown": 0, "": 0}
	if authority[candidateNature] < authority[existingNature] {
		return false
	}
	candidateTime, candidateTimeOK := metadataSourceTime(candidateMeta)
	existingTime, existingTimeOK := metadataSourceTime(existingMeta)
	if !candidateTimeOK || !existingTimeOK || !candidateTime.After(existingTime) {
		return false
	}
	// The explicit correction instruction must exist in the cited source text,
	// not merely in LLM-authored card prose. This prevents a generated phrase
	// such as "hereby corrected" from authorizing its own automatic publish.
	evidenceText := metadataEvidenceText(candidateMeta)
	cardText := candidate.Title + " " + candidate.Summary + " " + candidate.Content
	if !containsAnyFold(evidenceText, explicitCorrectionSignals) || containsAnyFold(cardText, uncertaintySignals) || containsAnyFold(cardText, highRiskSignals) {
		return false
	}
	return true
}

func classifyWikiChange(candidate, existing *types.WikiPage, reasons []string) string {
	hasReason := func(want string) bool {
		for _, reason := range reasons {
			if reason == want {
				return true
			}
		}
		return false
	}
	if hasReason("authoritative_explicit_correction") {
		return types.WikiChangeCategoryCorrection
	}
	if hasReason("cross_page_claim_conflict") || hasReason("cross_page_claim_uncertain") || hasReason("cross_page_claim_supersedes_existing") {
		return types.WikiChangeCategoryConflict
	}
	if hasReason("possible_duplicate") {
		return types.WikiChangeCategoryMergeDuplicate
	}
	if existing == nil {
		return types.WikiChangeCategoryAddition
	}
	if candidate != nil && (candidate.Status == types.WikiPageStatusArchived || candidate.MaturityStatus == types.WikiMaturityOutdated || candidate.MaturityStatus == types.WikiMaturityArchived) {
		return types.WikiChangeCategoryRetirement
	}
	if hasReason("card_conclusion_changed") || hasReason("knowledge_type_changed") || hasReason("risk_boundary_changed") || hasReason("rollback_requested") {
		return types.WikiChangeCategoryCorrection
	}
	return types.WikiChangeCategoryUpdate
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
		{"applicability", !governanceJSONEqual(a.Applicability, b.Applicability)},
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
	category := classifyWikiChange(candidate, existing, assessment.Reasons)
	set := &types.WikiChangeSet{ID: setID, TenantID: candidate.TenantID, KnowledgeBaseID: candidate.KnowledgeBaseID, KnowledgeID: knowledgeID, Status: types.WikiChangeSetPending, ReviewLevel: assessment.Level, ChangeCategory: category, Reasons: assessment.Reasons, ModelID: modelID, PromptVersion: wikiCardPromptVersion, CandidateFingerprint: fingerprint, CreatedAt: now, UpdatedAt: now}
	metadata, _ := candidate.PageMetadata.Map()
	excerpts, _ := json.Marshal(metadata["evidence_excerpts"])
	set.Items = []types.WikiChangeItem{{ID: uuid.NewString(), ChangeSetID: setID, Operation: operation, ChangeCategory: category, PageID: candidate.ID, PageSlug: candidate.Slug, ExpectedVersion: expectedVersion, Before: before, After: pageJSON(candidate), ChangedFields: changedCardFields(existing, candidate), EvidenceChunkIDs: candidate.ChunkRefs, EvidenceExcerpts: types.JSON(excerpts), CreatedAt: now}}
	if err := s.repo.CreateChangeSet(ctx, set); err != nil {
		return nil, err
	}
	if assessment.Level == types.WikiReviewLevelL0 {
		comment := "Automatically approved by L0 governance policy"
		decision := &types.WikiReviewDecision{Decision: types.WikiReviewApproved, Comment: comment}
		if category == types.WikiChangeCategoryCorrection {
			retained := strings.TrimSpace(candidate.Summary)
			if retained == "" {
				retained = strings.TrimSpace(candidate.Content)
			}
			decision.Resolution = types.WikiCorrectionAutoApplied
			decision.RetainedClaim = retained
			decision.Comment = "Automatically applied an explicit correction from a newer authoritative source"
		}
		if err := s.repo.ReviewChangeSet(ctx, candidate.KnowledgeBaseID, set.ID, "system", decision); err != nil {
			return nil, err
		}
		set.Status = types.WikiChangeSetApplied
	}
	s.requestGraphRefresh(ctx, candidate.TenantID, candidate.KnowledgeBaseID, []string{candidate.Slug})
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
func (s *wikiGovernanceService) ListPendingGraphCards(ctx context.Context, kbID string, limit int) ([]*types.WikiPendingGraphCard, error) {
	return s.repo.ListPendingGraphCards(ctx, kbID, limit)
}
func (s *wikiGovernanceService) UpdatePendingGraphCard(ctx context.Context, kbID, changeItemID string, page *types.WikiPage) (bool, error) {
	return s.repo.UpdatePendingGraphCard(ctx, kbID, changeItemID, page)
}
func (s *wikiGovernanceService) ReviewChangeSet(ctx context.Context, kbID, id, reviewerID string, d *types.WikiReviewDecision) error {
	set, err := s.repo.GetChangeSet(ctx, kbID, id)
	if err != nil {
		return err
	}
	if err := s.repo.ReviewChangeSet(ctx, kbID, id, reviewerID, d); err != nil {
		return err
	}
	slugs := make([]string, 0, len(set.Items))
	for _, item := range set.Items {
		slugs = append(slugs, item.PageSlug)
	}
	s.requestGraphRefresh(ctx, set.TenantID, kbID, slugs)
	return nil
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
func (s *wikiGovernanceService) SubmitFeedbackSignal(ctx context.Context, tenantID uint64, kbID, actor string, input *types.WikiFeedbackSignalInput) (*types.WikiFeedbackSignal, error) {
	return s.repo.SubmitFeedbackSignal(ctx, tenantID, kbID, actor, input)
}
func (s *wikiGovernanceService) ListFeedbackSignals(ctx context.Context, req types.WikiFeedbackSignalListRequest) ([]*types.WikiFeedbackSignal, int64, error) {
	return s.repo.ListFeedbackSignals(ctx, req)
}
func (s *wikiGovernanceService) GetFeedbackSignal(ctx context.Context, kbID, id string) (*types.WikiFeedbackSignal, error) {
	return s.repo.GetFeedbackSignal(ctx, kbID, id)
}
