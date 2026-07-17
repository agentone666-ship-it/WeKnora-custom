package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type adminHTTP struct {
	store *adminStore
	token string
}

func (a *adminHTTP) register(mux *http.ServeMux) {
	mux.HandleFunc("/admin", a.page)
	mux.HandleFunc("/admin/", a.page)
	mux.HandleFunc("/admin/api/overview", a.withAuth(a.overview))
	mux.HandleFunc("/admin/api/members", a.withAuth(a.members))
	mux.HandleFunc("/admin/api/members/", a.withAuth(a.memberAction))
	mux.HandleFunc("/admin/api/logs", a.withAuth(a.logs))
	mux.HandleFunc("/admin/api/feedback", a.withAuth(a.feedback))
	mux.HandleFunc("/admin/api/feedback/", a.withAuth(a.feedbackDetail))
}

func (a *adminHTTP) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(adminHTML))
}

func (a *adminHTTP) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "管理密钥无效"})
			return
		}
		next(w, r)
	}
}

func (a *adminHTTP) overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	data, err := a.store.overview()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
}

func (a *adminHTTP) members(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.store.listMembers()
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": rows})
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			CanRead  bool   `json:"can_read"`
			CanWrite bool   `json:"can_write"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]any{"success": false, "error": "请求格式错误"})
			return
		}
		member, token, err := a.store.createMember(req.Name, req.CanRead, req.CanWrite)
		if err != nil {
			writeJSON(w, 400, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "data": member, "token": token})
	default:
		methodNotAllowed(w)
	}
}

func (a *adminHTTP) memberAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/api/members/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := parseUint(parts[0])
	if err != nil {
		writeJSON(w, 400, map[string]any{"success": false, "error": "成员 ID 无效"})
		return
	}
	if len(parts) == 2 && parts[1] == "rotate-token" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		member, token, err := a.store.rotateToken(id)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": member, "token": token})
		return
	}
	if len(parts) != 1 || r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		writeJSON(w, 400, map[string]any{"success": false, "error": "请求格式错误"})
		return
	}
	member, err := a.store.updateMember(id, values)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": member})
}

func (a *adminHTTP) logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	memberID64, _ := strconv.ParseUint(r.URL.Query().Get("member_id"), 10, 64)
	rows, total, err := a.store.listLogs(logQuery{Page: page, PageSize: pageSize, Tool: r.URL.Query().Get("tool"), Status: r.URL.Query().Get("status"), MemberID: uint(memberID64)})
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": rows, "total": total})
}

func (a *adminHTTP) feedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	rows, total, err := a.store.listFeedback(feedbackQuery{Page: page, PageSize: pageSize, FeedbackType: r.URL.Query().Get("feedback_type"), TraceStatus: r.URL.Query().Get("trace_status")})
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": rows, "total": total})
}

func (a *adminHTTP) feedbackDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/admin/api/feedback/"))
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	row, err := a.store.getFeedback(id)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": row})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAdminError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
}
