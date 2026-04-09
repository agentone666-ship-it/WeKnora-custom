package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// WikiPageType constants define the types of wiki pages
const (
	// WikiPageTypeSummary represents a document summary page
	WikiPageTypeSummary = "summary"
	// WikiPageTypeEntity represents an entity page (person, organization, place, etc.)
	WikiPageTypeEntity = "entity"
	// WikiPageTypeConcept represents a concept/topic page
	WikiPageTypeConcept = "concept"
	// WikiPageTypeIndex represents the wiki index page (index.md)
	WikiPageTypeIndex = "index"
	// WikiPageTypeLog represents the operation log page (log.md)
	WikiPageTypeLog = "log"
	// WikiPageTypeSynthesis represents a synthesis/analysis page.
	// NOT auto-created by ingest — Agent creates these via wiki_write_page tool
	// when it generates cross-document analysis, trends, or insights during conversations.
	WikiPageTypeSynthesis = "synthesis"
	// WikiPageTypeComparison represents a comparison page.
	// NOT auto-created by ingest — Agent creates these via wiki_write_page tool
	// when the user asks to compare entities, concepts, or approaches.
	WikiPageTypeComparison = "comparison"
)

// WikiPageStatus constants
const (
	// WikiPageStatusDraft indicates the page is a draft
	WikiPageStatusDraft = "draft"
	// WikiPageStatusPublished indicates the page is published and visible
	WikiPageStatusPublished = "published"
	// WikiPageStatusArchived indicates the page is archived
	WikiPageStatusArchived = "archived"
)

// WikiPage represents a single wiki page in a wiki knowledge base.
// Wiki pages are LLM-generated, interlinked markdown documents that form
// a persistent, compounding knowledge artifact.
type WikiPage struct {
	// Unique identifier (UUID)
	ID string `json:"id" gorm:"type:varchar(36);primaryKey"`
	// Tenant ID for multi-tenant isolation
	TenantID uint64 `json:"tenant_id" gorm:"index"`
	// Knowledge base this page belongs to
	KnowledgeBaseID string `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	// URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag"
	// Unique within a knowledge base
	Slug string `json:"slug" gorm:"type:varchar(255);uniqueIndex:idx_kb_slug"`
	// Human-readable title
	Title string `json:"title" gorm:"type:varchar(512)"`
	// Page type: summary, entity, concept, index, log, synthesis, comparison
	PageType string `json:"page_type" gorm:"type:varchar(32);index"`
	// Page status: draft, published, archived
	Status string `json:"status" gorm:"type:varchar(32);default:'published'"`
	// Full markdown content
	Content string `json:"content" gorm:"type:text"`
	// One-line summary for index listing
	Summary string `json:"summary" gorm:"type:text"`
	// Alternate names, abbreviations, acronyms or translated names
	Aliases StringArray `json:"aliases" gorm:"type:json"`
	// References to source knowledge IDs that contributed to this page
	SourceRefs StringArray `json:"source_refs" gorm:"type:json"`
	// Slugs of pages that link TO this page (backlinks)
	InLinks StringArray `json:"in_links" gorm:"type:json"`
	// Slugs of pages this page links to (outbound links)
	OutLinks StringArray `json:"out_links" gorm:"type:json"`
	// Arbitrary metadata (tags, categories, dates, etc.)
	PageMetadata JSON `json:"page_metadata" gorm:"column:page_metadata;type:json"`
	// Version number, incremented on each update
	Version int `json:"version" gorm:"default:1"`
	// Creation time
	CreatedAt time.Time `json:"created_at"`
	// Last update time
	UpdatedAt time.Time `json:"updated_at"`
	// Soft delete
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

// TableName specifies the database table name
func (WikiPage) TableName() string {
	return "wiki_pages"
}

// WikiConfig stores wiki-specific configuration for a knowledge base.
// Applicable to document-type knowledge bases with wiki feature enabled.
// When Enabled is true, document ingestion triggers automatic wiki page generation.
type WikiConfig struct {
	// Enabled activates the wiki feature for this knowledge base
	Enabled bool `yaml:"enabled" json:"enabled"`
	// AutoIngest triggers wiki page generation/update when new documents are added
	AutoIngest bool `yaml:"auto_ingest" json:"auto_ingest"`
	// SynthesisModelID is the LLM model ID used for wiki page generation and updates
	SynthesisModelID string `yaml:"synthesis_model_id" json:"synthesis_model_id"`
	// MaxPagesPerIngest limits pages created/updated per ingest operation (0 = no limit)
	MaxPagesPerIngest int `yaml:"max_pages_per_ingest" json:"max_pages_per_ingest"`
}

// Value implements the driver.Valuer interface
func (c WikiConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface
func (c *WikiConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// WikiPageListRequest represents a request to list wiki pages with filtering
type WikiPageListRequest struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	PageType        string `json:"page_type,omitempty"`   // filter by type
	Status          string `json:"status,omitempty"`      // filter by status
	Query           string `json:"query,omitempty"`       // full-text search
	Page            int    `json:"page,omitempty"`        // pagination page (1-based)
	PageSize        int    `json:"page_size,omitempty"`   // pagination size
	SortBy          string `json:"sort_by,omitempty"`     // "updated_at", "created_at", "title"
	SortOrder       string `json:"sort_order,omitempty"`  // "asc" or "desc"
}

// WikiPageListResponse represents a paginated list of wiki pages
type WikiPageListResponse struct {
	Pages      []*WikiPage `json:"pages"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// WikiGraphData represents the link graph structure for visualization
type WikiGraphData struct {
	Nodes []WikiGraphNode `json:"nodes"`
	Edges []WikiGraphEdge `json:"edges"`
}

// WikiGraphNode represents a node in the wiki link graph
type WikiGraphNode struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	PageType string `json:"page_type"`
	// Number of inbound + outbound links
	LinkCount int `json:"link_count"`
}

// WikiGraphEdge represents a directed edge in the wiki link graph
type WikiGraphEdge struct {
	Source string `json:"source"` // source slug
	Target string `json:"target"` // target slug
}

// WikiStats provides aggregate statistics about the wiki
type WikiStats struct {
	TotalPages    int64            `json:"total_pages"`
	PagesByType   map[string]int64 `json:"pages_by_type"`
	TotalLinks    int64            `json:"total_links"`
	OrphanCount   int64            `json:"orphan_count"`    // pages with no inbound links
	RecentUpdates []*WikiPage      `json:"recent_updates"`  // last N updated pages
	PendingTasks  int64            `json:"pending_tasks"`   // number of documents waiting to be ingested
	IsActive      bool             `json:"is_active"`       // whether wiki ingestion is currently running
}
