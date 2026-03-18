# Web UI Design Spec

Replace the native iOS app with a browser-based UI served directly by the Go server. This makes the remote control device-agnostic — any phone, tablet, or desktop browser can manage Claude Code sessions.

## Motivation

The iOS app adds build complexity (Xcode, signing, App Store) and limits access to Apple devices. A web UI eliminates all of that. The Go server already runs an HTTP server, so serving static files is trivial. The existing REST API stays unchanged; the web UI is a new consumer alongside the hooks.

## V1 Scope

**In scope:**
- Login screen (paste API key)
- Session list with status indicators
- Prompt cards with inline response input
- Instruction sending
- Server-Sent Events for real-time updates
- Responsive layout (desktop sidebar + mobile stacked)

**Out of scope (deferred):**
- Multi-server management
- Session archiving UI
- QR code scanning
- Service worker / offline support
- Any frontend build step (bundler, transpiler)

## Architecture

### New Code

- `server/web/` — Go package that embeds and serves static files
- `server/web/static/index.html` — single HTML file with login and dashboard views
- `server/web/static/style.css` — responsive styles
- `server/web/static/app.js` — Alpine.js application logic + SSE client
- `server/web/static/alpine.min.js` — vendored Alpine.js (~17kb)
- `GET /api/events` — new SSE endpoint streaming session/prompt state

### Existing Code Changes

- `server/api/router.go` — restructure routing: static files and SSE endpoint mount outside auth middleware, API endpoints stay behind auth middleware
- No changes to hooks, DB schema, tunnel, or existing API endpoints

### Technology Choices

- **Alpine.js** (~17kb) — lightweight reactive framework loaded via vendored script. Adds reactivity through HTML attributes (`x-data`, `x-for`, `x-show`, `x-on`). No build step required. Handles DOM updates, conditional rendering, and list rendering declaratively instead of manual DOM manipulation.
- **Embedded files** — Go's `embed` package bundles static files (including vendored Alpine.js) into the binary. No separate file serving or build step.
- **SSE over WebSockets** — the browser mostly receives updates; SSE is simpler, built into Go stdlib and all browsers, and auto-reconnects.

## Auth Flow

1. Browser loads `/` — serves `index.html`
2. JS checks `localStorage` for saved API key
3. If no key: show login screen with a text input for the API key
4. User pastes key from terminal output, clicks "Connect"
5. JS calls `GET /api/status` with `Authorization: Bearer <key>` to validate
6. On 200: save key to `localStorage`, show dashboard
7. On 401: show error, stay on login screen
8. All subsequent API calls include the `Bearer` token
9. On any 401 response: clear `localStorage`, redirect to login

## SSE Endpoint

### `GET /api/events`

Requires `Authorization: Bearer <key>` header (same auth as all other endpoints).

**Connection flow:**
1. Server accepts connection, sets `Content-Type: text/event-stream`
2. Immediately sends full state snapshot
3. Every 3 seconds, sends updated state
4. If connection drops, browser's `EventSource` auto-reconnects

**Event format:**
```
event: update
data: {"sessions": [...], "prompts": [...]}

```

- `sessions` — all active (non-archived) sessions, same shape as `GET /api/sessions`
- `prompts` — all prompts (pending first, then recent answered), same shape as `GET /api/prompts`

**Why full state, not diffs:** The state is small (handful of sessions, dozens of prompts). Sending full state on each tick keeps the client simple — no diff reconciliation, no out-of-order issues. If state grows, we can optimize later.

**Auth for EventSource:** The browser's `EventSource` API doesn't support custom headers. The SSE endpoint will accept the API key as a query parameter: `GET /api/events?token=<key>`. This is acceptable because the connection is localhost or ngrok (HTTPS). The query param auth is SSE-only; all other endpoints continue using the `Authorization` header. The SSE handler validates the token internally (not through AuthMiddleware) since it's mounted outside the auth middleware chain.

**Cleanup:** The SSE handler must respect `r.Context().Done()` to detect client disconnects and avoid leaking goroutines. When the client disconnects, stop the ticker and return.

## UI Layout

### Desktop (>768px)

```
+------------------+----------------------------------------+
|                  |                                        |
|  Sessions        |  Prompts                               |
|  -----------     |  +----------------------------------+  |
|  > Session 1  *  |  | Claude: Which DB should we use?  |  |
|    Session 2     |  | [response input] [Send]          |  |
|    Session 3     |  +----------------------------------+  |
|                  |  +----------------------------------+  |
|                  |  | Claude: I'll use SQLite then.     |  |
|                  |  | Replied: "SQLite"                 |  |
|                  |  +----------------------------------+  |
|                  |                                        |
|                  +----------------------------------------+
|                  | Send Instruction                       |
|                  | [instruction input]          [Send]    |
+------------------+----------------------------------------+
```

- Sidebar: ~250px fixed width, scrollable session list
- Main area: prompt cards, scrollable
- Instruction input: sticky at bottom of main area, only visible when a session is selected

### Mobile (<768px)

```
+----------------------------------------+
| [Session Dropdown v]                   |
+----------------------------------------+
| Claude: Which DB should we use?        |
| [response input] [Send]               |
+----------------------------------------+
| Claude: I'll use SQLite then.          |
| Replied: "SQLite"                      |
+----------------------------------------+
| [instruction input]          [Send]    |
+----------------------------------------+
```

- Session selector becomes a dropdown at the top
- Prompt cards stack vertically
- Instruction input sticky at bottom

### Visual Design

- Minimal, clean. Dark and light mode via `prefers-color-scheme`.
- Status dots: green = waiting (Claude needs input), yellow = active (Claude is working), gray = idle/stale (no heartbeat for 5+ min)
- Pending prompt cards have a subtle green highlight
- Answered prompts show response in muted text
- Notification-type prompts show message only, no response input

## Component Behavior

### Login Screen

- Single centered card with: title, API key input (password-masked with toggle), "Connect" button
- Shows server URL being connected to (derived from current page URL)
- Error message area for failed auth
- "Disconnect" option in dashboard header to return to login

### Session List / Selector

- Shows `computerName / projectName` (last path segment) for each session
- Status dot next to each session
- "All Sessions" option to view prompts across all sessions
- Pending prompt count badge per session
- Click to select; selected session highlighted

### Prompt Cards

- Ordered: pending first (newest on top), then answered (newest on top)
- **Pending prompt (type=prompt, status=pending):**
  - Green left border or subtle green background
  - Claude's message
  - Text input + "Send" button
  - After sending: card transitions to answered state
- **Answered prompt:**
  - Gray styling
  - Claude's message
  - "Replied: [response]" in muted text
  - Relative timestamp
- **Notification (type=notification):**
  - No response input
  - Shows message + relative timestamp

### Instruction Input

- Fixed at bottom of main area
- Text input + "Send" button
- Only enabled when a session is selected
- After sending: brief success indicator (checkmark or flash), input clears
- Helper text: "Delivered when Claude finishes its current turn"

### Real-time Updates

- SSE connection established on login
- On each `update` event: re-render session list and prompt cards
- If SSE disconnects: show subtle "Reconnecting..." indicator, auto-retry
- Fallback: if SSE fails 3 times, fall back to 5-second polling via `GET /api/sessions` + `GET /api/prompts`

## Error Handling

- **Network errors:** Toast/banner at top: "Connection lost. Reconnecting..."
- **Auth errors (401):** Clear stored key, return to login
- **Rate limit (429):** Show "Too many requests, slow down" message
- **Send failures:** Show error inline on the prompt card or instruction input that failed

## Routing & Middleware Structure

The current router wraps all routes in `RateLimiter → AuthMiddleware → mux`. The web UI requires a split:

```
root mux
├── /api/events     → SSE handler (validates token from query param internally)
├── /api/*          → RateLimiter → AuthMiddleware → API mux (existing endpoints)
└── /*              → static file server (no auth, serves index.html + assets)
```

Static files must be served without auth so the browser can load `index.html` before the user has authenticated. The SSE endpoint handles its own auth via query parameter. All other `/api/*` routes go through the existing middleware chain.

`NewRouter` returns this root mux. The caller (`main.go`) doesn't change.

## Default View

On first load after login, the dashboard shows "All Sessions" with all prompts. The most natural starting point — the user sees everything and can drill into a specific session.

## File Structure

```
server/
  web/
    web.go          # Go handler: embed + serve static files
    static/
      index.html      # Single HTML file
      style.css       # Responsive styles
      app.js          # Alpine.js application logic + SSE
      alpine.min.js   # Vendored Alpine.js (~17kb)
  api/
    router.go       # Modified: restructure routing for split middleware
    events.go       # New: SSE endpoint handler
```
