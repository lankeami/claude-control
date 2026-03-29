# Auto-Continue with Compact — Design Spec

**Date:** 2026-03-29
**Issue:** #63

## Problem

Claude's auto-continue system resumes sessions up to `max_continuations` times, but each continuation carries the full conversation context. Over 5 continuations, token usage grows significantly. Running `/compact` periodically during auto-continue cycles reduces context size and saves tokens.

## Solution

Add a `compact_every_n_continues` setting per session. When set to N (> 0), every Nth continuation triggers a compact step before resuming work. When set to 0 (default), no compacting occurs — preserving current behavior.

## New Setting

- **`compact_every_n_continues`** — integer, default 0 (disabled)
- When > 0, on continuation counts divisible by N, run `/compact` before resuming
- Example: `compact_every_n_continues: 2` → compact on continuations 2, 4, 6...

## Database Change

New column on `sessions` table:

```sql
ALTER TABLE sessions ADD COLUMN compact_every_n_continues INTEGER NOT NULL DEFAULT 0
```

## Auto-Continue Loop Change

Current flow:
1. SIGINT the process (turn threshold reached)
2. Increment continuation count
3. Check max_continuations
4. Resume with "Continue where you left off."

New flow:
1. SIGINT the process (turn threshold reached)
2. Increment continuation count
3. Check max_continuations
4. **If `compact_every_n_continues > 0` AND `continuation_count % compact_every_n_continues == 0`:**
   a. Broadcast `compacting` SSE event
   b. Persist system message: "Running /compact to reduce context size..."
   c. Spawn `claude --resume <session_id> -p "/compact"` and wait for exit
   d. Broadcast `compact_complete` SSE event
   e. Persist system message: "Compact complete."
5. Resume with "Continue where you left off."

The compact step is a separate `claude -p` invocation that runs the slash command and exits. The subsequent resume inherits the compacted context.

## SSE Events

New events:
- `compacting`: `{"type":"compacting","continuation_count":N}` — broadcast before compact runs
- `compact_complete`: `{"type":"compact_complete","continuation_count":N}` — broadcast after compact finishes

## API Changes

### Session Creation
`POST /api/sessions/create` — new optional field:
- `compact_every_n_continues` (integer, default 0)

### Session JSON
The `Session` struct includes `compact_every_n_continues` in JSON responses.

## Web UI Changes

### Session Creation Form
- Add "Compact every N continues" number input (default 0, min 0)
- Help text: "Run /compact every N auto-continues to reduce token usage. 0 = disabled."

### SSE Event Handling
- `compacting` event: show system message "Running /compact to reduce context size..."
- `compact_complete` event: show system message "Compact complete."

### Turn Monitor
- When compacting is active, show a "Compacting..." indicator

## Files to Change

1. `server/db/db.go` — add migration for new column
2. `server/db/sessions.go` — update Session struct and CreateSession query
3. `server/api/managed_sessions.go` — update session creation endpoint and add compact logic to auto-continue loop
4. `server/web/static/app.js` — add UI setting, handle new SSE events

## Edge Cases

- If compact process fails (non-zero exit), log the error and continue with the resume anyway — don't block auto-continue on a failed compact
- If `compact_every_n_continues` is greater than `max_continuations`, compact never triggers — that's fine, user chose those settings
- Compact counts as a separate process invocation but does NOT count as a continuation
