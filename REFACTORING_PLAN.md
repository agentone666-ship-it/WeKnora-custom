# Wiki Ingest Language Refactoring - Implementation Plan

## Problem Statement

The wiki ingest pipeline contains hardcoded language mapping logic (lines 135-141 in `wiki_ingest.go`) that:
- Only supports 2 languages (Chinese and English)
- Duplicates logic already available in `types.LanguageLocaleName()`
- Uses inconsistent naming compared to the existing language middleware
- Cannot be easily extended for new languages

## Solution

Reuse the existing `types.LanguageLocaleName()` function from `/internal/types/context_helpers.go` which:
- Supports 9+ languages (Chinese, English, Korean, Japanese, Russian, French, German, Spanish, Portuguese)
- Is already tested and used throughout the codebase
- Provides consistent language name formatting for LLM prompts
- Handles unknown languages gracefully

## Implementation

### Change Location

**File:** `/internal/application/service/wiki_ingest.go`
**Method:** `ProcessWikiIngest()`
**Lines:** 135-141

### Before (Current Code)

```go
135	    // Determine language
136	    lang := "the same language as the source document"
137	    if kb.WikiConfig.WikiLanguage == "zh" {
138	        lang = "Chinese (中文)"
139	    } else if kb.WikiConfig.WikiLanguage == "en" {
140	        lang = "English"
141	    }
```

### After (Refactored Code)

```go
135	    // Determine language - reuse middleware infrastructure for consistent naming
136	    // Supports: Chinese (Simplified/Traditional), English, Korean, Japanese, Russian, French, German, Spanish, Portuguese
137	    lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

### Code Diff

```diff
-   // Determine language
-   lang := "the same language as the source document"
-   if kb.WikiConfig.WikiLanguage == "zh" {
-       lang = "Chinese (中文)"
-   } else if kb.WikiConfig.WikiLanguage == "en" {
-       lang = "English"
-   }
+   // Determine language - reuse middleware infrastructure for consistent naming
+   // Supports: Chinese (Simplified/Traditional), English, Korean, Japanese, Russian, French, German, Spanish, Portuguese
+   lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

### Verification

The types package is already imported at line 14:
```go
"github.com/Tencent/WeKnora/internal/types"
```

No additional imports needed.

## Impact Analysis

### Benefits

✅ **Code Reduction:** 6 lines → 3 lines (50% reduction)
✅ **Language Coverage:** 2 languages → 9+ languages
✅ **Consistency:** Aligns with middleware language naming conventions
✅ **Maintainability:** Centralized language mapping (single source of truth)
✅ **Extensibility:** Adding new languages only requires updating `LanguageLocaleName()`
✅ **Testing:** Reuses existing, tested function

### Supported Languages (After Refactoring)

| Code | Output |
|------|--------|
| `zh-CN`, `zh`, `zh-Hans` | Chinese (Simplified) |
| `zh-TW`, `zh-HK`, `zh-Hant` | Chinese (Traditional) |
| `en-US`, `en`, `en-GB` | English |
| `ko-KR`, `ko` | Korean |
| `ja-JP`, `ja` | Japanese |
| `ru-RU`, `ru` | Russian |
| `fr-FR`, `fr` | French |
| `de-DE`, `de` | German |
| `es-ES`, `es` | Spanish |
| `pt-BR`, `pt` | Portuguese |
| *unknown* | Returns the locale code as-is |

### Backward Compatibility

✅ **Fully Compatible**
- Existing KB configs with `WikiLanguage: "zh"` and `WikiLanguage: "en"` continue to work
- The function handles short codes: `"zh" → "Chinese (Simplified)"`, `"en" → "English"`
- No database schema changes required
- No migration needed

## Testing Recommendations

### Test Cases

```go
// Test 1: Existing short code support
lang := types.LanguageLocaleName("zh")     // Expected: "Chinese (Simplified)"
lang := types.LanguageLocaleName("en")     // Expected: "English"

// Test 2: Full locale code support
lang := types.LanguageLocaleName("zh-CN")  // Expected: "Chinese (Simplified)"
lang := types.LanguageLocaleName("en-US")  // Expected: "English"
lang := types.LanguageLocaleName("ko-KR")  // Expected: "Korean"

// Test 3: Unknown locale fallback
lang := types.LanguageLocaleName("xx-YY")  // Expected: "xx-YY"
lang := types.LanguageLocaleName("")       // Expected: ""
```

### Integration Testing

1. Create a wiki KB with `WikiLanguage: "zh"` and verify summary pages are generated in Chinese
2. Create a wiki KB with `WikiLanguage: "en"` and verify summary pages are generated in English
3. (Optional) Create a wiki KB with `WikiLanguage: "ko"` and verify Korean language support

## Future Enhancements

### Option 1: Full Locale Code Normalization (Recommended)

Store full locale codes in WikiConfig to align with middleware:

```go
// In WikiConfig struct
type WikiConfig struct {
    WikiLanguage string // Store "zh-CN" instead of "zh"
    // ...
}

// No conversion needed
lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

### Option 2: Context-Aware Language Selection

Use context language as fallback:

```go
// Determine language: prefer KB config, fallback to context/env, then default
lang := kb.WikiConfig.WikiLanguage
if lang == "" {
    // Fallback to context language
    if ctxLang, ok := types.LanguageFromContext(ctx); ok {
        lang = ctxLang
    } else {
        // Fallback to env/default
        lang = types.DefaultLanguage()
    }
}
humanReadableLang := types.LanguageLocaleName(lang)
```

## Implementation Steps

1. **Step 1:** Open `/internal/application/service/wiki_ingest.go`
2. **Step 2:** Replace lines 135-141 with the refactored code shown above
3. **Step 3:** Save the file
4. **Step 4:** Run tests to verify:
   ```bash
   go test ./internal/application/service/...
   go test ./internal/types/...
   ```
5. **Step 5:** (Optional) Run the wiki ingest pipeline with test documents

## Rollback Plan

If issues arise:
```bash
git checkout HEAD -- internal/application/service/wiki_ingest.go
```

## Related Files

### Definition Location
- **File:** `/internal/types/context_helpers.go`
- **Function:** `LanguageLocaleName()` (lines 85-112)

### Other Usage Points (Informational)
- `/internal/types/context_helpers.go` - `LanguageNameFromContext()` function (line 82)

### Similar Patterns (For Future Refactoring)
- Search for other hardcoded language mappings in the codebase that could benefit from centralization

## Sign-Off

- **Requested By:** User (mentions "中间件中已经有language的判断逻辑")
- **Implementation Date:** [TBD]
- **Reviewer:** [TBD]
- **Approval:** [TBD]

