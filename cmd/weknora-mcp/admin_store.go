package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mcpMember struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Name         string     `json:"name" gorm:"size:120;not null"`
	TokenHash    string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	TokenPrefix  string     `json:"token_prefix" gorm:"size:20;not null"`
	CanRead      bool       `json:"can_read" gorm:"not null;default:true"`
	CanWrite     bool       `json:"can_write" gorm:"not null;default:false"`
	Role         string     `json:"role" gorm:"size:24;not null;default:viewer"`
	AllowedTools []string   `json:"allowed_tools" gorm:"serializer:json;type:text"`
	Enabled      bool       `json:"enabled" gorm:"not null;default:true"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

type mcpCallLog struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	RequestID       string    `json:"request_id" gorm:"size:64;index"`
	MemberID        *uint     `json:"member_id" gorm:"index"`
	MemberName      string    `json:"member_name" gorm:"size:120;index"`
	Tool            string    `json:"tool" gorm:"size:64;index"`
	Status          string    `json:"status" gorm:"size:20;index"`
	ErrorMessage    string    `json:"error_message" gorm:"type:text"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"size:64;index"`
	Subject         string    `json:"subject" gorm:"type:text"`
	ResponseContent string    `json:"response_content" gorm:"type:text"`
	DurationMS      int64     `json:"duration_ms"`
	ClientIP        string    `json:"client_ip" gorm:"size:80"`
	UserAgent       string    `json:"user_agent" gorm:"size:300"`
	CreatedAt       time.Time `json:"created_at" gorm:"index"`
}

type adminStore struct{ db *gorm.DB }

type metricPoint struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

type metricRank struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Value int64  `json:"value"`
}

type adminMetrics struct {
	Days              int           `json:"days"`
	TotalCalls        int64         `json:"total_calls"`
	SuccessfulCalls   int64         `json:"successful_calls"`
	FailedCalls       int64         `json:"failed_calls"`
	SuccessRate       float64       `json:"success_rate"`
	KnowledgeQueries  int64         `json:"knowledge_queries"`
	KnowledgeHits     int64         `json:"knowledge_hits"`
	HitRate           float64       `json:"hit_rate"`
	NoAnswerRate      float64       `json:"no_answer_rate"`
	ActiveMembers     int64         `json:"active_members"`
	AverageDuration   int64         `json:"average_duration_ms"`
	P95Duration       int64         `json:"p95_duration_ms"`
	FeedbackCount     int64         `json:"feedback_count"`
	FeedbackRate      float64       `json:"feedback_rate"`
	Trend             []metricPoint `json:"trend"`
	TopTools          []metricRank  `json:"top_tools"`
	TopKnowledgeBases []metricRank  `json:"top_knowledge_bases"`
}

func openAdminStore(path string) (*adminStore, error) {
	if path == "" {
		path = "data/weknora-mcp-admin.db"
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&mcpMember{}, &mcpCallLog{}, &mcpCallReference{}, &mcpFeedback{}, &mcpFeedbackNode{}); err != nil {
		return nil, err
	}
	store := &adminStore{db: db}
	if err := store.migrateMemberPermissions(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *adminStore) migrateMemberPermissions() error {
	var members []mcpMember
	if err := s.db.Find(&members).Error; err != nil {
		return err
	}
	for i := range members {
		member := &members[i]
		role := normalizeRole(member.Role, member.CanWrite)
		member.Role = role
		member.CanRead = true
		member.CanWrite = role != roleViewer
		if member.AllowedTools == nil {
			member.AllowedTools = roleToolNames(role)
		}
		if err := s.db.Select("role", "can_read", "can_write", "allowed_tools").Updates(member).Error; err != nil {
			return err
		}
	}
	return nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:8] + "…" + token[len(token)-4:]
}

func generateMemberToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "wk_mcp_" + hex.EncodeToString(raw), nil
}

func (s *adminStore) bootstrapLegacy(name, token string, canWrite bool) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	hash := tokenHash(token)
	var existing mcpMember
	if err := s.db.Where("token_hash = ?", hash).First(&existing).Error; err == nil {
		if canWrite && !existing.CanWrite {
			existing.CanRead = true
			existing.CanWrite = true
			existing.Role = roleContributor
			existing.AllowedTools = roleToolNames(roleContributor)
			existing.Enabled = true
			return s.db.Select("can_read", "can_write", "role", "allowed_tools", "enabled").Updates(&existing).Error
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	role := normalizeRole("", canWrite)
	return s.db.Create(&mcpMember{Name: name, TokenHash: hash, TokenPrefix: tokenPrefix(token), CanRead: true, CanWrite: canWrite, Role: role, AllowedTools: roleToolNames(role), Enabled: true}).Error
}

func (s *adminStore) authenticate(token string) (*mcpMember, error) {
	if token == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var member mcpMember
	if err := s.db.Where("token_hash = ?", tokenHash(token)).First(&member).Error; err != nil {
		return nil, err
	}
	if !member.Enabled || !member.CanRead {
		return nil, errors.New("member disabled or read permission revoked")
	}
	now := time.Now()
	_ = s.db.Model(&member).Update("last_used_at", now).Error
	member.LastUsedAt = &now
	return &member, nil
}

func (s *adminStore) listMembers() ([]mcpMember, error) {
	var rows []mcpMember
	err := s.db.Order("created_at DESC").Find(&rows).Error
	return rows, err
}

func (s *adminStore) createMember(name string, canRead, canWrite bool) (*mcpMember, string, error) {
	role := normalizeRole("", canWrite)
	return s.createMemberWithPermissions(name, role, roleToolNames(role))
}

func (s *adminStore) createMemberWithPermissions(name, role string, requestedTools []string) (*mcpMember, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("name is required")
	}
	role = strings.ToLower(strings.TrimSpace(role))
	tools, err := normalizeAllowedTools(role, requestedTools)
	if err != nil {
		return nil, "", err
	}
	token, err := generateMemberToken()
	if err != nil {
		return nil, "", err
	}
	member := &mcpMember{Name: name, TokenHash: tokenHash(token), TokenPrefix: tokenPrefix(token), CanRead: true, CanWrite: role != roleViewer, Role: role, AllowedTools: tools, Enabled: true}
	if err := s.db.Create(member).Error; err != nil {
		return nil, "", err
	}
	return member, token, nil
}

func (s *adminStore) updateMember(id uint, values map[string]any) (*mcpMember, error) {
	var member mcpMember
	if err := s.db.First(&member, id).Error; err != nil {
		return nil, err
	}
	if value, ok := values["name"].(string); ok {
		member.Name = strings.TrimSpace(value)
	}
	if value, ok := values["enabled"].(bool); ok {
		member.Enabled = value
	}
	role := member.Role
	if value, ok := values["role"].(string); ok {
		role = strings.ToLower(strings.TrimSpace(value))
	} else if value, ok := values["can_write"].(bool); ok {
		role = normalizeRole("", value)
	}
	if !validRole(role) {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	requestedTools := member.AllowedTools
	if value, ok := values["allowed_tools"]; ok {
		requestedTools = nil
		items, ok := value.([]any)
		if !ok {
			if stringsList, ok := value.([]string); ok {
				requestedTools = stringsList
			} else {
				return nil, errors.New("allowed_tools must be an array")
			}
		} else {
			requestedTools = make([]string, 0, len(items))
			for _, item := range items {
				name, ok := item.(string)
				if !ok {
					return nil, errors.New("allowed_tools must contain only strings")
				}
				requestedTools = append(requestedTools, name)
			}
		}
	}
	tools, err := normalizeAllowedTools(role, requestedTools)
	if err != nil {
		return nil, err
	}
	member.Role = role
	member.AllowedTools = tools
	member.CanRead = true
	member.CanWrite = role != roleViewer
	if err := s.db.Select("name", "enabled", "role", "allowed_tools", "can_read", "can_write").Updates(&member).Error; err != nil {
		return nil, err
	}
	if err := s.db.First(&member, id).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (s *adminStore) rotateToken(id uint) (*mcpMember, string, error) {
	var member mcpMember
	if err := s.db.First(&member, id).Error; err != nil {
		return nil, "", err
	}
	token, err := generateMemberToken()
	if err != nil {
		return nil, "", err
	}
	if err := s.db.Model(&member).Updates(map[string]any{"token_hash": tokenHash(token), "token_prefix": tokenPrefix(token)}).Error; err != nil {
		return nil, "", err
	}
	member.TokenPrefix = tokenPrefix(token)
	return &member, token, nil
}

func (s *adminStore) addLog(row *mcpCallLog) {
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now()
	}
	_ = s.db.Create(row).Error
}

type logQuery struct {
	Page, PageSize int
	Tool, Status   string
	MemberID       uint
}

type mcpCallLogDetail struct {
	mcpCallLog
	References []mcpCallReference `json:"references"`
}

func (s *adminStore) listLogs(q logQuery) ([]mcpCallLog, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 50
	}
	db := s.db.Model(&mcpCallLog{})
	if q.Tool != "" {
		db = db.Where("tool = ?", q.Tool)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.MemberID != 0 {
		db = db.Where("member_id = ?", q.MemberID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []mcpCallLog
	err := db.Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	return rows, total, err
}

func (s *adminStore) getLogDetail(id uint) (*mcpCallLogDetail, error) {
	var row mcpCallLog
	if err := s.db.First(&row, id).Error; err != nil {
		return nil, err
	}
	references, err := s.referencesByRequestID(row.RequestID)
	if err != nil {
		return nil, err
	}
	return &mcpCallLogDetail{mcpCallLog: row, References: references}, nil
}

func (s *adminStore) overview() (map[string]any, error) {
	var members, enabled, calls, failed int64
	if err := s.db.Model(&mcpMember{}).Count(&members).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&mcpMember{}).Where("enabled = ?", true).Count(&enabled).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&mcpCallLog{}).Count(&calls).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&mcpCallLog{}).Where("status != ?", "success").Count(&failed).Error; err != nil {
		return nil, err
	}
	feedback, mismatches, err := s.feedbackCounts()
	if err != nil {
		return nil, err
	}
	return map[string]any{"members": members, "enabled_members": enabled, "calls": calls, "failed_calls": failed, "feedback": feedback, "trace_mismatches": mismatches}, nil
}

func metricPercent(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func (s *adminStore) metrics(days int, memberID uint) (*adminMetrics, error) {
	if days != 1 && days != 7 && days != 30 {
		days = 7
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	logs := func() *gorm.DB {
		db := s.db.Model(&mcpCallLog{}).Where("created_at >= ? AND tool != ?", start, "authentication")
		if memberID != 0 {
			db = db.Where("member_id = ?", memberID)
		}
		return db
	}
	out := &adminMetrics{Days: days, Trend: []metricPoint{}, TopTools: []metricRank{}, TopKnowledgeBases: []metricRank{}}
	if err := logs().Count(&out.TotalCalls).Error; err != nil {
		return nil, err
	}
	if err := logs().Where("status = ?", "success").Count(&out.SuccessfulCalls).Error; err != nil {
		return nil, err
	}
	out.FailedCalls = out.TotalCalls - out.SuccessfulCalls
	out.SuccessRate = metricPercent(out.SuccessfulCalls, out.TotalCalls)
	if err := logs().Where("tool IN ?", []string{"ask_wiki", "ask_rag"}).Count(&out.KnowledgeQueries).Error; err != nil {
		return nil, err
	}
	hitQuery := s.db.Model(&mcpCallLog{}).
		Where("mcp_call_logs.created_at >= ? AND mcp_call_logs.tool IN ?", start, []string{"ask_wiki", "ask_rag"}).
		Where("EXISTS (SELECT 1 FROM mcp_call_references r WHERE r.request_id = mcp_call_logs.request_id)")
	if memberID != 0 {
		hitQuery = hitQuery.Where("mcp_call_logs.member_id = ?", memberID)
	}
	if err := hitQuery.Count(&out.KnowledgeHits).Error; err != nil {
		return nil, err
	}
	out.HitRate = metricPercent(out.KnowledgeHits, out.KnowledgeQueries)
	out.NoAnswerRate = 100 - out.HitRate
	if out.KnowledgeQueries == 0 {
		out.NoAnswerRate = 0
	}
	if err := logs().Where("member_id IS NOT NULL").Distinct("member_id").Count(&out.ActiveMembers).Error; err != nil {
		return nil, err
	}
	var average *float64
	if err := logs().Select("AVG(duration_ms)").Scan(&average).Error; err != nil {
		return nil, err
	}
	if average != nil {
		out.AverageDuration = int64(*average)
	}
	var durations []int64
	if err := logs().Order("duration_ms ASC").Pluck("duration_ms", &durations).Error; err != nil {
		return nil, err
	}
	if len(durations) > 0 {
		out.P95Duration = durations[(len(durations)*95+99)/100-1]
	}
	feedbackQuery := s.db.Model(&mcpFeedback{}).Where("created_at >= ?", start)
	if memberID != 0 {
		feedbackQuery = feedbackQuery.Where("member_id = ?", memberID)
	}
	if err := feedbackQuery.Count(&out.FeedbackCount).Error; err != nil {
		return nil, err
	}
	out.FeedbackRate = metricPercent(out.FeedbackCount, out.KnowledgeQueries)

	var trendRows []struct {
		Label string
		Value int64
	}
	if err := logs().Select("date(created_at, 'localtime') AS label, count(*) AS value").Group("label").Order("label ASC").Scan(&trendRows).Error; err != nil {
		return nil, err
	}
	trendByDay := map[string]int64{}
	for _, row := range trendRows {
		trendByDay[row.Label] = row.Value
	}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		out.Trend = append(out.Trend, metricPoint{Label: key, Value: trendByDay[key]})
	}
	if err := logs().Select("tool AS name, count(*) AS value").Group("tool").Order("value DESC").Limit(5).Scan(&out.TopTools).Error; err != nil {
		return nil, err
	}
	if err := logs().Where("knowledge_base_id != ''").Select("knowledge_base_id AS name, count(*) AS value").Group("knowledge_base_id").Order("value DESC").Limit(5).Scan(&out.TopKnowledgeBases).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func parseUint(value string) (uint, error) {
	var id uint
	_, err := fmt.Sscan(value, &id)
	return id, err
}
