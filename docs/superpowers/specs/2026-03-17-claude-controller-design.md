# Claude Controller — Design Spec

**Date:** 2026-03-17
**Status:** Approved

## Overview

A system for remotely controlling multiple Claude Code sessions from an iPhone. Three components: a Go server running on the user's Mac/PC, Claude Code hook scripts, and a native iOS app. No cloud hosting — the Go server runs locally and is exposed via ngrok.

## Architecture

```
┌─────────────────────────────────────┐
│          Mac/PC                     │
│                                     │
│  Claude Code ──hooks──▶ Go Server   │
│                         │  (REST API + SQLite)
│                         │           │
│                         ▼           │
│                       ngrok         │
└─────────────────────┬───────────────┘
                      │ public tunnel
                      ▼
┌─────────────────────────────────────┐
│          iPhone App (SwiftUI)       │
│                                     │
│  Polls Go server via ngrok URL      │
│  Displays prompt queue              │
│  Sends responses back               │
└─────────────────────────────────────┘
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| Server | Go + SQLite + ngrok |
| Hooks | Bash (macOS) + PowerShell (Windows) |
| iOS App | Swift / SwiftUI |
| Auth | Shared API key via QR code |
| Connectivity | ngrok tunnel, iOS polls REST API |
| Storage | SQLite on local machine |

## Go Server

### Startup Flow

1. Start HTTP server on `localhost:8080`
2. Initialize SQLite database (auto-create tables if missing)
3. Start ngrok tunnel
4. Generate API key (first run only, stored in SQLite)
5. Display QR code in terminal containing `{"url": "https://abc123.ngrok.io", "key": "sk-xxx"}`

### Data Model (SQLite)

**sessions**

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| computer_name | TEXT | Hostname of the machine |
| project_path | TEXT | Working directory of the Claude session |
| status | TEXT | `active`, `idle`, `waiting` |
| created_at | DATETIME | When the session was first registered |
| last_seen_at | DATETIME | Last heartbeat timestamp |
| archived | BOOLEAN | Whether the user has archived this session |

**prompts**

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| session_id | TEXT | Foreign key to sessions |
| claude_message | TEXT | The question/output from Claude |
| type | TEXT | `prompt` (needs response) or `notification` (fire-and-forget) |
| response | TEXT | User's answer (null until responded) |
| status | TEXT | `pending` or `answered` |
| created_at | DATETIME | When the prompt was received |
| answered_at | DATETIME | When the user responded |

### API Endpoints

All endpoints require `Authorization: Bearer <api-key>` header.

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| POST | `/api/sessions/register` | Hook | Register/upsert a Claude session with computer name + project |
| POST | `/api/sessions/:id/heartbeat` | Hook | Keep session marked active |
| POST | `/api/prompts` | Hook | Submit a prompt Claude is waiting on |
| GET | `/api/prompts/:id/response` | Hook | Block-poll until user responds (long-poll, 2s intervals) |
| GET | `/api/sessions` | iOS | List all active sessions |
| GET | `/api/prompts?status=pending` | iOS | Get pending prompt queue |
| GET | `/api/prompts?session_id=:id` | iOS | Get prompt history for a session |
| POST | `/api/prompts/:id/respond` | iOS | Submit a response to a pending prompt |
| POST | `/api/sessions/:id/instruct` | iOS | Send a new freeform instruction to Claude |
| GET | `/api/pairing` | iOS | Validate API key and confirm pairing |
| GET | `/api/status` | iOS | Health check / connectivity test |

## Claude Code Hooks

Two hook scripts per platform: bash (macOS) and PowerShell (Windows). Located in `hooks/` directory.

### Configuration

`~/.claude-controller.json`:

```json
{
  "server_url": "http://localhost:8080",
  "computer_name": "Jays-MacBook-Pro"
}
```

### Hook Events

**Stop hook** — fires when Claude finishes a turn and waits for input:

1. Capture Claude's last message from hook stdin
2. Read computer name from config (falls back to `hostname`)
3. Read project path from working directory
4. `POST /api/sessions/register` (idempotent upsert by computer + project)
5. `POST /api/prompts` with `{ session_id, claude_message, type: "prompt" }`
6. Poll `GET /api/prompts/:id/response` every 2 seconds
7. When response arrives, echo it to stdout — Claude receives it as user input
8. No timeout — polls indefinitely until a response is received

**Notification hook** — fires when Claude sends a notification:

1. `POST /api/sessions/register` (upsert)
2. `POST /api/prompts` with `{ session_id, claude_message, type: "notification" }`
3. Exit immediately (fire-and-forget, no polling)

### Graceful Degradation

- If the Go server is not running (localhost:8080 unreachable), the hook exits silently
- Claude continues normally as if no hook was installed
- No disruption to the Claude session

## iOS App

### Screens

**1. Pairing Screen (first launch)**

- Camera opens for QR code scanning
- QR contains ngrok URL + API key as JSON
- Validates via `GET /api/pairing`
- Stores credentials in iOS Keychain
- Supports multiple pairings (one per computer)

**2. Main Screen — Session Selector + Prompt Queue**

```
┌────────────────────────────────┐
│  ▼ Jays-MacBook-Pro / claude-  │  ← dropdown: sessions
│    controller                  │     (computer + project)
├────────────────────────────────┤
│                                │
│  ● Claude is waiting...        │  ← prompt card (pending)
│  "Which database do you want   │
│   to use?                      │
│   A) SQLite  B) Postgres"      │
│                                │
│  ┌────────────────────────┐    │
│  │ Type your response...  │    │
│  └────────────────────────┘    │
│         [Send]                 │
│                                │
├────────────────────────────────┤
│  ○ 2 min ago                   │  ← answered prompt
│  "Task complete: refactored    │
│   auth module"                 │
│         Replied: "thanks"      │
│                                │
├────────────────────────────────┤
│  ○ 5 min ago                   │  ← notification
│  "Build succeeded"             │
└────────────────────────────────┘
│       [+ New Instruction]      │
└────────────────────────────────┘
```

**3. Settings Screen**

- List of paired computers with re-scan / remove options
- Server connection status indicator
- Archive management

### Behaviors

- Polls `GET /api/prompts?status=pending` every 3 seconds
- Pending prompts appear at top with text field and Send button
- Answered prompts and notifications show below as history
- Session dropdown shows `computer_name / project` with status dot:
  - Green = waiting for input
  - Gray = idle (no heartbeat for 5+ minutes)
- "New Instruction" button sends freeform message via `POST /api/sessions/:id/instruct`
- App icon badge shows count of pending prompts (updated via local polling)

### Stale Session Handling

- Sessions with no heartbeat for 5+ minutes get dimmed visual treatment (gray dot)
- **Prompts never expire** — pending prompts stay answerable indefinitely since the hook blocks and polls until answered
- Manual archive button on each session to tuck away sessions you're done with
- Archived section at bottom of session list, expandable, with unarchive button

## Error Handling

**ngrok URL changes (free tier restart):**
- Go server detects new ngrok URL on restart, displays updated QR code in terminal
- iOS app shows "Connection lost" banner with "Re-scan QR" button
- `GET /api/status` used for connectivity detection

**Multiple computers:**
- Each computer runs its own Go server + ngrok tunnel
- iOS app supports multiple saved pairings in settings
- Session dropdown groups by computer name

## Project Structure

```
claude-controller/
├── server/           # Go server
│   ├── main.go
│   ├── api/          # HTTP handlers
│   ├── db/           # SQLite layer
│   └── ngrok/        # ngrok tunnel management
├── hooks/            # Claude Code hook scripts
│   ├── stop.sh       # macOS Stop hook
│   ├── stop.ps1      # Windows Stop hook
│   ├── notify.sh     # macOS Notification hook
│   └── notify.ps1    # Windows Notification hook
├── ios/              # SwiftUI app
│   └── ClaudeController/
│       ├── App/
│       ├── Views/
│       ├── Models/
│       ├── Services/
│       └── ClaudeController.xcodeproj
└── docs/
```
