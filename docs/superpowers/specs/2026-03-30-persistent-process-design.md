# Persistent Process for Managed Sessions

**Date:** 2026-03-30
**Status:** Approved

## Problem

The current managed mode spawns a new `claude -p` process for every user turn. This causes:

1. **Startup latency** — each turn pays the cost of loading Node.js, reading configs, and authenticating
2. **Resource waste** — each process loads the full Claude Code runtime into memory, multiplied across concurrent sessions
3. **Fragile context handoff** — `--resume` between separate processes occasionally loses state or fails

## Solution

Replace per-turn process spawning with a **single long-lived process per session** using `--input-format stream-json`. Turns are sent as JSON lines to stdin. The process stays warm between turns.

### Current Flow

```
User message → Spawn `claude -p --resume <id> "prompt"` → Read stdout → Process exits
User message → Spawn `claude -p --resume <id> "prompt"` → Read stdout → Process exits
```

### New Flow

```
Session created → Spawn `claude -p --output-format stream-json --input-format stream-json`
User message → Write JSON to stdin → Read NDJSON from stdout until `result` message
User message → Write JSON to stdin → Read NDJSON from stdout until `result` message
...
Session ends → Close stdin → Process exits
```

## Process Lifecycle

### States

- **idle** — No process (new session, process exited/crashed, or idle timeout)
- **working** — Process alive, currently processing a turn
- **waiting** — Process alive, idle between turns, stdin open

These map directly to the existing `activity_state` field in the database. The `waiting` state now means the process is genuinely warm and ready, not just "exited cleanly."

### State Transitions

| Event | From | To | Action |
|---|---|---|---|
| First message | idle | working | Spawn new process (no `--resume`) |
| Subsequent message, process warm | waiting | working | Write user JSON to stdin |
| Subsequent message, process dead | idle | working | Spawn with `--resume <session_id>` |
| Turn completes (`result` message) | working | waiting | Update `LastActivity` timestamp |
| Idle timeout (30 min default) | waiting | idle | Close stdin, let process exit |
| Process crashes | working/waiting | idle | Detect via goroutine, log error |
| Server restart | any | idle | Reset stale states (existing behavior) |

### Recovery

`--resume` becomes a **recovery mechanism** rather than the primary turn-handling strategy. It is only used when:

- Process died unexpectedly
- Process was reaped by idle timeout
- Server restarted

The happy path (user actively chatting) never spawns after the first message.

## Stdin/Stdout Protocol

### Input (Go writes to Claude's stdin)

Each user turn is a single JSON line:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Fix the login bug"}]}}
```

Requires `-p --input-format stream-json --output-format stream-json`.

### Output (Go reads from Claude's stdout)

NDJSON stream, same format as current `--output-format stream-json`:

1. `{"type":"init", ...}` — session metadata (first turn only)
2. Assistant messages — content blocks, tool uses, partial results
3. `{"type":"result", "session_id":"...", "cost_usd":..., ...}` — **turn boundary**

The `result` message signals end-of-turn. Unlike today where EOF signals completion (process exits), the stream stays open. The Go reader must use `result` as the delimiter to know when a turn is done.

## Changes to `manager.go`

### New Process Struct Fields

```go
type Process struct {
    Cmd          *exec.Cmd
    Stdin        io.WriteCloser   // NEW: feed turns
    Stdout       io.ReadCloser
    Stderr       io.ReadCloser
    Done         chan struct{}
    ExitCode     int
    TimedOut     bool
    LastActivity time.Time         // NEW: for idle timeout
}
```

### New Manager Methods

**`EnsureProcess(sessionID, opts)`** — Returns an existing warm process or spawns a new one.

- If a process exists for the session and is alive, return it
- If no process exists, spawn with base args: `-p --output-format stream-json --input-format stream-json`
- If the session has a `claude_session_id` (previous context), add `--resume <id>` to recover
- Start the stdout reader goroutine and idle timeout goroutine

**`SendTurn(sessionID, message)`** — Writes a user message JSON line to the process's stdin.

- Calls `EnsureProcess` first
- Marshals the message to JSON, writes to stdin with trailing newline
- Updates `LastActivity`
- Returns immediately (caller reads output via Broadcaster/SSE as today)

### Idle Timeout

Per-process goroutine that checks `LastActivity`. After a configurable duration (default 30 minutes), closes stdin to let the process exit gracefully. The `Done` channel fires, cleanup runs, and the next `EnsureProcess` call spawns fresh with `--resume`.

### Stdout Reader Changes

The existing NDJSON reader (`stream.go`) reads lines and broadcasts them. Currently it reads until EOF. The change:

- Keep reading lines and broadcasting as today
- When a `result` message is seen, broadcast it AND signal "turn complete" (so the API handler knows the turn is done)
- Do NOT stop reading — keep the goroutine alive for the next turn
- Only exit the reader when the process actually exits (EOF on stdout)

## What This Does Not Solve

- **Slash commands** (`/login`, `/config`) — still REPL-only. `stream-json` input is still headless mode.
- **Permission prompts** — still require `--dangerously-skip-permissions` or `--permission-prompt-tool`
- **Interactive tool approval** — same constraint as today

These would require Approach 3 (Node.js SDK bridge) and are out of scope for this change.

## Configuration

New settings (added to existing config):

| Setting | Default | Description |
|---|---|---|
| `idle_timeout_minutes` | 30 | Kill warm process after this many minutes of inactivity |

## Migration

- Existing sessions with `claude_session_id` values continue to work — `--resume` is used when spawning a fresh process for a session that has prior context
- No database schema changes
- No API changes — SSE streaming works the same, messages persist the same way
- The change is entirely within `managed/manager.go` and `managed/stream.go`
