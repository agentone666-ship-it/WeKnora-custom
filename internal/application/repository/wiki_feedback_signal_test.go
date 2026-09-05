package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFeedbackCandidateNodeIDsExpandInPostgresINPredicate(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	require.NoError(t, err)

	ids := feedbackCandidateNodeIDs(&types.WikiFeedbackSignalInput{
		RecalledNodeIDs: types.StringArray{"page-1", "page-2"},
	})
	var pages []types.WikiPage
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Where("knowledge_base_id = ? AND status <> ? AND id IN ?", "kb-1", types.WikiPageStatusArchived, ids).
		Find(&pages).Statement

	require.Contains(t, stmt.SQL.String(), "id IN ($3,$4)")
	require.Equal(t, []any{"kb-1", types.WikiPageStatusArchived, "page-1", "page-2"}, stmt.Vars)
}

func TestFeedbackAffectedPagesUsesOnlyExplicitTargets(t *testing.T) {
	pages := []types.WikiPage{
		{ID: "target", Slug: "card/revenue", Title: "商业化核心指标", Content: "2026 年营收目标为 800 万元。", OutLinks: types.StringArray{"card/pricing"}},
		{ID: "related", Slug: "card/pricing", Title: "示例商城收费模式", Content: "示例商城商业化业务的收费规则。", InLinks: types.StringArray{"card/revenue"}},
		{ID: "generic", Slug: "card/platform", Title: "平台介绍", Content: "示例商城商业化业务介绍。"},
	}
	input := &types.WikiFeedbackSignalInput{
		FeedbackText:     "示例商城商业化业务 2026 年营收目标已调整为 1200 万元。",
		OriginalQuestion: "示例商城商业化业务今年的营收目标是多少？",
	}

	affected, reasons := feedbackAffectedPages(pages, types.StringArray{"target"}, input)

	require.Equal(t, []types.WikiPage{pages[0]}, affected)
	require.Equal(t, []string{"explicit_attribution"}, reasons["target"])
	require.NotContains(t, reasons, "related")
	require.NotContains(t, reasons, "generic")
}

func TestFeedbackAffectedPagesNarrowsOnlyWithinRecalledPages(t *testing.T) {
	pages := []types.WikiPage{
		{ID: "metrics", Slug: "card/metrics", Title: "商业化核心指标", Content: "2026 年营收目标为 800 万元。"},
		{ID: "pricing", Slug: "card/pricing", Title: "示例商城收费模式", Content: "商业化业务当前采用会员收费。"},
		{ID: "source", Slug: "source/interview", Title: "访谈来源", Content: "原文写明：2026 年营收目标为 800 万元。"},
	}
	input := &types.WikiFeedbackSignalInput{
		FeedbackText:     "商业化核心指标中的营收目标已经调整。",
		OriginalQuestion: "商业化核心指标里的 2026 年营收目标是多少？",
		AnswerExcerpt:    "2026 年营收目标为 800 万元。",
	}

	affected, reasons := feedbackAffectedPages(pages, nil, input)
	ids := make([]string, 0, len(affected))
	for _, page := range affected {
		ids = append(ids, page.ID)
	}

	require.ElementsMatch(t, []string{"metrics", "source"}, ids)
	require.Contains(t, reasons["metrics"], "recalled_page_mention:商业化核心指标")
	require.Contains(t, reasons["metrics"], "answer_excerpt_match")
	require.Contains(t, reasons["source"], "answer_excerpt_match")
	require.NotContains(t, ids, "pricing")
}

func TestFeedbackAffectedPagesLeavesUnclearFeedbackForManualAttribution(t *testing.T) {
	pages := []types.WikiPage{
		{ID: "pricing", Slug: "card/pricing", Title: "收费模式", Content: "会员收费规则。"},
		{ID: "refund", Slug: "card/refund", Title: "退款时效", Content: "退款三个工作日到账。"},
	}
	input := &types.WikiFeedbackSignalInput{
		FeedbackText:     "这个答案不准确，请核实。",
		OriginalQuestion: "具体规则是什么？",
	}

	affected, reasons := feedbackAffectedPages(pages, nil, input)

	require.Empty(t, affected)
	require.Empty(t, reasons)
}

func TestFeedbackCandidateContentDoesNotAppendUnmatchedCorrection(t *testing.T) {
	page := &types.WikiPage{Content: "原有知识内容。"}
	input := &types.WikiFeedbackSignalInput{
		AnswerExcerpt:       "回答里没有这段原文。",
		SuggestedCorrection: "这是新的反馈说法。",
	}

	require.Equal(t, page.Content, feedbackCandidateContent(page, input))
}

func TestFeedbackCandidateContentReplacesMatchedAnswerExcerpt(t *testing.T) {
	page := &types.WikiPage{Content: "退款通常三个工作日到账。"}
	input := &types.WikiFeedbackSignalInput{
		AnswerExcerpt:       "三个工作日",
		SuggestedCorrection: "五个工作日",
	}

	require.Equal(t, "退款通常五个工作日到账。", feedbackCandidateContent(page, input))
}
