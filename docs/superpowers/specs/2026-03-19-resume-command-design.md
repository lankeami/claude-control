# `/resume` Command in Web UI — Design Spec

## Overview

Add a `/resume` command to the claude-controller web UI that lists recent Claude Code CLI sessions for the current project and lets the user resume one in the current managed session window.

## How It Works

1. User types `/resume` in the web UI chat input for a managed session
2. Web UI intercepts it (does not send as a message to Claude)
3. Calls `GET /api/sessions/{id}/resumable` on the server
4. Server looks up the managed session's CWD, derives the `sessions-index.json` path, reads and returns the entries
5. Web UI renders a picker overlay showing recent sessions
6. User picks one → web UI calls `POST /api/sessions/{id}/resume` with `{"session_id": "<chosen-uuid>"}`
7. Server interrupts any running process on the current managed session, updates the session's internal ID to the chosen UUID, sets `initialized = true` (so next message uses `--resume`)
8. Web UI clears chat and is ready for the next message in the resumed session

## Data Source

Claude Code stores session metadata at:

```
~/.claude/projects/<encoded-cwd>/sessions-index.json
```

### Path encoding

The CWD is encoded by replacing each `/` with `-` and trimming the leading `-`.

Example: `/Users/jaychinthrajah/workspaces/_personal_/kidventory` becomes `Users-jaychinthrajah-workspaces--personal--kidventory`.

### sessions-index.json format

```json
{
  "version": 1,
  "entries": [
    {
      "sessionId": "uuid",
      "fullPath": "/Users/.../<uuid>.jsonl",
      "fileMtime": 1769973620356,
      "firstPrompt": "Create a new document...",
      "summary": "Complete TestFlight Guide for Beginners",
      "messageCount": 6,
      "created": "2026-01-21T15:17:25.170Z",
      "modified": "2026-01-21T15:36:58.223Z",
      "gitBranch": "main",
      "projectPath": "/Users/.../kidventory",
      "isSidechain": false
    }
  ]
}
```

## API

### `GET /api/sessions/{id}/resumable`

Returns the list of Claude Code sessions available for resume, scoped to the managed session's CWD.

**Response:**

```json
{
  "sessions": [
    {
      "session_id": "uuid",
      "summary": "Complete TestFlight Guide for Beginners",
      "first_prompt": "Create a new document...",
      "message_count": 6,
      "created": "2026-01-21T15:17:25.170Z",
      "modified": "2026-01-21T15:36:58.223Z",
      "git_branch": "main"
    }
  ]
}
```

- Sorted by `modified` descending (most recent first)
- Limited to 20 entries
- Excludes sidechain sessions (`isSidechain: true`)

**Error cases:**
- 404 if session not found or not a managed session
- 404 if `sessions-index.json` doesn't exist for this project (no prior CLI sessions)
- 500 if file is unreadable or malformed

### `POST /api/sessions/{id}/resume`

Switches the managed session to resume a specific Claude Code session.

**Request:**

```json
{
  "session_id": "uuid-of-session-to-resume"
}
```

**Behavior:**
1. Validate that `session_id` exists in the resumable list (prevent arbitrary UUID injection)
2. If a process is currently running on this managed session, send SIGINT and wait for it to stop
3. Update the managed session's ID in the database to the chosen session UUID
4. Set `initialized = true` so the next `POST /api/sessions/{id}/message` uses `--resume <uuid>` instead of `--session-id <uuid>`
5. Return 200 with the updated session

**Response:**

```json
{
  "id": "new-session-uuid",
  "status": "idle",
  "cwd": "/path/to/project"
}
```

**Error cases:**
- 404 if managed session not found
- 400 if `session_id` is not in the resumable list
- 409 if session is in a state that can't be interrupted

## Web UI Changes

### Command interception

In `handleInput()`, before sending a message, check if the input starts with `/resume`. If so:
- Don't send it as a message
- Call `GET /api/sessions/{id}/resumable`
- Show the picker

### Picker overlay

A modal/overlay (similar to the existing new-session modal) showing:
- Each session as a row with:
  - **Summary** (bold) — the session's summary field
  - **First prompt** (truncated to ~80 chars, muted text)
  - **Branch** badge
  - **Message count** and **last modified** (relative time)
- Clicking a row calls `POST /api/sessions/{id}/resume` with that session's UUID
- Close button / click outside to dismiss

### After resume

- Close the picker
- Clear `chatMessages`
- Update the session reference (the managed session now has a new ID)
- Toast: "Resumed: <summary>"
- Ready for the user to type their next message

## Server Implementation

### Path encoding function

```go
func claudeProjectDir(cwd string) string {
    // /Users/foo/bar -> Users-foo-bar
    encoded := strings.ReplaceAll(cwd, "/", "-")
    encoded = strings.TrimPrefix(encoded, "-")
    return encoded
}
```

### Reading sessions-index.json

```go
func sessionsIndexPath(cwd string) string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".claude", "projects", claudeProjectDir(cwd), "sessions-index.json")
}
```

### Session ID swap

The key operation is updating the managed session's `id` in the database to the chosen session UUID. This requires:
1. Updating the `sessions` table row's `id` column
2. Updating any foreign keys in `messages` that reference the old ID
3. Updating the in-memory process manager's tracking map

Since this is a managed session and messages are stored in the `messages` table keyed by `session_id`, we need to either:
- **Option A:** Change the session's `id` column and cascade-update `messages.session_id`
- **Option B:** Add a separate `claude_session_id` field that tracks which CLI session to `--resume`, leaving the managed session's own ID stable

**Option B is cleaner** — it avoids primary key changes and keeps the managed session's identity stable in the web UI, SSE subscriptions, and URL routes. The `claude_session_id` is what gets passed to `--resume`.

### New DB column

```sql
ALTER TABLE sessions ADD COLUMN claude_session_id TEXT;
```

When `claude_session_id` is set, the message handler uses it for `--resume` instead of the session's own `id`. When null, falls back to using the session `id` (current behavior).

## Files to modify

- `server/db/db.go` — migration to add `claude_session_id` column
- `server/db/sessions.go` — add `ClaudeSessionID` field, `SetClaudeSessionID()`, update `scanSession()`
- `server/api/managed_sessions.go` — add `handleResumableList()` and `handleResumeSession()` handlers; update message handler to use `claude_session_id` when set
- `server/api/router.go` — register new routes
- `server/web/static/app.js` — `/resume` interception, picker UI, resume flow
- `server/web/static/index.html` — picker overlay markup (if not generated dynamically in JS)
