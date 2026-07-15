package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
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
