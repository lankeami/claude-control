# Smart File Viewer — Design Spec

## Overview

Add filetype-aware rendering to the file viewer's "Full" view in the web UI. Instead of displaying all files as plain text in a `<pre>` tag, route each file through a renderer matched to its type: syntax-highlighted code, rendered markdown, collapsible JSON trees, CSV tables, inline image previews, and sandboxed HTML previews.

## Goals

- Each filetype gets its best viewing experience
- Lightweight: highlight.js (~40KB) from CDN + existing marked.js — no heavy editors
- Frontend-only routing — no API changes except binary file support for images
- Diff view is unchanged

## Non-Goals

- File editing
- Code folding, minimap, or other IDE features
- Content-based filetype detection (shebang sniffing, magic bytes)

## Architecture

### Renderer Router

A `getRenderer(filePath)` function maps file extensions and known filenames to renderer functions. It extracts the extension from the file path, looks it up in a mapping, and returns the appropriate renderer. Falls back to `renderPlainText` for unrecognized extensions.

```
filePath → extract extension → lookup in map → renderer function
                                    ↓ (no match)
                             check filename map
                                    ↓ (no match)
                              renderPlainText
```

### Extension → Renderer Mapping

| Category | Extensions / Filenames | Renderer |
|----------|----------------------|----------|
| Markdown | `.md`, `.mdx` | `renderMarkdown` |
| JSON | `.json` | `renderJSON` |
| CSV/TSV | `.csv`, `.tsv` | `renderCSV` |
| Images | `.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`, `.ico`, `.bmp` | `renderImage` |
| HTML | `.html`, `.htm` | `renderHTMLPreview` |
| Code | `.py`, `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.rb`, `.java`, `.rs`, `.css`, `.scss`, `.sh`, `.bash`, `.zsh`, `.sql`, `.yaml`, `.yml`, `.toml`, `.xml`, `.c`, `.cpp`, `.h`, `.hpp`, `.swift`, `.kt`, `.lua`, `.r`, `.php`, `.pl`, `.ex`, `.erl`, `.hs`, `.scala`, `.clj`, `.dart`, `.vim`, `.dockerfile` | `renderCode` |
| Known filenames | `Dockerfile`, `Makefile`, `Gemfile`, `Rakefile`, `.gitignore`, `.env`, `.dockerignore`, `Vagrantfile`, `Procfile` | `renderCode` (with explicit language hint) |
| Default | everything else | `renderPlainText` |

### Filename → Language Hint Mapping

For extensionless files, map the full filename to a highlight.js language:

| Filename | Language |
|----------|----------|
| `Dockerfile` | `dockerfile` |
| `Makefile` | `makefile` |
| `Gemfile` | `ruby` |
| `Rakefile` | `ruby` |
| `Vagrantfile` | `ruby` |
| `Procfile` | `yaml` |
| `.gitignore` | `plaintext` |
| `.env` | `bash` |
| `.dockerignore` | `plaintext` |

## Security

All renderers except `renderHTMLPreview` inject HTML into the main DOM via Alpine.js `x-html`. File content is untrusted, so every renderer must HTML-escape all file-sourced strings (`&`, `<`, `>`, `"`, `'`) before inserting them into the generated HTML.

A shared `escapeHtml(str)` utility function handles this. Every renderer calls it on raw file content before embedding in HTML attributes or text nodes.

**Per-renderer sanitization:**
- `renderCode` — escape content before wrapping in `<code>` tags. highlight.js operates on the escaped text (it handles entities correctly).
- `renderJSON` — escape all keys and string values before inserting into tree HTML.
- `renderCSV` — escape all cell values before inserting into `<td>` elements.
- `renderMarkdown` — uses marked.js with `{ breaks: true }`. After `marked.parse()`, a post-processing step strips any HTML tags not in an allowlist: `<p>`, `<h1>`–`<h6>`, `<ul>`, `<ol>`, `<li>`, `<a>`, `<strong>`, `<em>`, `<code>`, `<pre>`, `<blockquote>`, `<table>`, `<thead>`, `<tbody>`, `<tr>`, `<th>`, `<td>`, `<img>`, `<br>`, `<hr>`, `<span>`, `<div>`. This strips `<script>`, `<iframe>`, event handler attributes, etc. while preserving normal markdown output. Note: the existing chat message rendering also uses unsanitized `marked.parse()` but that is out of scope for this feature.
- `renderHTMLPreview` — sandboxed iframe with `sandbox=""` (no permissions, no script execution). Source view uses `renderCode` which escapes content.
- `renderPlainText` — escape content before inserting into `<pre>`.
- `renderImage` — no file content is inserted as HTML text; base64 data goes into `src` attribute only.

**CDN fallback:** If `window.hljs` is undefined (CDN unreachable), `renderCode` falls back to `renderPlainText` behavior (escaped content in `<pre>` with no highlighting).

## Renderers

### `renderMarkdown(content)`

Passes content through marked.js (already loaded via CDN for chat messages). Returns HTML wrapped in `<div class="markdown-content">`. Reuses the same CSS styles already applied to chat message markdown. Code blocks within markdown get syntax highlighting via highlight.js integration with marked.js (custom renderer that calls `hljs.highlight()` on fenced code blocks).

Raw HTML in markdown source is sanitized per the Security section above — only safe markdown-generated tags are allowed through.

### `renderCode(content, language)`

Uses `hljs.highlight(content, {language: lang})` which takes raw (unescaped) content and returns `{value: '<escaped+highlighted HTML>', language: '...'}`. The highlighted HTML is wrapped in `<pre class="code-viewer"><code>`. Line numbers are rendered by splitting the highlighted output into lines and wrapping each in `<span class="line">` for CSS counter numbering. A language badge is displayed in the top-right corner showing the detected/specified language name.

When no explicit language is provided, uses `hljs.highlightAuto(content)` for auto-detection. The auto-detected language name is shown in the badge.

Languages not included in the highlight.js common bundle (e.g., Elixir, Erlang, Haskell, Scala, Clojure, Dart) will render unhighlighted with line numbers — this is acceptable degradation. The common bundle covers ~35 languages which handles the vast majority of files.

If `window.hljs` is undefined (CDN failure), falls back to escaped plain text in `<pre>`.

### `renderJSON(content)`

Parses the JSON string. If parsing fails, falls back to `renderCode(content, 'json')`.

On success, renders a collapsible tree:
- Objects rendered as `{ }` with child key-value pairs
- Arrays rendered as `[ ]` with indexed children
- Each object/array node has a `▶`/`▼` toggle to expand/collapse
- Primitive values are color-coded: strings (green), numbers (blue), booleans (orange), null (gray)
- Keys are displayed in a distinct color (default text color, bold)
- All keys and string values are HTML-escaped before insertion
- Files with fewer than 100 top-level entries (keys for objects, elements for arrays) start fully expanded; larger files start collapsed to depth 2

The tree is built by a recursive `buildJSONTree(value, key, depth)` function that returns an HTML string.

### `renderCSV(content)`

Best-effort CSV parser for common CSV files. Parses the content by:
1. Splitting into lines
2. Splitting each line on commas (or tabs for TSV), handling quoted fields containing the delimiter
3. First row becomes `<thead>` with `<th>` cells
4. Remaining rows become `<tbody>` with `<td>` cells
5. All cell values are HTML-escaped

Wrapped in `<div class="csv-table-wrapper">` with horizontal scroll for wide tables. Alternating row background colors for readability.

**Limitations:** This is a simple parser. Files with embedded newlines in quoted fields, escaped quotes (`""`), or other RFC 4180 edge cases may not render correctly and will fall back to syntax-highlighted code view. Falls back if column counts vary by more than 20% across rows.

TSV files use tab as the delimiter instead of comma.

### `renderImage(content, filePath)`

For binary images (non-SVG): displays `<img src="data:{mimeType};base64,{content}">`. The MIME type is inferred from the file extension (e.g., `.png` → `image/png`).

For SVG files: the raw SVG text content is inserted into the `src` attribute as a data URI (`data:image/svg+xml;charset=utf-8,{urlEncodedContent}`) — not injected as raw HTML into the DOM.

Image is centered in the viewer with `max-width: 100%; max-height: 80vh` to fit within the panel. Filename is shown below the image.

### `renderHTMLPreview(content)`

Renders in a sandboxed iframe: `<iframe srcdoc="..." sandbox="">`. Empty `sandbox` attribute blocks all permissions by default (no scripts, no same-origin access, no forms).

A toggle in the viewer header switches between:
- **Preview** — the rendered iframe, no scripts (default)
- **Source** — the raw HTML displayed via `renderCode(content, 'html')`

### `renderPlainText(content)`

HTML-escapes content and displays in a `<pre>` tag. This is the fallback for any unrecognized file type.

## Backend Changes

### `/api/files/content` endpoint

One change: when the requested file has an image extension (`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.ico`, `.bmp`), read the file as binary and return base64-encoded content:

```json
{
  "content": "<base64-encoded data>",
  "binary": true
}
```

**Go implementation:** Use `encoding/base64.StdEncoding.EncodeToString(buf)` to encode the raw bytes before assigning to the `Content` field. Do not use `string(buf)` on binary data — it produces invalid UTF-8 that `json.Encode` will corrupt.

For all other files, behavior is unchanged:

```json
{
  "content": "<raw text>",
  "binary": false
}
```

SVG files remain as text (they are XML) — no base64 encoding needed.

The binary detection is a simple extension check, matching the same image extensions listed in the renderer mapping (minus `.svg`).

## Frontend Changes

### New CDN Dependencies (`index.html`)

- **highlight.js core + common languages**: `https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js` (~40KB)
- **highlight.js light theme**: `https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github.min.css`
- **highlight.js dark theme**: `https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css`

The existing app uses `@media (prefers-color-scheme: dark)` for theme switching. Match this: load the light theme with `media="(prefers-color-scheme: light)"` and the dark theme with `media="(prefers-color-scheme: dark)"` so the browser applies the correct one automatically.

### `app.js` Changes

**New Alpine.js data property:**
- `viewerFullHtml: ''` — rendered HTML for the full view (replaces raw text display)
- `viewerFileType: ''` — detected file type label for the header badge

**Modified `switchToFullView()`:**
After fetching content from `/api/files/content`, instead of setting `viewerContent` directly:
1. Call `getRenderer(filePath)` to get the renderer function
2. Call the renderer with the content (and binary flag for images)
3. Set `viewerFullHtml` to the returned HTML
4. Set `viewerFileType` to the detected type label

**Caching:** The existing `fileContentCache` continues to cache raw API responses. Add a parallel `renderedContentCache` keyed by `filePath + '::' + sessionId` that caches the rendered HTML string. This avoids re-running highlight.js on large files when toggling between diff/full views. Both caches are invalidated together when the file tree refreshes.

**New functions:**
- `getRenderer(filePath)` — extension/filename lookup, returns `{render, label}` object
- `renderMarkdown(content)` — marked.js rendering
- `renderCode(content, language)` — highlight.js rendering with line numbers
- `renderJSON(content)` — collapsible tree builder
- `renderCSV(content)` — table builder
- `renderImage(content, filePath)` — image display
- `renderHTMLPreview(content)` — iframe + source toggle
- `renderPlainText(content)` — current behavior

### `index.html` Changes

The full view section changes from:

```html
<pre class="full-file-content" x-text="viewerContent"></pre>
```

To:

```html
<div class="full-file-content" x-html="viewerFullHtml"></div>
```

A file type badge is added to the viewer header next to the filename:

```html
<span class="file-type-badge" x-text="viewerFileType" x-show="viewerFileType"></span>
```

### `style.css` Changes

New CSS classes:

- `.file-type-badge` — small pill badge next to filename in viewer header
- `.markdown-content` — styled container for rendered markdown (reuses chat markdown styles where possible)
- `.code-viewer` — `<pre>` with line numbers via CSS counters, relative positioning for language badge
- `.code-viewer .language-badge` — absolute positioned label in top-right
- `.code-viewer .line` — individual line span for CSS counter line numbers
- `.json-tree` — tree container styles
- `.json-tree .toggle` — expand/collapse button styles
- `.json-tree .key` — bold key text
- `.json-tree .string`, `.number`, `.boolean`, `.null` — value type colors
- `.csv-table-wrapper` — horizontal scroll container
- `.csv-table` — styled table with alternating rows, sticky header
- `.image-preview` — centered image container with constraints
- `.html-preview` — iframe container with source toggle button

## Files Modified

| File | Change |
|------|--------|
| `server/web/static/index.html` | Add highlight.js CDN links, change full view from `<pre x-text>` to `<div x-html>`, add file type badge |
| `server/web/static/app.js` | Add renderer router, all renderer functions, modify `switchToFullView()`, new data properties |
| `server/web/static/style.css` | Add styles for all renderer output types |
| `server/api/files.go` | Add binary/base64 response for image file extensions |

## Edge Cases

- **Large files (>1MB)**: Truncation warning still applies. Renderers work on truncated content — may produce incomplete markdown/JSON/CSV but that's acceptable with the warning shown.
- **Malformed JSON**: Falls back to syntax-highlighted code view.
- **Malformed CSV**: Falls back to syntax-highlighted code view if column counts are wildly inconsistent.
- **Binary files that aren't images**: Current "Binary file" message is preserved — the renderer router only activates for known extensions.
- **Empty files**: Each renderer handles empty content gracefully (show empty state or blank viewer).
- **SVG files**: Treated as images for rendering (inline display) but fetched as text (no base64).
