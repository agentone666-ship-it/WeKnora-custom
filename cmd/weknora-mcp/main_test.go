package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIClientRequestSetsAPIPrincipalHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "sk-test" {
			t.Errorf("X-API-Key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization must not duplicate the API key, got %q", got)
		}
		if got := r.Header.Get("X-External-User-ID"); got != "mcp-service" {
			t.Errorf("X-External-User-ID = %q", got)
		}
		if got := strings.TrimSpace(r.Header.Get("X-Request-ID")); got == "" {
			t.Error("X-Request-ID is empty")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	client := &apiClient{
		baseURL:        server.URL,
		apiKey:         "sk-test",
		externalUserID: "mcp-service",
		http:           server.Client(),
	}
	if _, _, err := client.request(context.Background(), http.MethodGet, "/knowledge-bases", nil); err != nil {
		t.Fatal(err)
	}
}

func TestFilterKnowledgeBasesMatchesNameDescriptionAndDefault(t *testing.T) {
	items := []knowledgeBaseSummary{
		{ID: "formal", Name: "示例商城运营（正式）", Description: "正式运营规则"},
		{ID: "test", Name: "示例商城运营（测试）", Description: "试验数据"},
		{ID: "other", Name: "产品手册", Description: "示例商城业务说明"},
	}

	got := filterKnowledgeBases(items, "正式", "formal")
	if len(got) != 1 || got[0].ID != "formal" || !got[0].IsDefault {
		t.Fatalf("formal search = %#v", got)
	}

	got = filterKnowledgeBases(items, "业务说明", "formal")
	if len(got) != 1 || got[0].ID != "other" || got[0].IsDefault {
		t.Fatalf("description search = %#v", got)
	}
}

func TestFilterKnowledgeBasesEmptyQueryCapsResults(t *testing.T) {
	items := make([]knowledgeBaseSummary, 25)
	for i := range items {
		items[i] = knowledgeBaseSummary{ID: string(rune('a' + i)), Name: "KB"}
	}
	got := filterKnowledgeBases(items, "", "a")
	if len(got) != 20 {
		t.Fatalf("len = %d, want 20", len(got))
	}
	if !got[0].IsDefault {
		t.Fatal("default knowledge base was not marked")
	}
}
