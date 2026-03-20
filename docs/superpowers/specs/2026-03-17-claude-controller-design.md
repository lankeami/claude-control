# Claude Controller — Design Spec

**Date:** 2026-03-17
**Status:** Approved

## Overview

A system for remotely controlling multiple Claude Code sessions from any device. Three components: a Go server running on the user's Mac/PC (with embedded web UI), Claude Code hook scripts, and a native iOS app. No cloud hosting — the Go server runs locally and is exposed via ngrok.

> **Note:** This spec covers the original hook-based architecture. The system was later extended with **managed sessions** (server spawns `claude -p` directly) and a **`/resume` command** (continue previous CLI sessions in the web UI). See the dedicated specs for those features:
> - `docs/superpowers/specs/2026-03-19-managed-sessions-design.md`
> - `docs/superpowers/specs/2026-03-19-resume-command-design.md`

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

1. Start HTTP server on a configurable port (default `localhost:8080`, configurable via `--port` flag or `PORT` env var; auto-detects next available port if occupied)
2. Initialize SQLite database with WAL mode enabled (auto-create tables if missing)
3. Start ngrok tunnel
4. Generate API key (first run only, stored in SQLite)
5. Display QR code in terminal containing `{"url": "https://abc123.ngrok.io", "key": "sk-xxx", "version": 1}`

### Data Model (SQLite)

SQLite is opened with WAL mode (`PRAGMA journal_mode=WAL`) and a busy timeout (`PRAGMA busy_timeout=5000`) to handle concurrent writes from multiple hook scripts.

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

`UNIQUE(computer_name, project_path)` constraint — used for upsert via `INSERT ... ON CONFLICT DO UPDATE`.

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

**instructions**

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT (UUID) | Primary key |
| session_id | TEXT | Foreign key to sessions |
| message | TEXT | Freeform instruction from the user |
| status | TEXT | `queued` or `delivered` |
| created_at | DATETIME | When the instruction was queued |
| delivered_at | DATETIME | When it was delivered to Claude |

### API Endpoints

All endpoints require `Authorization: Bearer <api-key>` header. Rate limited to 60 requests/minute per IP. Lockout after 10 failed auth attempts per IP (resets after 15 minutes).

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| POST | `/api/sessions/register` | Hook | Register/upsert a Claude session with computer name + project |
| POST | `/api/sessions/:id/heartbeat` | Hook | Keep session marked active |
| POST | `/api/prompts` | Hook | Submit a prompt Claude is waiting on |
| GET | `/api/prompts/:id/response` | Hook | Long-poll for user's response (server holds connection up to 30s, returns immediately when response arrives; hook retries on timeout) |
| GET | `/api/sessions/:id/instructions` | Hook | Check for queued instructions (called by Stop hook) |
| GET | `/api/sessions` | iOS | List all active sessions |
| GET | `/api/prompts?status=pending` | iOS | Get pending prompt queue |
| GET | `/api/prompts?session_id=:id` | iOS | Get prompt history for a session |
| POST | `/api/prompts/:id/respond` | iOS | Submit a response to a pending prompt |
| POST | `/api/sessions/:id/instruct` | iOS | Queue a freeform instruction for the next time Claude stops |
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

#### Claude Code Hooks Contract

Hooks receive JSON on stdin and return output via stdout/stderr. The key mechanism for the Stop hook:
- Returning `{"decision": "block", "reason": "..."}` on stdout prevents Claude from stopping and feeds the `reason` text to Claude as context, causing it to continue processing.
- The input includes `stop_hook_active: true` when Claude is already continuing from a previous Stop hook — **the hook must check this to prevent infinite loops**.

#### Stop hook — fires when Claude finishes a turn

**Input received on stdin:**
```json
{
  "session_id": "abc123",
  "transcript_path": "~/.claude/projects/.../session.jsonl",
  "cwd": "/Users/.../project",
  "hook_event_name": "Stop",
  "stop_hook_active": false
}
```

**Flow:**

1. Parse JSON from stdin
2. **If `stop_hook_active` is true**: check for queued instructions only (see step 6b), do NOT post the current message as a prompt — this prevents infinite loops
3. Read computer name from config (falls back to `hostname`)
4. Read project path from `cwd` field in stdin JSON
5. `POST /api/sessions/register` (idempotent upsert by computer + project)
6. **If `stop_hook_active` is false** (normal stop):
   - a. Read the transcript file to extract Claude's last message
   - b. `POST /api/prompts` with `{ session_id, claude_message, type: "prompt" }`
   - c. Long-poll `GET /api/prompts/:id/response` (server holds up to 30s per request, hook retries indefinitely)
   - d. When response arrives, output JSON to stdout:
     ```json
     {"decision": "block", "reason": "User responded: <their response>"}
     ```
   - e. Claude receives the reason and continues
7. **If `stop_hook_active` is true** (Claude continued from a previous hook):
   - a. Check `GET /api/sessions/:id/instructions` for queued instructions
   - b. If instruction found, output: `{"decision": "block", "reason": "User instruction: <message>"}`
   - c. If no instruction, exit with code 0 (Claude stops normally)

#### Notification hook — fires when Claude sends a notification

**Input received on stdin:**
```json
{
  "session_id": "abc123",
  "transcript_path": "~/.claude/projects/.../session.jsonl",
  "cwd": "/Users/.../project",
  "hook_event_name": "Notification",
  "message": "Claude is waiting for your input"
}
```

**Flow:**

1. Parse JSON from stdin
2. `POST /api/sessions/register` (upsert)
3. `POST /api/prompts` with `{ session_id, claude_message: message, type: "notification" }`
4. Exit immediately (fire-and-forget, no polling)

#### Instruction Delivery

The "New Instruction" feature in the iOS app works by queuing instructions that get delivered on Claude's next Stop event. This means instructions can only be delivered when Claude finishes a turn — they cannot interrupt Claude mid-work. The iOS app should make this clear (e.g., "Instruction queued — will be delivered when Claude finishes its current turn").

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
- "New Instruction" button sends freeform message via `POST /api/sessions/:id/instruct` — displays "Instruction queued — will be delivered when Claude finishes its current turn"
- App icon badge shows count of pending prompts (updated via local polling)
- Adaptive polling: polls every 3 seconds when sessions are active/waiting, slows to every 15 seconds when all sessions are idle (preserves battery)

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
