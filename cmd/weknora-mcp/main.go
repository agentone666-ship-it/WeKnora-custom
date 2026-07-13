package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type apiClient struct {
	baseURL         string
	apiKey          string
	wikiAgentID     string
	knowledgeBaseID string
	http            *http.Client
}

type chatRequest struct {
	Query            string   `json:"query"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty"`
	AgentEnabled     bool     `json:"agent_enabled,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	DisableTitle     bool     `json:"disable_title"`
	Channel          string   `json:"channel"`
}

type manualKnowledgeRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Status  string `json:"status,omitempty"`
	Channel string `json:"channel"`
}

type authRoleKey struct{}
type authMemberKey struct{}
type requestMetaKey struct{}

type requestMeta struct {
	ClientIP  string
	UserAgent string
}

const authRoleWriter = "writer"

var uploadExtensions = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".epub": true, ".mhtml": true,
	".txt": true, ".md": true, ".csv": true, ".json": true, ".xml": true, ".html": true,
	".mp3": true, ".wav": true, ".m4a": true, ".flac": true, ".ogg": true, ".aac": true,
}

func main() {
	transport := flag.String("transport", envOr("WEKNORA_MCP_TRANSPORT", "http"), "transport: http or stdio")
	listen := flag.String("listen", envOr("WEKNORA_MCP_LISTEN", ":8787"), "listen address")
	apiURL := flag.String("api-url", envOr("WEKNORA_API_URL", "http://localhost:8080/api/v1"), "WeKnora API base URL")
	apiKey := flag.String("api-key", os.Getenv("WEKNORA_MCP_API_KEY"), "WeKnora API key")
	wikiAgentID := flag.String("wiki-agent-id", os.Getenv("WEKNORA_WIKI_AGENT_ID"), "default Wiki Agent ID")
	authToken := flag.String("auth-token", os.Getenv("WEKNORA_MCP_AUTH_TOKEN"), "Bearer token required by HTTP clients")
	writeTokens := flag.String("write-tokens", os.Getenv("WEKNORA_MCP_WRITE_TOKENS"), "comma-separated Bearer tokens allowed to update or upload knowledge")
	adminToken := flag.String("admin-token", os.Getenv("WEKNORA_MCP_ADMIN_TOKEN"), "administrator token for the standalone web console")
	adminDB := flag.String("admin-db", envOr("WEKNORA_MCP_ADMIN_DB", "data/weknora-mcp-admin.db"), "SQLite database used by the MCP admin console")
	knowledgeBaseID := flag.String("knowledge-base-id", os.Getenv("WEKNORA_MCP_KB_ID"), "optional fixed knowledge base ID for all tools")
	flag.Parse()
	if strings.TrimSpace(*apiKey) == "" {
		log.Fatal("WEKNORA_MCP_API_KEY or --api-key is required")
	}

	client := &apiClient{baseURL: strings.TrimRight(*apiURL, "/"), apiKey: *apiKey, wikiAgentID: *wikiAgentID, knowledgeBaseID: strings.TrimSpace(*knowledgeBaseID), http: &http.Client{Timeout: 10 * time.Minute}}
	store, err := openAdminStore(*adminDB)
	if err != nil {
		log.Fatalf("open MCP admin database: %v", err)
	}
	if err := store.bootstrapLegacy("默认只读成员", strings.TrimSpace(*authToken), false); err != nil {
		log.Fatalf("bootstrap MCP reader: %v", err)
	}
	for i, token := range splitTokens(*writeTokens) {
		if err := store.bootstrapLegacy(fmt.Sprintf("默认写入成员 %d", i+1), token, true); err != nil {
			log.Fatalf("bootstrap MCP writer: %v", err)
		}
	}
	s := server.NewMCPServer("weknora-wiki", "0.1.0", server.WithToolCapabilities(true))
	s.AddTool(mcp.NewTool("ask_wiki",
		mcp.WithDescription("Ask a WeKnora Wiki knowledge base through its Wiki Agent."),
		mcp.WithString("question", mcp.Required(), mcp.Description("Question to answer.")),
		mcp.WithString("knowledge_base_id", mcp.Description("Optional knowledge base ID. Omit it when this MCP server is bound to a default knowledge base.")),
		mcp.WithString("agent_id", mcp.Description("Optional Wiki Agent ID; defaults to WEKNORA_WIKI_AGENT_ID.")),
	), auditTool(store, "ask_wiki", client.askWiki))
	s.AddTool(mcp.NewTool("ask_rag",
		mcp.WithDescription("Ask a WeKnora knowledge base using the normal RAG pipeline."),
		mcp.WithString("question", mcp.Required(), mcp.Description("Question to answer.")),
		mcp.WithString("knowledge_base_id", mcp.Description("Optional knowledge base ID. Omit it when this MCP server is bound to a default knowledge base.")),
	), auditTool(store, "ask_rag", client.askRAG))
	s.AddTool(mcp.NewTool("update_knowledge",
		mcp.WithDescription("Add a Markdown knowledge entry. Requires a writer-authorized MCP token."),
		mcp.WithString("knowledge_base_id", mcp.Description("Optional target knowledge base ID. Omit it when this MCP server is bound to a default knowledge base.")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Knowledge title.")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Markdown knowledge content.")),
	), auditTool(store, "update_knowledge", client.updateKnowledge))
	s.AddTool(mcp.NewTool("upload_knowledge_file",
		mcp.WithDescription("Upload a base64-encoded document to the bound WeKnora knowledge base. Requires a writer-authorized MCP token. Supports PDF, Word, Excel, PowerPoint, EPUB, MHTML, text, Markdown, CSV, JSON, XML, HTML, and common audio files."),
		mcp.WithString("knowledge_base_id", mcp.Description("Optional target knowledge base ID. Omit it when this MCP server is bound to a default knowledge base.")),
		mcp.WithString("file_name", mcp.Required(), mcp.Description("Original file name including extension, for example handbook.pdf.")),
		mcp.WithString("content_base64", mcp.Required(), mcp.Description("Base64-encoded raw file bytes.")),
		mcp.WithBoolean("enable_multimodel", mcp.Description("Enable multimodal parsing for documents containing important images.")),
	), auditTool(store, "upload_knowledge_file", client.uploadKnowledgeFile))

	if strings.EqualFold(*transport, "stdio") {
		if err := server.ServeStdio(s); err != nil {
			log.Fatal(err)
		}
		return
	}
	if !strings.EqualFold(*transport, "http") {
		log.Fatalf("unsupported transport %q (use http or stdio)", *transport)
	}
	if strings.TrimSpace(*authToken) == "" {
		log.Fatal("WEKNORA_MCP_AUTH_TOKEN or --auth-token is required for HTTP transport")
	}
	adminAccessToken := strings.TrimSpace(*adminToken)
	if adminAccessToken == "" {
		// Backwards-compatible bootstrap for existing installations. Operators
		// should set a separate admin token after their first console login.
		adminAccessToken = strings.TrimSpace(*authToken)
		log.Printf("WARNING: WEKNORA_MCP_ADMIN_TOKEN is unset; the legacy MCP read token can access /admin")
	}
	httpServer := server.NewStreamableHTTPServer(s)
	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth(store, httpServer))
	admin := &adminHTTP{store: store, token: adminAccessToken}
	admin.register(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
	})
	log.Printf("WeKnora MCP server listening on %s/mcp", *listen)
	log.Printf("WeKnora MCP admin console listening on %s/admin", *listen)
	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatal(err)
	}
}

func splitTokens(raw string) []string {
	var tokens []string
	for _, token := range strings.Split(raw, ",") {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func bearerAuth(store *adminStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		member, err := store.authenticate(provided)
		meta := requestMeta{ClientIP: clientIP(r), UserAgent: r.UserAgent()}
		if err != nil {
			store.addLog(&mcpCallLog{Tool: "authentication", Status: "denied", ErrorMessage: "unauthorized", ClientIP: meta.ClientIP, UserAgent: meta.UserAgent})
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		role := "reader"
		if member.CanWrite {
			role = authRoleWriter
		}
		ctx := context.WithValue(r.Context(), authRoleKey{}, role)
		ctx = context.WithValue(ctx, authMemberKey{}, member)
		ctx = context.WithValue(ctx, requestMetaKey{}, meta)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func auditTool(store *adminStore, tool string, next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started := time.Now()
		result, err := next(ctx, request)
		status := "success"
		errorMessage := ""
		if err != nil {
			status, errorMessage = "failed", err.Error()
		}
		if result != nil && result.IsError {
			status = "failed"
			errorMessage = toolResultText(result)
		}
		row := &mcpCallLog{RequestID: request.Header.Get("X-Request-ID"), Tool: tool, Status: status, ErrorMessage: errorMessage, KnowledgeBaseID: request.GetString("knowledge_base_id", ""), Subject: auditSubject(tool, request), DurationMS: time.Since(started).Milliseconds()}
		if member, ok := ctx.Value(authMemberKey{}).(*mcpMember); ok {
			row.MemberID, row.MemberName = &member.ID, member.Name
		}
		if meta, ok := ctx.Value(requestMetaKey{}).(requestMeta); ok {
			row.ClientIP, row.UserAgent = meta.ClientIP, meta.UserAgent
		}
		store.addLog(row)
		return result, err
	}
}

func auditSubject(tool string, request mcp.CallToolRequest) string {
	if strings.HasPrefix(tool, "ask_") {
		return request.GetString("question", "")
	}
	if tool == "upload_knowledge_file" {
		return request.GetString("file_name", "")
	}
	return request.GetString("title", "")
}

func toolResultText(result *mcp.CallToolResult) string {
	for _, item := range result.Content {
		if text, ok := item.(mcp.TextContent); ok {
			return text.Text
		}
	}
	return "tool returned an error"
}

func requireWriter(ctx context.Context) error {
	if role, _ := ctx.Value(authRoleKey{}).(string); role != authRoleWriter {
		return errors.New("this tool requires a writer-authorized MCP token")
	}
	return nil
}

func (c *apiClient) scopedKnowledgeBase(request mcp.CallToolRequest) (string, error) {
	requested := strings.TrimSpace(request.GetString("knowledge_base_id", ""))
	if c.knowledgeBaseID == "" {
		if requested == "" {
			return "", errors.New("knowledge_base_id is required because this MCP server has no default knowledge base")
		}
		return requested, nil
	}
	if requested == "" {
		return c.knowledgeBaseID, nil
	}
	if requested != c.knowledgeBaseID {
		return "", fmt.Errorf("this MCP server is restricted to knowledge_base_id %s", c.knowledgeBaseID)
	}
	return c.knowledgeBaseID, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (c *apiClient) request(ctx context.Context, method, path string, body any) ([]byte, string, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("WeKnora API %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *apiClient) uploadFile(ctx context.Context, kb, fileName string, data []byte, enableMultimodel bool) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	_ = w.WriteField("fileName", fileName)
	_ = w.WriteField("channel", "mcp")
	_ = w.WriteField("enable_multimodel", fmt.Sprintf("%t", enableMultimodel))
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/knowledge-bases/"+kb+"/knowledge/file", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WeKnora API %s: %s", resp.Status, strings.TrimSpace(string(response)))
	}
	return response, nil
}

func (c *apiClient) createSession(ctx context.Context) (string, error) {
	data, _, err := c.request(ctx, http.MethodPost, "/sessions", map[string]string{"title": "MCP Wiki Query"})
	if err != nil {
		return "", err
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	if envelope.Data.ID != "" {
		return envelope.Data.ID, nil
	}
	if envelope.ID != "" {
		return envelope.ID, nil
	}
	return "", errors.New("WeKnora did not return a session id")
}

func (c *apiClient) ask(ctx context.Context, path string, req chatRequest) (string, error) {
	sessionID, err := c.createSession(ctx)
	if err != nil {
		return "", err
	}
	data, contentType, err := c.request(ctx, http.MethodPost, path+"/"+sessionID, req)
	if err != nil {
		return "", err
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return strings.TrimSpace(string(data)), nil
	}
	return parseSSEText(data), nil
}

func parseSSEText(data []byte) string {
	var out strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		for _, key := range []string{"content", "text", "answer"} {
			if value, ok := event[key].(string); ok {
				out.WriteString(value)
			}
		}
		if nested, ok := event["data"].(map[string]any); ok {
			if value, ok := nested["content"].(string); ok {
				out.WriteString(value)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func (c *apiClient) askWiki(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	question, err := request.RequireString("question")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	agentID := request.GetString("agent_id", "")
	if agentID == "" {
		agentID = c.wikiAgentID
	}
	if agentID == "" {
		return mcp.NewToolResultError("agent_id is required for ask_wiki (or set WEKNORA_WIKI_AGENT_ID)"), nil
	}
	answer, err := c.ask(ctx, "/agent-chat", chatRequest{Query: question, KnowledgeBaseIDs: []string{kb}, AgentEnabled: true, AgentID: agentID, DisableTitle: true, Channel: "mcp"})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(answer), nil
}

func (c *apiClient) askRAG(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	question, err := request.RequireString("question")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	answer, err := c.ask(ctx, "/knowledge-chat", chatRequest{Query: question, KnowledgeBaseIDs: []string{kb}, DisableTitle: true, Channel: "mcp"})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(answer), nil
}

func (c *apiClient) updateKnowledge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	data, _, err := c.request(ctx, http.MethodPost, "/knowledge-bases/"+kb+"/knowledge/manual", manualKnowledgeRequest{Title: title, Content: content, Status: "pending", Channel: "mcp"})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (c *apiClient) uploadKnowledgeFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireWriter(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb, err := c.scopedKnowledgeBase(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fileName, err := request.RequireString("file_name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fileName = filepath.Base(strings.TrimSpace(fileName))
	ext := strings.ToLower(filepath.Ext(fileName))
	if fileName == "." || fileName == "" || !uploadExtensions[ext] {
		return mcp.NewToolResultError("unsupported or missing file extension"), nil
	}
	encoded, err := request.RequireString("content_base64")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return mcp.NewToolResultError("content_base64 is not valid base64"), nil
	}
	if len(data) == 0 {
		return mcp.NewToolResultError("uploaded file is empty"), nil
	}
	if len(data) > 50*1024*1024 {
		return mcp.NewToolResultError("uploaded file exceeds the MCP 50 MB limit"), nil
	}
	response, err := c.uploadFile(ctx, kb, fileName, data, request.GetBool("enable_multimodel", false))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(response)), nil
}
