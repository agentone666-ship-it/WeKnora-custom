package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// wikiLinkRegex matches [[wiki-link]] syntax in markdown content
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// wikiPageService implements the WikiPageService interface
type wikiPageService struct {
	repo      interfaces.WikiPageRepository
	chunkRepo interfaces.ChunkRepository
	kbService interfaces.KnowledgeBaseService
}

// NewWikiPageService creates a new wiki page service
func NewWikiPageService(
	repo interfaces.WikiPageRepository,
	chunkRepo interfaces.ChunkRepository,
	kbService interfaces.KnowledgeBaseService,
) interfaces.WikiPageService {
	return &wikiPageService{
		repo:      repo,
		chunkRepo: chunkRepo,
		kbService: kbService,
	}
}

// CreatePage creates a new wiki page
func (s *wikiPageService) CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	if page.ID == "" {
		page.ID = uuid.New().String()
	}
	if page.Slug == "" {
		return nil, errors.New("wiki page slug is required")
	}
	if page.KnowledgeBaseID == "" {
		return nil, errors.New("knowledge_base_id is required")
	}
	if page.Status == "" {
		page.Status = types.WikiPageStatusPublished
	}
	if page.Version == 0 {
		page.Version = 1
	}

	// Parse outbound links from content
	page.OutLinks = s.parseOutLinks(page.Content)

	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now

	if err := s.repo.Create(ctx, page); err != nil {
		return nil, fmt.Errorf("create wiki page: %w", err)
	}

	// Update inbound links on target pages
	s.updateInLinks(ctx, page.KnowledgeBaseID, page.Slug, page.OutLinks)

	return page, nil
}

// UpdatePage updates an existing wiki page
func (s *wikiPageService) UpdatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	existing, err := s.repo.GetBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	if err != nil {
		return nil, fmt.Errorf("get existing page: %w", err)
	}

	oldOutLinks := existing.OutLinks

	// Update fields (version is incremented by the repository's optimistic lock)
	existing.Title = page.Title
	existing.Content = page.Content
	existing.Summary = page.Summary
	existing.PageType = page.PageType
	existing.SourceRefs = page.SourceRefs
	existing.PageMetadata = page.PageMetadata
	existing.Status = page.Status
	existing.UpdatedAt = time.Now()

	// Re-parse outbound links
	existing.OutLinks = s.parseOutLinks(existing.Content)

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update wiki page: %w", err)
	}

	// Update inbound links: remove old, add new
	s.removeInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, oldOutLinks)
	s.updateInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, existing.OutLinks)

	return existing, nil
}

// UpdatePageMeta updates only metadata (status, source_refs) without version bump or link re-parse.
func (s *wikiPageService) UpdatePageMeta(ctx context.Context, page *types.WikiPage) error {
	page.UpdatedAt = time.Now()
	return s.repo.UpdateMeta(ctx, page)
}

// GetPageBySlug retrieves a wiki page by its slug
func (s *wikiPageService) GetPageBySlug(ctx context.Context, kbID string, slug string) (*types.WikiPage, error) {
	return s.repo.GetBySlug(ctx, kbID, slug)
}

// GetPageByID retrieves a wiki page by its ID
func (s *wikiPageService) GetPageByID(ctx context.Context, id string) (*types.WikiPage, error) {
	return s.repo.GetByID(ctx, id)
}

// ListPages lists wiki pages with optional filtering and pagination
func (s *wikiPageService) ListPages(ctx context.Context, req *types.WikiPageListRequest) (*types.WikiPageListResponse, error) {
	pages, total, err := s.repo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	return &types.WikiPageListResponse{
		Pages:      pages,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// DeletePage soft-deletes a wiki page
func (s *wikiPageService) DeletePage(ctx context.Context, kbID string, slug string) error {
	page, err := s.repo.GetBySlug(ctx, kbID, slug)
	if err != nil {
		return err
	}

	// Remove inbound link references from pages this page links to
	s.removeInLinks(ctx, kbID, slug, page.OutLinks)

	// Delete the page
	if err := s.repo.Delete(ctx, kbID, slug); err != nil {
		return err
	}

	// Delete synced chunk
	s.deleteChunkForPage(ctx, page)

	return nil
}

// GetIndex returns the index page for a knowledge base
func (s *wikiPageService) GetIndex(ctx context.Context, kbID string) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, "index")
	if err != nil {
		if errors.Is(err, repository.ErrWikiPageNotFound) {
			// Create default index page
			return s.createDefaultPage(ctx, kbID, "index", "Index", types.WikiPageTypeIndex,
				"# Wiki Index\n\nThis is the index page. It will be automatically updated as pages are added.\n")
		}
		return nil, err
	}
	return page, nil
}

// GetLog returns the log page for a knowledge base
func (s *wikiPageService) GetLog(ctx context.Context, kbID string) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, "log")
	if err != nil {
		if errors.Is(err, repository.ErrWikiPageNotFound) {
			return s.createDefaultPage(ctx, kbID, "log", "Log", types.WikiPageTypeLog,
				"# Wiki Operation Log\n\nChronological record of wiki operations.\n")
		}
		return nil, err
	}
	return page, nil
}

// GetGraph returns the link graph data for visualization
func (s *wikiPageService) GetGraph(ctx context.Context, kbID string) (*types.WikiGraphData, error) {
	pages, err := s.repo.ListAll(ctx, kbID)
	if err != nil {
		return nil, err
	}

	nodeMap := make(map[string]*types.WikiGraphNode)
	var edges []types.WikiGraphEdge

	// Build nodes
	for _, p := range pages {
		linkCount := len(p.InLinks) + len(p.OutLinks)
		nodeMap[p.Slug] = &types.WikiGraphNode{
			Slug:      p.Slug,
			Title:     p.Title,
			PageType:  p.PageType,
			LinkCount: linkCount,
		}
	}

	// Build edges from outbound links
	for _, p := range pages {
		for _, target := range p.OutLinks {
			if _, exists := nodeMap[target]; exists {
				edges = append(edges, types.WikiGraphEdge{
					Source: p.Slug,
					Target: target,
				})
			}
		}
	}

	nodes := make([]types.WikiGraphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, *n)
	}

	return &types.WikiGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// GetStats returns aggregate statistics about the wiki
func (s *wikiPageService) GetStats(ctx context.Context, kbID string) (*types.WikiStats, error) {
	counts, err := s.repo.CountByType(ctx, kbID)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, c := range counts {
		total += c
	}

	orphans, err := s.repo.CountOrphans(ctx, kbID)
	if err != nil {
		return nil, err
	}

	// Count total links
	pages, err := s.repo.ListAll(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var totalLinks int64
	for _, p := range pages {
		totalLinks += int64(len(p.OutLinks))
	}

	// Get recent updates (last 10)
	listReq := &types.WikiPageListRequest{
		KnowledgeBaseID: kbID,
		Page:            1,
		PageSize:        10,
		SortBy:          "updated_at",
		SortOrder:       "desc",
	}
	recentPages, _, err := s.repo.List(ctx, listReq)
	if err != nil {
		return nil, err
	}

	return &types.WikiStats{
		TotalPages:    total,
		PagesByType:   counts,
		TotalLinks:    totalLinks,
		OrphanCount:   orphans,
		RecentUpdates: recentPages,
	}, nil
}

// RebuildLinks re-parses all pages and rebuilds bidirectional link references
func (s *wikiPageService) RebuildLinks(ctx context.Context, kbID string) error {
	pages, err := s.repo.ListAll(ctx, kbID)
	if err != nil {
		return err
	}

	// Build slug-to-page map
	pageMap := make(map[string]*types.WikiPage)
	for _, p := range pages {
		pageMap[p.Slug] = p
	}

	// Clear all inbound links first
	for _, p := range pages {
		p.InLinks = types.StringArray{}
	}

	// Re-parse outbound links and rebuild inbound links
	for _, p := range pages {
		p.OutLinks = s.parseOutLinks(p.Content)
		for _, target := range p.OutLinks {
			if tp, exists := pageMap[target]; exists {
				tp.InLinks = append(tp.InLinks, p.Slug)
			}
		}
	}

	// Save all pages (link rebuild is metadata-only, no version bump)
	for _, p := range pages {
		p.UpdatedAt = time.Now()
		if err := s.repo.UpdateMeta(ctx, p); err != nil {
			logger.Warnf(ctx, "wiki: failed to update links for page %s: %v", p.Slug, err)
		}
	}

	return nil
}

// ListAllPages retrieves all wiki pages without pagination.
func (s *wikiPageService) ListAllPages(ctx context.Context, kbID string) ([]*types.WikiPage, error) {
	return s.repo.ListAll(ctx, kbID)
}

// SearchPages performs full-text search over wiki pages
func (s *wikiPageService) SearchPages(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error) {
	return s.repo.Search(ctx, kbID, query, limit)
}

// --- Internal helpers ---

// parseOutLinks extracts [[wiki-link]] slugs from markdown content
func (s *wikiPageService) parseOutLinks(content string) types.StringArray {
	matches := wikiLinkRegex.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var links types.StringArray

	for _, match := range matches {
		if len(match) > 1 {
			slug := strings.TrimSpace(match[1])
			// Handle [[Title|slug]] format
			if parts := strings.SplitN(slug, "|", 2); len(parts) == 2 {
				slug = strings.TrimSpace(parts[1])
			}
			slug = normalizeSlug(slug)
			if slug != "" && !seen[slug] {
				seen[slug] = true
				links = append(links, slug)
			}
		}
	}
	return links
}

// normalizeSlug normalizes a wiki link slug
func normalizeSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

// updateInLinks adds the source slug to the in_links of target pages
func (s *wikiPageService) updateInLinks(ctx context.Context, kbID string, sourceSlug string, targets types.StringArray) {
	for _, targetSlug := range targets {
		targetPage, err := s.repo.GetBySlug(ctx, kbID, targetSlug)
		if err != nil {
			continue // target page may not exist yet
		}
		if !containsString(targetPage.InLinks, sourceSlug) {
			targetPage.InLinks = append(targetPage.InLinks, sourceSlug)
			targetPage.UpdatedAt = time.Now()
			if err := s.repo.UpdateMeta(ctx, targetPage); err != nil {
				logger.Warnf(ctx, "wiki: failed to update in_links for %s: %v", targetSlug, err)
			}
		}
	}
}

// removeInLinks removes the source slug from the in_links of target pages
func (s *wikiPageService) removeInLinks(ctx context.Context, kbID string, sourceSlug string, targets types.StringArray) {
	for _, targetSlug := range targets {
		targetPage, err := s.repo.GetBySlug(ctx, kbID, targetSlug)
		if err != nil {
			continue
		}
		newInLinks := removeString(targetPage.InLinks, sourceSlug)
		if len(newInLinks) != len(targetPage.InLinks) {
			targetPage.InLinks = newInLinks
			targetPage.UpdatedAt = time.Now()
			if err := s.repo.UpdateMeta(ctx, targetPage); err != nil {
				logger.Warnf(ctx, "wiki: failed to update in_links for %s: %v", targetSlug, err)
			}
		}
	}
}

// deleteChunkForPage removes the synced chunk for a wiki page
func (s *wikiPageService) deleteChunkForPage(ctx context.Context, page *types.WikiPage) {
	chunkID := "wp-" + page.ID
	if err := s.chunkRepo.DeleteChunk(ctx, page.TenantID, chunkID); err != nil {
		logger.Warnf(ctx, "wiki: failed to delete chunk for page %s: %v", page.Slug, err)
	}
}

// createDefaultPage creates a default system page (index, log)
func (s *wikiPageService) createDefaultPage(ctx context.Context, kbID string, slug string, title string, pageType string, content string) (*types.WikiPage, error) {
	// Get KB to get tenant ID
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get knowledge base: %w", err)
	}

	page := &types.WikiPage{
		ID:              uuid.New().String(),
		TenantID:        kb.TenantID,
		KnowledgeBaseID: kbID,
		Slug:            slug,
		Title:           title,
		PageType:        pageType,
		Status:          types.WikiPageStatusPublished,
		Content:         content,
		Summary:         title,
		Version:         1,
	}

	if err := s.repo.Create(ctx, page); err != nil {
		return nil, fmt.Errorf("create default %s page: %w", slug, err)
	}
	return page, nil
}

// containsString checks if a string slice contains a given string
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a slice
func removeString(slice []string, s string) types.StringArray {
	result := make(types.StringArray, 0, len(slice))
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
