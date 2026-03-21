# Activity Status Pills Design Spec

## Overview

Real-time activity indicators in the managed session chat UI that show what Claude is currently doing — thinking, running tools, idle, stalled, or disconnected. Activity states are displayed as compact inline pills that stack in the chat flow, visually distinct from actual message bubbles.

## Motivation

When using managed sessions remotely, users have no visibility into whether Claude is actively thinking, running a long tool, waiting for something, or if the service has hung. The existing `running`/`idle` status is too coarse. Users need granular, real-time feedback to know if things are progressing normally.

## Scope

- **Web UI only** — managed sessions
- Not hook mode, not iOS app

## Architecture

Two components:

1. **Client-side state machine** — derives activity state from NDJSON events already flowing via the per-session SSE stream (`/api/sessions/{id}/stream`)
2. **Server-side heartbeat** — lightweight periodic SSE event to let the client distinguish "Claude thinking quietly" from "connection dropped"

## NDJSON Event Format

Claude CLI emits NDJSON (newline-delimited JSON) lines to stdout. Each line has a top-level `type` field. The key types relevant to activity tracking:

- **`assistant`** — Contains a `message.content` array. Each content block has its own `type`:
  - `text` — Claude is writing text (actual response content)
  - `tool_use` — Claude is invoking a tool. Has `name` and `input` fields.
- **`tool_result`** — Result of a tool execution (top-level event type)
- **`result`** — Final result of the turn (top-level event type)
- **`done`** — Synthetic event sent by the Go server when the process exits (not from Claude CLI)

### Example NDJSON sequence and expected pills

```
User sends message →
  [no events yet, show: "Thinking..."]

Receive: {"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/server/api/handler.go"}}]}}
  [pill: ● "Read /server/api/handler.go"]

Receive: {"type":"tool_result","content":"file contents..."}
  [pill: ✓ "Read /server/api/handler.go (2s)"]
  [pill: ● "Thinking..."]

Receive: {"type":"assistant","message":{"content":[{"type":"text","text":"I can see the handler..."}]}}
  [pill: ✓ "Thinking... (1s)"]
  — text content rendered as normal chat message —

Receive: {"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/server/api/handler.go"}}]}}
  [pill: ● "Edit /server/api/handler.go"]

Receive: {"type":"tool_result","content":"..."}
  [pill: ✓ "Edit /server/api/handler.go (3s)"]
  [pill: ● "Thinking..."]

Receive: {"type":"result",...}
  — OR —
Receive SSE event type "done"
  [clear all pills, transition to idle]
```

## Activity State Machine

The client tracks activity state by parsing NDJSON events. Because `tool_use` is nested inside `assistant` events (not a top-level type), the client must inspect `message.content` blocks:

| Condition | Activity State | Display |
|---|---|---|
| Message sent, no events yet | Thinking | "Thinking..." |
| `assistant` event with `tool_use` content block | Tool running | "{tool_name} {context}" |
| `assistant` event with only `text` content blocks | Writing | (no pill — text renders as chat message) |
| `tool_result` event (top-level) | Thinking | Previous tool pill → completed; new "Thinking..." pill |
| `result` event OR `done` SSE event | Done | Clear all pills |
| No NDJSON event for 60s+ | Stale | Current pill turns amber: "{last activity} — may be stalled" |
| No heartbeat for 30s+ | Disconnected | Red pill: "Connection lost — server may be down" |

**Key distinction:** An `assistant` event with `text` content does NOT show a "Thinking" pill — it means Claude is actively writing a response, which renders as a normal chat message. A "Thinking" pill only appears (a) when a message is first sent with no response yet, or (b) after a `tool_result` while waiting for the next `assistant` event.

### State transitions

- Each new NDJSON event resets the staleness timer (60s countdown)
- Each heartbeat resets the heartbeat timer (30s countdown)
- On `result` or `done` event, all activity pills are cleared
- The staleness timer only runs while the session status is `running`
- The heartbeat timer runs whenever an SSE connection is open

### Tool name extraction

The `tool_use` content block contains a `name` field (e.g., "Read", "Bash", "Edit") and an `input` object. Extract context from `input`:
- **Read/Edit:** `input.file_path` → show file name
- **Bash:** `input.command` → show first ~30 chars of command
- **Grep:** `input.pattern` → show pattern
- **Other tools:** show tool name only

Truncate the full pill text to ~40 chars.

### Stacking limits

Show at most **10 completed pills** plus the current active pill. If more than 10 tools have run, older completed pills are dropped from the display. This prevents visual overload during long tool-heavy turns.

## UI: Stacking Inline Pills

### Visual design

Activity pills are compact, right-aligned elements in the chat flow — visually distinct from message bubbles:

- **Size:** Smaller font (11px), rounded pill shape (border-radius: 12px), compact padding
- **Position:** Right-aligned (same side as Claude's messages), stacking vertically
- **Not persisted:** Pills are ephemeral JS-only state, not saved to the messages table

### Three visual states

**Active (current activity):**
- Green-tinted background (`#1a2a1a`), green border (`#3a4a3a`), green text (`#7cb87c`)
- Pulsing dot indicator before the text
- Shows tool name and optional context

**Completed (past activity):**
- Gray background (`#2a2a3a`), subtle border (`#3a3a4a`), muted text (`#6a7a8a`)
- Checkmark icon before the text
- Shows duration (e.g., "3s", "12s")

**Stale (no events for 60s+):**
- Amber background (`#2a1a0a`), amber border (`#5a4a2a`), amber text (`#c8a040`)
- Warning triangle icon
- Appends "— may be stalled" with elapsed time

**Connection lost (no heartbeat for 30s+):**
- Red background (`#2a0a0a`), red border (`#5a2a2a`), red text (`#c05050`)
- X icon
- Text: "Connection lost — server may be down"

### Lifecycle

1. When a message is sent, show an initial "Thinking..." pill immediately
2. As NDJSON events arrive, add/update pills based on the state machine table
3. `assistant` events with `tool_use` blocks → new active tool pill (previous pill → completed with duration)
4. `assistant` events with `text` blocks → no pill (text renders as chat message)
5. `tool_result` events → mark previous tool pill as completed, show new "Thinking..." pill
6. On `result` or `done` event, clear all pills
7. If the user sends another message, the cycle repeats
8. Max 10 completed pills visible — older ones dropped from display

## Server Heartbeat

### Implementation

The heartbeat flows through the existing Broadcaster as a special JSON message — no changes to the SSE event format needed. The client distinguishes heartbeats by parsing the `type` field, same as it does for all other NDJSON events.

**Heartbeat message (sent through Broadcaster):**
```json
{"type":"heartbeat","ts":1679000000000}
```

Timestamp is Unix milliseconds (matches JavaScript's `Date.now()`).

The client uses heartbeats to:
- Confirm the SSE connection is alive
- Reset its heartbeat timeout (30s)
- Distinguish "Claude is thinking (heartbeats arriving, no NDJSON)" from "connection dead (no heartbeats)"

### Where to add the ticker

In `StreamNDJSON` in `server/managed/stream.go`, add a 15-second `time.Ticker` that runs alongside the existing STDOUT line reader (in a select loop or separate goroutine). The ticker sends the heartbeat JSON through the Broadcaster via `b.Send()`. The ticker stops when the process exits.

**Important:** The ticker lives in `StreamNDJSON`, not in `handleSessionStream`. This ensures one ticker per process (not one per connected browser tab), since `StreamNDJSON` is called once per process while `handleSessionStream` is called once per SSE client.

## Files to Modify

- `server/web/static/app.js` — Activity state machine, pill rendering, staleness/heartbeat timers
- `server/web/static/style.css` — Pill styles (active, completed, stale, connection-lost states + pulse animation)
- `server/web/static/index.html` — Pill container element in the chat area template
- `server/managed/stream.go` — Heartbeat ticker in `StreamNDJSON`
- `server/api/managed_sessions.go` — Pass heartbeat events through SSE to clients

## Design Decisions

### Client-side state derivation vs server-side tracking
Client derives state from NDJSON events it already receives — zero server changes for the core feature. The server only adds a lightweight heartbeat. This avoids coupling the server to NDJSON format changes and keeps the server simple.

### Stacking pills vs single updating pill
Stacking provides a visible trail of activity. Users can see what Claude has done, not just what it's doing now. This gives better context for understanding progress.

### Inline pills vs message bubbles
Pills are visually distinct from messages (smaller, muted, pill-shaped) to prevent confusion about what is a "real" message from Claude vs. a status indicator.

### Staleness threshold: 60 seconds
Most NDJSON events arrive within seconds. A 60-second gap is unusual and worth flagging. This avoids false positives during normal tool execution (e.g., a long Bash command) while still catching genuine stalls.

### Heartbeat interval: 15 seconds, timeout: 30 seconds
15s is frequent enough to detect connection loss quickly. 30s timeout (2 missed heartbeats) avoids false alarms from network jitter.

### Pills not persisted
Activity pills are transient UI state. Persisting them would add unnecessary DB writes and complicate the messages table. If the user reloads the page mid-execution, they just see the SSE reconnect and pills start fresh.

## Out of Scope

- Hook mode sessions
- iOS app
- Persisting pills to database
- Customizable thresholds
- Tool progress (e.g., "Bash: 50% complete")
- Sound/notification on staleness
