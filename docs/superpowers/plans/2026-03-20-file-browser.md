# File Browser Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a file browser panel showing files Claude touched during a session, with a split-view file viewer displaying diffs and full file content.

**Architecture:** Right sidebar shows a file tree built from session messages. Clicking a file splits the chat area to show a viewer with diff/full toggle. New `session_files` DB table tracks files per session. Server extracts file paths from NDJSON `tool_use` content blocks during managed sessions and from transcript JSONL for hook sessions.

**Tech Stack:** Go (server/API), SQLite (persistence), Alpine.js (reactive UI), vanilla CSS (layout/styling)

**Spec:** `docs/superpowers/specs/2026-03-20-file-browser-design.md`

**Codebase patterns:**
- DB layer: `type Store struct` in `db/` package, methods on `(s *Store)`, internal field `s.db`
- API layer: `type Server struct` in `api/` package, methods on `(s *Server)`, DB access via `s.store`
- Routes: registered on `apiMux` directly (e.g., `apiMux.HandleFunc("GET /api/...", s.handlerName)`)
- Session lookup: `s.store.GetSessionByID(id)`
- Transcript path: `s.store.GetTranscriptPath(id)`
- NDJSON structure: top-level `{"type":"assistant","message":{"content":[...]}}` with `tool_use` blocks inside `content` array

---

## File Structure

### New Files
- `server/db/session_files.go` — DB methods for the `session_files` table (insert, list, check existence)
- `server/api/files.go` — HTTP handlers for `GET /api/sessions/{id}/files` and `GET /api/files/content`

### Modified Files
- `server/db/db.go:38-99` — Add `session_files` table to migration
- `server/api/router.go:49-55` — Register new file routes on `apiMux`
- `server/api/managed_sessions.go:138-154` — Extract file paths from `tool_use` content blocks in `onLine` callback
- `server/web/static/index.html:45-152` — Add file tree sidebar and file viewer panel markup
- `server/web/static/style.css` — File tree styles, viewer styles, split layout, diff coloring
- `server/web/static/app.js` — File tree state, viewer logic, SSE integration, new API calls

---

## Task 1: Database — `session_files` Table

**Files:**
- Modify: `server/db/db.go:38-99`
- Create: `server/db/session_files.go`

- [ ] **Step 1: Add migration SQL to db.go**

In `server/db/db.go`, add a new migration string to the `migrations` slice (after the existing entries around line 85):

```go
`CREATE TABLE IF NOT EXISTS session_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id),
    UNIQUE(session_id, file_path, action)
)`,
```

- [ ] **Step 2: Create session_files.go with struct and methods**

Create `server/db/session_files.go`:

```go
package db

import "time"

type SessionFile struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	FilePath  string    `json:"file_path"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertSessionFile records a file touched during a session. Ignores duplicates.
func (s *Store) InsertSessionFile(sessionID, filePath, action string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO session_files (session_id, file_path, action) VALUES (?, ?, ?)`,
		sessionID, filePath, action,
	)
	return err
}

// ListSessionFiles returns all files touched in a session.
func (s *Store) ListSessionFiles(sessionID string) ([]SessionFile, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, file_path, action, created_at FROM session_files WHERE session_id = ? ORDER BY created_at`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []SessionFile
	for rows.Next() {
		var f SessionFile
		if err := rows.Scan(&f.ID, &f.SessionID, &f.FilePath, &f.Action, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// SessionFileExists checks if a file path was touched in a session.
func (s *Store) SessionFileExists(sessionID, filePath string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_files WHERE session_id = ? AND file_path = ?`,
		sessionID, filePath,
	).Scan(&count)
	return count > 0, err
}
```

- [ ] **Step 3: Verify the server builds**

Run: `cd server && go build ./...`
Expected: Build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add server/db/db.go server/db/session_files.go
git commit -m "feat: add session_files table for tracking files touched per session"
```

---

## Task 2: Extract File Paths from Managed Session NDJSON

**Files:**
- Modify: `server/api/managed_sessions.go:138-154`

The NDJSON from Claude CLI has this structure for assistant messages:
```json
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"...","old_string":"...","new_string":"..."}}]}}
```

The `onLine` callback fires for each NDJSON line. Currently, when `parseRole(line)` returns `"assistant"`, it extracts text via `extractAssistantText`. We need to also extract file paths from `tool_use` content blocks.

- [ ] **Step 1: Add file path extraction to the onLine callback**

In `server/api/managed_sessions.go`, modify the `onLine` callback (line 138). After the existing assistant text handling block (ending at line 153), add extraction for all NDJSON lines (not just assistant — tool_use may come as separate events too):

```go
onLine := func(line string) {
    role := parseRole(line)

    // Existing assistant text persistence (keep as-is, lines 143-153)
    if role == "assistant" {
        text := extractAssistantText(line)
        if text != "" {
            _, _ = s.store.CreateMessage(sessionID, role, text)
        }
        turnCount++
        if turnCount >= sess.MaxTurns {
            log.Printf("session %s hit turn limit (%d), interrupting", sessionID, sess.MaxTurns)
            _ = s.manager.Interrupt(sessionID)
        }
    }

    // Extract file paths from tool_use content blocks
    extractSessionFiles(line, sessionID, s.store)
}
```

Then add a new function after `extractAssistantText` (after line 284):

```go
// extractSessionFiles pulls file paths from tool_use content blocks in NDJSON lines.
func extractSessionFiles(line, sessionID string, store *db.Store) {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.Message.Content == nil {
		return
	}

	var blocks []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		var inp struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(b.Input, &inp) != nil || inp.FilePath == "" {
			continue
		}
		action := ""
		switch b.Name {
		case "Edit":
			action = "edit"
		case "Write":
			action = "write"
		case "Read":
			action = "read"
		}
		if action != "" {
			_ = store.InsertSessionFile(sessionID, inp.FilePath, action)
		}
	}
}
```

- [ ] **Step 2: Verify the server builds**

Run: `cd server && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat: extract file paths from tool_use events in managed sessions"
```

---

## Task 3: API Endpoints — List Files and Read Content

**Files:**
- Create: `server/api/files.go`
- Modify: `server/api/router.go:49-55`

- [ ] **Step 1: Create files.go with both handlers**

Create `server/api/files.go`:

```go
package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type fileEntry struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

// handleListSessionFiles returns files touched during a session.
func (s *Server) handleListSessionFiles(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var files []fileEntry

	if sess.Mode == "managed" {
		dbFiles, err := s.store.ListSessionFiles(sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, f := range dbFiles {
			files = append(files, fileEntry{Path: f.FilePath, Action: f.Action})
		}
	} else {
		// Hook session: extract from transcript JSONL
		files = extractFilesFromTranscript(sess.TranscriptPath)
	}

	if files == nil {
		files = []fileEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"files": files})
}

// extractFilesFromTranscript parses transcript JSONL to find file paths.
// Uses same JSONL format as handleGetTranscript in transcript.go.
func extractFilesFromTranscript(transcriptPath string) []fileEntry {
	if transcriptPath == "" {
		return nil
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := make(map[string]bool)
	var files []fileEntry

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "assistant" {
			continue
		}

		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(entry.Message.Content, &blocks) != nil {
			continue
		}

		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			var inp struct {
				FilePath string `json:"file_path"`
			}
			if json.Unmarshal(b.Input, &inp) != nil || inp.FilePath == "" {
				continue
			}
			action := ""
			switch b.Name {
			case "Edit":
				action = "edit"
			case "Write":
				action = "write"
			case "Read":
				action = "read"
			}
			if action == "" {
				continue
			}
			key := inp.FilePath + ":" + action
			if !seen[key] {
				seen[key] = true
				files = append(files, fileEntry{Path: inp.FilePath, Action: action})
			}
		}
	}
	return files
}

const maxFileSize = 1 << 20 // 1MB

// handleGetFileContent reads a file from disk for the file viewer.
func (s *Server) handleGetFileContent(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")

	if filePath == "" || sessionID == "" {
		http.Error(w, "missing path or session_id", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Authorization: check file was touched in this session
	authorized := false
	if sess.Mode == "managed" {
		exists, err := s.store.SessionFileExists(sessionID, filePath)
		if err == nil && exists {
			authorized = true
		}
	} else {
		// Hook session: check transcript
		files := extractFilesFromTranscript(sess.TranscriptPath)
		for _, f := range files {
			if f.Path == filePath {
				authorized = true
				break
			}
		}
	}

	if !authorized {
		http.Error(w, "file not associated with session", http.StatusForbidden)
		return
	}

	// Resolve symlinks and validate path is within session cwd
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"path": filePath, "content": "", "exists": false,
			"truncated": false, "binary": false,
		})
		return
	}

	if sess.CWD != "" {
		cwd, _ := filepath.EvalSymlinks(sess.CWD)
		if cwd != "" && !strings.HasPrefix(resolved, cwd+string(filepath.Separator)) && resolved != cwd {
			http.Error(w, "file outside session working directory", http.StatusForbidden)
			return
		}
	}

	// Open and read file
	file, err := os.Open(resolved)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"path": filePath, "content": "", "exists": false,
			"truncated": false, "binary": false,
		})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	truncated := stat.Size() > maxFileSize
	readSize := stat.Size()
	if truncated {
		readSize = maxFileSize
	}

	buf := make([]byte, readSize)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf = buf[:n]

	// Binary detection: check first 512 bytes for null bytes
	checkLen := 512
	if len(buf) < checkLen {
		checkLen = len(buf)
	}
	binary := false
	for i := 0; i < checkLen; i++ {
		if buf[i] == 0 {
			binary = true
			break
		}
	}

	content := ""
	if !binary {
		content = string(buf)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"path":      filePath,
		"content":   content,
		"exists":    true,
		"truncated": truncated,
		"binary":    binary,
	})
}
```

Note: Check that the `Session` struct has a `TranscriptPath` field and a `CWD` field. Look at `server/db/sessions.go` for the exact field names and adjust accordingly. The `TranscriptPath` is set via `UpsertSession` for hook sessions. For managed sessions, `CWD` is set in `CreateManagedSession`.

- [ ] **Step 2: Register routes in router.go**

In `server/api/router.go`, add two new routes after the managed session routes (after line 55):

```go
// File browser endpoints
apiMux.HandleFunc("GET /api/sessions/{id}/files", s.handleListSessionFiles)
apiMux.HandleFunc("GET /api/files/content", s.handleGetFileContent)
```

- [ ] **Step 3: Verify the server builds**

Run: `cd server && go build ./...`
Expected: Build succeeds. Fix any import issues.

- [ ] **Step 4: Commit**

```bash
git add server/api/files.go server/api/router.go
git commit -m "feat: add API endpoints for listing session files and reading file content"
```

---

## Task 4: Web UI — File Tree Sidebar (HTML + CSS)

**Files:**
- Modify: `server/web/static/index.html:45-152`
- Modify: `server/web/static/style.css`

- [ ] **Step 1: Add file tree sidebar markup to index.html**

In `index.html`, after the main area closing `</div>` (around line 151) and before the closing dashboard `</div>`, add the file tree sidebar:

```html
<!-- File Tree Sidebar -->
<div class="file-tree-sidebar" x-show="authenticated && selectedSessionId">
  <div class="file-tree-header">
    <span>Files</span>
    <span class="file-count-badge" x-show="sessionFiles.length > 0" x-text="sessionFiles.length"></span>
  </div>
  <div class="file-tree-list">
    <template x-if="visibleFileNodes.length === 0">
      <div class="file-tree-empty">No files modified yet</div>
    </template>
    <template x-for="node in visibleFileNodes" :key="node.path + node.action">
      <div class="file-tree-item"
           :class="{ 'is-dir': node.isDir, 'is-selected': viewerFile === node.path }"
           :style="'padding-left: ' + (node.depth * 16 + 8) + 'px'"
           @click="node.isDir ? toggleDir(node) : openFileViewer(node.path)">
        <span class="file-tree-icon" x-text="node.isDir ? (node.open ? '▾' : '▸') : ''"></span>
        <span class="file-tree-name" x-text="node.name" :title="node.path"></span>
        <span class="file-tree-action" :class="'action-' + (node.action || '')"
              x-show="!node.isDir && node.action"
              x-text="node.action === 'edit' ? 'M' : node.action === 'write' ? 'A' : 'R'"></span>
      </div>
    </template>
  </div>
</div>
```

Note: `visibleFileNodes` is a computed getter that flattens the tree, skipping children of collapsed directories. This avoids Alpine.js recursive template limitations.

- [ ] **Step 2: Wrap main area content in a split container and add file viewer**

In the main area section (around line 77-151), wrap the existing content (chat-area, response-area, instruction-bar) in a flex container and add the viewer panel. The main area inner content should become:

```html
<div class="main-content-split">
  <div class="chat-column" :class="{ 'has-viewer': viewerFile }">
    <!-- Move existing: main-header, chat-area, response-area, instruction-bar here -->
  </div>
  <div class="file-viewer-column" x-show="viewerFile" x-cloak>
    <div class="file-viewer-header">
      <span class="file-viewer-path" x-text="viewerFileName"></span>
      <div class="file-viewer-controls">
        <button :class="{ active: viewerMode === 'diff' }" @click="viewerMode = 'diff'">Diff</button>
        <button :class="{ active: viewerMode === 'full' }" @click="switchToFullView()">Full</button>
        <button class="file-viewer-close" @click="closeFileViewer()">&times;</button>
      </div>
    </div>
    <div class="file-viewer-body">
      <div x-show="viewerMode === 'diff'" class="diff-view">
        <template x-if="viewerDiffs.length === 0">
          <div class="diff-empty">No edits recorded for this file. Switch to Full view to see file content.</div>
        </template>
        <template x-for="(diff, i) in viewerDiffs" :key="i">
          <div class="diff-block">
            <div class="diff-old" x-show="diff.old_string" x-text="diff.old_string"></div>
            <div class="diff-new" x-text="diff.new_string || diff.content"></div>
          </div>
        </template>
      </div>
      <div x-show="viewerMode === 'full'" class="full-view">
        <div x-show="viewerLoading" class="viewer-loading">Loading...</div>
        <div x-show="viewerBinary" class="viewer-binary">Binary file — cannot display.</div>
        <pre x-show="!viewerLoading && !viewerBinary" class="full-file-content" x-text="viewerContent"></pre>
        <div x-show="viewerTruncated" class="viewer-truncated">File truncated (exceeds 1MB).</div>
      </div>
    </div>
  </div>
</div>
```

- [ ] **Step 3: Add CSS styles**

Append to `server/web/static/style.css`:

```css
/* File Tree Sidebar */
.file-tree-sidebar {
  width: 280px;
  min-width: 280px;
  border-left: 1px solid var(--border);
  background: var(--bg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.file-tree-header {
  padding: 12px 16px;
  font-weight: 600;
  font-size: 14px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 8px;
}
.file-count-badge {
  background: var(--bg-secondary);
  color: var(--text-muted);
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 8px;
  font-weight: 500;
}
.file-tree-list {
  overflow-y: auto;
  flex: 1;
  padding: 4px 0;
}
.file-tree-empty {
  color: var(--text-muted);
  font-size: 13px;
  padding: 16px;
  text-align: center;
}
.file-tree-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  cursor: pointer;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.file-tree-item:hover { background: var(--bg-secondary); }
.file-tree-item.is-selected { background: color-mix(in srgb, var(--accent) 15%, transparent); }
.file-tree-icon { width: 12px; font-size: 10px; color: var(--text-muted); flex-shrink: 0; }
.file-tree-name { overflow: hidden; text-overflow: ellipsis; }
.file-tree-action {
  margin-left: auto; font-size: 10px; font-weight: 600;
  padding: 1px 4px; border-radius: 3px; flex-shrink: 0;
}
.file-tree-action.action-edit { color: var(--yellow); }
.file-tree-action.action-write { color: var(--green); }
.file-tree-action.action-read { color: var(--text-muted); }

/* Main content split */
.main-content-split { display: flex; flex: 1; overflow: hidden; }
.chat-column { display: flex; flex-direction: column; flex: 1; min-width: 0; overflow: hidden; }

/* File Viewer */
.file-viewer-column {
  flex: 1; min-width: 0; border-left: 1px solid var(--border);
  display: flex; flex-direction: column; overflow: hidden;
}
.file-viewer-header {
  padding: 8px 12px; border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between;
  gap: 8px; background: var(--bg-secondary);
}
.file-viewer-path { font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.file-viewer-controls { display: flex; gap: 4px; flex-shrink: 0; }
.file-viewer-controls button {
  padding: 3px 10px; font-size: 12px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text); border-radius: 4px; cursor: pointer;
}
.file-viewer-controls button.active { background: var(--accent); color: white; border-color: var(--accent); }
.file-viewer-close { border: none !important; background: none !important; color: var(--text-muted) !important; font-size: 16px !important; padding: 2px 6px !important; }
.file-viewer-close:hover { color: var(--text) !important; }
.file-viewer-body {
  flex: 1; overflow-y: auto; padding: 12px;
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace; font-size: 12px; line-height: 1.5;
}

/* Diff blocks */
.diff-block { margin-bottom: 16px; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
.diff-old { background: color-mix(in srgb, var(--red) 12%, var(--bg)); padding: 8px 12px; white-space: pre-wrap; word-break: break-word; border-left: 3px solid var(--red); }
.diff-new { background: color-mix(in srgb, var(--green) 12%, var(--bg)); padding: 8px 12px; white-space: pre-wrap; word-break: break-word; border-left: 3px solid var(--green); }
.diff-empty { color: var(--text-muted); padding: 24px; text-align: center; }

/* Full file view */
.full-file-content { margin: 0; white-space: pre-wrap; word-break: break-word; }
.viewer-loading, .viewer-binary, .viewer-truncated { color: var(--text-muted); padding: 8px 0; }
.viewer-truncated { border-top: 1px solid var(--border); margin-top: 12px; padding-top: 12px; font-size: 11px; }

/* Mobile: hide file panels */
@media (max-width: 768px) {
  .file-tree-sidebar { display: none; }
  .file-viewer-column { display: none; }
}
```

- [ ] **Step 4: Verify the layout renders**

Run: `cd server && go run .`
Open the web UI. Check: file tree sidebar appears on the right when a session is selected (with "No files modified yet" empty state), chat area still renders correctly.

- [ ] **Step 5: Commit**

```bash
git add server/web/static/index.html server/web/static/style.css
git commit -m "feat: add file tree sidebar and file viewer panel layout"
```

---

## Task 5: Web UI — File Tree + Viewer Logic (Alpine.js)

**Files:**
- Modify: `server/web/static/app.js:2-54` (Alpine data init)
- Modify: `server/web/static/app.js` (add new methods)

- [ ] **Step 1: Add reactive state to Alpine data init**

In `app.js`, add these properties to the Alpine data object (around line 35, near existing state):

```javascript
// File browser state
sessionFiles: [],
fileTreeData: [],  // nested tree structure with open/closed state
viewerFile: null,
viewerMode: 'diff',
viewerDiffs: [],
viewerContent: '',
viewerLoading: false,
viewerBinary: false,
viewerTruncated: false,
fileContentCache: {},
```

- [ ] **Step 2: Add the `visibleFileNodes` computed getter**

This flattens the nested tree into a list, skipping children of collapsed dirs:

```javascript
get visibleFileNodes() {
  const nodes = [];
  const walk = (items) => {
    for (const node of items) {
      nodes.push(node);
      if (node.isDir && node.open && node.children) {
        walk(node.children);
      }
    }
  };
  walk(this.fileTreeData);
  return nodes;
},
```

- [ ] **Step 3: Add file tree loading and building methods**

```javascript
async loadSessionFiles(sessionId) {
  if (!sessionId) { this.sessionFiles = []; this.fileTreeData = []; return; }
  try {
    const resp = await fetch(`/api/sessions/${sessionId}/files`, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!resp.ok) { this.sessionFiles = []; this.fileTreeData = []; return; }
    const data = await resp.json();
    this.sessionFiles = data.files || [];
    this.fileTreeData = this.buildFileTree(this.sessionFiles);
  } catch (e) {
    this.sessionFiles = [];
    this.fileTreeData = [];
  }
},

buildFileTree(files) {
  if (!files || files.length === 0) return [];

  // Find common prefix to strip
  const paths = files.map(f => f.path);
  const prefix = this.commonPrefix(paths);

  // Build nested structure
  const root = {};
  for (const file of files) {
    const rel = file.path.substring(prefix.length).replace(/^\//, '');
    const parts = rel.split('/');
    let node = root;
    for (let i = 0; i < parts.length; i++) {
      if (!node[parts[i]]) node[parts[i]] = {};
      if (i < parts.length - 1) {
        node = node[parts[i]];
      } else {
        node[parts[i]]._file = file;
      }
    }
  }

  // Convert to array with depth info
  const toArray = (obj, depth) => {
    const entries = Object.entries(obj).filter(([k]) => k !== '_file');
    entries.sort(([a, aVal], [b, bVal]) => {
      const aDir = !aVal._file;
      const bDir = !bVal._file;
      if (aDir !== bDir) return aDir ? -1 : 1;
      return a.localeCompare(b);
    });
    const result = [];
    for (const [name, val] of entries) {
      if (val._file) {
        result.push({ name, path: val._file.path, action: val._file.action, isDir: false, depth, open: false, children: [] });
      } else {
        const children = toArray(val, depth + 1);
        result.push({ name, path: prefix + name, isDir: true, depth, open: true, children, action: null });
      }
    }
    return result;
  };
  return toArray(root, 0);
},

commonPrefix(paths) {
  if (paths.length === 0) return '';
  if (paths.length === 1) return paths[0].substring(0, paths[0].lastIndexOf('/') + 1);
  let prefix = paths[0];
  for (let i = 1; i < paths.length; i++) {
    while (prefix.length > 0 && !paths[i].startsWith(prefix)) {
      prefix = prefix.substring(0, prefix.lastIndexOf('/'));
    }
  }
  if (prefix && !prefix.endsWith('/')) prefix = prefix.substring(0, prefix.lastIndexOf('/') + 1);
  return prefix || '/';
},

toggleDir(node) {
  node.open = !node.open;
},
```

- [ ] **Step 4: Add file viewer methods**

```javascript
openFileViewer(filePath) {
  if (this.viewerFile === filePath) { this.closeFileViewer(); return; }
  this.viewerFile = filePath;
  this.viewerMode = 'diff';
  this.viewerContent = '';
  this.viewerLoading = false;
  this.viewerBinary = false;
  this.viewerTruncated = false;

  // Build diffs from chat messages (works for hook sessions with transcript data)
  this.viewerDiffs = this.chatMessages
    .filter(m => m.file_path === filePath && (m.msg_type === 'edit' || m.msg_type === 'write'))
    .map(m => ({ old_string: m.old_string || '', new_string: m.new_string || '', content: m.content || '', type: m.msg_type }));
},

get viewerFileName() {
  if (!this.viewerFile) return '';
  return this.viewerFile.split('/').pop();
},

async switchToFullView() {
  this.viewerMode = 'full';
  if (!this.viewerFile) return;

  const cacheKey = this.viewerFile + ':' + this.selectedSessionId;
  if (this.fileContentCache[cacheKey]) {
    const cached = this.fileContentCache[cacheKey];
    this.viewerContent = cached.content;
    this.viewerBinary = cached.binary;
    this.viewerTruncated = cached.truncated;
    return;
  }

  this.viewerLoading = true;
  try {
    const params = new URLSearchParams({ path: this.viewerFile, session_id: this.selectedSessionId });
    const resp = await fetch('/api/files/content?' + params, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!resp.ok) { this.viewerContent = 'Error loading file.'; return; }
    const data = await resp.json();
    this.viewerContent = data.content || '';
    this.viewerBinary = data.binary || false;
    this.viewerTruncated = data.truncated || false;
    if (!data.exists) this.viewerContent = 'File no longer exists on disk.';
    this.fileContentCache[cacheKey] = data;
  } catch (e) {
    this.viewerContent = 'Error loading file.';
  } finally {
    this.viewerLoading = false;
  }
},

closeFileViewer() {
  this.viewerFile = null;
  this.viewerDiffs = [];
  this.viewerContent = '';
},
```

- [ ] **Step 5: Wire file loading into session selection**

Find the session selection handler (the `$watch` or method that calls `fetchManagedMessages` or `fetchTranscript`). Add cleanup at the start and loading after chat messages load:

```javascript
// At start of session switch:
this.closeFileViewer();
this.sessionFiles = [];
this.fileTreeData = [];
this.fileContentCache = {};

// After chat messages are loaded:
this.loadSessionFiles(this.selectedSessionId);
```

- [ ] **Step 6: Add SSE integration for real-time file tree updates**

In the SSE message handler (`startSessionSSE`, around line 516-573), add file path extraction from tool_use events. Find where SSE messages are parsed and add:

```javascript
// When a new SSE data line arrives and is parsed as JSON:
// Check for tool_use content blocks inside assistant messages
try {
  const parsed = JSON.parse(event.data);
  // The NDJSON has message.content[] with tool_use blocks
  if (parsed.message && parsed.message.content) {
    const content = parsed.message.content;
    if (Array.isArray(content)) {
      for (const block of content) {
        if (block.type === 'tool_use' && block.input && block.input.file_path) {
          const toolName = block.name;
          if (['Edit', 'Write', 'Read'].includes(toolName)) {
            const action = toolName.toLowerCase();
            if (!this.sessionFiles.find(f => f.path === block.input.file_path && f.action === action)) {
              this.sessionFiles.push({ path: block.input.file_path, action });
              this.fileTreeData = this.buildFileTree(this.sessionFiles);
            }
          }
        }
      }
    }
  }
} catch (e) { /* ignore parse errors */ }
```

Integrate this into the existing SSE message handler — don't create a separate listener. The exact integration point depends on how the handler currently processes `event.data`.

- [ ] **Step 7: Verify end-to-end**

Run: `cd server && go run .`
1. Create a managed session and send a message that triggers file edits
2. Verify the file tree populates in the right sidebar
3. Click a file — verify the viewer opens with diff view
4. Toggle to Full — verify content loads from disk
5. Click X — verify chat returns to full width
6. Switch sessions — verify tree resets and reloads

- [ ] **Step 8: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add file tree logic, viewer, and SSE integration"
```

---

## Task 6: Integration Testing

- [ ] **Step 1: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests pass (new table created by migration is backward-compatible).

- [ ] **Step 2: Manual end-to-end test**

Start server, create a managed session, send messages that trigger Edit/Write/Read tools. Verify:
- File tree populates with correct file paths
- Diff view shows old_string/new_string for edits
- Full view loads file content from disk
- Tree updates live as new tool_use events arrive via SSE
- Switching sessions clears and reloads the tree
- Authorization: `/api/files/content` rejects paths not in the session

- [ ] **Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: integration fixes for file browser panel"
```
