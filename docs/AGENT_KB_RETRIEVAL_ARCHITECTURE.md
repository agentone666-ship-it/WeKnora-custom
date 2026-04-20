# Agent/Bot Knowledge Base Integration Architecture

## Overview
This document maps how agents/bots in WeKnora use knowledge bases, reference datasets, configure retrieval strategies, and execute the retrieval pipeline.

---

## 1. AGENT/BOT MODEL DEFINITIONS

### Primary Models

#### `CustomAgent` (`internal/types/custom_agent.go`)
- **Role**: User-configurable AI agent (similar to GPTs)
- **Key Fields**:
  - `ID`: Agent identifier (UUID for custom, or `builtin-*` prefix for built-in)
  - `TenantID`: Owner tenant (composite primary key with ID)
  - `Config`: `CustomAgentConfig` struct containing full configuration
  - `IsBuiltin`: Boolean flag (built-in vs. custom)

**Built-in Agent IDs**:
- `builtin-quick-answer`: RAG mode for Q&A
- `builtin-smart-reasoning`: ReAct mode for multi-step reasoning
- `builtin-deep-researcher`, `builtin-data-analyst`, etc.

#### `AgentConfig` (`internal/types/agent.go`)
- **Role**: Runtime agent configuration (resolved from CustomAgent + Tenant config)
- **Key Fields**:
  ```go
  KnowledgeBases      []string      // Accessible KB IDs
  KnowledgeIDs        []string      // Individual document IDs (within KBs)
  SearchTargets       SearchTargets // Pre-computed unified search targets (runtime only)
  AllowedTools        []string      // List of tool names agent can use
  WebSearchEnabled    bool          // Whether web search is available
  MultiTurnEnabled    bool          // Multi-turn conversation support
  RetrieveKBOnlyWhenMentioned bool // Only retrieve KB when explicitly mentioned with @
  Thinking            *bool         // Extended thinking mode support
  LLMCallTimeout      int           // Single LLM call timeout in seconds
  ```

#### `SessionAgentConfig` (`internal/types/agent.go`)
- **Role**: Session-level agent override
- **Immutable fields only**: `AgentModeEnabled`, `WebSearchEnabled`, `KnowledgeBases`, `KnowledgeIDs`
- Other configs read from Tenant at runtime

---

## 2. KNOWLEDGE BASE BINDING

### How Agents Reference Knowledge Bases

**Path**: `internal/types/agent.go` → `AgentConfig.KnowledgeBases[]`

1. **Agent Config Level**: 
   - `CustomAgentConfig.AvailableKnowledgeBases` contains KB IDs agent can access
   - These are stored in the agent definition

2. **Session Override Level**:
   - `SessionAgentConfig.KnowledgeBases` can override/restrict agent's KB access
   - Resolved at request entry point

3. **Runtime Resolution** (`session_agent_qa.go:242`):
   ```go
   agentConfig.KnowledgeBases, agentConfig.KnowledgeIDs = 
       s.resolveKnowledgeBases(ctx, req)
   ```

### Knowledge Base Model (`internal/types/knowledgebase.go`)

```go
type KnowledgeBase struct {
    ID                    string              // Unique identifier
    Type                  string              // "document", "faq", "wiki"
    TenantID              uint64              // Owner tenant
    EmbeddingModelID      string              // Embedding model for vectors
    ChunkingConfig        ChunkingConfig      // Document splitting strategy
    FAQConfig            *FAQConfig           // FAQ-specific indexing options
    WikiConfig           *WikiConfig          // Wiki-specific configuration
    VLMConfig            VLMConfig            // Vision model for image processing
    QuestionGenerationConfig *QuestionGenerationConfig // Auto-generate questions
    // ... other metadata
}
```

**Knowledge Base Types**:
- `"document"`: Standard document-based KB
- `"faq"`: FAQ pairs (question-answer)
- `"wiki"`: Wiki pages with structured content

---

## 3. RETRIEVAL STRATEGY CONFIGURATION

### Global Retrieval Configuration (`internal/types/retrieval_config.go`)

**Stored at**: Tenant level (JSONB column in `tenants` table)
**Managed via**: Settings UI at `/tenants/kv/retrieval-config`

```go
type RetrievalConfig struct {
    EmbeddingTopK       int     // Vector search result limit (default: 50)
    VectorThreshold     float64 // Min similarity score 0-1 (default: 0.15)
    KeywordThreshold    float64 // Min keyword match score 0-1 (default: 0.3)
    RerankTopK          int     // Results after reranking (default: 10)
    RerankThreshold     float64 // Min rerank score (default: 0.2)
    RerankModelID       string  // Rerank model to use
}
```

**Application**: Used by all chat/search operations in the tenant

### Retriever Types (`internal/types/retriever.go`)

```go
type RetrieverType string

const (
    KeywordsRetrieverType  RetrieverType = "keywords"   // Exact/keyword matching
    VectorRetrieverType    RetrieverType = "vector"     // Semantic/embedding search
    WebSearchRetrieverType RetrieverType = "websearch"  // Web search
)
```

### Retriever Engine Types (`internal/types/retriever.go`)

```go
type RetrieverEngineType string

const (
    PostgresRetrieverEngineType      RetrieverEngineType = "postgres"       // Native SQL
    ElasticsearchRetrieverEngineType RetrieverEngineType = "elasticsearch"  // ES
    InfinityRetrieverEngineType      RetrieverEngineType = "infinity"       // Vector DB
    ElasticFaissRetrieverEngineType  RetrieverEngineType = "elasticfaiss"   // FAISS
    QdrantRetrieverEngineType        RetrieverEngineType = "qdrant"         // Qdrant
    MilvusRetrieverEngineType        RetrieverEngineType = "milvus"         // Milvus
    WeaviateRetrieverEngineType      RetrieverEngineType = "weaviate"       // Weaviate
    SQLiteRetrieverEngineType        RetrieverEngineType = "sqlite"         // SQLite
)
```

### Retriever Engine Configuration (Tenant-level)

**Field**: `Tenant.EffectiveEngines` → `[]RetrieverEngineParams`

```go
type RetrieverEngineParams struct {
    RetrieverEngineType RetrieverEngineType  // e.g., "elasticsearch"
    RetrieverType       RetrieverType        // e.g., "vector" or "keywords"
}
```

**Determines**: Which physical backend (ES, Postgres, etc.) handles which retrieval type

---

## 4. THE RETRIEVAL PIPELINE

### Entry Point: Agent Execution

**File**: `internal/application/service/session_agent_qa.go`

**Flow**:
1. **Request arrives** with `QARequest` containing user query and agent ID
2. **Resolve Tenant**: Determine retrieval tenant (own tenant or shared agent tenant)
3. **Build AgentConfig** (line 210-298):
   ```go
   func (s *sessionService) buildAgentConfig(ctx context.Context, ...) {
       // ... resolve KnowledgeBases and KnowledgeIDs
       agentConfig.KnowledgeBases, agentConfig.KnowledgeIDs = 
           s.resolveKnowledgeBases(ctx, req)
       
       // Build SearchTargets (next step)
       searchTargets, _ := s.buildSearchTargets(ctx, agentTenantID, 
           agentConfig.KnowledgeBases, agentConfig.KnowledgeIDs)
       agentConfig.SearchTargets = searchTargets
   }
   ```

### Stage 1: Build Search Targets (`session_knowledge_qa.go:393-486`)

**Function**: `buildSearchTargets(ctx, tenantID, knowledgeBaseIDs, knowledgeIDs)`

**Purpose**: Convert KnowledgeBase IDs and Knowledge IDs into unified SearchTargets

**Output**: `SearchTargets` = `[]*SearchTarget`

```go
type SearchTarget struct {
    Type            SearchTargetType  // "knowledge_base" or "knowledge"
    KnowledgeBaseID string            // KB to search
    TenantID        uint64            // KB's tenant (for cross-tenant shared KBs)
    KnowledgeIDs    []string          // Specific file IDs (only for "knowledge" type)
}
```

**Logic**:
```
For each KnowledgeBaseID:
  → Create SearchTarget(Type=knowledge_base, KnowledgeBaseID=id, TenantID=resolved)

For each KnowledgeID:
  → Find its parent KnowledgeBase
  → If KB not already fully searched, create SearchTarget(Type=knowledge, 
     KnowledgeBaseID=parent, KnowledgeIDs=[kid1, kid2, ...])
```

**Stored in**: `AgentConfig.SearchTargets` (runtime only, not persisted)

### Stage 2: Agent Initialization

**File**: `internal/application/service/agent_service.go:81-179`

**Creates**: `AgentEngine` with:
- `AgentConfig` (with SearchTargets)
- Chat model
- Rerank model
- Event bus
- Context manager

### Stage 3: Tool Execution - Knowledge Search

**File**: `internal/agent/tools/knowledge_search.go`

**Tool Definition**:
- **Name**: `"knowledge_search"`
- **Type**: `BaseTool`
- **Description**: Semantic/vector search for retrieving knowledge by meaning
- **Input**: 1-5 semantic queries (not raw text)
- **Retrieval Strategy**: Uses vector/semantic matching

**Execution Flow** (Execute method):
1. Parse input queries
2. Get embedding model from KB
3. Compute query embeddings
4. Call HybridSearch with SearchTargets
5. Return ranked results

### Stage 4: Hybrid Search (`knowledgebase_search.go:77-120`)

**Function**: `HybridSearch(ctx, kbID, params)`

**Parameters**:
- `kbID`: Primary KB (or first of multiple)
- `params.KnowledgeBaseIDs`: Override IDs (for multi-KB search with same embedding model)
- `params.QueryText`: Query string
- `params.VectorThreshold`: Min vector similarity
- `params.KeywordThreshold`: Min keyword score
- `params.MatchCount`: Desired results
- `params.DisableVectorMatch / DisableKeywordsMatch`: Optional disable flags

**Process**:
```
1. Determine search KBs (use params.KnowledgeBaseIDs if provided, else [kbID])
2. Create CompositeRetrieveEngine with tenant's configured engines
3. For each KB:
   a. Get embedding model
   b. Compute query embedding (if vector search enabled)
   c. Create vector + keyword retrieve jobs
4. Execute parallel retrieval across all engines
5. Deduplicate and merge results
6. Apply RRF (Reciprocal Rank Fusion) fusion
7. Rerank using rerank model
8. Return top results
```

### Stage 5: Composite Retrieval Engine (`internal/application/service/retriever/composite.go`)

**Role**: Routes retrieval requests to correct backend based on RetrieverType

**Structure**:
```go
type CompositeRetrieveEngine struct {
    engineInfos []*engineInfo  // Each maps RetrieverType to backend engine
}

type engineInfo struct {
    retrieveEngine interfaces.RetrieveEngineService
    retrieverType  []types.RetrieverType  // "vector", "keywords", etc.
}
```

**Dispatch Logic** (`Retrieve` method):
```go
For each RetrieveParams:
    Find first engineInfo that supports params.RetrieverType
    Delegate to that engine's Retrieve method
```

**Created from Tenant config**:
```go
retrieveEngine, err := retriever.NewCompositeRetrieveEngine(
    registry,
    tenantInfo.GetEffectiveEngines()  // []RetrieverEngineParams
)
```

---

## 5. SEARCH/RETRIEVAL TARGETS AT REQUEST LEVEL

### ChatManage (Pipeline State)

**File**: `internal/types/chat_manage.go`

```go
type ChatManage struct {
    // Immutable config (set at entry point)
    PipelineRequest struct {
        KnowledgeBaseIDs []string      // User-selected KBs
        KnowledgeIDs     []string      // User-selected specific files
        SearchTargets    SearchTargets // Pre-computed unified targets
        VectorThreshold  float64       // From tenant RetrievalConfig
        KeywordThreshold float64       // From tenant RetrievalConfig
        EmbeddingTopK    int           // From tenant RetrievalConfig
        VectorDatabase   string        // Primary vector DB selection
    }
    // Mutable state
    PipelineState struct {
        SearchResult []*SearchResult     // Raw search results
        RerankResult []*SearchResult     // After reranking
        MergeResult  []*SearchResult     // After context merging
    }
}
```

### SearchTargets Usage

**Used by**: Chat pipeline plugins

**File**: `internal/application/service/chat_pipeline/search.go`

```go
func (p *PluginSearch) OnEvent(ctx, event, chatManage, next) error {
    // Check if targets exist
    if len(chatManage.SearchTargets) == 0 {
        // No targets, skip KB search (unless web search enabled)
        return nil
    }
    
    // Execute KB search using SearchTargets
    kbResults := p.searchByTargets(ctx, chatManage)
    
    // searchByTargets iterates SearchTargets and queries each
    for _, target := range chatManage.SearchTargets {
        // Determine which index to query based on target
        results := p.knowledgeBaseService.HybridSearch(
            ctx, target.KnowledgeBaseID,
            types.SearchParams{
                KnowledgeBaseIDs: []string{target.KnowledgeBaseID},
                KnowledgeIDs:     target.KnowledgeIDs,  // Scoped if present
                QueryText:        chatManage.RewriteQuery,
                // ... thresholds, top K from RetrievalConfig
            }
        )
    }
}
```

---

## 6. AGENT-LEVEL RETRIEVAL PREFERENCES

### AgentConfig: Retrieval Preferences

**Stored in CustomAgentConfig**:

```go
type CustomAgentConfig struct {
    // Tool selection
    AllowedTools []string  // e.g., ["knowledge_search", "web_search", ...]
    
    // Retrieval triggers
    RetrieveKBOnlyWhenMentioned bool  // @mention required for KB search
    
    // Search strategy
    WebSearchEnabled     bool     // Enable web search tool
    WebSearchMaxResults  int      // Max web results to return
    WebSearchProviderID  string   // Which web search provider
    
    // Model selection for retrieval
    RerankModelID string  // Rerank model for search results
    
    // History management
    MultiTurnEnabled bool  // Use conversation history
    HistoryTurns    int    // How many previous turns to keep
    
    // Context management
    RetainRetrievalHistory bool  // Keep wiki_read_page results across turns
}
```

### Agent Tool Definitions

**File**: `internal/agent/tools/definitions.go`

**Key Tools for Knowledge Retrieval**:

1. **`knowledge_search`** (Semantic Search)
   - Purpose: Vector/embedding-based semantic search
   - Input: 1-5 semantic queries (not raw text)
   - Backends: Vector retrieval engines
   - RetrievalType: `VectorRetrieverType`

2. **`grep_chunks`** (Keyword Search)
   - Purpose: Exact keyword/literal matching
   - Input: Keywords to find
   - Backends: Keyword retrieval engines (ES, SQL)
   - RetrievalType: `KeywordsRetrieverType`

3. **`list_knowledge_chunks`** (List Chunks)
   - Purpose: Browse document chunks
   - Input: KB ID, document ID
   - Backends: Direct document lookup

4. **`wiki_search`** (Wiki Search)
   - Purpose: Search within wiki pages
   - Input: Query
   - Backends: Wiki-specific retrieval

5. **`web_search`** (Web Search)
   - Purpose: Search the internet
   - Input: Query
   - Backends: External web search provider
   - RetrievalType: `WebSearchRetrieverType`

**Default Allowed Tools**:
```go
[]string{
    ToolThinking,
    ToolTodoWrite,
    ToolKnowledgeSearch,      // Vector search
    ToolGrepChunks,           // Keyword search
    ToolListKnowledgeChunks,
    ToolWebSearch,
    ToolWebFetch,
    ToolFinalAnswer,
}
```

---

## 7. KEY DECISION FLOWS

### Decision 1: Which KB to Search?
1. Agent has `AgentConfig.SearchTargets`
2. When agent calls tool, it optionally specifies `knowledge_base_ids`
3. If not specified, uses all SearchTargets
4. Explicit KB param overrides agent defaults

### Decision 2: Which Retrieval Strategy?
1. Agent decides: calls `knowledge_search` (vector) or `grep_chunks` (keyword)
2. Or both sequentially with different queries
3. Or combines with `query_knowledge_graph` (graph search)
4. Tool makes the strategic choice, not the system

### Decision 3: Which Backend?
1. `RetrieveParams.RetrieverType` set by tool
2. CompositeRetrieveEngine looks up configured engines
3. Finds engine that supports that RetrieverType
4. Delegates to that backend

### Decision 4: Result Quality (Reranking)?
1. `RetrievalConfig.RerankModelID` specifies reranker
2. HybridSearch uses it to rerank combined results
3. Threshold applied: `RetrievalConfig.RerankThreshold`
4. Top-K applied: `RetrievalConfig.RerankTopK`

---

## 8. DATA FLOW DIAGRAM

```
Request
  ↓
QARequest (agent ID, query, KB IDs)
  ↓
BuildAgentConfig
  ├→ Resolve KnowledgeBases from agent + session override
  ├→ Resolve KnowledgeIDs (individual documents)
  ├→ BuildSearchTargets (converts KB IDs → SearchTargets)
  └→ AgentConfig.SearchTargets = [SearchTarget{...}, ...]
  ↓
CreateAgentEngine(AgentConfig, ChatModel, RerankModel)
  ├→ Register tools (knowledge_search, grep_chunks, web_search, ...)
  └→ Tool registry has access to SearchTargets
  ↓
Agent Execution Loop (ReAct)
  ↓
Agent calls tool (e.g., knowledge_search)
  ├→ Tool receives SearchTargets from AgentConfig
  ├→ Optionally filters by knowledge_base_ids param
  └→ Computes query embedding
  ↓
HybridSearch(kb_id, params)
  ├→ Create CompositeRetrieveEngine with tenant engines
  ├→ Parallel: Vector search + Keyword search
  ├→ Both call Retrieve() which dispatches to backends
  └→ Merge + Rerank + Return
  ↓
Backend (ES, Postgres, Qdrant, etc.)
  ├→ Vector search via embedding DB
  ├→ Keyword search via ES/SQL
  └→ Return scored results
  ↓
RRF Fusion (combine vector + keyword)
  ↓
Reranker (optional)
  ↓
Top-K Results
  ↓
Tool Result → Agent Thought/Action Loop
```

---

## 9. CONFIGURATION PRECEDENCE

For any retrieval operation:

1. **Agent-level**: 
   - `CustomAgentConfig.AllowedTools` (which tools available)
   - `CustomAgentConfig.RetrieveKBOnlyWhenMentioned` (when to retrieve)
   - `CustomAgentConfig.KnowledgeBases` (which KBs)
   
2. **Session-level override**:
   - `SessionAgentConfig.KnowledgeBases` (restrict agent's KBs)
   
3. **Request-level**:
   - Query string, optional KB filter, images, etc.
   
4. **Tenant-level**:
   - `RetrievalConfig` (thresholds, top-K, reranker)
   - `Tenant.EffectiveEngines` (which backends for vector/keyword)
   
5. **Knowledge Base-level**:
   - `KnowledgeBase.EmbeddingModelID` (which embedding model)
   - `KnowledgeBase.Type` (document vs FAQ vs wiki)
   - `KnowledgeBase.FAQConfig`, `WikiConfig` (type-specific config)

---

## 10. KEY FILES SUMMARY

| File | Purpose |
|------|---------|
| `internal/types/agent.go` | `AgentConfig`, `SessionAgentConfig` |
| `internal/types/custom_agent.go` | `CustomAgent`, `CustomAgentConfig` |
| `internal/types/knowledgebase.go` | `KnowledgeBase`, KB types (document/faq/wiki) |
| `internal/types/retrieval_config.go` | `RetrievalConfig` (tenant-level thresholds) |
| `internal/types/retriever.go` | `RetrieverType`, `RetrieverEngineType` enums |
| `internal/types/search.go` | `SearchTarget`, `SearchTargets`, `SearchResult` |
| `internal/application/service/session_agent_qa.go` | Agent QA entry point, `buildAgentConfig`, `buildSearchTargets` |
| `internal/application/service/knowledgebase_search.go` | `HybridSearch` orchestration |
| `internal/application/service/retriever/composite.go` | `CompositeRetrieveEngine` (backend dispatch) |
| `internal/agent/tools/knowledge_search.go` | Knowledge search tool definition |
| `internal/agent/tools/definitions.go` | Tool names and available tools list |
| `internal/application/service/chat_pipeline/search.go` | Chat pipeline search plugin |

---

## Summary

**Agents bind to KBs via**:
- `CustomAgent.Config.AvailableKnowledgeBases` + `CustomAgent.Config.AvailableKnowledgeIDs`
- Resolved to `AgentConfig.KnowledgeBases` + `AgentConfig.KnowledgeIDs` at runtime
- Converted to `AgentConfig.SearchTargets` for unified multi-KB querying

**Retrieval strategy is determined by**:
- **Tool choice**: Agent decides to call `knowledge_search` (vector) vs `grep_chunks` (keyword)
- **Tenant config**: `RetrievalConfig` sets thresholds, top-K, and rerank model
- **Tenant engines**: `Tenant.EffectiveEngines` specifies which backends handle which retriever types
- **KB model**: `KnowledgeBase.EmbeddingModelID`, type, and type-specific configs

**Retrieval pipeline**:
1. Agent executes and calls a retrieval tool
2. Tool computes embedding or keywords
3. Tool calls `HybridSearch` with SearchTargets
4. `HybridSearch` creates `CompositeRetrieveEngine` from tenant engines
5. `CompositeRetrieveEngine` dispatches to appropriate backend based on RetrieverType
6. Results are fused (RRF), reranked, and returned
7. Agent uses results in reasoning loop

