# Shell Access for Managed Sessions — Design Spec

**Date:** 2026-03-23
**Issue:** #16 — Add shell access and restrict to current app location

## Problem

Developers using the web UI sometimes need to run shell commands in the project directory — checking git status, running builds, listing files, etc. Currently this requires switching to a terminal. Shell access from the UI would eliminate this context switch.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Scope | Tied to managed sessions | Inherits session CWD; no standalone shell needed |
| Restrictions | None (API key is the security boundary) | Claude already has full tool access; restricting shell commands provides a false sense of security |
| Output | Real-time streaming via SSE | Reuses existing Broadcaster infrastructure; covers all command durations |
| UI placement | Inline in chat | Shell output interleaves with Claude messages; single unified stream |
| Timeout | Default 30s, max 300s, configurable per-command | Prevents runaway processes |

## Architecture

### Approach: Extend the Managed Session Manager

Add shell execution as a new capability of the existing `Manager` in `server/managed/`. This reuses the battle-tested process lifecycle, Broadcaster, and SSE streaming infrastructure. No new packages needed.

## API

### Execute Shell Command

```
POST /api/sessions/{id}/shell
```

**Request:**
```json
{
  "command": "npm install",
  "timeout": 30
}
```

- `command` (required): Shell command string, executed via `sh -c "<command>"`
- `timeout` (optional): Seconds before graceful shutdown. Default 30, max 300.

**Validation:**
- Session must exist
- Session must be `mode=managed`
- `m.IsRunning(sessionID)` must be false (authoritative check via in-memory `procs` map, not DB status)
- `command` must not be empty

**Response (200):**
```json
{
  "id": "uuid-of-shell-command"
}
```

The command ID is used to correlate streamed output events.

**Error responses:**
- `404` — session not found
- `400` — empty command, invalid timeout, or session is hook-mode
- `409` — session is currently running (Claude is active)

## Process Lifecycle

### Manager Extension

New method on `Manager`:

```go
type ShellOpts struct {
    Command string
    CWD     string
    Timeout time.Duration
}

func (m *Manager) SpawnShell(sessionID string, opts ShellOpts) (*Process, error)
```

**Behavior:**
1. Check `m.IsRunning(sessionID)` — reject if any process (Claude or shell) is active
2. Build command: `exec.Command("sh", "-c", opts.Command)`
3. Set `cmd.Dir = opts.CWD` (handler reads `sess.CWD` from DB and passes it here)
4. Set `cmd.Stdin = nil` (no stdin — commands that read stdin fail fast rather than hang)
5. Set `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` (create process group for clean kill)
6. Capture stdout and stderr as separate pipes
7. **Register in `procs` map** — track the shell process so `IsRunning()` returns true
8. Set session status to `"running"` in DB (UI shows session as busy)
9. Start process
10. Launch background goroutine that:
    - Reads stdout/stderr line by line
    - Broadcasts each line through the session's existing Broadcaster
    - Waits for process exit
    - Broadcasts exit event
    - Removes process from `procs` map
    - Resets session status to `"idle"` in DB
11. Start timeout goroutine:
    - After `opts.Timeout`, send SIGINT to the process group
    - Wait 5 seconds grace period
    - If still running, send SIGKILL to the process group

**Key difference from Claude spawning:** Shell processes are short-lived one-shot commands, not long-running interactive sessions. However, they **must** be tracked in the `procs` map to participate in the same mutual exclusion mechanism — this prevents Claude messages and shell commands from running concurrently on the same Broadcaster.

**Concurrency guard:** The handler calls `m.IsRunning(sessionID)` as the authoritative check (not the DB status field, which can have race windows). This prevents conflicts with an active Claude process or another shell command using the same Broadcaster. Concurrent shell commands on the same session are rejected (return 409).

## Streaming Format

Shell output is broadcast as JSON through the session's existing SSE channel (`GET /api/sessions/{id}/stream`). The UI already subscribes to this channel for Claude output.

### Event Types

```json
{"type": "shell_start", "command": "npm install", "id": "uuid", "cwd": "/path/to/project"}
```
Sent immediately when the command starts. UI uses this to render the command header.

```json
{"type": "shell_output", "text": "added 150 packages in 4s\n", "stream": "stdout", "id": "uuid"}
{"type": "shell_output", "text": "npm WARN deprecated\n", "stream": "stderr", "id": "uuid"}
```
Sent for each line of output. `stream` field distinguishes stdout from stderr.

```json
{"type": "shell_exit", "code": 0, "id": "uuid"}
```
Sent when the command completes. `code` is the exit code (0 = success). If killed by timeout, `code` will be -1 and an additional field `"timeout": true` is included.

### Done Event

The existing `{"type": "done"}` event is NOT sent after shell commands — it's reserved for Claude process completion. The `shell_exit` event serves the same purpose for shell commands.

## Persistence

Shell commands and output are stored in the existing `messages` table. No schema changes needed.

### Command Message

```
role: "shell"
content: "npm install"
```

Stored when the command is submitted (before execution starts).

### Output Message

```
role: "shell_output"
content: '{"stdout": "added 150 packages in 4s\n...", "stderr": "npm WARN deprecated...", "exit_code": 0, "timed_out": false}'
```

Stored when the command completes. Contains the full accumulated stdout, stderr, exit code, and timeout status as a JSON string. Output is capped at 1MB per stream (stdout/stderr) — truncated with a `[truncated]` marker if exceeded.

This means `GET /api/sessions/{id}/messages` returns shell history alongside Claude messages — no new endpoints needed for history. **Important:** The message filter in `fetchManagedMessages` in `app.js` must be updated to include `shell` and `shell_output` roles so they render on page reload.

## UI Changes

### Input Mode Toggle

The existing chat input gets a mode toggle:
- A `$` button (or similar indicator) next to the input field toggles between "chat mode" and "shell mode"
- In shell mode, the input has a monospace font and `$` prefix visual indicator
- Pressing Enter in shell mode posts to `POST /api/sessions/{id}/shell`
- Pressing Enter in chat mode posts to `POST /api/sessions/{id}/message` (existing behavior)

### Shell Message Rendering

Shell messages in the chat stream render with terminal styling:
- **Command header:** Dark background, monospace font, shows `$ command` with a terminal icon
- **stdout:** White/light text on dark background, monospace
- **stderr:** Orange/red text on dark background, monospace
- **Exit code:** Shown as a badge — green for 0, red for non-zero
- **Timeout indicator:** If timed out, show a warning badge

### SSE Handling

The existing `sessionSSE.onmessage` handler in `app.js` is extended to handle the new event types:
- `shell_start` → Add a shell command block to `chatMessages`
- `shell_output` → Append text to the current shell block (distinguish stdout/stderr by color)
- `shell_exit` → Mark the shell block as complete, show exit code badge

### State

New Alpine.js state:
```javascript
shellMode: false,         // Toggle between chat and shell input
activeShellId: null,      // Currently streaming shell command ID
```

## Security

### Threat Model

The security boundary is the API key, which is already the sole authentication mechanism for all features including:
- Sending messages to Claude (which has access to Bash, file editing, etc.)
- Reading/writing files via the browse endpoint
- Creating and managing sessions

Shell access does not expand the attack surface — an attacker with the API key can already execute arbitrary code through Claude. Shell access just makes it direct.

### Mitigations

| Threat | Mitigation |
|--------|------------|
| Runaway process | Server-side timeout: SIGINT → 5s grace → SIGKILL on process group (default 30s, max 300s) |
| Output flooding | Cap broadcast buffer; drop lines if subscriber is slow (existing Broadcaster behavior) |
| Directory escape | Commands execute in session CWD via `cmd.Dir`; no chroot (same as Claude) |
| Concurrent conflicts | Reject shell commands while Claude is running (409) |

## Files to Modify

| File | Change |
|------|--------|
| `server/managed/manager.go` | Add `SpawnShell` method |
| `server/api/router.go` | Add `POST /api/sessions/{id}/shell` route |
| `server/api/managed_sessions.go` | Add `handleShellExecute` handler |
| `server/db/messages.go` | No changes needed (reuse existing `CreateMessage`) |
| `server/web/static/app.js` | Add shell mode toggle, shell message rendering, SSE handler extensions |
| `server/web/static/style.css` | Add terminal-styled shell message CSS |
| `server/web/static/index.html` | Add shell mode toggle button and shell message templates |

## Out of Scope

- Interactive/PTY shells (no `vim`, `top`, etc.)
- Shell history search (up-arrow in input)
- Tab completion
- Multiple concurrent shell commands per session
- Environment variable customization per command
- Shell access for hook-mode sessions
