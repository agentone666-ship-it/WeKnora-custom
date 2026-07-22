package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWikiPageVersionLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wiki-version-lifecycle?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageVersion{}))

	ctx := context.Background()
	page := &types.WikiPage{ID: "page-1", TenantID: 9, KnowledgeBaseID: "kb-1", Slug: "concept/rag", Title: "RAG", Content: "first", Summary: "v1", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, Version: 1}
	require.NoError(t, db.Create(page).Error)
	pageRepo := repository.NewWikiPageRepository(db)
	versionRepo := repository.NewWikiPageVersionRepository(db)
	svc := NewWikiPageVersionService(versionRepo, pageRepo)

	v1, err := svc.CreateDraft(ctx, page.ID, "alice", "initial", page)
	require.NoError(t, err)
	require.Equal(t, 2, v1.Version)
	draftEdit := *page
	draftEdit.Content, draftEdit.Summary = "edited in draft", "draft summary"
	v1, err = svc.UpdateDraft(ctx, page.ID, v1.Version, "edit before publish", &draftEdit)
	require.NoError(t, err)
	require.Equal(t, "edit before publish", v1.ChangeSummary)
	materializedBeforePublish, err := pageRepo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, "first", materializedBeforePublish.Content)
	_, err = svc.CreateDraft(ctx, page.ID, "alice", "duplicate", page)
	require.ErrorIs(t, err, ErrWikiPageDraftExists)
	v1, err = svc.Publish(ctx, page.ID, v1.Version, "reviewer")
	require.NoError(t, err)
	require.Equal(t, types.WikiPageVersionPublished, v1.State)

	edited, err := pageRepo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, "edited in draft", edited.Content)
	edited.Content, edited.Summary = "second", "v2"
	v2, err := svc.CreateDraft(ctx, page.ID, "bob", "expand content", edited)
	require.NoError(t, err)
	require.Equal(t, v1.ID, v2.ParentVersionID)
	diff, err := svc.Diff(ctx, page.ID, 2, 3)
	require.NoError(t, err)
	require.Equal(t, [2]any{"edited in draft", "second"}, diff.Changes["content"])
	_, err = svc.Publish(ctx, page.ID, 3, "reviewer")
	require.NoError(t, err)

	current, err := pageRepo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, "second", current.Content)
	require.Equal(t, 3, current.Version)
	history, err := svc.Get(ctx, page.ID, 1)
	require.NoError(t, err)
	require.Equal(t, types.WikiPageVersionHistory, history.State)
	require.NoError(t, svc.Archive(ctx, page.ID, 1))
	require.Error(t, svc.Archive(ctx, page.ID, 3))

	rolledBack, err := svc.Rollback(ctx, page.ID, 1, "admin")
	require.NoError(t, err)
	require.Equal(t, 4, rolledBack.Version)
	current, err = pageRepo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, "first", current.Content)
	require.Equal(t, 4, current.Version)

	versions, err := svc.List(ctx, page.ID)
	require.NoError(t, err)
	require.Len(t, versions, 4)
	require.Equal(t, []int{4, 3, 2, 1}, []int{versions[0].Version, versions[1].Version, versions[2].Version, versions[3].Version})
}

func TestWikiPageVersionNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wiki-version-not-found?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageVersion{}))
	svc := NewWikiPageVersionService(repository.NewWikiPageVersionRepository(db), repository.NewWikiPageRepository(db))
	_, err = svc.Get(context.Background(), "missing", 1)
	require.True(t, errors.Is(err, repository.ErrWikiPageVersionNotFound))
}

func TestWikiPageVersionFirstDraftPreservesPublishedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wiki-version-first-draft?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}, &types.WikiPageVersion{}))

	ctx := context.Background()
	published := &types.WikiPage{ID: "page-first-draft", TenantID: 9, KnowledgeBaseID: "kb-1", Slug: "concept/original", Title: "Original", Content: "published content", Summary: "published summary", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, Version: 7}
	require.NoError(t, db.Create(published).Error)

	pageRepo := repository.NewWikiPageRepository(db)
	svc := NewWikiPageVersionService(repository.NewWikiPageVersionRepository(db), pageRepo)
	draftPage := *published
	draftPage.Content = "draft content"
	draftPage.Summary = "draft summary"

	draft, err := svc.CreateDraft(ctx, published.ID, "alice", "edit existing page", &draftPage)
	require.NoError(t, err)
	require.Equal(t, 8, draft.Version)

	diff, err := svc.Diff(ctx, published.ID, 7, 8)
	require.NoError(t, err)
	require.Equal(t, [2]any{"published content", "draft content"}, diff.Changes["content"])
	require.Equal(t, [2]any{"published summary", "draft summary"}, diff.Changes["summary"])

	_, err = svc.Publish(ctx, published.ID, draft.Version, "reviewer")
	require.NoError(t, err)
	current, err := pageRepo.GetByID(ctx, published.ID)
	require.NoError(t, err)
	require.Equal(t, "draft content", current.Content)

	rolledBack, err := svc.Rollback(ctx, published.ID, 7, "admin")
	require.NoError(t, err)
	require.Equal(t, 9, rolledBack.Version)
	current, err = pageRepo.GetByID(ctx, published.ID)
	require.NoError(t, err)
	require.Equal(t, "published content", current.Content)
	require.Equal(t, "published summary", current.Summary)
}
