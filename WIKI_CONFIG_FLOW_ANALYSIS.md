# WeKnora Wiki Config Flow Analysis - Frontend to Database

## Executive Summary

The `wiki_config` field **IS DEFINED** in the `KnowledgeBase` struct but **IS NOT CURRENTLY INCLUDED** in the `KnowledgeBaseConfig` struct used by the update endpoints. This means:

- ✅ `wiki_config` is stored in the database (`knowledge_bases.wiki_config` JSONB column)
- ✅ `wiki_config` can be set during KB creation (included in full `KnowledgeBase` struct)
- ❌ `wiki_config` **CANNOT** be updated via the `/knowledge-bases/{id}` update endpoint
- ⚠️ Frontend sends `config: { wiki_config: {...} }` but backend ignores it

---

## Detailed Flow Analysis

### 1. DATABASE LAYER
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/migrations/versioned/000032_wiki_pages.up.sql`

```sql
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS wiki_config JSONB;

COMMENT ON COLUMN knowledge_bases.wiki_config IS 
  'Wiki configuration: {"auto_ingest": bool, "synthesis_model_id": string, "wiki_language": string, "max_pages_per_ingest": int}';
```

**Status:** ✅ Column exists and properly typed as JSONB

---

### 2. TYPE DEFINITIONS

#### 2a. WikiConfig Struct
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/wiki_page.go` (Lines 89-120)

```go
type WikiConfig struct {
    // Enabled activates the wiki feature for this knowledge base
    Enabled bool `yaml:"enabled" json:"enabled"`
    
    // AutoIngest triggers wiki page generation/update when new documents are added
    AutoIngest bool `yaml:"auto_ingest" json:"auto_ingest"`
    
    // SynthesisModelID is the LLM model ID used for wiki page generation and updates
    SynthesisModelID string `yaml:"synthesis_model_id" json:"synthesis_model_id"`
    
    // WikiLanguage is the preferred language for wiki content (zh, en, auto)
    WikiLanguage string `yaml:"wiki_language" json:"wiki_language"`
    
    // MaxPagesPerIngest limits pages created/updated per ingest operation (0 = no limit)
    MaxPagesPerIngest int `yaml:"max_pages_per_ingest" json:"max_pages_per_ingest"`
}

// Implements driver.Valuer and sql.Scanner for GORM
func (c WikiConfig) Value() (driver.Value, error) { return json.Marshal(c) }
func (c *WikiConfig) Scan(value interface{}) error { ... }
```

**Status:** ✅ Properly defined with GORM marshaling

#### 2b. KnowledgeBase Struct
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/knowledgebase.go` (Line 76)

```go
type KnowledgeBase struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Type string `json:"type"`
    // ... other fields ...
    
    // WikiConfig stores wiki-specific configuration (only for wiki type knowledge bases)
    WikiConfig *WikiConfig `yaml:"wiki_config" json:"wiki_config" gorm:"column:wiki_config;type:json"`
    
    // ... timestamps ...
}
```

**Status:** ✅ Field exists with proper GORM mapping

#### 2c. KnowledgeBaseConfig Struct (REQUEST STRUCTURE)
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/knowledgebase.go` (Lines 99-107)

```go
type KnowledgeBaseConfig struct {
    // Chunking configuration
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config"`
    
    // Image processing configuration
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
    
    // FAQ configuration (only for FAQ type knowledge bases)
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config"`
    
    // ❌ MISSING: WikiConfig is NOT included here!
}
```

**Status:** ❌ **CRITICAL ISSUE**: WikiConfig is missing from KnowledgeBaseConfig

---

### 3. HANDLER LAYER

#### 3a. CreateKnowledgeBase Handler
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/handler/knowledgebase.go` (Lines 114-147)

```go
func (h *KnowledgeBaseHandler) CreateKnowledgeBase(c *gin.Context) {
    ctx := c.Request.Context()
    
    // Parse request body as full KnowledgeBase struct
    var req types.KnowledgeBase
    if err := c.ShouldBindJSON(&req); err != nil {
        // ... error handling ...
        return
    }
    
    // Passes to service
    kb, err := h.service.CreateKnowledgeBase(ctx, &req)
    
    // ... response ...
}
```

**Request Format Accepted:**
```json
{
  "name": "My KB",
  "type": "document",
  "wiki_config": {
    "enabled": true,
    "auto_ingest": true,
    "synthesis_model_id": "gpt-4",
    "wiki_language": "en"
  }
}
```

**Status:** ✅ Can accept wiki_config during creation

#### 3b. UpdateKnowledgeBase Handler
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/handler/knowledgebase.go` (Lines 433-488)

```go
type UpdateKnowledgeBaseRequest struct {
    Name        string                     `json:"name"        binding:"required"`
    Description string                     `json:"description"`
    Config      *types.KnowledgeBaseConfig `json:"config"`  // ❌ Uses KnowledgeBaseConfig, not full KnowledgeBase
}

func (h *KnowledgeBaseHandler) UpdateKnowledgeBase(c *gin.Context) {
    // Parse as UpdateKnowledgeBaseRequest
    var req UpdateKnowledgeBaseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        // ... error handling ...
        return
    }
    
    // Calls service with config
    kb, err := h.service.UpdateKnowledgeBase(ctx, id, req.Name, req.Description, req.Config)
    
    // ... response ...
}
```

**Request Format:**
```json
{
  "name": "Updated KB Name",
  "description": "Updated description",
  "config": {
    "chunking_config": { ... },
    "image_processing_config": { ... }
    // ❌ wiki_config cannot be sent here because KnowledgeBaseConfig doesn't have it
  }
}
```

**Status:** ❌ **CRITICAL ISSUE**: Cannot pass wiki_config in update request

---

### 4. SERVICE LAYER

#### 4a. CreateKnowledgeBase Service
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/service/knowledgebase.go` (Lines 73-98)

```go
func (s *knowledgeBaseService) CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
    // Generate UUID and set creation timestamps
    if kb.ID == "" {
        kb.ID = uuid.New().String()
    }
    kb.CreatedAt = time.Now()
    kb.TenantID = types.MustTenantIDFromContext(ctx)
    kb.UpdatedAt = time.Now()
    kb.EnsureDefaults()  // ✅ This calls EnsureDefaults which validates wiki_config
    
    logger.Infof(ctx, "Creating knowledge base, ID: %s, tenant ID: %d, name: %s", kb.ID, kb.TenantID, kb.Name)
    
    if err := s.repo.CreateKnowledgeBase(ctx, kb); err != nil {
        return nil, err
    }
    
    return kb, nil
}
```

**Status:** ✅ Receives full KnowledgeBase with wiki_config

#### 4b. EnsureDefaults() Method
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/types/knowledgebase.go` (Lines 472-508)

```go
func (kb *KnowledgeBase) EnsureDefaults() {
    if kb == nil {
        return
    }
    if kb.Type == "" {
        kb.Type = KnowledgeBaseTypeDocument
    }
    
    // Clear type-specific configs that don't belong
    if kb.Type != KnowledgeBaseTypeFAQ {
        kb.FAQConfig = nil
    }
    
    // Set defaults for Wiki
    if kb.IsWikiEnabled() {  // Checks if WikiConfig != nil && WikiConfig.Enabled
        if kb.WikiConfig.WikiLanguage == "" {
            kb.WikiConfig.WikiLanguage = "auto"
        }
        if !kb.WikiConfig.AutoIngest {
            kb.WikiConfig.AutoIngest = true
        }
    }
    
    // Set defaults for FAQ ...
}

func (kb *KnowledgeBase) IsWikiEnabled() bool {
    return kb != nil && kb.WikiConfig != nil && kb.WikiConfig.Enabled
}
```

**Status:** ✅ Properly sets wiki_config defaults during creation

#### 4c. UpdateKnowledgeBase Service
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/service/knowledgebase.go` (Lines 265-311)

```go
func (s *knowledgeBaseService) UpdateKnowledgeBase(ctx context.Context,
    id string,
    name string,
    description string,
    config *types.KnowledgeBaseConfig,  // ❌ Only receives KnowledgeBaseConfig
) (*types.KnowledgeBase, error) {
    if id == "" {
        return nil, errors.New("knowledge base ID cannot be empty")
    }
    
    // Get existing knowledge base
    kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
    if err != nil {
        return nil, err
    }
    
    // Update the knowledge base properties
    kb.Name = name
    kb.Description = description
    if config != nil {
        kb.ChunkingConfig = config.ChunkingConfig
        kb.ImageProcessingConfig = config.ImageProcessingConfig
        if config.FAQConfig != nil {
            kb.FAQConfig = config.FAQConfig
        }
        // ❌ NO CODE TO UPDATE kb.WikiConfig - it's not in config!
    }
    kb.UpdatedAt = time.Now()
    kb.EnsureDefaults()  // ✅ But if wiki_config was passed, this would validate it
    
    if err := s.repo.UpdateKnowledgeBase(ctx, kb); err != nil {
        return nil, err
    }
    
    return kb, nil
}
```

**Status:** ❌ **CRITICAL ISSUE**: Cannot update wiki_config because it's not in the config parameter

---

### 5. REPOSITORY LAYER

#### 5a. CreateKnowledgeBase Repository
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/repository/knowledgebase.go` (Lines 25-28)

```go
func (r *knowledgeBaseRepository) CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
    return r.db.WithContext(ctx).Create(kb).Error  // ✅ GORM will serialize all fields, including wiki_config
}
```

**Status:** ✅ GORM properly handles all fields

#### 5b. UpdateKnowledgeBase Repository
**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/internal/application/repository/knowledgebase.go` (Lines 115-118)

```go
func (r *knowledgeBaseRepository) UpdateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
    return r.db.WithContext(ctx).Save(kb).Error  // ✅ GORM.Save() will update all fields, including wiki_config
}
```

**Status:** ✅ Repository properly saves wiki_config if it's set

---

### 6. FRONTEND API LAYER

**File:** `/Users/wizard/code/go/src/git.woa.com/wxg-prc/WeKnora/frontend/src/api/knowledge-base/index.ts`

#### 6a. createKnowledgeBase
```typescript
export function createKnowledgeBase(data: { 
  name: string; 
  description?: string; 
  type?: 'document' | 'faq';
  chunking_config?: any;
  embedding_model_id?: string;
  // ... other fields ...
  // ❓ No wiki_config field in type hints!
}) {
  return post(`/api/v1/knowledge-bases`, data);
}
```

**Status:** ⚠️ Type hints don't include wiki_config (but it would be accepted as any)

#### 6b. updateKnowledgeBase
```typescript
export function updateKnowledgeBase(id: string, data: { 
  name: string; 
  description?: string; 
  config: any  // Any config object
}) {
  return put(`/api/v1/knowledge-bases/${id}`, data);
}
```

**Status:** ⚠️ Frontend can send `config: { wiki_config: {...} }` but backend ignores it

---

## Tracing the Flow

### Creating a KB with wiki_config ✅
```
Frontend: POST /api/v1/knowledge-bases { name, type, wiki_config: {...} }
    ↓
Handler.CreateKnowledgeBase: Parses as types.KnowledgeBase
    ↓
Service.CreateKnowledgeBase: Receives KnowledgeBase with wiki_config
    ↓
Service.EnsureDefaults: Validates and sets defaults for wiki_config
    ↓
Repository.CreateKnowledgeBase: GORM.Create() serializes wiki_config to JSONB
    ↓
Database: wiki_config column populated ✅
```

### Updating a KB with wiki_config ❌
```
Frontend: PUT /api/v1/knowledge-bases/{id} { name, config: { wiki_config: {...} } }
    ↓
Handler.UpdateKnowledgeBase: Parses as UpdateKnowledgeBaseRequest
    ↓
❌ UpdateKnowledgeBaseRequest.Config is of type KnowledgeBaseConfig (NOT including WikiConfig)
    ↓
❌ JSON unmarshaling fails or ignores wiki_config field
    ↓
Service.UpdateKnowledgeBase: Receives config WITHOUT wiki_config
    ↓
❌ Code never updates kb.WikiConfig - it stays at existing value
    ↓
Repository.UpdateKnowledgeBase: GORM.Save() persists unchanged wiki_config
    ↓
Database: wiki_config column NOT updated ❌
```

---

## Required Fixes

### Fix 1: Add WikiConfig to KnowledgeBaseConfig Struct
**File:** `internal/types/knowledgebase.go`

```go
type KnowledgeBaseConfig struct {
    // Chunking configuration
    ChunkingConfig ChunkingConfig `yaml:"chunking_config" json:"chunking_config"`
    
    // Image processing configuration
    ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
    
    // FAQ configuration (only for FAQ type knowledge bases)
    FAQConfig *FAQConfig `yaml:"faq_config" json:"faq_config"`
    
    // ADD THIS:
    // Wiki configuration (for document type knowledge bases with wiki feature enabled)
    WikiConfig *WikiConfig `yaml:"wiki_config" json:"wiki_config"`
}
```

### Fix 2: Update UpdateKnowledgeBase Service
**File:** `internal/application/service/knowledgebase.go`

```go
func (s *knowledgeBaseService) UpdateKnowledgeBase(ctx context.Context,
    id string,
    name string,
    description string,
    config *types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
    // ... existing code ...
    
    if config != nil {
        kb.ChunkingConfig = config.ChunkingConfig
        kb.ImageProcessingConfig = config.ImageProcessingConfig
        if config.FAQConfig != nil {
            kb.FAQConfig = config.FAQConfig
        }
        
        // ADD THIS:
        if config.WikiConfig != nil {
            kb.WikiConfig = config.WikiConfig
        }
    }
    
    // ... rest of code ...
}
```

### Fix 3: Update Frontend Type Hints
**File:** `frontend/src/api/knowledge-base/index.ts`

```typescript
export function createKnowledgeBase(data: { 
  name: string; 
  description?: string; 
  type?: 'document' | 'faq';
  chunking_config?: any;
  embedding_model_id?: string;
  summary_model_id?: string;
  vlm_config?: { enabled: boolean; model_id?: string };
  storage_provider_config?: { provider: string };
  asr_config?: { enabled: boolean; model_id?: string; language?: string };
  extract_config?: any;
  faq_config?: { index_mode: string; question_index_mode?: string };
  // ADD THIS:
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

---

## Summary Table

| Component | Location | Status | Can Handle wiki_config |
|-----------|----------|--------|----------------------|
| Database Column | `migrations/000032_wiki_pages.up.sql` | ✅ Exists | ✅ Yes |
| WikiConfig Type | `internal/types/wiki_page.go` | ✅ Defined | ✅ Yes |
| KnowledgeBase Struct | `internal/types/knowledgebase.go` | ✅ Has field | ✅ Yes |
| KnowledgeBaseConfig Struct | `internal/types/knowledgebase.go` | ❌ Missing field | ❌ No |
| CreateKnowledgeBase Handler | `internal/handler/knowledgebase.go` | ✅ Full struct | ✅ Yes |
| UpdateKnowledgeBase Handler | `internal/handler/knowledgebase.go` | ⚠️ Limited struct | ❌ No |
| CreateKnowledgeBase Service | `internal/application/service/knowledgebase.go` | ✅ Receives KB | ✅ Yes |
| UpdateKnowledgeBase Service | `internal/application/service/knowledgebase.go` | ❌ Not updated | ❌ No |
| Repository Layer | `internal/application/repository/knowledgebase.go` | ✅ Works | ✅ Yes |
| Frontend API | `frontend/src/api/knowledge-base/index.ts` | ⚠️ Missing types | ⚠️ Partial |

---

## Conclusion

The infrastructure to handle `wiki_config` is almost complete, but there's a **critical gap in the update flow**:

1. **Creation works** ✅ - Wiki config can be set when creating a KB
2. **Update broken** ❌ - Wiki config cannot be updated after creation
3. **Root cause** - `KnowledgeBaseConfig` struct is missing the `WikiConfig` field
4. **Impact** - Frontend sends wiki config updates but backend ignores them

**3 simple code changes are needed** to fix this issue (see Required Fixes section above).
