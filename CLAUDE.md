# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Controller — a system for remotely controlling multiple Claude Code sessions from an iPhone. Three components: Go server, Claude Code hooks (bash + PowerShell), and a native SwiftUI iOS app. No cloud services — everything runs locally with ngrok tunneling to the phone.

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

### Hooks
```bash
./hooks/install.sh                             # Install hooks into Claude Code settings
```

## Architecture

- `server/` — Go REST API. `db/` = SQLite layer, `api/` = HTTP handlers + middleware, `tunnel/` = ngrok management.
- `hooks/` — Bash (macOS) and PowerShell (Windows) scripts triggered by Claude Code Stop and Notification events. Stop hook blocks and long-polls the local server for the user's response.
- `ios/` — SwiftUI app. `Services/` has API client and adaptive polling, `Views/` has all screens, `Models/` has Codable types matching server JSON.

## Key Design Decisions

- Stop hook returns `{"decision": "block", "reason": "..."}` JSON to feed responses back to Claude as context
- `stop_hook_active` field in hook input prevents infinite loops — when true, only check for queued instructions
- Instructions from the iOS app queue and deliver on the next Stop event (cannot interrupt Claude mid-turn)
- Long-poll: server holds HTTP connection up to 30s, hook retries indefinitely until user responds
- SQLite with WAL mode + busy_timeout for concurrent writes from multiple hook scripts
- QR code pairing: Go server displays QR in terminal containing `{"url": "...", "key": "...", "version": 1}`
- Hooks talk to localhost only; ngrok tunnel is for the iOS app's remote access

## Spec & Plan

- Design spec: `docs/superpowers/specs/2026-03-17-claude-controller-design.md`
- Implementation plan: `docs/superpowers/plans/2026-03-17-claude-controller.md`
