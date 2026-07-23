package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func authContextServer(t *testing.T, acceptedKind, acceptedToken, role string, fullAccess bool, capabilities, knowledgeBaseIDs []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepted := false
		switch acceptedKind {
		case authSourceWebUser:
			accepted = r.Header.Get("Authorization") == "Bearer "+acceptedToken && r.Header.Get("X-API-Key") == ""
		case authSourceAPIKey:
			accepted = r.Header.Get("X-API-Key") == acceptedToken && r.Header.Get("Authorization") == ""
		}
		if !accepted {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		auth := map[string]any{
			"type":            acceptedKind,
			"tenant_id":       7,
			"user_id":         "user-1",
			"role":            role,
			"is_system_admin": false,
			"principal_type":  acceptedKind,
			"principal_id":    "principal-1",
		}
		if acceptedKind == authSourceAPIKey {
			auth["api_key"] = map[string]any{
				"full_access":        fullAccess,
				"capabilities":       capabilities,
				"knowledge_base_ids": knowledgeBaseIDs,
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"user": map[string]any{"id": "user-1", "username": "测试账号"},
				"auth": auth,
			},
		})
	}))
}

func TestUnifiedAuthResolverMapsWebRoles(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		toolCount int
	}{
		{name: "viewer", role: roleViewer, toolCount: 13},
		{name: "contributor", role: roleContributor, toolCount: 20},
		{name: "admin", role: roleAdmin, toolCount: 20},
		{name: "owner", role: roleOwner, toolCount: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := authContextServer(t, authSourceWebUser, "jwt-token", tt.role, false, nil, nil)
			defer server.Close()
			resolver := &unifiedAuthResolver{baseURL: server.URL, http: server.Client()}
			identity, credential, err := resolver.resolve(context.Background(), "jwt-token")
			if err != nil {
				t.Fatal(err)
			}
			if identity.Source != authSourceWebUser || identity.Role != tt.role || len(identity.AllowedTools) != tt.toolCount {
				t.Fatalf("identity = %#v", identity)
			}
			if credential.Kind != authSourceWebUser || credential.Token != "jwt-token" {
				t.Fatalf("credential = %#v", credential)
			}
		})
	}
}

func TestUnifiedAuthResolverMapsAPIKeyCapabilitiesAndKnowledgeBases(t *testing.T) {
	tests := []struct {
		name         string
		fullAccess   bool
		capabilities []string
		wantTools    []string
	}{
		{name: "retrieve", capabilities: []string{"retrieve"}, wantTools: []string{"get_knowledge", "get_knowledge_base", "hybrid_search", "list_knowledge", "list_knowledge_bases", "search_knowledge", "search_knowledge_bases", "submit_knowledge_feedback", "wiki_read_page", "wiki_read_source_doc", "wiki_search"}},
		{name: "chat", capabilities: []string{"chat"}, wantTools: []string{"ask_rag", "ask_wiki", "submit_knowledge_feedback"}},
		{name: "ingest", capabilities: []string{"ingest"}, wantTools: []string{"create_manual_knowledge", "delete_knowledge", "import_knowledge_url", "reparse_knowledge", "update_knowledge", "update_manual_knowledge", "upload_knowledge_file"}},
		{name: "full_access", fullAccess: true, wantTools: sortedToolNames(allToolNames())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := authContextServer(t, authSourceAPIKey, "api-key", roleViewer, tt.fullAccess, tt.capabilities, []string{"kb-a", "kb-b"})
			defer server.Close()
			resolver := &unifiedAuthResolver{baseURL: server.URL, http: server.Client()}
			identity, credential, err := resolver.resolve(context.Background(), "api-key")
			if err != nil {
				t.Fatal(err)
			}
			if identity.Source != authSourceAPIKey || !reflect.DeepEqual(identity.KnowledgeBaseIDs, []string{"kb-a", "kb-b"}) {
				t.Fatalf("identity = %#v", identity)
			}
			if got := sortedToolNames(identity.AllowedTools); !reflect.DeepEqual(got, tt.wantTools) {
				t.Fatalf("tools = %#v, want %#v", got, tt.wantTools)
			}
			if credential.Kind != authSourceAPIKey {
				t.Fatalf("credential = %#v", credential)
			}
		})
	}
}

func TestAPIClientForwardsCanonicalCallerCredential(t *testing.T) {
	tests := []struct {
		name              string
		credential        upstreamCredential
		wantAuthorization string
		wantAPIKey        string
	}{
		{name: "web user", credential: upstreamCredential{Kind: authSourceWebUser, Token: "jwt"}, wantAuthorization: "Bearer jwt"},
		{name: "api key", credential: upstreamCredential{Kind: authSourceAPIKey, Token: "key"}, wantAPIKey: "key"},
		{name: "legacy fallback", wantAPIKey: "service-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &apiClient{apiKey: "service-key"}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := context.Background()
			if tt.credential.Token != "" {
				ctx = context.WithValue(ctx, upstreamCredentialKey{}, tt.credential)
			}
			client.setAuthHeaders(ctx, req)
			if got := req.Header.Get("Authorization"); got != tt.wantAuthorization {
				t.Fatalf("Authorization = %q, want %q", got, tt.wantAuthorization)
			}
			if got := req.Header.Get("X-API-Key"); got != tt.wantAPIKey {
				t.Fatalf("X-API-Key = %q, want %q", got, tt.wantAPIKey)
			}
		})
	}
}

func TestAdminUnifiedAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		role       string
		fullAccess bool
		wantStatus int
	}{
		{name: "owner jwt", kind: authSourceWebUser, role: roleOwner, wantStatus: http.StatusNoContent},
		{name: "admin jwt", kind: authSourceWebUser, role: roleAdmin, wantStatus: http.StatusNoContent},
		{name: "viewer jwt", kind: authSourceWebUser, role: roleViewer, wantStatus: http.StatusForbidden},
		{name: "contributor jwt", kind: authSourceWebUser, role: roleContributor, wantStatus: http.StatusForbidden},
		{name: "full api key", kind: authSourceAPIKey, role: roleViewer, fullAccess: true, wantStatus: http.StatusNoContent},
		{name: "scoped api key", kind: authSourceAPIKey, role: roleAdmin, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := authContextServer(t, tt.kind, "credential", tt.role, tt.fullAccess, []string{"retrieve"}, nil)
			defer server.Close()
			admin := &adminHTTP{authResolver: &unifiedAuthResolver{baseURL: server.URL, http: server.Client()}, token: "legacy-admin"}
			handler := admin.withAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
			req.Header.Set("Authorization", "Bearer credential")
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
