# File Browser Panel — Design Spec

## Overview

Add a file browser panel to the web UI that shows files Claude has touched during a session, with a split-view file viewer showing diffs and full content.

## Layout

The dashboard gains a third column:

```
┌──────────┬─────────────────────────┬──────────────┐
│ Sessions │        Chat Area        │  File Tree   │
│ (260px)  │      (flex: 1)          │   (280px)    │
│          │                         │              │
│          │                         │  ▸ src/      │
│          │                         │    main.go   │
│          │                         │    api.go    │
│          │                         │  ▸ web/      │
│          │                         │    style.css │
└──────────┴─────────────────────────┴──────────────┘
```

When a file is clicked, the chat area splits:

```
┌──────────┬──────────────┬──────────────┬──────────────┐
│ Sessions │    Chat      │ File Viewer  │  File Tree   │
│ (260px)  │  (flex: 1)   │  (flex: 1)   │   (280px)    │
│          │              │              │              │
│          │              │ [Diff] [Full]│              │
│          │              │              │              │
│          │              │ - old line   │              │
│          │              │ + new line   │              │
└──────────┴──────────────┴──────────────┴──────────────┘
```

Clicking outside (or a close button) collapses the viewer back to chat-only.

## File Tree

### Data Source

Files are extracted differently per session mode:

**Hook sessions:** The `/api/sessions/{id}/transcript` endpoint returns structured messages with `msg_type` (`edit`, `write`) and `file_path` fields. The client extracts file paths from these.

**Managed sessions:** The SSE stream from `/api/sessions/{id}/stream` receives raw NDJSON from Claude CLI, including `tool_use` events for Edit/Write tools. Currently these are discarded client-side. The fix:

1. **Client-side capture:** When processing SSE events, parse `tool_use` events with tool names `Edit`, `Write`, or `Read`. Extract `file_path` from the tool input and store in a client-side `sessionFiles` map.
2. **Server-side persistence:** Add a `session_files` table to track files touched per session. When the managed session handler processes NDJSON lines, extract file paths from `tool_use` events and insert into this table. This ensures file data survives page refreshes.
3. **New endpoint:** `GET /api/sessions/{id}/files` returns the list of files touched in a session (from the `session_files` table for managed sessions, or parsed from transcript for hook sessions).

### Tree Building

1. Collect file paths from the files endpoint (or client-side capture for live updates)
2. Deduplicate and organize into a directory tree
3. Sort directories first, then files alphabetically
4. Strip common prefix (session `cwd`) to keep paths short

### Tree Rendering

- Directories are collapsible (click to expand/collapse)
- Files show just the filename; full path shown on hover (title attribute)
- Files with edits get a colored dot indicator (blue for edited, green for created)
- Tree header: "Files" with a count badge
- Empty state: "No files modified yet" when no file messages exist

### Real-Time Updates

During an active managed session, the file tree updates live:
- As SSE `tool_use` events arrive, new file paths are added to the tree immediately
- No full re-fetch needed — just append to the existing client-side tree data
- For hook sessions, the tree updates when new transcript data is loaded

## File Viewer

### Diff View (Default)

When a file is clicked, the viewer opens showing all changes made to that file during the session:

- Collect all `edit` messages for the file, in chronological order
- Display each edit as a diff block: red background for `old_string`, green background for `new_string`
- Separator between multiple edits to the same file
- For `write` messages (new file creation), show "New file created" header and use the Full File View content (since write content is not stored in transcript messages)

### Full File View (Toggle)

- New API endpoint: `GET /api/files/content?path=<absolute_path>&session_id=<id>`
- Server reads the file from disk and returns its content
- Displayed as plain text with a monospace font
- Session ID is required for authorization
- Client caches the response (keyed by path + session) to avoid redundant disk reads

### Toggle Controls

- Two buttons at top of viewer: `[Diff]` `[Full]`
- Active mode is highlighted
- Default is Diff

### Close Behavior

- "X" button in the viewer header to close
- Clicking the same file again in the tree also closes it
- When closed, the chat area returns to full width

## New API Endpoints

### `GET /api/sessions/{id}/files`

Returns the list of files touched during a session.

**Response:**
```json
{
  "files": [
    {"path": "/abs/path/to/main.go", "action": "edit"},
    {"path": "/abs/path/to/new.go", "action": "write"}
  ]
}
```

For managed sessions, reads from `session_files` table. For hook sessions, parses the transcript JSONL on demand.

### `GET /api/files/content`

**Query params:**
- `path` (string, required) — absolute file path
- `session_id` (string, required) — session ID for authorization

**Authorization:**
- Requires valid API key (Bearer token)
- Server checks that the file path appears in the `session_files` table or transcript for the given session
- Server resolves symlinks (`filepath.EvalSymlinks`) and validates the resolved path is within the session's `cwd`

**Limits:**
- Max file size: 1MB. Files exceeding this return truncated content with a `truncated: true` flag.
- Binary detection: If the first 512 bytes contain null bytes, return `{"binary": true, "content": ""}` instead of raw content.

**Response:**
```json
{
  "path": "/path/to/file.go",
  "content": "package main\n...",
  "exists": true,
  "truncated": false,
  "binary": false
}
```

If the file has been deleted since the session:
```json
{
  "path": "/path/to/file.go",
  "content": "",
  "exists": false
}
```

## Database Changes

### New table: `session_files`

```sql
CREATE TABLE session_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    action TEXT NOT NULL,  -- 'edit', 'write', 'read'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id),
    UNIQUE(session_id, file_path, action)
);
```

Populated by the managed session NDJSON handler when it encounters `tool_use` events for file-related tools.

## Mobile Behavior

On mobile (< 768px):
- File tree is hidden by default (same as sessions sidebar)
- A "Files" button in the header toggles a dropdown/overlay with the file list
- Clicking a file opens the viewer as a full-screen overlay with back button
- No split view on mobile — viewer replaces the chat entirely

## Implementation Components

### Server (Go)
- `db/session_files.go` — new table, insert/query methods
- `api/files.go` — handlers for `GET /api/sessions/{id}/files` and `GET /api/files/content`
- `managed/session.go` — extract file paths from `tool_use` NDJSON events during streaming
- DB migration in `db/db.go` to create `session_files` table

### Web UI (HTML/CSS/JS)
- **index.html**: Add file tree sidebar markup, file viewer panel markup
- **style.css**: File tree styles, viewer styles, split layout, diff coloring, mobile responsive rules
- **app.js**:
  - `loadSessionFiles(sessionId)` — fetch files list from API
  - `buildFileTree(files)` — organize flat list into tree structure
  - `openFileViewer(filePath)` — show viewer with diff
  - `toggleFileView(mode)` — switch between diff/full
  - `closeFileViewer()` — collapse viewer
  - `fetchFileContent(path, sessionId)` — API call for full file content (with caching)
  - SSE handler update: capture `tool_use` events to update tree in real-time
  - Reactive state: `sessionFiles`, `viewerFile`, `viewerMode`, `viewerContent`, `fileContentCache`
