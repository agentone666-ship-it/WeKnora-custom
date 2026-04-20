# Agent/KB Integration - Implementation Patterns

This document provides practical implementation patterns and code walkthroughs for working with the agent/KB system.

## Table of Contents

1. [Creating a Custom Agent](#creating-a-custom-agent)
2. [Configuring Agent Knowledge Bases](#configuring-agent-knowledge-bases)
3. [Common Configuration Scenarios](#common-configuration-scenarios)
4. [Retrieving Search Results](#retrieving-search-results)
5. [Debugging Configuration Issues](#debugging-configuration-issues)

---

## Creating a Custom Agent

### Agent Creation Flow

```go
// From handler layer (e.g., HTTP POST /agents)
customAgent := &types.CustomAgent{
    ID:       uuid.New().String(),
    TenantID: tenantID,
    Name:     "Research Assistant",
    Config: types.CustomAgentConfig{
        // KB Configuration
        KBSelectionMode: "selected",
        KnowledgeBases:  []string{"kb-001", "kb-002"},
        
        // Model Configuration
        ModelID:       "model-gpt4",
        RerankModelID: "model-rerank",
        
        // Agent Mode Configuration
        AgentMode:      types.AgentModeSmartReasoning,
        MaxIterations:  15,
        Temperature:    0.7,
        
        // Tool Configuration
        AllowedTools: []string{
            tools.ToolKnowledgeSearch,
            tools.ToolGrepChunks,
            tools.ToolWebSearch,
            tools.ToolFinalAnswer,
        },
        
        // Retrieval Strategy
        EmbeddingTopK:   20,
        VectorThreshold: 0.4,
        KeywordThreshold: 0.5,
        RerankTopK:      10,
        
        // Multi-turn
        MultiTurnEnabled: true,
        HistoryTurns:     5,
        
        // Web Search
        WebSearchEnabled:    true,
        WebSearchMaxResults: 8,
    },
}

// Save to database
err := s.customAgentRepo.Create(ctx, customAgent)
```

### Agent Defaults

The system automatically sets defaults for omitted fields:

```go
// From types.CustomAgent.EnsureDefaults()
func (a *CustomAgent) EnsureDefaults() {
    if a.Config.Temperature < 0 {
        a.Config.Temperature = 0.7
    }
    if a.Config.MaxIterations == 0 {
        a.Config.MaxIterations = 10
    }
    if a.Config.WebSearchMaxResults == 0 {
        a.Config.WebSearchMaxResults = 5
    }
    if a.Config.HistoryTurns == 0 {
        a.Config.HistoryTurns = 5
    }
    if a.Config.EmbeddingTopK == 0 {
        a.Config.EmbeddingTopK = 10
    }
    // ... more defaults
}
```

---

## Configuring Agent Knowledge Bases

### Pattern 1: "selected" Mode - Specific KBs

For agents with curated knowledge bases:

```go
customAgent.Config.KBSelectionMode = "selected"
customAgent.Config.KnowledgeBases = []string{
    "sales-kb",
    "product-docs-kb",
    "faq-kb",
}
```

When a request is made:
1. `resolveKnowledgeBases()` detects mode="selected"
2. Returns `["sales-kb", "product-docs-kb", "faq-kb"]`
3. `buildSearchTargets()` creates 3 SearchTargets (KNOWLEDGE_BASE type)
4. Tool receives SearchTargets restricted to these 3 KBs
5. Agent cannot access other KBs even if @mentioned

### Pattern 2: "all" Mode - Dynamic KB Access

For research agents with broad KB access:

```go
customAgent.Config.KBSelectionMode = "all"
customAgent.Config.KnowledgeBases = []string{}  // Ignored when mode="all"
```

When a request is made:
1. `resolveKnowledgeBases()` calls `ListKnowledgeBases()`
2. For non-shared agents: also includes user's shared KBs
3. Returns ALL accessible KB IDs at request time
4. `buildSearchTargets()` creates SearchTarget for each KB
5. Agent can search any accessible KB without reconfiguration

### Pattern 3: "none" Mode - No KB Access

For agents that don't use knowledge bases:

```go
customAgent.Config.KBSelectionMode = "none"
customAgent.Config.KnowledgeBases = []string{}

// Knowledge base tools are automatically filtered out
// Only web search and other tools available
```

### Pattern 4: RetrieveKBOnlyWhenMentioned - Explicit Selection

For agents where KB usage is optional:

```go
customAgent.Config.KBSelectionMode = "selected"
customAgent.Config.KnowledgeBases = []string{"kb-001", "kb-002"}
customAgent.Config.RetrieveKBOnlyWhenMentioned = true
```

Behavior:
- Agent has access to kb-001 and kb-002
- KB search only occurs if user explicitly @mentions a KB
- Without @mention: KB tools won't be called by LLM

Example query behaviors:
```
Query: "What is this?"
  → No KB search (no @mention)
  → May use web search if enabled

Query: "What is this? @kb-001"
  → KB search in kb-001 only
  → Respects RetrieveKBOnlyWhenMentioned setting
```

---

## Common Configuration Scenarios

### Scenario 1: Specialized Domain Expert

Use case: Agent focused on specific domain (e.g., finance)

```go
customAgent.Config = types.CustomAgentConfig{
    AgentMode:      "smart-reasoning",
    KBSelectionMode: "selected",
    KnowledgeBases:  []string{"finance-kb", "compliance-kb"},
    
    AllowedTools: []string{
        tools.ToolKnowledgeSearch,
        tools.ToolDatabaseQuery,
        tools.ToolFinalAnswer,
    },
    
    MaxIterations:      20,
    Temperature:        0.5,  // Conservative
    MultiTurnEnabled:   true,
    HistoryTurns:       10,
    
    EmbeddingTopK:      25,
    VectorThreshold:    0.6,  // High threshold = precise matches
    
    WebSearchEnabled: false,  // Rely on internal KB only
}
```

### Scenario 2: General-Purpose Research Assistant

Use case: Broad access, flexible retrieval

```go
customAgent.Config = types.CustomAgentConfig{
    AgentMode:       "smart-reasoning",
    KBSelectionMode: "all",  // All accessible KBs
    
    AllowedTools: []string{
        tools.ToolKnowledgeSearch,
        tools.ToolGrepChunks,
        tools.ToolWebSearch,
        tools.ToolTodoWrite,
        tools.ToolFinalAnswer,
    },
    
    MaxIterations:    15,
    Temperature:      0.7,   // Balanced
    MultiTurnEnabled: true,
    HistoryTurns:     5,
    
    EmbeddingTopK:     10,
    VectorThreshold:   0.3,  // Lower = broader matches
    
    WebSearchEnabled:    true,
    WebSearchMaxResults: 10,
}
```

### Scenario 3: FAQ-Focused Quick Responder

Use case: Fast answers from FAQ KB only

```go
customAgent.Config = types.CustomAgentConfig{
    AgentMode:       "quick-answer",  // RAG mode, not agent mode
    KBSelectionMode: "selected",
    KnowledgeBases:  []string{"faq-kb"},
    
    AllowedTools: []string{
        tools.ToolKnowledgeSearch,
        tools.ToolFinalAnswer,
    },
    
    MaxIterations:    1,     // Quick, single shot
    Temperature:      0.3,   // Deterministic
    MultiTurnEnabled: false,
    
    EmbeddingTopK:     5,
    VectorThreshold:   0.7,  // Very high = exact matches
    
    FAQPriorityEnabled:        true,
    FAQDirectAnswerThreshold:  0.8,
    FAQScoreBoost:            2.0,
}
```

### Scenario 4: Cross-Tenant Shared Agent

Use case: Agent shared across organizations

```go
// Organization A's agent (owner)
sharedAgent := &types.CustomAgent{
    ID:       "shared-research-agent",
    TenantID: orgAID,  // ← Owner is Org A
    Config: types.CustomAgentConfig{
        KBSelectionMode: "selected",
        KnowledgeBases:  []string{"orgA-kb-1", "orgA-kb-2"},
        // ... other config
    },
}

// When Organization B uses this agent:
req := &types.QARequest{
    CustomAgent: sharedAgent,
    Session: &types.Session{
        TenantID: orgBID,  // ← User is from Org B
    },
    Query: "What is...",
}

// During buildAgentConfig():
agentTenantID := sharedAgent.TenantID  // orgAID
sessionTenantID := req.Session.TenantID // orgBID

isSharedAgent := sessionTenantID != agentTenantID  // TRUE

// Key behavior:
// - Only Org A's KBs (orgA-kb-1, orgA-kb-2) are searched
// - Org B user CANNOT add their own KBs via @mention
// - This prevents data leak across organizations
```

---

## Retrieving Search Results

### Pattern 1: Direct Tool Usage in Agent

When agent calls knowledge_search tool:

```go
// In LLM response, agent generates:
{
  "tool_name": "knowledge_search",
  "arguments": {
    "queries": [
      "What are the key features of our product?",
      "How does the pricing model work?"
    ],
    "knowledge_base_ids": []  // Optional: empty = search all allowed KBs
  }
}

// Tool execution (knowledge_search.go:155)
func (t *KnowledgeSearchTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
    var input KnowledgeSearchInput
    json.Unmarshal(args, &input)
    
    // t.searchTargets = pre-computed targets from config
    searchTargets := t.searchTargets
    
    // If user specifies KBs, filter further
    if len(input.KnowledgeBaseIDs) > 0 {
        filtered := filterTargets(searchTargets, input.KnowledgeBaseIDs)
        searchTargets = filtered
    }
    
    // Execute search using all targets
    results := t.concurrentSearchByTargets(
        ctx, input.Queries, searchTargets, ...)
    
    return &types.ToolResult{
        Success: true,
        Output:  formatResults(results),
        Data:    resultsAsJSON(results),
    }, nil
}
```

### Pattern 2: Non-Agent KB Search (Chat Pipeline)

```go
// Via chat pipeline for non-agent mode
chatManage := &types.ChatManage{
    Query:           "Summarize Q3 results",
    SearchTargets:   /* pre-computed */,
    VectorThreshold: 0.3,
    KeywordThreshold: 0.5,
    EmbeddingTopK:   10,
}

// Pipeline plugin executes
plugin.OnEvent(ctx, types.CHUNK_SEARCH, chatManage, next)
    ↓
plugin.searchByTargets(ctx, chatManage)
    ├─ Groups targets by embedding model
    ├─ For full KBs: HybridSearch(all full KB IDs)
    ├─ For specific files: Per-target search
    └─ Returns merged SearchResults
```

### Pattern 3: Accessing Single SearchTarget

```go
// When processing specific knowledge files
target := &types.SearchTarget{
    Type:            types.SearchTargetTypeKnowledge,
    KnowledgeBaseID: "kb-001",
    TenantID:        tenantID,
    KnowledgeIDs:    []string{"file-1", "file-2"},
}

// In searchSingleTarget (search.go:460)
params := types.SearchParams{
    QueryText:        query,
    QueryEmbedding:   embedding,
    KnowledgeBaseIDs: []string{target.KnowledgeBaseID},
    KnowledgeIDs:     target.KnowledgeIDs,  // ← Restricts to these files
    VectorThreshold:  0.3,
    KeywordThreshold: 0.5,
    MatchCount:       10,
}

results, _ := knowledgeBaseService.HybridSearch(ctx, target.KnowledgeBaseID, params)
// Returns chunks from kb-001 that are in file-1 or file-2
```

---

## Debugging Configuration Issues

### Issue 1: "No Search Targets Available"

**Error**: Tool returns error "no knowledge bases specified and no search targets configured"

**Diagnosis**:
```go
// In knowledge_search.go:196
if len(searchTargets) == 0 {
    logger.Errorf(ctx, "[Tool][KnowledgeSearch] No search targets available")
    return &types.ToolResult{
        Success: false,
        Error:   "no knowledge bases specified and no search targets configured",
    }, err
}
```

**Causes**:
1. Agent configured with KBSelectionMode="none"
2. Request has no @mentions and RetrieveKBOnlyWhenMentioned=true
3. Agent has no allowed KBs for shared agent scenario

**Fix**:
```go
// Check agent config
if customAgent.Config.KBSelectionMode == "none" {
    // Need to enable KB mode
    customAgent.Config.KBSelectionMode = "selected"
    customAgent.Config.KnowledgeBases = []string{"kb-id"}
}

// Or check @mention requirement
if customAgent.Config.RetrieveKBOnlyWhenMentioned && len(req.KnowledgeBaseIDs) == 0 {
    // Requires @mention
    req.KnowledgeBaseIDs = []string{"kb-id"}  // User needs to mention
}
```

### Issue 2: "Shared Agent Blocked All @Mentions"

**Error**: @mention filtered out with warning "Blocking @mentioned KB: not in shared agent's allowed scope"

**Diagnosis**:
```go
// In session_qa_helpers.go:231
if !allowedSet[id] {
    logger.Warnf(ctx, "Blocking @mentioned KB %s: not in shared agent's allowed scope", id)
}
```

**Cause**: Using shared agent from different organization, @mentioning KB outside agent's scope

**Fix**:
```go
// Check agent's allowed KBs
allowedKBs := customAgent.Config.KnowledgeBases
userMentionedKBs := req.KnowledgeBaseIDs

// Only use KBs that are in both lists
validKBs := make([]string, 0)
for _, kb := range userMentionedKBs {
    for _, allowed := range allowedKBs {
        if kb == allowed {
            validKBs = append(validKBs, kb)
        }
    }
}

// Or: configure agent with mode="all" to allow any KB
customAgent.Config.KBSelectionMode = "all"
```

### Issue 3: "Failed to Get Chat Model"

**Error**: "Failed to get chat model: model not found"

**Diagnosis**:
```go
// In session_agent_qa.go:88
summaryModel, err := s.modelService.GetChatModel(ctx, effectiveModelID)
if err != nil {
    logger.Warnf(ctx, "Failed to get chat model: %v", err)
    return fmt.Errorf("failed to get chat model: %w", err)
}
```

**Causes**:
1. CustomAgent.Config.ModelID doesn't exist
2. No model configured at any level
3. Model is disabled/deleted

**Debug**:
```go
// Check model resolution priority
effectiveModelID := ""

// Priority 1: Request override
if req.SummaryModelID != "" {
    effectiveModelID = req.SummaryModelID
}

// Priority 2: Agent config
if effectiveModelID == "" && customAgent.Config.ModelID != "" {
    effectiveModelID = customAgent.Config.ModelID
}

// Priority 3: KB default
if effectiveModelID == "" {
    effectiveModelID = s.selectChatModelID(ctx, req.Session, kbs, knowledgeIDs)
}

// Verify model exists
model, err := s.modelService.GetChatModel(ctx, effectiveModelID)
if err != nil {
    logger.Infof(ctx, "Model %s not found: %v", effectiveModelID, err)
}
```

### Issue 4: Knowledge Base Tools Filtered Out

**Error**: Agent calls knowledge_search but tool not available

**Diagnosis**:
```go
// In agent_service.go:338
hasKnowledge := len(config.KnowledgeBases) > 0 || len(config.KnowledgeIDs) > 0
if !hasKnowledge {
    // Remove all KB-related tools
    filteredTools = filterOut(allowedTools, [
        ToolKnowledgeSearch,
        ToolGrepChunks,
        ToolListKnowledgeChunks,
        // etc
    ])
}
```

**Cause**: Agent has no KBs configured

**Fix**:
```go
// Ensure at least one KB is configured
customAgent.Config.KBSelectionMode = "selected"
customAgent.Config.KnowledgeBases = []string{"kb-id"}

// Or use "all" mode
customAgent.Config.KBSelectionMode = "all"
```

### Issue 5: Low Recall (Few Results)

**Diagnosis**:
```
Query results: 2 chunks
Expected: 10+ chunks
```

**Causes**:
1. VectorThreshold too high
2. EmbeddingTopK too low
3. KB doesn't contain relevant content

**Tune Parameters**:
```go
// Lower VectorThreshold for broader matches
customAgent.Config.VectorThreshold = 0.3  // Was 0.7

// Increase TopK to retrieve more candidates
customAgent.Config.EmbeddingTopK = 50  // Was 10

// Or relax keyword matching
customAgent.Config.KeywordThreshold = 0.2  // Was 0.5
```

### Issue 6: Rerank Model Missing

**Error**: "Rerank model not configured for custom agent, but knowledge_search tool is enabled"

**Diagnosis**:
```go
// In session_agent_qa.go:104
hasKnowledgeSearchTool := false
for _, tool := range agentConfig.AllowedTools {
    if tool == tools.ToolKnowledgeSearch {
        hasKnowledgeSearchTool = true
        break
    }
}

if hasKnowledgeSearchTool {
    rerankModelID := req.CustomAgent.Config.RerankModelID
    if rerankModelID == "" {
        return errors.New("rerank model is not configured")
    }
}
```

**Cause**: Using knowledge_search but no rerank model specified

**Fix**:
```go
customAgent.Config.RerankModelID = "model-rerank-id"

// Or disable knowledge_search if rerank unavailable
customAgent.Config.AllowedTools = []string{
    tools.ToolGrepChunks,  // Keyword search doesn't need rerank
    tools.ToolFinalAnswer,
}
```

---

## Summary

Key patterns to remember:

1. **Always set KBSelectionMode** - Defaults to "selected" which requires explicit KnowledgeBases
2. **SearchTargets computed once** - Set before creating agent engine
3. **Tools constrained by SearchTargets** - Cannot search outside pre-computed scope
4. **Shared agents cannot leak KBs** - isSharedAgent detection prevents cross-org data access
5. **Configuration precedence matters** - Request > Agent > Tenant > System defaults
6. **Rerank model required for knowledge_search** - Or use grep_chunks instead
7. **Use mode="all" for flexible KB access** - Resolves at request time
8. **Use RetrieveKBOnlyWhenMentioned for optional KB** - KB search only on @mention
