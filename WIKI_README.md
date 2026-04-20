# WeKnora Wiki System - Complete Documentation

Welcome! This directory contains comprehensive documentation of the WeKnora wiki knowledge base system.

## Quick Start

**New to the wiki system?** Start here:

1. **Read WIKI_SYSTEM_SUMMARY.md** (5-10 minutes)
   - Understand what the wiki system does
   - Learn the key architecture concepts
   - Get familiar with common workflows

2. **Browse WIKI_DOCS_INDEX.md** (2 minutes)
   - Find what you're looking for
   - Navigate between documents
   - Make decisions about which doc to read next

3. **Dive into specific docs**
   - **Implementing a feature?** → WIKI_IMPLEMENTATION_GUIDE.md
   - **Designing changes?** → WIKI_TECHNICAL_ANALYSIS.md
   - **Need complete reference?** → WIKI_SYSTEM_DEEP_DIVE.md

## Document Overview

| Document | Purpose | Length | Audience |
|----------|---------|--------|----------|
| **WIKI_SYSTEM_SUMMARY.md** | Executive overview of wiki functionality | 470 lines | Everyone |
| **WIKI_SYSTEM_DEEP_DIVE.md** | Comprehensive technical reference | 950 lines | Developers, Architects |
| **WIKI_TECHNICAL_ANALYSIS.md** | Deep architectural analysis with edge cases | 1,170 lines | Advanced developers, Architects |
| **WIKI_IMPLEMENTATION_GUIDE.md** | Practical how-to guide with code examples | 750 lines | Feature developers |
| **WIKI_DOCS_INDEX.md** | Navigation and cross-references | 480 lines | Everyone |
| **WIKI_README.md** | This file | - | First-time readers |

**Total:** ~5,800 lines of documentation + ~7,300 lines of source code

## Key Concepts at a Glance

### What is Wiki?

A knowledge graph system that:
- Extracts entities and concepts from uploaded documents
- Synthesizes multi-document knowledge into canonical pages
- Maintains bidirectional link references for graph traversal
- Auto-links page content via pattern matching
- Integrates with chat retrieval for enhanced results

### Core Architecture

```
Document Upload
    ↓
Queue (Redis, 30s debounce)
    ↓
Async Batch Processing (MAP-REDUCE)
    ├─ Extract: LLM calls for entities/concepts
    ├─ Synthesize: Merge multi-document knowledge
    ├─ Post-process: Inject cross-links, rebuild index
    ↓
Database Storage + Retrieval Indexing
    ↓
Chat Integration (1.3x score boost for wiki pages)
```

### Key Design Decisions

1. **Wiki as Feature Flag** — Not a KB type, but an option on any KB
2. **Async with Debouncing** — Batches documents for efficiency
3. **MAP-REDUCE Pattern** — Parallel extraction and synthesis
4. **Bidirectional Links** — Automatic link maintenance for graph queries
5. **Text-Based Injection** — Pattern matching for auto-linking
6. **Optimistic Locking** — Version-based conflict detection

## FAQ

**Q: Is wiki enabled for my knowledge base?**  
A: Check if `kb.WikiConfig.Enabled` is true. Use `GetKnowledgeBaseByID` to verify.

**Q: Why is my page not showing in search?**  
A: Pages must have status=`published`. Check `RunLint` for health issues. Wiki page chunks need to be synchronized (should happen automatically).

**Q: How long does wiki ingestion take?**  
A: Documents queue with a 30-second debounce. Typically 5-30 seconds per batch depending on LLM latency.

**Q: Can I update a page while ingestion is running?**  
A: Yes, but you might get `ErrWikiPageConflict` if concurrent updates hit the same page. Retry with fresh version.

**Q: How do I fix broken links?**  
A: Run `WikiLintService.RunLint()` to detect them. Use `AutoFix()` to repair automatically, or manually update page content.

**Q: Can wiki pages link to each other?**  
A: Yes! Links are parsed from `[[slug]]` or `[[slug|display text]]` markdown syntax in page content.

**Q: What's the difference between UpdatePage and UpdatePageMeta?**  
A: `UpdatePage` is for content changes and bumps version. `UpdatePageMeta` is for internal bookkeeping (links, status) without version bump.

**Q: How does cross-link injection work?**  
A: System scans page content for matches of entity/concept names, wraps unlinked mentions as `[[slug|name]]`. Smart enough to skip code blocks and existing links.

**Q: What if LLM extraction fails?**  
A: Document is logged as processed anyway. No automatic retry. Document can be re-uploaded if needed.

**Q: How can I monitor wiki health?**  
A: Use `WikiLintService.RunLint()` which returns a health score (0-100), statistics, and detected issues.

## Common Tasks

### Check if Wiki is Enabled
```go
kb, _ := kbService.GetKnowledgeBaseByID(ctx, kbID)
if kb.IsWikiEnabled() {
    // Wiki is active
}
```

### Create a Wiki Page
```go
page := &types.WikiPage{
    Slug:    "entity/acme-corp",
    Title:   "Acme Corporation",
    Content: "[[entity/bob]] founded Acme in 2020.",
    PageType: types.WikiPageTypeEntity,
}
created, _ := wikiService.CreatePage(ctx, page)
```

### Get All Pages
```go
pages, _ := wikiService.ListAllPages(ctx, kbID)
for _, p := range pages {
    fmt.Printf("%s: %s (%d inbound, %d outbound links)\n", 
        p.Slug, p.Title, len(p.InLinks), len(p.OutLinks))
}
```

### Check Page Health
```go
report, _ := lintService.RunLint(ctx, kbID)
fmt.Printf("Health Score: %d/100\n", report.HealthScore)
if report.HealthScore < 70 {
    fixed, _ := lintService.AutoFix(ctx, kbID)
    fmt.Printf("Fixed %d issues\n", fixed)
}
```

### Update Page Content
```go
page, _ := wikiService.GetPageBySlug(ctx, kbID, slug)
page.Content = newContent  // Update content
updated, err := wikiService.UpdatePage(ctx, page)
if err != nil {
    // Handle conflict retry
}
```

## File Structure

```
WeKnora/
├── WIKI_README.md                               ← You are here
├── WIKI_DOCS_INDEX.md                           ← Navigation hub
├── WIKI_SYSTEM_SUMMARY.md                       ← Start here (5 min)
├── WIKI_SYSTEM_DEEP_DIVE.md                     ← Comprehensive reference
├── WIKI_TECHNICAL_ANALYSIS.md                   ← Deep architecture
├── WIKI_IMPLEMENTATION_GUIDE.md                 ← How-to guide
│
├── internal/types/
│   ├── wiki_page.go                             ← Data models
│   └── wiki_page_test.go                        ← Model tests
│
├── internal/application/service/
│   ├── wiki_page.go                             ← Core service (628 lines)
│   ├── wiki_ingest.go                           ← Async pipeline (1,053 lines)
│   ├── wiki_ingest_batch.go                     ← MAP-REDUCE handler (796 lines)
│   ├── wiki_linkify.go                          ← Cross-link injection (565 lines)
│   ├── wiki_lint.go                             ← Health checking (323 lines)
│   ├── wiki_page_test.go                        ← Service tests
│   ├── wiki_ingest_test.go                      ← Ingest tests
│   ├── wiki_linkify_test.go                     ← Linkify tests (285 lines)
│   ├── chat_pipeline/
│   │   └── wiki_boost.go                        ← Retrieval plugin
│   └── ...
│
├── internal/application/repository/
│   └── wiki_page.go                             ← Data access layer (357 lines)
│
├── internal/handler/
│   └── wiki_page.go                             ← HTTP endpoints (560 lines)
│
├── internal/agent/
│   ├── prompts_wiki.go                          ← LLM prompts (258 lines)
│   └── tools/
│       ├── wiki_write_page.go                   ← Create/update tool
│       ├── wiki_delete_page.go                  ← Delete tool
│       ├── wiki_replace_text.go                 ← Find/replace tool
│       └── ...
│
└── ... (other WeKnora files)
```

## Reading Paths

### Path 1: Quick Orientation (15 minutes)
1. WIKI_README.md (this file) — 3 min
2. WIKI_SYSTEM_SUMMARY.md — 5 min
3. WIKI_DOCS_INDEX.md → choose next doc — 5 min

### Path 2: Implementing a Feature (90 minutes)
1. WIKI_SYSTEM_SUMMARY.md — 5 min
2. WIKI_IMPLEMENTATION_GUIDE.md → relevant task — 20 min
3. Source code reading (related service) — 30 min
4. Write tests — 20 min
5. Review WIKI_TECHNICAL_ANALYSIS.md for edge cases — 15 min

### Path 3: Architectural Review (2-3 hours)
1. WIKI_SYSTEM_SUMMARY.md — 10 min
2. WIKI_SYSTEM_DEEP_DIVE.md (full) — 45 min
3. WIKI_TECHNICAL_ANALYSIS.md (full) — 60 min
4. Source code deep dive — 45 min

### Path 4: Debugging (30-60 minutes)
1. WIKI_TECHNICAL_ANALYSIS.md → Part 4 (Edge Cases) — 15 min
2. WIKI_IMPLEMENTATION_GUIDE.md → Part 5 (Debugging) — 10 min
3. SQL queries in appendix — 5 min
4. Source code inspection — 20-30 min

## Key Files to Know

**Most Important:**
- `wiki_page.go` (service) — Core operations
- `wiki_ingest.go` — Async orchestration
- `wiki_ingest_batch.go` — Batch processor
- `wiki_linkify.go` — Link injection algorithm

**Important:**
- `wiki_page.go` (types) — Data structures
- `wiki_lint.go` — Health checking
- `wiki_page.go` (repository) — Data access
- `wiki_page.go` (handler) — HTTP API

**Reference:**
- `prompts_wiki.go` — LLM prompts
- `wiki_boost.go` — Retrieval integration

## Testing

All wiki functionality has tests:
- Unit tests: `wiki_linkify_test.go` (285 lines, 13 test functions)
- Service tests: `wiki_page_test.go` (100 lines)
- Integration tests: `wiki_ingest_test.go`

Run tests:
```bash
go test ./internal/application/service/...
go test ./internal/application/repository/...
go test ./internal/types/...
```

## Performance Notes

- **Debounce window:** 30 seconds
- **Batch size:** 5 documents per batch
- **Parallel goroutines:** 10 (extraction), 10 (synthesis)
- **Lock TTL:** 5 minutes
- **Pending operations TTL:** 24 hours
- **Typical page size:** 2-50 KB
- **Cross-link injection:** O(n*m) where n=pages, m=slug count

## Operational Monitoring

Key metrics to watch:
- `wiki:pending:{kbID}` list length (pending documents)
- `wiki:active:{kbID}` presence (processing running?)
- Health score from `RunLint` (0-100)
- Orphan page count (pages with no inbound links)
- Broken link count (pages linking to nonexistent targets)

## Getting Help

1. **API question?** → WIKI_SYSTEM_DEEP_DIVE.md (API Endpoints section)
2. **Implementation question?** → WIKI_IMPLEMENTATION_GUIDE.md
3. **Architecture question?** → WIKI_TECHNICAL_ANALYSIS.md
4. **Can't find something?** → WIKI_DOCS_INDEX.md (Navigation)
5. **Need to run SQL query?** → WIKI_IMPLEMENTATION_GUIDE.md (Appendix C)

## Contributing to Documentation

If you:
- Find inaccuracies → update the doc
- Find gaps → add examples or explanations
- Find unclear sections → simplify or add context
- Implement new features → document them here

Keep all docs in `/` (root directory) for easy access.

---

**Last Updated:** April 20, 2026  
**Total Documentation:** ~5,800 lines  
**Total Source Code:** ~7,300 lines (wiki-related files only)

