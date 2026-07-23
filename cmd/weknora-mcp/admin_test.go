package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAdminStore(t *testing.T) *adminStore {
	t.Helper()
	store, err := openAdminStore(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAdminHTMLRendersFeedbackAndLogMarkdownSafely(t *testing.T) {
	required := []string{
		"function md(v,compact)",
		"function mdInline(v)",
		"md(f.feedback_text,true)",
		"md(f.original_question||'-')",
		"md(f.answer_excerpt||'-')",
		"md(f.suggested_correction||'-')",
		"md(l.response_content||'该历史调用未保存完整回答，请查看下方召回内容。')",
		"md(r.content_excerpt||'-')",
		"/^(https?:\\/\\/|\\/(?!\\/)|#)/i.test(decoded)",
		`rel="noopener noreferrer"`,
	}
	for _, fragment := range required {
		if !strings.Contains(adminHTML, fragment) {
			t.Errorf("admin HTML is missing Markdown rendering safeguard %q", fragment)
		}
	}

	if strings.Contains(adminHTML, "+f.feedback_text+") || strings.Contains(adminHTML, "+r.content_excerpt+") {
		t.Fatal("feedback or recall Markdown is interpolated without the renderer")
	}
}

func TestMemberPermissionsAndTokenRotation(t *testing.T) {
	store := newTestAdminStore(t)
	member, token, err := store.createMember("测试成员", true, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.authenticate(token)
	if err != nil || got.ID != member.ID || got.CanWrite {
		t.Fatalf("unexpected authenticated member: %#v, err=%v", got, err)
	}
	updated, err := store.updateMember(member.ID, map[string]any{"can_write": true})
	if err != nil || !updated.CanRead || !updated.CanWrite {
		t.Fatalf("write permission did not imply read permission: %#v, err=%v", updated, err)
	}
	_, rotated, err := store.rotateToken(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.authenticate(token); err == nil {
		t.Fatal("old token remained valid after rotation")
	}
	if _, err := store.authenticate(rotated); err != nil {
		t.Fatalf("rotated token is invalid: %v", err)
	}
}

func TestAdminAPIAndLogs(t *testing.T) {
	store := newTestAdminStore(t)
	admin := &adminHTTP{store: store, token: "admin-secret"}
	mux := http.NewServeMux()
	admin.register(mux)

	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/members", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	body := bytes.NewBufferString(`{"name":"设备 A","can_read":true,"can_write":true}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/members", body)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("missing one-time token: %v, body=%s", err, rec.Body.String())
	}

	logRow := &mcpCallLog{RequestID: "req-log-detail", MemberName: "设备 A", Tool: "ask_wiki", Status: "success", Subject: "测试问题的完整请求内容", ResponseContent: "知识库完整回答"}
	store.addLog(logRow)
	if err := store.saveRecallSnapshot(knowledgeAnswer{RequestID: logRow.RequestID, RecalledNodes: []recalledNode{{NodeID: "node-detail", KnowledgeTitle: "业务说明", ContentExcerpt: "召回的知识内容"}}}); err != nil {
		t.Fatal(err)
	}
	logReq := httptest.NewRequest(http.MethodGet, "/admin/api/logs?tool=ask_wiki", nil)
	logReq.Header.Set("Authorization", "Bearer admin-secret")
	logRec := httptest.NewRecorder()
	mux.ServeHTTP(logRec, logReq)
	if logRec.Code != http.StatusOK || !bytes.Contains(logRec.Body.Bytes(), []byte("测试问题的完整请求内容")) {
		t.Fatalf("logs status=%d body=%s", logRec.Code, logRec.Body.String())
	}
	detailReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/api/logs/%d", logRow.ID), nil)
	detailReq.Header.Set("Authorization", "Bearer admin-secret")
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || !bytes.Contains(detailRec.Body.Bytes(), []byte("知识库完整回答")) || !bytes.Contains(detailRec.Body.Bytes(), []byte("召回的知识内容")) {
		t.Fatalf("log detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}

func TestMetricsReportsKnowledgeUse(t *testing.T) {
	store := newTestAdminStore(t)
	member, _, err := store.createMember("业务助手", true, false)
	if err != nil {
		t.Fatal(err)
	}
	store.addLog(&mcpCallLog{RequestID: "hit-1", MemberID: &member.ID, MemberName: member.Name, Tool: "ask_wiki", Status: "success", KnowledgeBaseID: "kb-1", DurationMS: 120})
	store.addLog(&mcpCallLog{RequestID: "miss-1", MemberID: &member.ID, MemberName: member.Name, Tool: "ask_rag", Status: "success", KnowledgeBaseID: "kb-1", DurationMS: 240})
	other, _, err := store.createMember("其他账号", true, false)
	if err != nil {
		t.Fatal(err)
	}
	store.addLog(&mcpCallLog{RequestID: "other-hit", MemberID: &other.ID, MemberName: other.Name, Tool: "ask_wiki", Status: "success", KnowledgeBaseID: "kb-2", DurationMS: 900})
	if err := store.saveRecallSnapshot(knowledgeAnswer{RequestID: "hit-1", RecalledNodes: []recalledNode{{NodeID: "node-1", KnowledgeBaseID: "kb-1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.saveRecallSnapshot(knowledgeAnswer{RequestID: "other-hit", RecalledNodes: []recalledNode{{NodeID: "node-2", KnowledgeBaseID: "kb-2"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Create(&mcpFeedback{ID: "feedback-member", MemberID: &member.ID, MemberName: member.Name, FeedbackType: "other", FeedbackText: "反馈"}).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := store.metrics(7, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.TotalCalls != 2 || metrics.KnowledgeQueries != 2 || metrics.KnowledgeHits != 1 || metrics.HitRate != 50 || metrics.ActiveMembers != 1 || metrics.FeedbackCount != 1 || len(metrics.TopKnowledgeBases) != 1 || metrics.TopKnowledgeBases[0].Name != "kb-1" {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}
}

func TestAdminFeedbackAPIIncludesTraceDetail(t *testing.T) {
	store := newTestAdminStore(t)
	seedRecallSnapshot(t, store)
	receipt, err := store.submitFeedback(context.Background(), feedbackInput{FeedbackText: "引用过时", FeedbackType: "outdated", RelatedRequestID: "req-1", TargetNodeIDs: []string{"node-1"}})
	if err != nil {
		t.Fatal(err)
	}
	admin := &adminHTTP{store: store, token: "admin-secret"}
	mux := http.NewServeMux()
	admin.register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/admin/api/feedback?trace_status=verified", nil)
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !bytes.Contains(listRec.Body.Bytes(), []byte(receipt.FeedbackID)) {
		t.Fatalf("feedback list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/admin/api/feedback/"+receipt.FeedbackID, nil)
	detailReq.Header.Set("Authorization", "Bearer admin-secret")
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || !bytes.Contains(detailRec.Body.Bytes(), []byte(`"node_id":"node-1"`)) || !bytes.Contains(detailRec.Body.Bytes(), []byte(`"content_hash":"sha256:`)) {
		t.Fatalf("feedback detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}
