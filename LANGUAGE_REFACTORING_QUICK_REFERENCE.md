# Language Middleware Refactoring - Quick Reference

## TL;DR

Replace 6 lines of hardcoded language logic in `wiki_ingest.go` with 1 line that reuses tested infrastructure.

## What to Change

**File:** `/internal/application/service/wiki_ingest.go`
**Lines:** 135-141
**Method:** `ProcessWikiIngest()`

### Simple Copy-Paste Replacement

```go
// OLD CODE (delete this):
    // Determine language
    lang := "the same language as the source document"
    if kb.WikiConfig.WikiLanguage == "zh" {
        lang = "Chinese (中文)"
    } else if kb.WikiConfig.WikiLanguage == "en" {
        lang = "English"
    }

// NEW CODE (replace with this):
    // Determine language
    lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)
```

## Key Benefits

| Aspect | Before | After |
|--------|--------|-------|
| Languages Supported | 2 (zh, en) | 9+ (zh, en, ko, ja, ru, fr, de, es, pt) |
| Code Lines | 6 | 1 |
| Consistency | Varies | Unified middleware naming |
| Maintenance | Hardcoded | Centralized in `context_helpers.go` |
| Extension Cost | High | Low |

## No Breaking Changes

✅ Existing code works as-is
✅ No database migration needed
✅ No API changes
✅ Fully backward compatible

## Function Reference

**Function:** `types.LanguageLocaleName(locale string) string`
**Location:** `/internal/types/context_helpers.go` lines 85-112
**Already imported in wiki_ingest.go:** ✅ Yes (line 14)

**Supported Inputs:**

```
"zh-CN" or "zh" → "Chinese (Simplified)"
"zh-TW" or "zh-HK" → "Chinese (Traditional)"
"en-US" or "en" → "English"
"ko-KR" or "ko" → "Korean"
"ja-JP" or "ja" → "Japanese"
"ru-RU" or "ru" → "Russian"
"fr-FR" or "fr" → "French"
"de-DE" or "de" → "German"
"es-ES" or "es" → "Spanish"
"pt-BR" or "pt" → "Portuguese"
[any unknown] → returns the input as-is
```

## Testing

### Quick Test
```bash
cd /Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora
go test -v ./internal/types
go test -v ./internal/application/service
```

### Manual Verification
```go
// In your test:
fmt.Println(types.LanguageLocaleName("zh"))     // Should print: Chinese (Simplified)
fmt.Println(types.LanguageLocaleName("en"))     // Should print: English
fmt.Println(types.LanguageLocaleName("ko"))     // Should print: Korean
```

## Related Documentation

- Full analysis: `LANGUAGE_MIDDLEWARE_ANALYSIS.md`
- Implementation plan: `REFACTORING_PLAN.md`
- Main analysis: `AGENT_WIKI_ANALYSIS.md`

## Files Involved

| File | Purpose | Action |
|------|---------|--------|
| `/internal/middleware/language.go` | Language extraction middleware | Reference only |
| `/internal/types/context_helpers.go` | Language mapping functions | Reuse `LanguageLocaleName()` |
| `/internal/application/service/wiki_ingest.go` | Wiki ingest service | Update lines 135-141 |

## Implementation Checklist

- [ ] Open `wiki_ingest.go`
- [ ] Find lines 135-141
- [ ] Delete the 6-line language logic
- [ ] Replace with `lang := types.LanguageLocaleName(kb.WikiConfig.WikiLanguage)`
- [ ] Save file
- [ ] Run tests: `go test ./internal/...`
- [ ] Commit changes

That's it! 🎉

