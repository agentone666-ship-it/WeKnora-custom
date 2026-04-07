package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// ---- wiki_read_page ----

type wikiReadPageTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
}

func NewWikiReadPageTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiReadPageTool{
		BaseTool: NewBaseTool(
			ToolWikiReadPage,
			`Read a wiki page by its slug. Returns the full markdown content, metadata, and links.
Use this to read specific wiki pages when you know their slug (e.g. "entity/acme-corp", "concept/rag", "summary/document-title").`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "slug": {
      "type": "string",
      "description": "The wiki page slug to read (e.g. 'entity/acme-corp', 'concept/rag')"
    },
    "knowledge_base_id": {
      "type": "string",
      "description": "Optional: specific knowledge base ID. If omitted, searches all wiki KBs."
    }
  },
  "required": ["slug"]
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
	}
}

func (t *wikiReadPageTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		Slug            string `json:"slug"`
		KnowledgeBaseID string `json:"knowledge_base_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &types.ToolResult{Success: false, Error: "Invalid parameters: " + err.Error()}, nil
	}

	kbIDs := t.kbIDs
	if params.KnowledgeBaseID != "" {
		kbIDs = []string{params.KnowledgeBaseID}
	}

	for _, kbID := range kbIDs {
		page, err := t.wikiService.GetPageBySlug(ctx, kbID, params.Slug)
		if err == nil && page != nil {
			output := fmt.Sprintf("# %s\n**Type**: %s | **Version**: %d | **Updated**: %s\n**Links to**: %s\n**Linked from**: %s\n\n---\n\n%s",
				page.Title, page.PageType, page.Version, page.UpdatedAt.Format("2006-01-02"),
				strings.Join(page.OutLinks, ", "),
				strings.Join(page.InLinks, ", "),
				page.Content,
			)
			return &types.ToolResult{Success: true, Output: output}, nil
		}
	}

	return &types.ToolResult{Success: false, Error: fmt.Sprintf("Wiki page '%s' not found", params.Slug)}, nil
}

// ---- wiki_write_page ----

type wikiWritePageTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
	tenantID    uint64
}

func NewWikiWritePageTool(wikiService interfaces.WikiPageService, kbIDs []string, tenantID uint64) types.Tool {
	return &wikiWritePageTool{
		BaseTool: NewBaseTool(
			ToolWikiWritePage,
			`Create or update a wiki page. Use this to save valuable analysis, synthesis, or new knowledge into the wiki.
The page content should be in Markdown format. Use [[slug]] syntax to create links between pages.`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "slug": {
      "type": "string",
      "description": "Page slug (e.g. 'synthesis/quarterly-review', 'comparison/tool-a-vs-tool-b')"
    },
    "title": {
      "type": "string",
      "description": "Human-readable page title"
    },
    "content": {
      "type": "string",
      "description": "Full Markdown content of the page. Use [[slug]] for wiki links."
    },
    "page_type": {
      "type": "string",
      "enum": ["summary", "entity", "concept", "synthesis", "comparison"],
      "description": "Type of wiki page"
    },
    "knowledge_base_id": {
      "type": "string",
      "description": "Target knowledge base ID. Required if multiple wiki KBs are available."
    }
  },
  "required": ["slug", "title", "content", "page_type"]
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
		tenantID:    tenantID,
	}
}

func (t *wikiWritePageTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		Slug            string `json:"slug"`
		Title           string `json:"title"`
		Content         string `json:"content"`
		PageType        string `json:"page_type"`
		KnowledgeBaseID string `json:"knowledge_base_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &types.ToolResult{Success: false, Error: "Invalid parameters: " + err.Error()}, nil
	}

	kbID := params.KnowledgeBaseID
	if kbID == "" && len(t.kbIDs) > 0 {
		kbID = t.kbIDs[0]
	}
	if kbID == "" {
		return &types.ToolResult{Success: false, Error: "No wiki knowledge base available"}, nil
	}

	// Check if page exists (update) or new (create)
	existing, err := t.wikiService.GetPageBySlug(ctx, kbID, params.Slug)
	if err == nil && existing != nil {
		existing.Title = params.Title
		existing.Content = params.Content
		existing.PageType = params.PageType
		existing.Summary = truncateForSummary(params.Content, 200)

		if _, err := t.wikiService.UpdatePage(ctx, existing); err != nil {
			return &types.ToolResult{Success: false, Error: "Failed to update page: " + err.Error()}, nil
		}
		return &types.ToolResult{
			Success: true,
			Output:  fmt.Sprintf("Updated wiki page [[%s]] (v%d)", params.Slug, existing.Version),
		}, nil
	}

	// Create new page
	page := &types.WikiPage{
		ID:              uuid.New().String(),
		TenantID:        t.tenantID,
		KnowledgeBaseID: kbID,
		Slug:            params.Slug,
		Title:           params.Title,
		Content:         params.Content,
		PageType:        params.PageType,
		Status:          types.WikiPageStatusPublished,
		Summary:         truncateForSummary(params.Content, 200),
	}

	if _, err := t.wikiService.CreatePage(ctx, page); err != nil {
		return &types.ToolResult{Success: false, Error: "Failed to create page: " + err.Error()}, nil
	}

	return &types.ToolResult{
		Success: true,
		Output:  fmt.Sprintf("Created wiki page [[%s]] — %s", params.Slug, params.Title),
	}, nil
}

// ---- wiki_search ----

type wikiSearchTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
}

func NewWikiSearchTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiSearchTool{
		BaseTool: NewBaseTool(
			ToolWikiSearch,
			`Search wiki pages by keyword. Returns matching pages with titles, slugs, and summaries.
Use this to find relevant wiki pages when you don't know the exact slug.`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Search query (keywords or phrase)"
    },
    "limit": {
      "type": "integer",
      "description": "Max results to return (default 10)"
    }
  },
  "required": ["query"]
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
	}
}

func (t *wikiSearchTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &types.ToolResult{Success: false, Error: "Invalid parameters: " + err.Error()}, nil
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}

	var allPages []*types.WikiPage
	for _, kbID := range t.kbIDs {
		pages, err := t.wikiService.SearchPages(ctx, kbID, params.Query, params.Limit)
		if err == nil {
			allPages = append(allPages, pages...)
		}
	}

	if len(allPages) == 0 {
		return &types.ToolResult{
			Success: true,
			Output:  fmt.Sprintf("No wiki pages found matching '%s'", params.Query),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d wiki pages:\n\n", len(allPages)))
	for _, p := range allPages {
		fmt.Fprintf(&sb, "- **[[%s]]** (%s) — %s\n", p.Slug, p.PageType, p.Summary)
	}

	return &types.ToolResult{Success: true, Output: sb.String()}, nil
}

// ---- wiki_read_index ----

type wikiReadIndexTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
}

func NewWikiReadIndexTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiReadIndexTool{
		BaseTool: NewBaseTool(
			ToolWikiReadIndex,
			`Read the wiki index page. The index lists all wiki pages organized by category.
Use this first to understand what knowledge is available in the wiki before searching or reading specific pages.`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "knowledge_base_id": {
      "type": "string",
      "description": "Optional: specific knowledge base ID"
    }
  }
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
	}
}

func (t *wikiReadIndexTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
	}
	_ = json.Unmarshal(args, &params)

	kbIDs := t.kbIDs
	if params.KnowledgeBaseID != "" {
		kbIDs = []string{params.KnowledgeBaseID}
	}

	var output strings.Builder
	for _, kbID := range kbIDs {
		indexPage, err := t.wikiService.GetIndex(ctx, kbID)
		if err == nil && indexPage != nil {
			if len(kbIDs) > 1 {
				fmt.Fprintf(&output, "## Wiki Index (KB: %s)\n\n", kbID)
			}
			output.WriteString(indexPage.Content)
			output.WriteString("\n\n")
		}
	}

	if output.Len() == 0 {
		return &types.ToolResult{Success: true, Output: "No wiki index found. The wiki may be empty."}, nil
	}

	return &types.ToolResult{Success: true, Output: output.String()}, nil
}

// ---- wiki_lint ----

type wikiLintTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
}

func NewWikiLintTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiLintTool{
		BaseTool: NewBaseTool(
			ToolWikiLint,
			`Check the health of the wiki. Reports issues like orphan pages, broken links, and provides statistics.
Use this to identify maintenance tasks and ensure wiki quality.`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "knowledge_base_id": {
      "type": "string",
      "description": "Optional: specific knowledge base ID"
    }
  }
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
	}
}

func (t *wikiLintTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
	}
	_ = json.Unmarshal(args, &params)

	kbIDs := t.kbIDs
	if params.KnowledgeBaseID != "" {
		kbIDs = []string{params.KnowledgeBaseID}
	}

	var output strings.Builder
	for _, kbID := range kbIDs {
		stats, err := t.wikiService.GetStats(ctx, kbID)
		if err != nil {
			fmt.Fprintf(&output, "## Wiki Health Check (KB: %s)\nError: %v\n\n", kbID, err)
			continue
		}

		graph, _ := t.wikiService.GetGraph(ctx, kbID)

		fmt.Fprintf(&output, "## Wiki Health Check (KB: %s)\n\n", kbID)
		fmt.Fprintf(&output, "### Statistics\n")
		fmt.Fprintf(&output, "- **Total pages**: %d\n", stats.TotalPages)
		for pt, count := range stats.PagesByType {
			fmt.Fprintf(&output, "  - %s: %d\n", pt, count)
		}
		fmt.Fprintf(&output, "- **Total links**: %d\n", stats.TotalLinks)
		fmt.Fprintf(&output, "- **Orphan pages** (no inbound links): %d\n", stats.OrphanCount)

		// Health score (simple heuristic)
		healthScore := 100
		if stats.TotalPages > 0 {
			orphanPct := float64(stats.OrphanCount) / float64(stats.TotalPages) * 100
			if orphanPct > 50 {
				healthScore -= 30
			} else if orphanPct > 25 {
				healthScore -= 15
			}
		}
		if stats.TotalLinks == 0 && stats.TotalPages > 2 {
			healthScore -= 20
		}

		// Check for broken links
		brokenLinks := 0
		if graph != nil {
			slugSet := make(map[string]bool)
			for _, n := range graph.Nodes {
				slugSet[n.Slug] = true
			}
			for _, e := range graph.Edges {
				if !slugSet[e.Target] {
					brokenLinks++
				}
			}
		}
		if brokenLinks > 0 {
			healthScore -= brokenLinks * 5
			fmt.Fprintf(&output, "- **Broken links**: %d\n", brokenLinks)
		}

		if healthScore < 0 {
			healthScore = 0
		}
		fmt.Fprintf(&output, "\n### Health Score: %d/100\n\n", healthScore)

		// Suggestions
		fmt.Fprintf(&output, "### Suggestions\n")
		if stats.OrphanCount > 0 {
			fmt.Fprintf(&output, "- Link orphan pages from related entity/concept pages\n")
		}
		if brokenLinks > 0 {
			fmt.Fprintf(&output, "- Fix or remove %d broken [[wiki-link]] references\n", brokenLinks)
		}
		if stats.TotalPages < 3 {
			fmt.Fprintf(&output, "- Wiki is sparse — consider ingesting more documents\n")
		}
		output.WriteString("\n")
	}

	return &types.ToolResult{Success: true, Output: output.String()}, nil
}

// --- Helper ---

func truncateForSummary(content string, maxLen int) string {
	// Take first paragraph or first maxLen chars
	lines := strings.SplitN(content, "\n\n", 2)
	summary := strings.TrimSpace(lines[0])
	summary = strings.TrimPrefix(summary, "# ")
	summary = strings.TrimPrefix(summary, "## ")
	runes := []rune(summary)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return summary
}
