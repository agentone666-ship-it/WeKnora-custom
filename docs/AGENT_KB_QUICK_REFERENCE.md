# Agent & Knowledge Base Integration - Quick Reference

## Core Data Structures

### Agent Models

```
CustomAgent (persisted)
├── ID: agent-uuid or builtin-quick-answer
├── TenantID: owner tenant
├── Config: CustomAgentConfig
│   ├── AvailableKnowledgeBases: []string         # Which KBs agent can access
│   ├── AvailableKnowledgeIDs: []string           # Which documents it can access
│   ├── AllowedTools: []string                    # Tool whitelist
│   ├── RerankModelID: string                     # For search result ranking
│   ├── WebSearchEnabled: bool
│   ├── MultiTurnEnabled: bool
│   ├── RetrieveKBOnlyWhenMentioned: bool
│   └── [... more settings ...]
└── IsBuiltin: bool
    
↓ (Runtime resolution)

AgentConfig (runtime only)
├── KnowledgeBases: []string                      # Resolved KB IDs
├── KnowledgeIDs: []string                        # Resolved document IDs
├── SearchTargets: []*SearchTarget               # ← UNIFIED search scope
├── AllowedTools: []string
├── RetrievalConfig params
└── [... execution config ...]
```

### Knowledge Base Model

```
KnowledgeBase
├── ID: kb-uuid
├── Type: "document" | "faq" | "wiki"             # KB type
├── TenantID: owner tenant
├── EmbeddingModelID: model-id                    # For vector search
├── FAQConfig: FAQConfig (if Type == "faq")       # FAQ indexing strategy
├── WikiConfig: WikiConfig (if Type == "wiki")    # Wiki-specific config
└── ChunkingConfig: ChunkingConfig                # How documents split
```

### Retrieval Configuration (Tenant-level)

```
RetrievalConfig (at Tenant)
├── EmbeddingTopK: int             # Vector search: max candidates (default: 50)
├── VectorThreshold: float64       # Min similarity 0-1 (default: 0.15)
├── KeywordThreshold: float64      # Min keyword match 0-1 (default: 0.3)
├── RerankTopK: int                # Final results count (default: 10)
├── RerankThreshold: float64       # Min rerank score (default: 0.2)
└── RerankModelID: string          # Which reranker to use

Tenant.EffectiveEngines: []RetrieverEngineParams
├── RetrieverEngineType: "elasticsearch" | "postgres" | "qdrant" | ...
└── RetrieverType: "vector" | "keywords" | "websearch"
```

## How Agents Access KBs: Step-by-Step

### 1. Request Arrives
```
QARequest {
    Query: "user question",
    Agent: CustomAgent (with Config.AvailableKnowledgeBases),
    SessionAgentConfig: {KnowledgeBases: override}  // optional override
}
```

### 2. Resolve Runtime Configuration
```
buildAgentConfig() {
    // Merge CustomAgent + SessionAgentConfig
    agentConfig.KnowledgeBases = resolveKnowledgeBases(customAgent, sessionConfig)
    agentConfig.KnowledgeIDs = resolveKnowledgeIDs(...)
}
```

### 3. Convert to Unified Search Targets
```
buildSearchTargets(knowledgeBaseIDs, knowledgeIDs) {
    // For each KB:
    targets.append(SearchTarget{
        Type: "knowledge_base",           // Search entire KB
        KnowledgeBaseID: id,
        TenantID: resolved_tenant,
        KnowledgeIDs: []                  // Empty = whole KB
    })
    
    // For specific documents:
    targets.append(SearchTarget{
        Type: "knowledge",                // Search specific files
        KnowledgeBaseID: parent_kb,
        TenantID: resolved_tenant,
        KnowledgeIDs: [doc1, doc2, ...]  // Only these
    })
}
// Result stored in: AgentConfig.SearchTargets
```

### 4. Agent Calls Retrieval Tool
```
Agent's thought: "I need to search knowledge..."
Agent.ExecuteTool("knowledge_search", {
    queries: ["What is RAG?"],
    knowledge_base_ids: []    // if empty, uses SearchTargets
})
```

### 5. Tool Performs Hybrid Search
```
Tool.Execute() {
    // Get query embedding
    queryEmbedding = getEmbedding(query)  // from KB's EmbeddingModelID
    
    // Call HybridSearch
    results = HybridSearch(kb_id, {
        QueryText: query,
        Embedding: queryEmbedding,
        KnowledgeIDs: target.KnowledgeIDs,      // Scoped if present
        VectorThreshold: retrieval_config.threshold,
        KeywordThreshold: retrieval_config.threshold,
        MatchCount: retrieval_config.top_k,
        RetrieverType: "vector"                 // for vector search
    })
}
```

### 6. Hybrid Search Orchestrates Retrieval
```
HybridSearch(kb_id, params) {
    // 1. Create composite engine from tenant's configured backends
    engine = CompositeRetrieveEngine(tenant.EffectiveEngines)
    
    // 2. Execute parallel retrieval
    vectorResults = engine.Retrieve(RetrieveParams{
        RetrieverType: "vector",
        KnowledgeBaseIDs: [kb_id],
        Embedding: query_embedding,
        Threshold: vector_threshold,
        TopK: 5x over_retrieval
    })
    
    keywordResults = engine.Retrieve(RetrieveParams{
        RetrieverType: "keywords",
        KnowledgeBaseIDs: [kb_id],
        Query: query_text,
        Threshold: keyword_threshold,
        TopK: 5x over_retrieval
    })
    
    // 3. Merge with RRF (Reciprocal Rank Fusion)
    merged = RRF(vectorResults, keywordResults)
    
    // 4. Rerank
    final = Rerank(merged, rerank_model, top_k=10)
    
    return final
}
```

### 7. CompositeRetrieveEngine Routes to Backend
```
CompositeRetrieveEngine.Retrieve(params) {
    // params.RetrieverType = "vector" or "keywords"
    
    // Find engine that supports this type
    for engineInfo in this.engineInfos {
        if engineInfo.supports(params.RetrieverType) {
            return engineInfo.engine.Retrieve(params)  // Delegate
        }
    }
}

// Backends that might handle it:
// - "vector" type → Elasticsearch, Qdrant, Milvus, etc.
// - "keywords" type → Elasticsearch, PostgreSQL, etc.
```

## Enumeration Reference

### RetrieverType (what to search)
- `"vector"` - Semantic/embedding search
- `"keywords"` - Exact keyword/full-text search
- `"websearch"` - External web search

### RetrieverEngineType (where to search)
- `"elasticsearch"` - ES backend (supports both vector & keywords)
- `"postgres"` - PostgreSQL (keywords)
- `"qdrant"` - Qdrant vector DB
- `"infinity"` - Infinity vector DB
- `"milvus"` - Milvus vector DB
- `"weaviate"` - Weaviate vector DB
- `"elasticfaiss"` - FAISS indices
- `"sqlite"` - SQLite (keywords)

### SearchTargetType
- `"knowledge_base"` - Search entire KB
- `"knowledge"` - Search specific files/documents only

### KnowledgeBase Type
- `"document"` - Standard documents
- `"faq"` - FAQ pairs
- `"wiki"` - Wiki pages

## Tool Choices

### Available Tools (Agents can use)
- `"knowledge_search"` → VectorRetrieverType (semantic)
- `"grep_chunks"` → KeywordsRetrieverType (exact match)
- `"list_knowledge_chunks"` → Direct doc access
- `"query_knowledge_graph"` → Graph search
- `"web_search"` → WebSearchRetrieverType
- `"wiki_search"` → Wiki search
- [... and more]

**Agent decides which tool → System routes to right backend**

## Configuration Precedence (lowest to highest)

1. **System default** (hardcoded)
   - `EmbeddingTopK = 50`
   - `VectorThreshold = 0.15`
   
2. **Knowledge Base config**
   - `KB.EmbeddingModelID` (which embedding model)
   - `KB.FAQConfig` (FAQ indexing strategy)
   
3. **Tenant-level RetrievalConfig**
   - `RetrievalConfig.EmbeddingTopK`
   - `RetrievalConfig.VectorThreshold`
   - `Tenant.EffectiveEngines` (which backends)
   
4. **Agent-level config**
   - `CustomAgentConfig.AllowedTools`
   - `CustomAgentConfig.RerankModelID`
   
5. **Session-level override**
   - `SessionAgentConfig.KnowledgeBases` (restrict KBs)
   
6. **Request-level**
   - Tool parameters (e.g., explicit `knowledge_base_ids`)

## Key Files

| Layer | File | Key Classes |
|-------|------|-------------|
| **Model** | `internal/types/agent.go` | `AgentConfig`, `SessionAgentConfig` |
| **Model** | `internal/types/custom_agent.go` | `CustomAgent`, `CustomAgentConfig` |
| **Model** | `internal/types/knowledgebase.go` | `KnowledgeBase` |
| **Model** | `internal/types/retrieval_config.go` | `RetrievalConfig` |
| **Model** | `internal/types/retriever.go` | `RetrieverType`, `RetrieverEngineType` |
| **Model** | `internal/types/search.go` | `SearchTarget`, `SearchTargets` |
| **Service** | `internal/application/service/session_agent_qa.go` | `buildAgentConfig`, `buildSearchTargets` |
| **Service** | `internal/application/service/knowledgebase_search.go` | `HybridSearch` |
| **Service** | `internal/application/service/retriever/composite.go` | `CompositeRetrieveEngine` |
| **Tool** | `internal/agent/tools/knowledge_search.go` | Knowledge search tool impl |
| **Tool** | `internal/agent/tools/definitions.go` | Tool registry, tool names |
| **Pipeline** | `internal/application/service/chat_pipeline/search.go` | `PluginSearch` (chat pipeline) |

## Common Scenarios

### Q: How does an agent know which KBs to search?
**A:** `AgentConfig.SearchTargets` - pre-computed at request entry point, contains all KBs the agent has access to (after resolving CustomAgent + SessionAgentConfig + permissions)

### Q: Can an agent search multiple KBs with one query?
**A:** Yes! If they share the same embedding model:
- `HybridSearch(kb1_id, params{KnowledgeBaseIDs: [kb1, kb2, kb3]})`
- Results are merged and reranked together

### Q: How are vector vs keyword results combined?
**A:** RRF (Reciprocal Rank Fusion) - mathematical fusion of ranks, then reranked

### Q: What decides: Elasticsearch vs Qdrant?
**A:** `Tenant.EffectiveEngines` - configured at tenant level, tells system which backend handles which retriever type

### Q: Can an agent override its KB list?
**A:** Partially - `SessionAgentConfig.KnowledgeBases` can restrict (not expand) at session level

### Q: Where is reranking configured?
**A:** 
- Agent level: `CustomAgentConfig.RerankModelID` (which model)
- Tenant level: `RetrievalConfig.RerankTopK`, `RerankThreshold` (result quality thresholds)
