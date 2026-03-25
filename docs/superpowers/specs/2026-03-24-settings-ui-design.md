# Settings UI Design Spec

**Date:** 2026-03-24
**Issue:** #44 — Settings changes via UI

## Overview

Add a settings page to the web UI that allows users to view and modify environment variables (`PORT`, `NGROK_AUTHTOKEN`, `CLAUDE_BIN`, `CLAUDE_ARGS`, `CLAUDE_ENV`) that are currently configured via `.env` file. Includes a first-run setup modal when no `.env` file exists.

## API Endpoints

### `GET /api/settings/exists`

Lightweight check for first-run detection. Called on page load after authentication.

**Response:**
```json
{ "exists": true }
```

### `GET /api/settings`

Returns current settings. `NGROK_AUTHTOKEN` is masked (last 4 characters only).

**Response:**
```json
{
  "port": "9999",
  "ngrok_authtoken": "****abcd",
  "claude_bin": "/opt/homebrew/bin/claude",
  "claude_args": "--dangerously-skip-permissions",
  "claude_env": "CLAUDE_CONFIG_DIR=/Users/jay/.claude"
}
```

If the `.env` file doesn't exist, returns empty strings for all fields (the server may still have values from environment variables, but the settings UI only manages the `.env` file).

### `PUT /api/settings`

Accepts settings, writes to `.env` file, and hot-reloads applicable values.

**Request:**
```json
{
  "port": "9999",
  "ngrok_authtoken": "new-token-here",
  "claude_bin": "/opt/homebrew/bin/claude",
  "claude_args": "--dangerously-skip-permissions",
  "claude_env": "CLAUDE_CONFIG_DIR=/Users/jay/.claude"
}
```

**Sentinel behavior for `ngrok_authtoken`:** If the value matches the masked pattern (starts with `****`), the field is left unchanged in the `.env` file. This prevents users from accidentally blanking out the token when they only want to change other fields.

**Response:**
```json
{
  "restart_required": true
}
```

`restart_required` is `true` when `PORT` or `NGROK_AUTHTOKEN` changed compared to the current `.env` values.

## Hot-Reload Mechanism

### What reloads immediately

`CLAUDE_BIN`, `CLAUDE_ARGS`, and `CLAUDE_ENV` are used only when spawning new `claude -p` processes. On save:

1. Write new values to `.env` file (atomic: write `.env.tmp`, rename to `.env`)
2. Call `Manager.UpdateConfig(newCfg)` — a new thread-safe method on `managed.Manager`:
   ```go
   func (m *Manager) UpdateConfig(cfg Config) {
       m.mu.Lock()
       defer m.mu.Unlock()
       m.cfg = cfg
   }
   ```
   **Race safety:** `Spawn()` currently reads `m.cfg` fields before acquiring `m.mu`. To prevent a data race with `UpdateConfig`, `Spawn()` must copy the config under `m.mu` before using it:
   ```go
   m.mu.Lock()
   cfg := m.cfg // copy struct
   m.mu.Unlock()
   // use cfg.ClaudeBin, cfg.ClaudeArgs, cfg.ClaudeEnv below
   ```
3. The next `claude -p` invocation picks up the new config

### What requires a restart

`PORT` — the server is already bound to a TCP listener. `NGROK_AUTHTOKEN` — the tunnel is already established (or not). These values are written to `.env` but only take effect on next server start.

When either changes, the API returns `restart_required: true` and the frontend shows a persistent yellow banner: "Server restart required for PORT/NGROK changes to take effect."

## Frontend

### Settings gear icon

Added to the sidebar header, to the left of "Sessions" text. Uses an inline SVG gear icon. Opens the settings modal on click.

### Settings modal

Follows the same pattern as the existing `showNewSessionModal`:

- **Fields:** PORT, NGROK_AUTHTOKEN (password input), CLAUDE_BIN, CLAUDE_ARGS, CLAUDE_ENV
- **Helper text** under each field explaining format (e.g., "Space-separated CLI flags" for CLAUDE_ARGS, "Comma-separated KEY=VALUE pairs" for CLAUDE_ENV)
- **Save** button calls `PUT /api/settings`
- On success with `restart_required: true`: show yellow banner
- On success without restart required: close modal, show brief success indication
- **Cancel** closes modal without saving

### First-run setup modal

On page load, after authentication, call `GET /api/settings/exists`. If `.env` doesn't exist:

- Show a setup modal titled "Welcome — Configure Claude Controller"
- Same fields as settings modal
- "Save & Continue" button creates the `.env` file
- "Skip" button closes the modal (user runs with defaults, no `.env` created)

### Data flow

1. Page loads → authenticate → `GET /api/settings/exists`
2. If no `.env` → show first-run modal
3. User fills in fields → `PUT /api/settings` → `.env` created, manager config updated
4. Later: gear icon → `GET /api/settings` → populate form → edit → `PUT /api/settings`

## .env File Handling

### Format

Written with section comments for readability:

```bash
# Server
PORT=9999
NGROK_AUTHTOKEN=token-here

# Managed session config
CLAUDE_BIN=/opt/homebrew/bin/claude
CLAUDE_ARGS=--dangerously-skip-permissions
CLAUDE_ENV=CLAUDE_CONFIG_DIR=/Users/jay/.claude
```

### Reading

The existing `loadDotEnv` function in `main.go` handles reading on startup. The new `GET /api/settings` endpoint reads the `.env` file directly (not from `os.Getenv`) to show what's in the file.

### Writing

Atomic write: write to `.env.tmp` in the same directory, then `os.Rename` to `.env`. This prevents partial writes on crash.

### Edge cases

- **`.env` exists but is empty** — treated as "exists", no first-run modal. Settings page shows empty fields.
- **Concurrent writes** — unlikely (single user), but atomic rename handles it safely.
- **File permissions** — `.env` written with `0600` (contains `NGROK_AUTHTOKEN`; consistent with `api.key` file permissions).

## Input Validation

`PUT /api/settings` validates before writing:

- **PORT**: Must be a valid integer in range 1–65535, or empty (meaning "use default"). Returns `400` with error message on invalid input.
- **CLAUDE_BIN**: Saved as-is. No existence check — the error surfaces at spawn time, which is more informative than a pre-check (binary might not exist yet, or might be on a different PATH).
- **CLAUDE_ARGS**: Saved as-is. Space-separated; arguments with spaces are not supported (documented as known limitation in helper text).
- **CLAUDE_ENV**: Saved as-is. Comma-separated KEY=VALUE pairs.
- **NGROK_AUTHTOKEN**: Saved as-is (unless sentinel).
- All fields are optional (empty string = unset).

## Server Plumbing

`NewRouter` gains an `envPath string` parameter. `main.go` resolves the `.env` path to absolute before passing it:

```go
envPath, _ := filepath.Abs(".env")
router := api.NewRouter(store, apiKey, mgr, envPath)
```

The `Server` struct gains an `envPath string` field used by the settings handlers.

`PUT /api/settings` does **not** call `os.Setenv` — the Manager config is the source of truth for spawning processes. `os.Environ()` values from startup remain unchanged; this is acceptable because managed processes inherit env vars from the Manager config, not from the server's environment.

## Excluded Settings

`BIND_HOST` is intentionally excluded from the settings UI. It is an advanced/deployment setting (e.g., binding to `0.0.0.0` in Docker) that most users should not need to change.

## Files to modify

### New files
- `server/api/settings.go` — Settings API handlers
- `server/api/settings_test.go` — Settings API tests

### Modified files
- `server/api/router.go` — Register settings routes, add `envPath` to `Server` struct and `NewRouter`
- `server/managed/manager.go` — Add `UpdateConfig` method, fix `Spawn` to copy config under lock
- `server/main.go` — Resolve `.env` path to absolute, pass to `NewRouter`
- `server/web/static/index.html` — Settings gear icon, settings modal, first-run modal
- `server/web/static/app.js` — Settings state, API calls, modal logic
- `server/web/static/style.css` — Settings modal styles, restart banner
