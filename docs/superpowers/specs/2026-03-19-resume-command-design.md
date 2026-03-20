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

The CWD is encoded by replacing `/`, `_`, and `.` with `-`. The leading `-` is kept (not trimmed).

Example: `/Users/jaychinthrajah/workspaces/_personal_/kidventory` becomes `-Users-jaychinthrajah-workspaces--personal--kidventory`.

Verified against real directories:
- `_personal_` → `--personal--` (underscores replaced)
- `/.claude-worktrees/` → `--claude-worktrees-` (dot replaced)

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
2. If a process is currently running on this managed session, call `Teardown(sessionID, 5*time.Second)` to send SIGINT and wait for the process to exit (not just `Interrupt()`, which doesn't wait)
3. Set `claude_session_id` to the chosen UUID and `initialized = true` atomically in a single DB update
4. Delete existing messages from the `messages` table for this managed session (they belong to the previous conversation)
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

### Discoverability

Add a hint to the chat input placeholder for managed sessions: `"Send a message... (type /resume to continue a previous session)"`.

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
    // /Users/foo/_bar_/.baz -> -Users-foo--bar---baz
    // Replace /, _, and . with -
    r := strings.NewReplacer("/", "-", "_", "-", ".", "-")
    return r.Replace(cwd)
}
```

### Reading sessions-index.json

```go
func sessionsIndexPath(cwd string) (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("cannot determine home directory: %w", err)
    }
    return filepath.Join(home, ".claude", "projects", claudeProjectDir(cwd), "sessions-index.json"), nil
}
```

### Session ID swap

A separate `claude_session_id` column tracks which CLI session to `--resume`, leaving the managed session's own `id` stable in the web UI, SSE subscriptions, and URL routes.

### New DB column

```sql
ALTER TABLE sessions ADD COLUMN claude_session_id TEXT;
```

When `claude_session_id` is set, the message handler uses it for `--resume`/`--session-id` instead of the session's own `id`. When null, falls back to using the session `id` (current behavior).

### Message handler change

The `handleSendMessage` handler must use `claude_session_id` when set:

```go
resumeID := sessionID
if sess.ClaudeSessionID != "" {
    resumeID = sess.ClaudeSessionID
}
if sess.Initialized {
    args = append(args, "--resume", resumeID)
} else {
    args = append(args, "--session-id", resumeID)
}
```

### Message cleanup on resume

When resuming a different CLI session, delete all existing rows from the `messages` table for this managed session. They belong to the previous conversation and would otherwise intermix with the resumed session's output.

### Session JSON includes `claude_session_id`

The `Session` struct's JSON serialization includes `claude_session_id` so the web UI knows which CLI session is active. The picker filters out the currently active `claude_session_id` to avoid no-op resumes.

## Files to modify

- `server/db/db.go` — migration to add `claude_session_id` column
- `server/db/sessions.go` — add `ClaudeSessionID` field, `SetClaudeSessionID()`, update `scanSession()`
- `server/api/managed_sessions.go` — add `handleResumableList()` and `handleResumeSession()` handlers; update message handler to use `claude_session_id` when set
- `server/api/router.go` — register new routes
- `server/web/static/app.js` — `/resume` interception, picker UI, resume flow
- `server/web/static/index.html` — picker overlay markup (if not generated dynamically in JS)
