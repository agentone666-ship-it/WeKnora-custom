# Wiki System Implementation Guide

**Target Audience:** Developers implementing wiki features or extending the system  
**Last Updated:** April 2026

---

## Part 1: Common Tasks and Patterns

### Task 1: Create a New Wiki Page Programmatically

**Scenario:** Agent tool or batch process needs to create a page

```go
import (
    "context"
    "time"
    "github.com/google/uuid"
    "github.com/Tencent/WeKnora/internal/types"
    "github.com/Tencent/WeKnora/internal/types/interfaces"
)

func createEntityPage(
    ctx context.Context,
    svc interfaces.WikiPageService,
    kbID string,
    name string,
    details string,
) (*types.WikiPage, error) {
    page := &types.WikiPage{
        ID:              uuid.New().String(),
        KnowledgeBaseID: kbID,
        Slug:            slugify(name),  // "Acme Corp" → "entity/acme-corp"
        Title:           name,
        PageType:        types.WikiPageTypeEntity,
        Status:          types.WikiPageStatusPublished,
        Content:         formatMarkdown(details),
        Summary:         firstSentence(details),
        Aliases:         []string{},  // Alternative names
        SourceRefs:      []string{},  // Knowledge IDs
        Version:         1,
    }
    
    // Service handles link parsing and inbound link updates
    return svc.CreatePage(ctx, page)
}

// Helper: normalize slug like the service does
func slugify(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    s = strings.ReplaceAll(s, " ", "-")
    return "entity/" + s
}
```

**Key Points:**
- Service automatically parses OutLinks from content
- InLinks are updated on target pages automatically
- Version starts at 1
- Timestamps set by service

### Task 2: Update an Existing Page

**Scenario:** Need to modify page content (e.g., agent synthesis)

```go
func updatePageContent(
    ctx context.Context,
    svc interfaces.WikiPageService,
    kbID string,
    slug string,
    newContent string,
    newSummary string,
) (*types.WikiPage, error) {
    // 1. Fetch existing page
    existing, err := svc.GetPageBySlug(ctx, kbID, slug)
    if err != nil {
        return nil, fmt.Errorf("page not found: %w", err)
    }
    
    // 2. Update only the fields you want to change
    existing.Content = newContent
    existing.Summary = newSummary
    // DO NOT modify Version, OutLinks, InLinks, etc.
    
    // 3. Call UpdatePage
    // - Service re-parses OutLinks
    // - Service updates InLinks on target pages
    // - Service increments version (optimistic lock)
    updated, err := svc.UpdatePage(ctx, existing)
    if err != nil {
        // Might be ErrWikiPageConflict if version changed
        // Retry with fresh fetch if needed
        return nil, fmt.Errorf("update failed: %w", err)
    }
    
    return updated, nil
}

// INCORRECT: Do this instead
// Wrong: page.OutLinks = []string{...}  // Don't manually set!
// Wrong: page.InLinks = []string{...}   // Service maintains these!
```

**Handling Conflicts:**

```go
// If update fails with ErrWikiPageConflict, retry
func updatePageWithRetry(ctx context.Context, svc interfaces.WikiPageService, 
    kbID, slug, newContent string) (*types.WikiPage, error) {
    
    maxRetries := 3
    for attempt := 1; attempt <= maxRetries; attempt++ {
        existing, _ := svc.GetPageBySlug(ctx, kbID, slug)
        existing.Content = newContent
        
        updated, err := svc.UpdatePage(ctx, existing)
        if err == nil {
            return updated, nil
        }
        if !strings.Contains(err.Error(), "conflict") {
            return nil, err  // Not a conflict, real error
        }
        // Conflict: retry with fresh page
    }
    return nil, fmt.Errorf("max retries exceeded")
}
```

### Task 3: Work with Links Programmatically

**Scenario:** Need to query the link graph

```go
func analyzePageConnections(
    ctx context.Context,
    svc interfaces.WikiPageService,
    kbID string,
    slug string,
) {
    // Get the page
    page, _ := svc.GetPageBySlug(ctx, kbID, slug)
    
    // Pages that link TO this page (backlinks)
    fmt.Printf("Inbound links: %v\n", page.InLinks)    // []{"page-a", "page-b"}
    
    // Pages this page links to (references)
    fmt.Printf("Outbound links: %v\n", page.OutLinks)  // []{"page-c", "page-d"}
    
    // Get the full graph
    graph, _ := svc.GetGraph(ctx, kbID)
    
    // Find all pages that mention this page
    incomingReferences := make(map[string]string)
    for _, node := range graph.Nodes {
        p, _ := svc.GetPageBySlug(ctx, kbID, node.Slug)
        for _, outLink := range p.OutLinks {
            if outLink == slug {
                incomingReferences[node.Slug] = node.Title
            }
        }
    }
}
```

**Link Consistency Guarantees:**

```go
// If you fetch a page and it has OutLinks, those pages should exist
// (unless the link is broken)
page, _ := svc.GetPageBySlug(ctx, kbID, slug)
for _, target := range page.OutLinks {
    targetPage, err := svc.GetPageBySlug(ctx, kbID, target)
    if err != nil {
        // Broken link! Linter would flag this
        fmt.Printf("BROKEN: %s links to nonexistent %s\n", slug, target)
    }
}

// If you see page.InLinks, that page should have you in its OutLinks
// (Unless there's a race condition or bug)
page, _ := svc.GetPageBySlug(ctx, kbID, slug)
for _, inLink := range page.InLinks {
    inLinkPage, _ := svc.GetPageBySlug(ctx, kbID, inLink)
    if !contains(inLinkPage.OutLinks, slug) {
        fmt.Printf("INCONSISTENT: %s claims to link to us, but we're not in their OutLinks\n", inLink)
    }
}
```

### Task 4: Iterate Over All Pages Efficiently

**Scenario:** Batch processing, statistics generation

```go
func iterateAllPages(ctx context.Context, svc interfaces.WikiPageService, kbID string) error {
    // Option 1: ListAllPages (simple, loads everything into memory)
    pages, err := svc.ListAllPages(ctx, kbID)
    for _, page := range pages {
        processPage(page)
    }
    
    // Option 2: Paginated List (memory-efficient, used by UI)
    pageSize := 100
    for page := 1; ; page++ {
        req := &types.WikiPageListRequest{
            KnowledgeBaseID: kbID,
            Page:            page,
            PageSize:        pageSize,
            SortBy:          "updated_at",
            SortOrder:       "desc",
        }
        resp, _ := svc.ListPages(ctx, req)
        
        for _, p := range resp.Pages {
            processPage(p)
        }
        
        if resp.Page * resp.PageSize >= int(resp.Total) {
            break  // Last page
        }
    }
    
    return nil
}

// Filter by type
func getConceptPages(ctx context.Context, svc interfaces.WikiPageService, kbID string) ([]*types.WikiPage, error) {
    pages, _ := svc.ListAllPages(ctx, kbID)
    var concepts []*types.WikiPage
    for _, p := range pages {
        if p.PageType == types.WikiPageTypeConcept {
            concepts = append(concepts, p)
        }
    }
    return concepts, nil
}
```

### Task 5: Work with Cross-Link Injection

**Scenario:** Need to understand or extend linkification

```go
import (
    "github.com/Tencent/WeKnora/internal/application/service"
)

// The service-level function
func injectCrossLinksExample(ctx context.Context, svc interfaces.WikiPageService, kbID string) {
    // Get all pages to collect link references
    allPages, _ := svc.ListAllPages(ctx, kbID)
    
    // Build refs from entity/concept pages
    var refs []linkRef
    for _, p := range allPages {
        if p.PageType == types.WikiPageTypeEntity || p.PageType == types.WikiPageTypeConcept {
            // Each page becomes a potential link target
            ref := linkRef{
                slug:      p.Slug,
                matchText: p.Title,  // or p.Aliases[i]
            }
            refs = append(refs, ref)
        }
    }
    
    // Call the service (internal function, not exported)
    // Typically called by InjectCrossLinks() which batches this
    for _, p := range allPages {
        if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
            continue  // Don't inject into system pages
        }
        
        // linkifyContent() is in wiki_linkify.go, NOT exported
        // This is called internally by the service
    }
}
```

**Key Implementation Detail:**

The `linkifyContent` function is package-private (lowercase). You shouldn't call it directly. Instead:

```go
// Correct: Use service-level InjectCrossLinks
svc.InjectCrossLinks(ctx, kbID, []string{"entity/acme"})

// Incorrect: Don't try to call linkifyContent directly
// content, _ := linkifyContent(page.Content, refs, page.Slug)
// (Won't compile, it's in service package not exported)
```

---

## Part 2: Testing Patterns

### Unit Test: Page Creation and Links

```go
package service

import (
    "context"
    "testing"
    "github.com/Tencent/WeKnora/internal/types"
)

func TestCreatePageWithLinks(t *testing.T) {
    ctx := context.Background()
    
    // Setup
    repo := NewMockWikiPageRepository()
    svc := NewWikiPageService(repo, ...)
    
    // Create first page
    page1 := &types.WikiPage{
        Slug:    "entity/acme",
        Title:   "Acme Corp",
        Content: "[[entity/bob]] founded Acme.",
    }
    created1, _ := svc.CreatePage(ctx, page1)
    
    // Verify OutLinks parsed
    if len(created1.OutLinks) != 1 || created1.OutLinks[0] != "entity/bob" {
        t.Fatalf("OutLinks not parsed correctly")
    }
    
    // Create target page
    page2 := &types.WikiPage{
        Slug:  "entity/bob",
        Title: "Bob Smith",
    }
    created2, _ := svc.CreatePage(ctx, page2)
    
    // Verify InLinks updated
    // (Requires mocking updateInLinks or using integration test)
    refreshed2, _ := svc.GetPageBySlug(ctx, "entity/bob")
    if !contains(refreshed2.InLinks, "entity/acme") {
        t.Fatalf("InLinks not updated")
    }
}
```

### Integration Test: Full Pipeline

```go
func TestWikiIngestToRetrieval(t *testing.T) {
    // Setup: real DB, real LLM mock
    ctx := context.Background()
    db := setupTestDB()
    modelSvc := NewMockModelService()  // Mock LLM responses
    
    // 1. Create KB with wiki enabled
    kb := &types.KnowledgeBase{
        ID: "test-kb",
        WikiConfig: types.WikiConfig{
            Enabled:            true,
            AutoIngest:        true,
            SynthesisModelID:   "test-model",
        },
    }
    // ... save to DB
    
    // 2. Enqueue document
    ingestSvc := NewWikiIngestService(...)
    ingestSvc.EnqueueWikiIngest(ctx, kb.ID, "doc1", "Document Title")
    
    // 3. Manually trigger batch processing (or wait + poll)
    handler := NewProcessWikiIngestHandler(ingestSvc, ...)
    task, _ := asynq.NewTask("wiki:ingest", payload)
    handler(task)
    
    // 4. Verify pages created
    pages, _ := pageSvc.ListAllPages(ctx, kb.ID)
    if len(pages) == 0 {
        t.Fatalf("No pages created")
    }
    
    // 5. Verify chunks synchronized
    chunks, _ := chunkService.GetChunks(ctx, "wp-*")  // Wiki page chunks
    if len(chunks) != len(pages) {
        t.Fatalf("Not all pages chunked")
    }
    
    // 6. Search and verify boost
    results, _ := retriever.Search(ctx, "query")
    // Wiki page chunks should rank higher due to 1.3x boost
}
```

---

## Part 3: Error Handling

### Common Errors and Recovery

**ErrWikiPageNotFound**

```go
import (
    "errors"
    "github.com/Tencent/WeKnora/internal/application/repository"
)

page, err := svc.GetPageBySlug(ctx, kbID, slug)
if errors.Is(err, repository.ErrWikiPageNotFound) {
    // Page doesn't exist
    // Safe to create new page with this slug
    return svc.CreatePage(ctx, &types.WikiPage{Slug: slug, ...})
}
```

**ErrWikiPageConflict**

```go
existing, _ := svc.GetPageBySlug(ctx, kbID, slug)
existing.Content = newContent

updated, err := svc.UpdatePage(ctx, existing)
if errors.Is(err, repository.ErrWikiPageConflict) {
    // Another process updated the page
    // Retry: fetch fresh version and reapply changes
    existing, _ = svc.GetPageBySlug(ctx, kbID, slug)
    existing.Content = newContent
    updated, _ = svc.UpdatePage(ctx, existing)  // Should succeed now
}
```

**LLM Extraction Failures**

```go
// During wiki ingest, if LLM call fails:
// 1. Error logged, extraction returns empty
// 2. Document marked as "processed" anyway
// 3. No automatic retry
// Recovery: Manually re-upload document

// Check for failed extractions
for _, doc := range pendingOps {
    if doc.Status == "failed" {
        fmt.Printf("Document %s failed to extract\n", doc.KnowledgeID)
        // Re-upload or check logs for details
    }
}
```

---

## Part 4: Performance Optimization

### Batch Creation

**Don't do this:**

```go
// N separate Create calls → N DB round-trips, N link updates
for _, data := range largeBatch {
    page := mapToPage(data)
    svc.CreatePage(ctx, &page)  // Slow!
}
```

**Do this instead:**

```go
// Batch in the ingest service (30s debounce)
for _, data := range largeBatch {
    ingestSvc.EnqueueWikiIngest(ctx, kbID, data.KnowledgeID, data.Title)
}
// Service batches and processes in one async task
```

### Query Optimization

**List with Filters**

```go
// Good: Let repository handle filtering
req := &types.WikiPageListRequest{
    KnowledgeBaseID: kbID,
    PageType:        "entity",  // Filter in DB
    Status:          "published",  // Filter in DB
    Query:           "acme",  // Full-text search in DB
    PageSize:        50,
}
resp, _ := svc.ListPages(ctx, req)

// Bad: Fetch all and filter in memory
pages, _ := svc.ListAllPages(ctx, kbID)  // Loads everything!
var entities []*types.WikiPage
for _, p := range pages {
    if p.PageType == "entity" { entities = append(entities, p) }
}
```

### Link Rebuild Efficiency

```go
// RebuildLinks is O(n²) in worst case (all pages link to all others)
// For large wikis (>1000 pages):
// - Run in background
// - Consider scheduling off-peak
// - Monitor resource usage

// Example: scheduled maintenance
func scheduleNightlyRebuild(svc interfaces.WikiPageService) {
    go func() {
        ticker := time.NewTicker(24 * time.Hour)
        for range ticker.C {
            if shouldRebuild() {  // Check health score
                svc.RebuildLinks(context.Background(), kbID)
                logger.Infof(context.Background(), "nightly rebuild complete")
            }
        }
    }()
}
```

---

## Part 5: Debugging and Troubleshooting

### Check Page Consistency

```go
func validatePageConsistency(ctx context.Context, 
    svc interfaces.WikiPageService, kbID, slug string) []string {
    
    var issues []string
    page, _ := svc.GetPageBySlug(ctx, kbID, slug)
    
    // 1. Check OutLinks exist
    for _, target := range page.OutLinks {
        if target == page.Slug {
            issues = append(issues, fmt.Sprintf("Self-link detected"))
        }
        tp, err := svc.GetPageBySlug(ctx, kbID, target)
        if err != nil {
            issues = append(issues, fmt.Sprintf("Broken OutLink: %s", target))
        } else if !contains(tp.InLinks, slug) {
            issues = append(issues, fmt.Sprintf("InLink inconsistency: %s should have inbound from %s", target, slug))
        }
    }
    
    // 2. Check InLinks reciprocal
    for _, source := range page.InLinks {
        sp, _ := svc.GetPageBySlug(ctx, kbID, source)
        if !contains(sp.OutLinks, slug) {
            issues = append(issues, fmt.Sprintf("InLink inconsistency: %s claims to link here but doesn't", source))
        }
    }
    
    // 3. Check content matches OutLinks
    foundLinks := parseLinks(page.Content)
    for _, link := range foundLinks {
        if !contains(page.OutLinks, link) {
            issues = append(issues, fmt.Sprintf("Content has link [[%s]] not in OutLinks", link))
        }
    }
    
    return issues
}

// Usage
issues := validatePageConsistency(ctx, svc, kbID, "entity/acme")
if len(issues) > 0 {
    logger.Warnf(ctx, "Page consistency issues: %v", issues)
    // Consider running RebuildLinks to fix
}
```

### Monitor Wiki Health

```go
func monitorWikiHealth(ctx context.Context, 
    lintSvc *service.WikiLintService, kbID string) {
    
    report, _ := lintSvc.RunLint(ctx, kbID)
    
    fmt.Printf("Health Score: %d/100\n", report.HealthScore)
    fmt.Printf("Total Issues: %d\n", len(report.Issues))
    
    for _, issue := range report.Issues {
        fmt.Printf("[%s] %s: %s\n", 
            issue.Severity, issue.Type, issue.Description)
    }
    
    if report.HealthScore < 70 {
        logger.Warnf(ctx, "Wiki health below threshold: %d/100", report.HealthScore)
        // Consider triggering AutoFix
        fixed, _ := lintSvc.AutoFix(ctx, kbID)
        logger.Infof(ctx, "AutoFix resolved %d issues", fixed)
    }
}
```

---

## Part 6: Common Mistakes and How to Avoid Them

| Mistake | Problem | Fix |
|---------|---------|-----|
| Manually setting OutLinks | Links get out of sync | Let service parse from Content |
| Manually setting InLinks | Inbound refs become stale | Let service maintain via updateInLinks |
| Calling linkifyContent directly | Package is private | Use service.InjectCrossLinks |
| Not handling ErrWikiPageConflict | Updates silently fail | Add retry logic for conflicts |
| Storing page without timestamp | Audit trail lost | Set CreatedAt/UpdatedAt before Create |
| Mixing wiki and non-wiki KBs | Feature gating fails | Check IsWikiEnabled() always |
| Assuming slug format | Future formats might change | Use parseSlug() helper |
| Long-running UpdatePage loops | Timeout/deadlock risk | Batch with EnqueueWikiIngest |
| Ignoring LLM extraction errors | Partial data persists | Log and monitor failures |
| Direct database updates | Links become inconsistent | Always use service layer |

---

## Appendix A: Key Functions Reference

### WikiPageService

| Method | Purpose | When to Use |
|--------|---------|------------|
| CreatePage | Insert new page | Creating extracted/synthesized pages |
| UpdatePage | Update content (version bump) | Modifying page text, title, summary |
| UpdatePageMeta | Update metadata (no version bump) | Internal link maintenance |
| GetPageBySlug | Fetch by (kbID, slug) | Retrieve for display/editing |
| GetPageByID | Fetch by UUID | Less common, used internally |
| ListPages | Paginated list with filters | UI pagination, search |
| ListAllPages | All pages without pagination | Batch processing, analysis |
| SearchPages | Full-text search | User search queries |
| DeletePage | Soft delete | Remove pages |
| GetIndex | Fetch/create index page | Getting wiki directory |
| GetLog | Fetch/create log page | Getting operation history |
| GetGraph | Build link graph | Visualization, analysis |
| GetStats | Aggregate statistics | Dashboard, monitoring |
| RebuildLinks | Re-establish all references | Recovery after bugs |
| InjectCrossLinks | Auto-link pages | Post-ingest cross-linking |
| RebuildIndexPage | Regenerate index | After page changes |

### WikiLintService

| Method | Purpose | Returns |
|--------|---------|---------|
| RunLint | Health check | WikiLintReport (issues, score, stats) |
| AutoFix | Auto-repair issues | Number of fixed issues |

### WikiIngestService

| Method | Purpose | When to Use |
|--------|---------|------------|
| EnqueueWikiIngest | Queue document for processing | After document upload |
| EnqueueWikiRetract | Queue document deletion | When document deleted |

---

## Appendix B: Configuration

```yaml
# Example wiki config in knowledge base
wiki_config:
  enabled: true
  auto_ingest: true
  synthesis_model_id: "gpt-4"  # or "claude-3-opus", etc.
  max_pages_per_ingest: 0  # 0 = unlimited

# Environment-level defaults
WIKI_INGEST_DELAY: 30s              # Debounce window
WIKI_MAX_DOCS_PER_BATCH: 5          # Docs per batch
WIKI_MAX_CONTENT_SIZE: 33554432     # 32 MB per document
WIKI_PENDING_TTL: 86400             # 24 hours
WIKI_LOCK_TTL: 300                  # 5 minutes

# LLM-specific settings
WIKI_SYNTHESIS_MODEL: "gpt-4-turbo"
WIKI_EXTRACTION_MODEL: "gpt-4"
WIKI_TEMPERATURE: 0.3               # Lower = more deterministic
WIKI_MAX_TOKENS: 4000               # Per LLM call
```

---

## Appendix C: SQL Queries for Debugging

```sql
-- Find all orphan pages
SELECT slug, title, page_type, updated_at 
FROM wiki_pages 
WHERE knowledge_base_id = 'kb123'
  AND page_type NOT IN ('index', 'log')
  AND JSON_LENGTH(in_links) = 0
ORDER BY updated_at DESC;

-- Find broken links
SELECT DISTINCT p.slug, p.title, 
       JSON_UNQUOTE(JSON_EXTRACT(p.out_links, '$[*]')) as broken_target
FROM wiki_pages p
WHERE knowledge_base_id = 'kb123'
  AND JSON_LENGTH(p.out_links) > 0
  AND NOT EXISTS (
    SELECT 1 FROM wiki_pages t 
    WHERE t.knowledge_base_id = p.knowledge_base_id
      AND t.slug = JSON_UNQUOTE(JSON_EXTRACT(p.out_links, '$[0]'))
  );

-- Find highest-linked pages (hub pages)
SELECT slug, title, 
       JSON_LENGTH(in_links) as inbound_count,
       JSON_LENGTH(out_links) as outbound_count
FROM wiki_pages 
WHERE knowledge_base_id = 'kb123'
  AND page_type NOT IN ('index', 'log')
ORDER BY JSON_LENGTH(in_links) DESC
LIMIT 20;

-- Find pages by type distribution
SELECT page_type, COUNT(*) as count, 
       AVG(LENGTH(content)) as avg_size,
       AVG(JSON_LENGTH(in_links)) as avg_inlinks
FROM wiki_pages 
WHERE knowledge_base_id = 'kb123'
  AND deleted_at IS NULL
GROUP BY page_type
ORDER BY count DESC;

-- Monitor recent activity
SELECT slug, title, page_type, updated_at, version
FROM wiki_pages 
WHERE knowledge_base_id = 'kb123'
  AND deleted_at IS NULL
ORDER BY updated_at DESC
LIMIT 20;

-- Find pages by source knowledge
SELECT slug, title, COUNT(*) as source_count
FROM wiki_pages, JSON_TABLE(
  source_refs, '$[*]' COLUMNS (ref VARCHAR(100) PATH '$')
) jt
WHERE knowledge_base_id = 'kb123'
  AND knowledge_id = 'knowledgeID123'
GROUP BY slug, title;
```

---

## Summary

This guide covers the essential patterns for working with the wiki system. Key principles:

1. **Always use the service layer** — don't access repository directly
2. **Let the service maintain links** — don't manually set InLinks/OutLinks
3. **Handle concurrency** — retry on ErrWikiPageConflict
4. **Use the ingest pipeline** — batch operations via EnqueueWikiIngest
5. **Monitor health** — run RunLint periodically
6. **Debug with tools** — use GetGraph, GetStats, linter for troubleshooting

For more details, see `WIKI_TECHNICAL_ANALYSIS.md` for deep dives into architecture and edge cases.

