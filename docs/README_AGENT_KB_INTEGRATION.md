# Agent & Knowledge Base Integration Documentation

This directory contains comprehensive documentation on how agents/bots use knowledge bases in WeKnora, including model definitions, retrieval configuration, and the complete retrieval pipeline.

## Documents

### 1. [AGENT_KB_RETRIEVAL_ARCHITECTURE.md](./AGENT_KB_RETRIEVAL_ARCHITECTURE.md) - **Deep Dive (581 lines)**
The comprehensive reference guide covering:
- Agent/Bot model definitions (CustomAgent, AgentConfig, SessionAgentConfig)
- Knowledge base binding mechanisms
- Retrieval strategy configuration (RetrievalConfig, RetrieverType, RetrieverEngineType)
- Complete retrieval pipeline stages (1-7)
- SearchTargets computation and usage
- Agent-level retrieval preferences
- Key decision flows
- Data flow diagram
- Configuration precedence
- Complete file manifest

**Best for**: Understanding the complete architecture, implementation details, and data flows.

### 2. [AGENT_KB_QUICK_REFERENCE.md](./AGENT_KB_QUICK_REFERENCE.md) - **Quick Guide (286 lines)**
A practical, scenario-focused guide including:
- Core data structures with tree diagrams
- Step-by-step KB access flow
- Enumeration reference (RetrieverType, RetrieverEngineType, etc.)
- Tool choices and routing
- Configuration precedence summary
- Key files lookup table
- Common Q&A scenarios

**Best for**: Quick lookups, implementation decisions, and troubleshooting.

---

## Key Concepts at a Glance

### The Agent-KB Link

**Agents reference KBs through**:
```
CustomAgent.Config.AvailableKnowledgeBases[]
    ↓
buildAgentConfig() resolves to
    ↓
AgentConfig.KnowledgeBases[]
    ↓
buildSearchTargets() converts to
    ↓
AgentConfig.SearchTargets[] (unified search scope)
    ↓
Used by retrieval tools at execution time
```

### Retrieval Strategy Decision Path

1. **Which KBs?** → `AgentConfig.SearchTargets` (pre-computed)
2. **Which retrieval type?** → Agent decides (tool choice: knowledge_search vs grep_chunks)
3. **Which backend?** → `Tenant.EffectiveEngines` routes RetrieverType to engine
4. **Result quality?** → `RetrievalConfig` (thresholds, reranking)

### Search Pipeline Overview

```
Agent Tool Call
    ↓
HybridSearch (orchestration)
    ├→ Vector search (via embedding model)
    ├→ Keyword search (via full-text index)
    ├→ RRF fusion
    └→ Reranking
        ↓
CompositeRetrieveEngine (backend dispatch)
    ├→ "vector" type → Qdrant/Milvus/Weaviate/Elasticsearch
    ├→ "keywords" type → Elasticsearch/PostgreSQL/SQLite
    └→ "websearch" type → External provider
        ↓
Backend-specific retrieval
    ↓
Results merged, fused, reranked
    ↓
Agent uses in reasoning loop
```

---

## File Organization

### Core Model Files

| Path | Purpose | Key Classes |
|------|---------|-------------|
| `internal/types/agent.go` | Agent runtime config | `AgentConfig`, `SessionAgentConfig` |
| `internal/types/custom_agent.go` | Agent definition | `CustomAgent`, `CustomAgentConfig` |
| `internal/types/knowledgebase.go` | KB model & types | `KnowledgeBase` (document/faq/wiki) |
| `internal/types/retrieval_config.go` | Tenant retrieval settings | `RetrievalConfig` |
| `internal/types/retriever.go` | Retriever type enums | `RetrieverType`, `RetrieverEngineType` |
| `internal/types/search.go` | Search models | `SearchTarget`, `SearchTargets`, `SearchResult` |

### Service/Pipeline Files

| Path | Purpose | Key Functions |
|------|---------|----------------|
| `internal/application/service/session_agent_qa.go` | Agent QA entry point | `buildAgentConfig()`, `buildSearchTargets()` |
| `internal/application/service/knowledgebase_search.go` | Search orchestration | `HybridSearch()` |
| `internal/application/service/retriever/composite.go` | Backend dispatcher | `CompositeRetrieveEngine` |
| `internal/agent/tools/knowledge_search.go` | Vector search tool | Knowledge search implementation |
| `internal/agent/tools/definitions.go` | Tool registry | Tool names and availability |
| `internal/application/service/chat_pipeline/search.go` | Chat pipeline search | `PluginSearch` |

---

## Enumerations Quick Ref

### RetrieverType (What)
- `"vector"` - Semantic/embedding search
- `"keywords"` - Exact keyword/full-text search
- `"websearch"` - External web search

### RetrieverEngineType (Where)
- `"elasticsearch"` - Elasticsearch backend
- `"postgres"` - PostgreSQL backend
- `"qdrant"` - Qdrant vector DB
- `"milvus"` - Milvus vector DB
- `"weaviate"` - Weaviate vector DB
- `"infinity"` - Infinity vector DB
- `"elasticfaiss"` - FAISS backend
- `"sqlite"` - SQLite backend

### SearchTargetType
- `"knowledge_base"` - Search entire KB
- `"knowledge"` - Search specific files within KB

### KnowledgeBase.Type
- `"document"` - Standard document-based
- `"faq"` - Question-answer pairs
- `"wiki"` - Wiki pages

---

## Configuration Hierarchy

From lowest to highest priority:

1. **System defaults** (hardcoded in code)
2. **Knowledge Base config** (EmbeddingModelID, FAQConfig, etc.)
3. **Tenant-level RetrievalConfig** (thresholds, top-K, reranker)
4. **Agent-level config** (AllowedTools, RerankModelID)
5. **Session-level override** (KnowledgeBases restriction)
6. **Request/Tool-level** (explicit parameters)

---

## Common Implementation Patterns

### Pattern 1: Agent Access to Multiple KBs
```go
// If KBs share embedding model:
HybridSearch(kb1_id, params{
    KnowledgeBaseIDs: [kb1, kb2, kb3],  // Search all 3
    QueryText: query,
    // ... results merged and reranked together
})
```

### Pattern 2: Scoped Document Search
```go
// Search only specific documents within a KB:
SearchTarget{
    Type: "knowledge",
    KnowledgeBaseID: kb_id,
    KnowledgeIDs: [doc1, doc2],  // Only these files
}
```

### Pattern 3: Backend Selection
```go
// CompositeRetrieveEngine routes automatically:
// RetrrieverType="vector" → Qdrant (if configured)
// RetrieverType="keywords" → Elasticsearch (if configured)
```

### Pattern 4: Result Fusion
```go
// Vector + Keyword results merged via RRF:
vectorScores = [query1: 0.9, query2: 0.8]
keywordScores = [query2: 0.85, query3: 0.7]
fusedScores = RRF(vectorScores, keywordScores)
// Then reranked
```

---

## Related Documentation

- **Agent Architecture**: See `docs/AGENT_ARCHITECTURE.md` (if exists)
- **Chat Pipeline**: See `docs/CHAT_PIPELINE.md` (if exists)
- **Knowledge Base Management**: See `docs/KNOWLEDGE_BASE.md` (if exists)
- **API Documentation**: See `docs/api/` directory

---

## Quick Start: Adding a New Retrieval Strategy

1. **Create retriever type**: Add constant to `RetrieverType` enum in `internal/types/retriever.go`
2. **Register backend**: Implement `RetrieveEngineService` interface
3. **Configure at tenant level**: Update `Tenant.EffectiveEngines` to include new type
4. **Agent tool**: Create tool that sets `params.RetrieverType` to new type
5. **CompositeRetrieveEngine**: Automatically routes based on type

---

## FAQ

**Q: Where is KB access control enforced?**  
A: Multiple places:
- `buildSearchTargets()` checks permissions via `kbShareService`
- Session initialization validates agent access
- Request handler validates user/session authorization

**Q: How are cross-tenant shared KBs handled?**  
A: 
- `SearchTarget.TenantID` tracks which tenant owns each KB
- HybridSearch handles tenant isolation
- Permissions resolved at buildSearchTargets time

**Q: Can I change retrieval strategy per-agent?**  
A: Indirectly - via `AllowedTools` (which tools agent can use) and tool implementation

**Q: How does reranking work?**  
A: 
- Tenant specifies `RetrievalConfig.RerankModelID`
- HybridSearch applies after RRF fusion
- Results filtered by `RerankThreshold`
- Limited to `RerankTopK` results

**Q: What's the difference between vector and keyword search?**  
A: 
- **Vector**: Semantic/conceptual similarity (embeddings)
- **Keyword**: Exact text matching/full-text search (BM25, ES)

---

## Contributing

When modifying agent or KB integration:
1. Update relevant section in both documents
2. Keep Quick Reference as 1-page reference, Architecture as comprehensive guide
3. Update files list if adding new source files
4. Document new enumerations/types
5. Test configuration precedence scenarios

---

Generated: 2026-04-20  
Last Updated: See git history
