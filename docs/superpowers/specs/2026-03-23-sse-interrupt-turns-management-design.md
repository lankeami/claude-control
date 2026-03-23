# SSE Interrupt for Turns Management

**Date:** 2026-03-23
**Issue:** #36
**Status:** Design

## Problem

Managed sessions using the Superpowers plugin consume many turns internally (tool calls, thinking, etc.). When `turn_count` reaches `max_turns`, the server sends SIGINT and Claude stops. The user must manually intervene to resume progress. This creates a poor experience for autonomous workflows.

## Solution

Server-side auto-continue: when turns approach the limit, the server interrupts Claude, then automatically resumes with a continuation prompt. The turn counter resets on each re-spawn, allowing Claude to keep working across multiple continuation cycles.

## Terminology

**`turn_count`** counts assistant message events in the NDJSON stream (each line with `type === "assistant"`), not logical conversation turns. A single logical turn with multiple tool calls produces multiple assistant messages. The threshold calculation and `max_turns` value are calibrated to this counting behavior.

## Data Model

Two new columns on the `sessions` table:

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `auto_continue_threshold` | REAL | 0.8 | Fraction of `max_turns` at which to trigger auto-continue (0.0–1.0) |
| `max_continuations` | INTEGER | 5 | Max auto-resumes per user message before requiring manual intervention |

**Validation:** `auto_continue_threshold` must be in range (0.0, 1.0]. A value of 0.0 or negative is invalid (would trigger immediately). A value of 1.0 means auto-continue triggers at exactly `max_turns`, effectively replacing the existing hard-limit behavior.

`continuation_count` is tracked in-memory within the streaming goroutine — not persisted. It resets with each user-initiated `POST /message`.

## Auto-Continue Flow

The auto-continue logic **replaces** the existing hard-limit SIGINT in `handleSendMessage`. The current `if count >= sess.MaxTurns` block is removed; the threshold-based auto-continue subsumes it.

```
1. User sends POST /message
   → ResetTurnCount(), continuation_count = 0

2. Spawn `claude -p` (with --resume if initialized)

3. Stream NDJSON, count assistant messages
   → Session status remains "running" throughout the entire loop

4. On each assistant message:
   a. IncrementTurnCount()
   b. If turn_count >= floor(auto_continue_threshold * max_turns):
      i.   Set autoInterrupting flag (goroutine-local variable)
      ii.  Send SIGINT to process via Manager.Interrupt()
      iii. Wait for process to exit (<-proc.Done)
      iv.  Clear autoInterrupting flag
      v.   Increment continuation_count
      vi.  If continuation_count > max_continuations:
           → Broadcast SSE: {"type":"auto_continue_exhausted","continuation_count":N}
           → Persist message with role "system": "Auto-continue limit reached (N/N)"
           → Break loop, session goes idle
      vii. Broadcast SSE: {"type":"auto_continuing","continuation_count":N,"max_continuations":M}
      viii.Persist message with role "system": "Auto-continuing (N/M)..."
      ix.  ResetTurnCount()
      x.   Spawn new `claude -p --resume` with prompt:
           "You were interrupted due to turn limits. Continue where you left off."
      xi.  Continue streaming from step 3

5. If process exits naturally (exit code 0):
   → Done, no auto-continue needed
```

### Key implementation details

- **`autoInterrupting` is goroutine-local**: It is a plain `bool` variable inside the streaming goroutine, not a shared field on `Process` or `Manager`. Set before SIGINT, checked after process exits.
- **`done` SSE event suppression**: The `{"type":"done","exit_code":N}` event must only be sent when the auto-continue loop terminates (natural exit, exhaustion, manual interrupt, or error) — not after each intermediate process exit. The web UI's `done` handler calls `stopSessionSSE()`, so sending it prematurely would disconnect the client mid-loop.
- **Session status stays `"running"`**: Throughout the auto-continue loop, the session status remains `"running"` and only transitions to `"idle"` when the loop terminates. This prevents the UI from briefly showing the session as idle between cycles.
- **Re-spawn safety**: `proc.Done` is closed after the process is removed from `m.procs` (current code: `delete` then `close(proc.Done)`), so calling `Spawn` after `<-proc.Done` is safe — no race with the cleanup goroutine.

## SSE Events

Two new event types sent through the existing per-session SSE stream:

| Event | Payload | When |
|-------|---------|------|
| `auto_continuing` | `{"type":"auto_continuing","continuation_count":N,"max_continuations":M}` | After threshold SIGINT, before re-spawn |
| `auto_continue_exhausted` | `{"type":"auto_continue_exhausted","continuation_count":N}` | When `max_continuations` reached |

## Web UI Behavior

- **`auto_continuing` event**: Render a system message in chat: "Auto-continuing (2/5)..." with role `"system"`. Usage panel resets the turn progress bar.
- **`auto_continue_exhausted` event**: Render system message: "Auto-continue limit reached. Send a message to continue manually." Session transitions to idle.
- Existing interrupt button still works — manual SIGINT breaks the auto-continue loop.

## Edge Cases

### Manual interrupt during auto-continue
The streaming goroutine checks the `autoInterrupting` flag after the process exits. If the process was SIGINT'd without the flag being set, it was a manual interrupt (via `POST /interrupt`). In that case, break the loop and go idle. Since the flag is goroutine-local and only the streaming goroutine sets it, there is no race condition.

### Claude finishes before threshold
Process exits with code 0 naturally. No auto-continue needed, loop ends cleanly. This is the happy path.

### Non-zero exit (error)
Don't auto-continue on errors. Only auto-continue when the exit was caused by the server's threshold SIGINT (i.e., `autoInterrupting` was true when the process exited).

### Minimum progress guard
If the process completes fewer than 2 assistant turns before hitting the threshold again, treat it as "not making progress" and stop the auto-continue loop. This prevents tight loops when `max_turns` is very small or the threshold is misconfigured.

### SSE client disconnects
The goroutine detects context cancellation and stops spawning new processes. Clean shutdown.

## Configuration

No changes to `POST /api/sessions/create` request shape — the new fields use defaults. They can be exposed in the session creation UI later if needed.

The defaults (80% threshold, 5 max continuations) work with the existing `max_turns` default of 50, meaning auto-continue triggers at assistant message 40 and allows up to 5 cycles (effectively 200 assistant messages per user message before requiring manual intervention).

## Global SSE Integration

The existing `GET /api/events` endpoint (3-second broadcast) already includes `turn_count` per session. The global broadcast will also include `continuation_count` (read from an in-memory field on the manager) so the session list can show "Auto-continuing 2/5" status. The UI will see `turn_count` reset to 0 on each auto-continue cycle, and the usage panel will update accordingly.
