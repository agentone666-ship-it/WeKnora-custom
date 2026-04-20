# WeKnora Wiki System - Complete Technical Deep Dive

**Last Updated:** 2026-04-20

This document provides an exhaustive technical analysis of the WeKnora Wiki system, including data models, service architecture, async pipelines, API endpoints, and integration points.

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Core Data Models](#core-data-models)
3. [Service Architecture](#service-architecture)
4. [Async Wiki Generation Pipeline](#async-wiki-generation-pipeline)
5. [Cross-Link Injection System](#cross-link-injection-system)
6. [Wiki Chunking & Retrieval](#wiki-chunking--retrieval)
7. [Agent Integration](#agent-integration)
8. [API Endpoints](#api-endpoints)
9. [Configuration & Settings](#configuration--settings)
10. [Error Handling & Observability](#error-handling--observability)

---

## System Overview

### What is Wiki?

Wiki is **NOT** a knowledge base type. Instead, it is a **feature flag** (`WikiConfig.Enabled`) that can be enabled on document-type knowledge bases. When enabled, the system:

1. **Automatically generates LLM-synthesized wiki pages** from uploaded documents
2. **Maintains bidirectional link references** between pages
3. **Provides graph visualization** of page relationships
4. **Boosts wiki page retrieval** in search results
5. **Tracks wiki health** via linting and statistics

### Key Architectural Decisions

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| **Page Types** | 7 types (summary, entity, concept, index, log, synthesis, comparison) | Different semantic purposes for different page categories |
| **Link Representation** | Bidirectional (InLinks + OutLinks) | Enables graph traversal and orphan detection |
| **Async Processing** | Asynq + Redis with debouncing | Batches rapid uploads, prevents thundering herd |
| **Indexing** | Dual-path (chunks + wiki pages) | Preserves raw docs while adding synthesis |
| **Cross-Linking** | Pure text-based pattern matching | No LLM cost, deterministic, handles code blocks |
| **Chunk Syncing** | WikiPage → Chunk (ChunkType=wiki_page) | Reuses existing retrieval pipeline |
| **Version Control** | Optimistic locking per page | Allows concurrent edits with conflict detection |

---

## Core Data Models

### WikiPage Entity

**Location:** `internal/types/wiki_page.go` (lines 43-84)

```go
type WikiPage struct {
    ID              string              // UUID
    TenantID        uint64              // Multi-tenant isolation
    KnowledgeBaseID string              // Parent KB
    Slug            string              // URL-friendly identifier (unique per KB)
    Title           string              // Human-readable title
    PageType        string              // "summary" | "entity" | "concept" | "index" | "log" | "synthesis" | "comparison"
    Status          string              // "draft" | "published" | "archived"
    Content         string              // Full markdown content
    Summary         string              // One-line summary for index listing
    Aliases         StringArray         // Alternative names (JSON array)
    SourceRefs      StringArray         // Knowledge IDs that contributed (JSON, format: "id|title")
    InLinks         StringArray         // Backlinks (pages linking to this)
    OutLinks        StringArray         // Outbound links (pages this links to)
    PageMetadata    JSON                // Arbitrary key-value metadata
    Version         int                 // For optimistic locking
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       gorm.DeletedAt      // Soft delete
}
```

**Database:** Table `wiki_pages`
- Primary Key: `id` (UUID)
- Unique Index: `(knowledge_base_id, slug)`
- Foreign Index: `knowledge_base_id`, `tenant_id`

### WikiPageIssue Entity

**Location:** `internal/types/wiki_page.go` (lines 176-196)

```go
type WikiPageIssue struct {
    ID                    string
    TenantID              uint64
    KnowledgeBaseID       string
    Slug                  string
    IssueType             string      // "orphan_page" | "broken_link" | "stale_ref" | etc.
    Description           string
    SuspectedKnowledgeIDs StringArray // Knowledge IDs related to the issue
    Status                string      // "pending" | "resolved" | "ignored"
    ReportedBy            string      // "linter" | "agent" | "user"
    CreatedAt             time.Time
    UpdatedAt             time.Time
    DeletedAt             gorm.DeletedAt
}
```

### WikiConfig Structure

**Location:** `internal/types/knowledgebase.go` (lines 94-103)

```go
type WikiConfig struct {
    Enabled              bool   // Feature flag
    AutoIngest           bool   // Automatically generate wiki from documents
    SynthesisModelID     string // LLM model for generation (defaults to KB.SummaryModelID)
    MaxPagesPerIngest    int    // Limit pages per batch (0 = unlimited)
}
```

Embedded in `KnowledgeBase`:
```go
type KnowledgeBase struct {
    // ... other fields ...
    WikiConfig *WikiConfig // pointer, only set if wiki is enabled
    // ...
}
```

---

## Service Architecture

### Service Dependency Graph

```
WikiPageService (interface)
├── repo: WikiPageRepository (database layer)
├── chunkRepo: ChunkRepository (for chunk deletion on page delete)
└── kbService: KnowledgeBaseService

WikiIngestService (interface: TaskHandler)
├── wikiService: WikiPageService
├── kbService: KnowledgeBaseService
├── knowledgeSvc: KnowledgeService (for getting document content)
├── chunkRepo: ChunkRepository (for reading document chunks)
├── modelService: ModelService (for LLM chat models)
├── task: TaskEnqueuer (Asynq task queue)
└── redisClient: *redis.Client (for pending list & distributed lock)
```

### WikiPageService Interface

**Location:** `internal/types/interfaces/wiki_page.go` (lines 12-77)

**Implemented by:** `internal/application/service/wiki_page.go`

**Key Methods:**

| Method | Purpose |
|--------|---------|
| `CreatePage(ctx, page)` | Create wiki page, parse links, sync to chunks |
| `UpdatePage(ctx, page)` | Update with link re-parsing and version bump |
| `UpdatePageMeta(ctx, page)` | Update metadata-only (no version bump) |
| `GetPageBySlug(ctx, kbID, slug)` | Retrieve single page |
| `ListPages(ctx, req)` | Paginated list with filtering |
| `DeletePage(ctx, kbID, slug)` | Soft-delete and clean links |
| `GetIndex(ctx, kbID)` | Get/create index page |
| `GetLog(ctx, kbID)` | Get/create log page |
| `GetGraph(ctx, kbID)` | Build link graph for visualization |
| `GetStats(ctx, kbID)` | Aggregate statistics |
| `RebuildLinks(ctx, kbID)` | Full rebuild of all link references |
| `InjectCrossLinks(ctx, kbID, slugs)` | Auto-link mentions in specified pages |
| `RebuildIndexPage(ctx, kbID)` | Regenerate index page directory |
| `SearchPages(ctx, kbID, query, limit)` | Full-text search |
| `CreateIssue(ctx, issue)` | Flag an issue |
| `ListIssues(ctx, kbID, slug, status)` | Retrieve issues |
| `UpdateIssueStatus(ctx, issueID, status)` | Mark resolved/ignored |

---

## Async Wiki Generation Pipeline

### Architecture: MAP-REDUCE Pattern with Debouncing

The wiki ingest pipeline uses a **debounced batch processing** model:

```
Document Upload Event
    ↓
EnqueueWikiIngest() called
    ├─ RPush knowledge_id to Redis list: wiki:pending:{kbID}
    └─ Schedule asynq task with 30s delay
    
Multiple rapid uploads (within 30s window)
    ├─ Each RPush adds to same list
    └─ Each schedules a new task
    
First task fires at t=30s
    ├─ Acquires redis lock: wiki:active:{kbID} (TTL: 5 min)
    ├─ Drains pending list: [doc1, doc2, doc3, ...]
    ├─ Processes all in MAP-REDUCE
    └─ Trims processed docs from list
    
Remaining tasks fire at t=35s, t=40s, ...
    ├─ Find empty pending list
    └─ Return as no-op
```

### ProcessWikiIngest() - Main Handler

**Location:** `internal/application/service/wiki_ingest_batch.go` (lines 46-362)

**Flow:**

1. **Initialization (lines 46-167)**
   - Parse payload
   - Inject tenant ID and language into context
   - Acquire non-blocking Redis lock (`SetNX` with 5-min TTL)
   - Spawn background task to refresh lock every 2 minutes
   - Validate KB and fetch synthesis model

2. **Queue Peek (lines 170-186)**
   - `peekPendingList()`: Fetch up to `wikiMaxDocsPerBatch` (5) operations without removing
   - Deduplicate by knowledge_id, keeping only the LAST operation per doc
   - Supports both Redis mode and Lite mode (fallback for testing)

3. **MAP Phase (lines 212-273)**
   - Parallel processing: up to 10 concurrent goroutines
   - For each pending operation:
     - **Ingest:** Call `mapOneDocument()` to extract entities/concepts
     - **Retract:** Mark page slugs for removal/cleanup
   - Result: `slugUpdates` map (slug → list of updates to apply)
   - Accumulate `docPreview` for logging

4. **REDUCE Phase (lines 275-305)**
   - Parallel processing: up to 10 concurrent goroutines
   - For each affected slug:
     - Call `reduceSlugUpdates()` to apply all accumulated changes
     - Perform LLM deduplication, content synthesis, and save
   - Track pages affected by ingests vs retracts separately

5. **Post-Processing (lines 308-356)**
   - **Log Entries:** Append to log page (chronological trace)
   - **Index Rebuild:** Update index page with change description
   - **Dead Link Cleanup:** Remove references to deleted/archived pages
   - **Cross-Link Injection:** Auto-link mentions of existing pages
   - **Draft Publishing:** Transition draft pages to published
   - **List Trimming:** Remove processed items from Redis pending list

6. **Follow-Up Scheduling (line 360)**
   - If more items remain in pending list: schedule follow-up task
   - Short delay (5s) since lock already held

### mapOneDocument() - Extract Entities & Concepts

**Location:** `internal/application/service/wiki_ingest_batch.go` (lines 364-532)

**Process:**

```
Input: WikiPendingOp {Op: "ingest", KnowledgeID: "doc-uuid", ...}

1. Fetch chunks for document via ChunkRepository
2. Reconstruct full content from chunks
3. Truncate to 32KB if needed (maxContentForWiki)
4. Extract document title from metadata or first chunk
5. Call LLM: "WikiKnowledgeExtractPrompt"
   ├─ Input: document title, content, previous slugs
   └─ Output: JSON {entities: [...], concepts: [...]}
6. Deduplicate extracted items against existing wiki pages
7. Generate summary page via "WikiSummaryPrompt"
8. Compile SlugUpdate[] for all affected pages
   ├─ Summary page update
   ├─ Entity pages
   ├─ Concept pages
   └─ Stale page retracts (pages from old ingest that aren't in new extraction)
9. Return docIngestResult {title, summary, pages affected}
```

**LLM Prompts Used:**
- `agent.WikiKnowledgeExtractPrompt`: Extract entities and concepts
- `agent.WikiDeduplicationPrompt`: Merge/deduplicate extracted items
- `agent.WikiSummaryPrompt`: Generate document summary page

### reduceSlugUpdates() - Apply Changes to Page

**Location:** `internal/application/service/wiki_ingest_batch.go` (lines 589-796)

**Logic:**

```
Input: slug, updates[]SlugUpdate (summary, entities, concepts, retracts)

For Summary Page:
├─ Create or update
├─ Set content from LLM-generated summary
└─ Add source ref

For Entity/Concept Pages:
├─ Load existing page (or create new draft)
├─ Separate updates into: additions, retracts
├─ Call LLM: "WikiPageModifyPrompt"
│  ├─ Inputs:
│  │  ├─ HasAdditions: "1" or ""
│  │  ├─ HasRetractions: "1" or ""
│  │  ├─ ExistingContent: current page content
│  │  ├─ NewContent: new entities/concepts to add
│  │  ├─ DeletedContent: content from retracted documents
│  │  ├─ RemainingSourcesContent: other docs still referencing this page
│  │  └─ AvailableSlugs: related pages (for cross-reference suggestions)
│  └─ Output: Updated markdown content
├─ Update title, content, aliases, source_refs
├─ Save via wikiService.UpdatePage() or CreatePage()
└─ Return (changed, affectedType, err)
```

**Key Features:**
- **Multi-source pages:** Can reference multiple documents
- **Incremental updates:** Previous content preserved and merged
- **Stale handling:** Retracting removes document from page without deleting page
- **Content synthesis:** LLM rewrites to incorporate changes while preserving key info

### Redis Keys Used

| Key Pattern | Purpose | Type | TTL |
|-------------|---------|------|-----|
| `wiki:pending:{kbID}` | List of pending operations | Redis List | 24 hours |
| `wiki:active:{kbID}` | Lock for concurrent batch prevention | String | 5 minutes (refreshed every 2 min) |

### Constants

```go
wikiIngestDelay       = 30 * time.Second   // Debounce window
wikiPendingTTL        = 24 * time.Hour     // Stale cleanup
wikiMaxDocsPerBatch   = 5                  // Per-task limit
maxContentForWiki     = 32768              // Rune limit for LLM input
```

---

## Cross-Link Injection System

### Purpose

Auto-detect mentions of existing wiki page titles in content and wrap them with `[[wiki-links]]` to establish connections. This is done via pure text processing (no LLM call) with sophisticated pattern matching.

### Implementation: linkifyContent()

**Location:** `internal/application/service/wiki_linkify.go` (lines 35-82)

**Algorithm:**

1. **Sort refs by matchText length (descending)**
   - Prevents shorter substrings from shadowing longer terms
   - Example: "Python" before "Python Software Foundation"

2. **Build forbidden spans**
   - Regions that must NOT be touched:
     - Fenced code blocks (```/~~~)
     - Inline code (backtick-delimited)
     - Existing wiki links [[slug|text]]
     - Markdown links [text](url)
     - Images ![alt](url)
     - Reference-style links [text][ref]
     - Autolinks <url>

3. **For each ref:**
   - Skip if already linked elsewhere (one link per slug per page)
   - Find first safe match outside forbidden spans
   - Check word boundaries (for ASCII-letter matches)
   - Wrap with `[[slug|matchText]]`
   - Update forbidden spans to include new link
   - Mark slug as used

4. **Return:** Modified content + changed flag

### Forbidden Span Detection: computeForbiddenSpans()

**Location:** `internal/application/service/wiki_linkify.go` (lines 202-296)

**Covers:**

- **Fenced code:** Lines starting with ``` or ~~~ (3+ chars)
  - Implementation: Line-based scanning, end via matching fence
  
- **Inline code:** Backtick runs
  - Rules: Matching length, stops at double newline or closing backtick
  
- **Reference definitions:** `[label]: url ...`
  - Pattern: Optional indent (0-3 spaces), `[label]:` at line start
  
- **Wiki links:** `[[slug|text]]` or `[[slug]]`
  - Extraction: Parse slug portion, mark as used
  
- **Markdown links:** `[text](url)`
  - Balanced bracket/paren matching with escape handling
  
- **Reference links:** `[text][label]`
  - Two-part pattern matching
  
- **Images:** `![alt](url)`
  - Same logic as markdown links
  
- **Autolinks:** `<scheme://url>` or `<mailto:addr>`

### Word Boundary Checking: hasWordBoundary()

**Location:** `internal/application/service/wiki_linkify.go` (lines 140-154)

**Purpose:** For ASCII-letter matches, ensure match is not mid-word

**Rules:**
- ASCII word characters: `[a-zA-Z0-9_]`
- Non-ASCII (CJK, punctuation): Always treated as boundary
- Example: 
  - "Python" in "Python 3" ✓ (space is boundary)
  - "Python" in "Python3" ✗ (digit is word char)
  - "北京" in "北京邮电" ✓ (multi-char CJK handled by length ordering)

---

## Wiki Chunking & Retrieval

### Wiki Page → Chunk Sync

**Location:** `internal/application/service/wiki_page.go` (lines 431-436)

Wiki pages are synced as chunks to enable retrieval through the normal search pipeline.

```go
// When a wiki page is deleted:
func (s *wikiPageService) deleteChunkForPage(ctx context.Context, page *types.WikiPage) {
    chunkID := "wp-" + page.ID
    if err := s.chunkRepo.DeleteChunk(ctx, page.TenantID, chunkID); err != nil {
        logger.Warnf(ctx, "wiki: failed to delete chunk for page %s: %v", page.Slug, err)
    }
}
```

**Chunk Structure:**

When a wiki page is converted to a chunk:
- **ChunkID:** `"wp-" + page.ID` (prefixed for differentiation)
- **ChunkType:** `"wiki_page"` (defined in `internal/types/chunk.go`)
- **Content:** `page.Content` (full markdown)
- **Title:** `page.Title`
- **Metadata:** Includes `page.Slug`, `page.PageType`, `page.SourceRefs`
- **Embedding:** Created via standard embedding pipeline

**Note:** Actual creation/update of wiki chunks happens during the ingest pipeline via the standard chunk indexing service, not shown explicitly in wiki_page.go. The `CreatePage()` and `UpdatePage()` methods likely trigger chunk sync via hooks or post-update callbacks (to be confirmed in chunk.go or repository layer).

### Retrieval Boosting: WikiBoost Plugin

**Location:** `internal/application/service/chat_pipeline/wiki_boost.go`

**Activation:** Runs in `CHUNK_RERANK` phase (after initial retrieval and reranking)

**Algorithm:**

```go
const wikiBoostFactor = 1.3

1. Check if any chunks have ChunkType == "wiki_page"
   ├─ Fast path: Return early if none found
   └─ Reason: Avoid KB service call on every turn

2. Verify at least one search target is a wiki KB
   ├─ Confirm KB.IsWikiEnabled()
   └─ Skip if none are wiki KBs

3. For each wiki_page chunk in results:
   ├─ Multiply score by 1.3x
   └─ Boost indicates better quality/relevance

4. Re-sort results by score (stable sort)
   └─ Wiki pages move up in ranking
```

**Rationale:** Wiki pages contain LLM-synthesized, cross-referenced knowledge that is typically more coherent than raw document chunks, so they deserve higher retrieval score.

---

## Agent Integration

### Wiki Read & Write Tools

**Location:** `internal/agent/tools/wiki_*.go`

#### wiki_read_page.go

Retrieves a wiki page for agent inspection:

```
Input: slug (e.g., "entity/openai")
Output: {title, content, type, summary, aliases, in_links, out_links}
Use Case: Agent wants to review/understand a page before editing
```

#### wiki_write_page.go

Creates or overwrites a wiki page:

**Location:** Lines 23-167

```
Input: {
    slug,           // URL-friendly identifier
    title,          // Human-readable title
    summary,        // One-line summary
    content,        // FULL markdown (no placeholders)
    page_type,      // "synthesis", "comparison", "entity", etc.
    aliases,        // Alternative names (optional)
    source_refs     // Knowledge IDs that contributed (optional)
}

Flow:
1. Validate KB has wiki enabled
2. Check if page exists (create vs update decision)
3. Resolve source_refs: UUIDs → full format "id|title"
4. Call wikiService.CreatePage() or UpdatePage()
   ├─ Parses outbound links from content
   └─ Updates bidirectional references
5. Inject cross-links (auto-link mentions)
6. Rebuild index page
7. Return: action, slug, title, page_type, summary
```

#### wiki_read_source_doc.go

Retrieve a source document that contributed to wiki pages:

```
Input: knowledge_id (UUID of source document)
Output: {title, content, summary}
Use Case: Agent wants to verify facts against original source
```

#### wiki_delete_page.go

Soft-delete a wiki page:

```
Input: slug
Flow:
1. Validate ownership
2. Call wikiService.DeletePage()
   ├─ Remove inbound link references
   ├─ Delete page record (soft delete)
   └─ Remove associated chunks
3. Rebuild index page
4. Rebuild links (if needed)
```

#### wiki_update_issue.go / wiki_read_issue.go / wiki_flag_issue.go

Issue management for flagging/tracking wiki health problems.

#### wiki_rename_page.go

Rename a page slug and update all references:

```
Input: old_slug, new_slug
Flow:
1. Get existing page
2. Update slug
3. Update outbound references in other pages
4. Create redirect (if needed)
```

---

## API Endpoints

**Base Path:** `/api/v1/knowledgebase/{kb_id}/wiki`

**Registered in:** `internal/router/router.go` line 839

### Core Endpoints

| Method | Path | Handler | Purpose |
|--------|------|---------|---------|
| `GET` | `/pages` | ListPages | Paginated list with filtering |
| `POST` | `/pages` | CreatePage | Create new wiki page |
| `GET` | `/pages/{slug}` | GetPage | Retrieve single page |
| `PUT` | `/pages/{slug}` | UpdatePage | Update page content |
| `DELETE` | `/pages/{slug}` | DeletePage | Soft-delete page |
| `GET` | `/index` | GetIndex | Get index page (auto-create if missing) |
| `GET` | `/log` | GetLog | Get operation log (auto-create if missing) |
| `GET` | `/graph` | GetGraph | Retrieve link graph for visualization |
| `GET` | `/stats` | GetStats | Get aggregate statistics |
| `POST` | `/rebuild-links` | RebuildLinks | Force rebuild of all link references |
| `POST` | `/inject-cross-links` | InjectCrossLinks | Auto-link mentions in specified pages |
| `POST` | `/rebuild-index` | RebuildIndexPage | Regenerate index page |
| `GET` | `/search` | SearchPages | Full-text search |
| `GET` | `/lint` | RunLint | Health check and issue detection |
| `POST` | `/issues` | CreateIssue | Flag an issue on a page |
| `GET` | `/issues` | ListIssues | Retrieve issues (with optional filtering) |
| `PATCH` | `/issues/{id}` | UpdateIssueStatus | Mark issue resolved/ignored |

**Handler Location:** `internal/handler/wiki_page.go`

### Request/Response Examples

**Create Page:**
```json
POST /api/v1/knowledgebase/kb-123/wiki/pages
{
  "slug": "entity/acme-corp",
  "title": "ACME Corporation",
  "summary": "A fictional corporation in roadrunner cartoons",
  "content": "# ACME Corporation\n\nAcme Corporation is a fictional...",
  "page_type": "entity",
  "aliases": ["Acme", "ACME Inc"],
  "source_refs": ["doc-uuid-1", "doc-uuid-2"]
}

Response:
{
  "id": "page-uuid",
  "slug": "entity/acme-corp",
  "title": "ACME Corporation",
  "status": "published",
  "in_links": [],
  "out_links": [],
  "created_at": "2026-04-20T10:30:00Z",
  "updated_at": "2026-04-20T10:30:00Z"
}
```

**List Pages:**
```json
GET /api/v1/knowledgebase/kb-123/wiki/pages?page=1&page_size=20&page_type=entity&status=published

Response:
{
  "pages": [...],
  "total": 150,
  "page": 1,
  "page_size": 20,
  "total_pages": 8
}
```

**Get Graph:**
```json
GET /api/v1/knowledgebase/kb-123/wiki/graph

Response:
{
  "nodes": [
    {"slug": "entity/acme-corp", "title": "ACME Corp", "page_type": "entity", "link_count": 5},
    ...
  ],
  "edges": [
    {"source": "entity/acme-corp", "target": "concept/destruction"},
    ...
  ]
}
```

**Get Stats:**
```json
GET /api/v1/knowledgebase/kb-123/wiki/stats

Response:
{
  "total_pages": 150,
  "pages_by_type": {
    "summary": 20,
    "entity": 60,
    "concept": 70
  },
  "total_links": 300,
  "orphan_count": 5,
  "recent_updates": [...],
  "pending_tasks": 3,
  "pending_issues": 2,
  "is_active": false
}
```

---

## Configuration & Settings

### KB-Level WikiConfig

**Stored in:** `knowledge_bases.wiki_config` (JSON column)

**Frontend Form:** `frontend/src/api/knowledge-base/index.ts`

**Fields:**

```typescript
interface WikiConfig {
  enabled: boolean;                    // Enable/disable wiki
  auto_ingest: boolean;                // Auto-generate from documents
  synthesis_model_id: string;          // LLM model for generation
  max_pages_per_ingest: number;        // Batch size limit (0 = unlimited)
}
```

**API Update:**
```
PUT /api/v1/knowledgebase/{id}
{
  "wiki_config": {
    "enabled": true,
    "auto_ingest": true,
    "synthesis_model_id": "gpt-4-turbo",
    "max_pages_per_ingest": 0
  }
}
```

### Agent Configuration

**Related Fields in AgentConfig:**

```go
type AgentConfig struct {
    KnowledgeBases    []string // KB IDs accessible to agent
    KnowledgeIDs      []string // Individual document IDs
    RetrieveKBOnlyWhenMentioned bool // Only search when @ mentioned
    RetainRetrievalHistory      bool // Keep wiki_read_page results across turns
}
```

Agents access wiki pages through:
1. Knowledge search (if KB in `KnowledgeBases` list)
2. Direct wiki_read_page tool (if tool enabled)
3. Synthesis/comparison creation via wiki_write_page tool

---

## Error Handling & Observability

### Logging

**Log Levels Used:**

- `Infof()`: Normal operations (page created, docs ingested, etc.)
- `Warnf()`: Recoverable issues (failed link update, missing chunks, etc.)
- `Errorf()`: Failures in critical paths (LLM failures, database errors, etc.)

**Key Log Events:**

```
"wiki ingest: batch processing %d ops for KB %s"
"wiki ingest: processing document '%s' (%s)"
"wiki ingest: failed to map knowledge %s: %v"
"wiki ingest: mapped knowledge %s title=%q generated_updates=%d"
"wiki ingest: batch completed for KB %s, %d ops, %d pages affected"
"wiki ingest stats: kb=%s ... pending_ops=%d ops(ingest=%d,retract=%d) ingest(success=%d,failed=%d)"
```

### Asynq Task Telemetry

**Deferred logging in ProcessWikiIngest() (lines 67-91):**

Captures comprehensive stats in a single log line on task completion:

```go
defer func() {
    logger.Infof(ctx,
        "wiki ingest stats: kb=%s tenant=%d retry=%d/%d status=%s elapsed=%s mode=%s "+
        "lock_acquired=%v pending_ops=%d ops(ingest=%d,retract=%d) ingest(success=%d,failed=%d) "+
        "retract_handled=%d pages(total=%d) index(rebuild_attempted=%v,rebuild_succeeded=%v) "+
        "followup=%v preview=%s",
        payload.KnowledgeBaseID, payload.TenantID, retryCount, maxRetry, exitStatus,
        time.Since(taskStartedAt), mode, lockAcquired, pendingOpsCount, ingestOps, retractOps,
        ingestSucceeded, ingestFailed, retractHandled, totalPagesAffected,
        indexRebuildAttempted, indexRebuildSucceeded, followUpScheduled, previewStringSlice(docPreview, 6),
    )
}()
```

**Status Values:**
- `"success"`: Normal completion
- `"invalid_payload"`: Malformed task data
- `"active_lock_conflict"`: Another batch in progress (retry)
- `"kb_not_wiki_enabled"`: KB doesn't have wiki enabled
- `"auto_ingest_disabled"`: AutoIngest flag is off
- `"missing_synthesis_model"`: No LLM model configured
- `"no_pending_ops"`: Empty pending list

### Error Handling Patterns

**Silent Failures (log but continue):**
- Failed link updates: Continue with other links
- Failed chunk deletion: Log warning but don't fail page delete
- Failed cross-link injection: Log warning but page is still complete

**Batch Partial Failures:**
- If one document fails in MAP phase: Continue with others (don't fail whole batch)
- Mark individual failures in stats but complete remaining work

**Retry Logic:**
- Asynq MaxRetry: 10 (high to outlast 5-minute active lock TTL)
- Timeout: 60 minutes per task
- Queue: "low" priority (non-critical background work)

---

## Wiki Linting System

**Location:** `internal/application/service/wiki_lint.go`

### Issue Types Detected

| Type | Severity | AutoFixable | Description |
|------|----------|-------------|-------------|
| `orphan_page` | Warning | No | Page has no inbound links |
| `broken_link` | Error | Yes | Outbound link points to deleted page |
| `stale_ref` | Warning | No | Page references deleted document |
| `missing_cross_ref` | Info | No | Page mentions entity but doesn't link |
| `empty_content` | Warning | Yes | Page content < 50 characters |
| `duplicate_slug` | Error | No | Slug collision (shouldn't occur) |

### Health Score Calculation

```
Base: 100
Deductions:
- Each orphan: -0.5 points (if orphan% > 50: -25)
- Each broken link: -2 points
- Each stale ref: -1 point
```

---

## Performance Considerations

### Optimization Techniques

1. **Debouncing:** 30-second window prevents immediate task scheduling
2. **Batching:** Up to 5 docs per ingest task (wikiMaxDocsPerBatch)
3. **Parallel Processing:**
   - MAP phase: 10 concurrent goroutines
   - REDUCE phase: 10 concurrent goroutines
4. **Deduplication:** Single LLM call for entities + concepts (not separate)
5. **Pre-loading:** All pages fetched once at batch start (WikiBatchContext)
6. **Fast Path:** Wiki boost skips work if no wiki_page chunks exist
7. **Content Truncation:** Wiki generation limited to 32KB per document

### Scaling Considerations

- **Redis pending list:** 24-hour TTL prevents unbounded growth
- **Active lock:** 5-minute TTL with 2-minute refresh ensures cleanup
- **Task timeout:** 60 minutes prevents hanging tasks
- **Concurrent batches:** Lock prevents parallel processing for same KB (sequential per KB)

---

## Common Workflows

### Uploading a Document with Wiki Enabled

```
1. User uploads document to knowledge base
2. Document ingestion creates chunks via ChunkService
3. If WikiConfig.AutoIngest = true:
   ├─ EnqueueWikiIngest() called
   ├─ Knowledge ID added to redis pending list
   └─ Asynq task scheduled (30s delay)
4. After 30s (or when batch fills):
   ├─ ProcessWikiIngest() executes
   ├─ MAP phase: Extract entities/concepts from document
   ├─ REDUCE phase: Merge into wiki pages, call LLM for synthesis
   ├─ Rebuild index page
   ├─ Inject cross-links
   └─ Publish draft pages
5. Wiki pages now appear in search results (with boost)
6. User can browse via /wiki/pages, /wiki/graph, /wiki/stats
```

### Agent Creating a Synthesis Page

```
1. Agent calls wiki_write_page tool
   ├─ slug: "synthesis/multi-document-analysis"
   ├─ title: "Comparison of Approaches"
   ├─ content: Full markdown with [[wiki-links]]
   └─ page_type: "synthesis"
2. Tool calls wikiService.CreatePage()
   ├─ Parses outbound links from content
   ├─ Creates bidirectional references
   └─ Page created as "published" (not draft)
3. Tool calls InjectCrossLinks()
   └─ Auto-links mentions in other pages
4. Tool calls RebuildIndexPage()
   └─ Adds synthesis page to index
5. Result returned to agent
```

### Viewing Wiki Visualization

```
1. GET /api/v1/knowledgebase/{id}/wiki/graph
2. Response includes nodes (pages) and edges (links)
3. Frontend renders D3/Force graph
4. Nodes sized by link count
5. Edges directed (arrow indicates "links to")
6. User can click nodes to view page content
```

---

## Future Enhancements (Design Patterns)

### Potential Improvements

1. **Incremental indexing:** Only re-index changed pages instead of full batch
2. **Distributed locking:** Use Redlock for multi-node safety (if scaling horizontally)
3. **Caching:** Cache graph data with TTL to avoid rebuilding on every request
4. **Async search indexing:** Defer full-text index updates to background job
5. **Wiki versioning:** Maintain page version history with diffs
6. **Conflict resolution:** Smarter handling of concurrent edits
7. **Custom LLM prompts:** Allow per-KB customization of generation prompts
8. **Link scoring:** Weight links by relevance instead of binary presence
9. **Smart archiving:** Auto-archive orphaned pages after N days
10. **Wiki search autocomplete:** Suggest page slugs/titles during creation

---

## Appendix: Key Files Quick Reference

| File | Lines | Purpose |
|------|-------|---------|
| `types/wiki_page.go` | 43-84 | WikiPage entity definition |
| `types/wiki_page.go` | 94-103 | WikiConfig structure |
| `service/wiki_page.go` | 45-78 | CreatePage method |
| `service/wiki_page.go` | 81-111 | UpdatePage method |
| `service/wiki_ingest.go` | 118-177 | EnqueueWikiIngest function |
| `service/wiki_ingest_batch.go` | 46-362 | ProcessWikiIngest handler |
| `service/wiki_ingest_batch.go` | 364-532 | mapOneDocument extraction |
| `service/wiki_ingest_batch.go` | 589-796 | reduceSlugUpdates synthesis |
| `service/wiki_linkify.go` | 35-82 | linkifyContent algorithm |
| `service/wiki_lint.go` | 74-250+ | RunLint health check |
| `service/chat_pipeline/wiki_boost.go` | 1-98 | Retrieval boosting |
| `agent/tools/wiki_write_page.go` | 23-167 | Create/update pages |
| `handler/wiki_page.go` | — | HTTP endpoints |
| `router/router.go` | 839 | Route registration |

---

**End of Document**

Generated: 2026-04-20 | Based on codebase commit: latest
