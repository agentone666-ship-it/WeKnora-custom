# WeKnora Agent & Language Infrastructure - Complete Analysis Summary

**Generated:** April 7, 2026
**Status:** Complete ✅

---

## Executive Summary

This analysis addresses four key areas of the WeKnora codebase:

1. **Agent ReAct Engine Architecture** - Understanding how tools are dispatched
2. **System Prompts** - How the agent learns about available tools
3. **Wiki Tool Registration** - Dynamic tool availability based on KB type
4. **Language Infrastructure Refactoring** - Eliminating duplicate language logic

### Key Findings

✅ **Agent uses automatic tool dispatch** - No user intervention needed, LLM decides tool usage
✅ **Wiki tools conditionally registered** - Only visible when wiki KBs are detected
✅ **System prompts are generic** - No dedicated wiki-specific prompt for the Agent
✅ **Language infrastructure exists** - Middleware contains reusable language mapping
❌ **wiki_write_page not guaranteed** - LLM must independently decide to use it
❌ **Synthesis detection is heuristic** - In ingest pipeline, not Agent loop

---

## Document Overview

### 1. **AGENT_WIKI_ANALYSIS.md** (16 KB)
**Purpose:** Complete technical analysis of the Agent ReAct engine

**Sections:**
- Agent ReAct Engine - Main Loop Architecture
- Tool Dispatch Mechanism - Automatic, Not User-Triggered
- System Prompts - Where Tools Are Described
- Wiki Tool Registration - Conditional Based on KB Type
- Wiki Tools Implementation - 5 Tools Overview
- How the Agent Knows About Wiki Tools
- Is There a Dedicated Wiki-Specific System Prompt? (Answer: NO)
- Synthesis Detection - Where It Actually Happens
- Complete Tool List and Filter Logic
- Event-Driven Architecture
- Summary Table

**Key Code Files Referenced:**
- `/internal/agent/engine.go` - Core ReAct engine
- `/internal/agent/act.go` - Tool dispatch
- `/internal/agent/prompts.go` - System prompt building
- `/internal/agent/tools/wiki_tools.go` - Wiki tool implementations
- `/internal/application/service/agent_service.go` - Tool registration
- `/internal/application/service/wiki_ingest.go` - Wiki ingest pipeline

---

### 2. **LANGUAGE_MIDDLEWARE_ANALYSIS.md** (11 KB)
**Purpose:** Identify and document existing language infrastructure

**Sections:**
- Existing Language Middleware Infrastructure
- Current Wiki Ingest Language Logic (NEEDS REFACTORING)
- Refactoring Recommendations
- Integration Points
- Implementation Checklist
- Additional Observations
- Code Migration Path
- Files Modified Summary

**Key Functions Identified:**
- `types.LanguageLocaleName()` - 9+ language mapping (the solution!)
- `types.LanguageFromContext()` - Extract language from context
- `types.DefaultLanguage()` - Get configured default language
- `types.EnvLanguage()` - Read WEKNORA_LANGUAGE env var
- `middleware.Language()` - HTTP middleware for language extraction
- `middleware.parseFirstLanguageTag()` - Parse Accept-Language header

---

### 3. **REFACTORING_PLAN.md** (6.3 KB)
**Purpose:** Detailed implementation plan for language refactoring

**Sections:**
- Problem Statement
- Solution Overview
- Implementation Details (with code diffs)
- Impact Analysis
- Supported Languages (9+)
- Backward Compatibility Analysis
- Testing Recommendations
- Future Enhancement Options
- Implementation Steps (5 steps)
- Rollback Plan

**The One-Line Fix:**
```go
// Replace lines 135-141 in wiki_ingest.go with:
lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

**Benefits:**
- Reduces code from 6 lines to 1 line
- Supports 9+ languages instead of 2
- Eliminates code duplication
- Improves maintainability
- Fully backward compatible

---

### 4. **LANGUAGE_REFACTORING_QUICK_REFERENCE.md** (3.0 KB)
**Purpose:** Quick copy-paste guide for implementation

**Sections:**
- TL;DR Summary
- What to Change (exact location + code)
- Key Benefits Table
- Function Reference
- Testing Instructions
- Implementation Checklist

**Perfect for:** Developers implementing the refactoring quickly

---

## Key Questions Answered

### Q1: How does the Agent's ReAct engine dispatch tools?

**Answer:** Automatic and LLM-driven.

**Evidence:**
- File: `/internal/agent/engine.go` - `executeLoop()` method (lines 260-394)
- File: `/internal/agent/act.go` - `executeToolCalls()` function (lines 68-89)
- Tools are called automatically after LLM response without user intervention
- Max 5 iterations per query, each iteration: Think → Analyze → Act → Observe

**Key Detail:** Tool execution is triggered by LLM decision, not user action.

### Q2: What system prompts does the Agent use?

**Answer:** Generic RAG or Pure Agent templates, no dedicated wiki prompt.

**Evidence:**
- File: `/internal/agent/prompts.go` - `BuildSystemPromptWithOptions()` (lines 315-360)
- Two templates:
  - `GetPureAgentSystemPrompt()` - For no knowledge bases
  - `GetProgressiveRAGSystemPrompt()` - For knowledge bases
- Placeholders: `{{knowledge_bases}}`, `{{web_search_status}}`, `{{current_time}}`, `{{language}}`
- Wiki-specific prompts are in `prompts_wiki.go` but used only by ingest pipeline, not Agent

### Q3: How does the Agent know about wiki tools?

**Answer:** Via conditional registration based on KB type detection.

**Evidence:**
- File: `/internal/application/service/agent_service.go` - `registerTools()` (lines 316-477)
- Lines 370-389: Iterate search targets, detect wiki KBs by type
- If wiki KBs found → automatically add 5 wiki tools to allowed tools list
- Tools then passed to LLM via `buildToolsForLLM()` in `/internal/agent/observe.go`

**Key Detail:** Wiki tools only visible if wiki KBs are in search targets.

### Q4: Is there a dedicated wiki-related system prompt for the Agent?

**Answer:** NO. There is no such prompt.

**Evidence:**
- Searched `prompts.go` - no wiki-specific system prompt
- Wiki prompts in `prompts_wiki.go` are for ingest pipeline only, not Agent loop
- Tool description is minimal (lines 91-92 in `wiki_tools.go`)
- Agent decides to use `wiki_write_page` based on general reasoning, not explicit instruction

**Implication:** `wiki_write_page` is NOT guaranteed to be called - LLM must independently decide it's appropriate.

### Q5: Can the wiki language logic be refactored?

**Answer:** YES, completely. Reuse `types.LanguageLocaleName()`.

**Current Issue:**
- `wiki_ingest.go` lines 135-141 contain hardcoded language mapping (2 languages only)
- `types.LanguageLocaleName()` (lines 85-112 in `context_helpers.go`) already does this (9+ languages)
- Duplicated logic, inconsistent naming, hard to extend

**Solution:**
```go
lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

**Benefits:**
- 6 lines → 1 line (83% reduction)
- 2 languages → 9+ languages
- Consistent with middleware naming
- Centralized maintenance
- Fully backward compatible

---

## Recommended Actions

### Immediate (Next Sprint)
1. ✅ Review `AGENT_WIKI_ANALYSIS.md` for understanding Agent architecture
2. ✅ Review `LANGUAGE_MIDDLEWARE_ANALYSIS.md` for language infrastructure
3. 🔄 **Plan refactoring sprint** using `REFACTORING_PLAN.md`

### Short-term (1-2 weeks)
1. 🔄 **Implement language refactoring** using `LANGUAGE_REFACTORING_QUICK_REFERENCE.md`
   - Edit 1 file, 6 lines → 1 line
   - Run existing tests
   - Commit and merge
   - Time estimate: 30 minutes

### Medium-term (Optional Enhancements)
1. Add support for context-aware language fallback (use context language if KB language empty)
2. Normalize `WikiLanguage` storage format from short codes to full locales
3. Audit codebase for other hardcoded language mappings

### Long-term (Architectural)
1. Document language handling best practices
2. Consider similar refactorings for other duplicated logic
3. Expand language support based on user demand

---

## File Locations & Quick Links

### Analysis Documents
| Document | Size | Purpose |
|----------|------|---------|
| `AGENT_WIKI_ANALYSIS.md` | 16 KB | Complete Agent architecture analysis |
| `LANGUAGE_MIDDLEWARE_ANALYSIS.md` | 11 KB | Language infrastructure deep dive |
| `REFACTORING_PLAN.md` | 6.3 KB | Detailed implementation plan |
| `LANGUAGE_REFACTORING_QUICK_REFERENCE.md` | 3 KB | Quick copy-paste guide |
| `ANALYSIS_SUMMARY.md` | This file | Executive overview |

### Code Files Referenced

**Agent Engine:**
- `/internal/agent/engine.go` - ReAct loop implementation
- `/internal/agent/act.go` - Tool execution
- `/internal/agent/observe.go` - Tool visibility
- `/internal/agent/think.go` - LLM invocation
- `/internal/agent/prompts.go` - System prompt building

**Tools:**
- `/internal/agent/tools/wiki_tools.go` - 5 wiki tool implementations
- `/internal/agent/tools/definitions.go` - Tool metadata

**Services:**
- `/internal/application/service/agent_service.go` - Tool registration
- `/internal/application/service/wiki_ingest.go` - Wiki ingest pipeline (to be refactored)

**Infrastructure:**
- `/internal/middleware/language.go` - Language extraction middleware
- `/internal/types/context_helpers.go` - Language helpers (LanguageLocaleName function)
- `/internal/types/const.go` - Context key definitions

---

## Testing Recommendations

### Unit Tests
```bash
# Test language mapping
go test -v ./internal/types -run TestLanguageLocaleName

# Test agent engine
go test -v ./internal/agent -run TestEngine

# Test wiki ingest (after refactoring)
go test -v ./internal/application/service -run TestWikiIngest
```

### Integration Tests
```bash
# Full test suite
go test -v ./internal/...
```

### Manual Testing (After Refactoring)
1. Create wiki KB with WikiLanguage: "zh" → Verify Chinese output
2. Create wiki KB with WikiLanguage: "en" → Verify English output
3. Create wiki KB with WikiLanguage: "ko" → Verify Korean output (new!)
4. Create wiki KB with WikiLanguage: "unknown" → Verify graceful fallback

---

## Glossary

**ReAct:** Reasoning + Acting - LLM loop that reasons about problems and invokes tools
**Progressive RAG:** Retrieval-Augmented Generation mode with knowledge bases
**Pure Agent:** Mode with no knowledge bases
**Tool Registry:** Central registry holding all available tools
**Automatic Tool Dispatch:** Tools are executed by engine when LLM decides, without user confirmation
**Synthesis Detection:** Heuristic that counts page types and suggests synthesis opportunities
**EventBus:** Event-driven system for UI feedback

---

## Contact & Follow-up

**Questions about analysis?** Review the specific document sections above.

**Ready to implement?** Start with `LANGUAGE_REFACTORING_QUICK_REFERENCE.md`.

**Need more details?** Each major document section has code snippets and file references.

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-04-07 | Initial analysis and documentation |

---

**End of Summary**

All documents are located in `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/`

