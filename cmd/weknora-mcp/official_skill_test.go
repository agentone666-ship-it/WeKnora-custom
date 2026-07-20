package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func toolRequest(arguments map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: arguments}}
}

func writerContext() context.Context {
	return context.WithValue(context.Background(), authRoleKey{}, authRoleWriter)
}

func TestRegisterMCPToolsIncludesOfficialSkillSurface(t *testing.T) {
	s := server.NewMCPServer("weknora", "0.2.0", server.WithToolCapabilities(true))
	registerMCPTools(s, nil, &apiClient{})

	tools := s.ListTools()
	if got, want := len(tools), 20; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}

	wantNames := []string{
		"ask_rag", "ask_wiki", "create_manual_knowledge", "delete_knowledge",
		"get_knowledge", "get_knowledge_base", "hybrid_search", "import_knowledge_url",
		"list_knowledge", "list_knowledge_bases", "reparse_knowledge", "search_knowledge",
		"search_knowledge_bases", "submit_knowledge_feedback", "update_knowledge", "update_manual_knowledge", "upload_knowledge_file",
		"wiki_read_page", "wiki_read_source_doc", "wiki_search",
	}
	gotNames := make([]string, 0, len(tools))
	for name := range tools {
		gotNames = append(gotNames, name)
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("tool names = %v, want %v", gotNames, wantNames)
	}
}

func TestListKnowledgeBuildsOfficialPaginationAndFilters(t *testing.T) {
	var gotURL *url.URL
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer backend.Close()

	client := &apiClient{baseURL: backend.URL, apiKey: "test-key", knowledgeBaseID: "kb-fixed", http: backend.Client()}
	result, err := client.listKnowledge(context.Background(), toolRequest(map[string]any{
		"page":         3,
		"page_size":    50,
		"tag_id":       "tag-a",
		"tag_ids":      []any{"tag-b", "tag-a"},
		"keyword":      "deploy",
		"file_type":    "pdf",
		"parse_status": "completed",
		"source":       "mcp",
	}))
	if err != nil || result.IsError {
		t.Fatalf("listKnowledge error: %v / %s", err, toolResultText(result))
	}
	if gotURL == nil {
		t.Fatal("backend was not called")
	}
	if got, want := gotURL.Path, "/knowledge-bases/kb-fixed/knowledge"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	wantQuery := map[string]string{
		"page": "3", "page_size": "50", "tag_ids": "tag-b,tag-a", "keyword": "deploy",
		"file_type": "pdf", "parse_status": "completed", "source": "mcp",
	}
	for key, want := range wantQuery {
		if got := gotURL.Query().Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
}

func TestHybridSearchUsesPOSTEndpointAndBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer backend.Close()

	client := &apiClient{baseURL: backend.URL, apiKey: "test-key", knowledgeBaseID: "kb-1", http: backend.Client()}
	result, err := client.hybridSearch(context.Background(), toolRequest(map[string]any{
		"query_text":             "deployment process",
		"match_count":            7,
		"vector_threshold":       0.6,
		"keyword_threshold":      0.4,
		"disable_keywords_match": true,
		"disable_vector_match":   false,
		"knowledge_ids":          []any{"doc-1", "doc-2"},
		"tag_ids":                []any{"tag-1"},
		"only_recommended":       true,
	}))
	if err != nil || result.IsError {
		t.Fatalf("hybridSearch error: %v / %s", err, toolResultText(result))
	}
	if gotMethod != http.MethodPost || gotPath != "/knowledge-bases/kb-1/hybrid-search" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["query_text"] != "deployment process" || gotBody["match_count"] != float64(7) {
		t.Fatalf("unexpected body: %#v", gotBody)
	}
	if gotBody["vector_threshold"] != 0.6 || gotBody["keyword_threshold"] != 0.4 {
		t.Fatalf("unexpected thresholds: %#v", gotBody)
	}
}

func TestSearchKnowledgePassesScopesAndFixedKB(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/knowledge-search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer backend.Close()

	client := &apiClient{baseURL: backend.URL, apiKey: "test-key", knowledgeBaseID: "kb-fixed", http: backend.Client()}
	result, err := client.searchKnowledge(context.Background(), toolRequest(map[string]any{
		"query":         "deployment",
		"knowledge_ids": []any{"doc-1"},
		"tag_ids":       []any{"tag-1"},
	}))
	if err != nil || result.IsError {
		t.Fatalf("searchKnowledge error: %v / %s", err, toolResultText(result))
	}
	if gotBody["query"] != "deployment" {
		t.Fatalf("query body = %#v", gotBody)
	}
	if got := gotBody["knowledge_base_ids"]; !reflect.DeepEqual(got, []any{"kb-fixed"}) {
		t.Fatalf("knowledge_base_ids = %#v", got)
	}
	if got := gotBody["knowledge_ids"]; !reflect.DeepEqual(got, []any{"doc-1"}) {
		t.Fatalf("knowledge_ids = %#v", got)
	}
}

func TestImportKnowledgeURLRequiresWriterAndForwardsOfficialFields(t *testing.T) {
	var calls int
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/knowledge-bases/kb-1/knowledge/url" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"doc-1"}}`))
	}))
	defer backend.Close()

	client := &apiClient{baseURL: backend.URL, apiKey: "test-key", knowledgeBaseID: "kb-1", http: backend.Client()}
	request := toolRequest(map[string]any{
		"url":               "https://example.com/guide",
		"enable_multimodel": true,
		"title":             "Guide",
		"file_name":         "guide.html",
		"file_type":         "html",
		"tag_id":            "tag-a",
		"tag_ids":           []any{"tag-b"},
	})

	readerResult, err := client.importKnowledgeURL(context.Background(), request)
	if err != nil || !readerResult.IsError || !strings.Contains(toolResultText(readerResult), "writer-authorized") {
		t.Fatalf("reader result = %#v, err=%v", readerResult, err)
	}
	if calls != 0 {
		t.Fatalf("reader request reached backend %d times", calls)
	}

	writerResult, err := client.importKnowledgeURL(writerContext(), request)
	if err != nil || writerResult.IsError {
		t.Fatalf("writer result error: %v / %s", err, toolResultText(writerResult))
	}
	if calls != 1 {
		t.Fatalf("backend calls = %d, want 1", calls)
	}
	if gotBody["url"] != "https://example.com/guide" || gotBody["enable_multimodel"] != true || gotBody["channel"] != "mcp" {
		t.Fatalf("unexpected URL import body: %#v", gotBody)
	}
	if got := gotBody["tag_ids"]; !reflect.DeepEqual(got, []any{"tag-b", "tag-a"}) {
		t.Fatalf("tag_ids = %#v", got)
	}
}

func TestImportKnowledgeURLRejectsNonHTTPURL(t *testing.T) {
	client := &apiClient{knowledgeBaseID: "kb-1"}
	result, err := client.importKnowledgeURL(writerContext(), toolRequest(map[string]any{"url": "file:///etc/passwd"}))
	if err != nil || !result.IsError || !strings.Contains(toolResultText(result), "HTTP or HTTPS") {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
}
