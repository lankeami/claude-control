# Session Activity State Indicators

**Date:** 2026-03-23
**Issue:** #37 — Need a visual identifier for when a session requires user input

## Problem

When working across multiple projects simultaneously, there's no way to tell at a glance which sessions are actively working, which need user input, and which are idle. The session list sidebar shows a status dot, but the logic only distinguishes `idle` vs `active`/`running` — it never surfaces a "waiting for input" state.

## Approach

**Backend-derived state persisted to DB (Approach B).** Track a new `activity_state` field on each session, updated server-side during the managed process lifecycle. This makes state available to all clients (web UI, iOS app) via the existing global events endpoint, and survives page reloads.

## States

| State | Meaning | When Set | Visual |
|-------|---------|----------|--------|
| `working` | Claude process is actively running | Process spawned | Yellow dot, pulsing animation |
| `waiting` | Process exited cleanly, awaiting next user message | Process exits with code 0 | Green dot |
| `idle` | No active process, session inactive or errored out | Default, process exits non-zero, or no recent activity | Gray dot |

## Design

### Database

Add `activity_state` column to the `sessions` table:

```sql
ALTER TABLE sessions ADD COLUMN activity_state TEXT NOT NULL DEFAULT 'idle';
```

Valid values: `working`, `waiting`, `idle`. Existing rows default to `idle`.

The `activity_state` field is added to the `Session` Go struct with JSON tag `"activity_state"`.

### Backend Lifecycle Transitions

All transitions happen in the managed session handler code (`server/api/managed_sessions.go`):

1. **User sends a message** (POST to send endpoint, which spawns `claude -p`) → set `activity_state = 'working'`
2. **Process exits with code 0** (normal completion in SSE stream handler) → set `activity_state = 'waiting'`
3. **Process exits with non-zero code** (error/interrupt) → set `activity_state = 'idle'`
4. **Shell command starts** (`handleShellExecute`) → set `activity_state = 'working'`
5. **Shell command exits** → set `activity_state = 'waiting'` (code 0) or `idle` (non-zero)
6. **Session created** → starts as `idle` (default)
7. **Server startup** → reset any `activity_state = 'working'` to `idle` (stale from prior crash/restart)

A new DB method `UpdateActivityState(sessionID, state string)` handles these updates.

### Relationship to Existing `status` Field

The existing `status` field (`idle`/`running`/`active`) and its `SetSessionStatus()` calls in managed session handlers are **replaced** by `activity_state` and `UpdateActivityState()`. The `status` field remains in the DB and struct for backward compatibility with hook-mode sessions and the iOS app, but managed session code will no longer call `SetSessionStatus()` — it will use `UpdateActivityState()` exclusively. The frontend switches to reading `activity_state` for managed sessions.

### Hook-Mode Sessions

Hook-mode sessions don't have server-managed processes, so `activity_state` stays at its default `idle`. The frontend continues to derive hook-mode status from `last_seen_at` staleness (existing logic). This is a managed-mode enhancement only.

### Frontend Changes

**`sessionStatus()` in `app.js`:** For managed sessions, return `session.activity_state` instead of `session.status`. Map to CSS classes:

```javascript
sessionStatus(session) {
  if (session.mode === 'managed') {
    const state = session.activity_state || 'idle';
    if (state === 'working') return 'active';   // yellow dot (existing CSS)
    if (state === 'waiting') return 'waiting';   // green dot (existing CSS)
    return 'idle';                                // gray dot (existing CSS)
  }
  // Hook mode: existing staleness logic unchanged
  const lastSeen = new Date(session.last_seen_at);
  const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
  if (lastSeen < fiveMinAgo) return 'idle';
  return session.status;
}
```

**CSS:** Add a pulse animation to the `.status-dot.active` class to visually distinguish "working" from static states:

```css
.status-dot.active {
  background: var(--yellow);
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
```

The existing `.status-dot.waiting` (green) and `.status-dot.idle` (gray) classes already have correct colors.

### Global Events Endpoint

No changes needed — `/api/events` already returns the full session object. Once `activity_state` is in the Go struct, it's automatically included in JSON serialization.

### SSE Per-Session Stream

When the stream handler detects process exit, it updates `activity_state` in the DB before sending the `done` SSE event. This ensures the global events endpoint reflects the new state immediately.

## Scope

### In Scope
- New `activity_state` DB column + migration
- `UpdateActivityState()` DB method
- Lifecycle transitions in managed session handlers
- Frontend `sessionStatus()` update for managed sessions
- Pulse animation CSS for working state

### Out of Scope
- Hook-mode activity state tracking (would require protocol changes)
- iOS app UI changes (it will receive the data; UI updates are separate)
- Per-session SSE event for state changes (global polling is sufficient at 3s interval)

## Files to Modify

1. `server/db/db.go` — Add migration for `activity_state` column
2. `server/db/sessions.go` — Add `ActivityState` field to struct, add `UpdateActivityState()` method
3. `server/api/managed_sessions.go` — Set activity state at process start/exit
4. `server/web/static/app.js` — Update `sessionStatus()` for managed sessions
5. `server/web/static/style.css` — Add pulse animation for working state
