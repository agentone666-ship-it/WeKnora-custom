package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	store.addLog(&mcpCallLog{MemberName: "设备 A", Tool: "ask_wiki", Status: "success", Subject: "测试问题"})
	logReq := httptest.NewRequest(http.MethodGet, "/admin/api/logs?tool=ask_wiki", nil)
	logReq.Header.Set("Authorization", "Bearer admin-secret")
	logRec := httptest.NewRecorder()
	mux.ServeHTTP(logRec, logReq)
	if logRec.Code != http.StatusOK || !bytes.Contains(logRec.Body.Bytes(), []byte("测试问题")) {
		t.Fatalf("logs status=%d body=%s", logRec.Code, logRec.Body.String())
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
