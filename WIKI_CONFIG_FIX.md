# Wiki Config Fix - Complete Implementation Guide

## Problem Statement
The `wiki_config` field cannot be updated via the KB update endpoint because `KnowledgeBaseConfig` struct is missing the `WikiConfig` field.

## Solution Overview
Add `WikiConfig` field to `KnowledgeBaseConfig` struct and update the service layer to persist it.

---

## Change 1: Update Type Definition

**File:** `internal/types/knowledgebase.go`

**Location:** Lines 99-107

**Current Code:**
```go
// KnowledgeBaseConfig represents the knowledge base configuration
type KnowledgeBaseConfig struct {
	// Chunking configuration
	ChunkingConfig ChunkingConfig `yaml:"chunking_config"         json:"chunking_config"`
	// Image processing configuration
	ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
	// FAQ configuration (only for FAQ type knowledge bases)
	FAQConfig *FAQConfig `yaml:"faq_config"              json:"faq_config"`
}
```

**Fixed Code:**
```go
// KnowledgeBaseConfig represents the knowledge base configuration
type KnowledgeBaseConfig struct {
	// Chunking configuration
	ChunkingConfig ChunkingConfig `yaml:"chunking_config"         json:"chunking_config"`
	// Image processing configuration
	ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
	// FAQ configuration (only for FAQ type knowledge bases)
	FAQConfig *FAQConfig `yaml:"faq_config"              json:"faq_config"`
	// Wiki configuration (for document type knowledge bases with wiki feature enabled)
	WikiConfig *WikiConfig `yaml:"wiki_config"             json:"wiki_config"`
}
```

**Changes:**
- Added `WikiConfig *WikiConfig` field with proper YAML and JSON tags

---

## Change 2: Update Service Layer

**File:** `internal/application/service/knowledgebase.go`

**Location:** Lines 265-311 (UpdateKnowledgeBase method)

**Current Code (Lines 288-297):**
```go
	// Update the knowledge base properties
	kb.Name = name
	kb.Description = description
	if config != nil {
		kb.ChunkingConfig = config.ChunkingConfig
		kb.ImageProcessingConfig = config.ImageProcessingConfig
		if config.FAQConfig != nil {
			kb.FAQConfig = config.FAQConfig
		}
	}
```

**Fixed Code:**
```go
	// Update the knowledge base properties
	kb.Name = name
	kb.Description = description
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

**Changes:**
- Added conditional check to update `kb.WikiConfig` from `config.WikiConfig`

---

## Change 3: Update Frontend Type Hints (Optional but Recommended)

**File:** `frontend/src/api/knowledge-base/index.ts`

**Location:** Lines 11-33 (createKnowledgeBase function)

**Current Code:**
```typescript
export function createKnowledgeBase(data: { 
  name: string; 
  description?: string; 
  type?: 'document' | 'faq';
  chunking_config?: any;
  embedding_model_id?: string;
  summary_model_id?: string;
  vlm_config?: {
    enabled: boolean;
    model_id?: string;
  };
  storage_provider_config?: { provider: string };
  storage_config?: any; // legacy, kept for backward compat (dual-write)
  asr_config?: {
    enabled: boolean;
    model_id?: string;
    language?: string;
  };
  extract_config?: any;
  faq_config?: { index_mode: string; question_index_mode?: string };
}) {
  return post(`/api/v1/knowledge-bases`, data);
}
```

**Fixed Code:**
```typescript
export function createKnowledgeBase(data: { 
  name: string; 
  description?: string; 
  type?: 'document' | 'faq';
  chunking_config?: any;
  embedding_model_id?: string;
  summary_model_id?: string;
  vlm_config?: {
    enabled: boolean;
    model_id?: string;
  };
  storage_provider_config?: { provider: string };
  storage_config?: any; // legacy, kept for backward compat (dual-write)
  asr_config?: {
    enabled: boolean;
    model_id?: string;
    language?: string;
  };
  extract_config?: any;
  faq_config?: { index_mode: string; question_index_mode?: string };
  wiki_config?: {
    enabled: boolean;
    auto_ingest?: boolean;
    synthesis_model_id?: string;
    wiki_language?: string;
    max_pages_per_ingest?: number;
  };
}) {
  return post(`/api/v1/knowledge-bases`, data);
}
```

**Changes:**
- Added `wiki_config` property with full type definition for better IDE autocomplete

---

## Testing

### Test Case 1: Create KB with wiki_config

```bash
curl -X POST http://localhost:8080/api/v1/knowledge-bases \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "name": "Test KB with Wiki",
    "type": "document",
    "embedding_model_id": "text-embedding-ada-002",
    "wiki_config": {
      "enabled": true,
      "auto_ingest": true,
      "synthesis_model_id": "gpt-4",
      "wiki_language": "en"
    }
  }'
```

**Expected Result:**
- KB created with wiki_config populated ✅

### Test Case 2: Update KB with wiki_config (MAIN TEST)

```bash
# First, get the KB to see initial state
curl -X GET http://localhost:8080/api/v1/knowledge-bases/{id} \
  -H "Authorization: Bearer <token>"

# Then update it
curl -X PUT http://localhost:8080/api/v1/knowledge-bases/{id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "name": "Updated KB",
    "description": "Updated description",
    "config": {
      "wiki_config": {
        "enabled": false
      }
    }
  }'

# Verify the update
curl -X GET http://localhost:8080/api/v1/knowledge-bases/{id} \
  -H "Authorization: Bearer <token>"
```

**Expected Result (Before Fix):**
- wiki_config remains unchanged (bug) ❌

**Expected Result (After Fix):**
- wiki_config.enabled changed to false ✅

### Test Case 3: Update other config fields (Regression Test)

```bash
curl -X PUT http://localhost:8080/api/v1/knowledge-bases/{id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "name": "Updated KB",
    "config": {
      "chunking_config": {
        "chunk_size": 1024,
        "chunk_overlap": 100
      }
    }
  }'
```

**Expected Result:**
- Chunking config updated ✅
- wiki_config preserved ✅

---

## Verification Checklist

After implementing the fix, verify:

- [ ] Type definition compiles without errors
- [ ] Backend builds successfully
- [ ] Test Case 1 passes (create with wiki_config)
- [ ] Test Case 2 passes (update with wiki_config)
- [ ] Test Case 3 passes (no regression on other configs)
- [ ] Frontend compiles without TypeScript errors
- [ ] Database column `wiki_config` contains expected JSON
- [ ] EnsureDefaults() still works correctly
- [ ] Wiki feature still functions end-to-end

---

## Code Review Checklist

- [ ] All GORM tags are consistent with other fields
- [ ] JSON marshaling/unmarshaling follows existing patterns
- [ ] Null safety handled (config can be nil)
- [ ] No breaking changes to existing API
- [ ] Backwards compatible (existing requests still work)
- [ ] Service layer properly validates wiki_config
- [ ] Repository layer properly persists changes
- [ ] EnsureDefaults() method still handles wiki_config correctly

---

## Rollback Plan

If needed, to rollback:

1. Revert Change 1: Remove WikiConfig field from KnowledgeBaseConfig
2. Revert Change 2: Remove the wiki_config update code from service
3. Revert Change 3: Remove wiki_config from frontend type hints
4. Rebuild and redeploy

---

## Impact Analysis

### Affected Files
1. `internal/types/knowledgebase.go` - 1 line added
2. `internal/application/service/knowledgebase.go` - 3 lines added
3. `frontend/src/api/knowledge-base/index.ts` - 7 lines added

### Backwards Compatibility
- ✅ Fully backwards compatible
- ✅ Existing requests without wiki_config still work
- ✅ Existing KBs with wiki_config unaffected
- ✅ No database migration needed

### Performance Impact
- ✅ Negligible - only adds one more field to struct

### Security Impact
- ✅ No security issues introduced
- ✅ Same permission checks apply
- ✅ wiki_config validated through EnsureDefaults()

---

## Related Code

### WikiConfig Struct Definition
Location: `internal/types/wiki_page.go` (Lines 89-120)

### EnsureDefaults() Method
Location: `internal/types/knowledgebase.go` (Lines 472-508)
- Already handles wiki_config validation
- No changes needed

### Database Column
Location: `migrations/versioned/000032_wiki_pages.up.sql` (Line 7)
- Already created as JSONB
- No changes needed

---

## Timeline
- **Implementation:** ~5 minutes
- **Testing:** ~10 minutes
- **Code Review:** ~5 minutes
- **Deployment:** ~5 minutes
- **Total:** ~25 minutes
