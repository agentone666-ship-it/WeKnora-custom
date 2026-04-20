# Frontend Knowledge Base & Wiki Settings - Comprehensive Analysis

**Date**: 2026-04-20  
**Status**: Complete Frontend Architecture Discovery

---

## Executive Summary

The frontend implementation uses **Vue 3 + TypeScript** with **TDesign UI components**. The knowledge base configuration system is centered around a **modal-based editor** with a **left sidebar navigation** that controls which settings section is displayed.

### Key Architecture
- **Main Editor Modal**: `KnowledgeBaseEditorModal.vue` (1353 lines) - Central hub for all KB settings
- **Navigation Model**: Sidebar with tabs for basic, models, parser, storage, chunking, graph, wiki, asr, multimodal, advanced, datasource (edit only), and share (edit only)
- **Settings Panel Components**: Modular components for each settings category
- **API Layer**: RESTful API calls to `/api/v1/knowledge-bases` and `/api/v1/initialization/config/{kbId}`

---

## 1. WIKI SETTINGS COMPONENT

### ⚠️ Important Discovery: No Separate File!

**Wiki settings are INLINE in KnowledgeBaseEditorModal.vue (lines 123-187)**

**File Path**: `frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue`

### Wiki Config Data Model

```typescript
wikiConfig: {
  enabled: false,                 // Boolean - Wiki feature enable/disable
  autoIngest: true,               // Boolean - Auto-ingest mode
  synthesisModelId: '',           // String - Model ID for wiki synthesis (KnowledgeQA type)
  maxPagesPerIngest: 0,           // Number - Max pages to process per ingest cycle (0-50)
}
```

### Settings Fields

1. **启用 Wiki** (Enable Wiki) - Toggle switch
   - Binding: `formData.wikiConfig.enabled`
   - Tip: "启用后可自动处理 Wiki 内容"

2. **自动 Ingest** (Auto Ingest) - Toggle switch (conditional on enabled)
   - Binding: `formData.wikiConfig.autoIngest`
   - Default: true
   - Tip: "启用自动获取和处理新页面"

3. **合成模型** (Synthesis Model) - Model selector (conditional on enabled)
   - Binding: `formData.wikiConfig.synthesisModelId`
   - Model Type: "KnowledgeQA"
   - Tip: "用于生成 Wiki 页面摘要的模型"

4. **单次最大页面数** (Max Pages Per Ingest) - Number input (conditional on enabled)
   - Binding: `formData.wikiConfig.maxPagesPerIngest`
   - Range: 0-50
   - Tip: "每次处理的最大页面数"

### Backend Mapping (From KB Response)

```typescript
wikiConfig: {
  enabled: kb.wiki_config?.enabled ?? false,
  autoIngest: kb.wiki_config?.auto_ingest ?? true,
  synthesisModelId: kb.wiki_config?.synthesis_model_id || '',
  maxPagesPerIngest: kb.wiki_config?.max_pages_per_ingest || 0,
}
```

---

## 2. KNOWLEDGE BASE SETTINGS MAIN PAGE

### File Path: `frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue`

This is the **primary and comprehensive** knowledge base settings interface with a modal + sidebar navigation pattern.

### Sidebar Navigation Structure

**For Document KBs** (13 sections):
1. **基本信息** (Basic Info) - name, description, type selection
2. **模型配置** (Model Config) - LLM and Embedding models
3. **解析引擎** (Parser Settings) - Parser engine rules
4. **多模态配置** (Multimodal) - Image/video processing with VLM
5. **音频处理** (ASR) - Audio to speech recognition
6. **存储引擎** (Storage Engine) - local/minio/cos selection
7. **分块设置** (Chunking) - Chunk size, overlap, separators
8. **知识图谱** (Knowledge Graph) - Entity/relation extraction
9. **Wiki 设置** (Wiki Settings) - Wiki-specific config
10. **高级设置** (Advanced) - Question generation
11. **数据源** (Data Sources) - Edit mode only
12. **共享管理** (Sharing) - Edit mode only
13. Plus datasource badge counter

**For FAQ KBs** (3 sections):
1. **基本信息** (Basic Info)
2. **模型配置** (Model Config)
3. **FAQ 配置** (FAQ Config) - Index modes

### Complete FormData Structure

```typescript
{
  type: 'document' | 'faq',
  name: string,
  description: string,
  
  faqConfig: {
    indexMode: 'question_only' | 'question_answer',
    questionIndexMode: 'combined' | 'separate'
  },
  
  modelConfig: {
    llmModelId: string,
    embeddingModelId: string
  },
  
  chunkingConfig: {
    chunkSize: number,
    chunkOverlap: number,
    separators: string[],
    parserEngineRules?: ParserEngineRule[],
    enableParentChild: boolean,
    parentChunkSize: number,
    childChunkSize: number
  },
  
  storageProvider: string,
  
  multimodalConfig: {
    enabled: boolean,
    vllmModelId: string
  },
  
  asrConfig: {
    enabled: boolean,
    modelId: string,
    language: string
  },
  
  nodeExtractConfig: {
    enabled: boolean,
    text: string,
    tags: string[],
    nodes: Array<{ name: string; attributes: string[] }>,
    relations: Array<{ node1: string; node2: string; type: string }>
  },
  
  questionGenerationConfig: {
    enabled: boolean,
    questionCount: number
  },
  
  wikiConfig: {
    enabled: boolean,
    autoIngest: boolean,
    synthesisModelId: string,
    maxPagesPerIngest: number
  }
}
```

---

## 3. KNOWLEDGE BASE API CLIENT

### File Path: `frontend/src/api/knowledge-base/index.ts`

### Create KB Function

```typescript
export function createKnowledgeBase(data: {
  name: string;
  description?: string;
  type?: 'document' | 'faq';
  chunking_config?: ChunkingConfig;
  embedding_model_id?: string;
  summary_model_id?: string;
  vlm_config?: VLMConfig;
  storage_provider_config?: { provider: string };
  storage_config?: any;
  asr_config?: ASRConfig;
  extract_config?: any;
  faq_config?: FAQConfig;
  wiki_config?: WikiConfig;
}) {
  return post(`/api/v1/knowledge-bases`, data);
}
```

### Update KB Function

```typescript
export function updateKnowledgeBase(id: string, data: {
  name: string;
  description?: string;
  config?: {
    chunking_config?: ChunkingConfig;
    image_processing_config?: any;
    faq_config?: FAQConfig;
    wiki_config?: WikiConfig;
  }
}) {
  return put(`/api/v1/knowledge-bases/${id}`, data);
}
```

### TypeScript Interfaces

```typescript
interface WikiConfig {
  enabled: boolean;
  auto_ingest?: boolean;
  synthesis_model_id?: string;
  wiki_language?: string;
  max_pages_per_ingest?: number;
}

interface FAQConfig {
  index_mode: string;
  question_index_mode?: string;
}

interface ChunkingConfig {
  chunk_size?: number;
  chunk_overlap?: number;
  separators?: string[];
  parser_engine_rules?: { file_types: string[]; engine: string }[];
  enable_parent_child?: boolean;
  parent_chunk_size?: number;
  child_chunk_size?: number;
}

interface VLMConfig {
  enabled: boolean;
  model_id?: string;
}

interface ASRConfig {
  enabled: boolean;
  model_id?: string;
  language?: string;
}
```

### KBModelConfigRequest (For Updates)

From `frontend/src/api/initialization/index.ts`:

```typescript
export interface KBModelConfigRequest {
  llmModelId: string
  embeddingModelId: string
  vlm_config?: VLMConfig
  asr_config?: ASRConfig
  documentSplitting: {
    chunkSize: number
    chunkOverlap: number
    separators: string[]
    parserEngineRules?: { file_types: string[]; engine: string }[]
    enableParentChild?: boolean
    parentChunkSize?: number
    childChunkSize?: number
  }
  multimodal: { enabled: boolean }
  storageProvider?: string
  nodeExtract: {
    enabled: boolean
    text: string
    tags: string[]
    nodes: Node[]
    relations: Relation[]
  }
  questionGeneration?: {
    enabled: boolean
    questionCount: number
  }
}
```

---

## 4. CHUNKING SETTINGS COMPONENT

### File Path: `frontend/src/views/knowledge/settings/KBChunkingSettings.vue` (297 lines)

### Configuration Options

1. **Chunk Size** 
   - Range: 100-4000 characters
   - Default: 512
   - Slider control with marks

2. **Chunk Overlap**
   - Range: 0-500 characters
   - Default: 100
   - Slider control

3. **Separators**
   - Multi-select with creatable option
   - Predefined: `\n\n`, `\n`, `。`, `！`, `？`, `；`, `;`, ` `
   - Default: `['\n\n', '\n', '。', '！', '？', ';', '；']`

4. **Parent-Child Chunking**
   - Boolean toggle
   - Enables hierarchical chunking

5. **Parent Chunk Size** (conditional)
   - Range: 512-8192 characters
   - Default: 4096

6. **Child Chunk Size** (conditional)
   - Range: 64-2048 characters
   - Default: 384

---

## 5. KNOWLEDGE GRAPH SETTINGS COMPONENT

### File Path: `frontend/src/views/knowledge/settings/GraphSettings.vue` (21276 bytes)

### Features

1. **Database Status Warning**
   - Shows if graph database not enabled
   - Link to enable instructions

2. **Enable Entity/Relation Extraction**
   - Boolean toggle

3. **Tags Configuration**
   - Multi-select with creatable option
   - Auto-generate button (requires LLM)
   - Model type: KnowledgeQA

4. **Sample Text**
   - Textarea input
   - Auto-generate button
   - 5000 character limit
   - 6-12 lines display

---

## 6. ADDITIONAL SETTINGS COMPONENTS

| File | Purpose |
|------|---------|
| `KBModelConfig.vue` | LLM and Embedding model selection |
| `KBStorageSettings.vue` | Storage provider (local/minio/cos) |
| `KBAdvancedSettings.vue` | Question generation settings |
| `KBParserSettings.vue` | Parser engine rule mapping |
| `KBShareSettings.vue` | User/org sharing & permissions |
| `DataSourceSettings.vue` | Data source management |

---

## 7. INDEXING STRATEGY & INDEX-TYPE CONFIGS

### FAQ Indexing Strategy

```typescript
faqConfig: {
  indexMode: 'question_only' | 'question_answer',
  questionIndexMode: 'combined' | 'separate'
}
```

**Index Modes**:
- `question_only` - Higher precision (questions only)
- `question_answer` - Higher recall (Q+A both)

**Question Modes**:
- `combined` - Single combined vector
- `separate` - Separate entries

### Document Indexing

**Chunk-based indexing** with configurable:
- Chunk size (100-4000 tokens)
- Chunk overlap (0-500 tokens)
- Custom separators
- Parent-child hierarchy option

### Wiki Indexing

```typescript
wikiConfig: {
  enabled: boolean
  auto_ingest: boolean           // Automatic indexing toggle
  synthesis_model_id: string     // Model for synthesis
  max_pages_per_ingest: number   // Batch limit
}
```

---

## 8. DATA FLOW

### Create Mode
1. Initialize empty form with defaults
2. Fill sections via sidebar navigation
3. Validate all fields
4. Submit in **one request** to `/api/v1/knowledge-bases`
5. Return with new KB ID

### Edit Mode
1. Load KB data from `/api/v1/knowledge-bases/{id}`
2. Map response to form data
3. User modifies sections
4. On submit:
   - **PUT** `/api/v1/knowledge-bases/{id}` (name, description, FAQ, Wiki)
   - **PUT** `/api/v1/initialization/config/{kbId}` (models, chunking, storage, etc.)

---

## 9. VALIDATION RULES

```typescript
- KB name: Required, non-empty
- Embedding model: Required, must select one
- LLM model: Required, must select one
- Multimodal: If enabled, VLLM model required
- FAQ: If type='faq', indexMode required
- Storage: Can only change if NO files exist
```

---

## 10. FILE INVENTORY

### Settings Views
| File | Path | Lines | Purpose |
|------|------|-------|---------|
| Main Editor | `views/knowledge/KnowledgeBaseEditorModal.vue` | 1353 | Core settings modal |
| KB View | `views/knowledge/KnowledgeBase.vue` | 27993 | KB main interface |
| Model Config | `settings/KBModelConfig.vue` | ~80 | Model selection |
| Chunking | `settings/KBChunkingSettings.vue` | 297 | Chunking config |
| Graph | `settings/GraphSettings.vue` | 21276 | Knowledge graph |
| Parser | `settings/KBParserSettings.vue` | 31516 | Parser rules |
| Storage | `settings/KBStorageSettings.vue` | 7100 | Storage engine |
| Advanced | `settings/KBAdvancedSettings.vue` | ~80 | Advanced options |
| Share | `settings/KBShareSettings.vue` | 18014 | Sharing config |
| DataSource | `settings/DataSourceSettings.vue` | 16899 | Data management |

### API Layer
| File | Purpose |
|------|---------|
| `api/knowledge-base/index.ts` | KB CRUD operations |
| `api/initialization/index.ts` | Config updates |

---

## Key Implementation Details

### Wiki Config Always Sent (For Non-FAQ)

```typescript
if (formData.value.type !== 'faq') {
  data.wiki_config = {
    enabled: formData.value.wikiConfig?.enabled ?? false,
    auto_ingest: formData.value.wikiConfig?.autoIngest ?? true,
    synthesis_model_id: formData.value.wikiConfig?.synthesisModelId || '',
    max_pages_per_ingest: formData.value.wikiConfig?.maxPagesPerIngest || 0,
  }
}
```

### Dual Storage Config (Backward Compat)

```typescript
data.storage_provider_config = { provider: ... }
data.storage_config = { provider: ... }  // Legacy
```

### Conditional Section Visibility

- **Wiki section**: Only for document KBs
- **FAQ section**: Only for FAQ KBs
- **ASR/Multimodal**: Only for document KBs
- **DataSource/Share**: Only in edit mode

---

## Translation Keys

**Chinese (zh-CN)**:
- `knowledgeEditor.wiki.enableLabel` = "启用 Wiki"
- `knowledgeEditor.wiki.autoIngestLabel` = "自动 Ingest"
- `knowledgeEditor.wiki.synthesisModelLabel` = "合成模型"
- `knowledgeEditor.wiki.maxPagesLabel` = "单次最大页面数"

