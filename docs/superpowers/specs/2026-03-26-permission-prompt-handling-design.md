# Permission Prompt Handling for Managed Sessions

**Date:** 2026-03-26
**Issue:** #58 — Allow Claude to edit its own settings in the current Repo
**Status:** Design

## Problem

When Claude Code runs in managed mode (`claude -p`), it sometimes needs user permission to execute tools (e.g., Bash commands, file edits outside the allowed list) or presents multi-choice questions requiring user input. In the terminal CLI, these appear as interactive prompts with selectable options. In the web UI's managed sessions, there is no mechanism to detect or respond to these prompts — the process blocks indefinitely waiting for input that never comes.

## Solution: MCP Permission Prompt Tool

Use Claude Code's official `--permission-prompt-tool` flag to delegate permission prompts to an MCP tool. The MCP tool is an embedded subcommand of the Go server binary (`claude-controller mcp-bridge`) that forwards permission requests to the main Go server via HTTP. The Go server broadcasts the request to the web UI via SSE, the user responds via a modal, and the response flows back through the chain.

## Architecture

### Data Flow

```
Claude -p (needs permission)
  → calls MCP tool "permission_prompt"
    → MCP bridge (embedded Go subcommand, stdio)
      → HTTP POST to Go server: POST /api/sessions/:id/permission-request
        → Go server stores pending permission, broadcasts SSE "input_request"
          → Browser receives SSE, shows modal
            → User clicks Allow / Deny / Allow Always
              → Browser POSTs to /api/sessions/:id/permission-respond
                → Go server unblocks the permission-request handler
                  → HTTP response to MCP bridge
                    → MCP bridge returns JSON-RPC response to Claude
                      → Claude proceeds with the decision
```

### Components

#### 1. MCP Bridge (Embedded Subcommand)

**Invocation:** `claude-controller mcp-bridge --session-id <id> --port <port>`

Implements the MCP stdio protocol (JSON-RPC 2.0 over stdin/stdout):

- **`initialize`** — returns server info and capabilities
- **`tools/list`** — returns a single tool: `permission_prompt`
- **`tools/call`** for `permission_prompt` — forwards the permission request to the Go server and blocks until the user responds

The `permission_prompt` tool receives the following input from Claude:
- `tool_name` (string): the tool requesting permission (e.g., "Bash", "Edit")
- `description` (string): human-readable description of what the tool wants to do
- `input` (object): the tool's input parameters (command, file_path, etc.)

On receiving a call, the bridge:
1. POSTs to `http://localhost:<port>/api/sessions/<session-id>/permission-request`
2. Blocks waiting for the HTTP response (up to 5 minutes)
3. Returns the decision as the tool call result

#### 2. Go Server — New Endpoints

**`POST /api/sessions/:id/permission-request`** (called by MCP bridge)

- Receives: `{"tool_name": "Bash", "description": "Run echo hello", "input": {"command": "echo hello"}}`
- Creates a `pendingPermission` entry for the session (in-memory, mutex-protected)
- Broadcasts an `input_request` SSE event to all subscribers:
  ```json
  {
    "type": "input_request",
    "tool_name": "Bash",
    "description": "Run echo hello",
    "input": {"command": "echo hello"}
  }
  ```
- Updates activity state to `"input_needed"`
- **Blocks** on a response channel (buffered chan of size 1)
- On response or timeout (5 minutes): returns the decision to the MCP bridge
- On timeout: returns `{"decision": "deny", "reason": "timeout"}`

**`POST /api/sessions/:id/permission-respond`** (called by frontend)

- Receives: `{"decision": "allow" | "deny" | "allow_always"}`
- Writes the decision to the pending channel, unblocking the permission-request handler
- Clears `pendingPermission` for the session
- Updates activity state back to `"working"`
- Returns 404 if no pending permission request for the session
- Returns 200 on success

#### 3. Pending Permission State

In-memory state on the Go server, per session:

```go
type PendingPermission struct {
    ToolName    string          `json:"tool_name"`
    Description string          `json:"description"`
    Input       json.RawMessage `json:"input"`
    ResponseCh  chan string     // buffered, size 1
    CreatedAt   time.Time
}
```

Stored in a `map[string]*PendingPermission` on the `Server` struct (or a dedicated manager), protected by a mutex. Entries are created when the MCP bridge calls `permission-request` and removed when the user responds or the request times out.

On SSE reconnection, the frontend can check for a pending permission by hitting the existing session state endpoint (or a new `GET /api/sessions/:id/pending-permission` endpoint).

#### 4. Spawn Changes

When spawning `claude -p`, generate a temporary MCP config file:

```json
{
  "mcpServers": {
    "controller": {
      "command": "<path-to-claude-controller-binary>",
      "args": ["mcp-bridge", "--session-id", "<session-id>", "--port", "<server-port>"]
    }
  }
}
```

Write to a temp file (e.g., `/tmp/claude-mcp-<session-id>.json`). Clean up on process exit.

Add to claude args:
- `--permission-prompt-tool mcp__controller__permission_prompt`
- `--mcp-config <temp-file-path>`

The binary path is determined at startup (via `os.Executable()`).

#### 5. Frontend — Permission Modal

**SSE Handler (`app.js`):**

Add a case for `type: "input_request"` events:
```javascript
case 'input_request':
    this.pendingPermission = data;
    break;
```

**Modal UI (`index.html`):**

An overlay modal shown when `pendingPermission !== null`:

- **Header:** "Permission Required" (or "Input Required" for unstructured)
- **Body:** Tool name, description, and input context (e.g., the Bash command, the file path and edit details)
- **Actions for permission prompts:**
  - "Allow" button → `{"decision": "allow"}`
  - "Deny" button → `{"decision": "deny"}`
  - "Allow Always" button → `{"decision": "allow_always"}`
- **Auto-clear:** On receiving a `done` SSE event (process died), clear the modal

**Activity State:**

- New `"input_needed"` state alongside existing `working` / `waiting` / `idle`
- Visual indicator: orange pulsing dot + "Waiting for permission" banner in the session header
- The existing activity pill system continues showing tool context

#### 6. Activity State: `input_needed`

Add `"input_needed"` as a valid value for the `activity_state` field in the sessions table. Frontend handles this with:
- Orange pulsing dot in session list
- Banner in chat view: "Claude is waiting for your permission"
- The permission modal itself

On process exit or user response, state transitions back to `"working"` (if responded) or `"idle"` (if process died).

## Error Handling

| Scenario | Behavior |
|----------|----------|
| User doesn't respond within 5 minutes | MCP bridge receives timeout → returns deny to Claude |
| Process dies while waiting for permission | `done` SSE event clears the modal; pending permission entry cleaned up |
| Browser reconnects mid-permission | Frontend checks `GET /api/sessions/:id/pending-permission` on SSE connect |
| Multiple rapid permission requests | Sequential — Claude blocks on each, only one pending at a time |
| MCP bridge can't reach Go server | Bridge returns error to Claude; Claude handles gracefully |
| Server restarts while permission pending | Pending permissions are in-memory, so they're lost; process will time out |

## Implementation Note: Schema Discovery

The exact input/output schema for `--permission-prompt-tool` MCP calls has not been verified empirically. The schema described in this spec (tool_name, description, input fields; allow/deny/allow_always response values) is based on the documented behavior and reasonable inference. During implementation, the first task should be to trigger an actual permission prompt with `--permission-prompt-tool` pointing to a logging script, capture the exact JSON-RPC payload Claude sends, and adjust the MCP bridge accordingly.

## Testing Strategy

1. **Unit tests:** MCP bridge JSON-RPC parsing, permission request/respond endpoint logic
2. **Integration test:** Spawn a mock process that calls the permission endpoint, verify the blocking/unblocking flow
3. **Manual test:** Run a managed session, trigger a Bash command that needs permission, verify modal appears and response flows back

## Future: PTY Approach (Documented for Later)

For a future iteration, spawning Claude in a pseudo-terminal (PTY) would give full terminal emulation — handling colored output, interactive prompts, cursor control, and escape sequences natively. This would be more robust for edge cases (e.g., prompts that don't use the MCP permission tool) but significantly more complex:

- Requires terminal escape sequence parsing (ANSI codes, cursor positioning)
- Screen buffer management to extract readable text
- Fragile — dependent on Claude Code's terminal rendering, which can change
- Platform-specific PTY handling (different on macOS vs Linux)

The MCP-based approach handles the primary use case (permission prompts) cleanly. PTY could be explored later for full terminal parity if needed.

## Files to Modify / Create

| File | Change |
|------|--------|
| `server/main.go` | Add `mcp-bridge` subcommand dispatch |
| `server/mcp/bridge.go` (new) | MCP stdio bridge implementation |
| `server/managed/manager.go` | Store binary path; generate MCP config on spawn |
| `server/api/managed_sessions.go` | New permission-request/respond endpoints; spawn arg changes |
| `server/api/server.go` | Register new routes; add pending permission state |
| `server/db/db.go` | Add `input_needed` to activity state migration (if validated) |
| `server/web/static/app.js` | SSE handler for `input_request`; permission modal logic |
| `server/web/static/index.html` | Permission modal template; activity state styling |
