package types

import (
	"encoding/json"
	"time"
)

const (
	WikiPageVersionDraft     = "draft"
	WikiPageVersionPublished = "published"
	WikiPageVersionHistory   = "history"
	WikiPageVersionArchived  = "archived"
)

// WikiPageVersion is a snapshot of a logical wiki page. Draft snapshots may be
// edited until publication; published/history snapshots remain immutable.
type WikiPageVersion struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);index:idx_wiki_page_versions_list,priority:1"`
	PageID          string     `json:"page_id" gorm:"type:varchar(36);uniqueIndex:idx_wiki_page_version_number,priority:1;index:idx_wiki_page_versions_list,priority:2"`
	Version         int        `json:"version" gorm:"uniqueIndex:idx_wiki_page_version_number,priority:2"`
	State           string     `json:"state" gorm:"type:varchar(24);index:idx_wiki_page_versions_list,priority:3"`
	ParentVersionID string     `json:"parent_version_id,omitempty" gorm:"type:varchar(36);index"`
	Snapshot        JSON       `json:"snapshot" gorm:"type:json"`
	ChangeSummary   string     `json:"change_summary,omitempty" gorm:"type:text"`
	CreatedBy       string     `json:"created_by,omitempty" gorm:"type:varchar(255)"`
	PublishedBy     string     `json:"published_by,omitempty" gorm:"type:varchar(255)"`
	CreatedAt       time.Time  `json:"created_at"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
}

func (WikiPageVersion) TableName() string { return "wiki_page_versions" }

// UnmarshalSnapshot decodes a version snapshot into a page while keeping JSON
// typed fields on their database/sql Valuer implementations.
func (p *WikiPage) UnmarshalSnapshot(snapshot JSON) error { return json.Unmarshal(snapshot, p) }

type WikiPageVersionDiff struct {
	FromVersion int               `json:"from_version"`
	ToVersion   int               `json:"to_version"`
	Changes     map[string][2]any `json:"changes"`
}
