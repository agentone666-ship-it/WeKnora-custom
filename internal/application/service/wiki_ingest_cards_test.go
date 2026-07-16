package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestNormalizeCrossPageAssessmentsAllowsCrossConceptConflicts(t *testing.T) {
	related := map[string]*types.WikiPage{
		"concept/refund-policy": {Slug: "concept/refund-policy"},
		"card/service-standard": {Slug: "card/service-standard"},
	}
	raw := `{
  "assessments": [
    {"related_slug":"concept/refund-policy","relation":"conflicting","candidate_claim":"3 days","existing_claim":"5 days","reason":"same rule, different value","confidence":0.94,"applicability_overlap":true},
    {"related_slug":"card/service-standard","relation":"conflicting","candidate_claim":"3 days domestic","existing_claim":"5 days overseas","reason":"different regions","confidence":0.90,"applicability_overlap":false},
    {"related_slug":"concept/hallucinated","relation":"conflicting","candidate_claim":"x","existing_claim":"y","reason":"invented","confidence":1,"applicability_overlap":true}
  ]
}`

	got, missing, err := normalizeCrossPageAssessments(raw, related)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("missing=%d, want 0", missing)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2: %+v", len(got), got)
	}
	if got[0].Relation != "conflicting" {
		t.Fatalf("overlapping relation=%s, want conflicting", got[0].Relation)
	}
	if got[1].Relation != "complementary" {
		t.Fatalf("disjoint relation=%s, want complementary", got[1].Relation)
	}
}

func TestApplyCrossPageReviewMetadataUsesRiskThresholds(t *testing.T) {
	metadata := map[string]any{}
	applyCrossPageReviewMetadata(metadata, []crossPageAssessment{
		{RelatedSlug: "concept/a", Relation: "conflicting", Confidence: 0.9, ApplicabilityOverlap: true},
		{RelatedSlug: "concept/b", Relation: "uncertain", Confidence: 0.7, ApplicabilityOverlap: true},
		{RelatedSlug: "concept/c", Relation: "supersedes", Confidence: 0.8},
	}, 0, nil)
	for _, key := range []string{"cross_page_conflict", "cross_page_uncertain", "cross_page_supersedes"} {
		if value, _ := metadata[key].(bool); !value {
			t.Fatalf("%s was not set: %#v", key, metadata)
		}
	}

	failed := map[string]any{}
	applyCrossPageReviewMetadata(failed, nil, 0, errors.New("upstream unavailable"))
	if value, _ := failed["cross_page_check_failed"].(bool); !value {
		t.Fatalf("failed check was not marked: %#v", failed)
	}
}

func TestCrossPageAssessmentMetadataIsJSONSerializable(t *testing.T) {
	metadata := map[string]any{}
	applyCrossPageReviewMetadata(metadata, []crossPageAssessment{{RelatedSlug: "card/a", Relation: "consistent", Confidence: 0.8}}, 1, nil)
	if _, err := json.Marshal(metadata); err != nil {
		t.Fatalf("metadata is not serializable: %v", err)
	}
	if value, _ := metadata["cross_page_check_incomplete"].(bool); !value {
		t.Fatalf("missing assessment must mark the check incomplete")
	}
}

func TestCrossPageQuerySignalsIncludeClaimAndScope(t *testing.T) {
	got := crossPageQuerySignals(scenarioCardCandidate{
		Title:           "退款处理时效",
		Statement:       "退款必须在三个工作日内完成",
		AffectedMetrics: []string{"退款完成时长"},
		Scenarios:       []string{"售后处理"},
	})
	if len(got) != 3 {
		t.Fatalf("signals=%v, want title + claim + scope", got)
	}
	if got[1] != "退款必须在三个工作日内完成" {
		t.Fatalf("claim signal=%q", got[1])
	}
	if got[2] != "退款完成时长 售后处理" {
		t.Fatalf("scope signal=%q", got[2])
	}
}

// fakeWikiPageService embeds the WikiPageService interface so it satisfies the
// contract while only overriding the two methods crossPageCorrectionTarget uses.
// Calling any other method panics (nil interface), which is fine for these
// focused tests.
type fakeWikiPageService struct {
	interfaces.WikiPageService
	related []*types.WikiPage
	bySlug  map[string]*types.WikiPage
}

func (f *fakeWikiPageService) FindRelatedPages(ctx context.Context, kbID, excludeSlug, query string, pageTypes []string, limit int) ([]*types.WikiPage, error) {
	return f.related, nil
}

func (f *fakeWikiPageService) GetPageBySlug(ctx context.Context, kbID, slug string) (*types.WikiPage, error) {
	if f.bySlug == nil {
		return nil, nil
	}
	return f.bySlug[slug], nil
}

func newDeterministicCorrectionService() (*wikiIngestService, *types.WikiPage) {
	baseline := &types.WikiPage{
		Slug:          "card/rule-服务响应时限标准",
		Title:         "服务响应时限标准",
		Summary:       "普通服务请求响应时限为3个工作日",
		Content:       "# 服务响应时限标准\n\n所有普通服务请求必须在3个工作日内完成响应",
		PageType:      types.WikiPageTypeCard,
		KnowledgeType: types.WikiKnowledgeTypeRule,
		ReviewStatus:  types.WikiReviewApproved,
		BusinessLine:  "customer-service",
		Applicability: types.JSON(`{"scope":"中国大陆客户服务团队已受理的普通服务请求"}`),
	}
	svc := &wikiIngestService{wikiService: &fakeWikiPageService{related: []*types.WikiPage{baseline}, bySlug: map[string]*types.WikiPage{baseline.Slug: baseline}}}
	return svc, baseline
}

func TestCrossPageCorrectionTargetDeterministicMatchSurvivesSlugMismatch(t *testing.T) {
	svc, baseline := newDeterministicCorrectionService()
	// The LLM emitted an English title for the correction, producing a different
	// slug than the Chinese baseline. No high-confidence LLM assessment exists.
	card := scenarioCardCandidate{
		KnowledgeType: types.WikiKnowledgeTypeRule,
		Title:         "Standard service response time for mainland China",
		Statement:     "服务响应时限现更正为5个工作日，原3个工作日正式废止，以本版本为准",
		Applicability: map[string]any{"scope": "中国大陆客户服务团队已受理的普通服务请求"},
	}
	evidence := "本文件是对旧制度的明确更正。服务响应时限现更正为5个工作日，原3个工作日的规定正式废止，以本版本为准。"
	target, method := svc.crossPageCorrectionTarget(context.Background(), "kb-1", "card/rule-standard-service-response-time", card, evidence, nil)
	if target == nil {
		t.Fatal("expected deterministic match to locate the baseline card despite slug mismatch")
	}
	if target.Slug != baseline.Slug {
		t.Fatalf("matched slug=%q, want baseline %q", target.Slug, baseline.Slug)
	}
	if method != "deterministic_content_match" {
		t.Fatalf("method=%q, want deterministic_content_match", method)
	}
}

func TestCrossPageCorrectionTargetRejectsUnrelatedSubject(t *testing.T) {
	svc, _ := newDeterministicCorrectionService()
	card := scenarioCardCandidate{
		KnowledgeType: types.WikiKnowledgeTypeRule,
		Title:         "新员工入职流程更正版",
		Statement:     "新员工入职流程现更正为3个步骤，原5个步骤的规定正式废止，以本版本为准",
		Applicability: map[string]any{"scope": "中国大陆客户服务团队已受理的普通服务请求"},
	}
	evidence := "本文件是对旧制度的明确更正。原5个步骤的规定正式废止，以本版本为准。"
	target, method := svc.crossPageCorrectionTarget(context.Background(), "kb-1", "card/rule-onboarding", card, evidence, nil)
	if target != nil {
		t.Fatalf("unrelated subject must not match, got slug=%q method=%q", target.Slug, method)
	}
	if method != "" {
		t.Fatalf("method=%q, want empty for no match", method)
	}
}

func TestCrossPageCorrectionTargetStillUsesHighConfidenceAssessment(t *testing.T) {
	svc, baseline := newDeterministicCorrectionService()
	card := scenarioCardCandidate{
		KnowledgeType: types.WikiKnowledgeTypeRule,
		Title:         "Standard service response time for mainland China",
		Statement:     "服务响应时限现更正为5个工作日，以本版本为准",
		Applicability: map[string]any{"scope": "中国大陆客户服务团队已受理的普通服务请求"},
	}
	assessments := []crossPageAssessment{{RelatedSlug: baseline.Slug, Relation: "supersedes", Confidence: 0.96, ApplicabilityOverlap: true}}
	evidence := "服务响应时限现更正为5个工作日，原3个工作日正式废止，以本版本为准。"
	target, method := svc.crossPageCorrectionTarget(context.Background(), "kb-1", "card/rule-standard-service-response-time", card, evidence, assessments)
	if target == nil || target.Slug != baseline.Slug {
		t.Fatalf("expected high-confidence assessment path to match baseline, got target=%v", target)
	}
	if method != "cross_page_authoritative_match" {
		t.Fatalf("method=%q, want cross_page_authoritative_match", method)
	}
}
