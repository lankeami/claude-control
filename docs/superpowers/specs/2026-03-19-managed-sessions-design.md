# Managed Sessions — Direct CLI Control of Claude Code

**Date:** 2026-03-19
**Status:** Draft
**Replaces:** Hook-based instruction queueing (phased migration)

## Problem

The current hook-based architecture has fundamental limitations:

1. **Instructions rarely deliver** — once a hook times out, queued instructions are lost
2. **No mid-turn interrupts** — you must wait for Claude to finish before interacting
3. **No session spawning** — Claude Code must already be running for hooks to fire
4. **Complexity** — long-polling, infinite loop guards, graceful degradation add fragility

## Solution

The Go server spawns `claude -p` as a child process for each message. No PTY, no raw terminal — structured JSON protocol over stdout. The server manages session lifecycle, streams output to the web UI via SSE, and supports interrupt via SIGINT.

## Architecture

### Session Lifecycle

**Spawning:** The web UI sends a "create session" request with a `cwd` (project directory). The server creates a session record in SQLite with a UUID. No process is spawned yet.

**Sending a message — first message:**

```
<CLAUDE_BIN> <CLAUDE_ARGS> -p "<message>" --session-id <uuid> --output-format stream-json \
  --allowedTools <tools> --max-budget-usd <budget>
```

**Sending a message — subsequent messages:**

```
<CLAUDE_BIN> <CLAUDE_ARGS> -p "<message>" --resume <uuid> --output-format stream-json \
  --allowedTools <tools> --max-budget-usd <budget>
```

The server tracks whether a session has had its first message sent (`initialized` field). First message uses `--session-id <uuid>` to create the Claude Code session with that specific UUID. Subsequent messages use `--resume <uuid>` to continue it.

The server uses Go's `exec.Command` directly — no shell interpolation. The message is passed as a single argument to `-p`. The child process `cmd.Dir` is set to the session's `cwd` so Claude Code operates in the correct project directory.

The server reads NDJSON from stdout line-by-line, persists each line to the `messages` table, and forwards to connected SSE clients. Stderr is captured and logged server-side; if the process exits non-zero, the last stderr output is surfaced as a `system` event in the SSE stream.

When the process exits, the turn is complete. The exit code is persisted and included in the SSE `done` event.

**Key insight:** Each message is a separate process. `--resume` handles context continuity — Claude Code persists conversations in its own session store. The Go server does not maintain long-lived processes between turns.

**Interrupting:** SIGINT to the child process. Claude Code handles this gracefully — saves state and exits. The session can be resumed on the next message. From the web UI, this is a "Stop" button.

**Turn limiting:** `--max-turns` does not exist in the CLI. The server implements turn counting by tracking `assistant` messages in the NDJSON stream. When the count hits the session's configured limit, the server sends SIGINT. `--max-budget-usd` provides a cost safety net.

**Compacting:** Send `/compact` as a message via `POST /message`. The server detects slash command prefixes and passes them through to `claude -p`.

**Tearing down:** If a process is currently running, send SIGINT and wait for exit (5s timeout, then SIGKILL). Then remove the session record from SQLite.

### Streaming Output to Web UI

**Endpoint:** `GET /api/sessions/:id/stream`

The web UI opens an SSE connection. While a `claude -p` process is running, the server pipes NDJSON stdout as SSE `data:` frames. On process exit, sends a `done` event with the exit code.

**Reconnection:** The server persists all streamed messages to SQLite. On reconnect, the web UI fetches message history via `GET /api/sessions/:id/messages`, then opens SSE for live updates.

**Fan-out:** Multiple browser tabs on the same session share one stdout pipe — the server broadcasts to all connected SSE clients.

**Coexistence with `/api/events`:** The existing `/api/events` SSE endpoint continues to broadcast global state (session list, pending prompts for hook-mode). It is updated to include managed session status changes (idle/running). Per-session streaming is exclusively on `/api/sessions/:id/stream`.

**Message types** (from `--output-format stream-json`):
- `system` (subtype: `init`) — session started
- `assistant` — assistant text blocks
- `tool_use` / `tool_result` — tool calls and outputs
- `result` — final result with metadata and token usage

### Security Model

**Protocol-level isolation:** The server never sends raw keystrokes to a PTY. Every interaction is a structured CLI invocation via `exec.Command` (no shell). There is no shell session to escape into. Prompt injection that outputs `exit\nrm -rf /` is just text content inside Claude's response — it never hits a shell interpreter.

**Tool restrictions:** Each session is spawned with `--allowedTools`. The server enforces this — the web UI cannot override the tool list. Per-session config allows read-only sessions (`Read,Glob,Grep` only).

**Interrupt safety:** SIGINT is an OS signal sent by the Go server directly. It cannot be intercepted by Claude's output or tool calls.

**Existing auth carries over:** API key + rate limiting protects web endpoints. No new attack surface.

**Not protected:** Claude Code still runs with the user's filesystem permissions. If `--allowedTools` includes `Bash`, Claude can run arbitrary commands within its own sandbox. Stronger isolation (Docker/container) is future work.

### Data Model

**New fields on `sessions` table:**

| Field | Type | Description |
|-------|------|-------------|
| `mode` | TEXT | `"hook"` (existing) or `"managed"` (new) |
| `allowed_tools` | TEXT | JSON array of tool names |
| `max_turns` | INTEGER | Server-enforced turn limit, default 50 |
| `max_budget_usd` | REAL | Cost limit passed to CLI, default 5.00 |
| `initialized` | BOOLEAN | Whether first message has been sent (controls `--session-id` vs `--resume`) |

**PID is not stored in the database.** The Go server maintains an in-memory map of `sessionID → *exec.Cmd` for running processes. This avoids latency on the interrupt path and race conditions between spawn and PID persistence. The map is protected by a per-session mutex.

**Session uniqueness:** Managed sessions use a partial unique index: `CREATE UNIQUE INDEX idx_managed_cwd ON sessions(cwd) WHERE mode = 'managed'`. One managed session per project directory. This is separate from the hook-mode `UNIQUE(computer_name, project_path)` constraint.

**New `messages` table:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | TEXT (UUID) | Primary key |
| `session_id` | TEXT (UUID) | FK to sessions |
| `seq` | INTEGER | Auto-incrementing sequence within session (avoids timestamp collisions) |
| `role` | TEXT | `user`, `assistant`, `system`, `tool_use`, `tool_result` |
| `content` | TEXT | Raw JSON from the stream |
| `exit_code` | INTEGER | Process exit code (only set on final `result` message, null otherwise) |
| `created_at` | TIMESTAMP | Insertion time |

This replaces JSONL transcript parsing for managed sessions. Hook-mode sessions continue using JSONL parsing via the existing transcript endpoint.

### API Endpoints

**New endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/sessions/create` | Create a managed session (cwd, allowed_tools, max_turns, max_budget_usd) |
| `POST` | `/api/sessions/:id/message` | Send a message (spawns `claude -p`) |
| `POST` | `/api/sessions/:id/interrupt` | SIGINT the running process |
| `DELETE` | `/api/sessions/:id` | SIGINT + wait + tear down session |
| `GET` | `/api/sessions/:id/stream` | SSE stream of live output |
| `GET` | `/api/sessions/:id/messages` | Full message history from DB |

**Key behaviors:**

- `POST /message`: Acquires the session mutex briefly to check/set running state. Returns 409 if a process is already running. Spawns `claude -p` via `exec.Command`, stores `*exec.Cmd` in the in-memory map immediately after `cmd.Start()`, sets `initialized = true`, then returns 200 to the HTTP client. A background goroutine reads stdout, streams to SSE clients, persists messages, and cleans up the map entry on process exit. The mutex guards spawn/check/cleanup transitions only, not the process lifetime. The 409 guard also prevents a race on the `initialized` field.
- `POST /interrupt`: Acquires the session mutex, sends SIGINT to the stored `*exec.Cmd`, returns 200. Returns 404 if no process in the map.
- `DELETE`: Calls interrupt (if running), waits up to 5s for exit, then SIGKILL, then removes DB records.
- Slash commands (e.g., `/compact`, `/clear`) are sent as regular messages through `POST /message`. No dedicated endpoints.

**Existing endpoints unchanged:** `/api/sessions/register`, `/api/prompts/*`, `/api/sessions/:id/instruct`, `/api/sessions/:id/transcript` — all continue working for hook-mode sessions.

### Configurable Claude Command

**`.env` file:**
```
CLAUDE_BIN=claude
CLAUDE_ARGS=--dangerously-skip-permissions
CLAUDE_ENV=CLAUDE_CONFIG_DIR=/Users/jay/.claud-bb
```

Three separate variables avoid fragile shell-style parsing:
- `CLAUDE_BIN` — the binary to execute (default: `claude`)
- `CLAUDE_ARGS` — space-separated additional flags (default: empty). Limitation: arguments containing spaces are not supported. All known Claude CLI flags use single-token values.
- `CLAUDE_ENV` — comma-separated `KEY=VALUE` pairs added to the child process environment (default: empty). Limitation: values containing commas are not supported. Use absolute paths, no tilde expansion.

The server splits `CLAUDE_ARGS` on whitespace and prepends them before the per-message flags.

### Web UI Changes

**Session list:**
- "New Session" button — prompts for project directory, creates a managed session
- Mode badge on each session card (`hook` vs `managed`)
- Live status: idle, running (spinner), waiting (hook-mode only)

**Chat view for managed sessions:**
- Textarea sends to `POST /api/sessions/:id/message`
- Messages stream in via SSE
- "Stop" button appears while running — calls `/interrupt`
- Slash commands typed in the textarea are sent as regular messages

**No changes for:** Chat bubble rendering, diff display, hook-mode sessions.

## Phased Migration

1. **Phase 1:** Build managed sessions alongside hooks. Both modes work. Web UI supports both.
2. **Phase 2:** Validate managed sessions work reliably. Migrate primary usage.
3. **Phase 3:** Remove hook-related code, endpoints, and tables.

## Out of Scope

- iOS app changes (web UI only for now)
- Docker/container sandboxing
- Agent SDK integration (CLI flags are sufficient)
- Multi-machine / remote Claude Code instances
