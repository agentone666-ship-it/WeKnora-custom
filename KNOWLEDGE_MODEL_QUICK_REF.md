# Knowledge Base Data Model - Quick Reference

## Core Structs at a Glance

### 1. KnowledgeBase (`internal/types/knowledgebase.go:40-98`)
Main entity defining a knowledge base with configuration for:
- **Chunking**: `ChunkingConfig` (chunk size, overlap, parent-child strategy)
- **Indexing**: Determined by TENANT config via `Tenant.RetrieverEngines`
- **Multimodal**: `VLMConfig` (vision/image processing), `ASRConfig` (speech recognition)
- **Wiki**: `WikiConfig` (wiki page generation on document ingest)
- **Question Generation**: `QuestionGenerationConfig` (auto-generate Q&A from chunks)
- **Graph Extraction**: `ExtractConfig` (knowledge graph entity/relation extraction)
- **Embedding Model**: `EmbeddingModelID` (for vector search)
- **Summary Model**: `SummaryModelID` (for LLM-based summarization)

### 2. WikiConfig (`internal/types/wiki_page.go:91-103`)
Controls wiki feature for a knowledge base:
- `Enabled`: bool - Feature toggle
- `AutoIngest`: bool - Auto-generate pages when documents added
- `SynthesisModelID`: string - LLM model for page generation
- `MaxPagesPerIngest`: int - Rate limit (0 = unlimited)

### 3. ChunkingConfig (`internal/types/knowledgebase.go:118-141`)
Controls document chunking strategy:
- `ChunkSize`: int - Main chunk size in characters
- `ChunkOverlap`: int - Overlap between chunks
- `ParserEngineRules`: []ParserEngineRule - Parser selection per file type
- `EnableParentChild`: bool - Two-level chunking (context + matching)
- `ParentChunkSize`: int - Large chunks for context (default 4096)
- `ChildChunkSize`: int - Small chunks for embedding (default 384)

## Processing Pipeline Steps

### Step 1: Document Ingestion
```
Document Upload → ProcessDocument Task (async)
```
**File**: `internal/application/service/knowledge.go:7732`

**Key**: 
- Validates KB config (multimodal, ASR enabled)
- Downloads file from URL if provided
- Calls `convert()` to parse document

### Step 2: Chunk Creation & Indexing
```
processChunks() → Save chunks to DB → BatchIndex()
```
**File**: `internal/application/service/knowledge.go:1681`

**Key**:
- Creates chunks from parsed content
- Parent chunks stored but NOT indexed (parent-child mode)
- Child/text chunks prepared for indexing
- `retrieveEngine.BatchIndex()` uses **TENANT's RetrieverEngines** config
- Supports both vector and keyword indexing (can be combined)

### Step 3: Post-Processing
```
KnowledgePostProcess → Spawn async tasks
```
**File**: `internal/application/service/knowledge_post_process.go:42`

**Key**:
- Summary Generation (LLM summarization)
- Question Generation (if enabled)
- Graph RAG Extraction (if enabled)
- **Wiki Ingest** (if enabled AND AutoIngest=true AND has text chunks)

## Vector vs Keyword Indexing Decision

**Decision Location**: Tenant configuration, NOT knowledge base level

**How it works**:
1. Tenant has `RetrieverEngines` config specifying retriever types
2. Each engine can be: `"vector"`, `"keywords"`, or both
3. During `BatchIndex()`, all configured types are used
4. Enables **hybrid search** combining results from multiple index types

**Config Path**: 
- Table: `tenants`
- Column: `retriever_engines` (JSONB)
- Type: `[]RetrieverEngineParams`
- Each param specifies: `{RetrieverEngineType, RetrieverType}`

## Wiki Ingest Trigger

**Exact Condition** (`knowledge_post_process.go:123-126`):
```go
if kb.IsWikiEnabled() && kb.WikiConfig.AutoIngest && len(textChunks) > 0 {
    EnqueueWikiIngest(...)
}
```

**Breakdown**:
- `kb.IsWikiEnabled()` ⟹ `WikiConfig != nil && Enabled == true`
- `kb.WikiConfig.AutoIngest` ⟹ Must be explicitly enabled
- `len(textChunks) > 0` ⟹ Must have text content (not just images)

**Result**: 
- Enqueues `TypeWikiIngest` task
- LLM generates/updates wiki pages based on ingested content
- Uses `SynthesisModelID` from WikiConfig

## Retrieval Configuration

**File**: `internal/types/retrieval_config.go:14-27`
**Stored on**: `tenants.retrieval_config` (JSONB)

Controls:
- `EmbeddingTopK` (50): Max results from vector search
- `VectorThreshold` (0.15): Min similarity score for vectors
- `KeywordThreshold` (0.3): Min score for keywords
- `RerankTopK` (10): Results after reranking
- `RerankModelID`: LLM for reranking results

## API Endpoints

### Create Knowledge Base
```
POST /knowledge-bases
Body: types.KnowledgeBase
```

### Update Knowledge Base
```
PUT /knowledge-bases/{id}
Body: UpdateKnowledgeBaseRequest {
    Name: string
    Description: string
    Config: *KnowledgeBaseConfig { ChunkingConfig, WikiConfig, ... }
}
```

### Delete Knowledge Base
```
DELETE /knowledge-bases/{id}
```

## Important Constants

### Knowledge Base Types
- `"document"` - Regular documents with chunking/search
- `"faq"` - Q&A pairs
- `"wiki"` - Wiki pages (not a separate type, but feature on documents)

### Retriever Types
- `"vector"` - Vector/embedding-based search
- `"keywords"` - Full-text/keyword search
- `"websearch"` - Web search

### Chunk Types
- `"text"` - Main text content
- `"parent_text"` - Parent chunk (context, not indexed)
- `"image_ocr"` - OCR text from images
- `"image_caption"` - AI-generated image captions
- `"summary"` - Document summary chunk

## Key Takeaways

1. **Indexing is tenant-scoped**, not per-KB
2. **Hybrid search** uses multiple index types simultaneously
3. **Wiki ingest** requires three conditions: enabled, auto_ingest, content
4. **Parent-child chunking** splits semantic context from search terms
5. **Post-processing** is async and orchestrates multiple LLM tasks
6. **Multimodal** (images/speech) requires explicit config + KB support

