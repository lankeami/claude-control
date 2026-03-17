# Claude Controller

Remotely control multiple Claude Code sessions from your iPhone.

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

When Claude Code finishes a turn, hook scripts POST the output to a local Go server and block until you respond from the iOS app. Your response is fed back to Claude as context, so it keeps working — all from your phone.

## Components

| Component | Technology | Location |
|-----------|------------|----------|
| Server | Go + SQLite + ngrok | `server/` |
| Hooks | Bash (macOS) + PowerShell (Windows) | `hooks/` |
| iOS App | Swift / SwiftUI (iOS 17+) | `ios/` |

## Quick Start

### 1. Start the server

**Native (requires Go 1.22+):**

```bash
cd server
go run .
```

**Docker:**

```bash
NGROK_AUTHTOKEN=your-token docker compose up --build
```

The server starts on port 8080, creates an ngrok tunnel, and displays a QR code in the terminal.

### 2. Install the hooks

```bash
./hooks/install.sh
```

This writes `~/.claude-controller.json` with your server URL and API key, and registers the Stop and Notification hooks in Claude Code's settings. Restart any running Claude Code sessions afterward.

### 3. Pair the iOS app

Open the Claude Controller app on your iPhone and scan the QR code displayed in the terminal. The QR code contains the ngrok URL and API key.

## How It Works

1. Claude Code finishes a turn and fires the **Stop hook**
2. The hook extracts Claude's last message from the transcript
3. It POSTs the message to the Go server as a pending prompt
4. The hook **blocks** and long-polls the server for your response
5. You see the prompt on your iPhone and type a reply
6. The hook receives your response and returns `{"decision": "block", "reason": "User responded: ..."}` to Claude Code
7. Claude reads your response and continues working

**Instructions** can also be queued from the iOS app — they're delivered the next time Claude finishes a turn.

**Notifications** (e.g., "build succeeded") are fire-and-forget — they appear in the app but don't block Claude.

## Server

```bash
cd server && go build -o claude-controller .   # Build
cd server && go test ./... -v                  # Run all tests
cd server && go run .                          # Run (default :8080)
cd server && go run . --port 9090              # Custom port
```

### API Endpoints

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| POST | `/api/sessions/register` | Hook | Register/upsert a session |
| POST | `/api/sessions/:id/heartbeat` | Hook | Keep session active |
| POST | `/api/prompts` | Hook | Submit a prompt |
| GET | `/api/prompts/:id/response` | Hook | Long-poll for response (30s timeout) |
| GET | `/api/sessions` | iOS | List active sessions |
| GET | `/api/prompts?status=pending` | iOS | Get pending prompts |
| POST | `/api/prompts/:id/respond` | iOS | Send a response |
| POST | `/api/sessions/:id/instruct` | iOS | Queue an instruction |
| GET | `/api/pairing` | iOS | Validate pairing |
| GET | `/api/status` | iOS | Health check |

All endpoints require `Authorization: Bearer <api-key>`.

## iOS App

Open `ios/ClaudeController/` in Xcode (iOS 17.0+ deployment target). The app has no external dependencies.

**Screens:**
- **Pairing** — QR code scanner + manual entry
- **Main** — Session selector dropdown, pending prompt queue with response input, prompt history
- **Instruction** — Queue freeform instructions for Claude's next turn
- **Settings** — Manage paired servers, view archived sessions

The app polls the server every 3 seconds when sessions are active, slowing to 15 seconds when idle.

## Configuration

The hooks read from `~/.claude-controller.json`:

```json
{
  "server_url": "http://localhost:8080",
  "computer_name": "Jays-MacBook-Pro",
  "api_key": "sk-..."
}
```

## Requirements

- **Server:** Go 1.22+ (native) or Docker
- **Hooks:** `jq` and `curl` (macOS), PowerShell 5+ (Windows)
- **iOS App:** Xcode 15+, iOS 17+
- **ngrok:** Free account with auth token (`NGROK_AUTHTOKEN` env var)
