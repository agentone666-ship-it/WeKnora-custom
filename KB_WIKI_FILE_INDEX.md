# Knowledge Base & Wiki System - File Index & Cross-Reference

## COMPLETE FILE LISTING BY COMPONENT

### 1. DATA MODELS (internal/types/)

| File | Purpose | Key Types |
|------|---------|-----------|
| `knowledgebase.go` | KB data model | `KnowledgeBase`, `WikiConfig`, `ChunkingConfig`, `VLMConfig`, `ASRConfig`, `ExtractConfig`, `FAQConfig` |
| `knowledge.go` | Document/knowledge model | `Knowledge`, `ManualKnowledgeMetadata`, `Knowledge.Type` (manual, faq) |
| `wiki_page.go` | Wiki page model | `WikiPage`, `WikiPageIssue`, `WikiConfig`, `WikiStats`, `WikiGraphData` |
| `agent.go` | Agent configuration | `AgentConfig`, `SessionAgentConfig` (includes KnowledgeBases) |
| `chunk.go` | Chunk model | Indexed content units (not detailed in search but referenced) |
| `message.go` | Chat messages | Not detailed - used by sessions |
| `tag.go` | Tags/categories | For knowledge organization |

### 2. INTERFACES (internal/types/interfaces/)

| File | Purpose | Contracts |
|------|---------|-----------|
| `knowledgebase.go` | KB service interface | `KnowledgeBaseService`, `KnowledgeBaseRepository` |
| `knowledge.go` | Knowledge service interface | `KnowledgeService`, `KnowledgeRepository` |
| `wiki_page.go` | Wiki service interface | `WikiPageService`, `WikiPageRepository` |
| `agent.go` | Agent tool interface | Generic tool registration |
| `chunk.go` | Chunk access interface | `ChunkRepository`, `ChunkService` |

### 3. SERVICES (internal/application/service/)

| File | Purpose | Methods |
|------|---------|---------|
| `knowledgebase.go` | KB business logic | Create, Update, Get, List, HybridSearch |
| `knowledge.go` | Doc ingest pipeline | CreateKnowledge, UpdateKnowledge, Delete, Search |
| `wiki_ingest.go` | Wiki LLM generation | WikiIngestService, ProcessWikiIngest, ScheduleFollowUp |
| `wiki_ingest_batch.go` | Wiki batch processing | Batch LLM calls, link rebuilding |
| `wiki_page.go` | Wiki CRUD operations | CreatePage, UpdatePage, GetPageBySlug, ListPages, RebuildLinks |
| `wiki_linkify.go` | Wiki link parsing | Parse [[wiki-links]] syntax |
| `wiki_lint.go` | Wiki quality checks | Validate, find broken links, orphaned pages |
| `knowledge_post_process.go` | Post-processing docs | Summary generation, extraction |
| `chat_pipeline/search.go` | Retrieval in chat | Knowledge search during chat |
| `chat_pipeline/wiki_boost.go` | Wiki integration in chat | Boost wiki results in chat context |
| `chunk.go` | Chunk service | Chunk CRUD, retrieval |
| `retriever/keywords_vector_hybrid_indexer.go` | Hybrid search engine | Index, BatchIndex, Retrieve |

### 4. REPOSITORIES (internal/application/repository/)

| File | Purpose | Storage |
|------|---------|---------|
| `knowledgebase.go` | KB persistence | GORM: knowledge_bases table |
| `knowledge.go` | Knowledge persistence | GORM: knowledges table |
| `wiki_page.go` | Wiki persistence | GORM: wiki_pages, wiki_page_issues |
| `chunk.go` | Chunk persistence | GORM: chunks table |
| `chunk_sqlite_test.go` | SQLite chunk backend | For testing |
| `retriever/weaviate/repository.go` | Weaviate backend | Vector search |
| `retriever/elasticsearch/v7/repository.go` | Elasticsearch v7 | Vector + keyword |
| `retriever/elasticsearch/v8/repository.go` | Elasticsearch v8 | Vector + keyword |
| `retriever/qdrant/repository.go` | Qdrant backend | Vector search |
| `retriever/milvus/repository.go` | Milvus backend | Vector search |
| `retriever/postgres/repository.go` | PostgreSQL backend | Vector search with pgvector |
| `retriever/sqlite/repository.go` | SQLite backend | Local vector search |
| `tag.go` | Tag persistence | GORM: tags table |

### 5. HTTP HANDLERS (internal/handler/)

| File | Purpose | Endpoints |
|------|---------|-----------|
| `knowledgebase.go` | KB HTTP API | POST/PUT/GET/DELETE /api/v1/knowledge-bases |
| `wiki_page.go` | Wiki HTTP API | GET/POST/PUT/DELETE /api/v1/knowledgebase/{id}/wiki/* |
| `knowledge.go` | Knowledge HTTP API | File upload, URL add, manual create |
| `chunk.go` | Chunk HTTP API | Chunk retrieval, preview |
| `session/qa.go` | Session chat QA | Knowledge search in chat context |

### 6. ROUTING (internal/router/)

| File | Purpose | Route Registration |
|------|---------|-----------------|
| `router.go` | Main router | RegisterWikiPageRoutes (~line 839) |
| `task.go` | Task routing | Async task registration |

### 7. AGENT TOOLS (internal/agent/tools/)

| File | Purpose | Tool Name |
|------|---------|-----------|
| `wiki_write_page.go` | Agent creates page | `wiki_write_page` |
| `wiki_read_source_doc.go` | Agent reads original doc | `wiki_read_source_doc` |
| `wiki_tools.go` | Tool registration | WikiToolRegistry |
| `wiki_delete_page.go` | Agent deletes page | `wiki_delete_page` |
| `wiki_replace_text.go` | Agent edits page | `wiki_replace_text` |
| `wiki_rename_page.go` | Agent renames page | `wiki_rename_page` |
| `wiki_update_issue.go` | Agent resolves issue | `wiki_update_issue` |
| `wiki_read_issue.go` | Agent checks issue | `wiki_read_issue` |
| `wiki_flag_issue.go` | Agent flags problem | `wiki_flag_issue` |
| `knowledge_search.go` | Agent searches KB | `knowledge_search` |
| `grep_chunks.go` | Agent regex search | `grep_chunks` |
| `list_knowledge_chunks.go` | Agent lists docs | `list_knowledge_chunks` |
| `get_document_info.go` | Agent gets doc info | `get_document_info` |
| `web_search.go` | Agent web search | `web_search` |
| `definitions.go` | Tool definitions | Tool registry |

### 8. AGENT CONFIG (internal/agent/)

| File | Purpose | Content |
|------|---------|---------|
| `prompts_wiki.go` | Wiki-specific prompts | Agent instructions for wiki tools |
| `prompts.go` | General prompts | System prompt templates |
| `engine.go` | Agent loop | ReAct execution engine |
| `observe.go` | Agent observation | Tool result interpretation |

### 9. ASYNC TASKS (internal/types/ & services)

| Task Type | File | Handler | Payload |
|-----------|------|---------|---------|
| `TypeWikiIngest` | wiki_ingest.go | ProcessWikiIngest | WikiIngestPayload |
| (Knowledge ingest) | knowledge.go | Various | KnowledgePayload |

### 10. FRONTEND (frontend/src/)

| File | Purpose | Exports |
|------|---------|---------|
| `api/knowledge-base/index.ts` | KB API client | createKnowledgeBase, updateKnowledgeBase, getKnowledgeBaseById, etc. |
| `api/wiki/index.ts` | Wiki API client | listWikiPages, getWikiPage, updateWikiPage, etc. |
| `stores/knowledge.ts` | State management | Knowledge store (Pinia) |
| `stores/settings.ts` | Settings store | App settings |
| `hooks/useKnowledgeBase.ts` | KB hook | KB operations hook |
| `hooks/useKnowledgeBaseCreationNavigation.ts` | KB creation | Navigation after KB creation |

---

## KEY DATA FLOWS BY FILE

### Document Ingestion Flow
```
1. frontend/src/api/knowledge-base/index.ts
   → uploadKnowledgeFile(kbId, file)

2. internal/handler/knowledge.go
   → KnowledgeHandler.CreateKnowledge()

3. internal/application/service/knowledge.go
   → KnowledgeService.CreateKnowledge()
   → Creates Knowledge entity (ParseStatus=pending)
   → Enqueues ingest task

4. Async task processed:
   → Chunks created (chunks table)
   → Embeddings indexed (vector DB: Weaviate, Elasticsearch, etc.)
   → IF WikiConfig.AutoIngest:
      → internal/application/service/wiki_ingest.go
      → Enqueue WikiIngest task (30s delay)

5. Wiki ingest task:
   → internal/application/service/wiki_ingest_batch.go
   → ProcessWikiIngest()
   → LLM generates pages
   → internal/application/service/wiki_page.go
   → CreatePage() for each generated wiki page
   → internal/application/repository/wiki_page.go
   → Save to wiki_pages table

6. Link maintenance:
   → internal/application/service/wiki_linkify.go
   → Parse [[wiki-links]]
   → internal/application/service/wiki_page.go
   → RebuildLinks() updates InLinks/OutLinks
```

### Settings Update Flow
```
1. frontend/src/api/knowledge-base/index.ts
   → updateKnowledgeBase(id, config)

2. internal/handler/knowledgebase.go
   → KnowledgeBaseHandler.UpdateKnowledgeBase()
   → Validates config

3. internal/application/service/knowledgebase.go
   → KnowledgeBaseService.UpdateKnowledgeBase()
   → Merges config (including wiki_config)
   → Calls EnsureDefaults()

4. internal/application/repository/knowledgebase.go
   → KnowledgeBaseRepository.UpdateKnowledgeBase()
   → UPDATE knowledge_bases SET wiki_config = JSON_VALUE
```

### Agent Using Wiki
```
1. Session starts:
   → internal/handler/session/handler.go
   → Load session with agent_config

2. Agent receives query:
   → internal/types/agent.go
   → AgentConfig.KnowledgeBases → resolved to KB objects
   → For each wiki-enabled KB:
      → Add wiki tools to available tools

3. Agent calls wiki_read_page:
   → internal/agent/tools/wiki_tools.go
   → Call WikiPageService.GetPageBySlug()
   → internal/application/service/wiki_page.go
   → internal/application/repository/wiki_page.go
   → SELECT FROM wiki_pages WHERE knowledge_base_id = ? AND slug = ?

4. Agent calls wiki_write_page:
   → internal/agent/tools/wiki_write_page.go
   → Call WikiPageService.CreatePage()
   → Same flow as #5-6 in document ingestion
```

---

## DIRECT FILE RELATIONSHIPS

### KnowledgeBase ↔ WikiConfig
```
internal/types/knowledgebase.go::KnowledgeBase
    └─ WikiConfig *types.WikiConfig
       └─ Stored in knowledge_bases.wiki_config (JSON)

Update path:
    PUT handler (knowledgebase.go)
    → Service (knowledgebase.go)
    → Repository (knowledgebase.go)
    → Database: UPDATE knowledge_bases SET wiki_config = ?
```

### Knowledge ↔ Chunks
```
internal/types/knowledge.go::Knowledge
    └─ FK: knowledge_base_id

internal/types/chunk.go::Chunk
    └─ FK: knowledge_id → knowledge.id
    └─ Indexed in multiple backends:
        ├─ elasticsearch/v7/repository.go
        ├─ elasticsearch/v8/repository.go
        ├─ weaviate/repository.go
        ├─ qdrant/repository.go
        ├─ milvus/repository.go
        ├─ postgres/repository.go
        └─ sqlite/repository.go
```

### WikiPage ↔ KnowledgeBase
```
internal/types/wiki_page.go::WikiPage
    └─ FK: knowledge_base_id
    └─ Unique constraint: (knowledge_base_id, slug)
    └─ SourceRefs: knowledge IDs that contributed
```

### Agent ↔ KnowledgeBase
```
internal/types/agent.go::AgentConfig
    └─ KnowledgeBases []string (KB IDs)
    └─ Resolved at runtime to KnowledgeBase objects
    └─ Tool selection based on KB.IsWikiEnabled()
```

---

## SERVICE DEPENDENCY INJECTION

### Container (internal/container/container.go)
```
Provides instances of all services:
    - KnowledgeBaseService
    - KnowledgeService
    - WikiPageService
    - WikiLintService
    - Retrieval engines
    - Repositories
    - HTTP handlers
```

### Service Constructor Dependencies

**KnowledgeBaseService:**
```go
repo KnowledgeBaseRepository
kgRepo KnowledgeRepository
chunkRepo ChunkRepository
shareRepo KBShareRepository
kbShareService KBShareService
modelService ModelService
retrieveEngine RetrieveEngineRegistry
tenantRepo TenantRepository
fileSvc FileService
graphEngine RetrieveGraphRepository
asynqClient TaskEnqueuer
```

**WikiPageService:**
```go
repo WikiPageRepository
chunkRepo ChunkRepository
kbService KnowledgeBaseService
redisClient *redis.Client
```

**WikiIngestService:**
```go
wikiService WikiPageService
kbService KnowledgeBaseService
knowledgeSvc KnowledgeService
chunkRepo ChunkRepository
modelService ModelService
task TaskEnqueuer
redisClient *redis.Client
```

---

## CONFIGURATION FILES & ENVIRONMENT

### Backend Config (internal/config/config.go)
```
Reads from:
    - .env file
    - Environment variables
    - YAML config files

Key config sections:
    - Database (PostgreSQL, MySQL, SQLite)
    - Redis (for async tasks)
    - Retrieval engines (Elasticsearch, Weaviate, etc.)
    - LLM models (OpenAI, Claude, local, etc.)
    - Storage (local, S3, Minio, etc.)
```

### Environment Variables (.env)
```
# Database
DATABASE_URL=...

# Redis
REDIS_URL=...

# LLM
LLM_API_KEY=...
LLM_MODEL_ID=...

# Vector DB
WEAVIATE_URL=...
ELASTICSEARCH_URL=...

# Storage
STORAGE_PROVIDER=...
```

---

## BUILD & DEPLOYMENT

### Binary
- `server` - Main Go binary (compiled from cmd/desktop/main.go)
- `WeKnora` - Desktop app version

### Docker
- `Dockerfile` - Backend container
- `frontend/Dockerfile` - Frontend container
- `docker-compose.yml` - Full stack

### Kubernetes/Helm
- `helm/` directory - Helm charts

---

## SUMMARY TABLE

| Component | Count | Key Files |
|-----------|-------|-----------|
| Models | 7+ | types/*.go |
| Services | 10+ | service/*.go |
| Repositories | 10+ | repository/*.go |
| Handlers | 5+ | handler/*.go |
| Agent Tools | 13+ | tools/wiki_*.go, knowledge_*.go |
| Retrieval Backends | 6 | retriever/*/repository.go |
| Frontend APIs | 2 | api/knowledge-base/, api/wiki/ |
| Interfaces | 5+ | types/interfaces/*.go |

---

## QUICK FILE REFERENCE BY QUESTION

**Q: Where is WikiConfig defined?**
A: `internal/types/knowledgebase.go` (line ~94-103)

**Q: Where is wiki auto-ingest logic?**
A: `internal/application/service/wiki_ingest.go` and `wiki_ingest_batch.go`

**Q: How are KB settings persisted?**
A: `internal/handler/knowledgebase.go` → `service/knowledgebase.go` → `repository/knowledgebase.go`

**Q: Where do agents access KBs?**
A: `internal/types/agent.go` (AgentConfig.KnowledgeBases) → resolved in service layer

**Q: Where is wiki page model?**
A: `internal/types/wiki_page.go` (WikiPage struct, ~line 43-84)

**Q: How are wiki links maintained?**
A: `internal/application/service/wiki_linkify.go` and `wiki_page.go`::RebuildLinks

**Q: Where is hybrid retrieval implemented?**
A: `internal/application/service/retriever/keywords_vector_hybrid_indexer.go`

**Q: How does chunk embedding happen?**
A: Multiple backends in `retriever/*/repository.go` (Elasticsearch, Weaviate, etc.)

**Q: Frontend API for KB updates?**
A: `frontend/src/api/knowledge-base/index.ts`::updateKnowledgeBase()

**Q: Where is async task handling?**
A: Asynq queue, triggered in service layer, workers in `wiki_ingest.go`

