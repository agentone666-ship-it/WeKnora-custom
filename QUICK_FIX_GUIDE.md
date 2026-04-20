# Quick Fix Guide: Wiki Config Update Bug

⏱️ **Estimated Fix Time:** 5 minutes  
📝 **Complexity:** Low (3 struct fields + 1 service method)  
✅ **Backward Compatible:** Yes  

---

## The Problem

Users can create a KB with wiki enabled, but **cannot update wiki settings** after creation.

**Frontend sends:** `PUT /api/v1/knowledge-bases/{id} { config: { wiki_config: {...} } }`  
**Backend receives:** Ignored (struct field missing)  
**Result:** Changes don't save ❌

---

## The Solution: 3 Changes

### Change 1: Add WikiConfig to KnowledgeBaseConfig

**File:** `internal/types/knowledgebase.go` (Line 107)

**BEFORE:**
```go
type KnowledgeBaseConfig struct {
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config"`
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config"`
}
```

**AFTER:**
```go
type KnowledgeBaseConfig struct {
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config"`
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config"`
    WikiConfig *WikiConfig `yaml:"wiki_config" json:"wiki_config"`
}
```

**What to add:** One line after FAQConfig field ⬆️

---

### Change 2: Update UpdateKnowledgeBase Service

**File:** `internal/application/service/knowledgebase.go` (Line 277)

**BEFORE:**
```go
if config != nil {
    kb.ChunkingConfig = config.ChunkingConfig
    kb.ImageProcessingConfig = config.ImageProcessingConfig
    if config.FAQConfig != nil {
        kb.FAQConfig = config.FAQConfig
    }
}
```

**AFTER:**
```go
if config != nil {
    kb.ChunkingConfig = config.ChunkingConfig
    kb.ImageProcessingConfig = config.ImageProcessingConfig
    if config.FAQConfig != nil {
        kb.FAQConfig = config.FAQConfig
    }
    if config.WikiConfig != nil {
        kb.WikiConfig = config.WikiConfig
    }
}
```

**What to add:** 2-3 lines after FAQConfig handling ⬆️

---

### Change 3 (Optional): Update Frontend Type Hints

**File:** `frontend/src/api/knowledge-base/index.ts` (Line 38)

**In createKnowledgeBase function signature, add:**
```typescript
wiki_config?: {
  enabled: boolean;
  auto_ingest?: boolean;
  synthesis_model_id?: string;
  wiki_language?: string;
  max_pages_per_ingest?: number;
};
```

**Why:** Improves IDE autocomplete for developers (not required for functionality)

---

## Testing the Fix

### Quick Test (30 seconds)

1. Create new KB with wiki enabled
2. Edit the KB → Wiki Settings
3. Change synthesis model
4. Save
5. Reload the page
6. ✅ Model should still be changed

### Full Test (2 minutes)

```bash
# Run existing tests
go test ./internal/application/service/... -run TestUpdateKnowledgeBase

# Or manually test via API:
# 1. Create KB without wiki
curl -X POST http://localhost:8080/api/v1/knowledge-bases \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","type":"document"}'

# 2. Update with wiki enabled
curl -X PUT http://localhost:8080/api/v1/knowledge-bases/{id} \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","config":{"wiki_config":{"enabled":true,"synthesis_model_id":"gpt-4"}}}'

# 3. Verify in response and DB
```

---

## Files Modified

| File | Changes | Lines |
|------|---------|-------|
| `internal/types/knowledgebase.go` | Add 1 field | +1 |
| `internal/application/service/knowledgebase.go` | Add 2 lines | +2-3 |
| `frontend/src/api/knowledge-base/index.ts` | Add type hint | +6 (optional) |

**Total:** ~10 LOC across 2-3 files

---

## Verification Checklist

- [ ] Go compiles without errors
- [ ] TypeScript compiles without errors (if frontend changed)
- [ ] Tests pass
- [ ] Can create KB with wiki_config
- [ ] Can update KB wiki_config
- [ ] Changes persist after page reload
- [ ] No regressions in other KB settings updates

---

## Rollback (if needed)

Simply revert your changes:
```bash
git diff internal/types/knowledgebase.go      # See changes
git diff internal/application/service/knowledgebase.go  # See changes
git checkout internal/types/knowledgebase.go          # Undo
git checkout internal/application/service/knowledgebase.go  # Undo
```

---

## Why This Happens

The system uses two different struct types for KB operations:

1. **Create:** Uses full `KnowledgeBase` struct → all fields work ✅
2. **Update:** Uses `KnowledgeBaseConfig` struct → missing fields break ❌

Adding `WikiConfig` to `KnowledgeBaseConfig` bridges this gap.

---

## References

**For more details:**
- Full architecture: `KB_WIKI_ARCHITECTURE_ANALYSIS.md`
- Problem analysis: `WIKI_CONFIG_FLOW_ANALYSIS.md`
- Implementation plan: Plans directory

**Related code:**
- Handler: `internal/handler/knowledgebase.go` (Lines 433-488)
- Repository: `internal/application/repository/knowledgebase.go`
- Type def: `internal/types/wiki_page.go` (WikiConfig struct)

---

## Common Questions

**Q: Will this break existing code?**  
A: No, `WikiConfig` is optional (pointer), null values are valid.

**Q: Do I need a database migration?**  
A: No, the column already exists (`migrations/000032_wiki_pages.up.sql`).

**Q: What about the frontend?**  
A: The frontend already sends wiki_config; the backend just wasn't receiving it.

**Q: Can I deploy this independently?**  
A: Yes, it's backward compatible. Deploy backend first, frontend anytime.

**Q: How do I know it's working?**  
A: Edit wiki settings, save, reload page. Settings should persist.

---

## Contact / Issues

If you get compilation errors:
1. Check you added the field with correct struct tags
2. Run `go fmt` to fix formatting
3. Check imports if WikiConfig type not found

If wiki settings still aren't saving:
1. Verify both changes were applied
2. Restart backend service
3. Check browser cache (hard refresh: Cmd+Shift+R)

---

**Status:** Ready to implement ✅  
**Last Updated:** April 20, 2026
