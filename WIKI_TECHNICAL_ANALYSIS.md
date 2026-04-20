# WeKnora Wiki System: Comprehensive Technical Analysis

**Last Updated:** April 2026  
**Scope:** WeKnora internal wiki knowledge base system with focus on architecture, patterns, and edge cases

## Executive Summary

The WeKnora wiki system is a sophisticated knowledge graph engine that transforms document ingestion into interlinked markdown pages. It combines:

- **Async batch processing** with debouncing and deduplication
- **MAP-REDUCE parallel extraction** for scalable knowledge synthesis
- **Text-based cross-link injection** with sophisticated forbidden-span detection
- **Optimistic locking** for concurrent update safety
- **Bidirectional link maintenance** for graph traversal
- **Retrieval integration** via chunking and scoring plugins

This analysis covers architectural patterns, implementation details, edge cases, and operational considerations.

---

## Part 1: Architectural Overview

### 1.1 System Context

**Purpose:** Convert source documents into a queryable, interlinked knowledge graph that:
- Extracts entities and concepts from documents
- Synthesizes multi-document knowledge into canonical pages
- Maintains bidirectional link references for graph queries
- Injects cross-links into page content via pattern matching
- Integrates with chat pipeline for enhanced retrieval

**Key Design Decision: Wiki as Feature Flag**

Wiki is NOT a separate KB type. Instead:
- Wiki is a feature enabled via `WikiConfig` struct embedded in `KnowledgeBase`
- A KB can enable/disable wiki independently of KB type
- This allows wikis to coexist with traditional document-only KBs
- Enables progressive rollout and A/B testing

```go
type WikiConfig struct {
    Enabled              bool   // Feature flag
    AutoIngest          bool   // Trigger on doc upload
    SynthesisModelID    string // LLM model for generation
    MaxPagesPerIngest   int    // Rate limiting (0 = unlimited)
}
```

### 1.2 Core Data Flow

```
Upload Document
    ↓
Document Chunking (standard pipeline)
    ↓
EnqueueWikiIngest() → Redis List (wiki:pending:{kbID})
    ↓
[DEBOUNCE: 30-second window]
    ↓
ProcessWikiIngest() [Async Task Handler]
    ├─ Acquire Redis Lock (wiki:active:{kbID}, 5-min TTL)
    ├─ MAP Phase: Extract entities/concepts from all pending docs
    ├─ REDUCE Phase: Synthesize updates per slug
    ├─ Post-processing: Inject links, rebuild index, update log
    └─ Schedule follow-up if pending items remain
    ↓
Database Updates
    ├─ Create/update WikiPage records
    ├─ Update bidirectional links (InLinks/OutLinks)
    └─ Version bump with optimistic locking
    ↓
Chunk Synchronization
    ├─ Index each page as ChunkType="wiki_page"
    ├─ Apply 1.3x score boost in retrieval (WikiBoost plugin)
    └─ Integrate with existing vector/keyword search
```

### 1.3 Key Files and Responsibilities

| File | Lines | Purpose |
|------|-------|---------|
| `wiki_page.go` | 628 | Core CRUD, link management, graph queries, stats |
| `wiki_ingest.go` | 1,053 | Enqueue operations, debouncing, task scheduling |
| `wiki_ingest_batch.go` | 796 | MAP-REDUCE handler, LLM calls, synthesis |
| `wiki_linkify.go` | 565 | Cross-link injection with forbidden-span detection |
| `wiki_lint.go` | 323 | Health checks, orphan detection, link validation |
| `wiki_page.go` (types) | 196 | Data models, constants, interfaces |

---

## Part 2: Core Concepts and Patterns

### 2.1 Bidirectional Link References

**Problem:** Graph queries require knowing both "pages I link to" and "pages that link to me".

**Solution:** Maintain dual arrays on every WikiPage:
- `OutLinks []string` — pages this page references (populated during content parsing)
- `InLinks []string` — pages that reference this page (maintained reactively)

**Implementation:**

```go
// When creating/updating a page with content "[[entity/acme-corp]]..."
// 1. Parse content → OutLinks = ["entity/acme-corp"]
// 2. For each target in OutLinks:
//    - Fetch target page
//    - Add source slug to target.InLinks
//    - UpdateMeta() → no version bump

// When deleting a page:
// 1. For each target in OutLinks:
//    - Remove source slug from target.InLinks
//    - UpdateMeta() → maintains referential consistency
```

**Correctness Mechanism:** `RebuildLinks()` operation
- Full refresh of all bidirectional links
- Useful after bugs or batch migrations
- Iterates all pages, clears InLinks, rebuilds from OutLinks

**Edge Cases Handled:**
- Circular references (A links to B, B links to A) ✓
- Self-links (A links to A) — explicitly prevented
- Dangling links (A links to nonexistent B) — detected by linter, broken-link issue

### 2.2 Optimistic Locking with Version Numbers

**Problem:** Concurrent updates to the same page could result in lost writes.

**Solution:** Version numbers with conditional update check:

```go
// Repository.Update()
result := db.Model(page).
    Where("id = ? AND version = ?", page.ID, expectedVersion).
    Updates(page)  // Increments version to expectedVersion+1

if result.RowsAffected == 0 {
    // Conflict detected — return ErrWikiPageConflict
    // Caller must retry with fresh version
}
```

**Metadata vs. Content Updates:**
- `Update()` → increments version (user-visible content change)
- `UpdateMeta()` → does NOT increment version (internal link bookkeeping)

**Design Rationale:**
- Content changes visible to users bump the version
- Link maintenance is transparent, doesn't count as a change
- Prevents version thrashing from routine background operations

### 2.3 Async Batch Processing with Debouncing

**Problem:** Many small documents might trigger expensive LLM calls individually.

**Solution:** 30-second debounce window + natural batching:

```
T=0s: Document A uploaded → enqueue → schedule task @ T=30s
T=5s: Document B uploaded → enqueue → reschedule task @ T=35s
T=10s: Document C uploaded → enqueue → reschedule task @ T=40s
T=40s: Task fires with [A, B, C] batched together
```

**Implementation:**

```go
// wiki_ingest.go: EnqueueWikiIngest()
// 1. Create pending op with knowledge ID + ingest/retract flag
// 2. Push to Redis list: wiki:pending:{kbID}
// 3. Schedule asynq task with 30s delay (default)
// 4. If task already scheduled, asynq updates its ETA

// ProcessWikiIngest() [async handler]
// 1. Acquire Redis lock wiki:active:{kbID} (5-min TTL + refresh goroutine)
// 2. Peek all pending items (limit: 5 per batch by default)
// 3. Deduplicate by knowledge_id (keep last op for each)
// 4. MAP phase: extract from 10 parallel goroutines
// 5. REDUCE phase: synthesize in 10 parallel goroutines
// 6. If items remain: schedule follow-up task
```

**Constants:**
- `wikiIngestDelay` = 30 seconds (debounce window)
- `wikiMaxDocsPerBatch` = 5 documents per batch
- `maxContentForWiki` = 32 KB max content size per document
- `wikiPendingTTL` = 24 hours (pending operations auto-expire)

### 2.4 MAP-REDUCE Pattern for Knowledge Extraction

**Problem:** How to efficiently extract entities/concepts from multiple documents and synthesize cross-document knowledge?

**Solution:** Two-phase parallel processing:

#### MAP Phase (Document-Level Extraction)

```
For each pending document:
  1. Fetch document chunks (full content via knowledge service)
  2. Call LLM WikiKnowledgeExtractPrompt → {entities[], concepts[]}
  3. For each extracted item:
     - Check if slug already exists in KB
     - If exists and being updated: include in "additions" list
     - If new: create new slug
  4. Generate slug/display name for summary page
  5. Accumulate all updates in []SlugUpdate (per document)
  6. Run in parallel (10 goroutines by default)
```

**Output:** `map[slug][]SlugUpdate` — accumulated updates keyed by target slug

#### REDUCE Phase (Cross-Document Synthesis)

```
For each affected slug:
  1. Fetch existing page content (if exists)
  2. Group all accumulated updates:
     - additions: new info from multiple documents
     - retractions: deletions from removed documents
     - existing sources: sources NOT being deleted
  3. Call LLM WikiPageModifyPrompt:
     - Input: existing page + new info + info to remove
     - Output: updated page content + new summary
  4. Increment version, update timestamps
  5. Run in parallel (10 goroutines by default)
```

**Deduplication Strategy:**

```
If Document A extracted [entity: "Acme Corp", slug: "entity/acme-corp"]
And Document B extracts [entity: "Acme Corp", slug: "entity/acme-corp"]
  → Both map to the same page
  → Only one REDUCE operation for this slug
  → Both doc IDs end up in SourceRefs for that page
```

**LLM Conflict Detection:**

WikiPageModifyPrompt includes:
```
"CRITICAL CONFLICT CHECK: First verify that the <new_information> 
describes the EXACT SAME core entity/concept as this page. 
If the new info clearly belongs to a DIFFERENT but related 
entity, you MUST REJECT that part."
```

This prevents LLM from merging "Hunyuan Model" into "Qwen3 Model" page.

### 2.5 Cross-Link Injection with Forbidden Spans

**Problem:** Auto-linking all mentions of entity names creates false matches (e.g., "AI" inside "TRAINING") and overwrites existing structure.

**Solution:** Sophisticated pattern matching with forbidden-zone detection:

```go
// linkifyContent(content, refs, selfSlug)
// 1. Sort refs by matchText length (longer names first)
//    → prevents "北京" from matching inside "北京邮电大学"
// 2. Compute forbidden spans:
//    - Fenced code blocks (``` or ~~~)
//    - Inline code (backtick-delimited)
//    - Wiki links [[...]]
//    - Markdown links [text](url)
//    - Images ![alt](url)
//    - Reference-style links [text][label]
//    - Reference definitions [label]: url
//    - Autolinks <url>
// 3. For each ref:
//    - Find first safe match (outside forbidden zones)
//    - If ASCII-letter needle: require word boundaries
//    - Skip if slug already used (first-match-only)
//    - Skip if self-slug
//    - Inject [[slug|matchText]]
//    - Shift forbidden spans to track byte offsets
```

**Word Boundary Rules:**

- ASCII-letter needles (A-Z, a-z, 0-9, _) require boundaries
- Non-ASCII (CJK, Arabic, etc.) treated as inherent boundaries
- Example:
  ```
  "TRAINING" → NO match for "AI" (inside word)
  "AI is cool" → MATCHES "AI" (standalone)
  "北京邮电大学" → MATCHES "北京" at start (CJK boundary)
  ```

**First-Match-Only Semantics:**

Each slug links exactly once per page. After first injection:
- Move forbidden zone marker past the new link
- Track slug in "used" set
- Skip if same slug appears again

**Idempotence Guarantee:**

Running linkifyContent twice on same content:
```go
content1, changed1 := linkifyContent(input, refs, "self")  // changed1 = true
content2, changed2 := linkifyContent(content1, refs, "self") // changed2 = false
// content1 == content2 (no double-wrapping)
```

Test case confirms:
```go
once, _ := linkifyContent(input, refs, "")
twice, changed := linkifyContent(once, refs, "")
if changed {
    t.Fatalf("second run should be a no-op")
}
```

---

## Part 3: Detailed Implementation Patterns

### 3.1 Repository Layer: Optimistic Locking

**Methods:**

| Method | Behavior | Version Bump |
|--------|----------|--------------|
| `Create()` | INSERT new page | No (caller sets) |
| `Update()` | Conditional update WHERE version=expected | YES (+1) |
| `UpdateMeta()` | Update only link/status/source_refs fields | NO |
| `Delete()` | Soft delete (set DeletedAt) | No |
| `GetByID()` | SELECT by ID | N/A |
| `GetBySlug()` | SELECT by (kbID, slug) | N/A |
| `List()` | Paginated SELECT with filters | N/A |
| `ListAll()` | All pages in KB | N/A |
| `ListByType()` | All pages of type X | N/A |
| `CountByType()` | Pages per type | N/A |
| `CountOrphans()` | Pages with len(InLinks)==0 | N/A |
| `Search()` | Full-text + alias search | N/A |

**Query Details for List():**

```go
// PostgreSQL full-text search:
WHERE (to_tsvector('simple', coalesce(title, '') || ' ' || 
       coalesce(content, '')) @@ plainto_tsquery('simple', ?) 
       OR aliases::text ILIKE ?)
// Matches title, content, and aliases
```

**GetBySlug Uniqueness:**
```go
WHERE knowledge_base_id = ? AND slug = ?
// Slug must be unique per KB (enforced by unique index)
```

### 3.2 Service Layer: Multi-Step Operations

#### CreatePage Flow

```go
func (s *wikiPageService) CreatePage(ctx context.Context, page *WikiPage) (*WikiPage, error) {
    // 1. Validation
    if page.Slug == "" { return nil, "slug required" }
    if page.KnowledgeBaseID == "" { return nil, "kb_id required" }
    
    // 2. Parse outbound links from content
    page.OutLinks = s.parseOutLinks(page.Content)
    
    // 3. Timestamps
    now := time.Now()
    page.CreatedAt = now
    page.UpdatedAt = now
    
    // 4. Repository insert
    if err := s.repo.Create(ctx, page); err != nil {
        return nil, err  // Duplicate slug will fail here
    }
    
    // 5. Update inbound links on targets (async, fire-and-forget)
    s.updateInLinks(ctx, page.KnowledgeBaseID, page.Slug, page.OutLinks)
    
    return page, nil
}
```

#### UpdatePage Flow

```go
func (s *wikiPageService) UpdatePage(ctx context.Context, page *WikiPage) (*WikiPage, error) {
    // 1. Fetch existing to get old links + version
    existing, err := s.repo.GetBySlug(ctx, page.KnowledgeBaseID, page.Slug)
    
    // 2. Save old OutLinks for cleanup
    oldOutLinks := existing.OutLinks
    
    // 3. Update fields (caller provides new values)
    existing.Title = page.Title
    existing.Content = page.Content
    existing.Summary = page.Summary
    // ... etc
    
    // 4. Re-parse links from new content
    existing.OutLinks = s.parseOutLinks(existing.Content)
    existing.UpdatedAt = time.Now()
    
    // 5. Optimistic lock update (increments version)
    if err := s.repo.Update(ctx, existing); err != nil {
        return nil, err
        // If conflict: caller must retry with fresh version
    }
    
    // 6. Update inbound links: remove old, add new
    s.removeInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, oldOutLinks)
    s.updateInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, existing.OutLinks)
    
    return existing, nil
}
```

#### UpdatePageMeta Flow

```go
func (s *wikiPageService) UpdatePageMeta(ctx context.Context, page *WikiPage) error {
    // Fire-and-forget metadata update
    // Used for: link maintenance, status changes, source refs
    // NO version bump
    page.UpdatedAt = time.Now()
    return s.repo.UpdateMeta(ctx, page)
}
```

### 3.3 Async Pipeline: WikiIngestService

**Enqueue Operation:**

```go
func (s *WikiIngestService) EnqueueWikiIngest(
    ctx context.Context, 
    kbID string, 
    knowledgeID string, 
    title string,
) error {
    // 1. Validate KB has wiki enabled
    kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
    if !kb.IsWikiEnabled() { return "wiki not enabled" }
    
    // 2. Create pending operation
    op := WikiPendingOp{
        KnowledgeID: knowledgeID,
        Title:       title,
        Type:        "ingest",  // or "retract"
        EnqueuedAt:  time.Now(),
    }
    
    // 3. Push to Redis list
    redisKey := fmt.Sprintf("wiki:pending:%s", kbID)
    s.redisClient.RPush(ctx, redisKey, json.Marshal(op))
    
    // 4. Schedule async task (30s delay, auto-rescheduled if already queued)
    payload := WikiIngestPayload{KnowledgeBaseID: kbID}
    task, err := asynq.NewTask("wiki:ingest", payload.Marshal())
    opts := []asynq.Option{
        asynq.ProcessIn(30 * time.Second),
        asynq.Queue("wiki"),
    }
    s.taskClient.Enqueue(task, opts...)
    
    return nil
}
```

**Batch Processing:**

```go
func ProcessWikiIngest(task *asynq.Task) error {
    // 1. Parse payload
    var payload WikiIngestPayload
    json.Unmarshal(task.Payload(), &payload)
    kbID := payload.KnowledgeBaseID
    
    // 2. Acquire Redis lock (wiki:active:{kbID})
    lock := &redis.StatusCmd{}
    lock, err := redisClient.SetNX(
        ctx,
        fmt.Sprintf("wiki:active:%s", kbID),
        "1",
        5*time.Minute,
    )
    if lock.Val() == "" {
        // Already running, reschedule
        return asynq.SkipRetry  // Task handler will retry
    }
    
    // 3. Start refresh goroutine (extend lock every 2 min)
    go func() {
        ticker := time.NewTicker(2 * time.Minute)
        for range ticker.C {
            redisClient.Expire(ctx, lockKey, 5*time.Minute)
        }
    }()
    
    // 4. Peek pending items (dedup by knowledgeID)
    pendingOps := peekPendingList(redisClient, kbID, 5)
    
    // 5. MAP phase (parallel extraction)
    var wg sync.WaitGroup
    resultChan := make(chan SlugUpdate)
    for _, op := range pendingOps {
        wg.Add(1)
        go func(op WikiPendingOp) {
            defer wg.Done()
            updates := mapOneDocument(ctx, op, wikiService)
            for _, u := range updates {
                resultChan <- u
            }
        }(op)
    }
    wg.Wait()
    close(resultChan)
    
    // 6. Group updates by slug
    slugUpdates := make(map[string][]SlugUpdate)
    for u := range resultChan {
        slugUpdates[u.Slug] = append(slugUpdates[u.Slug], u)
    }
    
    // 7. REDUCE phase (parallel synthesis)
    for slug, updates := range slugUpdates {
        wg.Add(1)
        go func(slug string, updates []SlugUpdate) {
            defer wg.Done()
            reduceSlugUpdates(ctx, kbID, slug, updates, wikiService)
        }(slug, updates)
    }
    wg.Wait()
    
    // 8. Post-processing
    appendLogEntry(ctx, kbID, ...) // Update log page
    rebuildIndexPage(ctx, kbID, ...)  // Regenerate index
    injectCrossLinks(ctx, kbID, changedSlugs)  // Auto-link
    
    // 9. Cleanup & follow-up
    trimPendingList(redisClient, kbID, len(pendingOps))
    
    // 10. If items remain, schedule follow-up
    if remaining := redisClient.LLen(ctx, fmt.Sprintf("wiki:pending:%s", kbID)); remaining > 0 {
        // Reschedule with no delay (already running)
        task, _ := asynq.NewTask("wiki:ingest", payload.Marshal())
        taskClient.Enqueue(task)
    }
    
    // 11. Release lock
    redisClient.Del(ctx, fmt.Sprintf("wiki:active:%s", kbID))
    
    return nil
}
```

### 3.4 Linting and Health Monitoring

**WikiLintService Checks:**

1. **Orphan Pages** (warning severity)
   - Pages with `len(InLinks) == 0`
   - Excluded: index, log pages
   - Not auto-fixable

2. **Broken Links** (error severity)
   - OutLink reference to nonexistent page
   - Auto-fixable: `[[broken]]` → `broken` (plain text)

3. **Empty Content** (warning severity)
   - Page content < 50 characters
   - Auto-fixable: archive the page

4. **Missing Cross-References** (info severity)
   - Entity/concept name mentioned but not linked
   - Detects: `Contains(page.Content, entity.Title)` but `entity not in page.OutLinks`
   - Not auto-fixable (might be intentional)

5. **Empty Content** (warning)
   - < 50 chars of content
   - Auto-fixable: archive

6. **Duplicate Slug** (not currently implemented in code)

**Health Score Calculation:**

```
healthScore = 100

if orphanPct > 50% {
    healthScore -= 25
} else if orphanPct > 25% {
    healthScore -= 10
}

brokenLinkCount = count of LintIssueBrokenLink
healthScore -= brokenLinkCount * 5

if totalLinks == 0 and totalPages > 2 {
    healthScore -= 15
}

emptyPageCount = count of LintIssueEmptyContent
healthScore -= emptyPageCount * 3

if healthScore < 0 { healthScore = 0 }
```

**AutoFix Behavior:**

```go
func (s *WikiLintService) AutoFix(ctx context.Context, kbID string) (int, error) {
    report, _ := s.RunLint(ctx, kbID)
    
    fixed := 0
    for _, issue := range report.Issues {
        if !issue.AutoFixable { continue }
        
        switch issue.Type {
        case LintIssueBrokenLink:
            // Replace [[broken]] with plain text
            page.Content = ReplaceAll(page.Content, 
                "[[" + issue.TargetSlug + "]]", 
                issue.TargetSlug)
            wikiService.UpdatePage(ctx, page)
            fixed++
            
        case LintIssueEmptyContent:
            // Archive instead of delete
            page.Status = WikiPageStatusArchived
            wikiService.UpdatePage(ctx, page)
            fixed++
        }
    }
    
    if fixed > 0 {
        // Rebuild links to reflect changes
        wikiService.RebuildLinks(ctx, kbID)
    }
    
    return fixed, nil
}
```

---

## Part 4: Edge Cases and Gotchas

### 4.1 Concurrent Update Conflicts

**Scenario:** Two agents simultaneously update the same page

```
Agent A: Fetch page version=5
Agent B: Fetch page version=5
Agent A: Update WHERE version=5 ✓ → version becomes 6
Agent B: Update WHERE version=5 ✗ → ErrWikiPageConflict
```

**Caller Responsibility:**
- Must retry with fresh page fetch
- No automatic retry in service layer
- HTTP handlers should return 409 Conflict

**Note:** UpdateMeta bypasses this check (intentional for background operations)

### 4.2 Link Consistency Windows

**Problem:** Between UpdatePage and updateInLinks, there's a window where link consistency is broken

**Impact:** 
- If process crashes after UPDATE but before updateInLinks
- InLinks become stale
- RebuildLinks can fix this

**Mitigation:**
- RebuildLinks operation for emergency recovery
- Log entry tracks when link maintenance runs
- Linter detects orphan pages

### 4.3 Self-Link Prevention

**Code:**
```go
if r.slug == selfSlug { continue }  // Skip self-links
```

**Test Case:**
```go
func TestLinkifyContent_SkipsSelfSlug(t *testing.T) {
    refs := []linkRef{{slug: "beijing", matchText: "北京"}}
    // When rendering on beijing page itself
    got, changed := linkifyContent("北京是首都", refs, "beijing")
    if changed { t.Fatalf("should not self-link") }
}
```

### 4.4 LLM Conflict Detection

**Real Scenario:**
```
Document A: "Hunyuan Model is an LLM from Tencent"
    → Extracted as Entity: "Hunyuan Model", slug="entity/hunyuan-model"

Later, Document B: "Qwen3 is a powerful LLM"
    → Extracted as Entity: "Qwen3", slug="entity/qwen3"

If system tries to merge B into A's page:
    WikiPageModifyPrompt checks: "Does new_info describe same entity as page?"
    → NO (Qwen3 ≠ Hunyuan Model)
    → LLM rejects merge
```

### 4.5 Slug Reuse Across Updates

**Slug Continuity Rule (in WikiKnowledgeExtractPrompt):**

```
If previous slugs are provided:
  - If entity still exists: REUSE exact slug
  - If entity gone: DO NOT include in output
  - If new entity: generate new slug

This ensures slug stability when documents are updated.
```

**Example:**
```
Document v1: "Acme Corp is a 50-person startup"
    → Extracted: entity/acme-corp

Document v1 replaced with v2: "Acme Corp is now a 500-person company"
    → Extracted: entity/acme-corp (SAME slug, different details)
    → Triggers REDUCE for entity/acme-corp
    → Updates page with new details
    → SourceRefs now includes v2 instead of v1
```

### 4.6 CJK Text Handling in Cross-Link Injection

**Word Boundary Rules:**

```go
func hasWordBoundary(s string, pos, end int) bool {
    if pos > 0 {
        r, _ := utf8.DecodeLastRuneInString(s[:pos])
        if isASCIIWordRune(r) { return false }  // No boundary
    }
    if end < len(s) {
        r, _ := utf8.DecodeRuneInString(s[end:])
        if isASCIIWordRune(r) { return false }  // No boundary
    }
    return true
}
```

**Implication:**
- "北京邮电大学" (9 chars) → boundary after 北京 (CJK naturally ends)
- Can match "北京" even in the middle if preceded/followed by non-ASCII
- "北京" inside "北京邮电大学" WILL match if "长北京" is sorted shorter

**Test Case (sorting matters):**
```go
func TestLinkifyContent_LongerNameWinsOverSubstring(t *testing.T) {
    refs := []linkRef{
        {slug: "beijing", matchText: "北京"},
        {slug: "bupt", matchText: "北京邮电大学"},
    }
    got, _ := linkifyContent("我就读于北京邮电大学", refs, "")
    // Should link full university name, not partial
    if !strings.Contains(got, "[[bupt|北京邮电大学]]") {
        t.Fatalf("longer match not preferred")
    }
}
```

### 4.7 Forbidden Span Edge Cases

**Reference Definition Handling:**

```
[capital]: https://example.com/beijing
```

- Scanned in Pass 1 of `computeForbiddenSpans`
- Full line marked as forbidden
- Prevents "beijing" inside URL from being linked

**Inline Code Edge Case:**

```
Running `code with '''multiple''' backticks`
```

- Backtick run counting: matches opening run length exactly
- "Can't" in prose won't close inline code (only 1 backtick)

**Nested Brackets:**

```
[[[[nested]]]] or [[link with [brackets]]]
```

- `findClosingBracket` tracks depth
- Balanced tracking prevents false closes
- First `[[` opens, first `]]` closes if depth==1

### 4.8 Redis Timing Issues

**Lock Acquisition:**

```
Task A: SetNX("wiki:active:kb1", "1", 5m) → SET
Task B: SetNX("wiki:active:kb1", "1", 5m) → NOT SET (already exists)
Task B: Reschedule with exponential backoff
```

**Pending List Persistence:**

```
Pending item expires after 24 hours (wikiPendingTTL)
If ingestion is blocked for > 24h:
  - Items auto-purge from Redis
  - Next task finds empty list
  - Exits cleanly
```

**Lock Refresh Failure:**

```
If refresh goroutine crashes:
  - Lock still has 5-min TTL from last refresh
  - If process crashes too, another task can acquire lock after 5m
  - Prevents permanent deadlock
```

### 4.9 LLM Parsing Failures

**JSON Parsing Error:**

In `extractEntitiesAndConceptsNoUpsert`:
```go
var result struct {
    Entities []EntityJSON `json:"entities"`
    Concepts []ConceptJSON `json:"concepts"`
}
if err := json.Unmarshal(response, &result); err != nil {
    logger.Warnf(ctx, "failed to parse LLM response: %v", err)
    return nil, nil, nil  // Return empty, continue
}
```

- Continues with empty extraction
- Document still marked as processed
- No retry on parse failure

**Image Handling:**

In prompts:
```
<images>
  <image>
    <caption>...</caption>
    <url>https://...</url>
  </image>
</images>
```

- LLM can include images: `![caption](url)`
- Invalid URLs might cause rendering issues in frontend
- Currently no validation of image URLs

---

## Part 5: Operational Considerations

### 5.1 Performance Scaling

**Batch Size Limits:**
- `wikiMaxDocsPerBatch` = 5 (default)
- Prevents single batch from overwhelming resources
- Follow-up tasks handle remaining items

**Parallel Goroutines:**
- MAP phase: 10 concurrent extractions
- REDUCE phase: 10 concurrent syntheses
- Total concurrent LLM calls: 20 maximum per batch

**LLM Cost Implications:**
- Each document extraction = 1 LLM call (50-100 tokens typically)
- Each slug synthesis = 1 LLM call (100-300 tokens typically)
- 5 documents with 10 new entities = ~15-20 LLM calls per batch

### 5.2 Storage Considerations

**Database Schema:**
```sql
CREATE TABLE wiki_pages (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT,
    knowledge_base_id VARCHAR(36),
    slug VARCHAR(255),
    title VARCHAR(512),
    page_type VARCHAR(32),
    status VARCHAR(32),
    content TEXT,  -- Full markdown
    summary TEXT,
    aliases JSON,
    source_refs JSON,
    in_links JSON,
    out_links JSON,
    page_metadata JSON,
    version INT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    
    UNIQUE INDEX idx_kb_slug (knowledge_base_id, slug),
    INDEX idx_tenant,
    INDEX idx_kb,
    INDEX idx_page_type,
    INDEX idx_status,
    INDEX idx_deleted_at
);

CREATE TABLE wiki_page_issues (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT,
    knowledge_base_id VARCHAR(36),
    slug VARCHAR(255),
    issue_type VARCHAR(50),
    description TEXT,
    suspected_knowledge_ids JSON,
    status VARCHAR(20),
    reported_by VARCHAR(100),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    
    INDEX idx_kb_slug_status (knowledge_base_id, slug, status)
);
```

**Typical Page Size:**
- Summary page: 5-20 KB (document summary + structure)
- Entity/Concept page: 2-10 KB (focused on single entity/concept)
- Index page: 10-50 KB (grows with number of pages)
- Log page: 5-50 KB (operation history)

### 5.3 Monitoring and Debugging

**Key Metrics:**
- `wiki:pending:{kbID}` list length (pending documents)
- `wiki:active:{kbID}` presence (processing active?)
- Orphan page count (from GetStats)
- Broken link count (from RunLint)
- Health score (from RunLint)

**Logging Points:**
```
WikiBoost: boosted N wiki page chunks by 1.3x
wiki: injected cross-links in N pages
wiki lint: KB X — health score Y/100, Z issues
wiki: failed to update in_links for slug: error
wiki: create wiki page: error
wiki auto-fix: KB X — fixed N issues
```

**Debug Queries:**

```sql
-- Find pages with broken links
SELECT DISTINCT kbID, slug FROM wiki_pages 
WHERE JSON_LENGTH(out_links) > 0
AND NOT EXISTS (SELECT 1 FROM wiki_pages target 
    WHERE target.slug IN (SELECT * FROM JSON_EXTRACT(out_links, '$[*]')));

-- Find orphan pages
SELECT slug, title FROM wiki_pages 
WHERE knowledge_base_id = ? 
AND page_type NOT IN ('index', 'log')
AND JSON_LENGTH(in_links) = 0
ORDER BY updated_at DESC;

-- Find pages by source knowledge
SELECT * FROM wiki_pages 
WHERE knowledge_base_id = ? 
AND source_refs LIKE '%"knowledge123"%';
```

### 5.4 Common Pitfalls and Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Stale InLinks | Crash between UPDATE and updateInLinks | RunLint, AutoFix broken links |
| Duplicate entity pages | LLM generates new slug for same entity | Check previous_slugs in extraction prompt |
| Double-linked pages | linkifyContent called twice | Idempotence built-in; safe to re-run |
| Orphan pages | New page not added to index, no backlinks | RebuildIndexPage, manual linking |
| Broken links after retract | cleanDeadLinks not called | Re-run ProcessWikiIngest post-processing |
| High LLM costs | Too many batches | Increase wikiIngestDelay or batch size |
| Slow index rebuild | Index page has thousands of entries | Consider pagination or separate category pages |

---

## Part 6: Integration Points

### 6.1 Chat Pipeline Integration (WikiBoost)

**Plugin Flow:**

```
User Query
    ↓
Vector/Keyword Search
    ↓
Initial Retrieval (mix of doc chunks + wiki pages)
    ↓
CHUNK_RERANK Event
    ↓
PluginWikiBoost.OnEvent()
    ├─ Check for wiki_page chunks
    ├─ Verify KB has wiki enabled
    ├─ Multiply scores by 1.3x
    └─ Re-sort results
    ↓
Final Ranked Results (wiki pages boost up)
    ↓
LLM Generation (prefers wiki context)
```

**Score Boost Logic:**

```go
for i := range chatManage.RerankResult {
    if chatManage.RerankResult[i].ChunkType == ChunkTypeWikiPage {
        chatManage.RerankResult[i].Score *= 1.3
        boostedCount++
    }
}
// Re-sort after all boosts applied
sort.SliceStable(chatManage.RerankResult, func(i, j int) bool {
    return chatManage.RerankResult[i].Score > chatManage.RerankResult[j].Score
})
```

### 6.2 Agent Tool Integration

**Available Tools:**
- `wiki_write_page` — Create/update pages
- `wiki_delete_page` — Delete pages
- `wiki_replace_text` — Find/replace within page
- `wiki_read_source_doc` — Read source document for agent

**Typical Agent Flow:**

```
User: "Compare Hunyuan and Qwen models"
    ↓
Agent: Call wiki_read_source_doc (fetch docs mentioning both)
    ↓
Agent: Generate comparison analysis
    ↓
Agent: Call wiki_write_page (create synthesis/comparison page)
    ├─ slug: "synthesis/hunyuan-vs-qwen"
    ├─ page_type: "synthesis"
    ├─ content: Agent-generated comparison
    └─ title: "Hunyuan vs Qwen: Comparison"
    ↓
Service: InjectCrossLinks (auto-link entity/concept mentions)
    ↓
Service: RebuildIndexPage (add to index)
```

### 6.3 Chunk Synchronization

**When page is created/updated:**

```
1. Wiki page saved to database
2. Page serialized as chunk:
   - chunk_id: "wp-{page_id}"
   - chunk_type: "wiki_page"
   - content: page.Content
   - metadata: {slug, page_type, kb_id, ...}
3. Chunk indexed in vector + keyword search
4. Chunk becomes retrievable alongside doc chunks
```

**Index Updates:**

```
CreatePage
    ↓
Service saves to wiki_pages table
    ↓
Service calls chunkService.CreateChunk()
    ├─ Generate embedding
    ├─ Index keyword tokens
    └─ Store in vector database
    ↓
Next search includes this page
```

---

## Part 7: Future Enhancements and Recommendations

### 7.1 Potential Improvements

1. **Async Link Updates**
   - Currently synchronous, could be queued
   - Reduces latency for page creates/updates
   - Risk: temporary inconsistency

2. **Link Validation on Ingest**
   - Check OutLinks point to existing pages
   - Auto-generate fix suggestions
   - Add to linter checks

3. **Incremental Index Rebuild**
   - Current: regenerates entire index page
   - Proposed: delta updates (only add/remove changed sections)
   - Benefit: O(1) instead of O(n) for large wikis

4. **Slug Aliases for Navigation**
   - Allow `[[entity/hunyuan|Hunyuan]]` to resolve via aliases
   - Current: redirects only work if exact slug matches
   - Benefit: more natural linking

5. **Graph Export**
   - Mermaid diagram generation from graph
   - DOT format for Graphviz
   - Useful for documentation

### 7.2 Known Limitations

1. **First-Match-Only Linking**
   - Each slug links exactly once per page
   - Later mentions aren't linked
   - Trade-off: prevents visual clutter

2. **LLM Dependency**
   - Extraction quality depends on LLM version
   - Bad extractions not auto-detected
   - Mitigation: RunLint catches orphans/breaks

3. **Content-Based Matching**
   - Substring matching in cross-links
   - Case-sensitive (normalized to lowercase)
   - No semantic understanding of mentions

4. **Redis Dependency**
   - Debouncing/batching requires Redis
   - Pending operations lost on Redis crash
   - Mitigation: documents can be re-uploaded

---

## Conclusion

The WeKnora wiki system demonstrates sophisticated architectural patterns for knowledge extraction and synthesis:

- **Resilient async processing** with debouncing and batching
- **Parallel MAP-REDUCE** for scalable LLM integration
- **Robust consistency** via optimistic locking and bidirectional link maintenance
- **Sophisticated text processing** for cross-link injection with pattern matching
- **Comprehensive health monitoring** via linting and auto-fix

The system is production-ready with careful attention to edge cases, concurrency safety, and operational observability. Future enhancements can focus on incremental operations and graph visualization features.

