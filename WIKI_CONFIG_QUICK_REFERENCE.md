# Wiki Config Flow - Quick Reference

## Critical Finding: UPDATE BROKEN ❌

When updating a KB via `PUT /api/v1/knowledge-bases/{id}`, the `wiki_config` is **NOT** persisted.

## Why?

```
UpdateKnowledgeBaseRequest struct:
├── Name: string
├── Description: string
└── Config: KnowledgeBaseConfig ❌ Missing WikiConfig field
    ├── ChunkingConfig
    ├── ImageProcessingConfig
    └── FAQConfig
```

The `KnowledgeBaseConfig` struct lacks the `WikiConfig` field, so:
- ✅ Frontend CAN send `config: { wiki_config: {...} }` 
- ❌ But backend IGNORES it during unmarshaling
- ❌ Service never updates kb.WikiConfig
- ❌ Database column remains unchanged

## Creation Works ✅

```
POST /api/v1/knowledge-bases
{
  "name": "My KB",
  "wiki_config": { "enabled": true, ... }  ← FULL KnowledgeBase struct accepted
}

Handler parses as types.KnowledgeBase (not KnowledgeBaseConfig)
→ Service receives wiki_config
→ EnsureDefaults() validates it
→ Repository saves it to database ✅
```

## Update Broken ❌

```
PUT /api/v1/knowledge-bases/{id}
{
  "name": "Updated",
  "config": {
    "wiki_config": { "enabled": false }  ← Sent to backend
  }
}

Handler parses as UpdateKnowledgeBaseRequest
→ Config field is KnowledgeBaseConfig (no WikiConfig!)
→ JSON unmarshaling IGNORES wiki_config field
→ Service never sees it
→ Database NOT updated ❌
```

## Files That Need Changes

### 1. `/internal/types/knowledgebase.go` (Lines 99-107)
```diff
  type KnowledgeBaseConfig struct {
      ChunkingConfig ChunkingConfig
      ImageProcessingConfig ImageProcessingConfig
      FAQConfig *FAQConfig
+     WikiConfig *WikiConfig  // ADD THIS LINE
  }
```

### 2. `/internal/application/service/knowledgebase.go` (Lines 291-296)
```diff
  if config != nil {
      kb.ChunkingConfig = config.ChunkingConfig
      kb.ImageProcessingConfig = config.ImageProcessingConfig
      if config.FAQConfig != nil {
          kb.FAQConfig = config.FAQConfig
      }
+     if config.WikiConfig != nil {
+         kb.WikiConfig = config.WikiConfig
+     }
  }
```

### 3. `/frontend/src/api/knowledge-base/index.ts` (createKnowledgeBase function)
Add wiki_config to type hints (for TypeScript completion):
```typescript
wiki_config?: {
  enabled: boolean;
  auto_ingest?: boolean;
  synthesis_model_id?: string;
  wiki_language?: string;
  max_pages_per_ingest?: number;
};
```

## Database Path

### Storage
- Table: `knowledge_bases`
- Column: `wiki_config` (JSONB)
- Created by: Migration `000032_wiki_pages.up.sql`

### Data Types
- Go struct: `WikiConfig` in `/internal/types/wiki_page.go`
- Fields: `enabled`, `auto_ingest`, `synthesis_model_id`, `wiki_language`, `max_pages_per_ingest`
- GORM mapping: `gorm:"column:wiki_config;type:json"`

## Request/Response Flow

### Create (Works ✅)
```
POST /api/v1/knowledge-bases
→ CreateKnowledgeBaseRequest (full KnowledgeBase)
→ Handler: c.ShouldBindJSON(&types.KnowledgeBase)
→ Service: CreateKnowledgeBase(ctx, &kb)
→ Repository: GORM.Create(kb)
→ DB: wiki_config saved ✅
```

### Update (Broken ❌)
```
PUT /api/v1/knowledge-bases/{id}
→ UpdateKnowledgeBaseRequest (Config: KnowledgeBaseConfig)
→ Handler: c.ShouldBindJSON(&UpdateKnowledgeBaseRequest)
→ Service: UpdateKnowledgeBase(ctx, id, name, desc, config)
→ Service: config.WikiConfig is nil (never set!)
→ Repository: GORM.Save(kb) with old wiki_config
→ DB: wiki_config NOT updated ❌
```

## Handler Code Locations

| Operation | Handler | File:Lines | Status |
|-----------|---------|-----------|--------|
| Create | CreateKnowledgeBase | knowledgebase.go:114-147 | ✅ |
| Read | GetKnowledgeBase | knowledgebase.go:260-284 | ✅ |
| Update | UpdateKnowledgeBase | knowledgebase.go:446-488 | ❌ |
| Delete | DeleteKnowledgeBase | knowledgebase.go:502-536 | ✅ |

## Service Layer Locations

| Operation | Service Method | File:Lines | Status |
|-----------|----------------|-----------|--------|
| Create | CreateKnowledgeBase | knowledgebase.go:73-98 | ✅ |
| Read | GetKnowledgeBaseByID | knowledgebase.go:101-117 | ✅ |
| Update | UpdateKnowledgeBase | knowledgebase.go:265-311 | ❌ |
| List | ListKnowledgeBases | knowledgebase.go:157-210 | ✅ |

## Frontend Integration

### API Functions
- `createKnowledgeBase()` - accepts any data ✅
- `updateKnowledgeBase()` - accepts any config ✅ (but backend ignores wiki_config)
- `getKnowledgeBaseById()` - returns full KB ✅

### Type Hints Location
- File: `/frontend/src/api/knowledge-base/index.ts`
- Lines: 11-33 (createKnowledgeBase)
- Lines: 42-44 (updateKnowledgeBase)

## Test Cases

To verify the fix works:

### Test 1: Create with wiki_config
```
POST /api/v1/knowledge-bases
{
  "name": "Test KB",
  "type": "document",
  "wiki_config": {
    "enabled": true,
    "auto_ingest": true,
    "synthesis_model_id": "gpt-4",
    "wiki_language": "en"
  }
}
→ Should persist wiki_config ✅
```

### Test 2: Update wiki_config
```
PUT /api/v1/knowledge-bases/{id}
{
  "name": "Updated",
  "config": {
    "wiki_config": {
      "enabled": false
    }
  }
}
→ Before fix: wiki_config NOT updated ❌
→ After fix: wiki_config updated ✅
```

## Implementation Time Estimate
- Add WikiConfig to KnowledgeBaseConfig: 2 minutes
- Update Service layer: 3 minutes
- Update Frontend types: 2 minutes
- Testing: 5 minutes
- **Total: ~12 minutes**
