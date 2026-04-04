# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Controller — a system for remotely controlling multiple Claude Code sessions from any device. Two modes: **hook mode** (Claude Code runs independently, hooks relay prompts/responses) and **managed mode** (server spawns `claude -p` directly, streaming output to a web UI). Components: Go server with embedded web UI, Claude Code hooks (bash + PowerShell), and a native SwiftUI iOS app. No cloud services — everything runs locally with ngrok tunneling.

## Build & Test Commands

### Go Server
```bash
cd server && go build -o claude-controller .   # Build
cd server && go test ./... -v                  # Run all tests
cd server && go test ./db/ -v                  # Test DB layer only
cd server && go test ./api/ -v                 # Test API handlers only
cd server && go test ./api/ -v -run TestName   # Run single test
cd server && go run .                          # Run server (starts on :8080)
cd server && go run . --port 9090              # Custom port
```

### iOS App
Open `ios/ClaudeController/` in Xcode. Build target: iOS 17.0+.

### Docker
```bash
docker compose up --build                       # Build and run in container
NGROK_AUTHTOKEN=xxx docker compose up --build   # With ngrok tunnel
docker compose down                             # Stop
```

### Hooks
```bash
./hooks/install.sh                             # Install hooks into Claude Code settings
```

## Architecture

- `server/` — Go REST API. `db/` = SQLite layer, `api/` = HTTP handlers + middleware, `tunnel/` = ngrok management, `managed/` = managed session process lifecycle + NDJSON streaming, `web/` = embedded Alpine.js web UI.
- `hooks/` — Bash (macOS) and PowerShell (Windows) scripts triggered by Claude Code Stop and Notification events. Stop hook blocks and long-polls the local server for the user's response. Hooks skip execution when `CLAUDE_CONTROLLER_MANAGED=1` (set by managed sessions to prevent duplicate registrations).
- `ios/` — SwiftUI app. `Services/` has API client and adaptive polling, `Views/` has all screens, `Models/` has Codable types matching server JSON.

## Key Design Decisions

### Hook Mode
- Stop hook returns `{"decision": "block", "reason": "..."}` JSON to feed responses back to Claude as context
- `stop_hook_active` field in hook input prevents infinite loops — when true, only check for queued instructions
- Instructions from the web UI/iOS app queue and deliver on the next Stop event (cannot interrupt Claude mid-turn)
- Long-poll: server holds HTTP connection up to 30s, hook retries indefinitely until user responds

### Managed Mode
- Each message spawns a separate `claude -p` process; `--resume <uuid>` handles context continuity between turns
- Sessions have a `mode` field (`"hook"` or `"managed"`) — both coexist in the same database
- NDJSON streaming from stdout → SSE to browser; messages persisted to `messages` table
- Tool restrictions via `--allowedTools`, turn limits via server-side SIGINT, budget caps via `--max-budget-usd`
- `/resume` command reads Claude Code's native `sessions-index.json` to let users continue previous CLI sessions in the web UI
- `claude_session_id` field decouples the managed session's stable ID from the CLI session being resumed
- `activity_state` field tracks session lifecycle: `working` (process running), `waiting` (process exited cleanly, awaiting input), `idle` (no process, error, or new session). Updated server-side at process start/exit. Frontend shows yellow pulsing dot (working), green dot (waiting), gray dot (idle). On server startup, stale `working` states are reset to `idle`.

### Shared
- SQLite with WAL mode + busy_timeout for concurrent writes from multiple hook scripts
- QR code pairing: Go server displays QR in terminal containing `{"url": "...", "key": "...", "version": 1}`
- Hooks talk to localhost only; ngrok tunnel is for remote access (web UI + iOS app)

## Spec & Plan

- Core design spec: `docs/superpowers/specs/2026-03-17-claude-controller-design.md`
- Core implementation plan: `docs/superpowers/plans/2026-03-17-claude-controller.md`
- Web UI spec: `docs/superpowers/specs/2026-03-18-web-ui-design.md`
- Web UI plan: `docs/superpowers/plans/2026-03-18-web-ui.md`
- Chat UI spec: `docs/superpowers/specs/2026-03-18-chat-ui-design.md`
- Chat UI plan: `docs/superpowers/plans/2026-03-18-chat-ui.md`
- Managed sessions spec: `docs/superpowers/specs/2026-03-19-managed-sessions-design.md`
- Managed sessions plan: `docs/superpowers/plans/2026-03-19-managed-sessions.md`
- Resume command spec: `docs/superpowers/specs/2026-03-19-resume-command-design.md`
- Resume command plan: `docs/superpowers/plans/2026-03-19-resume-command.md`
- SSE interrupt / auto-continue spec: `docs/superpowers/specs/2026-03-23-sse-interrupt-turns-management-design.md`
- SSE interrupt / auto-continue plan: `docs/superpowers/plans/2026-03-23-sse-interrupt-turns-management.md`
- Session activity state spec: `docs/superpowers/specs/2026-03-23-session-activity-state-design.md`
- Session activity state plan: `docs/superpowers/plans/2026-03-23-session-activity-state.md`
- Shortcut creator spec: `docs/superpowers/specs/2026-04-03-shortcut-creator-design.md`
- Shortcut creator plan: `docs/superpowers/plans/2026-04-03-shortcut-creator.md`
