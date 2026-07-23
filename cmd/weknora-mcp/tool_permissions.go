package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	roleOwner       = "owner"
	roleAdmin       = "admin"
	roleContributor = "contributor"
	roleViewer      = "viewer"
)

type toolPermission struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Category     string   `json:"category"`
	Write        bool     `json:"write"`
	Capabilities []string `json:"capabilities,omitempty"`
}

var toolCatalog = []toolPermission{
	{Name: "ask_rag", Label: "RAG 问答", Category: "问答", Capabilities: []string{"chat"}},
	{Name: "ask_wiki", Label: "Wiki 问答", Category: "问答", Capabilities: []string{"chat"}},
	{Name: "submit_knowledge_feedback", Label: "提交知识反馈", Category: "反馈", Capabilities: []string{"chat", "retrieve"}},
	{Name: "search_knowledge_bases", Label: "搜索知识库", Category: "知识库读取", Capabilities: []string{"retrieve"}},
	{Name: "list_knowledge_bases", Label: "列出知识库", Category: "知识库读取", Capabilities: []string{"retrieve"}},
	{Name: "get_knowledge_base", Label: "获取知识库", Category: "知识库读取", Capabilities: []string{"retrieve"}},
	{Name: "get_knowledge", Label: "获取知识", Category: "知识读取", Capabilities: []string{"retrieve"}},
	{Name: "list_knowledge", Label: "列出知识", Category: "知识读取", Capabilities: []string{"retrieve"}},
	{Name: "hybrid_search", Label: "混合检索", Category: "知识读取", Capabilities: []string{"retrieve"}},
	{Name: "search_knowledge", Label: "搜索知识", Category: "知识读取", Capabilities: []string{"retrieve"}},
	{Name: "wiki_search", Label: "搜索 Wiki", Category: "Wiki 读取", Capabilities: []string{"retrieve"}},
	{Name: "wiki_read_page", Label: "读取 Wiki 页面", Category: "Wiki 读取", Capabilities: []string{"retrieve"}},
	{Name: "wiki_read_source_doc", Label: "读取 Wiki 来源文档", Category: "Wiki 读取", Capabilities: []string{"retrieve"}},
	{Name: "update_knowledge", Label: "新增 Markdown 知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "upload_knowledge_file", Label: "上传知识文件", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "create_manual_knowledge", Label: "创建手工知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "import_knowledge_url", Label: "导入网页知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "update_manual_knowledge", Label: "更新手工知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "delete_knowledge", Label: "删除知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
	{Name: "reparse_knowledge", Label: "重新解析知识", Category: "知识写入", Write: true, Capabilities: []string{"ingest"}},
}

func allToolNames() []string {
	out := make([]string, 0, len(toolCatalog))
	for _, item := range toolCatalog {
		out = append(out, item.Name)
	}
	return out
}

func validRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleOwner, roleAdmin, roleContributor, roleViewer:
		return true
	default:
		return false
	}
}

func normalizeRole(role string, canWrite bool) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if validRole(role) {
		return role
	}
	if canWrite {
		return roleContributor
	}
	return roleViewer
}

func roleToolNames(role string) []string {
	role = normalizeRole(role, false)
	out := make([]string, 0, len(toolCatalog))
	for _, item := range toolCatalog {
		if !item.Write || role != roleViewer {
			out = append(out, item.Name)
		}
	}
	return out
}

func normalizeAllowedTools(role string, requested []string) ([]string, error) {
	if !validRole(role) {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	known := make(map[string]toolPermission, len(toolCatalog))
	for _, item := range toolCatalog {
		known[item.Name] = item
	}
	if requested == nil {
		return roleToolNames(role), nil
	}
	allowedByRole := make(map[string]bool)
	for _, name := range roleToolNames(role) {
		allowedByRole[name] = true
	}
	seen := make(map[string]bool)
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("unknown MCP tool %q", name)
		}
		if allowedByRole[name] {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, item := range toolCatalog {
		if seen[item.Name] {
			out = append(out, item.Name)
		}
	}
	return out, nil
}

func memberAllowedTools(member *mcpMember) []string {
	if member == nil {
		return nil
	}
	role := normalizeRole(member.Role, member.CanWrite)
	if member.AllowedTools == nil {
		return roleToolNames(role)
	}
	tools, err := normalizeAllowedTools(role, member.AllowedTools)
	if err != nil {
		return nil
	}
	return tools
}

func memberCanUseTool(member *mcpMember, name string) bool {
	for _, allowed := range memberAllowedTools(member) {
		if allowed == name {
			return true
		}
	}
	return false
}

func requireTool(ctx context.Context, name string) error {
	if identity, ok := ctx.Value(unifiedAuthKey{}).(*unifiedIdentity); ok && identity != nil {
		if !identity.canUseTool(name) {
			return errors.New("the authenticated WeKnora identity is not authorized to use tool " + name)
		}
		return nil
	}
	member, ok := ctx.Value(authMemberKey{}).(*mcpMember)
	if !ok || member == nil {
		return nil // stdio transport has no member token boundary
	}
	if !memberCanUseTool(member, name) {
		return errors.New("this MCP member is not authorized to use tool " + name)
	}
	return nil
}

func filterToolsForMember(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	if identity, ok := ctx.Value(unifiedAuthKey{}).(*unifiedIdentity); ok && identity != nil {
		allowed := make(map[string]bool, len(identity.AllowedTools))
		for _, name := range identity.AllowedTools {
			allowed[name] = true
		}
		out := make([]mcp.Tool, 0, len(tools))
		for _, tool := range tools {
			if allowed[tool.Name] {
				out = append(out, tool)
			}
		}
		return out
	}
	member, ok := ctx.Value(authMemberKey{}).(*mcpMember)
	if !ok || member == nil {
		return tools
	}
	allowed := make(map[string]bool)
	for _, name := range memberAllowedTools(member) {
		allowed[name] = true
	}
	out := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if allowed[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}

func sortedToolNames(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
