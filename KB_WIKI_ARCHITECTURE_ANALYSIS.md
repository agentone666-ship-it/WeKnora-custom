# Knowledge Base System Architecture & Wiki Feature Analysis

## Executive Summary

This document provides a comprehensive overview of the WeKnora knowledge base system architecture, focusing on how Wiki functionality integrates into the knowledge base ecosystem, and how agents connect to and utilize knowledge bases.

---

## 1. KNOWLEDGE BASE DATA MODEL

### 1.1 Core Knowledge Base Entity

**File:** `internal/types/knowledgebase.go`

```go
type KnowledgeBase struct {
    ID                           string                        // UUID primary key
    Name                         string                        // User-friendly name
    Type                         string                        // "document", "faq", or "wiki"
    IsTemporary                  bool                          // Hidden from UI if true
    Description                  string
    TenantID                     uint64                        // Multi-tenant isolation
    
    // Configurations
    ChunkingConfig               ChunkingConfig                // Doc splitting settings
    ImageProcessingConfig        ImageProcessingConfig         // VLM settings
    EmbeddingModelID             string                        // Vector embedding model
    SummaryModelID               string                        // Summarization model
    VLMConfig                    VLMConfig                     // Vision-Language Model
    ASRConfig                    ASRConfig                     // Speech recognition
    StorageProviderConfig        *StorageProviderConfig        // Storage backend (local, minio, s3, etc.)
    ExtractConfig                *ExtractConfig                // Knowledge graph extraction
    FAQConfig                    *FAQConfig                    // FAQ-specific settings
    QuestionGenerationConfig     *QuestionGenerationConfig     // Auto-generate questions
    WikiConfig                   *WikiConfig                   // Wiki-specific settings ← KEY
    
    // Metadata
    IsPinned                     bool
    PinnedAt                     *time.Time
    CreatedAt                    time.Time
    UpdatedAt                    time.Time
    DeletedAt                    gorm.DeletedAt               // Soft delete
    
    // Computed fields (not persisted)
    KnowledgeCount               int64                        // Total documents
    ChunkCount                   int64                        // Total chunks
    IsProcessing                 bool                         // Async tasks running
    ProcessingCount              int64                        // Docs in progress
}

type KnowledgeBaseConfig struct {
    ChunkingConfig               ChunkingConfig
    ImageProcessingConfig        ImageProcessingConfig
    FAQConfig                    *FAQConfig
    WikiConfig                   *WikiConfig
}
```

### 1.2 Knowledge Base Types

```go
const (
    KnowledgeBaseTypeDocument = "document"    // Standard document KB
    KnowledgeBaseTypeFAQ      = "faq"         // FAQ KB
    KnowledgeBaseTypeWiki     = "wiki"        // ← NOT A TYPE! Wiki is a FEATURE
)
```

**CRITICAL:** Wiki is NOT a separate KB type. Instead, it's an add-on feature enabled via `WikiConfig` on document-type KBs via `IsWikiEnabled()` method.

### 1.3 Wiki Configuration

**File:** `internal/types/wiki_page.go`

```go
type WikiConfig struct {
    Enabled               bool      // Activates wiki generation for this KB
    AutoIngest            bool      // Auto-generate wiki pages on document ingestion
    SynthesisModelID      string    // LLM model ID for wiki page generation
    MaxPagesPerIngest     int       // Rate limit: pages created/updated per batch (0 = unlimited)
}

// Check if wiki is enabled
func (kb *KnowledgeBase) IsWikiEnabled() bool {
    return kb != nil && kb.WikiConfig != nil && kb.WikiConfig.Enabled
}
```

### 1.4 Chunking Configuration (Document Processing)

```go
type ChunkingConfig struct {
    ChunkSize              int                  // Document chunk size (default: 512)
    ChunkOverlap           int                  // Overlap between chunks (default: 20%)
    Separators             []string             // Custom separators (e.g., "\n\n", "\n")
    EnableMultimodal       bool                 // Deprecated: use VLMConfig instead
    ParserEngineRules      []ParserEngineRule   // Map file types to parser engines
    EnableParentChild      bool                 // Two-level parent-child chunking
    ParentChunkSize        int                  // Large context chunks (default: 4096)
    ChildChunkSize         int                  // Small embedding chunks (default: 384)
}

type ParserEngineRule struct {
    FileTypes []string    // e.g., ["pdf", "docx"]
    Engine    string      // e.g., "builtin", "ocr", "ml"
}
```

---

## 2. KNOWLEDGE ENTITY

### 2.1 Knowledge (Document) Data Model

**File:** `internal/types/knowledge.go`

```go
type Knowledge struct {
    ID                       string                // UUID
    TenantID                 uint64
    KnowledgeBaseID          string                // FK to KnowledgeBase
    TagID                    string                // Optional categorization
    Type                     string                // "manual" or content type
    Title                    string
    Description              string
    Source                   string                // URL or "manual"
    Channel                  string                // How it was ingested (web, api, browser_extension, etc.)
    
    // Processing pipeline
    ParseStatus              string                // pending, processing, completed, failed, deleting
    SummaryStatus            string                // none, pending, processing, completed, failed
    EnableStatus             string
    
    // File metadata
    FileName                 string
    FileType                 string
    FileSize                 int64
    FileHash                 string
    FilePath                 string
    StorageSize              int64
    
    // Content and metadata
    Metadata                 JSON                  // Manual/FAQ metadata
    LastFAQImportResult      JSON                  // FAQ import status
    EmbeddingModelID         string                // Model used for embedding
    
    // Timestamps
    CreatedAt                time.Time
    UpdatedAt                time.Time
    ProcessedAt              *time.Time
    ErrorMessage             string
    DeletedAt                gorm.DeletedAt       // Soft delete
}
```

### 2.2 Parse Status Lifecycle

```
pending → processing → completed (success) or failed
                    → deleting (async deletion in progress)
```

### 2.3 Manual Knowledge (Markdown)

```go
type ManualKnowledgeMetadata struct {
    Content                  string                // Markdown content
    Format                   string                // "markdown"
    Status                   string                // "draft" or "publish"
    Version                  int                   // Increment on update
    UpdatedAt                string                // RFC3339 timestamp
}
```

---

## 3. CHUNK DATA MODEL & INDEXING PIPELINE

### 3.1 Chunk Definition

**File:** `internal/types/chunk.go`

Chunks are the indexed units of content used for retrieval:
- Created from splitting `Knowledge` documents using `ChunkingConfig`
- Each chunk contains: content, metadata, embeddings, keywords
- Support both vector (embedding) and keyword (BM25) indexing

### 3.2 Indexing Types

```go
type RetrieverType string

const (
    VectorRetrieverType       = "vector"        // Semantic search via embeddings
    KeywordRetrieverType      = "keyword"       // BM25 keyword search
)
```

### 3.3 Hybrid Retrieval Engine

**File:** `internal/application/service/retriever/keywords_vector_hybrid_indexer.go`

```go
type KeywordsVectorHybridRetrieveEngineService struct {
    indexRepository           interfaces.RetrieveEngineRepository
    engineType                types.RetrieverEngineType
}

// Supports multiple backends:
// - Elasticsearch v7/v8
// - Weaviate
// - Qdrant
// - Milvus
// - SQLite (local)
// - PostgreSQL
```

### 3.4 Index() vs BatchIndex()

1. **Single Index()** - For individual document ingestion
   - Creates embedding if VectorRetrieverType enabled
   - Saves to repository (chunks + metadata)

2. **BatchIndex()** - For bulk operations
   - Batch embedding with retry logic
   - Concurrent batch saving (max 5 concurrent)
   - Chunk size: 40 items per batch

---

## 4. WIKI PAGE DATA MODEL

### 4.1 Wiki Page Entity

**File:** `internal/types/wiki_page.go`

```go
type WikiPage struct {
    ID                       string                // UUID
    TenantID                 uint64
    KnowledgeBaseID          string                // FK to KnowledgeBase
    Slug                     string                // URL-friendly (e.g., "entity/acme-corp")
    Title                    string                // Human-readable name
    PageType                 string                // summary, entity, concept, index, log, synthesis, comparison
    Status                   string                // draft, published, archived
    
    // Content
    Content                  string                // Full markdown
    Summary                  string                // One-line summary
    
    // References and links
    Aliases                  StringArray           // Alt names, acronyms (JSON)
    SourceRefs               StringArray           // Knowledge IDs that contributed (JSON)
    InLinks                  StringArray           // Pages linking TO this ([[slug]])
    OutLinks                 StringArray           // Pages this links to (JSON)
    
    // Metadata and versioning
    PageMetadata             JSON                  // Custom metadata (JSON)
    Version                  int                   // Increment on content change
    
    // Timestamps
    CreatedAt                time.Time
    UpdatedAt                time.Time
    DeletedAt                gorm.DeletedAt       // Soft delete
}

// Unique constraint: (KnowledgeBaseID, Slug)
```

### 4.2 Wiki Page Types

```go
const (
    WikiPageTypeSummary      = "summary"          // Auto-created from document
    WikiPageTypeEntity       = "entity"           // Auto-created (person, org, place)
    WikiPageTypeConcept      = "concept"          // Auto-created (topic, idea)
    WikiPageTypeIndex        = "index"            // Auto-created: table of contents
    WikiPageTypeLog          = "log"              // Auto-created: operation log
    WikiPageTypeSynthesis    = "synthesis"        // Agent-created: cross-doc analysis
    WikiPageTypeComparison   = "comparison"       // Agent-created: entity/concept comparison
)
```

### 4.3 Wiki Page Status

```go
const (
    WikiPageStatusDraft      = "draft"            // In progress
    WikiPageStatusPublished  = "published"        // Visible
    WikiPageStatusArchived   = "archived"         // Hidden but preserved
)
```

### 4.4 Wiki Page Issue Tracking

```go
type WikiPageIssue struct {
    ID                       string
    TenantID                 uint64
    KnowledgeBaseID          string
    Slug                     string                // Which page has the issue
    IssueType                string                // Type of issue (e.g., "missing_context")
    Description              string
    SuspectedKnowledgeIDs    StringArray           // Knowledge items to fix
    Status                   string                // pending, resolved, ignored
    ReportedBy               string                // Agent or user name
    CreatedAt                time.Time
    UpdatedAt                time.Time
}
```

### 4.5 Wiki Stats & Graph

```go
type WikiStats struct {
    TotalPages               int64
    PagesByType              map[string]int64
    TotalLinks               int64
    OrphanCount              int64                 // Pages with no inbound links
    RecentUpdates            []*WikiPage
    PendingTasks             int64                 // Docs waiting to ingest
    PendingIssues            int64
    IsActive                 bool                  // Wiki ingestion running
}

type WikiGraphData struct {
    Nodes                    []WikiGraphNode
    Edges                    []WikiGraphEdge
}
```

---

## 5. WIKI GENERATION & INGESTION PIPELINE

### 5.1 Wiki Ingest Service

**File:** `internal/application/service/wiki_ingest.go`

```go
type wikiIngestService struct {
    wikiService              interfaces.WikiPageService
    kbService                interfaces.KnowledgeBaseService
    knowledgeSvc             interfaces.KnowledgeService
    chunkRepo                interfaces.ChunkRepository
    modelService             interfaces.ModelService
    task                     interfaces.TaskEnqueuer
    redisClient              *redis.Client         // nil in Lite mode
}
```

### 5.2 Wiki Ingest Flow

1. **Document Ingestion Trigger**
   - User uploads document to KB with `WikiConfig.AutoIngest = true`
   - Document → Knowledge entity → Chunks (normal indexing)
   - Wiki ingest task enqueued (delayed 30 seconds to debounce)

2. **Wiki Ingest Task** (Async via Asynq)
   - Redis-backed pending queue: `wiki:pending:{kbID}`
   - Concurrent batch prevention: `wiki:active:{kbID}` lock (5 min TTL)
   - Max docs per batch: 5 (configurable via `MaxPagesPerIngest`)

3. **Page Generation** (LLM-powered)
   - For each pending document:
     - Fetch chunks (max 32KB content)
     - Call LLM (model from `WikiConfig.SynthesisModelID`)
     - Generate/update wiki pages (summary, entities, concepts)
     - Create cross-links between pages
   - Update `WikiPage` with:
     - Auto-generated content
     - SourceRefs → Knowledge IDs
     - OutLinks → Wiki links

4. **Link Maintenance**
   - Parse `[[wiki-link]]` syntax in content
   - Update bidirectional links (InLinks/OutLinks)
   - Rebuild index page with all pages
   - Maintain link graph for visualization

### 5.3 Wiki Ingest Payloads

**Asynq Task Type:** `types.TypeWikiIngest`

```go
type WikiIngestPayload struct {
    TenantID                 uint64
    KnowledgeBaseID          string
    Language                 string                // Optional language hint
    LiteOps                  []WikiPendingOp       // Fallback for Lite mode
}

type WikiPendingOp struct {
    Op                       string                // "ingest" or "retract"
    KnowledgeID              string
    Language                 string                // For ingest
    DocTitle                 string                // For retract
    DocSummary               string
    PageSlugs                []string              // For retract
}
```

### 5.4 Wiki Retraction (Document Deletion)

**Payload:** `WikiRetractPayload`

When a Knowledge document is deleted:
- Find all Wiki pages that reference it (SourceRefs)
- Update or archive affected pages
- Maintain referential integrity

### 5.5 Redis Keys Used

```
wiki:pending:{kbID}     → Redis List of pending WikiPendingOp (JSON)
wiki:active:{kbID}      → Redis String "1" with 5 min TTL (lock)
```

In Lite mode (no Redis): operations stored in task payload `LiteOps`

---

## 6. KNOWLEDGE BASE → AGENT CONNECTIONS

### 6.1 Agent Configuration

**File:** `internal/types/agent.go`

```go
type AgentConfig struct {
    // Knowledge access
    KnowledgeBases           []string              // Accessible KB IDs
    KnowledgeIDs             []string              // Specific document IDs
    RetrieveKBOnlyWhenMentioned bool               // Only search KB when user mentions @
    RetainRetrievalHistory   bool                  // Keep wiki_read_page results across turns
    
    // Tool control
    AllowedTools             []string              // Whitelist of tool names
    MaxIterations            int
    Temperature              float64
    
    // System prompt
    SystemPrompt             string                // Unified prompt template
    UseCustomSystemPrompt    bool
    
    // Web search
    WebSearchEnabled         bool
    WebSearchMaxResults      int
    
    // Multi-turn
    MultiTurnEnabled         bool
    HistoryTurns             int                   // How many turns to keep in context
    
    // Context budget
    MaxContextTokens         int                   // Default: 200k
    
    // Other configs...
    MCPSelectionMode         string
    SkillsEnabled            bool
    // ... many more fields
}

// Session-level override
type SessionAgentConfig struct {
    AgentModeEnabled         bool
    WebSearchEnabled         bool
    KnowledgeBases           []string
    KnowledgeIDs             []string
}
```

### 6.2 Knowledge Base Resolution

When agent runs:
1. Load AgentConfig.KnowledgeBases (KB IDs)
2. Resolve full KnowledgeBase objects
3. For wiki-enabled KBs:
   - Build search targets for wiki pages
   - Agent can use `wiki_read_page`, `wiki_write_page` tools
4. For document KBs:
   - Build search targets for chunks
   - Agent can use `knowledge_search` tools

### 6.3 Agent Tool: Wiki Tools

**Files:** `internal/agent/tools/wiki_*.go`

Available wiki tools:
- `wiki_read_page` - Retrieve wiki page by slug
- `wiki_write_page` - Create/update wiki page (synthesis, comparison)
- `wiki_rename_page` - Rename page slug
- `wiki_delete_page` - Archive page
- `wiki_replace_text` - Find/replace in page content
- `wiki_read_source_doc` - Get original Knowledge document
- `wiki_read_issue` - Check page issues
- `wiki_flag_issue` - Report problem with page
- `wiki_update_issue` - Resolve issue

### 6.4 Agent Tool: Knowledge Tools

**Files:** `internal/agent/tools/knowledge_*.go`, `internal/agent/tools/grep_chunks.go`

- `knowledge_search` - Semantic + keyword search in KB
- `grep_chunks` - Regex search in chunks
- `list_knowledge_chunks` - List documents in KB
- `get_document_info` - Fetch document metadata

---

## 7. KNOWLEDGE BASE SETTINGS PAGE ARCHITECTURE

### 7.1 Frontend API Layer

**File:** `frontend/src/api/knowledge-base/index.ts`

Key endpoints for KB settings:

```typescript
// Create KB with wiki config
createKnowledgeBase(data: {
    name: string;
    description?: string;
    type?: 'document' | 'faq';
    chunking_config?: ChunkingConfig;
    embedding_model_id?: string;
    wiki_config?: {
        enabled: boolean;
        auto_ingest?: boolean;
        synthesis_model_id?: string;
        wiki_language?: string;
        max_pages_per_ingest?: number;
    };
    vlm_config?: { enabled: boolean; model_id?: string };
    asr_config?: { enabled: boolean; model_id?: string };
    extract_config?: any;
    faq_config?: FAQConfig;
})

// Update KB with config
updateKnowledgeBase(id: string, data: {
    name: string;
    description?: string;
    config?: {
        chunking_config?: ChunkingConfig;
        image_processing_config?: ImageProcessingConfig;
        faq_config?: FAQConfig;
        wiki_config?: WikiConfig;
    };
})
```

### 7.2 Backend Handler

**File:** `internal/handler/knowledgebase.go`

- `CreateKnowledgeBase` - POST /api/v1/knowledge-bases
- `UpdateKnowledgeBase` - PUT /api/v1/knowledge-bases/{id}
- Validates ExtractConfig, all sub-configs
- Calls `KnowledgeBaseService` for business logic

### 7.3 Service Layer

**File:** `internal/application/service/knowledgebase.go`

```go
type knowledgeBaseService struct {
    repo                     interfaces.KnowledgeBaseRepository
    kgRepo                   interfaces.KnowledgeRepository
    chunkRepo                interfaces.ChunkRepository
    shareRepo                interfaces.KBShareRepository
    modelService             interfaces.ModelService
    retrieveEngine           interfaces.RetrieveEngineRegistry
    // ...
}

// CreateKnowledgeBase:
// - Generate UUID
// - Call EnsureDefaults() on KB
// - Call repo.CreateKnowledgeBase()
// - Return created KB

// UpdateKnowledgeBase:
// - Fetch existing KB
// - Update fields
// - Validate config
// - Call repo.UpdateKnowledgeBase()
```

### 7.4 Wiki Settings UI Integration

Frontend components receive WikiConfig as part of KnowledgeBase object:

```typescript
interface KnowledgeBase {
    id: string;
    name: string;
    type: 'document' | 'faq' | 'wiki';
    wiki_config?: {
        enabled: boolean;
        auto_ingest: boolean;
        synthesis_model_id: string;
        max_pages_per_ingest: number;
    };
    // ... other fields
}
```

Settings page form sections:
1. **Basic Info** - name, description
2. **Document Processing** - chunking_config, embedding_model_id
3. **Advanced Features:**
   - Image Processing (VLM)
   - Speech Recognition (ASR)
   - Knowledge Graph Extraction
   - **Wiki** (if document type):
     - Enable wiki
     - Auto-ingest toggle
     - Synthesis model selection
     - Max pages per ingest limit

---

## 8. DATABASE SCHEMA MAPPING

### 8.1 Tables

1. **knowledge_bases** (KnowledgeBase)
   - Primary fields: id, name, type, tenant_id
   - Configs (JSON): chunking_config, wiki_config, faq_config, extract_config, etc.
   - Timestamps: created_at, updated_at, deleted_at
   - Indicators: is_pinned, is_temporary, is_processing

2. **knowledges** (Knowledge)
   - FK: knowledge_base_id → knowledge_bases.id
   - Parse status tracking: parse_status, summary_status
   - File metadata: file_name, file_type, file_size, file_hash, file_path
   - Metadata: metadata (JSON), last_faq_import_result (JSON)

3. **chunks** (Chunk)
   - FK: knowledge_id → knowledges.id
   - Content and embeddings: content, embedding (vector)
   - Metadata: metadata (JSON)
   - Indexing: for vector and keyword retrieval

4. **wiki_pages** (WikiPage)
   - FK: knowledge_base_id → knowledge_bases.id
   - Unique: (knowledge_base_id, slug)
   - Content: content (text), summary, title
   - Links: in_links (JSON array), out_links (JSON array)
   - Versioning: version (auto-increment on content change)

5. **wiki_page_issues** (WikiPageIssue)
   - FK: knowledge_base_id, slug
   - Issue tracking: issue_type, status, description

### 8.2 Foreign Key Relationships

```
agents.agent_config.knowledge_bases[] → knowledge_bases.id
agents.session_agent_config.knowledge_bases[] → knowledge_bases.id

knowledge_bases (wiki_config) ← metadata JSON

knowledges.knowledge_base_id → knowledge_bases.id
chunks.knowledge_id → knowledges.id

wiki_pages.knowledge_base_id → knowledge_bases.id
wiki_page_issues.knowledge_base_id → knowledge_bases.id
```

---

## 9. SERVICE LAYER INTERFACES

### 9.1 Key Interfaces

**File:** `internal/types/interfaces/knowledgebase.go`

```go
type KnowledgeBaseService interface {
    CreateKnowledgeBase(ctx, kb) (*KB, error)
    GetKnowledgeBaseByID(ctx, id) (*KB, error)
    UpdateKnowledgeBase(ctx, id, name, desc, config) (*KB, error)
    ListKnowledgeBases(ctx) ([]*KB, error)
    HybridSearch(ctx, id, params) (results, error)
    // ... more methods
}

type KnowledgeBaseRepository interface {
    Create(ctx, kb) error
    GetByID(ctx, id) (*KB, error)
    Update(ctx, kb) error
    List(ctx, tenantID) ([]*KB, error)
    // ... more methods
}
```

**File:** `internal/types/interfaces/wiki_page.go`

```go
type WikiPageService interface {
    CreatePage(ctx, page) (*WikiPage, error)
    UpdatePage(ctx, page) (*WikiPage, error)
    GetPageBySlug(ctx, kbID, slug) (*WikiPage, error)
    ListPages(ctx, req) (*WikiPageListResponse, error)
    GetStats(ctx, kbID) (*WikiStats, error)
    RebuildLinks(ctx, kbID) error
    SearchPages(ctx, kbID, query) ([]*WikiPage, error)
    CreateIssue(ctx, issue) (*WikiPageIssue, error)
    // ... more methods
}

type WikiPageRepository interface {
    Create(ctx, page) error
    Update(ctx, page) error
    GetByID(ctx, id) (*WikiPage, error)
    GetBySlug(ctx, kbID, slug) (*WikiPage, error)
    List(ctx, req) ([]*WikiPage, int64, error)
    ListAll(ctx, kbID) ([]*WikiPage, error)
    // ... more methods
}
```

---

## 10. API ENDPOINTS

### 10.1 Knowledge Base Endpoints

```
GET    /api/v1/knowledge-bases                 - List KBs
POST   /api/v1/knowledge-bases                 - Create KB
GET    /api/v1/knowledge-bases/{id}            - Get KB details
PUT    /api/v1/knowledge-bases/{id}            - Update KB (including wiki_config)
DELETE /api/v1/knowledge-bases/{id}            - Delete KB
PUT    /api/v1/knowledge-bases/{id}/pin        - Pin/unpin KB
```

### 10.2 Wiki Endpoints

```
GET    /api/v1/knowledgebase/{kb_id}/wiki/pages              - List pages
POST   /api/v1/knowledgebase/{kb_id}/wiki/pages              - Create page
GET    /api/v1/knowledgebase/{kb_id}/wiki/pages/*slug        - Get page by slug
PUT    /api/v1/knowledgebase/{kb_id}/wiki/pages/*slug        - Update page
DELETE /api/v1/knowledgebase/{kb_id}/wiki/pages/*slug        - Delete page
GET    /api/v1/knowledgebase/{kb_id}/wiki/index              - Get index page
GET    /api/v1/knowledgebase/{kb_id}/wiki/log                - Get log page
GET    /api/v1/knowledgebase/{kb_id}/wiki/graph              - Get link graph
GET    /api/v1/knowledgebase/{kb_id}/wiki/stats              - Get wiki stats
GET    /api/v1/knowledgebase/{kb_id}/wiki/search             - Search pages
POST   /api/v1/knowledgebase/{kb_id}/wiki/rebuild-links      - Rebuild links
GET    /api/v1/knowledgebase/{kb_id}/wiki/lint               - Lint pages
POST   /api/v1/knowledgebase/{kb_id}/wiki/auto-fix           - Auto-fix issues
GET    /api/v1/knowledgebase/{kb_id}/wiki/issues             - List issues
PUT    /api/v1/knowledgebase/{kb_id}/wiki/issues/{id}/status - Update issue
```

### 10.3 Knowledge Endpoints

```
GET    /api/v1/knowledge-bases/{kb_id}/knowledge                 - List knowledge
POST   /api/v1/knowledge-bases/{kb_id}/knowledge/file            - Upload file
POST   /api/v1/knowledge-bases/{kb_id}/knowledge/url             - Add from URL
POST   /api/v1/knowledge-bases/{kb_id}/knowledge/manual          - Create manual
GET    /api/v1/knowledge/{id}                                    - Get knowledge
PUT    /api/v1/knowledge/manual/{id}                             - Update manual
POST   /api/v1/knowledge/{id}/reparse                            - Reparse document
DELETE /api/v1/knowledge/{id}                                    - Delete knowledge
GET    /api/v1/chunks/{id}?page=X&page_size=Y                    - Get chunks
GET    /api/v1/chunks/by-id/{chunk_id}                           - Get chunk by ID
POST   /api/v1/knowledge-search                                  - Semantic search
```

---

## 11. FLOW DIAGRAMS

### 11.1 Document Ingestion → Wiki Generation

```
User uploads document
    ↓
POST /api/v1/knowledge-bases/{kb_id}/knowledge/file
    ↓
KnowledgeService.CreateKnowledge()
    ├─→ Create Knowledge entity (ParseStatus=pending)
    ├─→ Trigger async ingest task (chunks + embedding)
    └─→ IF WikiConfig.AutoIngest:
        └─→ Enqueue WikiIngest task (delayed 30s for debounce)
            ↓
        Asynq Worker: ProcessWikiIngest()
            ├─→ Acquire wiki:active:{kbID} lock
            ├─→ Fetch pending from wiki:pending:{kbID}
            ├─→ For each doc (up to MaxPagesPerIngest):
            │   ├─→ Fetch chunks (max 32KB content)
            │   ├─→ Call LLM (SynthesisModelID)
            │   ├─→ Generate wiki pages:
            │   │   ├─→ Summary page
            │   │   ├─→ Entity pages
            │   │   └─→ Concept pages
            │   └─→ Update wiki_pages + links
            ├─→ RebuildLinks: rebuild in/out links
            ├─→ RebuildIndexPage
            └─→ IF more pending: schedule follow-up task
```

### 11.2 Agent Interacting with KB + Wiki

```
User message in agent chat
    ↓
Agent receives accessible KnowledgeBases (from AgentConfig)
    ↓
Agent decides to search KB
    ├─→ Tool: knowledge_search
    │   └─→ HybridRetrievalEngine.Retrieve()
    │       └─→ Returns chunks (vector + keyword match)
    │
    └─→ Tool: wiki_read_page
        └─→ WikiPageService.GetPageBySlug()
            └─→ Returns wiki page content + links

Agent can also:
    ├─→ Tool: wiki_write_page (create synthesis/comparison)
    │   └─→ WikiPageService.CreatePage()
    │
    └─→ Tool: wiki_read_source_doc
        └─→ Get original Knowledge document
```

### 11.3 KB Settings Update Flow

```
User opens KB Settings page
    ↓
GET /api/v1/knowledge-bases/{kb_id}
    ↓
Frontend receives KnowledgeBase with wiki_config
    ↓
User edits wiki_config:
    ├─→ Toggle enabled
    ├─→ Toggle auto_ingest
    ├─→ Select synthesis_model_id
    └─→ Set max_pages_per_ingest
    ↓
User clicks Save
    ↓
PUT /api/v1/knowledge-bases/{kb_id}
    │ payload:
    │ {
    │   name: "...",
    │   description: "...",
    │   config: {
    │     wiki_config: {
    │       enabled: true,
    │       auto_ingest: true,
    │       synthesis_model_id: "model-123",
    │       max_pages_per_ingest: 5
    │     }
    │   }
    │ }
    ↓
KnowledgeBaseHandler.UpdateKnowledgeBase()
    ↓
KnowledgeBaseService.UpdateKnowledgeBase()
    ├─→ Fetch existing KB
    ├─→ Merge config
    ├─→ Call EnsureDefaults()
    ├─→ Validate config
    └─→ Repository.UpdateKnowledgeBase()
        └─→ UPDATE knowledge_bases SET ... WHERE id = ?
            ↓
            wiki_config column updated with new JSON
```

---

## 12. KEY FILE LOCATIONS REFERENCE

### Backend (Go)

| Component | File(s) |
|-----------|---------|
| KB Model | `internal/types/knowledgebase.go` |
| Knowledge Model | `internal/types/knowledge.go` |
| Wiki Page Model | `internal/types/wiki_page.go` |
| Agent Config | `internal/types/agent.go` |
| KB Service | `internal/application/service/knowledgebase.go` |
| Knowledge Service | `internal/application/service/knowledge.go` |
| Wiki Ingest Service | `internal/application/service/wiki_ingest.go`, `wiki_ingest_batch.go` |
| Wiki Page Service | `internal/application/service/wiki_page.go` |
| KB Handler | `internal/handler/knowledgebase.go` |
| Wiki Handler | `internal/handler/wiki_page.go` |
| KB Repository | `internal/application/repository/knowledgebase.go` |
| Wiki Repository | `internal/application/repository/wiki_page.go` |
| KB Interfaces | `internal/types/interfaces/knowledgebase.go` |
| Wiki Interfaces | `internal/types/interfaces/wiki_page.go` |
| Hybrid Indexer | `internal/application/service/retriever/keywords_vector_hybrid_indexer.go` |
| Wiki Tools | `internal/agent/tools/wiki_*.go` |
| Router | `internal/router/router.go` (RegisterWikiPageRoutes ~L839) |

### Frontend (TypeScript/Vue)

| Component | File |
|-----------|------|
| KB API | `frontend/src/api/knowledge-base/index.ts` |
| Wiki API | `frontend/src/api/wiki/index.ts` |
| Knowledge Store | `frontend/src/stores/knowledge.ts` |
| KB Hooks | `frontend/src/hooks/useKnowledgeBase.ts` |

---

## 13. CONFIGURATION HIERARCHY

```
Tenant Level
├─→ Storage Engine Config (credentials)
├─→ Embedding Models
├─→ LLM Models
├─→ VLM Models
└─→ Agent Config (default)

Knowledge Base Level
├─→ Type (document, faq)
├─→ Chunking Config
├─→ Embedding Model ID (override tenant)
├─→ Image Processing Config
├─→ VLM Config
├─→ ASR Config
├─→ Extract Config
├─→ FAQ Config (if type=faq)
├─→ Question Generation Config
└─→ Wiki Config (if enabled on document KB)
    ├─→ Enabled (boolean)
    ├─→ AutoIngest (boolean)
    ├─→ SynthesisModelID (LLM for generation)
    └─→ MaxPagesPerIngest (rate limit)

Session Level
└─→ Session Agent Config
    ├─→ Enabled (override)
    ├─→ KnowledgeBases (override)
    └─→ WebSearchEnabled (override)
```

---

## 14. CRITICAL INTEGRATION POINTS

### 14.1 Wiki as Feature, Not Type

- KB Type is "document" (documents can have wiki)
- Wiki is enabled via `WikiConfig.Enabled = true`
- Check with `kb.IsWikiEnabled()` method
- **Common Mistake:** Treating wiki as a separate KB type

### 14.2 Multi-Index Pipeline

Each Knowledge document creates two separate indices:
1. **Chunk Index** - for document retrieval
   - Vector embeddings
   - Keyword (BM25)
   - Used by `knowledge_search` tool

2. **Wiki Index** - for wiki retrieval  
   - Wiki pages stored in `wiki_pages` table
   - Full-text search on wiki content
   - Link graph stored in InLinks/OutLinks
   - Used by `wiki_read_page` tool

### 14.3 Agent Knowledge Resolution

Agent receives KBIDs → Service resolves:
- KB type → document/faq/wiki
- For document KB: check WikiConfig
- Build search targets (chunks OR wiki pages)
- Populate SearchTargets used by tools

### 14.4 Async Task Coordination

- Document ingest triggers wiki ingest (asynq)
- Wiki ingest debounced 30s (batches rapid uploads)
- Redis lock prevents concurrent wiki batches
- Max 5 docs per batch to bound execution time
- Follow-up tasks scheduled automatically for remaining docs

---

## 15. SUMMARY TABLE

| Aspect | Details |
|--------|---------|
| **KB Types** | `document`, `faq`, `wiki` (NOT a type - wiki is feature on document) |
| **Wiki Enablement** | Via `WikiConfig` on document-type KB |
| **Indexing** | Dual pipeline: chunks (vector+keyword) + wiki pages (full-text) |
| **Wiki Page Types** | summary, entity, concept, index, log, synthesis, comparison |
| **Auto-Generation** | Via LLM when `WikiConfig.AutoIngest=true` + doc ingested |
| **LLM Model** | Specified in `WikiConfig.SynthesisModelID` |
| **Agent Access** | Via `AgentConfig.KnowledgeBases[]` → tools (knowledge_search, wiki_read_page) |
| **Link Tracking** | Bidirectional: InLinks + OutLinks on wiki pages |
| **Issue Tracking** | WikiPageIssue table for flagging problems |
| **Settings Storage** | `knowledge_bases.wiki_config` JSON column |
| **API Updates** | PUT `/api/v1/knowledge-bases/{id}` with config payload |
| **Async Queue** | Asynq with Redis backing (Lite mode in-memory) |
| **Rate Limiting** | `MaxPagesPerIngest` per batch |

---

## CONCLUSION

The WeKnora KB system is a sophisticated multi-modal architecture where:

1. **Knowledge Bases** are the top-level container with configurable type (document/faq)
2. **Wiki Feature** is an optional add-on to document-type KBs (via WikiConfig)
3. **Documents** are ingested into a KB and processed into **Chunks** (indexed for vector/keyword search)
4. **Wiki Ingest** is an async, LLM-powered pipeline that generates and maintains wiki pages from documents
5. **Agents** access KBs through a unified interface, with tools for both document search and wiki page retrieval
6. **Settings** flow through the KB Settings page UI to backend, where WikiConfig is updated atomically with other KB configs

The tight integration between knowledge bases, wiki pages, chunks, and agents creates a powerful knowledge management and retrieval system suitable for RAG applications, agent knowledge bases, and collaborative knowledge building.
