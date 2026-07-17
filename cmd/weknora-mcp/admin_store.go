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
	ID          uint       `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name" gorm:"size:120;not null"`
	TokenHash   string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"size:20;not null"`
	CanRead     bool       `json:"can_read" gorm:"not null;default:true"`
	CanWrite    bool       `json:"can_write" gorm:"not null;default:false"`
	Enabled     bool       `json:"enabled" gorm:"not null;default:true"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
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
	DurationMS      int64     `json:"duration_ms"`
	ClientIP        string    `json:"client_ip" gorm:"size:80"`
	UserAgent       string    `json:"user_agent" gorm:"size:300"`
	CreatedAt       time.Time `json:"created_at" gorm:"index"`
}

type adminStore struct{ db *gorm.DB }

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
	return &adminStore{db: db}, nil
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
			return s.db.Model(&existing).Updates(map[string]any{"can_read": true, "can_write": true, "enabled": true}).Error
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.db.Create(&mcpMember{Name: name, TokenHash: hash, TokenPrefix: tokenPrefix(token), CanRead: true, CanWrite: canWrite, Enabled: true}).Error
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
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("name is required")
	}
	token, err := generateMemberToken()
	if err != nil {
		return nil, "", err
	}
	member := &mcpMember{Name: name, TokenHash: tokenHash(token), TokenPrefix: tokenPrefix(token), CanRead: canRead || canWrite, CanWrite: canWrite, Enabled: true}
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
	updates := map[string]any{}
	for _, key := range []string{"name", "can_read", "can_write", "enabled"} {
		if value, ok := values[key]; ok {
			updates[key] = value
		}
	}
	if write, ok := updates["can_write"].(bool); ok && write {
		updates["can_read"] = true
	}
	if len(updates) > 0 {
		if err := s.db.Model(&member).Updates(updates).Error; err != nil {
			return nil, err
		}
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

func parseUint(value string) (uint, error) {
	var id uint
	_, err := fmt.Sscan(value, &id)
	return id, err
}
