package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerWikiAgentTools exposes the same read-only Wiki surface used by the
// built-in Wiki Agent. External agents can therefore perform their own tool
// loop without nesting a call to /agent-chat.
func registerWikiAgentTools(s *server.MCPServer, store *adminStore, client *apiClient) {
	s.AddTool(mcp.NewTool("wiki_search",
		mcp.WithDescription("Search Wiki pages using the same regex search surface as the built-in WeKnora Wiki Agent. Read matching pages with wiki_read_page before answering."),
		mcp.WithArray("queries", mcp.Required(), mcp.WithStringItems(), mcp.Description("Case-insensitive PostgreSQL regular-expression queries.")),
		mcp.WithInteger("limit", mcp.DefaultNumber(10), mcp.Description("Maximum results per query.")),
		mcp.WithString("knowledge_base_id", mcp.Description("Wiki knowledge base ID; may be omitted when the MCP server is bound to one KB.")),
	), auditTool(store, "wiki_search", client.knowledgeBaseID, client.wikiSearch))

	s.AddTool(mcp.NewTool("wiki_read_page",
		mcp.WithDescription("Read full Wiki pages, metadata, relationships, source references, and answer-governance fields by slug. This is the same primary evidence surface used by the built-in Wiki Agent."),
		mcp.WithArray("slugs", mcp.Required(), mcp.WithStringItems(), mcp.Description("Wiki page slugs, for example entity/acme-corp or index.")),
		mcp.WithString("knowledge_base_id", mcp.Description("Wiki knowledge base ID; may be omitted when the MCP server is bound to one KB.")),
	), auditTool(store, "wiki_read_page", client.knowledgeBaseID, client.wikiReadPage))

	s.AddTool(mcp.NewTool("wiki_read_source_doc",
		mcp.WithDescription("Drill into a source document referenced by a Wiki page. Search chunks with a case-insensitive regex or read a contiguous 1-based chunk range."),
		mcp.WithString("knowledge_id", mcp.Required(), mcp.Description("Source knowledge ID from a Wiki page's source_refs.")),
		mcp.WithString("query", mcp.Description("Optional case-insensitive regular expression used to filter chunks.")),
		mcp.WithInteger("start_chunk_index", mcp.Description("Optional 1-based first chunk index.")),
		mcp.WithInteger("end_chunk_index", mcp.Description("Optional 1-based last chunk index; at most 50 chunks are returned.")),
	), auditTool(store, "wiki_read_source_doc", client.knowledgeBaseID, client.wikiReadSourceDoc))
}

func (c *apiClient) wikiSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	queries := cleanStringSlice(request.GetStringSlice("queries", nil))
	if len(queries) == 0 {
		return mcp.NewToolResultError("queries is required"), nil
	}
	limit := request.GetInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}
	results := make([]json.RawMessage, 0, len(queries))
	for _, query := range queries {
		values := url.Values{"q": []string{query}, "limit": []string{strconv.Itoa(limit)}}
		data, _, reqErr := c.request(ctx, http.MethodGet, "/knowledgebase/"+url.PathEscape(kb)+"/wiki/search?"+values.Encode(), nil)
		if reqErr != nil {
			return officialToolResult(nil, reqErr)
		}
		results = append(results, json.RawMessage(data))
	}
	data, err := json.Marshal(map[string]any{"knowledge_base_id": kb, "results": results})
	return officialToolResult(data, err)
}

func (c *apiClient) wikiReadPage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	slugs := cleanStringSlice(request.GetStringSlice("slugs", nil))
	if len(slugs) == 0 {
		return mcp.NewToolResultError("slugs is required"), nil
	}
	pages := make([]json.RawMessage, 0, len(slugs))
	for _, slug := range slugs {
		segments := strings.Split(strings.Trim(slug, "/"), "/")
		for i := range segments {
			segments[i] = url.PathEscape(segments[i])
		}
		if len(segments) == 0 || segments[0] == "" {
			return mcp.NewToolResultError("wiki page slug cannot be empty"), nil
		}
		data, _, reqErr := c.request(ctx, http.MethodGet, "/knowledgebase/"+url.PathEscape(kb)+"/wiki/pages/"+strings.Join(segments, "/"), nil)
		if reqErr != nil {
			return officialToolResult(nil, reqErr)
		}
		pages = append(pages, json.RawMessage(data))
	}
	data, err := json.Marshal(map[string]any{"knowledge_base_id": kb, "pages": pages})
	return officialToolResult(data, err)
}

type wikiSourceChunk struct {
	ChunkIndex int    `json:"chunk_index"`
	Content    string `json:"content"`
}

func (c *apiClient) wikiReadSourceDoc(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	knowledgeID, err := requireKnowledgeID(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := c.authorizeKnowledgeID(ctx, knowledgeID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := strings.TrimSpace(request.GetString("query", ""))
	start := request.GetInt("start_chunk_index", 0)
	end := request.GetInt("end_chunk_index", 0)
	if start < 0 || end < 0 {
		return mcp.NewToolResultError("chunk indexes cannot be negative"), nil
	}
	if start > 0 {
		if end < start {
			end = start + 10
		}
		if end-start > 50 {
			end = start + 50
		}
	}
	var matcher *regexp.Regexp
	if query != "" && start == 0 {
		matcher, err = regexp.Compile("(?i)" + query)
		if err != nil {
			return mcp.NewToolResultError("invalid regex query: " + err.Error()), nil
		}
	}

	meta, _, err := c.request(ctx, http.MethodGet, "/knowledge/"+url.PathEscape(knowledgeID), nil)
	if err != nil {
		return officialToolResult(nil, err)
	}
	matched := make([]wikiSourceChunk, 0)
	total := 0
	for page := 1; ; page++ {
		path := "/chunks/" + url.PathEscape(knowledgeID) + "?page=" + strconv.Itoa(page) + "&page_size=100"
		data, _, reqErr := c.request(ctx, http.MethodGet, path, nil)
		if reqErr != nil {
			return officialToolResult(nil, reqErr)
		}
		var envelope struct {
			Data  []wikiSourceChunk `json:"data"`
			Total int               `json:"total"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return officialToolResult(nil, fmt.Errorf("parse source chunks: %w", err))
		}
		total = envelope.Total
		for _, chunk := range envelope.Data {
			oneBased := chunk.ChunkIndex + 1
			if start > 0 && (oneBased < start || oneBased > end) {
				continue
			}
			if matcher != nil && !matcher.MatchString(chunk.Content) {
				continue
			}
			matched = append(matched, chunk)
			if matcher != nil && len(matched) >= 50 {
				break
			}
		}
		if (matcher != nil && len(matched) >= 50) || page*100 >= envelope.Total || len(envelope.Data) == 0 || (start > 0 && page*100 >= end) {
			break
		}
	}
	if len(matched) == 0 && total == 0 {
		return mcp.NewToolResultError("source document has no readable text chunks"), nil
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ChunkIndex < matched[j].ChunkIndex })
	if len(matched) > 50 {
		matched = matched[:50]
	}
	if !json.Valid(meta) {
		return nil, errors.New("invalid knowledge metadata returned by WeKnora")
	}
	out, err := json.Marshal(map[string]any{
		"knowledge":    json.RawMessage(meta),
		"knowledge_id": knowledgeID,
		"query":        query,
		"total_chunks": total,
		"chunks":       matched,
	})
	return officialToolResult(out, err)
}
