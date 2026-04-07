package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// WikiLintIssueType defines the type of lint issue
type WikiLintIssueType string

const (
	LintIssueOrphanPage     WikiLintIssueType = "orphan_page"
	LintIssueBrokenLink     WikiLintIssueType = "broken_link"
	LintIssueStaleRef       WikiLintIssueType = "stale_ref"
	LintIssueMissingCrossRef WikiLintIssueType = "missing_cross_ref"
	LintIssueEmptyContent   WikiLintIssueType = "empty_content"
	LintIssueDuplicateSlug  WikiLintIssueType = "duplicate_slug"
)

// WikiLintIssueSeverity defines the severity of a lint issue
type WikiLintIssueSeverity string

const (
	SeverityInfo    WikiLintIssueSeverity = "info"
	SeverityWarning WikiLintIssueSeverity = "warning"
	SeverityError   WikiLintIssueSeverity = "error"
)

// WikiLintIssue represents a single lint finding
type WikiLintIssue struct {
	Type        WikiLintIssueType     `json:"type"`
	Severity    WikiLintIssueSeverity `json:"severity"`
	PageSlug    string                `json:"page_slug"`
	Description string                `json:"description"`
	AutoFixable bool                  `json:"auto_fixable"`
}

// WikiLintReport is the complete lint report for a wiki KB
type WikiLintReport struct {
	KnowledgeBaseID string           `json:"knowledge_base_id"`
	Issues          []WikiLintIssue  `json:"issues"`
	HealthScore     int              `json:"health_score"` // 0-100
	Stats           *types.WikiStats `json:"stats"`
	Summary         string           `json:"summary"`
}

// WikiLintService provides wiki health checking capabilities
type WikiLintService struct {
	wikiService interfaces.WikiPageService
	kbService   interfaces.KnowledgeBaseService
}

// NewWikiLintService creates a new wiki lint service
func NewWikiLintService(
	wikiService interfaces.WikiPageService,
	kbService interfaces.KnowledgeBaseService,
) *WikiLintService {
	return &WikiLintService{
		wikiService: wikiService,
		kbService:   kbService,
	}
}

// RunLint performs a comprehensive health check on a wiki knowledge base
func (s *WikiLintService) RunLint(ctx context.Context, kbID string) (*WikiLintReport, error) {
	// Validate KB
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get KB: %w", err)
	}
	if kb.Type != types.KnowledgeBaseTypeWiki {
		return nil, fmt.Errorf("KB %s is not a wiki type", kbID)
	}

	// Get stats
	stats, err := s.wikiService.GetStats(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	// Get graph for link analysis
	graph, err := s.wikiService.GetGraph(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}

	// Get all pages for detailed analysis
	resp, err := s.wikiService.ListPages(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: kbID,
		PageSize:        500,
	})
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}

	var issues []WikiLintIssue
	healthScore := 100

	// Build slug set for link validation
	slugSet := make(map[string]bool)
	for _, node := range graph.Nodes {
		slugSet[node.Slug] = true
	}

	// Check 1: Orphan pages (no inbound links, excluding index/log)
	for _, page := range resp.Pages {
		if page.PageType == types.WikiPageTypeIndex || page.PageType == types.WikiPageTypeLog {
			continue
		}
		if len(page.InLinks) == 0 {
			issues = append(issues, WikiLintIssue{
				Type:        LintIssueOrphanPage,
				Severity:    SeverityWarning,
				PageSlug:    page.Slug,
				Description: fmt.Sprintf("Page '%s' has no inbound links — it's disconnected from the wiki", page.Title),
				AutoFixable: false,
			})
		}
	}

	// Check 2: Broken links
	for _, page := range resp.Pages {
		for _, outLink := range page.OutLinks {
			if !slugSet[outLink] {
				issues = append(issues, WikiLintIssue{
					Type:        LintIssueBrokenLink,
					Severity:    SeverityError,
					PageSlug:    page.Slug,
					Description: fmt.Sprintf("Page '%s' links to [[%s]] which does not exist", page.Title, outLink),
					AutoFixable: true,
				})
			}
		}
	}

	// Check 3: Empty content
	for _, page := range resp.Pages {
		content := strings.TrimSpace(page.Content)
		if len(content) < 50 {
			issues = append(issues, WikiLintIssue{
				Type:        LintIssueEmptyContent,
				Severity:    SeverityWarning,
				PageSlug:    page.Slug,
				Description: fmt.Sprintf("Page '%s' has very little content (%d chars)", page.Title, len(content)),
				AutoFixable: true,
			})
		}
	}

	// Check 4: Missing cross-references (entities mentioned in content but not linked)
	entitySlugs := make(map[string]string) // slug -> title
	for _, page := range resp.Pages {
		if page.PageType == types.WikiPageTypeEntity || page.PageType == types.WikiPageTypeConcept {
			entitySlugs[page.Slug] = page.Title
		}
	}
	for _, page := range resp.Pages {
		for slug, title := range entitySlugs {
			if slug == page.Slug {
				continue
			}
			// Check if title is mentioned in content but not linked
			if strings.Contains(strings.ToLower(page.Content), strings.ToLower(title)) {
				linked := false
				for _, l := range page.OutLinks {
					if l == slug {
						linked = true
						break
					}
				}
				if !linked {
					issues = append(issues, WikiLintIssue{
						Type:        LintIssueMissingCrossRef,
						Severity:    SeverityInfo,
						PageSlug:    page.Slug,
						Description: fmt.Sprintf("Page '%s' mentions '%s' but doesn't link to [[%s]]", page.Title, title, slug),
						AutoFixable: true,
					})
				}
			}
		}
	}

	// Calculate health score
	if stats.TotalPages > 0 {
		// Penalize for orphans
		orphanPct := float64(stats.OrphanCount) / float64(stats.TotalPages) * 100
		if orphanPct > 50 {
			healthScore -= 25
		} else if orphanPct > 25 {
			healthScore -= 10
		}

		// Penalize for broken links
		brokenCount := 0
		for _, issue := range issues {
			if issue.Type == LintIssueBrokenLink {
				brokenCount++
			}
		}
		healthScore -= brokenCount * 5

		// Penalize for no links at all
		if stats.TotalLinks == 0 && stats.TotalPages > 2 {
			healthScore -= 15
		}

		// Penalize for empty pages
		emptyCount := 0
		for _, issue := range issues {
			if issue.Type == LintIssueEmptyContent {
				emptyCount++
			}
		}
		healthScore -= emptyCount * 3
	}

	if healthScore < 0 {
		healthScore = 0
	}

	// Generate summary
	var summary strings.Builder
	errorCount := 0
	warningCount := 0
	infoCount := 0
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			errorCount++
		case SeverityWarning:
			warningCount++
		case SeverityInfo:
			infoCount++
		}
	}

	if len(issues) == 0 {
		summary.WriteString("Wiki is healthy! No issues found.")
	} else {
		fmt.Fprintf(&summary, "Found %d issues: %d errors, %d warnings, %d suggestions.",
			len(issues), errorCount, warningCount, infoCount)
	}

	report := &WikiLintReport{
		KnowledgeBaseID: kbID,
		Issues:          issues,
		HealthScore:     healthScore,
		Stats:           stats,
		Summary:         summary.String(),
	}

	logger.Infof(ctx, "wiki lint: KB %s — health score %d/100, %d issues", kbID, healthScore, len(issues))

	return report, nil
}

// AutoFix attempts to automatically fix fixable issues
func (s *WikiLintService) AutoFix(ctx context.Context, kbID string) (int, error) {
	report, err := s.RunLint(ctx, kbID)
	if err != nil {
		return 0, err
	}

	fixed := 0
	for _, issue := range report.Issues {
		if !issue.AutoFixable {
			continue
		}

		switch issue.Type {
		case LintIssueBrokenLink:
			// Remove broken links from page content
			page, err := s.wikiService.GetPageBySlug(ctx, kbID, issue.PageSlug)
			if err != nil {
				continue
			}
			// Extract the broken slug from the description
			parts := strings.Split(issue.Description, "[[")
			if len(parts) < 2 {
				continue
			}
			brokenSlug := strings.Split(parts[1], "]]")[0]
			// Remove the [[broken-link]] from content
			page.Content = strings.ReplaceAll(page.Content, "[["+brokenSlug+"]]", brokenSlug)
			if _, err := s.wikiService.UpdatePage(ctx, page); err == nil {
				fixed++
			}

		case LintIssueEmptyContent:
			// Archive pages with very little content instead of deleting
			page, err := s.wikiService.GetPageBySlug(ctx, kbID, issue.PageSlug)
			if err != nil {
				continue
			}
			// Don't archive index or log pages
			if page.PageType == types.WikiPageTypeIndex || page.PageType == types.WikiPageTypeLog {
				continue
			}
			page.Status = types.WikiPageStatusArchived
			if _, err := s.wikiService.UpdatePage(ctx, page); err == nil {
				fixed++
			}
		}
	}

	// Rebuild links after fixes
	if fixed > 0 {
		_ = s.wikiService.RebuildLinks(ctx, kbID)
	}

	logger.Infof(ctx, "wiki auto-fix: KB %s — fixed %d issues", kbID, fixed)
	return fixed, nil
}
