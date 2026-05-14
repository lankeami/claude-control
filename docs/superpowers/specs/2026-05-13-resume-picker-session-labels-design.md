# Resume Picker Session Labels — Design Spec

**Date:** 2026-05-13
**Status:** Approved

## Problem

The `/resume` command opens a modal that lists previous Claude CLI sessions for the current project directory. Sessions without a Claude-generated summary show as "Untitled session". Sessions that do have summaries often use Claude's auto-generated text, which is rarely meaningful enough to distinguish sessions at a glance. Users cannot identify which CLI session corresponds to the work they want to resume.

## Solution Overview

Surface the managed session's user-defined `name` (set via the existing double-click rename in the sidebar) in the resume picker. When a CLI session in the picker matches the `claude_session_id` of a named managed session in our DB, that name is shown as the session's label. No new DB table is needed — this reuses the existing `sessions.name` and `sessions.claude_session_id` fields.

Additionally, fall back to `first_prompt` (already returned by the API but unused in the frontend) before showing "Untitled session", providing an immediate improvement for all sessions regardless of naming.

## Data Model

**No schema changes.** The existing `sessions` table already has:
- `claude_session_id TEXT` — the CLI session UUID, set when a managed session is initialized or resumed
- `name TEXT NOT NULL DEFAULT ''` — the user-defined name, set via the existing rename feature

**New DB method** in `server/db/sessions.go`:

```go
// GetManagedSessionNamesByCliIDs returns a map of claude_session_id → name
// for managed sessions that have a non-empty user-defined name.
func (s *Store) GetManagedSessionNamesByCliIDs(ids []string) (map[string]string, error)
// SELECT claude_session_id, name FROM sessions
// WHERE claude_session_id IN (?) AND name != ''
```

Uses a dynamic `IN (?)` placeholder built from the provided slice. Returns an empty map immediately (no query) when the slice is empty — SQLite `IN ()` with zero values is a syntax error. Returns an empty map (not error) when no matches are found.

## API Changes

### `GET /api/sessions/{id}/resumable` — extended

**Handler:** `server/api/resume.go` → `handleResumableList`

**Changes:**
1. After loading and filtering the CLI session list from `sessions-index.json` (or JSONL fallback), collect all `session_id` values.
2. Call `store.GetManagedSessionNamesByCliIDs(sessionIDs)` to get the name map.
3. Populate a new `label` field on each `resumableSession` from the map (empty string if no match).

**Updated struct:**

```go
type resumableSession struct {
    SessionID    string `json:"session_id"`
    Summary      string `json:"summary"`
    FirstPrompt  string `json:"first_prompt"`
    Label        string `json:"label"`           // NEW: from matching managed session name
    MessageCount int    `json:"message_count"`
    Created      string `json:"created"`
    Modified     string `json:"modified"`
    GitBranch    string `json:"git_branch"`
}
```

No new endpoints. No schema migrations.

## Frontend Changes

### Display chain

In the resume picker modal (`server/web/static/index.html`, currently line 1224):

**Before:**
```html
x-text="rs.summary || 'Untitled session'"
```

**After:**
```html
x-text="rs.label || rs.summary || rs.first_prompt || 'Untitled session'"
```

### Hint text

Add a small helper note below the resume picker title (visible only when the session list is loaded and non-empty):

```
Tip: rename a session from the sidebar to identify it here
```

Shown as muted subtext. Static, no state dependency beyond `!resumeLoading && resumableSessions.length > 0`.

## User Flow

1. User creates a managed session for `/path/to/project`.
2. Over time they work in it; at some point the session gets a `claude_session_id`.
3. User double-clicks the session name in the sidebar → renames it "auth JWT refactor".
4. Later, from the same or a different managed session for the same project, they type `/resume`.
5. The picker shows the CLI session that maps to "auth JWT refactor" with that label as its title.
6. Sessions without a matching managed session name fall back to `summary → first_prompt → 'Untitled session'`.

## Out of Scope

- Renaming CLI sessions directly from within the resume picker (no inline edit in the picker)
- Storing labels independently of the managed session (no `cli_session_labels` table)
- Pruning orphaned data (nothing new to prune — we're only reading existing rows)
- iOS app changes (the resume picker is web UI only)
- Hook mode sessions (feature is relevant to managed sessions only, where `claude_session_id` is tracked)
