# Claude Controller

Remotely control multiple Claude Code sessions from any device.

```
┌──────────────────────────────────────────────┐
│          Mac/PC                               │
│                                               │
│  Claude Code ──hooks──▶ Go Server ◀── Web UI │
│                         │  (REST API + SQLite) │
│       ▲                 │                      │
│       └── managed ──────┘  (spawns claude -p)  │
│                         ▼                      │
│                       ngrok                    │
└─────────────────────┬────────────────────────┘
                      │ public tunnel
                      ▼
┌─────────────────────────────────────┐
│     Browser / iPhone App            │
│                                     │
│  Web UI or iOS app via ngrok URL    │
│  Chat with Claude, manage sessions  │
│  Resume previous CLI sessions       │
└─────────────────────────────────────┘
```

Two modes of operation:

- **Hook mode** — Claude Code runs independently; hook scripts POST output to the server and block until you respond from the web UI or iOS app. Your response is fed back to Claude as context.
- **Managed mode** — The server spawns `claude -p` directly as a child process, streaming output to the web UI via SSE. Full lifecycle control: send messages, interrupt mid-turn, enforce turn limits, and restrict tools — all from the browser.

## Components

| Component | Technology | Location |
|-----------|------------|----------|
| Server | Go + SQLite + ngrok | `server/` |
| Web UI | Alpine.js (embedded in server binary) | `server/web/static/` |
| Hooks | Bash (macOS) + PowerShell (Windows) | `hooks/` |
| iOS App | Swift / SwiftUI (iOS 17+) | `ios/` |

## New? Start Here

If you're setting up Claude Controller for the first time, one command does everything:

```bash
make quickstart
```

This checks for Go, installs dependencies, builds the server, and opens the web UI at **http://localhost:9999** automatically.

See **[docs/getting-started.md](docs/getting-started.md)** for a full walkthrough including prerequisites, troubleshooting, and ngrok setup for remote access.

---

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

### Hook Mode (original)

1. Claude Code finishes a turn and fires the **Stop hook**
2. The hook extracts Claude's last message from the transcript
3. It POSTs the message to the Go server as a pending prompt
4. The hook **blocks** and long-polls the server for your response
5. You see the prompt on your phone/browser and type a reply
6. The hook receives your response and returns `{"decision": "block", "reason": "User responded: ..."}` to Claude Code
7. Claude reads your response and continues working

**Instructions** can also be queued from the web UI or iOS app — they're delivered the next time Claude finishes a turn.

**Notifications** (e.g., "build succeeded") are fire-and-forget — they appear in the UI but don't block Claude.

### Managed Mode

1. Create a session in the web UI — pick a working directory and configure tool permissions
2. Type a message in the chat interface
3. The server spawns `claude -p "<message>" --session-id <uuid> --output-format stream-json`
4. NDJSON output streams to the browser in real-time via SSE
5. When Claude finishes, type another message — the server uses `--resume <uuid>` to continue the conversation
6. Hit **Stop** to interrupt mid-turn (sends SIGINT); the session can resume on the next message

**Key capabilities:**
- **Tool restrictions** — each session has an allowed-tools list (e.g., read-only: `Read,Glob,Grep`)
- **Turn limiting** — server counts assistant turns and sends SIGINT when the limit is hit
- **Budget caps** — `--max-budget-usd` passed to the CLI for cost control
- **Session resumption** — type `/resume` in the chat to pick up a previous Claude Code CLI session (see below)
- **Session names** — assign custom names to sessions for easier identification (see below)

### Resuming Previous Sessions

The `/resume` command in the web UI lets you continue any previous Claude Code CLI session from the current project:

1. Type `/resume` in a managed session's chat input
2. The server reads Claude Code's native session index (`~/.claude/projects/<encoded-cwd>/sessions-index.json`)
3. A picker shows recent sessions with their summary, first prompt, branch, and message count
4. Select a session — the managed session switches to use `--resume <chosen-uuid>` for subsequent messages
5. Continue the conversation where the CLI session left off

### Session Names

Sessions can be given custom names for easier identification — especially useful when you have multiple sessions in the same project or want to quickly find one to `/resume`.

**Renaming in the web UI:**
- Double-click a session name in the sidebar or the header to edit it inline
- Press **Enter** to save, **Escape** to cancel
- Clear the name (empty string) to revert to the default computed name

**Renaming via API:**
```bash
curl -X PUT http://localhost:8080/api/sessions/<id>/name \
  -H "Authorization: Bearer <api-key>" \
  -H "Content-Type: application/json" \
  -d '{"name": "my session name"}'
```

When no custom name is set, sessions display a computed fallback: `computer_name / project` for hook-mode sessions, or the working directory name for managed sessions.

### Shortcuts

Configurable shortcuts map short keys (emoji or text abbreviations) to full messages. Useful for common responses you send frequently.

**Setting up shortcuts:**
1. Open Settings (gear icon in the sidebar)
2. Expand the **Shortcuts** accordion section
3. Add shortcuts with a key (max 20 characters) and the full message value
4. Save — shortcuts are stored server-side and available across all devices

**Using shortcuts:**
- Click the **😁** button in the chat input to open the shortcut picker, then click a shortcut to send it
- Or type the shortcut key directly in the text area and press Enter — if the entire message matches a shortcut key, the full value is sent instead

A default shortcut (👍 → "👍 Looks Good To Me") is available on first use and can be customized or removed.

**Shortcuts via API:**
```bash
# Get current shortcuts (included in settings response)
curl http://localhost:8080/api/settings \
  -H "Authorization: Bearer <api-key>"

# Update shortcuts
curl -X PUT http://localhost:8080/api/settings \
  -H "Authorization: Bearer <api-key>" \
  -H "Content-Type: application/json" \
  -d '{"shortcuts": [{"key": "👍", "value": "👍 Looks Good To Me"}, {"key": "🚀", "value": "Ship it!"}]}'
```

## Server

```bash
cd server && go build -o claude-controller .   # Build
cd server && go test ./... -v                  # Run all tests
cd server && go run .                          # Run (default :8080)
cd server && go run . --port 9090              # Custom port
```

### API Endpoints

**Hook-mode endpoints:**

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| POST | `/api/sessions/register` | Hook | Register/upsert a session |
| POST | `/api/sessions/:id/heartbeat` | Hook | Keep session active |
| POST | `/api/prompts` | Hook | Submit a prompt |
| GET | `/api/prompts/:id/response` | Hook | Long-poll for response (30s timeout) |

**Managed session endpoints:**

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| POST | `/api/sessions/create` | Web UI | Create a managed session (cwd, tools, budget) |
| POST | `/api/sessions/:id/message` | Web UI | Send a message (spawns `claude -p`) |
| POST | `/api/sessions/:id/interrupt` | Web UI | SIGINT the running process |
| DELETE | `/api/sessions/:id` | Web UI | Tear down a session |
| GET | `/api/sessions/:id/stream` | Web UI | SSE stream of live NDJSON output |
| GET | `/api/sessions/:id/messages` | Web UI | Fetch full message history |
| GET | `/api/sessions/:id/resumable` | Web UI | List resumable CLI sessions for this project |
| POST | `/api/sessions/:id/resume` | Web UI | Switch to resume a specific CLI session |

**Shared endpoints:**

| Method | Path | Caller | Purpose |
|--------|------|--------|---------|
| PUT | `/api/sessions/:id/name` | Web UI / iOS | Rename a session |
| GET | `/api/sessions` | Web UI / iOS | List active sessions |
| GET | `/api/prompts?status=pending` | Web UI / iOS | Get pending prompts |
| POST | `/api/prompts/:id/respond` | Web UI / iOS | Send a response |
| POST | `/api/sessions/:id/instruct` | Web UI / iOS | Queue an instruction |
| GET | `/api/events` | Web UI | SSE stream for global state updates |
| GET | `/api/settings` | Web UI / iOS | Get server settings (includes shortcuts) |
| PUT | `/api/settings` | Web UI / iOS | Update server settings (includes shortcuts) |
| GET | `/api/pairing` | iOS | Validate pairing |
| GET | `/api/status` | Any | Health check |

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

## Scheduled Tasks

Run shell commands or Claude prompts on a cron schedule, with output logging and a web UI for management.

### Creating a Task

In the web UI sidebar, click **+** next to "Scheduled Tasks" to open the create modal:

- **Name** — descriptive label (e.g., "Daily Backup")
- **Type** — `Shell Command` (runs `bash -c`) or `Claude Command` (runs `claude -p`)
- **Command** — the shell command or Claude prompt to execute
- **Working Directory** — absolute path where the task runs
- **Cron Expression** — standard 5-field cron (`minute hour dom month dow`). Presets available: hourly, daily 9am, weekdays, every 5min.

### Task Types

| Type | Execution | Example |
|------|-----------|---------|
| Shell | `bash -c "<command>"` in working directory | `tar -czf backup.tar.gz ./data` |
| Claude | `claude -p "<prompt>"` in working directory | `Summarize recent git commits` |

### Viewing Runs

Click a task in the sidebar to see its recent runs (last 20). Each run shows:
- Status (success/failed/running)
- Exit code
- Truncated stdout+stderr output (last 10KB)
- Relative timestamp and duration

### Manual Trigger

Click the **▶** button on any task to run it immediately, bypassing the cron schedule.

### API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/tasks` | Create a task |
| GET | `/api/tasks` | List all tasks (optional `?session_id=` filter) |
| GET | `/api/tasks/:taskId` | Get a task |
| PUT | `/api/tasks/:taskId` | Update a task |
| DELETE | `/api/tasks/:taskId` | Delete a task and its runs |
| GET | `/api/tasks/:taskId/runs` | List recent runs (last 20) |
| POST | `/api/tasks/:taskId/trigger` | Run a task immediately |

### Scheduler Details

- Tasks are checked every 30 seconds
- Each task runs with a 1-hour timeout
- The same task won't run concurrently (skipped if previous run is still going)
- On server restart: missed tasks within 5 minutes are executed; older missed tasks are rescheduled
- Stale runs (server crashed mid-execution) are marked as failed on startup
- Cron expressions use the server's local timezone

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
