# Remote Server Restart — Design Spec

## Problem

When the Go server gets into a bad state (stuck sessions, errors), the only way to restart it is physically at the machine. Users controlling sessions from a phone via the web UI have no way to bounce the server remotely.

The most common symptom is the "Error: session is currently processing" message, which occurs when `activity_state='working'` but the underlying process is stuck or the state is stale.

## Solution

A `POST /api/restart` endpoint that triggers graceful shutdown of all managed sessions, then self-re-execs the binary. The frontend provides a restart button and handles reconnection automatically.

## API

### `POST /api/restart`

Authenticated endpoint (same Bearer token auth as all API routes).

**Response:** `200 {"status": "restarting"}`

The 200 is sent and flushed before shutdown begins (with a ~500ms delay) so the client receives confirmation.

**Error responses:**
- `409 Conflict` — restart already in progress
- `401 Unauthorized` — missing/invalid auth token

## Shutdown Sequence

1. **Set restart flag** — prevents concurrent restart requests (atomic bool)
2. **Send 200 response** — confirm to client, flush, wait ~500ms
3. **Stop scheduler** — `sched.Stop()` to prevent new task execution
4. **Graceful shutdown of managed sessions:**
   - For each session with a running process: close stdin, wait up to 5s for exit, SIGKILL if timeout
   - Set all `working` activity states to `waiting` in the database (not `idle` — preserves conversation continuity so users can re-send their last message)
5. **Close HTTP listener** — stop accepting new connections
6. **Close database** — flush WAL, close connection
7. **Self-re-exec** — `syscall.Exec(os.Args[0], os.Args, os.Environ())`
8. **Fallback** — if re-exec fails, `os.Exit(0)` for wrapper script / process supervisor to handle

## Self-Re-Exec

Uses `syscall.Exec` to replace the current process with a fresh instance of the same binary, preserving the original command-line arguments and environment. This is an in-place replacement (same PID on Unix).

If `syscall.Exec` fails (e.g., binary was deleted, permission issue), falls back to `os.Exit(0)`. A wrapper script like:

```bash
#!/bin/bash
while true; do
    ./claude-controller "$@"
    echo "Server exited, restarting in 1s..."
    sleep 1
done
```

can catch this and restart the process.

## Frontend Changes

### Restart Button

Add an "Actions" section to the bottom of the settings modal, near the save button. This section contains a "Restart Server" button with a confirmation prompt ("Are you sure? This will restart the server and briefly disconnect all sessions.").

### Restart Flow

1. User clicks restart button
2. Frontend sends `POST /api/restart`
3. On 200 response: show "Server restarting..." banner/toast
4. SSE connections will drop — this is expected
5. Frontend polls `GET /api/events` every 1s until it responds
6. On successful response: dismiss banner, reconnect SSE streams
7. Optional: full page reload if SSE reconnect doesn't recover cleanly

### SSE Reconnection

The existing SSE streams (`/api/events` and `/api/sessions/{id}/stream`) already have reconnect logic with exponential backoff. The restart banner provides visual feedback during the reconnect window.

## Files Changed

| File | Change |
|------|--------|
| `server/api/restart.go` | New file: restart handler with shutdown orchestration |
| `server/api/router.go` | Add `POST /api/restart` route |
| `server/api/server.go` | Add shutdown dependencies (listener, DB closer, scheduler) to Server struct |
| `server/managed/manager.go` | Add `ShutdownAll()` method to gracefully stop all processes |
| `server/main.go` | Pass shutdown dependencies to API server; refactor signal handling |
| `server/web/static/app.js` | Restart button, restart banner, reconnect polling |
| `server/web/static/index.html` | Restart button markup (if not purely JS-driven) |

## Edge Cases

### Restart during active processing
Graceful shutdown sends SIGINT to running Claude processes, waits up to 5s, then kills. Activity states are set to `waiting` so the user can re-send their message after restart.

### Re-exec fails
Falls back to `os.Exit(0)`. If no wrapper script is running, user needs to manually restart — same as today. The frontend will show "Server restarting..." and eventually timeout with a "Could not reconnect" message.

### Concurrent restart requests
First request wins. Subsequent requests receive `409 Conflict` while restart is in progress.

### Binary changed on disk
If the binary has been rebuilt before restart is triggered, `syscall.Exec` will load the new binary. This is a feature — it means code changes can be picked up via restart even though the primary use case is bouncing bad state.

### Scheduler running tasks
`sched.Stop()` signals the scheduler to stop. If a task is currently executing, the shutdown proceeds without waiting for it to complete — the task's context will be cancelled, and it can be retried after restart via the existing `ReconcileMissed()` call on startup.

## Out of Scope

- Rebuilding the Go binary remotely (code changes + `go build`)
- Rolling restarts or zero-downtime restart
- Automatic restart on crash detection
- Health check endpoint (could be added separately)
