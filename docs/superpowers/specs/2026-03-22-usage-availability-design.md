# Session Usage & Availability — Design Spec

**Issue:** #29 — Claude usage and availability
**Date:** 2026-03-22

## Problem

Managed sessions have configurable turn limits (default 50) and budget caps (default $5). When a session hits its turn limit, the server logs `session X hit turn limit (50), interrupting` and sends SIGINT — but the user has no visibility into this in the web UI. They only discover the limit after it's been hit.

## Solution

Add a usage panel to the bottom of the left sidebar showing real-time turn count for the selected managed session, with color-coded progress bars and toast notifications at configurable thresholds.

## Scope

- **In scope:** Turn count tracking, sidebar usage panel, threshold toasts
- **Out of scope:** Budget tracking (Claude CLI doesn't expose real-time spend in NDJSON), aggregate cross-session usage, Anthropic API rate limits

## Backend Changes

### DB Schema

Add `turn_count` column to sessions table:

```sql
ALTER TABLE sessions ADD COLUMN turn_count INTEGER NOT NULL DEFAULT 0;
```

Add to Session struct:
```go
TurnCount int `json:"turn_count"`
```

### New DB Methods (`db/sessions.go`)

- `IncrementTurnCount(sessionID string) (int, error)` — atomically increments turn_count, returns new value
- `ResetTurnCount(sessionID string) error` — sets turn_count to 0

### Managed Sessions (`api/managed_sessions.go`)

- At start of `handleSendMessage`, call `ResetTurnCount(sessionID)` to reset for the new message. This matches the existing behavior where `turnCount` is a local variable scoped per `handleSendMessage` call — it tracks turns within a single user-message exchange, not cumulative across the session.
- Replace local `turnCount` variable with DB-backed `IncrementTurnCount` call on each assistant message
- The turn limit check uses the returned count from `IncrementTurnCount`

### SSE (`api/events.go`)

No changes needed. The `/api/events` endpoint already sends the full session list (including all struct fields) every 3 seconds. Since `turn_count` is added to the Session struct with a `json:"turn_count"` tag, it's automatically included in the SSE payload.

## Frontend Changes

### Usage Panel (`index.html`)

Add a `<div class="usage-panel">` pinned below the `.session-list` in the left sidebar. Visible only when a managed session is selected.

Layout:
```
┌─────────────────────┐
│ Turns    12 / 50    │
│ ████████░░░░░░░░░░  │
└─────────────────────┘
```

The panel sits between the session list and the bottom of the sidebar, separated by a border-top.

**Mobile:** The left sidebar is hidden on mobile (replaced by a `<select>` dropdown). The usage panel will only be visible on desktop. Mobile usage visibility is out of scope for this iteration.

### Progress Bar Colors

Based on percentage of `turn_count / max_turns`:

| Range | Color | CSS Variable |
|-------|-------|-------------|
| 0–79% | Blue | `var(--accent)` |
| 80–89% | Yellow/amber | `#f59e0b` |
| 90–100% | Red | `#ef4444` |

### Toast Notifications (`app.js`)

Enhance existing `toast(msg, duration)` to accept a type parameter: `toast(msg, duration, type)`.

Types:
- `'info'` (default) — existing dark background
- `'warning'` — amber/yellow background
- `'error'` — red background

Threshold triggers (fire once per threshold crossing):

| Threshold | Type | Message |
|-----------|------|---------|
| 80% | warning | "Turn limit warning (40/50) — approaching session limit" |
| 90% | error | "Turn limit critical (45/50) — session will be interrupted soon" |
| 100% | error | "Session interrupted — turn limit reached (50/50)" |

Track `lastTurnThreshold` per session in Alpine.js state to prevent duplicate toasts. Reset when selecting a different session or when turn count resets to 0. This state is ephemeral — on page refresh, thresholds may re-fire if the session is still above a threshold. This is acceptable since it serves as a reminder.

Add `toastType` to Alpine.js state (default `'info'`). Update the toast HTML element to apply the type as a CSS class: `:class="{ visible: showToast, warning: toastType === 'warning', error: toastType === 'error' }"`.

### Toast Styling (`style.css`)

Add modifier classes:

```css
.toast.warning { background: #f59e0b; color: #000; }
.toast.error { background: #ef4444; color: #fff; }
```

### SSE Update Handler (`app.js`)

In the existing SSE "update" event handler that processes sessions, after updating session data:

1. Find the selected session
2. If it's managed, compute `turnPercent = turn_count / max_turns * 100`
3. Determine threshold bucket (0, 80, 90, 100)
4. If threshold > lastTurnThreshold, fire appropriate toast
5. Update `lastTurnThreshold`

**Edge cases:**
- **SSE race on reset:** When `ResetTurnCount` is called at the start of a new message, SSE may briefly deliver `0/50` before assistant messages arrive. The frontend should only update the usage display when the session status is "running" and `turn_count > 0`, or when status transitions to "idle".
- **100% toast reliability:** The 3-second SSE polling interval means the frontend may not observe the exact 100% value if the session completes between polls. The 100% toast is best-effort. The session transitioning to "idle" after SIGINT is the definitive signal.
- **Resume sessions:** `ResumeSession` in `db/sessions.go` should also reset `turn_count` to 0, consistent with how it resets `initialized` and deletes messages.

## Data Flow

```
Claude CLI stdout → NDJSON stream → managed_sessions.go (count assistant msgs)
    → IncrementTurnCount (DB write) → /api/events SSE (reads sessions from DB)
    → Browser SSE handler → updates usage panel + checks thresholds → toast if needed
```

## Testing

- **DB tests:** Test IncrementTurnCount returns correct count, ResetTurnCount resets to 0
- **API tests:** Test that turn_count appears in session JSON, increments during message streaming
- **Manual:** Create a managed session with low max_turns (e.g., 5), send messages, verify panel updates and toasts fire at thresholds
