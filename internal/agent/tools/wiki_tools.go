package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
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
			// Resolve OutLinks summaries to provide 1-hop context
			var outLinksDesc []string
			if len(page.OutLinks) > 0 {
				for _, outSlug := range page.OutLinks {
					if linkPage, err := t.wikiService.GetPageBySlug(ctx, kbID, outSlug); err == nil && linkPage != nil {
						outLinksDesc = append(outLinksDesc, fmt.Sprintf("[[%s]] (%s)", outSlug, linkPage.Summary))
					} else {
						outLinksDesc = append(outLinksDesc, fmt.Sprintf("[[%s]]", outSlug))
					}
				}
			} else {
				outLinksDesc = []string{"(none)"}
			}

			// Resolve InLinks summaries to provide reverse 1-hop context
			var inLinksDesc []string
			if len(page.InLinks) > 0 {
				for _, inSlug := range page.InLinks {
					if linkPage, err := t.wikiService.GetPageBySlug(ctx, kbID, inSlug); err == nil && linkPage != nil {
						inLinksDesc = append(inLinksDesc, fmt.Sprintf("[[%s]] (%s)", inSlug, linkPage.Summary))
					} else {
						inLinksDesc = append(inLinksDesc, fmt.Sprintf("[[%s]]", inSlug))
					}
				}
			} else {
				inLinksDesc = []string{"(none)"}
			}

			output := fmt.Sprintf(`<wiki_page>
<metadata>
<title>%s</title>
<slug>%s</slug>
<type>%s</type>
</metadata>
<relationships>
<links_to>%s</links_to>
<linked_from>%s</linked_from>
</relationships>
<content>
%s
</content>
</wiki_page>`,
				page.Title, page.Slug, page.PageType,
				strings.Join(outLinksDesc, ", "),
				strings.Join(inLinksDesc, ", "),
				page.Content,
			)
			return &types.ToolResult{Success: true, Output: output}, nil
		}
	}

	return &types.ToolResult{Success: false, Error: fmt.Sprintf("Wiki page '%s' not found", params.Slug)}, nil
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
			Output:  fmt.Sprintf("<search_results count=\"0\" query=\"%s\" />", params.Query),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<search_results count=\"%d\" query=\"%s\">\n", len(allPages), params.Query))
	for _, p := range allPages {
		fmt.Fprintf(&sb, "<page>\n<title>%s</title>\n<slug>%s</slug>\n<type>%s</type>\n<summary>%s</summary>\n</page>\n", p.Title, p.Slug, p.PageType, p.Summary)
	}
	sb.WriteString("</search_results>")

	return &types.ToolResult{Success: true, Output: sb.String()}, nil
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
