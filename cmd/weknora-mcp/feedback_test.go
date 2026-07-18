package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestParseSSEResultIncludesTraceableReferences(t *testing.T) {
	payload := strings.Join([]string{
		`data: {"id":"req-backend","response_type":"references","knowledge_references":[{"id":"node-1","content":"The source content","knowledge_id":"knowledge-1","knowledge_base_id":"kb-1","parent_chunk_id":"parent-1","sub_chunk_id":["sub-1"],"knowledge_title":"Handbook","score":0.91,"match_type":"vector"}]}`,
		`data: {"id":"req-backend","response_type":"thinking","content":"I should search first."}`,
		`data: {"id":"req-backend","response_type":"tool_call","content":"Calling tool: wiki_search"}`,
		`data: {"id":"req-backend","response_type":"tool_result","content":"internal search output"}`,
		`data: {"id":"req-backend","response_type":"answer","content":"Hello "}`,
		`data: {"id":"req-backend","response_type":"answer","content":"world"}`,
		`data: [DONE]`,
	}, "\n")

	result := parseSSEResult([]byte(payload), knowledgeAnswer{RequestID: "req-mcp", SessionID: "session-1", KnowledgeBaseID: "kb-1", RecalledNodes: []recalledNode{}})
	if result.Answer != "Hello world" || result.RequestID != "req-mcp" || result.SessionID != "session-1" {
		t.Fatalf("unexpected answer envelope: %#v", result)
	}
	if len(result.RecalledNodes) != 1 {
		t.Fatalf("references=%d, want 1", len(result.RecalledNodes))
	}
	node := result.RecalledNodes[0]
	if node.NodeID != "node-1" || node.SourceType != "knowledge_chunk" || node.KnowledgeID != "knowledge-1" || node.ParentNodeID != "parent-1" || node.Rank != 1 {
		t.Fatalf("unexpected recalled node: %#v", node)
	}
	if len(node.SubNodeIDs) != 1 || node.SubNodeIDs[0] != "sub-1" || node.ContentExcerpt != "The source content" {
		t.Fatalf("missing trace snapshot: %#v", node)
	}
	if !strings.HasPrefix(node.ContentHash, "sha256:") || len(node.ContentHash) != len("sha256:")+64 {
		t.Fatalf("content hash is not stable sha256: %q", node.ContentHash)
	}
}

func seedRecallSnapshot(t *testing.T, store *adminStore) knowledgeAnswer {
	t.Helper()
	answer := knowledgeAnswer{
		Answer: "answer", RequestID: "req-1", SessionID: "session-1", KnowledgeBaseID: "kb-1",
		RecalledNodes: []recalledNode{
			{NodeID: "node-1", KnowledgeID: "knowledge-1", KnowledgeBaseID: "kb-1", Rank: 1, Score: 0.9, ContentExcerpt: "one", ContentHash: contentHash("one")},
			{NodeID: "node-2", KnowledgeID: "knowledge-2", KnowledgeBaseID: "kb-1", Rank: 2, Score: 0.8, ContentExcerpt: "two", ContentHash: contentHash("two")},
		},
	}
	if err := store.saveRecallSnapshot(answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestSubmitFeedbackAutoFillsAndVerifiesRecallSnapshot(t *testing.T) {
	store := newTestAdminStore(t)
	seedRecallSnapshot(t, store)
	member := &mcpMember{ID: 7, Name: "Reader", CanRead: true}
	ctx := context.WithValue(context.Background(), authMemberKey{}, member)

	receipt, err := store.submitFeedback(ctx, feedbackInput{
		FeedbackText: "The second source is outdated.", FeedbackType: "outdated",
		RelatedRequestID: "req-1", TargetNodeIDs: []string{"node-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TraceStatus != traceStatusVerified || len(receipt.RecalledNodeIDs) != 2 || receipt.KnowledgeBaseID != "kb-1" {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	view, err := store.getFeedback(receipt.FeedbackID)
	if err != nil {
		t.Fatal(err)
	}
	if view.MemberName != "Reader" || len(view.References) != 2 || len(view.TargetNodeIDs) != 1 || view.TargetNodeIDs[0] != "node-2" {
		t.Fatalf("feedback was not persisted with trace: %#v", view)
	}
}

func TestSubmitFeedbackKeepsTraceMismatchWithoutBindingSilently(t *testing.T) {
	store := newTestAdminStore(t)
	seedRecallSnapshot(t, store)

	receipt, err := store.submitFeedback(context.Background(), feedbackInput{
		FeedbackText: "This citation is wrong.", FeedbackType: "wrong_reference",
		RelatedRequestID: "req-1", RecalledNodeIDs: []string{"node-1"}, TargetNodeIDs: []string{"made-up-node"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TraceStatus != traceStatusMismatch || !strings.Contains(receipt.TraceMessage, "not a subset") {
		t.Fatalf("mismatch was not reported: %#v", receipt)
	}
	view, err := store.getFeedback(receipt.FeedbackID)
	if err != nil {
		t.Fatal(err)
	}
	if view.TraceStatus != traceStatusMismatch || len(view.TargetNodeIDs) != 1 || view.TargetNodeIDs[0] != "made-up-node" {
		t.Fatalf("mismatched feedback should still be stored: %#v", view)
	}
	var target mcpFeedbackNode
	if err := store.db.Where("feedback_id = ? AND role = ?", receipt.FeedbackID, "target").First(&target).Error; err != nil {
		t.Fatal(err)
	}
	if target.Verified || target.ReferenceID != nil {
		t.Fatalf("mismatched target was silently bound: %#v", target)
	}
}

func TestSubmitFeedbackOnlyRequiresTextAndReaderAccess(t *testing.T) {
	store := newTestAdminStore(t)
	member := &mcpMember{ID: 9, Name: "Read-only", CanRead: true, CanWrite: false}
	ctx := context.WithValue(context.Background(), authMemberKey{}, member)
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"feedback_text": "Please add an example."}}}

	result, err := store.feedbackTool(ctx, request)
	if err != nil || result.IsError {
		t.Fatalf("read-only feedback failed: result=%#v err=%v", result, err)
	}
	receipt, ok := result.StructuredContent.(*feedbackReceipt)
	if !ok || !receipt.Accepted || receipt.TraceStatus != traceStatusUntraced {
		t.Fatalf("unexpected structured receipt: %#v", result.StructuredContent)
	}
	var count int64
	if err := store.db.Model(&mcpFeedback{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("feedback count=%d err=%v", count, err)
	}
}

func TestFeedbackRejectsUnknownTypeWithoutCreatingRecord(t *testing.T) {
	store := newTestAdminStore(t)
	_, err := store.submitFeedback(context.Background(), feedbackInput{FeedbackText: "x", FeedbackType: "hallucinated"})
	if err == nil {
		t.Fatal("unknown feedback type was accepted")
	}
	var count int64
	_ = store.db.Model(&mcpFeedback{}).Count(&count).Error
	if count != 0 {
		t.Fatalf("invalid feedback was persisted: %d", count)
	}
}

func TestFeedbackReceiptJSONContainsResolvedNodeIDs(t *testing.T) {
	receipt := feedbackReceipt{Accepted: true, FeedbackID: "f-1", TraceStatus: traceStatusVerified, RecalledNodeIDs: []string{"node-1"}, TargetNodeIDs: []string{}}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"recalled_node_ids":["node-1"]`) || !strings.Contains(string(data), `"target_node_ids":[]`) {
		t.Fatalf("receipt omitted resolved IDs: %s", data)
	}
}

func TestAskPropagatesRequestIDAndPersistsRecallSnapshot(t *testing.T) {
	var seenRequestIDs []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRequestIDs = append(seenRequestIDs, r.Header.Get("X-Request-ID"))
		switch r.URL.Path {
		case "/sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"id":"session-ask"}}`))
		case "/knowledge-chat/session-ask":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"response_type\":\"references\",\"knowledge_references\":[{\"id\":\"node-ask\",\"content\":\"source\",\"knowledge_id\":\"knowledge-ask\",\"knowledge_base_id\":\"kb-ask\"}]}\n" +
				"data: {\"response_type\":\"answer\",\"content\":\"answer\"}\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	store := newTestAdminStore(t)
	client := &apiClient{baseURL: api.URL, apiKey: "key", store: store, http: api.Client()}
	ctx := context.WithValue(context.Background(), toolRequestIDKey{}, "req-ask")
	answer, err := client.ask(ctx, "/knowledge-chat", chatRequest{Query: "question"}, "kb-ask")
	if err != nil {
		t.Fatal(err)
	}
	if answer.RequestID != "req-ask" || len(answer.RecalledNodes) != 1 || answer.RecalledNodes[0].NodeID != "node-ask" {
		t.Fatalf("unexpected structured answer: %#v", answer)
	}
	if len(seenRequestIDs) != 2 || seenRequestIDs[0] != "req-ask" || seenRequestIDs[1] != "req-ask" {
		t.Fatalf("request id was not propagated: %#v", seenRequestIDs)
	}
	refs, err := store.referencesByRequestID("req-ask")
	if err != nil || len(refs) != 1 || refs[0].NodeID != "node-ask" {
		t.Fatalf("recall snapshot was not persisted: %#v err=%v", refs, err)
	}
}

func TestAskWikiStoresCleanAnswerAndTraceablePageFeedback(t *testing.T) {
	var seenPaths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sessions":
			_, _ = w.Write([]byte(`{"data":{"id":"session-wiki"}}`))
		case "/agent-chat/session-wiki":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"response_type":"thinking","content":"I should search."}`,
				`data: {"response_type":"tool_call","content":"Calling tool: wiki_read_page"}`,
				`data: {"response_type":"answer","content":"stream answer"}`,
			}, "\n")))
		case "/messages/session-wiki/load":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"request_id":"req-wiki","role":"assistant","content":"Clean final answer from [[entity/free-market|Free Market]].","is_completed":true,"agent_steps":[{"tool_calls":[{"name":"wiki_read_page","args":{"slugs":["entity/free-market"]},"result":{"success":true}}]}]}]}`))
		case "/knowledgebase/kb-wiki/wiki/pages/entity/free-market":
			_, _ = w.Write([]byte(`{"id":"page-1","knowledge_base_id":"kb-wiki","slug":"entity/free-market","title":"Free Market","page_type":"entity","content":"Authoritative page content","summary":"Summary","source_refs":["knowledge-1|Source document","knowledge-2"],"chunk_refs":["chunk-1","chunk-2"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	store := newTestAdminStore(t)
	client := &apiClient{baseURL: api.URL, apiKey: "key", store: store, http: api.Client()}
	ctx := context.WithValue(context.Background(), toolRequestIDKey{}, "req-wiki")
	answer, err := client.ask(ctx, "/agent-chat", chatRequest{Query: "question"}, "kb-wiki")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Answer != "Clean final answer from [[entity/free-market|Free Market]]." || strings.Contains(answer.Answer, "Calling tool") {
		t.Fatalf("agent progress leaked into answer: %q", answer.Answer)
	}
	if len(answer.RecalledNodes) != 1 {
		t.Fatalf("recalled nodes=%d, want 1: %#v", len(answer.RecalledNodes), answer.RecalledNodes)
	}
	node := answer.RecalledNodes[0]
	if node.NodeID != "page-1" || node.SourceType != "wiki_page" || node.WikiSlug != "entity/free-market" || node.KnowledgeID != "knowledge-1" {
		t.Fatalf("unexpected Wiki page trace: %#v", node)
	}
	if len(node.KnowledgeIDs) != 2 || len(node.SubNodeIDs) != 2 || node.ContentExcerpt != "Authoritative page content" || !strings.HasPrefix(node.ContentHash, "sha256:") {
		t.Fatalf("incomplete Wiki page snapshot: %#v", node)
	}

	refs, err := store.referencesByRequestID("req-wiki")
	if err != nil || len(refs) != 1 || refs[0].NodeID != "page-1" || refs[0].WikiSlug != "entity/free-market" || refs[0].SourceType != "wiki_page" {
		t.Fatalf("Wiki trace was not persisted: %#v err=%v", refs, err)
	}
	receipt, err := store.submitFeedback(context.Background(), feedbackInput{
		FeedbackText: "The Free Market definition is outdated.", FeedbackType: "outdated",
		RelatedRequestID: "req-wiki", TargetNodeIDs: []string{"page-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TraceStatus != traceStatusVerified || len(receipt.RecalledNodeIDs) != 1 || receipt.RecalledNodeIDs[0] != "page-1" {
		t.Fatalf("feedback did not verify against Wiki snapshot: %#v", receipt)
	}
	view, err := store.getFeedback(receipt.FeedbackID)
	if err != nil || len(view.References) != 1 || view.References[0].ContentHash != node.ContentHash {
		t.Fatalf("feedback detail lost historical Wiki snapshot: %#v err=%v", view, err)
	}
	if !containsString(seenPaths, "/messages/session-wiki/load?limit=20") || !containsString(seenPaths, "/knowledgebase/kb-wiki/wiki/pages/entity/free-market") {
		t.Fatalf("trace APIs were not called: %#v", seenPaths)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
