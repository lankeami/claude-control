---
name: cleanup-ports
description: Use when the user types /cleanup-ports, or reports that a Makefile/Docker app can't bind its port (EADDRINUSE, "address already in use", "port hijacked"), to find and kill rogue processes holding the app's port
---

# Cleanup Ports

Find and kill processes that are squatting on the port the current project's app expects to bind.

## When to Use

- User invokes `/cleanup-ports`
- `make run` / `make local` / `docker compose up` fails with `bind: address already in use` or `EADDRINUSE`
- A previous binary run (despite the rule to run inside Docker) hijacked the host port
- A stale process from a crashed container is still holding a port

## Resolving the Port

The port to clean is the one the app would actually use right now. Resolve in this order — **first hit wins**:

1. **`.env` in the project root** — grep for `^PORT=` (this is what the runtime loads)
2. **`Makefile` default** — grep for `^PORT\s*?=` or `^PORT\s*:=` (the `?=` default if `.env` doesn't set it)
3. **`docker-compose.yml`** — `ports:` mapping (host side of `HOST:CONTAINER`)
4. **Ask the user** if none of the above resolve

If `.env` and the Makefile disagree, **prefer `.env`** but mention the Makefile default in the report — the discrepancy itself is often the bug.

## Workflow

Do these steps in order. Use `TodoWrite` to track them.

### 1. Resolve the port

```bash
# From the project root
PORT_FROM_ENV=$(grep -E '^PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2)
PORT_FROM_MAKE=$(grep -E '^PORT\s*[?:]?=' Makefile 2>/dev/null | head -1 | sed -E 's/^PORT\s*[?:]?=\s*([0-9]+).*/\1/')
PORT="${PORT_FROM_ENV:-$PORT_FROM_MAKE}"
echo "Resolved PORT=$PORT (env=$PORT_FROM_ENV, make=$PORT_FROM_MAKE)"
```

Report both values to the user even if they match.

### 2. Confirm the app's own process (sanity check)

Look up what's listening so you can recognize the legitimate process if the user happens to have it running:

```bash
lsof -i :"$PORT" -sTCP:LISTEN -nP
```

Empty output means nothing is listening — there is nothing to clean. Tell the user the port is free and stop.

### 3. List every process touching the port

A process holding the port may not be in `LISTEN` state (e.g., a half-closed connection from a crashed parent). Get the full picture:

```bash
lsof -i :"$PORT" -nP
```

For each unique PID, gather details:

```bash
for PID in $(lsof -i :"$PORT" -nP -t | sort -u); do
  echo "=== PID $PID ==="
  ps -o pid=,ppid=,user=,command= -p "$PID"
  lsof -a -p "$PID" -d cwd -Fn | sed -n 's/^n//p'   # working directory
done
```

`lsof -d cwd -Fn` is the macOS-friendly way to get a process's current working directory.

### 4. Present the table

Show the user a table of what was found **before killing anything**:

| PID | Command | User | CWD | Notes |
|-----|---------|------|-----|-------|
| ... | ...     | ...  | ... | e.g. "matches project", "stale Docker proxy", "unrelated" |

Flag anything obviously unrelated to this project (different CWD, different binary name) — those are the hijackers.

### 5. Confirm before killing

Even though the user asked to "kill them", killing is destructive. Use `AskUserQuestion` to confirm which PIDs to kill, unless the user passed `--force` in the slash command args. Default options:

- Kill all listed PIDs
- Kill only the non-project PIDs
- Cancel

### 6. Kill

Send SIGTERM first, give it 2 seconds, then SIGKILL anything still alive:

```bash
for PID in $PIDS_TO_KILL; do
  kill "$PID" 2>/dev/null
done
sleep 2
for PID in $PIDS_TO_KILL; do
  if kill -0 "$PID" 2>/dev/null; then
    echo "PID $PID survived SIGTERM, sending SIGKILL"
    kill -9 "$PID"
  fi
done
```

### 7. Verify the port is free

```bash
lsof -i :"$PORT" -sTCP:LISTEN -nP || echo "Port $PORT is now free"
```

Report final state. If anything is still listening, surface the remaining PIDs — do not silently retry.

## Quick Reference

| Need | Command |
|------|---------|
| Resolve port from `.env` | `grep -E '^PORT=' .env \| cut -d= -f2` |
| List listeners on port | `lsof -i :PORT -sTCP:LISTEN -nP` |
| List **all** sockets on port | `lsof -i :PORT -nP` |
| Get PIDs only | `lsof -i :PORT -nP -t` |
| Get CWD of PID | `lsof -a -p PID -d cwd -Fn` |
| Get full command | `ps -o command= -p PID` |
| Graceful kill | `kill PID` (SIGTERM) |
| Force kill | `kill -9 PID` (SIGKILL) |

## Common Mistakes

- **Killing the project's own process by mistake.** If the app is supposed to be running (managed Docker container, dev server in another terminal), match CWD/command before killing. Ask the user.
- **Only checking `LISTEN` state.** A zombie child or half-closed socket may still hold the port without being in `LISTEN`. Use `lsof -i :PORT -nP` without the state filter to see everything.
- **Trusting the Makefile default over `.env`.** `.env` overrides at runtime. The user's reported failure is almost always on the `.env` port.
- **Using `fuser` on macOS.** `fuser` behaves differently on macOS than Linux; prefer `lsof`.
- **Skipping confirmation.** "List, then kill" still means show the list and get an OK first unless `--force` was passed.

## Args

- `/cleanup-ports` — interactive: confirm before killing
- `/cleanup-ports --force` — skip confirmation, kill every PID found on the port
- `/cleanup-ports PORT` — override port resolution (e.g. `/cleanup-ports 8080`)
