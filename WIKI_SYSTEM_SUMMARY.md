# WeKnora Wiki System - Executive Summary

**Document:** WIKI_SYSTEM_SUMMARY.md  
**Date:** 2026-04-20  
**Status:** Complete Technical Analysis

---

## What is the Wiki System?

WeKnora's Wiki is a **document synthesis and knowledge graph** feature for document-type knowledge bases. It:

- **Automatically extracts entities and concepts** from uploaded documents using LLM
- **Generates LLM-synthesized wiki pages** that aggregate information across documents
- **Maintains bidirectional link graphs** showing relationships between concepts
- **Provides visualization and statistics** for knowledge exploration
- **Boosts retrieval** of wiki pages in search results
- **Tracks health** with linting and issue detection

**Critical:** Wiki is **NOT a KB type** — it's a feature flag (`WikiConfig.Enabled`) on document-type KBs.

---

## Quick Architecture Map

```
Document Upload (KB with wiki enabled)
         ↓
   ChunkService (creates chunks)
         ↓
   EnqueueWikiIngest() → Redis pending list + Asynq task (30s delay)
         ↓
   ProcessWikiIngest (batch handler) ─────┬─ MAP Phase (extract entities/concepts)
         ├─ 10 parallel goroutines        │
         ├─ LLM calls for each document   │
         └─ Accumulate updates            │
                                          ├─ REDUCE Phase (merge & synthesize)
                                          ├─ 10 parallel goroutines
                                          ├─ LLM calls for page synthesis
                                          └─ Save to DB
         ↓
   Post-Processing
   ├─ Append log entries
   ├─ Rebuild index page (LLM-updated intro)
   ├─ Clean dead links
   ├─ Inject cross-links (pure text matching)
   ├─ Publish draft pages
   └─ Schedule follow-up if more pending
         ↓
   Wiki pages now indexed as chunks (ChunkType="wiki_page")
         ↓
   Search retrieval → WikiBoost plugin (1.3x score multiplier)
         ↓
   Agents can read/write wiki pages via tools
```

---

## Core Data Structures

### WikiPage (Database Entity)

```go
type WikiPage struct {
    ID              string          // UUID
    KnowledgeBaseID string          // Parent KB
    Slug            string          // URL-friendly: "entity/acme-corp"
    Title           string          // Human title
    PageType        string          // summary|entity|concept|index|log|synthesis|comparison
    Status          string          // draft|published|archived
    Content         string          // Full markdown
    Summary         string          // One-line for index
    Aliases         []string        // Alternative names
    SourceRefs      []string        // Knowledge IDs: "id|title"
    InLinks         []string        // Pages linking to this
    OutLinks        []string        // Pages this links to
    Version         int             // Optimistic locking
}
```

### WikiConfig (Feature Configuration)

```go
type WikiConfig struct {
    Enabled              bool    // Feature flag
    AutoIngest           bool    // Auto-generate from uploads
    SynthesisModelID     string  // LLM model (defaults to KB.SummaryModelID)
    MaxPagesPerIngest    int     // Batch size limit (0 = unlimited)
}
```

---

## Key Services

### WikiPageService

**Responsibilities:**
- CRUD operations on wiki pages
- Link parsing and bidirectional reference management
- Index/log page creation and updates
- Graph generation for visualization
- Statistics aggregation
- Cross-link injection (mentions → auto-links)

**Key Methods:**
- `CreatePage()`, `UpdatePage()`, `DeletePage()`
- `GetPageBySlug()`, `ListPages()`, `SearchPages()`
- `GetIndex()`, `GetLog()`
- `GetGraph()` (for visualization)
- `GetStats()` (aggregate statistics)
- `RebuildLinks()` (full rebuild)
- `InjectCrossLinks()` (auto-link mentions)

### WikiIngestService

**Responsibilities:**
- Orchestrate async wiki generation from documents
- Manage Redis pending queue and distributed locks
- Coordinate MAP-REDUCE batch processing
- Call LLM prompts for extraction and synthesis
- Handle retract operations (document deletion)
- Maintain operation log

**Key Methods:**
- `EnqueueWikiIngest()` (queue a document)
- `EnqueueWikiRetract()` (queue a deletion)
- `ProcessWikiIngest()` (main batch handler)
- `mapOneDocument()` (extract entities/concepts)
- `reduceSlugUpdates()` (merge and synthesize)

---

## Async Pipeline Details

### Debouncing Mechanism

```
Time    Action
─────────────────────────────────────
0s      doc1 uploaded → RPush + Enqueue(delay=30s)
5s      doc2 uploaded → RPush + Enqueue(delay=30s)
10s     doc3 uploaded → RPush + Enqueue(delay=30s)
30s     Task 1 fires → Drains [doc1,doc2,doc3] → Process all
35s     Task 2 fires → Drains [] → No-op return
40s     Task 3 fires → Drains [] → No-op return
```

**Benefit:** Natural batching without explicit deduplication logic.

### MAP Phase

Parallel extraction (10 goroutines max):
- For each document: Extract entities & concepts from chunks
- Call LLM: `WikiKnowledgeExtractPrompt`
- Result: List of SlugUpdate operations

**Per Document:**
- Read chunks from ChunkRepository
- Reconstruct content (truncated to 32KB)
- Call LLM extraction (single call for both entities + concepts)
- Deduplicate against existing pages (single LLM call for batch)
- Generate summary page
- Identify stale pages (from previous ingest)

### REDUCE Phase

Parallel merging (10 goroutines max):
- For each affected slug: Apply all accumulated updates
- Call LLM: `WikiPageModifyPrompt` (for entity/concept synthesis)
- Save page to DB

**Per Slug:**
- Separate updates into: additions, retracts, summary
- Build LLM prompt with context (existing content, new info, remaining sources)
- Call LLM to synthesize merged content
- Update page record

### Post-Processing

```
1. Append log entries (one per operation)
2. Rebuild index page (LLM updates intro, code rebuilds directory)
3. Clean dead links (remove [[broken-links]])
4. Inject cross-links (auto-link mentions of other pages)
5. Publish draft pages (transition to published)
6. Trim processed items from Redis pending list
7. Schedule follow-up if more items remain
```

---

## Cross-Link Injection

**Purpose:** Auto-detect mentions of existing wiki page titles and wrap with `[[wiki-links]]`.

**Implementation:** Pure text processing (no LLM cost)

**Algorithm:**
1. Sort refs by matchText length (descending) → longer strings match first
2. Compute forbidden spans → regions untouched (code blocks, existing links, etc.)
3. For each ref:
   - Skip if already linked elsewhere
   - Find first safe match outside forbidden spans
   - Check word boundaries (for ASCII text)
   - Wrap with `[[slug|matchText]]`

**Forbidden Regions:**
- Fenced code blocks (```/~~~)
- Inline code (backticks)
- Existing wiki links
- Markdown links [text](url)
- Images ![alt](url)
- Reference-style links
- Autolinks <url>

---

## Retrieval Integration

### ChunkType Classification

```
ChunkTypeWikiPage = "wiki_page"

Used to:
1. Differentiate wiki pages from document chunks
2. Apply retrieval boost (1.3x score multiplier)
3. Filter results if needed
```

### WikiBoost Plugin

**Phase:** `CHUNK_RERANK` (after initial retrieval and reranking)

**Algorithm:**
1. Check if any chunks have `ChunkType == "wiki_page"`
2. Verify at least one search target is a wiki KB
3. Multiply wiki page scores by 1.3x
4. Re-sort results

**Rationale:** Wiki pages are pre-synthesized and cross-referenced, making them more valuable than raw document chunks.

---

## Agent Integration

### Wiki Tools Available

| Tool | Purpose |
|------|---------|
| `wiki_read_page` | Retrieve page content for inspection |
| `wiki_write_page` | Create or overwrite page (synthesis/comparison) |
| `wiki_delete_page` | Soft-delete page |
| `wiki_rename_page` | Rename slug and update references |
| `wiki_read_source_doc` | Verify facts against original document |
| `wiki_flag_issue` | Report problem on page |
| `wiki_read_issue` | Review flagged issues |
| `wiki_update_issue` | Mark issue resolved/ignored |

### Common Agent Workflows

**Creating a Synthesis Page:**
```
1. Agent analyzes multiple documents
2. Calls wiki_write_page with:
   - slug: "synthesis/..."
   - page_type: "synthesis"
   - content: Full markdown with [[wiki-links]]
   - source_refs: Knowledge IDs of documents analyzed
3. Tool parses links from content
4. Tool calls InjectCrossLinks() (auto-link mentions)
5. Tool calls RebuildIndexPage() (add to index)
6. Page now appears in search and graph
```

---

## API Endpoints

**Base:** `/api/v1/knowledgebase/{kb_id}/wiki`

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/pages` | List with pagination/filtering |
| POST | `/pages` | Create page |
| GET | `/pages/{slug}` | Get single page |
| PUT | `/pages/{slug}` | Update page |
| DELETE | `/pages/{slug}` | Soft-delete |
| GET | `/index` | Get index (auto-create) |
| GET | `/log` | Get log (auto-create) |
| GET | `/graph` | Link graph for visualization |
| GET | `/stats` | Aggregate statistics |
| GET | `/lint` | Health check |
| POST | `/rebuild-links` | Force rebuild |
| GET | `/search` | Full-text search |

---

## Configuration

### KB-Level Settings

```json
{
  "enabled": true,
  "auto_ingest": true,
  "synthesis_model_id": "gpt-4-turbo",
  "max_pages_per_ingest": 0
}
```

Updated via: `PUT /api/v1/knowledgebase/{id}` with `wiki_config` field.

### Agent Access

```go
type AgentConfig struct {
    KnowledgeBases    []string // KB IDs agent can access
    KnowledgeIDs      []string // Individual docs
    // Tools available:
    // - wiki_read_page, wiki_write_page, wiki_delete_page, etc.
}
```

---

## Performance & Scaling

### Optimization Techniques

| Technique | Benefit |
|-----------|---------|
| **Debouncing (30s)** | Reduces task scheduling overhead |
| **Batching (5 docs)** | Amortizes LLM costs |
| **Parallel processing (10 goroutines)** | Utilizes multi-core efficiently |
| **Batch dedup (1 LLM call)** | Entities + concepts in single call |
| **Pre-loading (WikiBatchContext)** | All pages fetched once per batch |
| **Fast path (wiki boost)** | Skip work if no wiki chunks |
| **Content truncation (32KB)** | Limits LLM input size |

### Scaling Limits

- **Redis pending list:** 24-hour TTL (auto-cleanup)
- **Active lock:** 5-minute TTL with 2-minute refresh
- **Task timeout:** 60 minutes
- **Max retries:** 10 (outlasts lock TTL)
- **Sequential per KB:** One batch at a time per knowledge base

---

## Health Monitoring

### Linting Issues Detected

| Issue | Severity | Fixable |
|-------|----------|---------|
| Orphan page (no inbound links) | Warning | No |
| Broken link (target deleted) | Error | Yes |
| Empty content (<50 chars) | Warning | Yes |
| Missing cross-reference (mention not linked) | Info | No |
| Stale reference (doc deleted) | Warning | No |

### Health Score

Base: 100
- Each orphan (if >50%): -25
- Each broken link: -2
- Each stale ref: -1

**API:** `GET /api/v1/knowledgebase/{id}/wiki/lint`

---

## Common Questions

**Q: Is wiki a separate KB type?**  
A: No. Wiki is a feature flag on document-type KBs. You enable it via `WikiConfig.Enabled`.

**Q: What triggers wiki generation?**  
A: Document upload (if `WikiConfig.AutoIngest = true`). System calls `EnqueueWikiIngest()`.

**Q: Can I disable auto-generation?**  
A: Yes. Set `AutoIngest = false`. Manual wiki_write_page tool still works.

**Q: How is wiki content indexed?**  
A: Wiki pages are converted to chunks with `ChunkType = "wiki_page"` and indexed like normal.

**Q: How does wiki affect search results?**  
A: WikiBoost plugin increases wiki page scores by 1.3x during reranking.

**Q: Can agents create wiki pages?**  
A: Yes. Use `wiki_write_page` tool. Agent decides slug, content, page type.

**Q: What happens to wiki when I delete a document?**  
A: `EnqueueWikiRetract()` is called. Pages referencing the document are updated/deleted.

**Q: How do cross-links work?**  
A: Pure text matching. System finds mentions of page titles and wraps with `[[slug|title]]`.

**Q: What's the performance impact?**  
A: Minimal. Debouncing/batching amortizes LLM costs. Boosting adds negligible overhead.

---

## File Structure Overview

```
internal/
├── types/
│   ├── wiki_page.go                      # Data models
│   ├── chunk.go                          # ChunkType constants
│   └── interfaces/wiki_page.go           # Service interfaces
├── application/
│   ├── service/
│   │   ├── wiki_page.go                  # WikiPageService
│   │   ├── wiki_ingest.go                # EnqueueWikiIngest
│   │   ├── wiki_ingest_batch.go          # ProcessWikiIngest (MAP-REDUCE)
│   │   ├── wiki_linkify.go               # Cross-link injection
│   │   ├── wiki_lint.go                  # Health checking
│   │   ├── chat_pipeline/wiki_boost.go   # Retrieval boosting
│   │   └── chunk.go                      # Chunk operations
│   ├── repository/
│   │   └── wiki_page.go                  # Database layer
│   └── handler/
│       └── wiki_page.go                  # HTTP endpoints
├── agent/
│   ├── tools/
│   │   ├── wiki_write_page.go            # Create/update pages
│   │   ├── wiki_read_page.go             # Read pages
│   │   ├── wiki_delete_page.go           # Delete pages
│   │   └── ...                           # Other wiki tools
│   └── prompts_wiki.go                   # LLM prompts
└── router/router.go                      # Route registration
```

---

## Next Steps for Developers

### Understanding Specific Areas

1. **Page Creation Flow:** Read `wiki_page.go:CreatePage()` + `wiki_page.go:parseOutLinks()`
2. **Document Ingestion:** Read `wiki_ingest_batch.go:ProcessWikiIngest()` + `mapOneDocument()`
3. **Link Synthesis:** Read `wiki_ingest_batch.go:reduceSlugUpdates()` + LLM prompts
4. **Cross-linking:** Read `wiki_linkify.go:linkifyContent()` + `computeForbiddenSpans()`
5. **Retrieval Boost:** Read `chat_pipeline/wiki_boost.go` + chunk indexing flow

### Common Modifications

- **Change LLM prompts:** Edit `internal/agent/prompts_wiki.go`
- **Adjust batch size:** Change `wikiMaxDocsPerBatch` in `wiki_ingest.go`
- **Change boost factor:** Modify `wikiBoostFactor` in `wiki_boost.go`
- **Add issue type:** Extend `WikiLintIssueType` enum in `wiki_lint.go`
- **New agent tool:** Create `internal/agent/tools/wiki_*.go` + register

---

## References

- **Full Technical Deep Dive:** See `WIKI_SYSTEM_DEEP_DIVE.md` (948 lines)
- **Architecture Analysis:** See `KB_WIKI_ARCHITECTURE_ANALYSIS.md` (34KB)
- **File Index:** See `KB_WIKI_FILE_INDEX.md`

---

**End of Summary**

Generated: 2026-04-20
