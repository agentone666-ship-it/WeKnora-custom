# Manual Knowledge Download Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to download `manual` type knowledge items as `.md` files via the existing download endpoint.

**Architecture:** Add an early-return branch in `GetKnowledgeFile` that, for manual knowledge, reads `Metadata.content` from the database record and streams it as a `.md` file — no new routes or handlers needed. The frontend adds a download button to the manual UI block and fixes the `link.download` attribute to include the `.md` extension.

**Tech Stack:** Go 1.24 (Gin, GORM), Vue 3 + TypeScript (TDesign UI, Pinia)

**Spec:** `docs/superpowers/specs/2026-03-16-manual-knowledge-download-design.md`

---

## Files Changed

| Action | Path | What changes |
|--------|------|--------------|
| Modify | `internal/application/service/knowledge.go` | Add manual branch in `GetKnowledgeFile` (lines 2408–2425) |
| Modify | `frontend/src/components/doc-content.vue` | Add download button to manual block (line 694–700); fix `link.download` in `downloadFile()` (line 630) |

No new files are created.

---

## Chunk 1: Backend — GetKnowledgeFile manual branch

### Task 1: Add manual type support to GetKnowledgeFile

**Files:**
- Modify: `internal/application/service/knowledge.go:2408-2425`

- [ ] **Step 1: Verify the current state of GetKnowledgeFile**

  Open `internal/application/service/knowledge.go` and confirm the method looks like this (lines 2408–2425):

  ```go
  // GetKnowledgeFile retrieves the physical file associated with a knowledge entry
  func (s *knowledgeService) GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
  	// Get knowledge record
  	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
  	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
  	if err != nil {
  		return nil, "", err
  	}

  	// Resolve KB-level file service with FilePath fallback protection
  	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
  	file, err := s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath).GetFile(ctx, knowledge.FilePath)
  	if err != nil {
  		return nil, "", err
  	}

  	return file, knowledge.FileName, nil
  }
  ```

- [ ] **Step 2: Add the manual branch**

  Replace the body of `GetKnowledgeFile` so it reads:

  ```go
  // GetKnowledgeFile retrieves the physical file associated with a knowledge entry
  func (s *knowledgeService) GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
  	// Get knowledge record
  	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
  	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
  	if err != nil {
  		return nil, "", err
  	}

  	// Manual knowledge stores content in Metadata — stream it directly as a .md file.
  	if knowledge.IsManual() {
  		meta, err := knowledge.ManualMetadata()
  		if err != nil {
  			return nil, "", err
  		}
  		// ManualMetadata returns (nil, nil) when Metadata column is empty; treat as empty content.
  		content := ""
  		if meta != nil {
  			content = meta.Content
  		}
  		// Sanitize title: strip characters that are invalid or dangerous in HTTP headers / filenames.
  		safeName := strings.NewReplacer(
  			"\n", "", "\r", "", "/", "-", "\\", "-", "\"", "'",
  		).Replace(knowledge.Title)
  		filename := safeName + ".md"
  		return io.NopCloser(strings.NewReader(content)), filename, nil
  	}

  	// Resolve KB-level file service with FilePath fallback protection
  	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
  	file, err := s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath).GetFile(ctx, knowledge.FilePath)
  	if err != nil {
  		return nil, "", err
  	}

  	return file, knowledge.FileName, nil
  }
  ```

  The `strings` package is already imported in this file. `io` is also already imported.

- [ ] **Step 3: Verify imports are present**

  Confirm that `"io"` and `"strings"` are already in the import block at the top of `internal/application/service/knowledge.go`. They are — do **not** add duplicate imports.

- [ ] **Step 4: Verify the build compiles**

  Run from the repo root:

  ```bash
  go build ./internal/application/service/...
  ```

  Expected: exits 0, no output.

- [ ] **Step 5: Run the Go test suite**

  ```bash
  go test -v ./...
  ```

  Expected: all existing tests pass (there are no tests for `GetKnowledgeFile` yet — that's fine, the suite should be green overall).

- [ ] **Step 6: Commit**

  ```bash
  git add internal/application/service/knowledge.go
  git commit -m "feat: support manual knowledge download in GetKnowledgeFile"
  ```

---

## Chunk 2: Frontend — download button + filename fix

### Task 2: Add download button to manual knowledge UI block

**Files:**
- Modify: `frontend/src/components/doc-content.vue:694-700`

- [ ] **Step 1: Locate the manual block in the template**

  In `frontend/src/components/doc-content.vue`, find lines 694–700:

  ```vue
        <!-- 手动创建类型专属区域 -->
        <div v-else-if="details.type === 'manual'" class="manual_box">
          <span class="label">{{ $t('knowledgeBase.documentTitle') }}</span>
          <div class="manual_title_box">
            <span class="manual_title">{{ details.title }}</span>
          </div>
        </div>
  ```

- [ ] **Step 2: Add the download button**

  Replace those lines with:

  ```vue
        <!-- 手动创建类型专属区域 -->
        <div v-else-if="details.type === 'manual'" class="manual_box">
          <span class="label">{{ $t('knowledgeBase.documentTitle') }}</span>
          <div class="manual_title_box">
            <span class="manual_title">{{ details.title }}</span>
          </div>
          <div class="icon_box" @click="downloadFile()">
            <img class="download_box" src="@/assets/img/download.svg" alt="">
          </div>
        </div>
  ```

  The `.icon_box` and `.download_box` CSS classes are already defined in this component (used by the `file` type block above).

- [ ] **Step 3: Fix the link.download attribute and a pre-existing removeChild bug in downloadFile()**

  In the same file, locate `downloadFile()` at lines 619–641.

  **Fix 1 — filename extension (line 630).** The current line is:

  ```ts
        link.setAttribute("download", props.details.title);
  ```

  Replace it with:

  ```ts
        link.setAttribute("download", props.details.type === 'manual' ? props.details.title + '.md' : props.details.title);
  ```

  **Fix 2 — pre-existing removeChild bug (line 633).** The link is created but never appended to `document.body`, so `document.body.removeChild(link)` throws a DOM exception at runtime. While fixing this is strictly out of scope, you will be touching this function anyway, so add the missing `appendChild` call right before `link.click()`:

  Before `link.click()`, add:
  ```ts
        document.body.appendChild(link);
  ```

  The full corrected block (lines 627–635) should read:

  ```ts
        const link = document.createElement("a");
        link.style.display = "none";
        link.setAttribute("href", url.value);
        link.setAttribute("download", props.details.type === 'manual' ? props.details.title + '.md' : props.details.title);
        document.body.appendChild(link);
        link.click();
        nextTick(() => {
          document.body.removeChild(link);
          URL.revokeObjectURL(url.value);
        })
  ```

- [ ] **Step 4: Run TypeScript type check**

  ```bash
  cd frontend && npm run type-check
  ```

  Expected: exits 0, no type errors.

- [ ] **Step 5: Commit**

  ```bash
  git add frontend/src/components/doc-content.vue
  git commit -m "feat: add download button for manual knowledge in doc-content"
  ```

---

## Manual Smoke Test (after both tasks)

Start the dev server and verify end-to-end:

1. Open a knowledge base that contains a `manual` type knowledge item.
2. Navigate to the knowledge detail page — confirm the download icon is visible (same icon as file type).
3. Click the download icon.
4. Verify the browser downloads a file named `{title}.md`.
5. Open the file and confirm the contents match the Markdown content in the editor.
6. Edge cases to spot-check:
   - Knowledge item with empty content → downloads an empty `.md` file (not an error).
   - Knowledge item whose title contains `/` or `"` → filename has `-` or `'` substituted respectively.
