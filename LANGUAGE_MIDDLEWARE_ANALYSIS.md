# WeKnora Language Middleware Analysis & Refactoring Guide

## Overview

The user mentioned that middleware already contains language determination logic that could be reused in the wiki ingest pipeline. This document identifies the existing language handling infrastructure and provides refactoring recommendations.

---

## 1. Existing Language Middleware Infrastructure

### File: `/internal/middleware/language.go`

**Purpose:** HTTP middleware to extract and inject user language preference into request context.

**Middleware Function:**
```go
func Language() gin.HandlerFunc
```

**Language Detection Priority (highest to lowest):**
1. `WEKNORA_LANGUAGE` environment variable (deployment-level override)
2. `Accept-Language` HTTP header (first tag, e.g., "zh-CN,zh;q=0.9" → "zh-CN")
3. Hardcoded fallback: "zh-CN"

**Key Design Principle:**
- Separates UI locale (menu language) from document processing language
- A user may prefer English UI while processing Korean documents
- WEKNORA_LANGUAGE env var takes precedence because it controls document processing language globally

**Implementation Details:**

```go
// Language() middleware:
// 1. Check WEKNORA_LANGUAGE env var (read once at startup)
// 2. If set → inject into context and return
// 3. If not set → parse Accept-Language header
// 4. If header missing → use "zh-CN" fallback
// 5. Inject final result into both gin context and request context

func Language() gin.HandlerFunc {
    envLang := types.EnvLanguage()
    return func(c *gin.Context) {
        if envLang != "" {
            c.Set(types.LanguageContextKey.String(), envLang)
            ctx := context.WithValue(c.Request.Context(), types.LanguageContextKey, envLang)
            c.Request = c.Request.WithContext(ctx)
            c.Next()
            return
        }
        // ... parse header, fallback, inject
    }
}

// Helper: parseFirstLanguageTag(header string) string
// Extracts first language tag from Accept-Language header
// e.g., "zh-CN,zh;q=0.9,en;q=0.8" → "zh-CN"
```

### File: `/internal/types/context_helpers.go`

**Existing Helper Functions:**

#### 1. `EnvLanguage() string` (line 9-12)
```go
// Returns WEKNORA_LANGUAGE environment variable value, or empty string
func EnvLanguage() string {
    return strings.TrimSpace(os.Getenv("WEKNORA_LANGUAGE"))
}
```

#### 2. `DefaultLanguage() string` (line 14-21)
```go
// Returns configured default language locale
// Priority: WEKNORA_LANGUAGE env > "zh-CN"
func DefaultLanguage() string {
    if lang := EnvLanguage(); lang != "" {
        return lang
    }
    return "zh-CN"
}
```

#### 3. `LanguageFromContext(ctx context.Context) (string, bool)` (line 67-72)
```go
// Extracts language locale from context
// e.g., "zh-CN", "en-US"
// Returns ("zh-CN", false) when key is absent
func LanguageFromContext(ctx context.Context) (string, bool) {
    v, ok := ctx.Value(LanguageContextKey).(string)
    return v, ok && v != ""
}
```

#### 4. `LanguageNameFromContext(ctx context.Context) string` (line 74-83)
```go
// Returns HUMAN-READABLE language name for use in LLM prompts
// Falls back to DefaultLanguage()
// e.g., "zh-CN" -> "Chinese (Simplified)"
func LanguageNameFromContext(ctx context.Context) string {
    lang, ok := LanguageFromContext(ctx)
    if !ok {
        lang = DefaultLanguage()
    }
    return LanguageLocaleName(lang)
}
```

#### 5. **THE KEY FUNCTION:** `LanguageLocaleName(locale string) string` (line 85-112)
```go
// Maps locale code to human-readable language name
// Comprehensive mapping of 9 languages:
func LanguageLocaleName(locale string) string {
    switch locale {
    case "zh-CN", "zh", "zh-Hans":
        return "Chinese (Simplified)"
    case "zh-TW", "zh-HK", "zh-Hant":
        return "Chinese (Traditional)"
    case "en-US", "en", "en-GB":
        return "English"
    case "ko-KR", "ko":
        return "Korean"
    case "ja-JP", "ja":
        return "Japanese"
    case "ru-RU", "ru":
        return "Russian"
    case "fr-FR", "fr":
        return "French"
    case "de-DE", "de":
        return "German"
    case "es-ES", "es":
        return "Spanish"
    case "pt-BR", "pt":
        return "Portuguese"
    default:
        return locale  // Return locale itself for unknown locales
    }
}
```

### File: `/internal/types/const.go` (line 24-25)

**Context Key Definition:**
```go
// LanguageContextKey is the context key for user language preference (e.g. "zh-CN", "en-US")
LanguageContextKey ContextKey = "Language"
```

---

## 2. Current Wiki Ingest Language Logic (NEEDS REFACTORING)

### File: `/internal/application/service/wiki_ingest.go` (line 135-141)

**Current Implementation - Hardcoded and Limited:**

```go
// Determine language
lang := "the same language as the source document"
if kb.WikiConfig.WikiLanguage == "zh" {
    lang = "Chinese (中文)"
} else if kb.WikiConfig.WikiLanguage == "en" {
    lang = "English"
}
```

**Problems with Current Approach:**
1. **Hardcoded mapping** - Only supports 2 languages (zh, en)
2. **Limited coverage** - No support for Korean, Japanese, Russian, French, German, Spanish, Portuguese
3. **Inconsistent naming** - Uses different format than middleware ("Chinese (中文)" vs middleware's "Chinese (Simplified)")
4. **Not reusing existing infrastructure** - Duplicates logic already in `LanguageLocaleName()`
5. **Default fallback unclear** - Uses generic string "the same language as the source document"

---

## 3. Refactoring Recommendations

### Recommended Solution: Reuse `LanguageLocaleName()`

Replace wiki_ingest.go lines 135-141 with:

```go
// Get human-readable language name for LLM prompts
// Supports: Chinese (Simplified/Traditional), English, Korean, Japanese, Russian, French, German, Spanish, Portuguese
// Falls back to: Default language from context or WEKNORA_LANGUAGE env, or "zh-CN"
lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

**Changes Required:**

1. **In `wiki_ingest.go` ProcessWikiIngest() method:**

   Before (current, lines 135-141):
   ```go
   // Determine language
   lang := "the same language as the source document"
   if kb.WikiConfig.WikiLanguage == "zh" {
       lang = "Chinese (中文)"
   } else if kb.WikiConfig.WikiLanguage == "en" {
       lang = "English"
   }
   ```

   After (refactored):
   ```go
   // Get human-readable language name for LLM prompts
   // Reuses language mapping from middleware infrastructure (supports 9+ languages)
   lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
   ```

2. **Bonus: Align WikiConfig.WikiLanguage naming with context middleware:**

   Consider normalizing `WikiConfig.WikiLanguage` storage:
   - Currently: "zh", "en" (short codes)
   - Consider: Store full locale codes like "zh-CN", "en-US" for consistency with middleware
   - If changed, update migration to map old values → new values

3. **Optional: Further enhancement - use context language:**

   ```go
   // Determine language: prefer KB config, fallback to context, then env, then "zh-CN"
   lang := kb.WikiConfig.WikiLanguage
   if lang == "" {
       lang, _ = types.LanguageFromContext(ctx)
       if lang == "" {
           lang = types.DefaultLanguage()
       }
   }
   humanReadableLang := types.LanguageLocaleName(lang)
   ```

---

## 4. Integration Points

### Where `LanguageLocaleName()` is Already Used:

1. **`/internal/types/context_helpers.go`** - Definition (line 85-112)
2. **`LanguageNameFromContext()` function** - Calls it (line 82)

### Where to Add New Usage:

1. **`/internal/application/service/wiki_ingest.go`** - ProcessWikiIngest() (line 137)
2. **`/internal/application/service/wiki_ingest.go`** - extractEntitiesAndConcepts() (line 305)
3. **`/internal/application/service/wiki_ingest.go`** - upsertExtractedPages() (line 317)
4. **`/internal/application/service/wiki_ingest.go`** - rebuildIndexPage() (line 416)

All these functions currently pass language parameter to LLM prompts.

---

## 5. Implementation Checklist

- [ ] **Step 1:** Replace hardcoded language logic in `wiki_ingest.go` line 137 with `types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)`
- [ ] **Step 2:** Remove duplicated language mapping code (lines 135-141)
- [ ] **Step 3:** Add import if needed: `"github.com/Tencent/WeKnora/internal/types"` (likely already imported)
- [ ] **Step 4:** Test with different language values: "zh", "en", "zh-CN", "en-US" to ensure fallback works
- [ ] **Step 5:** Update documentation/comments to reference the shared `LanguageLocaleName()` function
- [ ] **Step 6 (Optional):** Consider full locale code normalization (zh → zh-CN, en → en-US) for consistency

---

## 6. Additional Observations

### Language vs WikiLanguage

The codebase has two language concepts:

1. **Context Language** (middleware-driven):
   - HTTP request Accept-Language header
   - WEKNORA_LANGUAGE environment variable
   - Used for UI rendering, document processing language
   - Format: "zh-CN", "en-US" (full locale)

2. **WikiLanguage** (KB config):
   - Stored in `KnowledgeBase.WikiConfig.WikiLanguage`
   - Format: Short codes "zh", "en"
   - Used specifically for wiki page generation language preference

### Recommendation: Unify Language Format

Consider normalizing both to use full locale codes:
- Store "zh-CN" instead of "zh"
- Store "en-US" instead of "en"
- This allows direct reuse of `LanguageLocaleName()` without conversion

---

## 7. Code Migration Path

If you decide to normalize WikiLanguage format:

```go
// Migration helper function
func normalizeWikiLanguage(shortCode string) string {
    switch shortCode {
    case "zh":
        return "zh-CN"
    case "en":
        return "en-US"
    default:
        return shortCode
    }
}

// In wiki_ingest.go ProcessWikiIngest():
normalizedLang := normalizeWikiLanguage(kb.WikiConfig.WikiLanguage)
lang := types.LanguageLocaleName(normalizedLang)
```

---

## Summary

### Quick Refactoring (No Schema Changes)

```diff
- lang := "the same language as the source document"
- if kb.WikiConfig.WikiLanguage == "zh" {
-     lang = "Chinese (中文)"
- } else if kb.WikiConfig.WikiLanguage == "en" {
-     lang = "English"
- }
+ lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

**Benefits:**
- ✅ Supports 9+ languages instead of 2
- ✅ Reuses existing, tested infrastructure
- ✅ Eliminates code duplication
- ✅ Consistent naming with middleware
- ✅ Single line of code vs 6 lines

### Full Refactoring (With Schema Normalization)

1. Update `KnowledgeBase.WikiConfig.WikiLanguage` format from short codes to full locales
2. Run database migration to convert existing values
3. Use `types.LanguageLocaleName()` directly without conversion
4. All language references consistent across codebase

---

## Files Modified Summary

| File | Changes | Lines |
|------|---------|-------|
| `/internal/application/service/wiki_ingest.go` | Replace lines 135-141 with single call to `types.LanguageLocaleName()` | 135-141 |
| **Reuse** | `/internal/types/context_helpers.go` | Use existing `LanguageLocaleName()` function | 85-112 |
| **Reuse** | `/internal/middleware/language.go` | Existing language detection infrastructure | All |
| **Optional** | Database migration | Normalize WikiLanguage format (if pursuing full refactoring) | - |
