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

- At start of `handleSendMessage`, call `ResetTurnCount(sessionID)` to reset for the new message
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

Track `lastTurnThreshold` per session in Alpine.js state to prevent duplicate toasts. Reset when selecting a different session or when turn count resets to 0.

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
