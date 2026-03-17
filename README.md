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
go run .                    # default port 8080
go run . --port 9999        # custom port
```

**Docker:**

```bash
NGROK_AUTHTOKEN=your-token docker compose up --build             # default port 8080
PORT=9999 NGROK_AUTHTOKEN=your-token docker compose up --build   # custom port
```

The server starts on the configured port, creates an ngrok tunnel, and displays a QR code in the terminal.

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

### Setting up the Xcode project (first time only)

The Swift source files are in `ios/ClaudeController/` but you need to create an Xcode project to build them:

1. Open Xcode (install from the Mac App Store if you don't have it)
2. **File → New → Project**
3. Select **App** under the iOS tab, click Next
4. Fill in:
   - Product Name: `ClaudeController`
   - Team: select your Apple ID (see "Add your Apple ID" below if not listed)
   - Organization Identifier: `com.yourname` (e.g. `com.jchinthrajah`)
   - Interface: **SwiftUI**
   - Language: **Swift**
5. Click Next, save to the `ios/` directory of this repo
6. **Delete the auto-generated files** — Xcode creates `ContentView.swift` and a default `ClaudeControllerApp.swift`. Right-click each in the project navigator → Delete → Move to Trash
7. **Add the existing source files** — Right-click the `ClaudeController` folder in the project navigator → Add Files to "ClaudeController" → select the `Models/`, `Services/`, `Views/` folders and `ClaudeControllerApp.swift`. Make sure "Copy items if needed" is **unchecked** and "Create groups" is selected
8. In the project navigator, click the top-level **ClaudeController** project → General tab:
   - Set **Minimum Deployments** to **iOS 17.0**
9. Under **Signing & Capabilities**:
   - Check "Automatically manage signing"
   - Team: select your personal team (see below)
   - Bundle Identifier: `com.yourname.ClaudeController`
10. Add camera permission — click the **Info** tab, add a row: key = `Privacy - Camera Usage Description`, value = `Scan QR code to pair with server`

After this, `make xcode` will open the project directly.

### Add your Apple ID to Xcode (if not already done)

1. Xcode → **Settings** (⌘,) → **Accounts** tab
2. Click **+** in the bottom left → **Apple ID**
3. Sign in with your Apple ID (any free Apple ID works)
4. Your "Personal Team" will appear in the Team dropdown

### Install the app on your iPhone

1. Connect your iPhone to your Mac with a USB cable
2. On your iPhone, tap **Trust** when prompted to trust this computer
3. Run `make xcode` to open the project
4. In the top toolbar, click the device dropdown (next to the play/stop buttons) and select your iPhone
5. Click the **Run** button (▶) or press **⌘R**
6. First build will take a minute. If you see a signing error, double-check your Team is set in Signing & Capabilities
7. On your iPhone: go to **Settings → General → VPN & Device Management** → tap your developer email → **Trust**
8. Go back to Xcode and hit Run again — the app will install and launch on your phone

**Troubleshooting:**
- "Untrusted Developer" → do step 7 above
- "No provisioning profile" → make sure you selected a Team in Signing & Capabilities
- "Device is busy" → wait a moment, unlock your phone, try again
- Build errors about missing types → make sure all folders (Models, Services, Views) are added to the project with "Create groups" selected

**Note:** With a free Apple ID, the app expires after 7 days. Just hit Run from Xcode again to refresh it. This is an Apple limitation for free developer accounts.

### Screens

- **Pairing** — QR code scanner + manual entry fallback
- **Main** — Session selector dropdown, pending prompt queue with response input, prompt history
- **Instruction** — Queue freeform instructions for Claude's next turn
- **Settings** — Manage paired servers, view archived sessions

The app polls the server every 3 seconds when sessions are active, slowing to 15 seconds when idle.

## Port Configuration

The default port is 8080. To use a custom port, set it in each component:

| Component | How to set port |
|-----------|----------------|
| Server (native) | `go run . --port 9999` or `PORT=9999 go run .` |
| Server (Docker) | `PORT=9999 docker compose up --build` |
| Hooks | Set `server_url` in `~/.claude-controller.json` (the install script prompts for this) |
| iOS app | Port is embedded in the ngrok URL from QR code — no separate config needed |

## Configuration

The hooks read from `~/.claude-controller.json`:

```json
{
  "server_url": "http://localhost:9999",
  "computer_name": "Jays-MacBook-Pro",
  "api_key": "sk-..."
}
```

## Requirements

- **Server:** Go 1.22+ (native) or Docker
- **Hooks:** `jq` and `curl` (macOS), PowerShell 5+ (Windows)
- **iOS App:** Xcode 15+, iOS 17+
- **ngrok:** Free account with auth token (`NGROK_AUTHTOKEN` env var)
