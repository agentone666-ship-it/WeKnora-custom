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
    // ManualMetadata returns (nil, nil) when Metadata column is empty;
    // treat as empty content rather than an error.
    content := ""
    if meta != nil {
        content = meta.Content
    }
    // Sanitize title for use as a filename: strip newlines and path separators.
    safeName := strings.NewReplacer(
        "\n", "", "\r", "", "/", "-", "\\", "-", "\"", "'",
    ).Replace(knowledge.Title)
    filename := safeName + ".md"
    return io.NopCloser(strings.NewReader(content)), filename, nil
}
```

- No new routes, handlers, or services required.
- All existing permission checks, logging, and error middleware remain intact.
- The handler sets `Content-Disposition: attachment; filename="{safeName}.md"` via the existing streaming logic.

#### Non-ASCII filenames in `Content-Disposition`

The existing handler writes the filename unquoted and without RFC 5987 encoding (`filename*=UTF-8''...`). This is a **known limitation inherited from the existing file-type download path** and is out of scope for this change. Most modern browsers (Chrome, Edge, Firefox) tolerate unquoted UTF-8 values in practice. A follow-up issue should address RFC 5987 encoding for all download types.

### Frontend — `frontend/src/components/doc-content.vue`

Two changes are required:

**1. Add download button to the `manual` UI block:**

```vue
<div v-else-if="details.type === 'manual'" class="manual_box">
  <span class="label">{{ $t('knowledgeBase.documentTitle') }}</span>
  <div class="manual_title_box">
    <span class="manual_title">{{ details.title }}</span>
  </div>
  <!-- NEW: download button, same styling as file type -->
  <div class="icon_box" @click="downloadFile()">
    <img class="download_box" src="@/assets/img/download.svg" alt="">
  </div>
</div>
```

**2. Fix `link.download` attribute in `downloadFile()` for manual type:**

The existing `downloadFile()` function sets `link.setAttribute("download", props.details.title)` without an extension. Browsers use the `download` attribute — not `Content-Disposition` — as the saved filename when `URL.createObjectURL` is used. For `manual` type, append `.md`:

```ts
const filename = details.value.type === 'manual'
  ? details.value.title + '.md'
  : details.value.title
link.setAttribute('download', filename)
```

`downloadFile()` otherwise calls `downKnowledgeDetails(id)` → `GET /api/v1/knowledge/:id/download` unchanged.

---

## Data Flow

```
User clicks download
  → downloadFile() [frontend, sets link.download = "{title}.md" for manual type]
  → GET /api/v1/knowledge/:id/download
  → DownloadKnowledgeFile handler [validates access]
  → GetKnowledgeFile service
      → if manual: sanitize title, read Metadata.content (empty string if nil)
      → return (ReadCloser, "{safeName}.md", nil)
  → handler streams response with Content-Disposition header
  → browser saves "{title}.md"
```

---

## Side Effects

`PreviewKnowledgeFile` also calls `GetKnowledgeFile` internally. After this change, preview requests for `manual` items will also succeed (returning the Markdown content with `text/markdown` MIME type via the existing `mimeTypeByExt(".md")` lookup). This is a desirable side effect and requires no additional changes.

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| `ManualMetadata()` parse error | Returns internal server error (existing error middleware handles it) |
| `ManualMetadata()` returns `nil` (empty Metadata column) | Treated as empty content; returns a valid empty `.md` file |
| Empty content (`""`) | Returns a valid empty `.md` file — not an error |
| Non-existent knowledge ID | Existing 404 handling unchanged |
| Insufficient permissions | Existing auth middleware unchanged |

---

## Out of Scope

- RFC 5987 encoding for non-ASCII `Content-Disposition` filenames (affects all download types, tracked separately)
- HTML / PDF / plain-text export formats
- Batch download of multiple knowledge items
- Version history download
