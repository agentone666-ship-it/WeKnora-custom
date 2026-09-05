package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func governedCard(kind, content, nature string, withEvidence bool) *types.WikiPage {
	meta, _ := json.Marshal(map[string]any{"document_nature": nature})
	p := &types.WikiPage{
		PageType:      types.WikiPageTypeCard,
		KnowledgeType: kind,
		Title:         "test card",
		Content:       content,
		PageMetadata:  types.JSON(meta),
	}
	if withEvidence {
		p.ChunkRefs = types.StringArray{"chunk-1"}
	}
	return p
}

func TestAssessWikiCardChangeUsesHighestReviewLevel(t *testing.T) {
	cases := []struct {
		name      string
		candidate *types.WikiPage
		existing  *types.WikiPage
		want      string
	}{
		{"new question is L1", governedCard(types.WikiKnowledgeTypeQuestion, "what happened", "meeting_record", true), nil, types.WikiReviewLevelL1},
		{"new rule requires human review", governedCard(types.WikiKnowledgeTypeRule, "must do this", "official_rule", true), nil, types.WikiReviewLevelL1},
		{"uncertain fact requires human review", governedCard(types.WikiKnowledgeTypeKnowledge, "可能有效", "analysis_report", true), nil, types.WikiReviewLevelL1},
		{"nature mismatch requires human review", governedCard(types.WikiKnowledgeTypeRule, "always do this", "customer_feedback", true), nil, types.WikiReviewLevelL1},
		{"missing evidence is L1", governedCard(types.WikiKnowledgeTypeMetric, "conversion rate", "operational_data", false), nil, types.WikiReviewLevelL1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AssessWikiCardChange(tc.candidate, tc.existing).Level; got != tc.want {
				t.Fatalf("level=%s, want %s", got, tc.want)
			}
		})
	}
}

func TestAssessWikiCardChangeProtectsTypeAndConclusion(t *testing.T) {
	existing := governedCard(types.WikiKnowledgeTypeHypothesis, "maybe", "proposal", true)
	existing.Version = 3

	typeChanged := governedCard(types.WikiKnowledgeTypeKnowledge, "confirmed", "analysis_report", true)
	if got := AssessWikiCardChange(typeChanged, existing).Level; got != types.WikiReviewLevelL1 {
		t.Fatalf("type conversion level=%s, want L1", got)
	}

	same := *existing
	same.ChunkRefs = append(same.ChunkRefs, "chunk-2")
	if got := AssessWikiCardChange(&same, existing).Level; got != types.WikiReviewLevelL0 {
		t.Fatalf("evidence-only level=%s, want L0", got)
	}

	changed := *existing
	changed.Content = "different conclusion"
	if got := AssessWikiCardChange(&changed, existing).Level; got != types.WikiReviewLevelL1 {
		t.Fatalf("conclusion change level=%s, want L1", got)
	}
}

func TestAssessWikiCardChangeEscalatesAuthorityAndEvidenceLoss(t *testing.T) {
	existing := governedCard(types.WikiKnowledgeTypeKnowledge, "official conclusion", "official_rule", true)
	existing.BusinessLine = "sales"
	existing.ScenarioIDs = types.StringArray{"scenario-1"}
	candidate := governedCard(types.WikiKnowledgeTypeKnowledge, "meeting conclusion", "meeting_record", false)
	candidate.BusinessLine = existing.BusinessLine
	candidate.ScenarioIDs = existing.ScenarioIDs
	assessment := AssessWikiCardChange(candidate, existing)
	if assessment.Level != types.WikiReviewLevelL1 {
		t.Fatalf("level=%s, want L1", assessment.Level)
	}
	want := map[string]bool{"lower_authority_source_would_override_existing_card": false, "published_card_lost_all_evidence": false}
	for _, reason := range assessment.Reasons {
		if _, ok := want[reason]; ok {
			want[reason] = true
		}
	}
	for reason, found := range want {
		if !found {
			t.Fatalf("missing reason %q in %v", reason, assessment.Reasons)
		}
	}
}

func TestAssessWikiCardChangeForcesLegacyReclassificationReview(t *testing.T) {
	candidate := governedCard(types.WikiKnowledgeTypeMetric, "conversion rate definition", "operational_data", true)
	candidate.BusinessLine = "growth"
	candidate.ScenarioIDs = types.StringArray{"scenario-1"}
	metadata, _ := candidate.PageMetadata.Map()
	metadata["force_review"] = true
	raw, _ := json.Marshal(metadata)
	candidate.PageMetadata = types.JSON(raw)
	assessment := AssessWikiCardChange(candidate, nil)
	if assessment.Level != types.WikiReviewLevelL1 {
		t.Fatalf("level=%s, want L1", assessment.Level)
	}
	found := false
	for _, reason := range assessment.Reasons {
		found = found || reason == "legacy_reclassification"
	}
	if !found {
		t.Fatalf("missing legacy_reclassification reason: %v", assessment.Reasons)
	}
}

func TestAssessWikiCardChangeEscalatesCrossPageConflictSignals(t *testing.T) {
	for _, signal := range []string{
		"cross_page_conflict",
		"cross_page_uncertain",
		"cross_page_supersedes",
		"cross_page_check_failed",
		"cross_page_check_incomplete",
	} {
		t.Run(signal, func(t *testing.T) {
			candidate := governedCard(types.WikiKnowledgeTypeKnowledge, "neutral supported statement", "analysis_report", true)
			candidate.BusinessLine = "support"
			candidate.ScenarioIDs = types.StringArray{"scenario-1"}
			metadata, _ := candidate.PageMetadata.Map()
			metadata[signal] = true
			raw, _ := json.Marshal(metadata)
			candidate.PageMetadata = types.JSON(raw)
			assessment := AssessWikiCardChange(candidate, nil)
			if assessment.Level != types.WikiReviewLevelL1 {
				t.Fatalf("signal=%s level=%s, want L1; reasons=%v", signal, assessment.Level, assessment.Reasons)
			}
		})
	}
}

func TestDocumentNatureTypeMatrixRejectsDisallowedCards(t *testing.T) {
	for nature, allowed := range allowedTypesByNature {
		for knowledgeType := range validKnowledgeTypes {
			candidate := governedCard(knowledgeType, "neutral statement", nature, true)
			candidate.BusinessLine = "line"
			candidate.ScenarioIDs = types.StringArray{"scenario-1"}
			assessment := AssessWikiCardChange(candidate, nil)
			if !allowed[knowledgeType] && assessment.Level != types.WikiReviewLevelL1 {
				t.Fatalf("nature=%s type=%s level=%s, disallowed pair must be L1", nature, knowledgeType, assessment.Level)
			}
		}
	}
}

func TestClassifyWikiChangeSeparatesReviewLevelFromBusinessCategory(t *testing.T) {
	candidate := governedCard(types.WikiKnowledgeTypeRule, "new rule", "official_rule", true)
	if got := classifyWikiChange(candidate, nil, []string{"new_authoritative_card"}); got != types.WikiChangeCategoryAddition {
		t.Fatalf("new candidate category=%s, want addition", got)
	}
	if got := classifyWikiChange(candidate, nil, []string{"cross_page_claim_conflict"}); got != types.WikiChangeCategoryConflict {
		t.Fatalf("conflict category=%s, want conflict", got)
	}
	if got := classifyWikiChange(candidate, nil, []string{"cross_page_claim_uncertain"}); got != types.WikiChangeCategoryAddition {
		t.Fatalf("uncertain new candidate category=%s, want addition with L1 review", got)
	}
	existing := governedCard(types.WikiKnowledgeTypeRule, "old rule", "official_rule", true)
	if got := classifyWikiChange(candidate, existing, []string{"card_conclusion_changed"}); got != types.WikiChangeCategoryCorrection {
		t.Fatalf("correction category=%s, want correction", got)
	}
}

func TestPendingGraphGovernanceReclassifiesResolvedConflictAsAddition(t *testing.T) {
	candidate := governedCard(types.WikiKnowledgeTypeKnowledge, "稳定的业务说明", "business_manual", true)
	candidate.BusinessLine = "示例商城"
	candidate.ScenarioIDs = types.StringArray{"scenario-1"}
	metadata, err := json.Marshal(map[string]any{
		"document_nature": "business_manual",
		"cross_page_assessments": []map[string]any{{
			"related_slug": "card/knowledge-existing", "relation": "complementary",
			"existing_claim": "现有说法", "candidate_claim": "新增说法", "confidence": 0.9,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate.PageMetadata = types.JSON(metadata)

	governance := pendingGraphGovernance(candidate, nil, types.WikiReviewLevelL1)
	if governance.ChangeCategory != types.WikiChangeCategoryAddition {
		t.Fatalf("category=%s, want addition; reasons=%v", governance.ChangeCategory, governance.Reasons)
	}
	for _, reason := range governance.Reasons {
		if reason == "cross_page_claim_conflict" {
			t.Fatalf("stale conflict reason survived graph convergence: %v", governance.Reasons)
		}
	}
	if governance.ReviewLevel != types.WikiReviewLevelL1 {
		t.Fatalf("review level=%s, queued candidate must remain human-reviewed", governance.ReviewLevel)
	}
}

func TestOwnsGraphDerivedReviewEnvelopeOnlyForSingleIngestCandidate(t *testing.T) {
	ingest := &types.WikiChangeSet{
		PromptVersion:        wikiCardPromptVersion,
		CandidateFingerprint: "candidate-fingerprint",
		Items:                []types.WikiChangeItem{{ID: "item-1"}},
	}
	if !ownsGraphDerivedReviewEnvelope(ingest) {
		t.Fatal("single ingest candidate should own its graph-derived review envelope")
	}

	feedback := *ingest
	feedback.PromptVersion = "feedback-candidate-v2-full-page"
	if ownsGraphDerivedReviewEnvelope(&feedback) {
		t.Fatal("feedback candidate must preserve its correction review envelope")
	}

	rollback := *ingest
	rollback.CandidateFingerprint = ""
	if ownsGraphDerivedReviewEnvelope(&rollback) {
		t.Fatal("rollback candidate must preserve its rollback review envelope")
	}

	multiItem := *ingest
	multiItem.Items = append(multiItem.Items, types.WikiChangeItem{ID: "item-2"})
	if ownsGraphDerivedReviewEnvelope(&multiItem) {
		t.Fatal("multi-item candidate must not let one item overwrite the set review envelope")
	}
}

func setGovernanceSourceMetadata(t *testing.T, page *types.WikiPage, nature, sourceTime string, evidence ...string) {
	t.Helper()
	excerpts := map[string]string{}
	for index, excerpt := range evidence {
		excerpts[fmt.Sprintf("chunk-%d", index+1)] = excerpt
	}
	metadata, err := json.Marshal(map[string]any{"document_nature": nature, "source_updated_at": sourceTime, "evidence_excerpts": excerpts})
	if err != nil {
		t.Fatal(err)
	}
	page.PageMetadata = types.JSON(metadata)
}

func TestAssessWikiCardChangeAutoAppliesExplicitAuthoritativeCorrection(t *testing.T) {
	existing := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限为3个工作日", "official_rule", true)
	candidate := governedCard(types.WikiKnowledgeTypeRule, "本文件明确更正：服务响应时限现更正为5个工作日，以本版本为准", "official_rule", true)
	for _, page := range []*types.WikiPage{existing, candidate} {
		page.BusinessLine = "customer-service"
		page.AnswerStrength = types.WikiAnswerStrengthStrong
	}
	setGovernanceSourceMetadata(t, existing, "official_rule", "2026-07-01T00:00:00Z")
	setGovernanceSourceMetadata(t, candidate, "official_rule", "2026-07-15T00:00:00Z", candidate.Content)

	assessment := AssessWikiCardChange(candidate, existing)
	if assessment.Level != types.WikiReviewLevelL0 {
		t.Fatalf("level=%s reasons=%v, want L0 explicit correction", assessment.Level, assessment.Reasons)
	}
	found := false
	for _, reason := range assessment.Reasons {
		found = found || reason == "authoritative_explicit_correction"
	}
	if !found {
		t.Fatalf("missing automatic correction reason: %v", assessment.Reasons)
	}
	if got := classifyWikiChange(candidate, existing, assessment.Reasons); got != types.WikiChangeCategoryCorrection {
		t.Fatalf("category=%s, want correction", got)
	}
}

func TestGovernanceApplicabilityComparisonIgnoresJSONKeyOrder(t *testing.T) {
	left := types.JSON(`{"scope":"mainland","conditions":"accepted"}`)
	right := types.JSON(`{"conditions":"accepted","scope":"mainland"}`)
	if !governanceJSONEqual(left, right) {
		t.Fatal("equivalent applicability JSON must not be treated as a scope change")
	}
}

func TestAssessWikiCardChangeKeepsUnsafeCorrectionsInL1(t *testing.T) {
	newPair := func(content, sourceTime string) (*types.WikiPage, *types.WikiPage) {
		existing := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限为3个工作日", "official_rule", true)
		candidate := governedCard(types.WikiKnowledgeTypeRule, content, "official_rule", true)
		for _, page := range []*types.WikiPage{existing, candidate} {
			page.BusinessLine = "customer-service"
			page.AnswerStrength = types.WikiAnswerStrengthStrong
		}
		setGovernanceSourceMetadata(t, existing, "official_rule", "2026-07-10T00:00:00Z")
		setGovernanceSourceMetadata(t, candidate, "official_rule", sourceTime, content)
		return candidate, existing
	}
	cases := []struct {
		name, content, sourceTime string
	}{
		{"missing explicit correction signal", "服务响应时限调整为5个工作日", "2026-07-15T00:00:00Z"},
		{"source is not newer", "服务响应时限现更正为5个工作日", "2026-07-01T00:00:00Z"},
		{"high risk correction", "退款承诺现更正为5个工作日", "2026-07-15T00:00:00Z"},
		{"uncertain correction", "服务响应时限可能现更正为5个工作日", "2026-07-15T00:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate, existing := newPair(tc.content, tc.sourceTime)
			assessment := AssessWikiCardChange(candidate, existing)
			if assessment.Level != types.WikiReviewLevelL1 {
				t.Fatalf("level=%s reasons=%v, want L1", assessment.Level, assessment.Reasons)
			}
		})
	}
}

func TestAssessWikiCardChangeAutoCorrectionCanRetireDerivedOldPages(t *testing.T) {
	existing := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限为3个工作日", "official_rule", true)
	candidate := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限现更正为5个工作日，以本版本为准", "official_rule", true)
	for _, page := range []*types.WikiPage{existing, candidate} {
		page.BusinessLine = "customer-service"
		page.AnswerStrength = types.WikiAnswerStrengthStrong
	}
	existing.SourceRefs = types.StringArray{"old-document"}
	setGovernanceSourceMetadata(t, existing, "official_rule", "2026-07-01T00:00:00Z")
	metadata, _ := json.Marshal(map[string]any{
		"document_nature": "official_rule", "source_updated_at": "2026-07-15T00:00:00Z", "cross_page_supersedes": true,
		"evidence_excerpts":      map[string]string{"chunk-1": candidate.Content},
		"cross_page_assessments": []map[string]any{{"related_slug": "concept/response-time", "relation": "supersedes", "confidence": 0.98, "applicability_overlap": true}},
		"related_page_evidence":  map[string]any{"concept/response-time": map[string]any{"source_refs": []string{"old-document"}}},
	})
	candidate.PageMetadata = types.JSON(metadata)
	assessment := AssessWikiCardChange(candidate, existing)
	if assessment.Level != types.WikiReviewLevelL0 {
		t.Fatalf("level=%s reasons=%v, want safe derived-page correction to be L0", assessment.Level, assessment.Reasons)
	}
	if got := classifyWikiChange(candidate, existing, assessment.Reasons); got != types.WikiChangeCategoryCorrection {
		t.Fatalf("category=%s, want correction", got)
	}
}

func TestClaimOverlapScoreDistinguishesSameSubjectFromDifferent(t *testing.T) {
	same := claimOverlapScore(
		"服务响应时限现更正为5个工作日，原3个工作日正式废止",
		"所有普通服务请求必须在3个工作日内完成响应，服务响应时限标准",
	)
	if same < correctionClaimOverlapThreshold {
		t.Fatalf("same-subject overlap=%v, want >= %v", same, correctionClaimOverlapThreshold)
	}
	different := claimOverlapScore(
		"退款审批后5个工作日到账，原3个工作日正式废止",
		"新员工入职流程包括提交材料、开通账号和领取设备",
	)
	if different >= correctionClaimOverlapThreshold {
		t.Fatalf("different-subject overlap=%v, want < %v", different, correctionClaimOverlapThreshold)
	}
	if got := claimOverlapScore("a", "a b c"); got != 0 {
		t.Fatalf("short candidate overlap=%v, want 0", got)
	}
}

func TestCorrectionApplicabilityOverlapsHandlesValueEquality(t *testing.T) {
	if !correctionApplicabilityOverlaps(types.JSON(`{"scope":"中国大陆客户服务"}`), types.JSON(`{"region":"中国大陆客户服务"}`)) {
		t.Fatal("applicability blobs with equal string values must overlap even when keys differ")
	}
	if correctionApplicabilityOverlaps(types.JSON(`{"scope":"中国大陆"}`), types.JSON(`{"scope":"海外"}`)) {
		t.Fatal("disjoint applicability values must not overlap")
	}
	if !correctionApplicabilityOverlaps(types.JSON(`{}`), types.JSON(`{"scope":"mainland"}`)) {
		t.Fatal("empty applicability must be treated as undetermined overlap")
	}
}

// deterministicCorrectionPair builds an existing/candidate pair that is safe to
// auto-apply except for the cross-page evidence gate, plus the metadata needed
// to exercise the correction_target_method branching in isSafeAutomaticCorrection.
func deterministicCorrectionPair(t *testing.T, method string) (*types.WikiPage, *types.WikiPage) {
	t.Helper()
	existing := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限为3个工作日", "official_rule", true)
	candidate := governedCard(types.WikiKnowledgeTypeRule, "服务响应时限现更正为5个工作日，以本版本为准", "official_rule", true)
	for _, page := range []*types.WikiPage{existing, candidate} {
		page.BusinessLine = "customer-service"
		page.AnswerStrength = types.WikiAnswerStrengthStrong
		page.Applicability = types.JSON(`{"scope":"中国大陆客户服务团队已受理的普通服务请求"}`)
	}
	existing.SourceRefs = types.StringArray{"old-document"}
	setGovernanceSourceMetadata(t, existing, "official_rule", "2026-07-01T00:00:00Z")
	// A sub-0.9 supersedes assessment: under the strict LLM-evidence gate this
	// would block auto-apply; under the deterministic-match path it must not.
	metadata, _ := json.Marshal(map[string]any{
		"document_nature": "official_rule", "source_updated_at": "2026-07-15T00:00:00Z",
		"correction_target_method": method,
		"cross_page_supersedes":    true,
		"evidence_excerpts":        map[string]string{"chunk-1": "原3个工作日的规定正式废止，以本版本为准"},
		"cross_page_assessments":   []map[string]any{{"related_slug": existing.Slug, "relation": "supersedes", "confidence": 0.85, "applicability_overlap": true}},
		"related_page_evidence":    map[string]any{existing.Slug: map[string]any{"source_refs": []string{"old-document"}}},
	})
	candidate.PageMetadata = types.JSON(metadata)
	return candidate, existing
}

func TestAssessWikiCardChangeAutoAppliesDeterministicContentMatch(t *testing.T) {
	candidate, existing := deterministicCorrectionPair(t, "deterministic_content_match")
	assessment := AssessWikiCardChange(candidate, existing)
	if assessment.Level != types.WikiReviewLevelL0 {
		t.Fatalf("level=%s reasons=%v, want L0 for deterministic correction despite sub-0.9 assessment", assessment.Level, assessment.Reasons)
	}
	found := false
	for _, reason := range assessment.Reasons {
		found = found || reason == "authoritative_explicit_correction"
	}
	if !found {
		t.Fatalf("missing automatic correction reason: %v", assessment.Reasons)
	}
	if got := classifyWikiChange(candidate, existing, assessment.Reasons); got != types.WikiChangeCategoryCorrection {
		t.Fatalf("category=%s, want correction", got)
	}
}

func TestAssessWikiCardChangeKeepsLLMAssessedCorrectionUnderStrictEvidenceGate(t *testing.T) {
	// Same pair as above but matched via the LLM-assessment path: a sub-0.9
	// supersedes assessment must NOT auto-apply, proving the deterministic skip
	// is targeted rather than a blanket loosening of the evidence gate.
	candidate, existing := deterministicCorrectionPair(t, "cross_page_authoritative_match")
	assessment := AssessWikiCardChange(candidate, existing)
	if assessment.Level != types.WikiReviewLevelL1 {
		t.Fatalf("level=%s reasons=%v, want L1 when LLM assessment confidence is below 0.9", assessment.Level, assessment.Reasons)
	}
}
