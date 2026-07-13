package tools

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWikiPageAllowedForAnswer(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	cases := []struct {
		name string
		page *types.WikiPage
		want bool
	}{
		{"legacy page remains available", &types.WikiPage{PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished}, true},
		{"pending card hidden", &types.WikiPage{PageType: types.WikiPageTypeCard, ReviewStatus: types.WikiReviewPending, MaturityStatus: types.WikiMaturityVerified}, false},
		{"draft card hidden", &types.WikiPage{PageType: types.WikiPageTypeCard, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityDraft}, false},
		{"approved verified card visible", &types.WikiPage{PageType: types.WikiPageTypeCard, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified}, true},
		{"future rule hidden", &types.WikiPage{PageType: types.WikiPageTypeCard, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, EffectiveFrom: &future}, false},
		{"expired rule hidden", &types.WikiPage{PageType: types.WikiPageTypeCard, ReviewStatus: types.WikiReviewApproved, MaturityStatus: types.WikiMaturityVerified, EffectiveTo: &past}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wikiPageAllowedForAnswer(tc.page, now); got != tc.want {
				t.Fatalf("allowed=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestWikiAnswerPolicyKeepsKnowledgeKindsDistinct(t *testing.T) {
	question := wikiAnswerPolicy(&types.WikiPage{PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeQuestion})
	hypothesis := wikiAnswerPolicy(&types.WikiPage{PageType: types.WikiPageTypeCard, KnowledgeType: types.WikiKnowledgeTypeHypothesis})
	if question == hypothesis || question == "" || hypothesis == "" {
		t.Fatal("question and hypothesis must produce distinct non-empty answer policies")
	}
}
