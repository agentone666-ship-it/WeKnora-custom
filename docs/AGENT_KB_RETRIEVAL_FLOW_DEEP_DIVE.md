# Agent/KB Retrieval Flow - Deep Dive Analysis

## Overview

This document provides a technical deep-dive into how WeKnora agents use knowledge bases for information retrieval. It traces the complete flow from agent configuration through retrieval execution, showing exactly how agents dynamically select which knowledge bases to search and how retrieval strategies are applied.

## Table of Contents

1. [Configuration Hierarchy](#configuration-hierarchy)
2. [Request Entry Point](#request-entry-point)
3. [Search Target Resolution](#search-target-resolution)
4. [Runtime Agent Configuration](#runtime-agent-configuration)
5. [Tool Registration with SearchTargets](#tool-registration-with-searchtargets)
6. [Retrieval Pipeline Execution](#retrieval-pipeline-execution)
7. [Cross-Tenant KB Sharing](#cross-tenant-kb-sharing)
8. [Configuration Precedence](#configuration-precedence)
9. [Key Decision Points](#key-decision-points)

---

## Configuration Hierarchy

### Three-Level Configuration System

WeKnora uses a three-tier configuration system that resolves from most specific to most general:

```
┌─────────────────────────────────────────────┐
│ 1. Request-Level Override (Most Specific)    │
│    - SummaryModelID, KnowledgeBaseIDs, etc.  │
│    - Session-level web search enabled flag   │
└─────────────────────────────────────────────┘
           ↓ (if not provided)
┌─────────────────────────────────────────────┐
│ 2. Agent-Level Configuration (CustomAgent)   │
│    - CustomAgent.Config.ModelID              │
│    - CustomAgent.Config.KBSelectionMode      │
│    - CustomAgent.Config.AllowedTools         │
│    - CustomAgent.Config.Temperature, etc.    │
└─────────────────────────────────────────────┘
           ↓ (if not provided)
┌─────────────────────────────────────────────┐
│ 3. Tenant-Level Defaults (Most General)      │
│    - RetrievalConfig thresholds              │
│    - Tenant default models                   │
│    - WebSearchConfig                         │
└─────────────────────────────────────────────┘
```

### Configuration Objects

#### CustomAgent (Persistent, DB-Stored)
Located: `internal/types/custom_agent.go`

```go
type CustomAgent struct {
    ID          string                 // UUID or builtin-* ID
    Name        string
    TenantID    uint64                 // Agent owner (composite key)
    Config      CustomAgentConfig      // JSONB configuration
    CreatedAt   time.Time
}

type CustomAgentConfig struct {
    // KB Selection
    KBSelectionMode string    // "all", "selected", or "none"
    KnowledgeBases  []string  // IDs when mode="selected"
    
    // Retrieval
    ModelID         string
    RerankModelID   string
    AllowedTools    []string  // Which tools can be called
    
    // Strategy
    EmbeddingTopK       int
    VectorThreshold     float64
    KeywordThreshold    float64
    RerankTopK          int
    RerankThreshold     float64
    
    // Agent Mode
    MaxIterations   int
    Temperature     float64
    MultiTurnEnabled bool
    HistoryTurns    int
    
    // Advanced
    RetrieveKBOnlyWhenMentioned bool  // Only search KB if @mentioned
    WebSearchEnabled            bool
    WebSearchMaxResults         int
}
```

#### AgentConfig (Runtime Resolution)
Located: `internal/types/agent.go`

This is built at request time from CustomAgent + TenantInfo:

```go
type AgentConfig struct {
    MaxIterations              int
    AllowedTools              []string
    Temperature               float64
    KnowledgeBases            []string        // Resolved KB IDs
    KnowledgeIDs              []string        // Resolved file IDs
    SystemPrompt              string
    WebSearchEnabled          bool
    WebSearchMaxResults       int
    MultiTurnEnabled          bool
    HistoryTurns              int
    SearchTargets             SearchTargets   // ← Pre-computed HERE
    RetrieveKBOnlyWhenMentioned bool
    // ... more fields
}
```

#### RetrievalConfig (Tenant-Level)
Located: `internal/types/retrieval_config.go`

Stored as JSONB on tenants table:

```go
type RetrievalConfig struct {
    EmbeddingTopK       int       // default: 50
    VectorThreshold     float64   // default: 0.15
    KeywordThreshold    float64   // default: 0.3
    RerankTopK          int       // default: 10
    RerankThreshold     float64   // default: 0.2
    RerankModelID       string
}
```

---

## Request Entry Point

### AgentQA Handler Flow

Located: `internal/application/service/session_agent_qa.go`

**Key Function**: `func (s *sessionService) AgentQA(ctx context.Context, req *types.QARequest, eventBus *event.EventBus) error`

Entry point receives:
- `req.CustomAgent` - The agent being used (required for AgentQA)
- `req.Query` - User question
- `req.KnowledgeBaseIDs` - Optional @mentions (overrides agent config)
- `req.KnowledgeIDs` - Specific file @mentions
- `req.Session.TenantID` - Session owner tenant
- `req.WebSearchEnabled` - Session-level web search override

### Sequence of Operations

```
1. Validate customAgent is provided (line 36)
   ↓
2. Resolve retrieval tenant ID (line 42)
   - Returns agent's tenant if agent is from different org
   - Otherwise returns session tenant
   ↓
3. Load tenant info (lines 51-62)
   - May fetch agent's tenant if different from session tenant
   ↓
4. Build runtime AgentConfig (line 68)
   - This is where SearchTargets are computed
   ↓
5. Resolve effective model ID (line 79)
   ↓
6. Create agent engine (line 153)
   - Passes AgentConfig with SearchTargets
   ↓
7. Execute agent (line 191)
```

---

## Search Target Resolution

### What are SearchTargets?

Located: `internal/types/search.go`

```go
type SearchTarget struct {
    Type            SearchTargetType  // "knowledge_base" or "knowledge"
    KnowledgeBaseID string            // KB to search
    TenantID        uint64            // Which tenant owns this KB
    KnowledgeIDs    []string          // Specific files (only for "knowledge" type)
}

type SearchTargets []*SearchTarget
```

**Two Target Types**:
- `SearchTargetTypeKnowledgeBase` ("knowledge_base"): Search entire KB
- `SearchTargetTypeKnowledge` ("knowledge"): Search specific files

### buildSearchTargets Function

Located: `internal/application/service/session_knowledge_qa.go:393`

This function is **critical** - it's called once at request entry and pre-computes all search targets.

**Inputs**:
- `knowledgeBaseIDs` - Full KB IDs to search
- `knowledgeIDs` - Specific file IDs to search

**Logic**:

```
PASS 1: Full Knowledge Bases
├─ Batch-fetch KB records
├─ For each KB:
│  ├─ Resolve actual TenantID (own, shared, or agent's)
│  ├─ Check user has permission (for shared KBs)
│  └─ Create SearchTarget with Type=KNOWLEDGE_BASE
└─ Track which KBs are fully covered

PASS 2: Specific Files (KnowledgeIDs)
├─ Batch-fetch Knowledge records
├─ For each Knowledge:
│  ├─ Find its parent KnowledgeBaseID
│  ├─ Skip if parent KB already fully covered
│  ├─ Group by KnowledgeBaseID
│  └─ Create SearchTarget with Type=KNOWLEDGE + KnowledgeIDs list
└─ Result: One SearchTarget per unique KB, with specific file lists
```

**Key Algorithm Detail**:

If user passes:
```
KnowledgeBaseIDs: ["kb-1", "kb-2"]
KnowledgeIDs: ["file-3", "file-4", "file-5"]
```

Where file-3 and file-4 are in kb-1, and file-5 is in kb-3:

Result SearchTargets:
```
[
  {Type: KNOWLEDGE_BASE, KnowledgeBaseID: "kb-1"},      // Entire kb-1
  {Type: KNOWLEDGE_BASE, KnowledgeBaseID: "kb-2"},      // Entire kb-2
  {Type: KNOWLEDGE, KnowledgeBaseID: "kb-3", KnowledgeIDs: ["file-5"]}
]
```

Note: file-3 and file-4 are NOT created as separate targets because kb-1 is already being fully searched.

---

## Runtime Agent Configuration

### buildAgentConfig Function

Located: `internal/application/service/session_agent_qa.go:210`

**Input**: `CustomAgent` + `TenantInfo` + `QARequest`

**Output**: Runtime `AgentConfig` with pre-computed SearchTargets

**Steps**:

```go
1. Create base AgentConfig from CustomAgent.Config
   - Copy: MaxIterations, Temperature, WebSearchEnabled, etc.
   - Copy AllowedTools (or use defaults if empty)

2. Call EnsureDefaults() (line 65)
   - Sets missing values to hardcoded defaults

3. Resolve knowledge base IDs (line 242)
   agentConfig.KnowledgeBases = s.resolveKnowledgeBases(ctx, req)
   
   This function (session_qa_helpers.go:19) implements KB resolution priority:
   
   Priority 1: Explicit @mentions in request
     if req.KnowledgeBaseIDs or req.KnowledgeIDs provided
     └─ Use those (but validate against agent scope for shared agents)
   
   Priority 2: RetrieveKBOnlyWhenMentioned override
     if customAgent.Config.RetrieveKBOnlyWhenMentioned && no @mention
     └─ Clear the KB list (KB disabled for this request)
   
   Priority 3: Agent's configured KBs
     else
     └─ Resolve based on KBSelectionMode:
        ├─ "all": List all KBs (own + shared for non-shared agents)
        ├─ "selected": Use customAgent.Config.KnowledgeBases
        ├─ "none": Return empty
        └─ default: Fallback to KnowledgeBases for backward compat

4. Merge with tenant config (lines 260-273)
   - Set WebSearchMaxResults from tenant if not set
   - Resolve web search provider ID

5. Build SearchTargets (line 286)
   searchTargets, err := s.buildSearchTargets(ctx, agentTenantID, 
                                              agentConfig.KnowledgeBases, 
                                              agentConfig.KnowledgeIDs)
   agentConfig.SearchTargets = searchTargets
```

### resolveKnowledgeBasesFromAgent Function

Located: `internal/application/service/session_knowledge_qa.go:320`

This function implements the KB selection mode logic:

```go
func (s *sessionService) resolveKnowledgeBasesFromAgent(
    ctx context.Context,
    customAgent *types.CustomAgent,
    sessionTenantID uint64,
) []string {
    switch customAgent.Config.KBSelectionMode {
    
    case "all":
        // Fetch all KBs from agent's tenant
        allKBs, _ := s.knowledgeBaseService.ListKnowledgeBases(ctx)
        
        // For non-shared agents: also include user's shared KBs
        isSharedAgent := sessionTenantID != 0 && 
                       sessionTenantID != customAgent.TenantID
        if !isSharedAgent {
            sharedList, _ := s.kbShareService.ListSharedKnowledgeBases(
                ctx, userID, tenantID)
            // Merge shared KBs into allKBs
        }
        return allKBIDs
        
    case "selected":
        // Use explicitly configured list
        return customAgent.Config.KnowledgeBases
        
    case "none":
        return nil
        
    default:
        // Backward compat: use configured KBs
        return customAgent.Config.KnowledgeBases
    }
}
```

**Critical Detail**: Shared agent detection (line 346)
```go
isSharedAgent := sessionTenantID != 0 && 
               sessionTenantID != customAgent.TenantID
```

When `isSharedAgent` is true:
- ONLY use agent's tenant KBs
- DO NOT include user's shared KBs (security)
- This prevents data leak across organizations

---

## Tool Registration with SearchTargets

### Where SearchTargets Flow

The pre-computed SearchTargets from AgentConfig are passed to EVERY tool that needs to search:

Located: `internal/application/service/agent_service.go:318`

**Tool Registration**:

```go
func (s *agentService) registerTools(ctx context.Context, 
    registry *tools.ToolRegistry,
    config *types.AgentConfig,  // ← Contains SearchTargets
    rerankModel rerank.Reranker,
    chatModel chat.Chat,
    sessionID string) error {
    
    // ... determine allowedTools ...
    
    for _, toolName := range allowedTools {
        switch toolName {
        
        case tools.ToolKnowledgeSearch:
            // SearchTargets passed directly to tool
            toolToRegister = tools.NewKnowledgeSearchTool(
                s.knowledgeBaseService,
                s.knowledgeService,
                s.chunkService,
                config.SearchTargets,     // ← HERE
                rerankModel,
                chatModel,
                s.cfg,
            )
            
        case tools.ToolGrepChunks:
            toolToRegister = tools.NewGrepChunksTool(
                s.db, 
                config.SearchTargets,     // ← HERE
            )
            
        case tools.ToolListKnowledgeChunks:
            toolToRegister = tools.NewListKnowledgeChunksTool(
                s.knowledgeService, 
                s.chunkService, 
                config.SearchTargets,     // ← HERE
            )
            
        // ... similar for other KB tools ...
        }
    }
}
```

### KnowledgeSearchTool Initialization

Located: `internal/agent/tools/knowledge_search.go:133`

```go
type KnowledgeSearchTool struct {
    BaseTool
    knowledgeBaseService interfaces.KnowledgeBaseService
    knowledgeService     interfaces.KnowledgeService
    chunkService         interfaces.ChunkService
    searchTargets        types.SearchTargets    // ← Stored here
    rerankModel          rerank.Reranker
    chatModel            chat.Chat
    config               *config.Config
}

func NewKnowledgeSearchTool(..., searchTargets types.SearchTargets, ...) {
    return &KnowledgeSearchTool{
        // ...
        searchTargets: searchTargets,  // ← Captured at creation
        // ...
    }
}
```

### Tool Execution Uses SearchTargets

When agent calls knowledge_search tool at runtime:

Located: `internal/agent/tools/knowledge_search.go:155`

```go
func (t *KnowledgeSearchTool) Execute(
    ctx context.Context, 
    args json.RawMessage) (*types.ToolResult, error) {
    
    // Parse input
    var input KnowledgeSearchInput
    json.Unmarshal(args, &input)
    
    // Use pre-computed search targets
    searchTargets := t.searchTargets  // ← Line 180
    
    // Allow user to optionally filter to specific KBs
    if len(input.KnowledgeBaseIDs) > 0 {
        // Filter searchTargets to only include user-specified KBs
        userKBSet := make(map[string]bool)
        for _, kbID := range input.KnowledgeBaseIDs {
            userKBSet[kbID] = true
        }
        var filteredTargets types.SearchTargets
        for _, target := range t.searchTargets {
            if userKBSet[target.KnowledgeBaseID] {
                filteredTargets = append(filteredTargets, target)
            }
        }
        searchTargets = filteredTargets
    }
    
    // Get all KB IDs from targets
    kbIDs := searchTargets.GetAllKnowledgeBaseIDs()  // Line 205
    
    // Execute concurrent search using targets
    allResults := t.concurrentSearchByTargets(
        ctx, 
        input.Queries, 
        searchTargets,    // ← Passed to search executor
        topK, 
        vectorThreshold, 
        keywordThreshold, 
        kbTypeMap)
    
    // ... reranking, MMR, dedup ...
    
    return toolResult, nil
}
```

---

## Retrieval Pipeline Execution

### Two Parallel Search Paths

In the chat pipeline (non-agent mode):

Located: `internal/application/service/chat_pipeline/search.go:61`

```go
func (p *PluginSearch) OnEvent(ctx context.Context,
    eventType types.EventType, 
    chatManage *types.ChatManage, 
    next func() *PluginError) *PluginError {
    
    // Run KB search and web search CONCURRENTLY
    var wg sync.WaitGroup
    
    // Goroutine 1: Knowledge Base Search
    go func() {
        kbResults := p.searchByTargets(ctx, chatManage)
    }()
    
    // Goroutine 2: Web Search
    go func() {
        webResults := p.searchWebIfEnabled(ctx, chatManage)
    }()
}
```

### SearchByTargets Execution

Located: `internal/application/service/chat_pipeline/search.go:312`

This function orchestrates KB retrieval using SearchTargets:

```go
func (p *PluginSearch) searchByTargets(
    ctx context.Context,
    chatManage *types.ChatManage) []*types.SearchResult {
    
    // Step 1: Group targets by embedding model (optimization)
    // Targets with same embedding model share one embedding computation
    groups := groupTargetsByEmbeddingModel(
        ctx, 
        chatManage.SearchTargets,
        p.knowledgeBaseService)
    
    // Step 2: For each model group, execute search
    for modelKey, targets := range groups {
        // Compute embedding once for all targets in group
        queryEmbedding, _ := p.knowledgeBaseService.GetQueryEmbedding(
            ctx, targets[0].KnowledgeBaseID, queryText)
        
        // Separate full-KB from specific-knowledge targets
        var fullKBIDs []string
        var knowledgeTargets []*types.SearchTarget
        for _, t := range targets {
            if t.Type == types.SearchTargetTypeKnowledgeBase {
                fullKBIDs = append(fullKBIDs, t.KnowledgeBaseID)
            } else {
                knowledgeTargets = append(knowledgeTargets, t)
            }
        }
        
        // Execute combined search for full KBs (all in one call)
        if len(fullKBIDs) > 0 {
            params := types.SearchParams{
                QueryText:        queryText,
                QueryEmbedding:   queryEmbedding,
                KnowledgeBaseIDs: fullKBIDs,  // ← Multiple KBs combined
                VectorThreshold:  chatManage.VectorThreshold,
                KeywordThreshold: chatManage.KeywordThreshold,
                MatchCount:       chatManage.EmbeddingTopK,
            }
            res, _ := p.knowledgeBaseService.HybridSearch(
                ctx, fullKBIDs[0], params)
            // ← HybridSearch with multiple KBs returns unified results
        }
        
        // Execute individual search for specific-knowledge targets
        for _, target := range knowledgeTargets {
            p.searchSingleTarget(ctx, chatManage, target, 
                               queryText, queryEmbedding, ...)
        }
    }
    
    return allResults
}
```

### HybridSearch with Multiple KBs

Located: `internal/application/service/knowledgebase_search.go`

The HybridSearch function orchestrates parallel vector + keyword retrieval:

```go
func (s *Service) HybridSearch(
    ctx context.Context,
    mainKBID string,
    params types.SearchParams) ([]*types.SearchResult, error) {
    
    // params.KnowledgeBaseIDs can contain MULTIPLE KB IDs
    searchKBIDs := params.KnowledgeBaseIDs
    if len(searchKBIDs) == 0 {
        searchKBIDs = []string{mainKBID}
    }
    
    // Create CompositeRetrieveEngine for all KBs
    engine := retriever.NewCompositeRetrieveEngine(
        s.retrieverRegistry, 
        s.getTenantRetrieverEngines(ctx))
    
    // Execute PARALLEL vector + keyword search
    var wg sync.WaitGroup
    
    // Vector search
    go func() {
        vectorResults, _ := engine.Retrieve(ctx, 
            types.RetrieveParams{
                RetrieverType:   types.VectorRetrieverType,
                KnowledgeBaseIDs: searchKBIDs,  // All KBs
                EmbeddingTopK:    params.MatchCount * 5,  // Over-retrieve
                QueryEmbedding:   params.QueryEmbedding,
                VectorThreshold:  params.VectorThreshold,
            })
    }()
    
    // Keyword search
    go func() {
        keywordResults, _ := engine.Retrieve(ctx,
            types.RetrieveParams{
                RetrieverType:   types.KeywordsRetrieverType,
                KnowledgeBaseIDs: searchKBIDs,  // All KBs
                EmbeddingTopK:    params.MatchCount * 5,
                QueryText:        params.QueryText,
                KeywordThreshold: params.KeywordThreshold,
            })
    }()
    
    wg.Wait()
    
    // Merge using RRF (Reciprocal Rank Fusion)
    mergedResults := rrf(vectorResults, keywordResults)
    
    // Rerank
    if rerankModel != nil {
        rerankedResults := rerank(mergedResults, 
                                 params.QueryText, 
                                 rerankModel)
    }
    
    return rerankedResults, nil
}
```

---

## Cross-Tenant KB Sharing

### Shared Agent Scenario

When `sessionTenantID != customAgent.TenantID`:

Located: `internal/application/service/session_qa_helpers.go:37`

```go
// When using a shared agent, restrict @mentions to the agent's allowed KB scope
// to prevent users from injecting KB/knowledge IDs outside the agent's configured range.
if customAgent != nil && 
   req.Session != nil && 
   req.Session.TenantID != customAgent.TenantID {
    kbIDs, knowledgeIDs = s.restrictMentionsToAgentScope(
        ctx, customAgent, req.Session.TenantID, kbIDs, knowledgeIDs)
}
```

### Permission Validation

Located: `internal/application/service/session_knowledge_qa.go:393`

In buildSearchTargets:

```go
// For each KB in the request
kb := kbByID[kbID]
if kb == nil {
    // Not found, default to session tenant
    kbTenantMap[kbID] = tenantID
} else if kb.TenantID == tenantID {
    // Own KB
    kbTenantMap[kbID] = tenantID
} else if s.kbShareService != nil && userID != "" {
    // Cross-tenant shared KB - check permission
    hasAccess, _ := s.kbShareService.HasKBPermission(
        ctx, kbID, userID, types.OrgRoleViewer)
    if hasAccess {
        kbTenantMap[kbID] = kb.TenantID  // Use original owner
    } else {
        kbTenantMap[kbID] = tenantID     // Fallback
    }
} else {
    kbTenantMap[kbID] = tenantID
}
```

### SearchTarget Tenant Tracking

Each SearchTarget includes TenantID:

```go
targets = append(targets, &types.SearchTarget{
    Type:            types.SearchTargetTypeKnowledgeBase,
    KnowledgeBaseID: kbID,
    TenantID:        kbTenantMap[kbID],  // ← Track owner
})
```

The retrieval system uses this to scope searches correctly.

---

## Configuration Precedence

### Complete Priority Chain

```
Request Layer
  └─ req.SummaryModelID (overrides agent model)
  └─ req.KnowledgeBaseIDs (overrides agent KB config)
  └─ req.KnowledgeIDs (specific files)
  └─ req.WebSearchEnabled (request-level override)
  
        ↓ (if not provided)
        
Agent Layer (CustomAgent.Config)
  └─ ModelID (chat model for agent)
  └─ RerankModelID (rerank model)
  └─ AllowedTools (which tools available)
  └─ KBSelectionMode + KnowledgeBases
  └─ Temperature, MaxIterations
  └─ EmbeddingTopK, VectorThreshold, KeywordThreshold
  └─ WebSearchEnabled, WebSearchMaxResults
  └─ RetrieveKBOnlyWhenMentioned
  └─ SystemPrompt, MultiTurnEnabled
  
        ↓ (if not provided)
        
Tenant Layer (Tenant.RetrievalConfig)
  └─ EmbeddingTopK (default: 50)
  └─ VectorThreshold (default: 0.15)
  └─ KeywordThreshold (default: 0.3)
  └─ RerankTopK (default: 10)
  └─ RerankThreshold (default: 0.2)
  └─ RerankModelID
  
        ↓ (if not provided)
        
System Defaults
  └─ EmbeddingTopK: 50
  └─ VectorThreshold: 0.15
  └─ KeywordThreshold: 0.3
  └─ RerankTopK: 10
  └─ Temperature: 0.7
  └─ MaxIterations: 10
```

### Example: KBSelectionMode Resolution

```go
// resolveKnowledgeBases (session_qa_helpers.go:19)

hasExplicitMention := len(kbIDs) > 0 || len(knowledgeIDs) > 0

if hasExplicitMention {
    // Priority 1: Use request-specified targets
    kbIDs, knowledgeIDs = req.KnowledgeBaseIDs, req.KnowledgeIDs
} else if customAgent.Config.RetrieveKBOnlyWhenMentioned {
    // Priority 2: Mode flag disables KB for this request
    kbIDs, knowledgeIDs = nil, nil
} else {
    // Priority 3: Use agent's configured KBs based on mode
    kbIDs = resolveKnowledgeBasesFromAgent(customAgent, sessionTenantID)
}
```

---

## Key Decision Points

### 1. When is SearchTargets Computed?

**Answer**: Once at request entry, in `buildAgentConfig()` (line 286)

**Why**: Avoids recomputation on every tool call

**Flow**:
```
AgentQA() → buildAgentConfig() → buildSearchTargets() → agentConfig.SearchTargets
                                                              ↓
                                        CreateAgentEngine() → registerTools() →
                                        All tools capture SearchTargets at init
```

### 2. How Does Agent Choose Which Tool to Call?

**Answer**: The LLM decides which tool, but only allowed ones are available

```
// agent_service.go:328-334
var allowedTools []string
if len(config.AllowedTools) > 0 {
    allowedTools = config.AllowedTools  // ← Agent specifies
} else {
    allowedTools = tools.DefaultAllowedTools()
}

// ← These are the ONLY tools available to LLM
```

The agent.AllowedTools directly constrains what the LLM can call.

### 3. Can Agent Override Knowledge Bases?

**Partially - Depends on Configuration**:

```
If RetrieveKBOnlyWhenMentioned == true:
    └─ KB searches ONLY work if user explicitly mentions KB with @
    └─ No automatic KB retrieval
    
If KBSelectionMode == "selected":
    └─ Agent is LOCKED to configured KnowledgeBases
    └─ @mentions outside this set are rejected
    └─ Exception: Within search tool, user can filter to subset
    
If KBSelectionMode == "all":
    └─ Agent CAN search any KB user has access to
    └─ But searchTargets still pre-computed from "all"
```

### 4. What if CustomAgent.Config.AllowedTools is empty?

**Answer**: Use defaults (line 333)

```go
if len(config.AllowedTools) > 0 {
    allowedTools = config.AllowedTools
} else {
    allowedTools = tools.DefaultAllowedTools()  // ← USES DEFAULTS
}
```

Default allowed tools: `ToolThinking, ToolTodoWrite, ToolKnowledgeSearch, ToolGrepChunks, ToolListKnowledgeChunks, ToolQueryKnowledgeGraph, ToolGetDocumentInfo, ToolDatabaseQuery, ToolDataAnalysis, ToolDataSchema, ToolFinalAnswer`

### 5. How are Multiple KBs Searched Efficiently?

**Answer**: Group by embedding model, batch the search

Located: `search.go:312`

```
1. Group SearchTargets by embedding model identity
   └─ Targets with same model = one embedding computation

2. For each group:
   a. Compute query embedding ONCE
   b. Separate full-KB targets from specific-knowledge targets
   c. Combined search: HybridSearch(allFullKBIDs)
      └─ Searches ALL KBs in ONE retrieval call
      └─ Returns merged results from all KBs
   d. Individual search: One HybridSearch per specific-knowledge target
```

**Result**: If 5 KBs use same embedding model:
- Traditional: 5 embedding computations, 5 retrieval calls
- Optimized: 1 embedding computation, 2 retrieval calls (combined full + individuals)

---

## Summary

### Configuration Resolution Flow

```
QARequest
    ↓
resolveKnowledgeBases()
    ├─ Check: explicit @mention? → YES: use request KBs
    ├─ Check: RetrieveKBOnlyWhenMentioned? → YES: clear KBs
    └─ Otherwise: resolveKnowledgeBasesFromAgent()
        ├─ KBSelectionMode="all" → List all (own + shared)
        ├─ KBSelectionMode="selected" → Use CustomAgent.Config.KnowledgeBases
        └─ KBSelectionMode="none" → Empty list
    ↓
buildSearchTargets(ctx, tenantID, kbIDs, knowledgeIDs)
    ├─ PASS 1: Full KB → SearchTarget(KNOWLEDGE_BASE, kbID)
    ├─ PASS 2: Specific files → SearchTarget(KNOWLEDGE, kbID, [fileIDs])
    └─ Track TenantID per target (for cross-tenant sharing)
    ↓
agentConfig.SearchTargets = results
    ↓
registerTools(..., agentConfig)
    └─ Each tool receives: agentConfig.SearchTargets
    ↓
At runtime, when agent calls tool:
    └─ Tool uses pre-computed SearchTargets
    └─ Tool can filter by user-specified KB subset
    └─ Executes HybridSearch with targets
```

### Key Invariants

1. **SearchTargets computed once**: At `buildAgentConfig()` entry
2. **All KB tools constrained by SearchTargets**: Via constructor injection
3. **Permission checked at build time**: Not at retrieval time
4. **Cross-tenant sharing validated**: In `buildSearchTargets()`
5. **Shared agents cannot leak KBs**: `isSharedAgent` detection
6. **Configuration is immutable after engine creation**: No runtime config changes
