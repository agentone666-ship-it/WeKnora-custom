# Knowledge Base Processing Flow - Detailed with Line Numbers

## Complete Document Ingestion Flow

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ PHASE 1: DOCUMENT UPLOAD & CONVERSION                                       │
└──────────────────────────────────────────────────────────────────────────────┘

File: internal/application/service/knowledge.go
Function: ProcessDocument(ctx, *asynq.Task) error
Lines: 7732-8108+

STEP 1: Parse Task Payload (7733-7737)
  ├─ Unmarshal task.Payload() → types.DocumentProcessPayload
  ├─ Extract: KnowledgeID, KnowledgeBaseID, FilePath, FileURL, FileType
  └─ Log context setup

STEP 2: Idempotency Checks (7761-7801)
  ├─ Line 7762: Get Knowledge by ID
  │  └─ knowledgeService.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
  ├─ Line 7773: Check if already deleted
  │  └─ if knowledge.ParseStatus == types.ParseStatusDeleting → return nil
  ├─ Line 7779: Check if already completed
  │  └─ if knowledge.ParseStatus == types.ParseStatusCompleted → return nil
  └─ Line 7814: Update status to "processing"
     └─ knowledge.ParseStatus = "processing"

STEP 3: Fetch Knowledge Base Config (7803-7812)
  ├─ Line 7804: Get KB by ID
  │  └─ knowledgeService.kbService.GetKnowledgeBaseByID(ctx, kbID)
  ├─ Validate: EmbeddingModelID must be configured
  └─ Store KB config in context

STEP 4: Validate Multimodal Config (7821-7841)
  ├─ Line 7822: Check if image file without multimodal enabled
  │  └─ IsImageType(payload.FileType) && !payload.EnableMultimodel
  ├─ Line 7833: Check if audio file without ASR enabled
  │  └─ IsAudioType(payload.FileType) && !kb.ASRConfig.IsASREnabled()
  └─ Line 7844: Reject video files (not supported)
     └─ IsVideoType(payload.FileType) → fail

STEP 5: Handle File Input (7858-7931)
  ├─ If FileURL provided (7858-7921):
  │  ├─ Line 7860: Validate URL for SSRF protection
  │  │  └─ secutils.ValidateURLForSSRF(payload.FileURL)
  │  ├─ Line 7871: Download file from URL
  │  │  └─ downloadFileFromURL(ctx, url, &fileName, &fileType)
  │  ├─ Line 7900: Save downloaded file
  │  │  └─ fileSvc.SaveBytes(ctx, contentBytes, tenantID, fileName, true)
  │  └─ Line 7915: Delegate to convert()
  │
  ├─ Else if URL string provided (7922-7931):
  │  └─ Call convert() directly
  │
  └─ Call convert(ctx, payload, kb, knowledge, isLastRetry)
     └─ Returns: types.ReadResult + error

STEP 6: Parse Document to Chunks (after convert())
  ├─ convertResult contains: text chunks, images, OCR, etc.
  ├─ Validate conversion success
  └─ Proceed to chunk processing


┌──────────────────────────────────────────────────────────────────────────────┐
│ PHASE 2: CHUNK CREATION & INDEXING                                          │
└──────────────────────────────────────────────────────────────────────────────┘

File: internal/application/service/knowledge.go
Function: processChunks(ctx, kb, knowledge, chunks, opts)
Lines: 1681-2074

STEP 1: Setup (1681-1714)
  ├─ Line 1691: Create span for tracing
  └─ Line 1708: Get embedding model
     └─ embeddingModel = modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)

STEP 2: Idempotency Cleanup (1716-1744)
  ├─ Line 1720: Delete old chunks
  │  └─ chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID)
  ├─ Line 1729: Delete old index data
  │  └─ retrieveEngine.DeleteByKnowledgeIDList(ctx, [knowledgeID], ...)
  └─ Line 1739: Delete old graph data
     └─ graphEngine.DelGraph(ctx, [namespace])

STEP 3: Setup Parent-Child Chunking (if enabled) (1810-1876)
  ├─ Check: options.ParentChunks (1811)
  ├─ For each parent chunk (1815-1830):
  │  └─ Create types.Chunk with ChunkType = types.ChunkTypeParentText
  │     └─ Parent chunks NOT set up for indexing
  │
  ├─ For each text chunk (1849-1877):
  │  ├─ Create types.Chunk with ChunkType = types.ChunkTypeText
  │  └─ Wire ParentChunkID if parent-child mode enabled
  │     └─ textChunk.ParentChunkID = parentDBChunks[...].ID
  │
  └─ Result: insertChunks array contains parent + child chunks

STEP 4: Create Index Information (1908-1928)
  ├─ Line 1913: Get title prefix for semantic alignment
  │  └─ titlePrefix = knowledge.Title + "\n"
  │
  ├─ For each TEXT chunk (1917-1928):
  │  ├─ indexContent = titlePrefix + chunk.Content
  │  └─ Create IndexInfo:
  │     ├─ Content: titlePrefix + chunk text
  │     ├─ SourceID: chunk.ID
  │     ├─ ChunkID: chunk.ID
  │     ├─ KnowledgeID: knowledge.ID
  │     └─ IsEnabled: true
  │
  └─ Result: indexInfoList (for indexing ONLY child/text chunks)

STEP 5: Storage Quota Check (1932-1955)
  ├─ Line 1934: Estimate storage size
  │  └─ totalStorageSize = retrieveEngine.EstimateStorageSize(...)
  │
  └─ Line 1947: Check tenant quota
     └─ if tenantInfo.StorageUsed + totalStorageSize > quota → fail

STEP 6: Save Chunks to Database (1965-1973)
  ├─ Line 1966: Insert all chunks (parent + text)
  │  └─ chunkService.CreateChunks(ctx, insertChunks)
  │
  └─ Result: Chunks stored but not yet indexed

STEP 7: **CRITICAL - BATCH INDEXING** (1986-2008)
  ├─ Line 1987: ★ MAIN INDEXING CALL ★
  │  └─ retrieveEngine.BatchIndex(ctx, embeddingModel, indexInfoList)
  │
  ├─ Key: indexInfoList contains ONLY text chunks (not parent chunks)
  │
  ├─ BatchIndex() internally:
  │  ├─ Gets tenant config: tenantInfo.GetEffectiveEngines()
  │  │  └─ Returns: []RetrieverEngineParams
  │  │     Each param: {RetrieverEngineType, RetrieverType}
  │  │
  │  ├─ For EACH configured retriever type:
  │  │  ├─ If "vector": creates embeddings, stores in vector DB
  │  │  │  └─ Uses embeddingModel.GetDimensions()
  │  │  │
  │  │  └─ If "keywords": indexes full-text/keywords
  │  │     └─ Uses Elasticsearch, Postgres FTS, etc.
  │  │
  │  └─ Result: Content indexed in BOTH vector + keyword if configured
  │
  └─ Line 1988-2007: Error handling & cleanup if failed

STEP 8: Enqueue Post-Processing (2044-2066)
  ├─ Check: EnableMultimodel + StoredImages (2045-2046)
  │
  ├─ If multimodal tasks pending (2045-2046):
  │  └─ enqueueImageMultimodalTasks(ctx, knowledge, kb, images, ...)
  │     └─ Chunks will be processed after multimodal tasks complete
  │        └─ Those tasks will enqueue TypeKnowledgePostProcess when done
  │
  ├─ Else (no multimodal):
  │  ├─ Line 2050: Create KnowledgePostProcessPayload
  │  ├─ Line 2057: Enqueue TypeKnowledgePostProcess task
  │  │  └─ task.Enqueue(types.TypeKnowledgePostProcess, payload)
  │  └─ This triggers Phase 3 immediately (no multimodal processing)
  │
  └─ Result: Next phase scheduled

STEP 9: Update Storage (2068-2073)
  ├─ Line 2069: Update tenant storage usage
  │  └─ tenantInfo.StorageUsed += totalStorageSize
  │
  └─ Line 2070: Persist storage adjustment
     └─ tenantRepo.AdjustStorageUsed(ctx, tenantID, size)


┌──────────────────────────────────────────────────────────────────────────────┐
│ PHASE 3: POST-PROCESSING ORCHESTRATION                                      │
└──────────────────────────────────────────────────────────────────────────────┘

File: internal/application/service/knowledge_post_process.go
Type: KnowledgePostProcessService
Function: Handle(ctx, *asynq.Task) error
Lines: 42-128

STEP 1: Deserialize Input (44-54)
  ├─ Line 44: Unmarshal types.KnowledgePostProcessPayload
  ├─ Extract: TenantID, KnowledgeBaseID, KnowledgeID, Language
  └─ Line 51-54: Setup context

STEP 2: Fetch Knowledge & KB (57-69)
  ├─ Line 57: Get knowledge
  │  └─ knowledgeRepo.GetKnowledgeByIDOnly(ctx, knowledgeID)
  │
  └─ Line 66: Get knowledge base
     └─ kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)

STEP 3: Fetch Chunks (71-83)
  ├─ Line 72: Get all chunks for knowledge
  │  └─ chunkService.ListChunksByKnowledgeID(ctx, knowledgeID)
  │
  ├─ Line 79-82: Filter text-like chunks
  │  ├─ types.ChunkTypeText
  │  ├─ types.ChunkTypeImageOCR
  │  └─ types.ChunkTypeImageCaption
  │
  └─ Result: textChunks array

STEP 4: Update ParseStatus (85-103)
  ├─ Line 87: Check current status (should be "processing")
  ├─ Line 88: knowledge.ParseStatus = "completed"
  │
  ├─ Line 92-96: Set SummaryStatus
  │  ├─ If textChunks exist:
  │  │  └─ knowledge.SummaryStatus = SummaryStatusPending
  │  │     → Summary generation will be enqueued
  │  │
  │  └─ Else:
  │     └─ knowledge.SummaryStatus = SummaryStatusNone
  │        → No summary needed (no content)
  │
  └─ Line 98: Update knowledge in DB

STEP 5: Enqueue Summary Generation (106-109)
  ├─ Check: len(textChunks) > 0 (106)
  │
  ├─ Line 107: enqueueSummaryGenerationTask(ctx, payload)
  │  ├─ Creates types.SummaryGenerationPayload
  │  ├─ Task type: types.TypeSummaryGeneration
  │  ├─ Queue: "low" priority
  │  └─ Max retries: 3
  │
  └─ Result: Summary task enqueued

STEP 6: Enqueue Question Generation (if enabled) (106-109)
  ├─ Check: kb.QuestionGenerationConfig != nil (160)
  ├─ Check: kb.QuestionGenerationConfig.Enabled (160)
  │
  ├─ Line 164-170: Validate & bound QuestionCount
  │  ├─ Get: questionCount = kb.QuestionGenerationConfig.QuestionCount
  │  ├─ Default: 3 (if <= 0)
  │  └─ Max: 10 (cap at 10)
  │
  ├─ Line 172-177: Create payload
  │  └─ types.QuestionGenerationPayload{...}
  │
  ├─ Line 185: Enqueue task
  │  ├─ Task type: types.TypeQuestionGeneration
  │  ├─ Queue: "low" priority
  │  └─ Max retries: 3
  │
  └─ Result: Question generation task enqueued (if enabled)

STEP 7: Enqueue Graph RAG Extract (if enabled) (112-120)
  ├─ Line 112: Check kb.ExtractConfig != nil && kb.ExtractConfig.Enabled
  │
  ├─ Line 113-119: For each text-like chunk
  │  └─ NewChunkExtractTask(ctx, taskEnqueuer, tenantID, chunkID, summaryModelID)
  │
  ├─ Uses: kb.SummaryModelID for LLM extraction
  │
  └─ Result: Graph extraction tasks enqueued (one per chunk)

STEP 8: **CRITICAL - WIKI INGEST TRIGGER** (122-126)
  ├─ Line 123: ★ WIKI INGEST CONDITION ★
  │  └─ if kb.IsWikiEnabled() && kb.WikiConfig.AutoIngest && len(textChunks) > 0
  │
  ├─ Breakdown:
  │  ├─ kb.IsWikiEnabled():
  │  │  └─ From knowledgebase.go:509
  │  │     └─ kb.WikiConfig != nil && kb.WikiConfig.Enabled
  │  │
  │  ├─ kb.WikiConfig.AutoIngest:
  │  │  └─ From wiki_page.go:98
  │  │     └─ Must be explicitly true
  │  │
  │  └─ len(textChunks) > 0:
  │     └─ Must have text content to ingest
  │
  ├─ If condition TRUE (Line 124):
  │  ├─ EnqueueWikiIngest(ctx, taskEnqueuer, redisClient, ...)
  │  ├─ Task type: types.TypeWikiIngest (inferred)
  │  └─ Line 125: Log success
  │
  └─ If condition FALSE:
     └─ Wiki ingest skipped silently (common case)

STEP 9: Return Success (128)
  └─ return nil

┌──────────────────────────────────────────────────────────────────────────────┐
│ PHASE 4: ASYNC POST-PROCESSING TASKS (Run in parallel)                      │
└──────────────────────────────────────────────────────────────────────────────┘

File: internal/application/service/knowledge.go

Task 1: Summary Generation
  ├─ Function: ProcessSummaryGeneration (line 2227)
  ├─ Input: types.SummaryGenerationPayload
  ├─ Process:
  │  ├─ Fetch chunks for knowledge
  │  ├─ Get summary model: kb.SummaryModelID
  │  ├─ Generate summary via LLM
  │  ├─ Create summary chunk (ChunkType = ChunkTypeSummary)
  │  └─ Index summary chunk: retrieveEngine.BatchIndex()
  │
  └─ Result: knowledge.Description updated with summary

Task 2: Question Generation (if enabled)
  ├─ Function: ProcessQuestionGeneration (line 2416)
  ├─ Input: types.QuestionGenerationPayload
  ├─ Process:
  │  ├─ For each text chunk
  │  ├─ Use LLM to generate N questions
  │  ├─ Create question chunks
  │  └─ Index question chunks
  │
  └─ Result: Question chunks indexed for Q&A search

Task 3: Graph RAG Extraction (if enabled)
  ├─ Function: (Via NewChunkExtractTask)
  ├─ Input: ChunkID, SummaryModelID
  ├─ Process:
  │  ├─ Extract entities and relationships from chunk
  │  ├─ Build knowledge graph
  │  └─ Store graph nodes/edges
  │
  └─ Result: Knowledge graph constructed from chunk content

Task 4: Wiki Ingest (if triggered)
  ├─ Function: (Via EnqueueWikiIngest)
  ├─ Input: KnowledgeBaseID, KnowledgeID, TenantID
  ├─ Process:
  │  ├─ Fetch all chunks for knowledge
  │  ├─ Use SynthesisModelID (kb.WikiConfig.SynthesisModelID)
  │  ├─ Generate wiki pages from content
  │  │  ├─ Summary pages (one per document)
  │  │  ├─ Entity pages (auto-extracted)
  │  │  ├─ Concept pages (auto-extracted)
  │  │  └─ Interlinks between pages
  │  │
  │  └─ Store wiki pages in wiki_pages table
  │
  └─ Result: Wiki pages generated and interlinked

```

## Configuration Decision Points

### Vector vs Keyword Indexing

**Decision Point**: Line 1987 in processChunks()
```
retrieveEngine.BatchIndex(ctx, embeddingModel, indexInfoList)
```

**How decided**:
1. BatchIndex() calls retrieveEngine (injected dependency)
2. retrieveEngine gets tenant config: `tenantInfo.GetEffectiveEngines()`
3. Returns: `[]RetrieverEngineParams`
4. Each param specifies:
   - `RetrieverEngineType`: "postgres", "elasticsearch", etc.
   - `RetrieverType`: "vector", "keywords", or both
5. Implementation creates indices for ALL configured types

**Example Configurations**:
- Vector only: `{RetrieverEngineType: "postgres", RetrieverType: "vector"}`
- Keywords only: `{RetrieverEngineType: "elasticsearch", RetrieverType: "keywords"}`
- Hybrid: Both params included in array

### Wiki Ingest Trigger

**Decision Point**: Line 123 in knowledge_post_process.go
```
if kb.IsWikiEnabled() && kb.WikiConfig.AutoIngest && len(textChunks) > 0
```

**Configuration needed**:
- KB type: Any (typically "document")
- WikiConfig.Enabled: true
- WikiConfig.AutoIngest: true
- WikiConfig.SynthesisModelID: valid model ID
- At least one text chunk in document

### Question Generation Trigger

**Decision Point**: Line 160 in knowledge_post_process.go
```
if kb.QuestionGenerationConfig != nil && kb.QuestionGenerationConfig.Enabled
```

**Configuration needed**:
- QuestionGenerationConfig.Enabled: true
- QuestionGenerationConfig.QuestionCount: 1-10 (default 3)

### Graph Extraction Trigger

**Decision Point**: Line 112 in knowledge_post_process.go
```
if kb.ExtractConfig != nil && kb.ExtractConfig.Enabled
```

**Configuration needed**:
- ExtractConfig.Enabled: true

```

