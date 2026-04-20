# Knowledge Base & Wiki Feature - Quick Reference

## CRITICAL CONCEPTS

### 1. Wiki is NOT a KB Type
```go
// WRONG: Thinking wiki is a separate KB type
type == "wiki"

// CORRECT: Wiki is a feature enabled on document KBs
type == "document" && WikiConfig.Enabled == true
```

### 2. Three KB Types
| Type | Purpose | Can Have Wiki? |
|------|---------|----------------|
| `document` | Standard docs, PDFs, URLs, manual markdown | ✅ YES |
| `faq` | FAQ pairs (Q&A) | ❌ NO |
| `wiki` | ❌ DOES NOT EXIST - use document + WikiConfig |  |

### 3. WikiConfig Structure
```go
WikiConfig struct {
    Enabled              bool      // Feature toggle
    AutoIngest           bool      // Generate wiki pages on doc ingestion
    SynthesisModelID     string    // LLM model for page generation
    MaxPagesPerIngest    int       // Rate limit (pages per batch)
}
```

---

## DATA FLOW

### Document Ingestion with Wiki
```
1. User uploads PDF → Knowledge entity created
2. ParseStatus: pending → processing → completed
3. Chunks generated (vector + keyword indexed)
4. IF WikiConfig.AutoIngest:
   5a. Wiki ingest task queued (30s delay for debounce)
   5b. LLM generates wiki pages (summary, entities, concepts)
   5c. Wiki pages linked together ([[slug]] syntax)
```

### Wiki Page Types (Auto-generated)
- `summary` - Document overview
- `entity` - Person, organization, place extracted
- `concept` - Topic or idea from document
- `index` - Table of contents (auto-rebuilt)
- `log` - Operation history (auto-maintained)

**Agent-created types:**
- `synthesis` - Agent-generated cross-document analysis
- `comparison` - Agent-generated entity/concept comparison

---

## KEY FILES

### Models
```
internal/types/knowledgebase.go    ← KnowledgeBase + WikiConfig
internal/types/knowledge.go        ← Knowledge (document)
internal/types/wiki_page.go        ← WikiPage + WikiPageIssue
internal/types/agent.go            ← AgentConfig.KnowledgeBases
```

### Services
```
internal/application/service/knowledgebase.go      ← KB CRUD
internal/application/service/knowledge.go          ← Doc ingest
internal/application/service/wiki_ingest.go        ← Wiki generation
internal/application/service/wiki_page.go          ← Wiki CRUD
```

### Handlers/API
```
internal/handler/knowledgebase.go   ← PUT /api/v1/knowledge-bases/{id}
internal/handler/wiki_page.go       ← Wiki endpoints
```

### Frontend
```
frontend/src/api/knowledge-base/index.ts  ← KB API (includes wiki_config)
frontend/src/api/wiki/index.ts             ← Wiki API
```

---

## SETTINGS PAGE UPDATE FLOW

```json
// Frontend sends to PUT /api/v1/knowledge-bases/{id}
{
  "name": "My KB",
  "description": "...",
  "config": {
    "chunking_config": { ... },
    "wiki_config": {
      "enabled": true,
      "auto_ingest": true,
      "synthesis_model_id": "model-id-123",
      "max_pages_per_ingest": 5
    }
  }
}

// Stored in database
knowledge_bases.wiki_config = JSON serialized WikiConfig

// Retrieved on GET /api/v1/knowledge-bases/{id}
{
  "id": "...",
  "name": "My KB",
  "wiki_config": { ... }  // ← Embedded in KB object
}
```

---

## INDEXING PIPELINE

### Two Separate Indices

1. **Chunk Index** (from Documents)
   - Created: When Knowledge document ingested
   - Storage: chunks table + vector DB (Elasticsearch, Weaviate, etc.)
   - Indexed: Vector embeddings + BM25 keywords
   - Search Tool: `knowledge_search`

2. **Wiki Index** (from Wiki Pages)
   - Created: When auto-ingest generates pages
   - Storage: wiki_pages table
   - Indexed: Full-text on content, link graph in InLinks/OutLinks
   - Search Tool: `wiki_read_page`, `wiki_search`

### Hybrid Retrieval Engine
```go
internal/application/service/retriever/keywords_vector_hybrid_indexer.go

Supports multiple backends:
- Elasticsearch (v7, v8)
- Weaviate
- Qdrant
- Milvus
- PostgreSQL
- SQLite (local)
```

---

## AGENT INTEGRATION

### Agent Access to KB

```go
type AgentConfig struct {
    KnowledgeBases []string    // KB IDs agent can access
    KnowledgeIDs   []string    // Specific doc IDs agent can access
}

// Agent uses these tools:
- knowledge_search        → Search chunks in documents
- wiki_read_page          → Read wiki page by slug
- wiki_write_page         → Create/update wiki page
- wiki_replace_text       → Edit page content
- wiki_delete_page        → Archive page
- wiki_read_source_doc    → Get original document
- wiki_flag_issue         → Report problem
- wiki_update_issue       → Mark resolved
```

### Check if Agent Can Use Wiki

```go
kb, _ := kbService.GetKnowledgeBaseByID(ctx, kbID)
if kb.IsWikiEnabled() {
    // Agent can use wiki tools
}
```

---

## ASYNC TASK QUEUE

### Wiki Ingest Pipeline
```
Trigger: Document added to KB with WikiConfig.AutoIngest=true

Task Type: types.TypeWikiIngest
Payload: {
    TenantID: uint64,
    KnowledgeBaseID: string,
    Language: string,
    LiteOps: []WikiPendingOp  // for Lite mode
}

Queue: "low" priority
Timeout: 60 minutes
MaxRetry: 10
Delay: 5 seconds (after initial 30s debounce)

Redis Keys:
- wiki:pending:{kbID}   → List of pending ops
- wiki:active:{kbID}    → Lock (5 min TTL) to prevent concurrent batches

Rate Limit: Max 5 docs per batch (configurable via MaxPagesPerIngest)
```

### Flow
```
1. Document ingested
2. Task enqueued (30s delay to debounce rapid uploads)
3. Asynq picks up task
4. Try to acquire wiki:active:{kbID} lock
   - If locked: retry (asynq will retry automatically)
   - If acquired: proceed
5. Process up to MaxPagesPerIngest docs from wiki:pending:{kbID}
6. Generate wiki pages via LLM
7. Update InLinks/OutLinks
8. If more pending: schedule follow-up task
```

---

## COMMON OPERATIONS

### Create KB with Wiki
```typescript
// Frontend API call
createKnowledgeBase({
    name: "My Knowledge Base",
    type: "document",
    wiki_config: {
        enabled: true,
        auto_ingest: true,
        synthesis_model_id: "gpt-4",
        max_pages_per_ingest: 5
    }
})
```

### Update Wiki Settings
```typescript
// Frontend API call
updateKnowledgeBase("kb-id-123", {
    name: "Updated KB",
    config: {
        wiki_config: {
            enabled: true,
            auto_ingest: false,  // Disable auto-ingest
            synthesis_model_id: "gpt-3.5",
            max_pages_per_ingest: 10
        }
    }
})
```

### Check if KB has Wiki
```go
kb, _ := kbService.GetKnowledgeBaseByID(ctx, kbID)
if kb.IsWikiEnabled() {
    // Wiki is enabled for this KB
    fmt.Printf("Wiki config: %+v\n", kb.WikiConfig)
}
```

### List Wiki Pages
```go
pages, err := wikiService.ListPages(ctx, &WikiPageListRequest{
    KnowledgeBaseID: kbID,
    PageType: "entity",
    Page: 1,
    PageSize: 20,
})
```

### Search Wiki
```go
results, err := wikiService.SearchPages(ctx, kbID, "query text", 10)
```

### Manually Add Wiki Page (Agent Tool)
```
wiki_write_page(
    knowledge_base_id: "kb-id",
    slug: "concept/rag-systems",
    title: "RAG Systems",
    content: "# RAG Systems\n...",
    page_type: "synthesis"
)
```

---

## TROUBLESHOOTING

### Problem: Wiki pages not generating
**Check:**
1. WikiConfig.Enabled = true
2. WikiConfig.AutoIngest = true
3. SynthesisModelID is valid (test model availability)
4. Asynq worker running (check logs for wiki ingest tasks)
5. Redis connectivity (if not Lite mode)

### Problem: Wiki links broken
**Solution:**
1. Call `RebuildLinks()` endpoint
2. This re-parses all pages and rebuilds InLinks/OutLinks

### Problem: Stale wiki content
**Solution:**
1. Call wiki ingest for specific KB
2. Or delete KB and re-add with documents

### Problem: Wiki not appearing in agent tools
**Check:**
1. Agent has KB in config: `AgentConfig.KnowledgeBases`
2. KB has wiki enabled: `kb.IsWikiEnabled()`
3. Agent has `wiki_read_page` in allowed tools

---

## SETTINGS PAGE FORM STRUCTURE

```
Knowledge Base Settings
├─ Basic Info
│  ├─ Name (text)
│  ├─ Description (textarea)
│  └─ Type (select: document, faq) [read-only after creation]
│
├─ Document Processing
│  ├─ Chunking Config
│  │  ├─ Chunk Size (number)
│  │  ├─ Chunk Overlap (number)
│  │  └─ Enable Parent-Child Chunking (toggle)
│  └─ Embedding Model (select)
│
├─ Advanced Features
│  ├─ Image Processing / VLM (toggle + model select)
│  ├─ Speech Recognition / ASR (toggle + model select)
│  ├─ Knowledge Graph Extraction (toggle + config)
│  │
│  └─ 📌 WIKI SETTINGS (if type == "document")
│     ├─ Enable Wiki (toggle)
│     │  ├─ Auto-Ingest (toggle) [only if enabled]
│     │  ├─ Synthesis Model (select) [only if enabled]
│     │  └─ Max Pages Per Ingest (number) [only if enabled]
│     └─ [Link to Wiki management page]
│
└─ FAQ Settings (if type == "faq")
   ├─ Index Mode (select)
   └─ Question Index Mode (select)
```

---

## REDIS KEYS (Advanced)

```
# Wiki pending operations queue (List)
wiki:pending:{kbID}
  → JSON serialized WikiPendingOp[]
  → TTL: 24 hours (auto-cleanup)

# Wiki active batch lock (String)
wiki:active:{kbID}
  → Value: "1"
  → TTL: 5 minutes (auto-extend during processing)

# Note: In Lite mode (no Redis), operations stored in task payload
```

---

## INTERFACES TO IMPLEMENT

If adding new features or backends:

```go
// KB Service
interface KnowledgeBaseService {
    CreateKnowledgeBase(ctx, kb) (*KB, error)
    UpdateKnowledgeBase(ctx, id, name, desc, config) (*KB, error)
    GetKnowledgeBaseByID(ctx, id) (*KB, error)
    ListKnowledgeBases(ctx) ([]*KB, error)
}

// Wiki Service
interface WikiPageService {
    CreatePage(ctx, page) (*WikiPage, error)
    UpdatePage(ctx, page) (*WikiPage, error)
    GetPageBySlug(ctx, kbID, slug) (*WikiPage, error)
    ListPages(ctx, req) (*WikiPageListResponse, error)
    RebuildLinks(ctx, kbID) error
}

// Wiki Repository
interface WikiPageRepository {
    Create(ctx, page) error
    Update(ctx, page) error
    GetBySlug(ctx, kbID, slug) (*WikiPage, error)
    ListAll(ctx, kbID) ([]*WikiPage, error)
}
```

---

## SUMMARY

| Item | Value |
|------|-------|
| **Total KB Types** | 3 (document, faq, wiki) |
| **Wiki Type Status** | Feature on document KB, not separate type |
| **Enable Wiki** | WikiConfig.Enabled = true |
| **Auto-Gen Pages** | WikiConfig.AutoIngest = true + LLM model |
| **Page Types** | 7 (summary, entity, concept, index, log, synthesis, comparison) |
| **Indices** | 2 (chunks + wiki pages) |
| **Search Tools** | knowledge_search (chunks), wiki_read_page (pages) |
| **Settings Update** | PUT /api/v1/knowledge-bases/{id} |
| **Async Task** | TypeWikiIngest via Asynq |
| **Rate Limit** | MaxPagesPerIngest per batch |
| **Storage Config** | KB → WikiConfig JSON → database |

