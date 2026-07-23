package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	authSourceLegacyMCP = "legacy_mcp"
	authSourceWebUser   = "web_user"
	authSourceAPIKey    = "api_key"
)

type unifiedAuthKey struct{}
type upstreamCredentialKey struct{}

type upstreamCredential struct {
	Kind  string
	Token string
}

// unifiedIdentity is the MCP projection of the canonical authorization
// context returned by WeKnora's /auth/me endpoint.
type unifiedIdentity struct {
	Source           string
	Subject          string
	DisplayName      string
	TenantID         uint64
	Role             string
	IsSystemAdmin    bool
	FullAccess       bool
	Capabilities     []string
	KnowledgeBaseIDs []string
	AllowedTools     []string
}

func (i *unifiedIdentity) canUseTool(name string) bool {
	if i == nil {
		return false
	}
	for _, allowed := range i.AllowedTools {
		if allowed == name {
			return true
		}
	}
	return false
}

type unifiedAuthResolver struct {
	baseURL string
	http    *http.Client
}

func (r *unifiedAuthResolver) resolve(ctx context.Context, token string) (*unifiedIdentity, upstreamCredential, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, upstreamCredential{}, errors.New("missing bearer token")
	}
	if identity, err := r.resolveWith(ctx, token, authSourceWebUser); err == nil {
		return identity, upstreamCredential{Kind: authSourceWebUser, Token: token}, nil
	}
	if identity, err := r.resolveWith(ctx, token, authSourceAPIKey); err == nil {
		return identity, upstreamCredential{Kind: authSourceAPIKey, Token: token}, nil
	}
	return nil, upstreamCredential{}, errors.New("credential is not accepted by WeKnora")
}

func (r *unifiedAuthResolver) resolveWith(ctx context.Context, token, kind string) (*unifiedIdentity, error) {
	if r == nil || strings.TrimSpace(r.baseURL) == "" || r.http == nil {
		return nil, errors.New("unified auth resolver is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.baseURL, "/")+"/auth/me", nil)
	if err != nil {
		return nil, err
	}
	if kind == authSourceAPIKey {
		req.Header.Set("X-API-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("auth context returned %s", resp.Status)
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			User struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
			} `json:"user"`
			Auth struct {
				Type          string `json:"type"`
				TenantID      uint64 `json:"tenant_id"`
				UserID        string `json:"user_id"`
				Role          string `json:"role"`
				IsSystemAdmin bool   `json:"is_system_admin"`
				PrincipalType string `json:"principal_type"`
				PrincipalID   string `json:"principal_id"`
				APIKey        *struct {
					FullAccess       bool     `json:"full_access"`
					Capabilities     []string `json:"capabilities"`
					KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
				} `json:"api_key"`
			} `json:"auth"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode auth context: %w", err)
	}
	if !envelope.Success || envelope.Data.Auth.TenantID == 0 {
		return nil, errors.New("incomplete auth context")
	}
	auth := envelope.Data.Auth
	source := strings.TrimSpace(auth.Type)
	if source != authSourceAPIKey {
		source = authSourceWebUser
	}
	displayName := strings.TrimSpace(envelope.Data.User.Username)
	if displayName == "" {
		displayName = strings.TrimSpace(envelope.Data.User.Email)
	}
	subject := strings.TrimSpace(auth.PrincipalType) + ":" + strings.TrimSpace(auth.PrincipalID)
	if strings.Trim(subject, ":") == "" {
		subject = strings.TrimSpace(auth.UserID)
	}
	identity := &unifiedIdentity{
		Source:        source,
		Subject:       subject,
		DisplayName:   displayName,
		TenantID:      auth.TenantID,
		Role:          normalizeRole(auth.Role, false),
		IsSystemAdmin: auth.IsSystemAdmin,
	}
	if auth.APIKey != nil {
		identity.FullAccess = auth.APIKey.FullAccess
		identity.Capabilities = append([]string(nil), auth.APIKey.Capabilities...)
		identity.KnowledgeBaseIDs = append([]string(nil), auth.APIKey.KnowledgeBaseIDs...)
	}
	identity.AllowedTools = toolsForUnifiedIdentity(identity)
	return identity, nil
}

func toolsForUnifiedIdentity(identity *unifiedIdentity) []string {
	if identity == nil {
		return nil
	}
	if identity.Source != authSourceAPIKey {
		return roleToolNames(identity.Role)
	}
	if identity.FullAccess {
		return allToolNames()
	}
	capabilities := make(map[string]bool, len(identity.Capabilities))
	for _, capability := range identity.Capabilities {
		capabilities[strings.TrimSpace(capability)] = true
	}
	out := make([]string, 0, len(toolCatalog))
	for _, tool := range toolCatalog {
		for _, capability := range tool.Capabilities {
			if capabilities[capability] {
				out = append(out, tool.Name)
				break
			}
		}
	}
	return out
}
