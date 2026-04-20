# Wiki System Documentation Index

**Purpose:** Central reference for all wiki system documentation  
**Last Updated:** April 2026

---

## Quick Navigation

### For First-Time Readers
1. Start with **WIKI_SYSTEM_SUMMARY.md** (5 min read)
   - Overview of wiki functionality
   - Key architectural decisions
   - Common workflows

2. Then read **WIKI_SYSTEM_DEEP_DIVE.md** (15 min read)
   - Detailed service descriptions
   - Data flow diagrams
   - Complete API endpoints

### For Developers Implementing Features
1. **WIKI_IMPLEMENTATION_GUIDE.md** (primary reference)
   - Common tasks with code examples
   - Testing patterns
   - Error handling
   - Performance optimization
   - Debugging techniques

2. **WIKI_TECHNICAL_ANALYSIS.md** (deep reference)
   - Architectural patterns explained
   - Edge cases documented
   - Operational considerations
   - Known limitations

### For Architects and Code Reviewers
1. **WIKI_TECHNICAL_ANALYSIS.md** (full read recommended)
   - Part 2: Core Concepts and Patterns
   - Part 3: Detailed Implementation Patterns
   - Part 4: Edge Cases and Gotchas
   - Part 5: Operational Considerations
   - Part 7: Future Enhancements

2. **Source Code** (in this order)
   - `internal/types/wiki_page.go` — data models
   - `internal/application/service/wiki_page.go` — core service
   - `internal/application/service/wiki_ingest.go` — async pipeline
   - `internal/application/service/wiki_ingest_batch.go` — MAP-REDUCE handler
   - `internal/application/service/wiki_linkify.go` — cross-link injection
   - `internal/application/service/wiki_lint.go` — health checking

---

## Documentation Overview

### WIKI_SYSTEM_SUMMARY.md
**Type:** Executive Summary  
**Length:** ~470 lines  
**Audience:** Everyone  
**Key Sections:**
- What is wiki?
- Quick architecture map
- Core data structures
- Key services overview
- Async pipeline simplified
- Cross-link injection overview
- Retrieval integration
- Agent integration
- API endpoints quick reference
- Configuration overview
- FAQ

**When to Read:** First introduction to wiki system

---

### WIKI_SYSTEM_DEEP_DIVE.md
**Type:** Technical Reference  
**Length:** ~950 lines  
**Audience:** Developers, architects  
**Key Sections:**
- Complete system overview
- Core data models with full struct definitions
- Service architecture with dependency graphs
- Exhaustive async pipeline documentation
- Cross-link injection algorithm details
- Wiki chunking and retrieval integration
- Agent integration with workflows
- Complete API endpoints table
- Configuration and error handling
- Performance optimization
- Common workflows and FAQ
- File structure appendix

**When to Read:** When you need comprehensive understanding of a subsystem

---

### WIKI_TECHNICAL_ANALYSIS.md
**Type:** Deep Technical Analysis  
**Length:** ~1,170 lines  
**Audience:** Architects, advanced developers  
**Key Sections:**
- **Part 1:** Architectural Overview (1.1-1.3)
  - System context and purpose
  - Wiki as feature flag (key design decision)
  - Core data flow diagram
  - Key files and responsibilities
  
- **Part 2:** Core Concepts and Patterns (2.1-2.5)
  - Bidirectional link references
  - Optimistic locking with version numbers
  - Async batch processing with debouncing
  - MAP-REDUCE pattern for extraction
  - Cross-link injection with forbidden spans
  
- **Part 3:** Detailed Implementation Patterns (3.1-3.4)
  - Repository layer: optimistic locking
  - Service layer: multi-step operations
  - Async pipeline: WikiIngestService
  - Linting and health monitoring
  
- **Part 4:** Edge Cases and Gotchas (4.1-4.9)
  - Concurrent update conflicts
  - Link consistency windows
  - Self-link prevention
  - LLM conflict detection
  - Slug reuse across updates
  - CJK text handling
  - Forbidden span edge cases
  - Redis timing issues
  - LLM parsing failures
  
- **Part 5:** Operational Considerations (5.1-5.4)
  - Performance scaling
  - Storage considerations
  - Monitoring and debugging
  - Common pitfalls and solutions
  
- **Part 6:** Integration Points (6.1-6.3)
  - Chat pipeline integration
  - Agent tool integration
  - Chunk synchronization
  
- **Part 7:** Future Enhancements (7.1-7.2)
  - Potential improvements
  - Known limitations

**When to Read:** When designing new features or debugging complex issues

---

### WIKI_IMPLEMENTATION_GUIDE.md
**Type:** Practical How-To Guide  
**Length:** ~750 lines  
**Audience:** Developers implementing wiki features  
**Key Sections:**
- **Part 1:** Common Tasks and Patterns
  - Task 1: Create a new wiki page programmatically
  - Task 2: Update an existing page
  - Task 3: Work with links programmatically
  - Task 4: Iterate over all pages efficiently
  - Task 5: Work with cross-link injection
  
- **Part 2:** Testing Patterns
  - Unit tests
  - Integration tests
  
- **Part 3:** Error Handling
  - ErrWikiPageNotFound
  - ErrWikiPageConflict
  - LLM extraction failures
  
- **Part 4:** Performance Optimization
  - Batch creation
  - Query optimization
  - Link rebuild efficiency
  
- **Part 5:** Debugging and Troubleshooting
  - Check page consistency
  - Monitor wiki health
  
- **Part 6:** Common Mistakes
  - 10 common mistakes with fixes
  
- **Appendix A:** Key functions reference
- **Appendix B:** Configuration
- **Appendix C:** SQL queries for debugging

**When to Read:** Before implementing any wiki feature

---

### From Previous Sessions: Supporting Documentation

#### KB_WIKI_ARCHITECTURE_ANALYSIS.md
**Type:** Initial architecture analysis  
**Length:** ~750 lines  
**Key Content:**
- High-level system design
- Data model analysis
- Service relationships
- Batch processing flow
- Integration points

#### KB_WIKI_QUICK_REFERENCE.md
**Type:** Quick reference  
**Length:** ~300 lines  
**Key Content:**
- Quick navigation to common tasks
- API endpoint quick list
- Configuration quick reference
- Common errors quick lookup

#### KB_WIKI_FILE_INDEX.md
**Type:** File directory and location reference  
**Length:** ~250 lines  
**Key Content:**
- All wiki-related files with locations
- File purposes and responsibilities
- Cross-references
- Updated file counts and statistics

---

## Source Code Quick Reference

### Core Data Models
**File:** `internal/types/wiki_page.go`
- `WikiPage` struct (84 lines)
- `WikiConfig` struct (12 lines)
- `WikiPageType` constants
- `WikiPageStatus` constants
- `WikiStats`, `WikiGraphNode`, `WikiGraphEdge` structs
- `WikiPageIssue` struct

### Service Layer
**File:** `internal/application/service/wiki_page.go`
- `WikiPageService` implementation
- CreatePage, UpdatePage, UpdatePageMeta
- GetPageBySlug, GetPageByID, ListPages
- DeletePage, RebuildLinks, InjectCrossLinks
- Helper functions: parseOutLinks, normalizeSlug, updateInLinks, removeInLinks

**File:** `internal/application/service/wiki_lint.go`
- `WikiLintService` implementation
- RunLint (comprehensive health check)
- AutoFix (automatic repair)

### Async Pipeline
**File:** `internal/application/service/wiki_ingest.go`
- `WikiIngestService` implementation
- EnqueueWikiIngest, EnqueueWikiRetract
- Debouncing mechanism
- Redis queue management

**File:** `internal/application/service/wiki_ingest_batch.go`
- ProcessWikiIngest handler (MAP-REDUCE)
- mapOneDocument (extraction)
- reduceSlugUpdates (synthesis)
- Post-processing steps

### Text Processing
**File:** `internal/application/service/wiki_linkify.go`
- linkifyContent (main algorithm)
- computeForbiddenSpans (markdown structure detection)
- findFirstSafeMatch (safe occurrence finding)
- hasWordBoundary, isASCIIWordRune (boundary checking)
- Comprehensive forbidden span detection

### Repository Layer
**File:** `internal/application/repository/wiki_page.go`
- `WikiPageRepository` implementation
- Create, Update, UpdateMeta operations
- GetByID, GetBySlug queries
- List with filtering and pagination
- Optimistic locking implementation

### HTTP Handlers
**File:** `internal/handler/wiki_page.go`
- `WikiPageHandler` (HTTP endpoints)
- ListPages, CreatePage, GetPage, UpdatePage, DeletePage
- GetGraph, GetStats, RebuildLinks, RebuildIndexPage
- Health checking and stats endpoints

### Integration Plugins
**File:** `internal/application/service/chat_pipeline/wiki_boost.go`
- `PluginWikiBoost` retrieval scoring plugin
- 1.3x score multiplier for wiki pages
- Ranks wiki content higher in search results

### Agent Tools
**File:** `internal/agent/tools/wiki_write_page.go`
- `wikiWritePageTool` (create/update pages from agent)
- Parameters: slug, title, summary, content, page_type

**File:** `internal/agent/tools/wiki_delete_page.go`
- `wikiDeletePageTool` (delete pages from agent)

**File:** `internal/agent/tools/wiki_replace_text.go`
- `wikiReplaceTextTool` (find/replace within page)

### LLM Prompts
**File:** `internal/agent/prompts_wiki.go`
- `WikiSummaryPrompt` (summary generation)
- `WikiKnowledgeExtractPrompt` (entity/concept extraction)
- `WikiPageModifyPrompt` (page updating)

---

## Cross-Document References

### By Topic

**Understanding Concurrency:**
1. WIKI_TECHNICAL_ANALYSIS.md → Part 4.1 (Concurrent update conflicts)
2. WIKI_IMPLEMENTATION_GUIDE.md → Part 3 (Error handling)
3. wiki_page.go (repo) → Update method (lines 36-60)

**Understanding Links:**
1. WIKI_TECHNICAL_ANALYSIS.md → Part 2.1 (Bidirectional links)
2. wiki_page.go (service) → updateInLinks, removeInLinks methods
3. WIKI_IMPLEMENTATION_GUIDE.md → Task 3 (Work with links)

**Understanding Async Processing:**
1. WIKI_TECHNICAL_ANALYSIS.md → Part 2.3 & 2.4 (Debouncing and MAP-REDUCE)
2. WIKI_SYSTEM_DEEP_DIVE.md → Async pipeline section
3. wiki_ingest.go → EnqueueWikiIngest method
4. wiki_ingest_batch.go → ProcessWikiIngest handler

**Understanding Cross-Link Injection:**
1. WIKI_TECHNICAL_ANALYSIS.md → Part 2.5 (Forbidden spans)
2. WIKI_IMPLEMENTATION_GUIDE.md → Task 5 (Cross-link injection)
3. wiki_linkify.go → Full implementation
4. wiki_linkify_test.go → Test cases (coverage)

**Understanding Health Checking:**
1. WIKI_TECHNICAL_ANALYSIS.md → Part 3.4 (Linting)
2. wiki_lint.go → Full implementation
3. WIKI_IMPLEMENTATION_GUIDE.md → Part 5 (Monitoring)

---

## Key Diagrams and Flows

### Data Flow (ASCII)
```
Document Upload
    ↓
EnqueueWikiIngest → Redis List (30s debounce)
    ↓
ProcessWikiIngest [Async]
    ├─ MAP: Extract entities/concepts (parallel)
    ├─ REDUCE: Synthesize pages (parallel)
    ├─ Post-processing: Links, index, cross-link injection
    └─ Schedule follow-up if pending remain
    ↓
Wiki Pages in DB
    ├─ Synchronized to chunks (ChunkType="wiki_page")
    ├─ Integrated in retrieval pipeline
    └─ Boosted 1.3x in chat search
    ↓
Chat Integration
    └─ Higher-ranked results → better answers
```

### Service Dependencies
```
WikiPageService
    ├─ WikiPageRepository (data access)
    ├─ ChunkRepository (sync to retrieval)
    ├─ KnowledgeBaseService (KB validation)
    └─ Redis (pending queue, locking)

WikiIngestService
    ├─ WikiPageService (create/update pages)
    ├─ KnowledgeBaseService (KB config)
    ├─ KnowledgeService (fetch document chunks)
    ├─ ChunkRepository (sync results)
    ├─ ModelService (LLM calls)
    ├─ Task queue (Asynq)
    └─ Redis (debouncing, locking)

WikiLintService
    ├─ WikiPageService (get pages, graph)
    ├─ KnowledgeBaseService (KB validation)
    └─ No database write access (read-only)
```

### State Transitions
```
WikiPage States:
    draft ──→ published ──→ archived
                  ↑
                  └─── (UpdatePage increments version)

WikiPageIssue States:
    pending ──→ acknowledged ──→ resolved

Page Creation Flow:
    1. Create page + parse OutLinks
    2. Insert to DB
    3. Update target InLinks (async, fire-and-forget)
    4. Return to caller

Page Update Flow:
    1. Fetch existing (get current version)
    2. Update fields + re-parse OutLinks
    3. Optimistic lock check (WHERE version = X)
    4. Update InLinks on old targets (remove)
    5. Update InLinks on new targets (add)
    6. Return updated page or ErrWikiPageConflict
```

---

## Decision Matrix: Which Document to Read

| Question | Document |
|----------|----------|
| What is wiki and why do we have it? | WIKI_SYSTEM_SUMMARY.md |
| How do I create a new wiki page? | WIKI_IMPLEMENTATION_GUIDE.md (Task 1) |
| How does async processing work? | WIKI_TECHNICAL_ANALYSIS.md (Part 2.3-2.4) |
| What are the edge cases I should know about? | WIKI_TECHNICAL_ANALYSIS.md (Part 4) |
| How do I test my wiki feature? | WIKI_IMPLEMENTATION_GUIDE.md (Part 2) |
| What's the complete API? | WIKI_SYSTEM_DEEP_DIVE.md |
| How do I debug a broken link? | WIKI_IMPLEMENTATION_GUIDE.md (Part 5) |
| What's the data model? | internal/types/wiki_page.go |
| How does link injection work? | WIKI_TECHNICAL_ANALYSIS.md (Part 2.5) |
| How do I handle version conflicts? | WIKI_IMPLEMENTATION_GUIDE.md (Part 3) |
| What performance considerations exist? | WIKI_TECHNICAL_ANALYSIS.md (Part 5.1) |
| How is wiki integrated with chat? | WIKI_TECHNICAL_ANALYSIS.md (Part 6.1) |
| What are the LLM prompts? | internal/agent/prompts_wiki.go |
| How do I monitor wiki health? | WIKI_IMPLEMENTATION_GUIDE.md (Part 5) + wiki_lint.go |
| What's the full system architecture? | WIKI_TECHNICAL_ANALYSIS.md (Part 1) |

---

## Maintenance and Updates

This documentation set should be updated when:
- New API endpoints are added or modified
- Database schema changes
- LLM prompt templates change significantly
- Performance characteristics change
- New edge cases discovered
- Configuration options change
- Integration points change

**To update documentation:**
1. Identify which docs are affected
2. Update in order: code first, then summaries
3. Regenerate any tables or metrics
4. Update cross-references
5. Review for accuracy and clarity

---

## Feedback and Questions

If documentation is:
- **Unclear:** Note the specific section and what's confusing
- **Incomplete:** Note what's missing
- **Outdated:** Note what's changed in the code
- **Inaccurate:** Note what's wrong and what's correct

Post improvements as pull requests with clear descriptions.

---

## Statistics

| Metric | Value |
|--------|-------|
| Total documentation | ~3,900 lines |
| Source code (wiki only) | ~7,300 lines |
| Total wiki-related files | ~20 files |
| Test coverage | ~285 lines (wiki_linkify_test.go + wiki_page_test.go) |
| LLM prompts | 3 major templates |
| Database tables | 2 (wiki_pages, wiki_page_issues) |
| Key services | 3 (WikiPageService, WikiIngestService, WikiLintService) |
| API endpoints | 20+ (see WIKI_SYSTEM_DEEP_DIVE.md) |

