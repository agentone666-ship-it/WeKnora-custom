package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ---- wiki_read_page ----

type wikiReadPageTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
	seenLinks   map[string]bool
	mu          sync.Mutex
}

func NewWikiReadPageTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiReadPageTool{
		BaseTool: NewBaseTool(
			ToolWikiReadPage,
			`Read one or more wiki pages by their slugs. Returns the full markdown content, metadata, and links.
Use this to read specific wiki pages when you know their slug (e.g. "entity/acme-corp", "concept/rag").`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "slugs": {
      "type": "array",
      "items": { "type": "string" },
      "description": "List of wiki page slugs to read (e.g. ['entity/acme-corp', 'index'])"
    },
    "knowledge_base_id": {
      "type": "string",
      "description": "Optional: specific knowledge base ID. If omitted, searches all wiki KBs."
    }
  },
  "required": ["slugs"]
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
		seenLinks:   make(map[string]bool),
	}
}

func (t *wikiReadPageTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		Slug            any    `json:"slug"`
		Slugs           any    `json:"slugs"`
		KnowledgeBaseID string `json:"knowledge_base_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &types.ToolResult{Success: false, Error: "Invalid parameters: " + err.Error()}, nil
	}

	var slugsToFetch []string
	slugsToFetch = append(slugsToFetch, parseStringOrArray(params.Slugs)...)
	slugsToFetch = append(slugsToFetch, parseStringOrArray(params.Slug)...)

	if len(slugsToFetch) == 0 {
		return &types.ToolResult{Success: false, Error: "Missing 'slugs' parameter"}, nil
	}

	kbIDs := t.kbIDs
	if params.KnowledgeBaseID != "" {
		kbIDs = []string{params.KnowledgeBaseID}
	}

	var outputs []string
	var errs []string

	formatLinks := func(slugs []string, kbID string) []string {
		var descs []string
		for _, s := range slugs {
			t.mu.Lock()
			seen := t.seenLinks[s]
			t.seenLinks[s] = true
			t.mu.Unlock()

			if seen {
				// We already injected the summary for this link in this session
				descs = append(descs, fmt.Sprintf("[[%s]] (summary omitted, already seen)", s))
			} else {
				if linkPage, err := t.wikiService.GetPageBySlug(ctx, kbID, s); err == nil && linkPage != nil {
					descs = append(descs, fmt.Sprintf("[[%s]] (%s)", s, linkPage.Summary))
				} else {
					descs = append(descs, fmt.Sprintf("[[%s]]", s))
				}
			}
		}
		if len(descs) == 0 {
			return []string{"(none)"}
		}
		return descs
	}

	for _, slug := range slugsToFetch {
		found := false
		for _, kbID := range kbIDs {
			page, err := t.wikiService.GetPageBySlug(ctx, kbID, slug)
			if err == nil && page != nil {
				t.mu.Lock()
				t.seenLinks[slug] = true
				t.mu.Unlock()

				outLinksDesc := formatLinks(page.OutLinks, kbID)
				inLinksDesc := formatLinks(page.InLinks, kbID)

				// Render source refs
				var sourcesDesc []string
				if len(page.SourceRefs) > 0 {
					for _, ref := range page.SourceRefs {
						// SourceRefs might be "knowledgeID" or "knowledgeID|Title"
						kid := ref
						title := ""
						if pipeIdx := strings.Index(ref, "|"); pipeIdx > 0 {
							kid = ref[:pipeIdx]
							title = ref[pipeIdx+1:]
						}
						if title != "" {
							sourcesDesc = append(sourcesDesc, fmt.Sprintf(`<source knowledge_id="%s">%s</source>`, kid, title))
						} else {
							sourcesDesc = append(sourcesDesc, fmt.Sprintf(`<source knowledge_id="%s"/>`, kid))
						}
					}
				}

				output := fmt.Sprintf(`<wiki_page>
<metadata>
<title>%s</title>
<slug>%s</slug>
<type>%s</type>
<aliases>%s</aliases>
</metadata>
<relationships>
<links_to>%s</links_to>
<linked_from>%s</linked_from>
</relationships>
<sources>
%s
</sources>
<summary>
%s
</summary>
<content>
%s
</content>
</wiki_page>`,
					page.Title, page.Slug, page.PageType,
					strings.Join(page.Aliases, ", "),
					strings.Join(outLinksDesc, ", "),
					strings.Join(inLinksDesc, ", "),
					strings.Join(sourcesDesc, "\n"),
					page.Summary,
					page.Content,
				)
				outputs = append(outputs, output)
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("Wiki page '%s' not found", slug))
		}
	}

	if len(outputs) == 0 {
		return &types.ToolResult{Success: false, Error: strings.Join(errs, "; ")}, nil
	}

	finalOutput := strings.Join(outputs, "\n\n")
	if len(errs) > 0 {
		finalOutput += fmt.Sprintf("\n\n<errors>\n%s\n</errors>", strings.Join(errs, "\n"))
	}

	return &types.ToolResult{Success: true, Output: finalOutput}, nil
}

// ---- wiki_search ----

type wikiSearchTool struct {
	BaseTool
	wikiService interfaces.WikiPageService
	kbIDs       []string
	seenSlugs   map[string]bool
	mu          sync.Mutex
}

func NewWikiSearchTool(wikiService interfaces.WikiPageService, kbIDs []string) types.Tool {
	return &wikiSearchTool{
		BaseTool: NewBaseTool(
			ToolWikiSearch,
			`Search wiki pages using PostgreSQL POSIX regular expressions (~* operator, case-insensitive).
STRONGLY PREFER using regex to search for multiple concepts at once rather than simple plain text queries.
Returns matching pages with titles, slugs, and summaries.
Examples:
- Alternation (RECOMMENDED): "stardust|skyvault" (matches either word)
- Multiple terms (RECOMMENDED): "psionic.*engine" (matches both words in order)
- Prefix matching: "^entity/.*" (finds all entities)
- Plain text: "engine" (matches anywhere in title/content/slug/summary)
Use this to find relevant wiki pages when you don't know the exact slug.`,
			json.RawMessage(`{
  "type": "object",
  "properties": {
    "queries": {
      "type": "array",
      "items": { "type": "string" },
      "description": "List of regex search queries to run"
    },
    "limit": {
      "type": "integer",
      "description": "Max results to return per query (default 10)"
    }
  },
  "required": ["queries"]
}`),
		),
		wikiService: wikiService,
		kbIDs:       kbIDs,
		seenSlugs:   make(map[string]bool),
	}
}

func (t *wikiSearchTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var params struct {
		Query   any `json:"query"`
		Queries any `json:"queries"`
		Limit   int `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &types.ToolResult{Success: false, Error: "Invalid parameters: " + err.Error()}, nil
	}

	var queriesToRun []string
	queriesToRun = append(queriesToRun, parseStringOrArray(params.Queries)...)
	queriesToRun = append(queriesToRun, parseStringOrArray(params.Query)...)

	if len(queriesToRun) == 0 {
		return &types.ToolResult{Success: false, Error: "Missing 'queries' parameter"}, nil
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}

	var allOutputs []string

	for _, query := range queriesToRun {
		var allPages []*types.WikiPage
		for _, kbID := range t.kbIDs {
			pages, err := t.wikiService.SearchPages(ctx, kbID, query, params.Limit)
			if err == nil {
				allPages = append(allPages, pages...)
			}
		}

		if len(allPages) == 0 {
			allOutputs = append(allOutputs, fmt.Sprintf("<search_results count=\"0\" query=\"%s\" />", query))
			continue
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("<search_results count=\"%d\" query=\"%s\">\n", len(allPages), query))
		for _, p := range allPages {
			t.mu.Lock()
			seen := t.seenSlugs[p.Slug]
			t.seenSlugs[p.Slug] = true
			t.mu.Unlock()

			snippet := extractSnippet(p.Content, query)
			snippetTag := ""
			if snippet != "" {
				snippetTag = fmt.Sprintf("\n<match_snippet>%s</match_snippet>", snippet)
			}

			aliasesTag := ""
			if len(p.Aliases) > 0 {
				aliasesTag = fmt.Sprintf("\n<aliases>%s</aliases>", strings.Join(p.Aliases, ", "))
			}

			if seen {
				fmt.Fprintf(&sb, "<page>\n<title>%s</title>\n<slug>%s</slug>\n<type>%s</type>%s\n<summary>(summary omitted, already seen in previous search)</summary>%s\n</page>\n", p.Title, p.Slug, p.PageType, aliasesTag, snippetTag)
			} else {
				fmt.Fprintf(&sb, "<page>\n<title>%s</title>\n<slug>%s</slug>\n<type>%s</type>%s\n<summary>%s</summary>%s\n</page>\n", p.Title, p.Slug, p.PageType, aliasesTag, p.Summary, snippetTag)
			}
		}
		sb.WriteString("</search_results>")
		allOutputs = append(allOutputs, sb.String())
	}

	return &types.ToolResult{Success: true, Output: strings.Join(allOutputs, "\n\n")}, nil
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

func parseStringOrArray(val any) []string {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []interface{}:
		var res []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				res = append(res, s)
			}
		}
		return res
	}
	return nil
}

// resolveSourceRefs enriches plain knowledge UUIDs to "uuid|title" format.
// Refs already in "uuid|title" format are left unchanged.
func resolveSourceRefs(ctx context.Context, knowledgeService interfaces.KnowledgeService, refs []string) []string {
	if len(refs) == 0 || knowledgeService == nil {
		return refs
	}
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.Contains(ref, "|") {
			resolved = append(resolved, ref)
			continue
		}
		kn, err := knowledgeService.GetKnowledgeByIDOnly(ctx, ref)
		if err != nil || kn == nil {
			resolved = append(resolved, ref)
			continue
		}
		title := kn.Title
		if title == "" {
			title = kn.FileName
		}
		if title != "" {
			resolved = append(resolved, ref+"|"+title)
		} else {
			resolved = append(resolved, ref)
		}
	}
	return resolved
}

func extractSnippet(content string, query string) string {
	if content == "" || query == "" {
		return ""
	}
	re, err := regexp.Compile("(?i)" + query)
	if err != nil {
		return ""
	}
	loc := re.FindStringIndex(content)
	if loc == nil {
		return ""
	}

	matchStr := content[loc[0]:loc[1]]
	before := content[:loc[0]]
	after := content[loc[1]:]

	beforeRunes := []rune(before)
	if len(beforeRunes) > 60 {
		beforeRunes = beforeRunes[len(beforeRunes)-60:]
	}

	afterRunes := []rune(after)
	if len(afterRunes) > 60 {
		afterRunes = afterRunes[:60]
	}

	matchRunes := []rune(matchStr)
	if len(matchRunes) > 100 {
		matchRunes = append(matchRunes[:100], []rune("...")...)
	}

	snippet := string(beforeRunes) + string(matchRunes) + string(afterRunes)
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	for strings.Contains(snippet, "  ") {
		snippet = strings.ReplaceAll(snippet, "  ", " ")
	}

	return "... " + strings.TrimSpace(snippet) + " ..."
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
