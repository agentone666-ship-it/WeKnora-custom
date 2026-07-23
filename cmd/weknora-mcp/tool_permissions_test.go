package main

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolCatalogHasExactlyTwentyUniqueTools(t *testing.T) {
	if len(toolCatalog) != 20 {
		t.Fatalf("tool catalog has %d tools, want 20", len(toolCatalog))
	}
	seen := map[string]bool{}
	for _, item := range toolCatalog {
		if seen[item.Name] {
			t.Fatalf("duplicate tool %q", item.Name)
		}
		seen[item.Name] = true
	}
}

func TestRoleToolLimitsAndCustomSubset(t *testing.T) {
	if got := len(roleToolNames(roleViewer)); got != 13 {
		t.Fatalf("viewer tool count = %d, want 13", got)
	}
	for _, role := range []string{roleOwner, roleAdmin, roleContributor} {
		if got := len(roleToolNames(role)); got != 20 {
			t.Fatalf("%s tool count = %d, want 20", role, got)
		}
	}
	tools, err := normalizeAllowedTools(roleViewer, []string{"ask_rag", "update_knowledge"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0] != "ask_rag" {
		t.Fatalf("viewer subset = %#v", tools)
	}
	if _, err := normalizeAllowedTools(roleAdmin, []string{"does_not_exist"}); err == nil {
		t.Fatal("unknown tool was accepted")
	}
}

func TestToolListFilterAndDirectCallGuard(t *testing.T) {
	member := &mcpMember{Role: roleAdmin, CanRead: true, CanWrite: true, AllowedTools: []string{"ask_rag"}}
	ctx := context.WithValue(context.Background(), authMemberKey{}, member)
	tools := []mcp.Tool{{Name: "ask_rag"}, {Name: "update_knowledge"}}
	filtered := filterToolsForMember(ctx, tools)
	if len(filtered) != 1 || filtered[0].Name != "ask_rag" {
		t.Fatalf("filtered tools = %#v", filtered)
	}
	if err := requireTool(ctx, "ask_rag"); err != nil {
		t.Fatalf("allowed tool rejected: %v", err)
	}
	if err := requireTool(ctx, "update_knowledge"); err == nil {
		t.Fatal("direct unauthorized tool call was accepted")
	}
}

func TestRoleDowngradeImmediatelyRemovesWriteTools(t *testing.T) {
	store := newTestAdminStore(t)
	member, _, err := store.createMemberWithPermissions("editor", roleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.updateMember(member.ID, map[string]any{"role": roleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CanWrite || memberCanUseTool(updated, "update_knowledge") {
		t.Fatalf("viewer retained write permission: %#v", updated)
	}
	if len(updated.AllowedTools) != 13 {
		t.Fatalf("viewer retained %d tools, want 13", len(updated.AllowedTools))
	}
}
