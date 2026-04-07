package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// WikiPageService defines the wiki page service interface.
// Provides high-level operations for wiki page CRUD, link management,
// and chunk synchronization.
type WikiPageService interface {
	// CreatePage creates a new wiki page, parses outbound links, updates
	// bidirectional link references, and syncs to chunks for retrieval.
	CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error)

	// UpdatePage updates an existing wiki page, re-parses links,
	// updates bidirectional references, increments version, and re-syncs chunks.
	UpdatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error)

	// GetPageBySlug retrieves a wiki page by its slug within a knowledge base.
	GetPageBySlug(ctx context.Context, kbID string, slug string) (*types.WikiPage, error)

	// GetPageByID retrieves a wiki page by its unique ID.
	GetPageByID(ctx context.Context, id string) (*types.WikiPage, error)

	// ListPages lists wiki pages with optional filtering and pagination.
	ListPages(ctx context.Context, req *types.WikiPageListRequest) (*types.WikiPageListResponse, error)

	// DeletePage soft-deletes a wiki page and removes its chunk sync.
	DeletePage(ctx context.Context, kbID string, slug string) error

	// GetIndex returns the index page for a knowledge base.
	// Creates a default one if it doesn't exist.
	GetIndex(ctx context.Context, kbID string) (*types.WikiPage, error)

	// GetLog returns the log page for a knowledge base.
	// Creates a default one if it doesn't exist.
	GetLog(ctx context.Context, kbID string) (*types.WikiPage, error)

	// GetGraph returns the link graph data for visualization.
	GetGraph(ctx context.Context, kbID string) (*types.WikiGraphData, error)

	// GetStats returns aggregate statistics about the wiki.
	GetStats(ctx context.Context, kbID string) (*types.WikiStats, error)

	// RebuildLinks re-parses all pages and rebuilds bidirectional link references.
	RebuildLinks(ctx context.Context, kbID string) error

	// SearchPages performs full-text search over wiki pages.
	SearchPages(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error)
}

// WikiPageRepository defines the wiki page data persistence interface.
type WikiPageRepository interface {
	// Create inserts a new wiki page record.
	Create(ctx context.Context, page *types.WikiPage) error

	// Update updates an existing wiki page record.
	Update(ctx context.Context, page *types.WikiPage) error

	// GetByID retrieves a wiki page by its unique ID.
	GetByID(ctx context.Context, id string) (*types.WikiPage, error)

	// GetBySlug retrieves a wiki page by slug within a knowledge base.
	GetBySlug(ctx context.Context, kbID string, slug string) (*types.WikiPage, error)

	// List retrieves wiki pages with filtering and pagination.
	List(ctx context.Context, req *types.WikiPageListRequest) ([]*types.WikiPage, int64, error)

	// ListByType retrieves all wiki pages of a given type within a knowledge base.
	ListByType(ctx context.Context, kbID string, pageType string) ([]*types.WikiPage, error)

	// ListBySourceRef retrieves all wiki pages that reference a given source knowledge ID.
	ListBySourceRef(ctx context.Context, kbID string, sourceKnowledgeID string) ([]*types.WikiPage, error)

	// ListAll retrieves all wiki pages in a knowledge base (for link rebuilding, graph generation).
	ListAll(ctx context.Context, kbID string) ([]*types.WikiPage, error)

	// Delete soft-deletes a wiki page by knowledge base ID and slug.
	Delete(ctx context.Context, kbID string, slug string) error

	// DeleteByID soft-deletes a wiki page by ID.
	DeleteByID(ctx context.Context, id string) error

	// Search performs full-text search on wiki pages within a knowledge base.
	Search(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error)

	// CountByType returns page counts grouped by type for a knowledge base.
	CountByType(ctx context.Context, kbID string) (map[string]int64, error)

	// CountOrphans returns the number of pages with no inbound links.
	CountOrphans(ctx context.Context, kbID string) (int64, error)
}
