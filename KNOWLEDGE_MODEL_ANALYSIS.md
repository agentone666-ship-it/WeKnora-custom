# Knowledge Base Data Model and Document Processing Pipeline - Comprehensive Analysis

## 1. KnowledgeBase Type Definition

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/knowledgebase.go`

**Lines:** 40-98 (Main struct definition), plus related constants 12-38

### Full KnowledgeBase Struct (lines 40-98):

```go
type KnowledgeBase struct {
    // Unique identifier of the knowledge base
    ID string `yaml:"id" json:"id" gorm:"type:varchar(36);primaryKey"`
    // Name of the knowledge base
    Name string `yaml:"name" json:"name"`
    // Type of the knowledge base (document, faq, etc.)
    Type string `yaml:"type" json:"type" gorm:"type:varchar(32);default:'document'"`
    // Whether this knowledge base is temporary (ephemeral) and should be hidden from UI
    IsTemporary bool `yaml:"is_temporary" json:"is_temporary" gorm:"default:false"`
    // Description of the knowledge base
    Description string `yaml:"description" json:"description"`
    // Tenant ID
    TenantID uint64 `yaml:"tenant_id" json:"tenant_id"`
    // Chunking configuration
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config" gorm:"type:json"`
    // Image processing configuration
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config" gorm:"type:json"`
    // ID of the embedding model
    EmbeddingModelID string `yaml:"embedding_model_id" json:"embedding_model_id"`
    // Summary model ID
    SummaryModelID string `yaml:"summary_model_id" json:"summary_model_id"`
    // VLM config
    VLMConfig VLMConfig `yaml:"vlm_config" json:"vlm_config" gorm:"type:json"`
    // ASR config (Automatic Speech Recognition)
    ASRConfig ASRConfig `yaml:"asr_config" json:"asr_config" gorm:"type:json"`
    // Storage provider config (new): only stores provider selection; credentials from tenant StorageEngineConfig
    StorageProviderConfig *StorageProviderConfig `yaml:"storage_provider_config" json:"storage_provider_config" gorm:"column:storage_provider_config;type:jsonb"`
    // Deprecated: legacy COS config column. Kept for backward compatibility with old data.
    StorageConfig StorageConfig `yaml:"-" json:"storage_config" gorm:"column:cos_config;type:json"`
    // Extract config
    ExtractConfig *ExtractConfig `yaml:"extract_config" json:"extract_config" gorm:"column:extract_config;type:json"`
    // FAQConfig stores FAQ specific configuration such as indexing strategy
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config" gorm:"column:faq_config;type:json"`
    // QuestionGenerationConfig stores question generation configuration for document knowledge bases
    QuestionGenerationConfig *QuestionGenerationConfig `yaml:"question_generation_config" json:"question_generation_config" gorm:"column:question_generation_config;type:json"`
    // WikiConfig stores wiki-specific configuration (only for wiki type knowledge bases)
    WikiConfig *WikiConfig `yaml:"wiki_config" json:"wiki_config" gorm:"column:wiki_config;type:json"`
    // Whether this knowledge base is pinned to the top of the list
    IsPinned bool `yaml:"is_pinned" json:"is_pinned" gorm:"default:false"`
    // Time when the knowledge base was pinned (nil if not pinned)
    PinnedAt *time.Time `yaml:"pinned_at" json:"pinned_at"`
    // Creation time of the knowledge base
    CreatedAt time.Time `yaml:"created_at" json:"created_at"`
    // Last updated time of the knowledge base
    UpdatedAt time.Time `yaml:"updated_at" json:"updated_at"`
    // Deletion time of the knowledge base
    DeletedAt gorm.DeletedAt `yaml:"deleted_at" json:"deleted_at" gorm:"index"`
    // Knowledge count (not stored in database, calculated on query)
    KnowledgeCount int64 `yaml:"knowledge_count" json:"knowledge_count" gorm:"-"`
    // Chunk count (not stored in database, calculated on query)
    ChunkCount int64 `yaml:"chunk_count" json:"chunk_count" gorm:"-"`
    // IsProcessing indicates if there is a processing import task (for FAQ type knowledge bases)
    IsProcessing bool `yaml:"is_processing" json:"is_processing" gorm:"-"`
    // ProcessingCount indicates the number of knowledge items being processed (for document type knowledge bases)
    ProcessingCount int64 `yaml:"processing_count" json:"processing_count" gorm:"-"`
    // ShareCount indicates the number of organizations this knowledge base is shared with (not stored in database)
    ShareCount int64 `yaml:"share_count" json:"share_count" gorm:"-"`
}
```

### Key Constants (lines 12-38):

```go
// Knowledge base types
const (
    KnowledgeBaseTypeDocument = "document"
    KnowledgeBaseTypeFAQ      = "faq"
    KnowledgeBaseTypeWiki     = "wiki"
)

// FAQ Index Modes
type FAQIndexMode string
const (
    FAQIndexModeQuestionOnly FAQIndexMode = "question_only"
    FAQIndexModeQuestionAnswer FAQIndexMode = "question_answer"
)

type FAQQuestionIndexMode string
const (
    FAQQuestionIndexModeCombined FAQQuestionIndexMode = "combined"
    FAQQuestionIndexModeSeparate FAQQuestionIndexMode = "separate"
)
```

### Important Methods:
- **Line 508-509**: `IsWikiEnabled()` - checks if wiki feature is enabled
- **Line 515-528**: `IsMultimodalEnabled()` - checks if multimodal processing is enabled
- **Line 475-504**: `EnsureDefaults()` - ensures type-specific defaults

---

## 2. WikiConfig Type Definition

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/wiki_page.go`

**Lines:** 91-103

```go
type WikiConfig struct {
    // Enabled activates the wiki feature for this knowledge base
    Enabled bool `yaml:"enabled" json:"enabled"`
    // AutoIngest triggers wiki page generation/update when new documents are added
    AutoIngest bool `yaml:"auto_ingest" json:"auto_ingest"`
    // SynthesisModelID is the LLM model ID used for wiki page generation and updates
    SynthesisModelID string `yaml:"synthesis_model_id" json:"synthesis_model_id"`
    // MaxPagesPerIngest limits pages created/updated per ingest operation (0 = no limit)
    MaxPagesPerIngest int `yaml:"max_pages_per_ingest" json:"max_pages_per_ingest"`
}
```

**Value() and Scan() Methods:** Lines 105-120 (JSONB serialization/deserialization)

---

## 3. ChunkingConfig Type Definition

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/knowledgebase.go`

**Lines:** 118-141

```go
type ChunkingConfig struct {
    // Chunk size
    ChunkSize int `yaml:"chunk_size" json:"chunk_size"`
    // Chunk overlap
    ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap"`
    // Separators
    Separators []string `yaml:"separators" json:"separators"`
    // EnableMultimodal (deprecated, kept for backward compatibility with old data)
    EnableMultimodal bool `yaml:"enable_multimodal,omitempty" json:"enable_multimodal,omitempty"`
    // ParserEngineRules configures which parser engine to use for each file type.
    ParserEngineRules []ParserEngineRule `yaml:"parser_engine_rules,omitempty" json:"parser_engine_rules,omitempty"`
    // EnableParentChild enables two-level parent-child chunking strategy.
    EnableParentChild bool `yaml:"enable_parent_child,omitempty" json:"enable_parent_child,omitempty"`
    // ParentChunkSize is the size of parent chunks (default: 4096).
    ParentChunkSize int `yaml:"parent_chunk_size,omitempty" json:"parent_chunk_size,omitempty"`
    // ChildChunkSize is the size of child chunks used for embedding (default: 384).
    ChildChunkSize int `yaml:"child_chunk_size,omitempty" json:"child_chunk_size,omitempty"`
}
```

---

## 4. Document Processing Pipeline

### 4.1 ProcessDocument Function

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/service/knowledge.go`

**Lines:** 7732-8108+ (Main entry point for document processing)

**Signature:**
```go
func (s *knowledgeService) ProcessDocument(ctx context.Context, t *asynq.Task) error
```

**Key Steps in ProcessDocument (lines 7732-8108+):**

1. **Unmarshal Task Payload** (7733-7737)
   - Payload type: `types.DocumentProcessPayload`
   - Includes: KnowledgeID, KnowledgeBaseID, FilePath, FileURL, FileType, etc.

2. **Idempotency Checks** (7761-7801)
   - Check knowledge existence
   - Check ParseStatus (skip if already completed)
   - Check if knowledge is being deleted
   - Update ParseStatus to "processing"

3. **KB Validation** (7803-7812)
   - Fetch knowledge base configuration
   - Validate embedding model configuration

4. **File Processing** (7821-7931)
   - Handle multimodal image processing flag
   - Handle ASR (Audio Speech Recognition) configuration
   - Download files from URL if needed (with SSRF protection)
   - Call `s.convert()` to convert files to readable format

5. **Chunk Processing** (8108+)
   - Call `s.processChunks()` for vectorization and indexing

### 4.2 processChunks Function

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/service/knowledge.go`

**Lines:** 1681-2074 (Core chunk processing and indexing)

**Signature:**
```go
func (s *knowledgeService) processChunks(ctx context.Context,
    kb *types.KnowledgeBase, 
    knowledge *types.Knowledge, 
    chunks []types.ParsedChunk,
    opts ...ProcessChunksOptions,
)
```

**Key Steps in processChunks:**

1. **Get Embedding Model** (1708-1714)
   - Fetch embedding model using `kb.EmbeddingModelID`
   - Get model dimensions for storage estimation

2. **Idempotency & Cleanup** (1716-1744)
   - Clean up old chunks: `s.chunkService.DeleteChunksByKnowledgeID()`
   - Delete old index data: `retrieveEngine.DeleteByKnowledgeIDList()`
   - Delete old graph data: `s.graphEngine.DelGraph()`

3. **Parent-Child Chunking (Optional)** (1810-1876)
   - If enabled, create parent chunks first (NOT indexed for vectors)
   - Create child chunks, linked to parent via `ParentChunkID`
   - Parent chunks provide context, children used for matching

4. **Create Index Information** (1908-1928)
   - Only for TEXT chunks, NOT parent chunks
   - Prepend document title to content for better semantic alignment
   - Create `IndexInfo` objects for each text chunk
   - Title prefix: `titlePrefix = knowledge.Title + "\n"`

5. **Storage Quota Check** (1932-1955)
   - Estimate storage size: `retrieveEngine.EstimateStorageSize()`
   - Verify tenant has sufficient quota

6. **Save Chunks to Database** (1965-1973)
   - `s.chunkService.CreateChunks(ctx, insertChunks)`
   - Includes all chunk types (parent, child, text)

7. **Batch Indexing** (1986-2008)
   - **CRITICAL**: `retrieveEngine.BatchIndex(ctx, embeddingModel, indexInfoList)`
   - This is where vector/keyword indexing decision is made (see Section 5 below)

8. **Enqueue Post-Processing Tasks** (2044-2066)
   - If multimodal enabled: enqueue image processing tasks
   - Otherwise: enqueue `TypeKnowledgePostProcess` task immediately

9. **Update Storage Usage** (2068-2073)
   - Adjust tenant storage quota: `tenantInfo.StorageUsed += totalStorageSize`

### Key Decision Point: Vector vs Keyword Indexing

**Location:** Lines 1987 and retrieval engine configuration

**How it's determined:**
- The `retrieveEngine.BatchIndex()` call does NOT take explicit parameters for vector vs keyword
- **Instead, the decision is made at the TENANT level** via `Tenant.RetrieverEngines` configuration
- The `GetEffectiveEngines()` method (tenant.go line 126-131) returns:
  - Configured engines if set: `t.RetrieverEngines.Engines`
  - Otherwise: system defaults via `GetDefaultRetrieverEngines()`

**See Section 5 for detailed indexing configuration.**

---

## 5. Retriever/Indexing Type Configuration

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/retriever.go`

**Lines:** 1-101

### RetrieverType Constants (lines 21-26):

```go
type RetrieverType string

const (
    KeywordsRetrieverType  RetrieverType = "keywords"  // Keywords retriever
    VectorRetrieverType    RetrieverType = "vector"    // Vector retriever
    WebSearchRetrieverType RetrieverType = "websearch" // Web search retriever
)
```

### RetrieverEngineType Constants (lines 8-16):

```go
const (
    PostgresRetrieverEngineType      RetrieverEngineType = "postgres"
    ElasticsearchRetrieverEngineType RetrieverEngineType = "elasticsearch"
    InfinityRetrieverEngineType      RetrieverEngineType = "infinity"
    ElasticFaissRetrieverEngineType  RetrieverEngineType = "elasticfaiss"
    QdrantRetrieverEngineType        RetrieverEngineType = "qdrant"
    MilvusRetrieverEngineType        RetrieverEngineType = "milvus"
    WeaviateRetrieverEngineType      RetrieverEngineType = "weaviate"
    SQLiteRetrieverEngineType        RetrieverEngineType = "sqlite"
)
```

### RetrieverEngineParams (lines 56-62):

```go
type RetrieverEngineParams struct {
    // Retriever engine type
    RetrieverEngineType RetrieverEngineType `yaml:"retriever_engine_type" json:"retriever_engine_type"`
    // Retriever type
    RetrieverType RetrieverType `yaml:"retriever_type" json:"retriever_type"`
}
```

### Tenant Configuration (tenant.go, lines 125-131):

```go
func (t *Tenant) GetEffectiveEngines() []RetrieverEngineParams {
    if len(t.RetrieverEngines.Engines) > 0 {
        return t.RetrieverEngines.Engines
    }
    return GetDefaultRetrieverEngines()
}
```

**Key Points:**
- Indexing types are configured per TENANT, not per knowledge base
- Multiple retriever engines can be configured (e.g., Vector + Keywords)
- Each engine specifies its type (RetrieverEngineType) and retriever type(s)
- During indexing, ALL configured retriever types are used
- This enables HYBRID search (vector + keyword combined)

---

## 6. Post-Processing Handler

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/service/knowledge_post_process.go`

**Lines:** 1-192 (Complete file)

### KnowledgePostProcessService.Handle Function

**Entry Point:** Lines 42-128

**Key Steps:**

1. **Fetch Knowledge and KB** (57-69)
   - Get knowledge by ID: `s.knowledgeRepo.GetKnowledgeByIDOnly()`
   - Get knowledge base: `s.kbService.GetKnowledgeBaseByIDOnly()`

2. **Fetch All Chunks** (71-83)
   - Get chunks by knowledge ID: `s.chunkService.ListChunksByKnowledgeID()`
   - Filter text-like chunks (TEXT, OCR, CAPTION)

3. **Update ParseStatus to Completed** (85-103)
   - Change ParseStatus from "processing" to "completed"
   - Set SummaryStatus based on whether text chunks exist:
     - If text chunks: `SummaryStatus = SummaryStatusPending`
     - Otherwise: `SummaryStatus = SummaryStatusNone`

4. **Enqueue Summary Generation** (106-109)
   - **Line 107**: `s.enqueueSummaryGenerationTask(ctx, payload)`
   - Only if text chunks exist

5. **Enqueue Question Generation (if enabled)** (106-109)
   - **Line 108**: `s.enqueueQuestionGenerationIfEnabled(ctx, payload, kb)`
   - Checks `kb.QuestionGenerationConfig.Enabled`
   - Uses QuestionCount (default 3, max 10)

6. **Spawn Graph RAG Tasks** (112-120)
   - **Lines 112-120**: Check `kb.ExtractConfig != nil && kb.ExtractConfig.Enabled`
   - For each text-like chunk, enqueue: `NewChunkExtractTask()`
   - Uses `kb.SummaryModelID` for LLM

7. **Wiki Ingest Trigger** (122-126)
   - **Lines 122-126**: CRITICAL - This is where wiki ingest is triggered
   - **Condition:**
     ```go
     if kb.IsWikiEnabled() && kb.WikiConfig.AutoIngest && len(textChunks) > 0 {
         EnqueueWikiIngest(...)
     }
     ```
   - **Exact condition breakdown:**
     - `kb.IsWikiEnabled()` = `kb.WikiConfig != nil && kb.WikiConfig.Enabled` (knowledgebase.go line 509)
     - `kb.WikiConfig.AutoIngest` = must be true (wiki_page.go line 98)
     - `len(textChunks) > 0` = must have text content to ingest

### Supporting Functions:

**enqueueSummaryGenerationTask** (lines 130-153)
- Payload: `types.SummaryGenerationPayload`
- Task type: `types.TypeSummaryGeneration`
- Queue: "low" priority
- Max retries: 3

**enqueueQuestionGenerationIfEnabled** (lines 155-191)
- Checks if enabled and validates question count
- Payload: `types.QuestionGenerationPayload`
- Task type: `types.TypeQuestionGeneration`
- Queue: "low" priority
- Max retries: 3

---

## 7. RetrievalConfig (Threshold Configuration)

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/retrieval_config.go`

**Lines:** 14-27

```go
type RetrievalConfig struct {
    // EmbeddingTopK is the maximum number of chunks returned by vector search (default: 50)
    EmbeddingTopK int `json:"embedding_top_k"`
    // VectorThreshold is the minimum vector similarity score (0-1, default: 0.15)
    VectorThreshold float64 `json:"vector_threshold"`
    // KeywordThreshold is the minimum keyword match score (0-1, default: 0.3)
    KeywordThreshold float64 `json:"keyword_threshold"`
    // RerankTopK is the maximum number of results after reranking (default: 10)
    RerankTopK int `json:"rerank_top_k"`
    // RerankThreshold is the minimum rerank score (-10 to 10, default: 0.2)
    RerankThreshold float64 `json:"rerank_threshold"`
    // RerankModelID is the ID of the rerank model to use (required for search)
    RerankModelID string `json:"rerank_model_id"`
}
```

**Stored on:** `tenants.retrieval_config` (JSONB column)

**Usage:** Controls search thresholds and result limits for both vector and keyword searches

---

## 8. API/DTO Types

**File Path:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/handler/knowledgebase.go`

### UpdateKnowledgeBaseRequest (lines 427-431)

```go
type UpdateKnowledgeBaseRequest struct {
    Name        string                     `json:"name"        binding:"required"`
    Description string                     `json:"description"`
    Config      *types.KnowledgeBaseConfig `json:"config"`
}
```

### KnowledgeBaseConfig (knowledgebase.go lines 100-110)

```go
type KnowledgeBaseConfig struct {
    // Chunking configuration
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config"`
    // Image processing configuration
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
    // FAQ configuration (only for FAQ type knowledge bases)
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config"`
    // Wiki configuration (only for wiki-enabled knowledge bases)
    WikiConfig *WikiConfig `yaml:"wiki_config" json:"wiki_config"`
}
```

### API Endpoints (knowledgebase.go):

- **CreateKnowledgeBase**: POST `/knowledge-bases` (lines 114-147)
  - Input: `types.KnowledgeBase`
  
- **UpdateKnowledgeBase**: PUT `/knowledge-bases/{id}` (lines 446-488)
  - Input: `UpdateKnowledgeBaseRequest`
  - Requires admin/editor permission
  
- **DeleteKnowledgeBase**: DELETE `/knowledge-bases/{id}` (lines 502-536)
  - Requires owner permission

---

## 9. Document Processing Flow Summary

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. DOCUMENT UPLOAD                                              │
│    ↓                                                            │
│ 2. ProcessDocument Task (Task Queue)                            │
│    ├─ Fetch Knowledge & KB                                     │
│    ├─ Validate configuration (multimodal, ASR)                 │
│    ├─ Download file if from URL (SSRF check)                   │
│    └─ Call convert() to parse file                             │
│       ↓                                                         │
│ 3. processChunks()                                              │
│    ├─ Get embedding model (kb.EmbeddingModelID)               │
│    ├─ Clean old chunks/indices                                 │
│    ├─ Create chunks (parent + child if enabled)                │
│    ├─ Estimate storage size                                    │
│    ├─ Save chunks to DB                                        │
│    └─ BatchIndex() ← VECTOR/KEYWORD decision here              │
│       ├─ Uses Tenant.RetrieverEngines config                   │
│       ├─ Calls all configured retriever types                  │
│       └─ Creates vectors + keyword indices                     │
│    ├─ Enqueue multimodal tasks (if enabled)                    │
│    └─ OR enqueue TypeKnowledgePostProcess (if no multimodal)   │
│       ↓                                                         │
│ 4. KnowledgePostProcess Task                                    │
│    ├─ Update ParseStatus = "completed"                         │
│    ├─ Set SummaryStatus = "pending" (if text chunks)           │
│    ├─ Enqueue Summary Generation (line 107)                    │
│    ├─ Enqueue Question Generation (line 108, if enabled)       │
│    ├─ Enqueue Graph RAG Extract (lines 114-119)               │
│    └─ **WIKI INGEST CHECK** (lines 123-126)                    │
│       IF kb.IsWikiEnabled() AND                                │
│          kb.WikiConfig.AutoIngest AND                          │
│          len(textChunks) > 0                                   │
│       THEN EnqueueWikiIngest()                                  │
│       ↓                                                         │
│ 5. POST-PROCESSING (Async Tasks)                               │
│    ├─ Summary Generation                                       │
│    ├─ Question Generation                                      │
│    ├─ Graph RAG Extraction                                     │
│    └─ Wiki Ingest (if triggered)                               │
└─────────────────────────────────────────────────────────────────┘
```

---

## 10. Key File Locations Summary

| Component | File Path | Key Lines |
|-----------|-----------|-----------|
| KnowledgeBase struct | `internal/types/knowledgebase.go` | 40-98 |
| WikiConfig struct | `internal/types/wiki_page.go` | 91-103 |
| ChunkingConfig struct | `internal/types/knowledgebase.go` | 118-141 |
| ProcessDocument | `internal/application/service/knowledge.go` | 7732-8108 |
| processChunks | `internal/application/service/knowledge.go` | 1681-2074 |
| KnowledgePostProcessService | `internal/application/service/knowledge_post_process.go` | 16-128 |
| RetrieverTypes | `internal/types/retriever.go` | 18-26 |
| Tenant.GetEffectiveEngines | `internal/types/tenant.go` | 125-131 |
| RetrievalConfig | `internal/types/retrieval_config.go` | 14-27 |
| API Handlers | `internal/handler/knowledgebase.go` | 114-488 |

