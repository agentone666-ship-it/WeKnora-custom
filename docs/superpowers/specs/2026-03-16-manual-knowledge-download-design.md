# Manual Knowledge Download — Design Spec

**Date:** 2026-03-16
**Status:** Approved

---

## Background

WeKnora supports multiple knowledge types. The `manual` type stores content as Markdown in the database `Metadata.content` field — there is no physical file on disk or in object storage. The existing download endpoint (`GET /api/v1/knowledge/:id/download`) only works for `file` type knowledge; calling it for `manual` type would silently fail with an internal error because `FilePath` is empty.

The frontend `doc-content.vue` component already omits the download button for `manual` type knowledge. This spec describes how to add proper download support.

---

## Goal

Allow users to download `manual` type knowledge as a `.md` file whose content is the Markdown stored in `Metadata.content` and whose filename is `{title}.md`.

---

## Architecture

### Backend — `internal/application/service/knowledge.go`

**Method:** `GetKnowledgeFile(ctx, id) (io.ReadCloser, string, error)`

Add an early-return branch before the existing file-service lookup:

```go
if knowledge.IsManual() {
    meta, err := knowledge.ManualMetadata()
    if err != nil {
        return nil, "", err
    }
    content := io.NopCloser(strings.NewReader(meta.Content))
    filename := knowledge.Title + ".md"
    return content, filename, nil
}
```

- No new routes, handlers, or services required.
- All existing permission checks, logging, and error middleware remain intact.
- The handler sets `Content-Disposition: attachment; filename="{title}.md"` via the existing streaming logic.

### Frontend — `frontend/src/components/doc-content.vue`

Add a download button to the existing `manual` type UI block:

```vue
<div v-else-if="details.type === 'manual'" class="manual_box">
  <span class="label">{{ $t('knowledgeBase.documentTitle') }}</span>
  <div class="manual_title_box">
    <span class="manual_title">{{ details.title }}</span>
  </div>
  <!-- NEW -->
  <div class="icon_box" @click="downloadFile()">
    <img class="download_box" src="@/assets/img/download.svg" alt="">
  </div>
</div>
```

`downloadFile()` is already implemented and calls `downKnowledgeDetails(id)` → `GET /api/v1/knowledge/:id/download`. No changes needed to the API layer.

---

## Data Flow

```
User clicks download
  → downloadFile() [frontend]
  → GET /api/v1/knowledge/:id/download
  → DownloadKnowledgeFile handler [validates access]
  → GetKnowledgeFile service
      → if manual: read Metadata.content → io.NopCloser(strings.NewReader(content))
      → return (ReadCloser, "{title}.md", nil)
  → handler streams response with Content-Disposition header
  → browser saves "{title}.md"
```

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| `ManualMetadata()` parse error | Returns internal server error (existing error middleware handles it) |
| Empty content (`""`) | Returns a valid empty `.md` file — not an error |
| Non-existent knowledge ID | Existing 404 handling unchanged |
| Insufficient permissions | Existing auth middleware unchanged |

---

## Out of Scope

- HTML / PDF / plain-text export formats
- Batch download of multiple knowledge items
- Version history download
