package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type WikiPageVersionRepository interface {
	Create(ctx context.Context, version *types.WikiPageVersion) error
	UpdateDraft(ctx context.Context, pageID string, version int, snapshot types.JSON, changeSummary string) error
	Get(ctx context.Context, pageID string, version int) (*types.WikiPageVersion, error)
	GetCurrent(ctx context.Context, pageID string) (*types.WikiPageVersion, error)
	GetDraft(ctx context.Context, pageID string) (*types.WikiPageVersion, error)
	List(ctx context.Context, pageID string) ([]*types.WikiPageVersion, error)
	NextVersion(ctx context.Context, pageID string) (int, error)
	Publish(ctx context.Context, version *types.WikiPageVersion, page *types.WikiPage, actor string) error
	Archive(ctx context.Context, pageID string, version int) error
}

type WikiPageVersionService interface {
	CreateDraft(ctx context.Context, pageID, actor, summary string, page *types.WikiPage) (*types.WikiPageVersion, error)
	UpdateDraft(ctx context.Context, pageID string, version int, summary string, page *types.WikiPage) (*types.WikiPageVersion, error)
	List(ctx context.Context, pageID string) ([]*types.WikiPageVersion, error)
	Get(ctx context.Context, pageID string, version int) (*types.WikiPageVersion, error)
	Diff(ctx context.Context, pageID string, from, to int) (*types.WikiPageVersionDiff, error)
	Publish(ctx context.Context, pageID string, version int, actor string) (*types.WikiPageVersion, error)
	Rollback(ctx context.Context, pageID string, version int, actor string) (*types.WikiPageVersion, error)
	Archive(ctx context.Context, pageID string, version int) error
}
