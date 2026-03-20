# Smart File Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add filetype-aware rendering to the file viewer's "Full" view — syntax highlighting, rendered markdown, collapsible JSON tree, CSV tables, inline image previews, and sandboxed HTML previews.

**Architecture:** Extension-based renderer router in the frontend (`app.js`). Each file extension maps to a renderer function that takes raw content and returns HTML. The only backend change is base64-encoding image files in the `/api/files/content` endpoint. highlight.js (CDN) handles syntax highlighting; existing marked.js handles markdown.

**Tech Stack:** highlight.js 11.9.0 (CDN), marked.js (already loaded), Alpine.js (existing), Go (backend image encoding)

**Spec:** `docs/superpowers/specs/2026-03-20-smart-file-viewer-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `server/web/static/index.html` | CDN script/style tags, full view HTML change (`<pre x-text>` → `<div x-html>`), file type badge |
| `server/web/static/app.js` | Renderer router, all renderer functions, `escapeHtml()`, `sanitizeHtml()`, modified `switchToFullView()`, new data properties |
| `server/web/static/style.css` | Styles for all renderer output types |
| `server/api/files.go` | Base64-encode image files in content endpoint |

---

### Task 1: Backend — Base64 Image Encoding

**Files:**
- Modify: `server/api/files.go:128-220`

- [ ] **Step 1: Add base64 import and image extension set**

In `server/api/files.go`, add `"encoding/base64"` and `"strings"` to the import block, and add a helper function:

```go
var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".ico": true, ".bmp": true,
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}
```

- [ ] **Step 2: Modify handleGetFileContent to base64-encode images**

In `handleGetFileContent`, after the binary detection block (line ~210), add image-specific handling. Replace the final `json.NewEncoder` block:

```go
// For image files, always return base64 regardless of null-byte detection
if isImageFile(filePath) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileContentResponse{
		Path:      filePath,
		Content:   base64.StdEncoding.EncodeToString(buf),
		Exists:    true,
		Truncated: truncated,
		Binary:    true,
	})
	return
}

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(fileContentResponse{
	Path:      filePath,
	Content:   string(buf),
	Exists:    true,
	Truncated: truncated,
	Binary:    binary,
})
```

- [ ] **Step 3: Verify it compiles**

Run: `cd server && go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 4: Run existing tests**

Run: `cd server && go test ./... -v`
Expected: All existing tests pass.

- [ ] **Step 5: Commit**

```bash
git add server/api/files.go
git commit -m "feat: base64-encode image files in content endpoint"
```

---

### Task 2: CDN Dependencies & HTML Structure

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add highlight.js CDN links**

After the `marked.min.js` script tag (line 8), add:

```html
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github.min.css" media="(prefers-color-scheme: light)">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css" media="(prefers-color-scheme: dark)">
<script src="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js"></script>
```

- [ ] **Step 2: Add file type badge to viewer header**

In the file viewer header (line 161), after the `viewerFileName` span, add the badge:

```html
<span class="file-viewer-path" x-text="viewerFileName"></span>
<span class="file-type-badge" x-text="viewerFileType" x-show="viewerFileType"></span>
```

- [ ] **Step 3: Change full view from `<pre x-text>` to `<div x-html>`**

Replace the full view content area (line 177):

Old:
```html
<pre x-show="!viewerLoading && !viewerBinary" class="full-file-content" x-text="viewerContent"></pre>
```

New:
```html
<div x-show="!viewerLoading && !viewerBinary" class="full-file-content" x-html="viewerFullHtml"></div>
```

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat: add highlight.js CDN and update full view to x-html"
```

---

### Task 3: Core Utilities & Renderer Router

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add new Alpine data properties**

In the data properties section (around line 52, after `fileContentCache: {}`), add:

```javascript
viewerFullHtml: '',
viewerFileType: '',
renderedContentCache: {},
```

- [ ] **Step 2: Add `escapeHtml` utility**

Add this function inside the Alpine data object, before the renderer functions:

```javascript
escapeHtml(str) {
  if (!str) return '';
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
},
```

- [ ] **Step 3: Add `sanitizeHtml` utility for markdown**

This strips disallowed tags from marked.js output while preserving safe ones:

```javascript
sanitizeHtml(html) {
  // Allowlist of safe tags that marked.js generates
  const allowed = new Set([
    'p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'ul', 'ol', 'li', 'a', 'strong', 'em', 'b', 'i',
    'code', 'pre', 'blockquote',
    'table', 'thead', 'tbody', 'tr', 'th', 'td',
    'img', 'br', 'hr', 'span', 'div', 'del', 'sup', 'sub'
  ]);
  // Strip tags not in allowlist, and remove event handler attributes
  return html
    .replace(/<\/?([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>/g, (match, tag) => {
      if (!allowed.has(tag.toLowerCase())) return '';
      // Remove event handlers (on*) and javascript: urls
      return match
        .replace(/\s+on\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]*)/gi, '')
        .replace(/\s+href\s*=\s*"javascript:[^"]*"/gi, '')
        .replace(/\s+href\s*=\s*'javascript:[^']*'/gi, '');
    });
},
```

- [ ] **Step 4: Add `getRenderer` router**

```javascript
getRenderer(filePath) {
  const name = filePath.split('/').pop();
  const ext = name.includes('.') ? name.split('.').pop().toLowerCase() : '';

  // Extension-based routing
  const extMap = {
    'md': { render: (c) => this.renderMarkdown(c), label: 'Markdown' },
    'mdx': { render: (c) => this.renderMarkdown(c), label: 'Markdown' },
    'json': { render: (c) => this.renderJSON(c), label: 'JSON' },
    'csv': { render: (c) => this.renderCSV(c, ','), label: 'CSV' },
    'tsv': { render: (c) => this.renderCSV(c, '\t'), label: 'TSV' },
    'html': { render: (c) => this.renderHTMLPreview(c), label: 'HTML' },
    'htm': { render: (c) => this.renderHTMLPreview(c), label: 'HTML' },
    'png': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'jpg': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'jpeg': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'gif': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'svg': { render: (c, f) => this.renderImage(c, f), label: 'SVG' },
    'webp': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'ico': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
    'bmp': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
  };

  // Code extensions
  const codeExts = [
    'py', 'go', 'js', 'ts', 'jsx', 'tsx', 'rb', 'java', 'rs', 'css', 'scss',
    'sh', 'bash', 'zsh', 'sql', 'yaml', 'yml', 'toml', 'xml', 'c', 'cpp',
    'h', 'hpp', 'swift', 'kt', 'lua', 'r', 'php', 'pl', 'ex', 'erl', 'hs',
    'scala', 'clj', 'dart', 'vim', 'dockerfile'
  ];
  // Map some extensions to highlight.js language names
  const langMap = {
    'py': 'python', 'js': 'javascript', 'ts': 'typescript', 'rb': 'ruby',
    'rs': 'rust', 'sh': 'bash', 'bash': 'bash', 'zsh': 'bash',
    'yml': 'yaml', 'kt': 'kotlin', 'ex': 'elixir', 'erl': 'erlang',
    'hs': 'haskell', 'clj': 'clojure', 'hpp': 'cpp', 'h': 'c',
    'jsx': 'javascript', 'tsx': 'typescript', 'scss': 'scss',
    'pl': 'perl', 'dockerfile': 'dockerfile'
  };

  if (extMap[ext]) return extMap[ext];

  if (codeExts.includes(ext)) {
    const lang = langMap[ext] || ext;
    return { render: (c) => this.renderCode(c, lang), label: lang.charAt(0).toUpperCase() + lang.slice(1) };
  }

  // Known filenames (no extension)
  const filenameMap = {
    'Dockerfile': { lang: 'dockerfile', label: 'Dockerfile' },
    'Makefile': { lang: 'makefile', label: 'Makefile' },
    'Gemfile': { lang: 'ruby', label: 'Ruby' },
    'Rakefile': { lang: 'ruby', label: 'Ruby' },
    'Vagrantfile': { lang: 'ruby', label: 'Ruby' },
    'Procfile': { lang: 'yaml', label: 'YAML' },
    '.gitignore': { lang: 'plaintext', label: 'Git Ignore' },
    '.env': { lang: 'bash', label: 'Env' },
    '.dockerignore': { lang: 'plaintext', label: 'Docker Ignore' },
  };
  if (filenameMap[name]) {
    const fm = filenameMap[name];
    return { render: (c) => this.renderCode(c, fm.lang), label: fm.label };
  }

  return { render: (c) => this.renderPlainText(c), label: '' };
},
```

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add escapeHtml, sanitizeHtml, and renderer router"
```

---

### Task 4: Code Renderer (highlight.js)

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `renderCode` function**

```javascript
renderCode(content, language) {
  if (!content) return '<pre class="code-viewer"><code></code></pre>';

  let highlighted;
  let detectedLang = language || '';

  if (typeof hljs !== 'undefined') {
    try {
      if (language && language !== 'plaintext') {
        const result = hljs.highlight(content, { language, ignoreIllegals: true });
        highlighted = result.value;
        detectedLang = result.language || language;
      } else if (language === 'plaintext') {
        highlighted = this.escapeHtml(content);
      } else {
        const result = hljs.highlightAuto(content);
        highlighted = result.value;
        detectedLang = result.language || '';
      }
    } catch (e) {
      // Language not supported — fall back to escaped plain text
      highlighted = this.escapeHtml(content);
    }
  } else {
    // highlight.js not loaded (CDN failure)
    highlighted = this.escapeHtml(content);
  }

  // Split into lines and wrap each for CSS counter line numbers
  const lines = highlighted.split('\n');
  const lineHtml = lines.map(line => '<span class="line">' + (line || ' ') + '</span>').join('\n');

  const badge = detectedLang ? '<span class="language-badge">' + this.escapeHtml(detectedLang) + '</span>' : '';

  return '<div class="code-viewer-wrapper">' + badge +
    '<pre class="code-viewer"><code>' + lineHtml + '</code></pre></div>';
},
```

- [ ] **Step 2: Add `renderPlainText` function**

```javascript
renderPlainText(content) {
  if (!content) return '<pre class="full-file-content-pre"></pre>';
  return '<pre class="full-file-content-pre">' + this.escapeHtml(content) + '</pre>';
},
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add renderCode and renderPlainText renderers"
```

---

### Task 5: Markdown Renderer

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `renderMarkdown` function**

```javascript
renderMarkdown(content) {
  if (!content) return '<div class="markdown-content"></div>';

  if (typeof marked === 'undefined') return this.renderPlainText(content);

  // Configure marked to use highlight.js for code blocks
  const renderer = new marked.Renderer();
  const self = this;
  renderer.code = function(code, language) {
    // marked v5+ passes an object {text, lang, escaped}
    const text = typeof code === 'object' ? code.text : code;
    const lang = typeof code === 'object' ? code.lang : language;
    if (typeof hljs !== 'undefined' && lang) {
      try {
        return '<pre><code class="hljs">' + hljs.highlight(text, { language: lang, ignoreIllegals: true }).value + '</code></pre>';
      } catch (e) {}
    }
    return '<pre><code>' + self.escapeHtml(text) + '</code></pre>';
  };

  const html = marked.parse(content, { renderer, breaks: true });
  return '<div class="markdown-content">' + this.sanitizeHtml(html) + '</div>';
},
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add renderMarkdown renderer with highlight.js code blocks"
```

---

### Task 6: JSON Tree Renderer

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `renderJSON` function**

```javascript
renderJSON(content) {
  if (!content) return '<div class="json-tree"></div>';

  let parsed;
  try {
    parsed = JSON.parse(content);
  } catch (e) {
    return this.renderCode(content, 'json');
  }

  const topLevelCount = Array.isArray(parsed) ? parsed.length :
    (typeof parsed === 'object' && parsed !== null) ? Object.keys(parsed).length : 0;
  const defaultExpanded = topLevelCount < 100;

  return '<div class="json-tree">' + this.buildJSONTree(parsed, null, 0, defaultExpanded) + '</div>';
},

buildJSONTree(value, key, depth, expanded) {
  const esc = (s) => this.escapeHtml(String(s));
  const keyPrefix = key !== null ? '<span class="json-key">' + esc(key) + '</span>: ' : '';
  const indent = depth * 16;

  if (value === null) {
    return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-null">null</span></div>';
  }
  if (typeof value === 'boolean') {
    return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-boolean">' + value + '</span></div>';
  }
  if (typeof value === 'number') {
    return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-number">' + value + '</span></div>';
  }
  if (typeof value === 'string') {
    return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-string">"' + esc(value) + '"</span></div>';
  }

  const isArray = Array.isArray(value);
  const entries = isArray ? value.map((v, i) => [i, v]) : Object.entries(value);
  const open = isArray ? '[' : '{';
  const close = isArray ? ']' : '}';
  const shouldExpand = expanded && depth < 5;
  const id = 'jt-' + Math.random().toString(36).substr(2, 8);

  let html = '<div class="json-line" style="padding-left:' + indent + 'px">';
  html += '<span class="json-toggle" onclick="var el=document.getElementById(\'' + id + '\');var t=this;if(el.style.display===\'none\'){el.style.display=\'block\';t.textContent=\'▼\'}else{el.style.display=\'none\';t.textContent=\'▶\'}">' + (shouldExpand ? '▼' : '▶') + '</span> ';
  html += keyPrefix + '<span class="json-bracket">' + open + '</span>';
  html += ' <span class="json-count">' + entries.length + (isArray ? ' items' : ' keys') + '</span>';
  html += '</div>';

  html += '<div id="' + id + '" style="display:' + (shouldExpand ? 'block' : 'none') + '">';
  for (const [k, v] of entries) {
    html += this.buildJSONTree(v, isArray ? null : k, depth + 1, shouldExpand);
  }
  html += '<div class="json-line" style="padding-left:' + indent + 'px"><span class="json-bracket">' + close + '</span></div>';
  html += '</div>';

  return html;
},
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add collapsible JSON tree renderer"
```

---

### Task 7: CSV Table Renderer

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `renderCSV` function**

```javascript
renderCSV(content, delimiter) {
  if (!content) return '<div class="csv-table-wrapper"></div>';

  const lines = content.split('\n').filter(l => l.trim());
  if (lines.length === 0) return this.renderPlainText(content);

  // Simple CSV parser with quoted field support
  const parseLine = (line, delim) => {
    const fields = [];
    let current = '';
    let inQuotes = false;
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      if (inQuotes) {
        if (ch === '"' && line[i + 1] === '"') {
          current += '"';
          i++;
        } else if (ch === '"') {
          inQuotes = false;
        } else {
          current += ch;
        }
      } else {
        if (ch === '"') {
          inQuotes = true;
        } else if (ch === delim) {
          fields.push(current);
          current = '';
        } else {
          current += ch;
        }
      }
    }
    fields.push(current);
    return fields;
  };

  const rows = lines.map(l => parseLine(l, delimiter));

  // Validate: check column count consistency
  if (rows.length > 1) {
    const headerLen = rows[0].length;
    const badRows = rows.filter(r => Math.abs(r.length - headerLen) / headerLen > 0.2).length;
    if (badRows > rows.length * 0.2) {
      return this.renderCode(content, delimiter === '\t' ? 'tsv' : 'csv');
    }
  }

  const esc = (s) => this.escapeHtml(s);
  let html = '<div class="csv-table-wrapper"><table class="csv-table">';

  // Header
  html += '<thead><tr>';
  for (const cell of rows[0]) {
    html += '<th>' + esc(cell) + '</th>';
  }
  html += '</tr></thead>';

  // Body
  html += '<tbody>';
  for (let i = 1; i < rows.length; i++) {
    html += '<tr>';
    for (const cell of rows[i]) {
      html += '<td>' + esc(cell) + '</td>';
    }
    html += '</tr>';
  }
  html += '</tbody></table></div>';

  return html;
},
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add CSV/TSV table renderer"
```

---

### Task 8: Image & HTML Preview Renderers

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `renderImage` function**

```javascript
renderImage(content, filePath) {
  if (!content) return '<div class="image-preview">No image data</div>';

  const ext = filePath.split('.').pop().toLowerCase();
  const mimeMap = {
    'png': 'image/png', 'jpg': 'image/jpeg', 'jpeg': 'image/jpeg',
    'gif': 'image/gif', 'svg': 'image/svg+xml', 'webp': 'image/webp',
    'ico': 'image/x-icon', 'bmp': 'image/bmp'
  };
  const mime = mimeMap[ext] || 'image/png';
  const fileName = filePath.split('/').pop();

  let src;
  if (ext === 'svg') {
    // SVG is text — use URL-encoded data URI
    src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(content);
  } else {
    // Binary images — content is already base64 from the backend
    src = 'data:' + mime + ';base64,' + content;
  }

  return '<div class="image-preview">' +
    '<img src="' + src + '" alt="' + this.escapeHtml(fileName) + '">' +
    '<div class="image-filename">' + this.escapeHtml(fileName) + '</div>' +
    '</div>';
},
```

- [ ] **Step 2: Add `renderHTMLPreview` function**

```javascript
renderHTMLPreview(content) {
  if (!content) return '<div class="html-preview"></div>';

  // Escape content for srcdoc attribute
  const srcdocContent = content.replace(/&/g, '&amp;').replace(/"/g, '&quot;');

  return '<div class="html-preview">' +
    '<div class="html-preview-tabs">' +
      '<button class="html-tab active" onclick="this.classList.add(\'active\');this.nextElementSibling.classList.remove(\'active\');this.parentElement.nextElementSibling.style.display=\'block\';this.parentElement.nextElementSibling.nextElementSibling.style.display=\'none\'">Preview</button>' +
      '<button class="html-tab" onclick="this.classList.add(\'active\');this.previousElementSibling.classList.remove(\'active\');this.parentElement.nextElementSibling.style.display=\'none\';this.parentElement.nextElementSibling.nextElementSibling.style.display=\'block\'">Source</button>' +
    '</div>' +
    '<iframe class="html-preview-frame" srcdoc="' + srcdocContent + '" sandbox=""></iframe>' +
    '<div class="html-source" style="display:none">' + this.renderCode(content, 'html') + '</div>' +
    '</div>';
},
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add image and HTML preview renderers"
```

---

### Task 9: Wire Up switchToFullView & Caching

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Modify `switchToFullView()`**

Replace the existing `switchToFullView()` method (lines 839-868) with:

```javascript
async switchToFullView() {
  this.viewerMode = 'full';
  if (!this.viewerFile) return;

  const cacheKey = this.viewerFile + '::' + this.selectedSessionId;

  // Check rendered content cache first
  if (this.renderedContentCache[cacheKey]) {
    const cached = this.renderedContentCache[cacheKey];
    this.viewerFullHtml = cached.html;
    this.viewerFileType = cached.fileType;
    this.viewerBinary = cached.binary;
    this.viewerTruncated = cached.truncated;
    return;
  }

  // Check raw content cache
  const rawCacheKey = this.viewerFile + ':' + this.selectedSessionId;
  let data;
  if (this.fileContentCache[rawCacheKey]) {
    data = this.fileContentCache[rawCacheKey];
  } else {
    this.viewerLoading = true;
    try {
      const params = new URLSearchParams({ path: this.viewerFile, session_id: this.selectedSessionId });
      const resp = await fetch('/api/files/content?' + params, {
        headers: { 'Authorization': 'Bearer ' + this.apiKey }
      });
      if (!resp.ok) { this.viewerFullHtml = '<pre>' + this.escapeHtml('Error loading file.') + '</pre>'; return; }
      data = await resp.json();
      if (!data.exists) { this.viewerFullHtml = '<pre>' + this.escapeHtml('File no longer exists on disk.') + '</pre>'; return; }
      this.fileContentCache[rawCacheKey] = data;
    } catch (e) {
      this.viewerFullHtml = '<pre>' + this.escapeHtml('Error loading file.') + '</pre>';
      return;
    } finally {
      this.viewerLoading = false;
    }
  }

  this.viewerBinary = data.binary || false;
  this.viewerTruncated = data.truncated || false;

  // For non-image binary files, show the binary message
  const ext = this.viewerFile.split('.').pop().toLowerCase();
  const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico', 'bmp'];
  if (data.binary && !imageExts.includes(ext)) {
    this.viewerFullHtml = '';
    this.viewerFileType = '';
    return;
  }

  // Route to the appropriate renderer
  const renderer = this.getRenderer(this.viewerFile);
  this.viewerFileType = renderer.label;
  this.viewerFullHtml = renderer.render(data.content, this.viewerFile);

  // Cache the rendered result
  this.renderedContentCache[cacheKey] = {
    html: this.viewerFullHtml,
    fileType: this.viewerFileType,
    binary: this.viewerBinary,
    truncated: this.viewerTruncated,
  };
},
```

- [ ] **Step 2: Update `closeFileViewer` to clear new properties**

In `closeFileViewer()`, add clearing the new properties:

```javascript
closeFileViewer() {
  this.viewerFile = null;
  this.viewerDiffs = [];
  this.viewerDiffHtml = '';
  this.viewerContent = '';
  this.viewerFullHtml = '';
  this.viewerFileType = '';
},
```

- [ ] **Step 3: Update `selectSession` to clear rendered cache**

In `selectSession()`, where `this.fileContentCache = {};` is set (line 343), add:

```javascript
this.renderedContentCache = {};
```

- [ ] **Step 4: Update `loadSessionFiles` to invalidate rendered cache**

At the start of `loadSessionFiles()`, add:

```javascript
this.renderedContentCache = {};
```

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: wire up renderer router in switchToFullView with caching"
```

---

### Task 10: CSS Styles for All Renderers

**Files:**
- Modify: `server/web/static/style.css`

- [ ] **Step 1: Add file type badge styles**

Append to `style.css`:

```css
/* File type badge */
.file-type-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--accent);
  color: white;
  margin-left: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
}
```

- [ ] **Step 2: Add code viewer styles**

```css
/* Code viewer with line numbers */
.code-viewer-wrapper {
  position: relative;
}

.code-viewer-wrapper .language-badge {
  position: absolute;
  top: 8px;
  right: 8px;
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--input-bg);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
  z-index: 1;
}

.code-viewer {
  margin: 0;
  padding: 12px 12px 12px 52px;
  font-family: 'SF Mono', Consolas, 'Menlo', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre;
  overflow-x: auto;
  counter-reset: line;
  background: transparent;
}

.code-viewer .line {
  display: block;
  counter-increment: line;
}

.code-viewer .line::before {
  content: counter(line);
  display: inline-block;
  width: 32px;
  margin-left: -40px;
  margin-right: 8px;
  text-align: right;
  color: var(--text-muted);
  opacity: 0.5;
  font-size: 11px;
  user-select: none;
}
```

- [ ] **Step 3: Add markdown content styles**

```css
/* Markdown rendered content */
.full-file-content .markdown-content {
  padding: 16px 20px;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  font-size: 14px;
  line-height: 1.6;
  color: var(--text);
}

.full-file-content .markdown-content h1,
.full-file-content .markdown-content h2,
.full-file-content .markdown-content h3,
.full-file-content .markdown-content h4,
.full-file-content .markdown-content h5,
.full-file-content .markdown-content h6 {
  margin-top: 1.2em;
  margin-bottom: 0.5em;
  font-weight: 600;
}

.full-file-content .markdown-content h1 { font-size: 1.5em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
.full-file-content .markdown-content h2 { font-size: 1.3em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
.full-file-content .markdown-content h3 { font-size: 1.15em; }

.full-file-content .markdown-content p { margin: 0.8em 0; }
.full-file-content .markdown-content ul,
.full-file-content .markdown-content ol { padding-left: 2em; margin: 0.5em 0; }

.full-file-content .markdown-content code {
  background: var(--input-bg);
  padding: 0.15em 0.4em;
  border-radius: 3px;
  font-size: 0.9em;
  font-family: 'SF Mono', Consolas, 'Menlo', monospace;
}

.full-file-content .markdown-content pre {
  background: var(--input-bg);
  padding: 12px;
  border-radius: 6px;
  overflow-x: auto;
}

.full-file-content .markdown-content pre code {
  background: transparent;
  padding: 0;
}

.full-file-content .markdown-content blockquote {
  border-left: 3px solid var(--accent);
  margin: 0.8em 0;
  padding: 0.5em 1em;
  color: var(--text-muted);
}

.full-file-content .markdown-content table {
  border-collapse: collapse;
  margin: 0.8em 0;
  width: 100%;
}

.full-file-content .markdown-content th,
.full-file-content .markdown-content td {
  border: 1px solid var(--border);
  padding: 6px 12px;
  text-align: left;
}

.full-file-content .markdown-content th {
  background: var(--input-bg);
  font-weight: 600;
}

.full-file-content .markdown-content a {
  color: var(--accent);
  text-decoration: none;
}

.full-file-content .markdown-content a:hover {
  text-decoration: underline;
}

.full-file-content .markdown-content img {
  max-width: 100%;
  border-radius: 4px;
}

.full-file-content .markdown-content hr {
  border: none;
  border-top: 1px solid var(--border);
  margin: 1.5em 0;
}
```

- [ ] **Step 4: Add JSON tree styles**

```css
/* JSON tree */
.json-tree {
  padding: 12px;
  font-family: 'SF Mono', Consolas, 'Menlo', monospace;
  font-size: 12px;
  line-height: 1.6;
}

.json-line {
  white-space: nowrap;
}

.json-toggle {
  cursor: pointer;
  user-select: none;
  display: inline-block;
  width: 12px;
  text-align: center;
  color: var(--text-muted);
}

.json-toggle:hover {
  color: var(--accent);
}

.json-key {
  color: var(--text);
  font-weight: 600;
}

.json-string { color: var(--green); }
.json-number { color: var(--accent); }
.json-boolean { color: #e67e22; }
.json-null { color: var(--text-muted); font-style: italic; }
.json-bracket { color: var(--text-muted); }
.json-count { color: var(--text-muted); font-size: 10px; font-style: italic; }
```

- [ ] **Step 5: Add CSV table styles**

```css
/* CSV table */
.csv-table-wrapper {
  overflow-x: auto;
  padding: 8px;
}

.csv-table {
  border-collapse: collapse;
  font-family: 'SF Mono', Consolas, 'Menlo', monospace;
  font-size: 12px;
  width: 100%;
}

.csv-table th,
.csv-table td {
  border: 1px solid var(--border);
  padding: 4px 10px;
  text-align: left;
  white-space: nowrap;
}

.csv-table th {
  background: var(--input-bg);
  font-weight: 600;
  position: sticky;
  top: 0;
  z-index: 1;
}

.csv-table tbody tr:nth-child(even) {
  background: color-mix(in srgb, var(--input-bg) 50%, var(--bg));
}
```

- [ ] **Step 6: Add image preview styles**

```css
/* Image preview */
.image-preview {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 20px;
  min-height: 200px;
}

.image-preview img {
  max-width: 100%;
  max-height: 80vh;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.image-preview .image-filename {
  margin-top: 12px;
  font-size: 12px;
  color: var(--text-muted);
}
```

- [ ] **Step 7: Add HTML preview styles**

```css
/* HTML preview */
.html-preview {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.html-preview-tabs {
  display: flex;
  gap: 0;
  border-bottom: 1px solid var(--border);
  padding: 0 8px;
  flex-shrink: 0;
}

.html-tab {
  padding: 6px 12px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}

.html-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}

.html-preview-frame {
  flex: 1;
  width: 100%;
  border: none;
  background: white;
  min-height: 300px;
}

.html-source {
  overflow: auto;
  flex: 1;
}
```

- [ ] **Step 8: Add plain text fallback style**

```css
/* Plain text fallback (replaces old x-text pre) */
.full-file-content-pre {
  margin: 0;
  padding: 12px;
  font-family: 'SF Mono', Consolas, 'Menlo', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}
```

- [ ] **Step 9: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat: add CSS styles for all file viewer renderers"
```

---

### Task 11: Manual Smoke Test

- [ ] **Step 1: Build and start the server**

Run: `cd server && go build -o claude-controller . && ./claude-controller`
Expected: Server starts on :8080.

- [ ] **Step 2: Test with a managed session**

Open the web UI, create/select a managed session with a project directory that has diverse file types. Click files in the file tree and switch to "Full" view. Verify:

- `.go` files show syntax-highlighted code with line numbers and a "Go" badge
- `.md` files render as formatted markdown with headings, lists, code blocks
- `.json` files show a collapsible tree with colored values
- `.css` files show syntax-highlighted CSS
- `.html` files show an iframe preview with a Source toggle
- Unknown extensions fall back to plain text
- `Dockerfile`, `Makefile` show syntax-highlighted code
- Diff view still works as before
- Toggling between diff and full view uses the cache (fast)

- [ ] **Step 3: Commit any fixes from smoke testing**

If any issues found during testing, fix and commit.
