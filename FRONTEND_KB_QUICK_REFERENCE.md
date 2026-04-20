# Frontend KB & Wiki Settings - Quick Reference

## File Paths Summary

### 1️⃣ **Wiki Settings Component**
```
📁 frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue
   └─ Lines 123-187: Wiki settings panel (INLINE, no separate file!)
```

**Key Data Fields**:
- `wikiConfig.enabled` - Toggle wiki on/off
- `wikiConfig.autoIngest` - Auto-ingest mode (default: true)
- `wikiConfig.synthesisModelId` - Model ID (KnowledgeQA type)
- `wikiConfig.maxPagesPerIngest` - Max pages per batch (0-50)

---

### 2️⃣ **Main KB Settings Page**
```
📁 frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue (1353 lines)
```

**Sidebar Navigation**:
- Basic Info
- Model Config
- Parser Settings
- Multimodal Config
- ASR Config
- Storage Engine
- **Chunking** (分块设置)
- **Knowledge Graph** (知识图谱)
- **Wiki** (Wiki 设置)
- Advanced Settings
- Data Sources (edit only)
- Share Management (edit only)

---

### 3️⃣ **KB API Client**
```
📁 frontend/src/api/knowledge-base/index.ts
```

**Key Functions**:
- `createKnowledgeBase(data)` - POST `/api/v1/knowledge-bases`
- `updateKnowledgeBase(id, data)` - PUT `/api/v1/knowledge-bases/{id}`
- `getKnowledgeBaseById(id)` - GET `/api/v1/knowledge-bases/{id}`

**Wiki Config Type**:
```typescript
wiki_config?: {
  enabled: boolean;
  auto_ingest?: boolean;
  synthesis_model_id?: string;
  max_pages_per_ingest?: number;
}
```

---

### 4️⃣ **Chunking Settings**
```
📁 frontend/src/views/knowledge/settings/KBChunkingSettings.vue (297 lines)
```

**Config Options**:
- Chunk Size: 100-4000 (default: 512)
- Chunk Overlap: 0-500 (default: 100)
- Separators: Multi-select customizable
- Parent-Child Chunking: Toggle
- Parent Size: 512-8192 (default: 4096)
- Child Size: 64-2048 (default: 384)

---

### 5️⃣ **Knowledge Graph Settings**
```
📁 frontend/src/views/knowledge/settings/GraphSettings.vue (21276 bytes)
```

**Features**:
- Enable/disable entity extraction
- Tags management with auto-generate
- Sample text configuration
- Database status check

---

## Data Flow

### Create Mode
```
Form (empty) 
  ↓ [Fill sections via sidebar]
Validation
  ↓
buildSubmitData()
  ↓
createKnowledgeBase(data)
  ↓ [One API call]
New KB created
```

### Edit Mode
```
loadKBData() → GET /api/v1/knowledge-bases/{id}
  ↓
Map response to formData
  ↓ [User edits]
Validation
  ↓
PUT /api/v1/knowledge-bases/{id} [name, desc, FAQ, Wiki]
PUT /api/v1/initialization/config/{kbId} [models, chunking, storage, etc.]
  ↓ [Two API calls]
Changes saved
```

---

## Form Data Structure

```typescript
{
  type: 'document' | 'faq',
  name: string,
  description: string,
  
  // For FAQ KBs
  faqConfig: {
    indexMode: 'question_only' | 'question_answer',
    questionIndexMode: 'combined' | 'separate'
  },
  
  // Models
  modelConfig: {
    llmModelId: string,
    embeddingModelId: string
  },
  
  // Document chunking
  chunkingConfig: {
    chunkSize: number,
    chunkOverlap: number,
    separators: string[],
    enableParentChild: boolean,
    parentChunkSize: number,
    childChunkSize: number
  },
  
  storageProvider: string,
  multimodalConfig: { enabled: boolean, vllmModelId: string },
  asrConfig: { enabled: boolean, modelId: string, language: string },
  nodeExtractConfig: { /* ... graph config ... */ },
  questionGenerationConfig: { enabled: boolean, questionCount: number },
  
  // Wiki settings
  wikiConfig: {
    enabled: boolean,
    autoIngest: boolean,
    synthesisModelId: string,
    maxPagesPerIngest: number
  }
}
```

---

## Indexing Strategies

### FAQ Indexing
- `index_mode`: "question_only" (precision) vs "question_answer" (recall)
- `question_index_mode`: "combined" vs "separate"

### Document Indexing
- Chunk-based with configurable size, overlap, separators
- Optional parent-child hierarchy

### Wiki Indexing
- Controlled by `wikiConfig.auto_ingest` flag
- Synthesis model handles page summarization
- Batch limit via `maxPagesPerIngest`

---

## Key Components by Path

| Feature | File | Lines |
|---------|------|-------|
| **Main Editor** | `KnowledgeBaseEditorModal.vue` | 1353 |
| **Models** | `settings/KBModelConfig.vue` | ~80 |
| **Chunking** | `settings/KBChunkingSettings.vue` | 297 |
| **Graph** | `settings/GraphSettings.vue` | 21276 |
| **Parser** | `settings/KBParserSettings.vue` | 31516 |
| **Storage** | `settings/KBStorageSettings.vue` | 7100 |
| **Advanced** | `settings/KBAdvancedSettings.vue` | ~80 |
| **Share** | `settings/KBShareSettings.vue` | 18014 |
| **DataSource** | `settings/DataSourceSettings.vue` | 16899 |

---

## Important Implementation Notes

1. **Wiki settings INLINE**: No separate Vue file - all in KnowledgeBaseEditorModal.vue (lines 123-187)

2. **Wiki always sent**: For document KBs, wiki_config is always sent even when disabled

3. **Dual storage config**: Both `storage_provider_config` and `storage_config` written for backward compatibility

4. **Conditional visibility**:
   - Wiki section: Document KBs only
   - FAQ section: FAQ KBs only
   - DataSource/Share: Edit mode only

5. **Embedding model locked**: Cannot change embedding model if KB has files

6. **Storage change warning**: Confirms user when changing storage provider if files exist

---

## Translation Keys (Chinese)

```
knowledgeEditor.wiki.enableLabel = "启用 Wiki"
knowledgeEditor.wiki.autoIngestLabel = "自动 Ingest"
knowledgeEditor.wiki.synthesisModelLabel = "合成模型"
knowledgeEditor.wiki.maxPagesLabel = "单次最大页面数"
```

---

## API Endpoints

| Method | Endpoint | Purpose |
|--------|----------|---------|
| GET | `/api/v1/knowledge-bases` | List KBs |
| POST | `/api/v1/knowledge-bases` | Create KB |
| GET | `/api/v1/knowledge-bases/{id}` | Get KB details |
| PUT | `/api/v1/knowledge-bases/{id}` | Update name/desc/FAQ/Wiki |
| PUT | `/api/v1/initialization/config/{kbId}` | Update models/chunking/storage |
| DELETE | `/api/v1/knowledge-bases/{id}` | Delete KB |

---

Generated: 2026-04-20
