package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var ErrWikiPageVersionNotFound = errors.New("wiki page version not found")

type wikiPageVersionRepository struct{ db *gorm.DB }

func NewWikiPageVersionRepository(db *gorm.DB) interfaces.WikiPageVersionRepository {
	return &wikiPageVersionRepository{db: db}
}

func (r *wikiPageVersionRepository) Create(ctx context.Context, version *types.WikiPageVersion) error {
	return r.db.WithContext(ctx).Create(version).Error
}

func (r *wikiPageVersionRepository) UpdateDraft(ctx context.Context, pageID string, version int, snapshot types.JSON, changeSummary string) error {
	result := r.db.WithContext(ctx).Model(&types.WikiPageVersion{}).
		Where("page_id = ? AND version = ? AND state = ?", pageID, version, types.WikiPageVersionDraft).
		Updates(map[string]any{"snapshot": snapshot, "change_summary": changeSummary})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWikiPageVersionNotFound
	}
	return nil
}

func (r *wikiPageVersionRepository) Get(ctx context.Context, pageID string, version int) (*types.WikiPageVersion, error) {
	var result types.WikiPageVersion
	err := r.db.WithContext(ctx).Where("page_id = ? AND version = ?", pageID, version).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWikiPageVersionNotFound
	}
	return &result, err
}

func (r *wikiPageVersionRepository) GetCurrent(ctx context.Context, pageID string) (*types.WikiPageVersion, error) {
	var result types.WikiPageVersion
	err := r.db.WithContext(ctx).Where("page_id = ? AND state = ?", pageID, types.WikiPageVersionPublished).Order("version DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWikiPageVersionNotFound
	}
	return &result, err
}

func (r *wikiPageVersionRepository) GetDraft(ctx context.Context, pageID string) (*types.WikiPageVersion, error) {
	var result types.WikiPageVersion
	err := r.db.WithContext(ctx).Where("page_id = ? AND state = ?", pageID, types.WikiPageVersionDraft).Order("version DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWikiPageVersionNotFound
	}
	return &result, err
}

func (r *wikiPageVersionRepository) List(ctx context.Context, pageID string) ([]*types.WikiPageVersion, error) {
	var result []*types.WikiPageVersion
	err := r.db.WithContext(ctx).Where("page_id = ?", pageID).Order("version DESC").Find(&result).Error
	return result, err
}

func (r *wikiPageVersionRepository) NextVersion(ctx context.Context, pageID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&types.WikiPageVersion{}).Where("page_id = ?", pageID).Select("COALESCE(MAX(version), 0)").Scan(&max).Error
	return max + 1, err
}

func (r *wikiPageVersionRepository) Publish(ctx context.Context, version *types.WikiPageVersion, page *types.WikiPage, actor string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked types.WikiPageVersion
		if err := tx.Where("id = ? AND state = ?", version.ID, types.WikiPageVersionDraft).First(&locked).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&types.WikiPageVersion{}).Where("page_id = ? AND state = ?", version.PageID, types.WikiPageVersionPublished).
			Updates(map[string]any{"state": types.WikiPageVersionHistory}).Error; err != nil {
			return err
		}
		if err := tx.Model(&locked).Updates(map[string]any{"state": types.WikiPageVersionPublished, "published_by": actor, "published_at": now}).Error; err != nil {
			return err
		}

		var restored types.WikiPage
		if err := restored.UnmarshalSnapshot(locked.Snapshot); err != nil {
			return err
		}
		restored.Version, restored.UpdatedAt = locked.Version, now
		result := tx.Model(&types.WikiPage{}).Where("id = ?", page.ID).
			Select("*").Omit("id", "tenant_id", "knowledge_base_id", "created_at", "deleted_at").Updates(&restored)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrWikiPageNotFound
		}
		version.State, version.PublishedBy, version.PublishedAt = types.WikiPageVersionPublished, actor, &now
		return nil
	})
}

func (r *wikiPageVersionRepository) Archive(ctx context.Context, pageID string, version int) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&types.WikiPageVersion{}).
		Where("page_id = ? AND version = ? AND state <> ?", pageID, version, types.WikiPageVersionPublished).
		Updates(map[string]any{"state": types.WikiPageVersionArchived, "archived_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWikiPageVersionNotFound
	}
	return nil
}
