package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

var ErrWikiPageDraftExists = errors.New("wiki page already has an active draft")

type wikiPageVersionService struct {
	versions interfaces.WikiPageVersionRepository
	pages    interfaces.WikiPageRepository
}

func NewWikiPageVersionService(versions interfaces.WikiPageVersionRepository, pages interfaces.WikiPageRepository) interfaces.WikiPageVersionService {
	return &wikiPageVersionService{versions: versions, pages: pages}
}

func (s *wikiPageVersionService) CreateDraft(ctx context.Context, pageID, actor, summary string, page *types.WikiPage) (*types.WikiPageVersion, error) {
	if pageID == "" {
		return nil, errors.New("page id is required")
	}
	if _, err := s.versions.GetDraft(ctx, pageID); err == nil {
		return nil, ErrWikiPageDraftExists
	} else if !errors.Is(err, repository.ErrWikiPageVersionNotFound) {
		return nil, err
	}
	// Always load the authoritative materialized page separately. On the first
	// draft, this is the source of the imported published snapshot; the caller's
	// page may already contain draft edits and must never seed published history.
	publishedPage, err := s.pages.GetByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if page == nil {
		page = publishedPage
	}
	if page.ID != pageID || page.KnowledgeBaseID != publishedPage.KnowledgeBaseID || page.TenantID != publishedPage.TenantID {
		return nil, errors.New("snapshot page id does not match")
	}
	draftSnapshot, err := json.Marshal(page)
	if err != nil {
		return nil, fmt.Errorf("marshal wiki page snapshot: %w", err)
	}
	parent := ""
	if current, currentErr := s.versions.GetCurrent(ctx, pageID); currentErr == nil {
		parent = current.ID
	} else if errors.Is(currentErr, repository.ErrWikiPageVersionNotFound) {
		// Existing installations already have authoritative pages but no version
		// rows. Seed the current page as the first immutable published snapshot so
		// the first draft does not erase the pre-versioning state or regress the
		// page's optimistic-lock revision number.
		publishedSnapshot, marshalErr := json.Marshal(publishedPage)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal published wiki page snapshot: %w", marshalErr)
		}
		seedVersion := publishedPage.Version
		if seedVersion < 1 {
			seedVersion = 1
		}
		now := time.Now()
		seed := &types.WikiPageVersion{ID: uuid.NewString(), TenantID: publishedPage.TenantID, KnowledgeBaseID: publishedPage.KnowledgeBaseID, PageID: pageID, Version: seedVersion, State: types.WikiPageVersionPublished, Snapshot: types.JSON(publishedSnapshot), ChangeSummary: "Imported current page", CreatedBy: actor, PublishedBy: actor, CreatedAt: now, PublishedAt: &now}
		if err := s.versions.Create(ctx, seed); err != nil {
			return nil, err
		}
		parent = seed.ID
	} else {
		return nil, currentErr
	}
	next, err := s.versions.NextVersion(ctx, pageID)
	if err != nil {
		return nil, err
	}
	version := &types.WikiPageVersion{ID: uuid.NewString(), TenantID: publishedPage.TenantID, KnowledgeBaseID: publishedPage.KnowledgeBaseID, PageID: pageID, Version: next, State: types.WikiPageVersionDraft, ParentVersionID: parent, Snapshot: types.JSON(draftSnapshot), ChangeSummary: summary, CreatedBy: actor, CreatedAt: time.Now()}
	if err := s.versions.Create(ctx, version); err != nil {
		return nil, err
	}
	return version, nil
}

func (s *wikiPageVersionService) List(ctx context.Context, pageID string) ([]*types.WikiPageVersion, error) {
	return s.versions.List(ctx, pageID)
}

func (s *wikiPageVersionService) UpdateDraft(ctx context.Context, pageID string, version int, summary string, page *types.WikiPage) (*types.WikiPageVersion, error) {
	if page == nil {
		return nil, errors.New("draft snapshot is required")
	}
	draft, err := s.versions.Get(ctx, pageID, version)
	if err != nil {
		return nil, err
	}
	if draft.State != types.WikiPageVersionDraft {
		return nil, errors.New("only draft versions can be edited")
	}
	publishedPage, err := s.pages.GetByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	page.ID, page.TenantID, page.KnowledgeBaseID = publishedPage.ID, publishedPage.TenantID, publishedPage.KnowledgeBaseID
	// Stable identity and routing fields are inherited from the logical page;
	// draft editing changes the page payload, not which node it represents.
	page.Slug = publishedPage.Slug
	snapshot, err := json.Marshal(page)
	if err != nil {
		return nil, fmt.Errorf("marshal wiki page draft: %w", err)
	}
	if err := s.versions.UpdateDraft(ctx, pageID, version, types.JSON(snapshot), summary); err != nil {
		return nil, err
	}
	draft.Snapshot, draft.ChangeSummary = types.JSON(snapshot), summary
	return draft, nil
}
func (s *wikiPageVersionService) Get(ctx context.Context, pageID string, version int) (*types.WikiPageVersion, error) {
	return s.versions.Get(ctx, pageID, version)
}

func (s *wikiPageVersionService) Diff(ctx context.Context, pageID string, from, to int) (*types.WikiPageVersionDiff, error) {
	a, err := s.versions.Get(ctx, pageID, from)
	if err != nil {
		return nil, err
	}
	b, err := s.versions.Get(ctx, pageID, to)
	if err != nil {
		return nil, err
	}
	var left, right types.WikiPage
	if err := json.Unmarshal(a.Snapshot, &left); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b.Snapshot, &right); err != nil {
		return nil, err
	}
	changes := make(map[string][2]any)
	for name, pair := range map[string][2]any{
		"title": {left.Title, right.Title}, "content": {left.Content, right.Content}, "summary": {left.Summary, right.Summary},
		"page_type": {left.PageType, right.PageType}, "status": {left.Status, right.Status}, "slug": {left.Slug, right.Slug},
		"aliases": {left.Aliases, right.Aliases}, "out_links": {left.OutLinks, right.OutLinks}, "page_metadata": {left.PageMetadata, right.PageMetadata},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			changes[name] = pair
		}
	}
	return &types.WikiPageVersionDiff{FromVersion: from, ToVersion: to, Changes: changes}, nil
}

func (s *wikiPageVersionService) Publish(ctx context.Context, pageID string, version int, actor string) (*types.WikiPageVersion, error) {
	v, err := s.versions.Get(ctx, pageID, version)
	if err != nil {
		return nil, err
	}
	if v.State != types.WikiPageVersionDraft {
		return nil, errors.New("only a draft version can be published")
	}
	page, err := s.pages.GetByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if err := s.versions.Publish(ctx, v, page, actor); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *wikiPageVersionService) Rollback(ctx context.Context, pageID string, version int, actor string) (*types.WikiPageVersion, error) {
	target, err := s.versions.Get(ctx, pageID, version)
	if err != nil {
		return nil, err
	}
	var snapshot types.WikiPage
	if err := json.Unmarshal(target.Snapshot, &snapshot); err != nil {
		return nil, err
	}
	draft, err := s.CreateDraft(ctx, pageID, actor, fmt.Sprintf("Rollback to version %d", version), &snapshot)
	if err != nil {
		return nil, err
	}
	return s.Publish(ctx, pageID, draft.Version, actor)
}

func (s *wikiPageVersionService) Archive(ctx context.Context, pageID string, version int) error {
	v, err := s.versions.Get(ctx, pageID, version)
	if err != nil {
		return err
	}
	if v.State == types.WikiPageVersionPublished {
		return errors.New("current published version cannot be archived")
	}
	return s.versions.Archive(ctx, pageID, version)
}
