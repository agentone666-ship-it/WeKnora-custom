# WeKnora Agent ReAct Engine & Wiki Tool Analysis

## Executive Summary

The WeKnora Agent uses a **ReAct (Reasoning + Acting)** loop architecture where:
1. The **Agent Engine** calls the LLM with available tools
2. The LLM decides which tools to call (or provide a final answer)
3. Tools are **automatically dispatched** by the engine - **NOT** requiring user explicit request
4. Wiki tools are **conditionally registered** when wiki knowledge bases are detected
5. **`wiki_write_page` is NOT automatically triggered** - it requires the Agent to explicitly decide to use it based on the system prompt

---

## 1. Agent ReAct Engine - Main Loop Architecture

### File: `/internal/agent/engine.go`

**Core Components:**
- **`AgentEngine` struct** (line 24-41): Main ReAct engine orchestrating the loop
- **`Execute()` method** (line 155-256): Entry point for agent execution
- **`executeLoop()` method** (line 260-394): Main ReAct loop implementation

**ReAct Loop Flow:**

```
executeLoop() iteration:
  1. Think Phase
     - Call LLM with messages + available tools (line 315)
     - LLM returns response with optional tool calls
  
  2. Analyze Phase  
     - Check for stop conditions: finish_reason=="stop" OR final_answer tool (line 338)
     - If LLM signals done: extract final answer and break loop
  
  3. Act Phase
     - Execute ALL tool calls in the response (line 369)
     - Support parallel execution if enabled (line 81 in act.go)
  
  4. Observe Phase
     - Collect tool results
     - Append results to message history (line 373)
     - Write to context manager (line 236 in observe.go)
  
  5. Continue to next round (line 382)
```

**Key Characteristics:**
- Max iterations configured in `config.MaxIterations` (default 5)
- Loop exits when:
  - LLM calls `final_answer` tool (explicit stop)
  - LLM stops naturally with `finish_reason=="stop"` (implicit stop)
  - Max iterations reached (graceful synthesis from existing results)
  - Context cancelled/timeout

---

## 2. Tool Dispatch Mechanism - Automatic, Not User-Triggered

### File: `/internal/agent/act.go`

**Tool Dispatch Logic:**

```go
// executeToolCalls (line 68-89)
// Called AUTOMATICALLY after LLM response, regardless of user action
func (e *AgentEngine) executeToolCalls(
    ctx context.Context, response *types.ChatResponse,
    step *types.AgentStep, iteration int, sessionID string,
)
```

**Process:**
1. **Line 72**: Check if response contains tool calls (`len(response.ToolCalls) == 0`)
2. **Lines 81-88**: Route to parallel or sequential execution
3. **For each tool call:**
   - Parse arguments (line 216-236)
   - Call `e.toolRegistry.ExecuteTool()` (line 266-269)
   - Emit events for UI feedback (line 244-255)
   - Collect results (line 279)

**Tool Execution Flow:**
```
LLM Response with tool calls
    ↓
parseToolCall() - parse JSON args
    ↓
runToolCall() - execute with timeout (line 207-330)
    ↓
Emit toolCallStart event (line 244-255)
    ↓
ExecuteTool() - actual tool execution
    ↓
Emit toolResult event + collect result (line 127-141, 173-187)
    ↓
Append to step.ToolCalls
    ↓
Continue loop or synthesize answer
```

**CRITICAL: Tool calls are triggered BY THE LLM, NOT the user**
- User input → LLM decision → Tool dispatch (automatic)
- The Agent never queries the user "should I call tool X?"
- Tools are included in the system prompt as options the LLM can choose from

---

## 3. System Prompts - Where Tools Are Described

### File: `/internal/agent/prompts.go`

**System Prompt Building:**
```go
BuildSystemPromptWithOptions() (line 315-360)
```

This function constructs the system prompt that:
1. Loads a template from config or uses default
2. Renders placeholders (knowledge bases, web search status, language)
3. Appends skill metadata if enabled
4. Appends selected documents info

**Template Sources:**
- **Pure Agent Mode** (no KB): `GetPureAgentSystemPrompt()` (line 365-372)
- **Progressive RAG Mode** (with KB): `GetProgressiveRAGSystemPrompt()` (line 377-384)
- **Custom Templates**: From config YAML files

**Default Templates loaded from:**
- Config: `config.PromptTemplates.AgentSystemPrompt`
- Mode-based: "pure" or "rag" template variant
- Fallback: Hardcoded defaults (if config not provided)

**The system prompt DOES NOT mention wiki tools explicitly** - it's handled at tool registration time.

### File: `/internal/agent/prompts_wiki.go`

**Wiki-Specific Prompts** (for document ingestion, NOT Agent decision):
- `WikiSummaryPrompt` (line 8): Generate summary page from document
- `WikiKnowledgeExtractPrompt` (line 32): Extract entities and concepts (single LLM call)
- `WikiPageUpdatePrompt` (line 87): Incrementally update existing page
- `WikiIndexRebuildPrompt` (line 107): Generate index from page listing
- `WikiLogEntryTemplate` (line 122): Simple template for log entries

**These prompts are used by the wiki ingest pipeline, NOT the Agent ReAct loop.**

---

## 4. Wiki Tool Registration - Conditional Based on KB Type

### File: `/internal/application/service/agent_service.go`

**Tool Registration Flow:**

```go
registerTools() (line 316-477)
```

**Step 1: Determine which tools to register**
```go
// Line 336-362: Filter tools based on KB availability
hasKnowledge := len(config.KnowledgeBases) > 0 || len(config.KnowledgeIDs) > 0

if !hasKnowledge {
    // Remove KB-related tools for Pure Agent Mode
    // tools like: knowledge_search, grep_chunks, list_knowledge_chunks, etc.
}
```

**Step 2: Detect Wiki KBs and add wiki tools automatically**
```go
// Line 370-389: AUTO-DETECT WIKI KBs
var wikiKBIDs []string
for _, target := range config.SearchTargets {
    kb, err := s.knowledgeBaseService.GetKnowledgeBaseByIDOnly(ctx, target.KnowledgeBaseID)
    if err == nil && kb.Type == types.KnowledgeBaseTypeWiki {
        wikiKBIDs = append(wikiKBIDs, kb.ID)
        wikiTenantID = kb.TenantID
    }
}

// If wiki KBs found → automatically add wiki tools
if len(wikiKBIDs) > 0 {
    allowedTools = append(allowedTools,
        tools.ToolWikiReadPage,
        tools.ToolWikiWritePage,
        tools.ToolWikiSearch,
        tools.ToolWikiReadIndex,
        tools.ToolWikiLint,
    )
}
```

**Step 3: Register each tool**
```go
// Line 394-473: Register tool implementations
for _, toolName := range allowedTools {
    switch toolName {
    case tools.ToolWikiReadPage:
        toolToRegister = tools.NewWikiReadPageTool(s.wikiPageService, wikiKBIDs)
    case tools.ToolWikiWritePage:
        toolToRegister = tools.NewWikiWritePageTool(s.wikiPageService, wikiKBIDs, wikiTenantID)
    case tools.ToolWikiSearch:
        toolToRegister = tools.NewWikiSearchTool(s.wikiPageService, wikiKBIDs)
    case tools.ToolWikiReadIndex:
        toolToRegister = tools.NewWikiReadIndexTool(s.wikiPageService, wikiKBIDs)
    case tools.ToolWikiLint:
        toolToRegister = tools.NewWikiLintTool(s.wikiPageService, wikiKBIDs)
    }
    registry.RegisterTool(toolToRegister)
}
```

**Result:** Wiki tools are included in the tools passed to LLM if wiki KBs are detected.

---

## 5. Wiki Tools Implementation

### File: `/internal/agent/tools/wiki_tools.go`

**5 Wiki Tools Available:**

#### 1. **wiki_read_page** (line 14-76)
```go
type wikiReadPageTool struct {
    wikiService interfaces.WikiPageService
    kbIDs       []string  // Search across these KBs
}

// Parameters: slug, knowledge_base_id (optional)
// Output: Full markdown content + metadata + links
```

#### 2. **wiki_write_page** (line 78-185)
```go
type wikiWritePageTool struct {
    wikiService interfaces.WikiPageService
    kbIDs       []string
    tenantID    uint64
}

// Parameters:
// - slug: e.g. "synthesis/quarterly-review" or "comparison/tool-a-vs-tool-b"
// - title: Human-readable title
// - content: Full Markdown
// - page_type: enum ["summary", "entity", "concept", "synthesis", "comparison"]
// - knowledge_base_id: Target KB (optional, uses first if multiple)

// Behavior:
// 1. Check if page exists (line 148)
//    - If exists: UPDATE and increment version (line 155)
//    - If new: CREATE with default status (line 165-175)
// 2. Return success + page info
```

#### 3. **wiki_search** (line 187-255)
```go
// Parameters: query (keyword search), limit (default 10)
// Output: List of matching pages with titles, slugs, summaries
```

#### 4. **wiki_read_index** (line 257-314)
```go
// Parameters: knowledge_base_id (optional)
// Output: Index page content (TOC of all pages)
```

#### 5. **wiki_lint** (line 316-427)
```go
// Parameters: knowledge_base_id (optional)
// Output: Wiki health report with:
// - Statistics (total pages, orphan pages, broken links)
// - Health score (0-100)
// - Suggestions for improvement
```

**Key: `wiki_write_page` is a TOOL like any other - the LLM decides if/when to call it based on the system prompt and context.**

---

## 6. How the Agent Knows About Wiki Tools

### Tool Visibility in LLM Call

**File: `/internal/agent/observe.go` (line 183-198)**

```go
func (e *AgentEngine) buildToolsForLLM() []chat.Tool {
    functionDefs := e.toolRegistry.GetFunctionDefinitions()
    tools := make([]chat.Tool, 0, len(functionDefs))
    
    for _, def := range functionDefs {
        tools = append(tools, chat.Tool{
            Type: "function",
            Function: chat.FunctionDef{
                Name:        def.Name,
                Description: def.Description,
                Parameters:  def.Parameters,
            },
        })
    }
    return tools
}
```

**Description of wiki_write_page in tool definition:**

```go
// From wiki_tools.go line 91-92
`Create or update a wiki page. Use this to save valuable analysis, 
synthesis, or new knowledge into the wiki.
The page content should be in Markdown format. Use [[slug]] syntax 
to create links between pages.`
```

The description tells the LLM:
- **Purpose**: Save analysis, synthesis, new knowledge
- **Format**: Markdown
- **When**: "valuable", "synthesis", "analysis" contexts
- **How**: Call `wiki_write_page` with slug, title, content, page_type

---

## 7. Is There a Dedicated Wiki-Specific System Prompt for the Agent?

### **NO - There is no dedicated system prompt that tells the Agent to use wiki_write_page**

**Evidence:**
1. **No wiki-specific prompt in `prompts.go`**: 
   - Only generic RAG/Pure Agent prompts
   - No mention of synthesis detection or wiki page creation instructions

2. **Wiki prompts in `prompts_wiki.go` are for ingest pipeline ONLY:**
   - `WikiSummaryPrompt` - used by wiki_ingest_service.ProcessWikiIngest()
   - `WikiKnowledgeExtractPrompt` - used by wiki_ingest_service.extractEntitiesAndConcepts()
   - `WikiPageUpdatePrompt` - used by wiki_ingest_service.upsertExtractedPages()
   - These are NOT part of the Agent's system prompt

3. **Tool description is minimal** (line 91-92 in wiki_tools.go):
   - Only says "save valuable analysis, synthesis..."
   - Doesn't explicitly instruct WHEN to write pages

**Result: The Agent will only call `wiki_write_page` if:**
- It's in the mood to write analysis (depends on its reasoning)
- The LLM interprets "valuable analysis" broadly
- **NOT guaranteed by any explicit instruction in the system prompt**

---

## 8. Synthesis Detection - Where It Actually Happens

### NOT in Agent ReAct Loop - Instead, in Wiki Ingest Pipeline

**File: `/internal/application/service/wiki_ingest.go`**

**Synthesis opportunities are detected AUTOMATICALLY during document ingest:**

```go
// ProcessWikiIngest() line 85-214
// Step 3: Detect synthesis opportunities (line 200-201)
synthesisSuggestions = s.detectSynthesisOpportunities(ctx, payload)
```

**Detection Logic: `detectSynthesisOpportunities()` (line 350-389)**

```go
// Pure heuristic - NO LLM CALL, just counts pages by type

typeCounts := make(map[string]int)

// Iterate all pages, count by type
for _, p := range resp.Pages {
    if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
        continue
    }
    typeCounts[p.PageType]++
}

// Generate suggestions if thresholds met:
if count := typeCounts[types.WikiPageTypeEntity]; count >= 3 {
    // "Synthesis opportunity: N entity pages could be synthesized..."
}
if count := typeCounts[types.WikiPageTypeConcept]; count >= 3 {
    // "Synthesis opportunity: N concept pages could be synthesized..."
}
```

**Rules:**
- Trigger if ≥3 entity pages exist
- Trigger if ≥3 concept pages exist
- Return human-readable suggestions
- Append to wiki log page (line 435-460)

**Result:** Suggestions are logged to the wiki, NOT automatically acted upon by the Agent.

---

## 9. Complete Tool List and Filter Logic

### File: `/internal/agent/tools/definitions.go`

**All Available Tools:**

```go
const (
    ToolThinking            = "thinking"
    ToolTodoWrite           = "todo_write"
    ToolGrepChunks          = "grep_chunks"
    ToolKnowledgeSearch     = "knowledge_search"
    ToolListKnowledgeChunks = "list_knowledge_chunks"
    ToolQueryKnowledgeGraph = "query_knowledge_graph"
    ToolGetDocumentInfo     = "get_document_info"
    ToolDatabaseQuery       = "database_query"
    ToolDataAnalysis        = "data_analysis"
    ToolDataSchema          = "data_schema"
    ToolWebSearch           = "web_search"
    ToolWebFetch            = "web_fetch"
    ToolFinalAnswer         = "final_answer"
    ToolExecuteSkillScript  = "execute_skill_script"
    ToolReadSkill           = "read_skill"
    ToolWikiReadPage        = "wiki_read_page"
    ToolWikiWritePage       = "wiki_write_page"
    ToolWikiSearch          = "wiki_search"
    ToolWikiReadIndex       = "wiki_read_index"
    ToolWikiLint            = "wiki_lint"
)
```

**Default Allowed Tools (`DefaultAllowedTools()`, line 66-80):**
```
thinking, todo_write, knowledge_search, grep_chunks, list_knowledge_chunks,
query_knowledge_graph, get_document_info, database_query, data_analysis,
data_schema, final_answer
```

**Dynamically Added Tools:**
- `web_search`, `web_fetch` - if `config.WebSearchEnabled`
- `read_skill`, `execute_skill_script` - if `config.SkillsEnabled`
- `wiki_*` (5 tools) - if wiki KBs detected in search targets

---

## 10. Event-Driven Architecture

**All agent actions emit events through EventBus for UI feedback:**

### File: `/internal/agent/act.go` (event emission points)

1. **Tool hint event** (line 244-255) - For UI progress bar
2. **Tool call pending event** (line 133-142 in think.go)
3. **Tool result event** (line 127-141) - Output + duration
4. **Tool execution event** (line 143-156) - For logging

### File: `/internal/agent/observe.go` (event emission for analysis)

1. **Final answer event** (line 85-103) - When agent stops
2. **Thinking events** (line 189-198 in think.go) - Thought streaming

**EventBus types:**
- `EventAgentToolCall` - Tool invocation
- `EventAgentTool` - Tool execution
- `EventAgentToolResult` - Tool result
- `EventAgentFinalAnswer` - Final answer
- `EventAgentThought` - Thinking content

---

## Summary Table

| Aspect | Details |
|--------|---------|
| **ReAct Loop File** | `/internal/agent/engine.go` (executeLoop) |
| **Tool Dispatch** | `/internal/agent/act.go` (executeToolCalls) - AUTOMATIC |
| **System Prompts** | `/internal/agent/prompts.go` (progressive RAG + pure agent) |
| **Wiki Tool Prompts** | `/internal/agent/prompts_wiki.go` (for ingest pipeline ONLY) |
| **Wiki Tool Registration** | `/internal/application/service/agent_service.go` (line 370-389) - AUTO-DETECT |
| **Wiki Tool Impl** | `/internal/agent/tools/wiki_tools.go` (5 tools) |
| **Synthesis Detection** | `/internal/application/service/wiki_ingest.go` (line 350-389) - HEURISTIC |
| **Tool Visibility** | `/internal/agent/observe.go` buildToolsForLLM() |
| **Is wiki_write_page auto-triggered?** | **NO** - LLM decides based on tool description + reasoning |
| **Dedicated wiki-specific Agent prompt?** | **NO** - Wiki prompts are for ingest, not Agent system prompt |

---

## Key Takeaways

1. ✅ **Agent uses ReAct loop** - Reason (LLM) → Act (tool dispatch) → Observe (results) → Repeat
2. ✅ **Tools are conditionally registered** - Wiki tools added if wiki KBs detected
3. ✅ **Tool dispatch is automatic** - No user prompt needed, LLM decides based on context
4. ❌ **No dedicated wiki system prompt** - General RAG prompt, wiki tools described minimally in tool definition
5. ❌ **`wiki_write_page` NOT guaranteed** - LLM must decide it's valuable, no explicit instruction
6. ✅ **Synthesis detection exists** - But in ingest pipeline, not Agent loop (heuristic, not LLM-driven)

