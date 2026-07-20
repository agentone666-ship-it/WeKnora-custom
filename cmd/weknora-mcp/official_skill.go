package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerOfficialSkillTools exposes the workflows documented by the official
// ClawHub WeKnora skill while keeping the original MCP tools for compatibility.
func registerOfficialSkillTools(s *server.MCPServer, store *adminStore, client *apiClient) {
	s.AddTool(mcp.NewTool("list_knowledge_bases",
		mcp.WithDescription("List WeKnora knowledge bases. Compatible with the official WeKnora skill GET /knowledge-bases workflow."),
		mcp.WithString("query", mcp.Description("Optional case-insensitive keyword matched against knowledge base name and description.")),
	), auditTool(store, "list_knowledge_bases", client.knowledgeBaseID, client.searchKnowledgeBases))

	s.AddTool(mcp.NewTool("get_knowledge_base",
		mcp.WithDescription("Get details for one WeKnora knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Description("Knowledge base ID. May be omitted when WEKNORA_MCP_KB_ID is configured.")),
	), auditTool(store, "get_knowledge_base", client.knowledgeBaseID, client.getKnowledgeBase))

	s.AddTool(mcp.NewTool("create_manual_knowledge",
		mcp.WithDescription("Create a Markdown knowledge entry. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_base_id", mcp.Description("Target knowledge base ID. May be omitted when WEKNORA_MCP_KB_ID is configured.")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Knowledge title.")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Markdown content.")),
		mcp.WithString("tag_id", mcp.Description("Optional single tag ID, matching the official skill parameter.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs.")),
	), auditTool(store, "create_manual_knowledge", client.knowledgeBaseID, client.createManualKnowledge))

	s.AddTool(mcp.NewTool("import_knowledge_url",
		mcp.WithDescription("Import a web page or remote document URL into a WeKnora knowledge base. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_base_id", mcp.Description("Target knowledge base ID. May be omitted when WEKNORA_MCP_KB_ID is configured.")),
		mcp.WithString("url", mcp.Required(), mcp.Description("HTTP or HTTPS URL to import.")),
		mcp.WithBoolean("enable_multimodel", mcp.DefaultBool(false), mcp.Description("Enable multimodal parsing.")),
		mcp.WithString("title", mcp.Description("Optional title override.")),
		mcp.WithString("file_name", mcp.Description("Optional remote file name.")),
		mcp.WithString("file_type", mcp.Description("Optional remote file type.")),
		mcp.WithString("tag_id", mcp.Description("Optional single tag ID.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs.")),
	), auditTool(store, "import_knowledge_url", client.knowledgeBaseID, client.importKnowledgeURL))

	s.AddTool(mcp.NewTool("get_knowledge",
		mcp.WithDescription("Get one knowledge entry, including parse_status and error_message for upload progress checks."),
		mcp.WithString("knowledge_id", mcp.Required(), mcp.Description("Knowledge entry ID.")),
	), auditTool(store, "get_knowledge", client.knowledgeBaseID, client.getKnowledge))

	s.AddTool(mcp.NewTool("list_knowledge",
		mcp.WithDescription("Browse paginated knowledge entries in a knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Description("Knowledge base ID. May be omitted when WEKNORA_MCP_KB_ID is configured.")),
		mcp.WithInteger("page", mcp.DefaultNumber(1), mcp.Description("Page number, starting at 1.")),
		mcp.WithInteger("page_size", mcp.DefaultNumber(20), mcp.Description("Items per page, from 1 to 1000.")),
		mcp.WithString("tag_id", mcp.Description("Optional single tag ID, matching the official skill parameter.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs.")),
		mcp.WithString("keyword", mcp.Description("Optional title/content keyword filter.")),
		mcp.WithString("file_type", mcp.Description("Optional file type filter.")),
		mcp.WithString("parse_status", mcp.Description("Optional parse status filter.")),
		mcp.WithString("source", mcp.Description("Optional source filter.")),
	), auditTool(store, "list_knowledge", client.knowledgeBaseID, client.listKnowledge))

	s.AddTool(mcp.NewTool("update_manual_knowledge",
		mcp.WithDescription("Edit an existing manual Markdown knowledge entry. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_id", mcp.Required(), mcp.Description("Manual knowledge entry ID.")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Updated title.")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Updated Markdown content.")),
		mcp.WithString("tag_id", mcp.Description("Optional single tag ID.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs.")),
	), auditTool(store, "update_manual_knowledge", client.knowledgeBaseID, client.updateManualKnowledge))

	s.AddTool(mcp.NewTool("delete_knowledge",
		mcp.WithDescription("Delete one knowledge entry. Deletion is asynchronous. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_id", mcp.Required(), mcp.Description("Knowledge entry ID.")),
	), auditTool(store, "delete_knowledge", client.knowledgeBaseID, client.deleteKnowledge))

	s.AddTool(mcp.NewTool("reparse_knowledge",
		mcp.WithDescription("Retry parsing a failed or outdated knowledge entry. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_id", mcp.Required(), mcp.Description("Knowledge entry ID.")),
	), auditTool(store, "reparse_knowledge", client.knowledgeBaseID, client.reparseKnowledge))

	s.AddTool(mcp.NewTool("hybrid_search",
		mcp.WithDescription("Run vector + keyword hybrid retrieval within one knowledge base."),
		mcp.WithString("knowledge_base_id", mcp.Description("Knowledge base ID. May be omitted when WEKNORA_MCP_KB_ID is configured.")),
		mcp.WithString("query_text", mcp.Required(), mcp.Description("Search query.")),
		mcp.WithInteger("match_count", mcp.DefaultNumber(5), mcp.Description("Maximum number of final matches.")),
		mcp.WithNumber("vector_threshold", mcp.DefaultNumber(0.5), mcp.Description("Minimum vector score from 0 to 1.")),
		mcp.WithNumber("keyword_threshold", mcp.DefaultNumber(0.5), mcp.Description("Minimum keyword score from 0 to 1.")),
		mcp.WithBoolean("disable_keywords_match", mcp.DefaultBool(false), mcp.Description("Disable keyword retrieval.")),
		mcp.WithBoolean("disable_vector_match", mcp.DefaultBool(false), mcp.Description("Disable vector retrieval.")),
		mcp.WithArray("knowledge_ids", mcp.WithStringItems(), mcp.Description("Optional knowledge entry IDs to restrict the search.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs.")),
		mcp.WithBoolean("only_recommended", mcp.DefaultBool(false), mcp.Description("Only return recommended chunks when supported.")),
	), auditTool(store, "hybrid_search", client.knowledgeBaseID, client.hybridSearch))

	s.AddTool(mcp.NewTool("search_knowledge",
		mcp.WithDescription("Search semantically across one or more WeKnora knowledge bases without LLM summarization."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query.")),
		mcp.WithString("knowledge_base_id", mcp.Description("Optional single knowledge base ID.")),
		mcp.WithArray("knowledge_base_ids", mcp.WithStringItems(), mcp.Description("Knowledge base IDs to search.")),
		mcp.WithArray("knowledge_ids", mcp.WithStringItems(), mcp.Description("Optional knowledge entry IDs to search.")),
		mcp.WithArray("tag_ids", mcp.WithStringItems(), mcp.Description("Optional tag IDs for a single knowledge base.")),
	), auditTool(store, "search_knowledge", client.knowledgeBaseID, client.searchKnowledge))
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "mcp-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("mcp-%d", time.Now().UnixNano())
}

func officialToolResult(data []byte, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func cleanStringSlice(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func requestTagIDs(request mcp.CallToolRequest) []string {
	values := append([]string{}, request.GetStringSlice("tag_ids", nil)...)
	if tagID := strings.TrimSpace(request.GetString("tag_id", "")); tagID != "" {
		values = append(values, tagID)
	}
	return cleanStringSlice(values)
}

func requireKnowledgeID(request mcp.CallToolRequest) (string, error) {
	id, err := request.RequireString("knowledge_id")
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("knowledge_id is required")
	}
	return id, nil
}

func (c *apiClient) authorizeKnowledgeID(ctx context.Context, id string) error {
	if c.knowledgeBaseID == "" {
		return nil
	}
	data, _, err := c.request(ctx, http.MethodGet, "/knowledge/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	var envelope struct {
		Data struct {
			KnowledgeBaseID string `json:"knowledge_base_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("parse knowledge scope: %w", err)
	}
	if envelope.Data.KnowledgeBaseID == "" {
		return errors.New("WeKnora did not return knowledge_base_id for scope validation")
	}
	if envelope.Data.KnowledgeBaseID != c.knowledgeBaseID {
		return fmt.Errorf("this MCP server is restricted to knowledge_base_id %s", c.knowledgeBaseID)
	}
	return nil
}

func (c *apiClient) getKnowledgeBase(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _, err := c.request(ctx, http.MethodGet, "/knowledge-bases/"+url.PathEscape(kb), nil)
	return officialToolResult(data, err)
}

func (c *apiClient) createManualKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title, err := request.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := request.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{
		"title":   title,
		"content": content,
		"status":  "pending",
		"channel": "mcp",
	}
	if tags := requestTagIDs(request); len(tags) > 0 {
		body["tag_ids"] = tags
	}
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge-bases/"+url.PathEscape(kb)+"/knowledge/manual", body)
	return officialToolResult(data, err)
}

func (c *apiClient) importKnowledgeURL(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceURL, err := request.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceURL = strings.TrimSpace(sourceURL)
	parsedURL, err := url.ParseRequestURI(sourceURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return mcp.NewToolResultError("url must be a valid HTTP or HTTPS URL"), nil
	}
	body := map[string]any{
		"url":               sourceURL,
		"enable_multimodel": request.GetBool("enable_multimodel", false),
		"channel":           "mcp",
	}
	for _, key := range []string{"title", "file_name", "file_type"} {
		if value := strings.TrimSpace(request.GetString(key, "")); value != "" {
			body[key] = value
		}
	}
	if tags := requestTagIDs(request); len(tags) > 0 {
		body["tag_ids"] = tags
	}
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge-bases/"+url.PathEscape(kb)+"/knowledge/url", body)
	return officialToolResult(data, err)
}

func (c *apiClient) getKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := requireKnowledgeID(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _, err := c.request(ctx, http.MethodGet, "/knowledge/"+url.PathEscape(id), nil)
	if err == nil && c.knowledgeBaseID != "" {
		var envelope struct {
			Data struct {
				KnowledgeBaseID string `json:"knowledge_base_id"`
			} `json:"data"`
		}
		if decodeErr := json.Unmarshal(data, &envelope); decodeErr != nil {
			err = fmt.Errorf("parse knowledge scope: %w", decodeErr)
		} else if envelope.Data.KnowledgeBaseID != c.knowledgeBaseID {
			err = fmt.Errorf("this MCP server is restricted to knowledge_base_id %s", c.knowledgeBaseID)
		}
	}
	return officialToolResult(data, err)
}

func (c *apiClient) listKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page := request.GetInt("page", 1)
	pageSize := request.GetInt("page_size", 20)
	if page < 1 {
		return mcp.NewToolResultError("page must be at least 1"), nil
	}
	if pageSize < 1 || pageSize > 1000 {
		return mcp.NewToolResultError("page_size must be between 1 and 1000"), nil
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))
	if tags := requestTagIDs(request); len(tags) > 0 {
		query.Set("tag_ids", strings.Join(tags, ","))
	}
	for _, key := range []string{"keyword", "file_type", "parse_status", "source"} {
		if value := strings.TrimSpace(request.GetString(key, "")); value != "" {
			query.Set(key, value)
		}
	}
	path := "/knowledge-bases/" + url.PathEscape(kb) + "/knowledge?" + query.Encode()
	data, _, err := c.request(ctx, http.MethodGet, path, nil)
	return officialToolResult(data, err)
}

func (c *apiClient) updateManualKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := requireKnowledgeID(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := c.authorizeKnowledgeID(ctx, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title, err := request.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := request.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{"title": title, "content": content, "status": "pending", "channel": "mcp"}
	if tags := requestTagIDs(request); len(tags) > 0 {
		body["tag_ids"] = tags
	}
	data, _, err := c.request(ctx, http.MethodPut, "/knowledge/manual/"+url.PathEscape(id), body)
	return officialToolResult(data, err)
}

func (c *apiClient) deleteKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := requireKnowledgeID(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := c.authorizeKnowledgeID(ctx, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _, err := c.request(ctx, http.MethodDelete, "/knowledge/"+url.PathEscape(id), nil)
	return officialToolResult(data, err)
}

func (c *apiClient) reparseKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := requireKnowledgeID(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := c.authorizeKnowledgeID(ctx, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge/"+url.PathEscape(id)+"/reparse", nil)
	return officialToolResult(data, err)
}

func (c *apiClient) hybridSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	queryText, err := request.RequireString("query_text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	matchCount := request.GetInt("match_count", 5)
	if matchCount < 1 || matchCount > 1000 {
		return mcp.NewToolResultError("match_count must be between 1 and 1000"), nil
	}
	vectorThreshold := request.GetFloat("vector_threshold", 0.5)
	keywordThreshold := request.GetFloat("keyword_threshold", 0.5)
	if vectorThreshold < 0 || vectorThreshold > 1 || keywordThreshold < 0 || keywordThreshold > 1 {
		return mcp.NewToolResultError("vector_threshold and keyword_threshold must be between 0 and 1"), nil
	}
	body := map[string]any{
		"query_text":             queryText,
		"match_count":            matchCount,
		"vector_threshold":       vectorThreshold,
		"keyword_threshold":      keywordThreshold,
		"disable_keywords_match": request.GetBool("disable_keywords_match", false),
		"disable_vector_match":   request.GetBool("disable_vector_match", false),
		"only_recommended":       request.GetBool("only_recommended", false),
		"knowledge_ids":          cleanStringSlice(request.GetStringSlice("knowledge_ids", nil)),
		"tag_ids":                cleanStringSlice(request.GetStringSlice("tag_ids", nil)),
	}
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge-bases/"+url.PathEscape(kb)+"/hybrid-search", body)
	return officialToolResult(data, err)
}

func (c *apiClient) searchKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	queryText, err := request.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	knowledgeBaseIDs := append([]string{}, request.GetStringSlice("knowledge_base_ids", nil)...)
	if single := strings.TrimSpace(request.GetString("knowledge_base_id", "")); single != "" {
		knowledgeBaseIDs = append(knowledgeBaseIDs, single)
	}
	knowledgeBaseIDs = cleanStringSlice(knowledgeBaseIDs)
	knowledgeIDs := cleanStringSlice(request.GetStringSlice("knowledge_ids", nil))
	if c.knowledgeBaseID != "" {
		if len(knowledgeBaseIDs) == 0 {
			knowledgeBaseIDs = []string{c.knowledgeBaseID}
		}
		for _, id := range knowledgeBaseIDs {
			if id != c.knowledgeBaseID {
				return mcp.NewToolResultError(fmt.Sprintf("this MCP server is restricted to knowledge_base_id %s", c.knowledgeBaseID)), nil
			}
		}
	}
	if len(knowledgeBaseIDs) == 0 && len(knowledgeIDs) == 0 {
		return mcp.NewToolResultError("at least one knowledge_base_id, knowledge_base_ids, or knowledge_ids value is required"), nil
	}
	body := map[string]any{
		"query":              queryText,
		"knowledge_base_ids": knowledgeBaseIDs,
		"knowledge_ids":      knowledgeIDs,
		"tag_ids":            cleanStringSlice(request.GetStringSlice("tag_ids", nil)),
	}
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge-search", body)
	return officialToolResult(data, err)
}
