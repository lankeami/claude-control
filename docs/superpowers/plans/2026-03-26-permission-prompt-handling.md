# Permission Prompt Handling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable the web UI to handle permission prompts and input requests from Claude Code managed sessions via an MCP bridge that forwards prompts to the user.

**Architecture:** Embedded `mcp-bridge` subcommand in the Go binary acts as an MCP stdio server. When Claude hits a permission prompt, it calls the MCP tool, which POSTs to the Go server. The server blocks until the frontend user responds via a modal, then returns the decision through the chain.

**Tech Stack:** Go (MCP bridge + API endpoints), Alpine.js (frontend modal), JSON-RPC 2.0 (MCP protocol)

---

### Task 0: Schema Discovery

**Files:**
- Create: `server/mcp/log_bridge.sh` (temporary, deleted after task)

Before building anything, discover the exact JSON-RPC payload Claude Code sends to `--permission-prompt-tool`.

- [ ] **Step 1: Create a logging MCP server script**

```bash
#!/bin/bash
# Minimal MCP server that logs all stdin to a file and responds with allow
LOG="/tmp/mcp-bridge-log.jsonl"
while IFS= read -r line; do
  echo "$line" >> "$LOG"
  # Parse JSON-RPC method
  method=$(echo "$line" | python3 -c "import sys,json; print(json.load(sys.stdin).get('method',''))" 2>/dev/null)
  id=$(echo "$line" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
  if [ "$method" = "initialize" ]; then
    echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{\"tools\":{}},\"serverInfo\":{\"name\":\"log-bridge\",\"version\":\"0.1.0\"}}}"
  elif [ "$method" = "notifications/initialized" ]; then
    : # no response needed for notifications
  elif [ "$method" = "tools/list" ]; then
    echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"tools\":[{\"name\":\"permission_prompt\",\"description\":\"Handle permission prompts\",\"inputSchema\":{\"type\":\"object\"}}]}}"
  elif [ "$method" = "tools/call" ]; then
    echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"decision\\\":\\\"allow\\\"}\"}]}}"
  fi
done
```

- [ ] **Step 2: Test with Claude Code**

Create a temp MCP config and run Claude with the logging bridge:

```bash
chmod +x server/mcp/log_bridge.sh

cat > /tmp/mcp-log-config.json << 'EOF'
{
  "mcpServers": {
    "controller": {
      "command": "/Users/jaychinthrajah/workspaces/_personal_/claude-control/server/mcp/log_bridge.sh"
    }
  }
}
EOF

cd /tmp/claude-stdin-test && claude -p "run the command: echo hello world" \
  --output-format stream-json --verbose \
  --permission-prompt-tool mcp__controller__permission_prompt \
  --mcp-config /tmp/mcp-log-config.json 2>/dev/null | head -50
```

- [ ] **Step 3: Inspect the logged payloads**

```bash
cat /tmp/mcp-bridge-log.jsonl
```

Document the exact `tools/call` payload structure — particularly the `params.arguments` field names and values. Update the MCP bridge implementation in Task 1 if the schema differs from the spec's assumed `tool_name`/`description`/`input` fields.

- [ ] **Step 4: Clean up**

```bash
rm server/mcp/log_bridge.sh /tmp/mcp-log-config.json /tmp/mcp-bridge-log.jsonl
```

---

### Task 1: MCP Bridge — Embedded Subcommand

**Files:**
- Create: `server/mcp/bridge.go`
- Create: `server/mcp/bridge_test.go`
- Modify: `server/main.go:28-31` (add subcommand dispatch before flag.Parse)

- [ ] **Step 1: Write the failing test for JSON-RPC message parsing**

```go
// server/mcp/bridge_test.go
package mcp

import "testing"

func TestParseRequest(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" {
		t.Errorf("method=%s, want initialize", req.Method)
	}
	if req.ID != 1 {
		t.Errorf("id=%v, want 1", req.ID)
	}
}

func TestParseRequestNotification(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "notifications/initialized" {
		t.Errorf("method=%s, want notifications/initialized", req.Method)
	}
	if req.ID != 0 {
		t.Errorf("id=%v, want 0 for notification", req.ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./mcp/ -v -run TestParseRequest`
Expected: FAIL — package `mcp` does not exist yet

- [ ] **Step 3: Implement the MCP bridge**

```go
// server/mcp/bridge.go
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result"`
}

func parseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Run starts the MCP stdio bridge. It reads JSON-RPC requests from stdin,
// handles MCP protocol messages, and forwards permission_prompt tool calls
// to the Go server via HTTP.
func Run(sessionID string, serverPort int) error {
	baseURL := fmt.Sprintf("http://localhost:%d", serverPort)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		req, err := parseRequest(line)
		if err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":     map[string]interface{}{"name": "claude-controller-bridge", "version": "1.0.0"},
			})

		case "notifications/initialized":
			// No response for notifications
			continue

		case "tools/list":
			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "permission_prompt",
						"description": "Handle permission prompts from Claude Code",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			})

		case "tools/call":
			var params ToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeToolError(os.Stdout, req.ID, "invalid params")
				continue
			}
			if params.Name != "permission_prompt" {
				writeToolError(os.Stdout, req.ID, "unknown tool: "+params.Name)
				continue
			}

			decision, err := forwardPermissionRequest(baseURL, sessionID, params.Arguments)
			if err != nil {
				writeToolError(os.Stdout, req.ID, "server error: "+err.Error())
				continue
			}

			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": decision},
				},
			})
		}
	}

	return scanner.Err()
}

func forwardPermissionRequest(baseURL, sessionID string, arguments json.RawMessage) (string, error) {
	url := fmt.Sprintf("%s/api/sessions/%s/permission-request", baseURL, sessionID)

	client := &http.Client{Timeout: 6 * time.Minute} // longer than server's 5min timeout
	resp, err := client.Post(url, "application/json", bytes.NewReader(arguments))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func writeResponse(w io.Writer, id int, result interface{}) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "%s\n", data)
}

func writeToolError(w io.Writer, id int, message string) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": message},
			},
			"isError": true,
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "%s\n", data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./mcp/ -v -run TestParseRequest`
Expected: PASS

- [ ] **Step 5: Write test for tool call params parsing**

```go
// Add to server/mcp/bridge_test.go
func TestParseToolCallParams(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"permission_prompt","arguments":{"tool_name":"Bash","command":"echo hello"}}}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Name != "permission_prompt" {
		t.Errorf("name=%s, want permission_prompt", params.Name)
	}
	if len(params.Arguments) == 0 {
		t.Error("expected non-empty arguments")
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd server && go test ./mcp/ -v`
Expected: PASS

- [ ] **Step 7: Add subcommand dispatch to main.go**

Add before the existing `flag.Parse()` call in `server/main.go`:

```go
// At the top of main(), before flag.Parse():
if len(os.Args) >= 2 && os.Args[1] == "mcp-bridge" {
    // Parse mcp-bridge specific flags
    bridgeFlags := flag.NewFlagSet("mcp-bridge", flag.ExitOnError)
    sessionID := bridgeFlags.String("session-id", "", "session ID")
    port := bridgeFlags.Int("port", 8080, "server port")
    bridgeFlags.Parse(os.Args[2:])
    if *sessionID == "" {
        log.Fatal("--session-id is required")
    }
    if err := mcp.Run(*sessionID, *port); err != nil {
        log.Fatalf("mcp-bridge error: %v", err)
    }
    return
}
```

Add import: `"github.com/jaychinthrajah/claude-controller/server/mcp"`

- [ ] **Step 8: Verify build**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add server/mcp/bridge.go server/mcp/bridge_test.go server/main.go
git commit -m "feat: add MCP bridge subcommand for permission prompt handling"
```

---

### Task 2: Permission Request/Respond Endpoints

**Files:**
- Create: `server/api/permissions.go`
- Create: `server/api/permissions_test.go`
- Modify: `server/api/router.go:11-15` (add permissions field to Server struct)
- Modify: `server/api/router.go:55-60` (register new routes)

- [ ] **Step 1: Write the failing test for permission-request endpoint**

```go
// server/api/permissions_test.go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPermissionRequestAndRespond(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	// Start permission request in background (it blocks)
	type result struct {
		status int
		body   string
	}
	ch := make(chan result, 1)
	go func() {
		body := `{"tool_name":"Bash","description":"Run echo hello","input":{"command":"echo hello"}}`
		req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-request", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-api-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- result{0, err.Error()}
			return
		}
		defer resp.Body.Close()
		var respBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&respBody)
		b, _ := json.Marshal(respBody)
		ch <- result{resp.StatusCode, string(b)}
	}()

	// Wait a moment for the request to register
	time.Sleep(100 * time.Millisecond)

	// Respond with allow
	respondBody := `{"decision":"allow"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-respond", strings.NewReader(respondBody))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("respond status=%d, want 200", resp.StatusCode)
	}

	// Check the blocked request completed
	select {
	case r := <-ch:
		if r.status != 200 {
			t.Fatalf("permission-request status=%d, want 200, body=%s", r.status, r.body)
		}
		if !strings.Contains(r.body, "allow") {
			t.Errorf("expected allow in response, got %s", r.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("permission-request did not complete after respond")
	}
}

func TestPermissionRespondNoRequest(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	body := `{"decision":"allow"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-respond", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status=%d, want 404 when no pending request", resp.StatusCode)
	}
}

func TestPermissionPendingEndpoint(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	// No pending permission initially
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/pending-permission", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["pending"] != false {
		t.Errorf("expected pending=false, got %v", result["pending"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestPermission`
Expected: FAIL — handlers don't exist yet

- [ ] **Step 3: Implement permissions.go**

```go
// server/api/permissions.go
package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// PendingPermission represents a permission request waiting for user response.
type PendingPermission struct {
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	ResponseCh  chan string     `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
}

// PermissionManager tracks pending permission requests per session.
type PermissionManager struct {
	mu      sync.Mutex
	pending map[string]*PendingPermission // sessionID → pending request
}

func NewPermissionManager() *PermissionManager {
	return &PermissionManager{
		pending: make(map[string]*PendingPermission),
	}
}

func (pm *PermissionManager) Set(sessionID string, p *PendingPermission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pending[sessionID] = p
}

func (pm *PermissionManager) Get(sessionID string) *PendingPermission {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.pending[sessionID]
}

func (pm *PermissionManager) Delete(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.pending, sessionID)
}

const permissionTimeout = 5 * time.Minute

func (s *Server) handlePermissionRequest(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		ToolName    string          `json:"tool_name"`
		Description string          `json:"description"`
		Input       json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	pending := &PendingPermission{
		ToolName:    req.ToolName,
		Description: req.Description,
		Input:       req.Input,
		ResponseCh:  make(chan string, 1),
		CreatedAt:   time.Now(),
	}

	s.permissions.Set(sessionID, pending)
	_ = s.store.UpdateActivityState(sessionID, "input_needed")

	// Broadcast input_request SSE event
	broadcaster := s.manager.GetBroadcaster(sessionID)
	eventJSON, _ := json.Marshal(map[string]interface{}{
		"type":        "input_request",
		"tool_name":   req.ToolName,
		"description": req.Description,
		"input":       req.Input,
	})
	broadcaster.Send(string(eventJSON))

	// Block until response or timeout
	select {
	case decision := <-pending.ResponseCh:
		s.permissions.Delete(sessionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"decision": decision})
	case <-time.After(permissionTimeout):
		s.permissions.Delete(sessionID)
		_ = s.store.UpdateActivityState(sessionID, "working")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"decision": "deny", "reason": "timeout"})
	case <-r.Context().Done():
		s.permissions.Delete(sessionID)
		return
	}
}

func (s *Server) handlePermissionRespond(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Decision == "" {
		http.Error(w, "decision is required", http.StatusBadRequest)
		return
	}

	pending := s.permissions.Get(sessionID)
	if pending == nil {
		http.Error(w, "no pending permission request", http.StatusNotFound)
		return
	}

	pending.ResponseCh <- req.Decision
	_ = s.store.UpdateActivityState(sessionID, "working")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handlePendingPermission(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	pending := s.permissions.Get(sessionID)

	w.Header().Set("Content-Type", "application/json")
	if pending == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"pending": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pending":     true,
		"tool_name":   pending.ToolName,
		"description": pending.Description,
		"input":       pending.Input,
		"created_at":  pending.CreatedAt,
	})
}
```

- [ ] **Step 4: Add PermissionManager to Server struct and routes**

In `server/api/router.go`, add the `permissions` field to the `Server` struct:

```go
type Server struct {
	store       *db.Store
	manager     *managed.Manager
	envPath     string
	permissions *PermissionManager
}
```

Update `NewRouter` to initialize it:

```go
s := &Server{store: store, manager: mgr, envPath: envPath, permissions: NewPermissionManager()}
```

Add routes in the managed session endpoints section:

```go
apiMux.HandleFunc("POST /api/sessions/{id}/permission-request", s.handlePermissionRequest)
apiMux.HandleFunc("POST /api/sessions/{id}/permission-respond", s.handlePermissionRespond)
apiMux.HandleFunc("GET /api/sessions/{id}/pending-permission", s.handlePendingPermission)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestPermission`
Expected: PASS (all 3 tests)

- [ ] **Step 6: Run all existing tests to verify no regressions**

Run: `cd server && go test ./... -v`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add server/api/permissions.go server/api/permissions_test.go server/api/router.go
git commit -m "feat: add permission request/respond endpoints with blocking channel pattern"
```

---

### Task 3: Spawn Changes — MCP Config Generation

**Files:**
- Modify: `server/managed/manager.go:13-17` (add ServerPort and BinaryPath to Config)
- Modify: `server/api/managed_sessions.go:57-86` (add MCP config args to buildClaudeArgs)
- Modify: `server/main.go:71-76` (pass port and binary path to managed config)

- [ ] **Step 1: Add ServerPort and BinaryPath to managed.Config**

In `server/managed/manager.go`, update the Config struct:

```go
type Config struct {
	ClaudeBin  string
	ClaudeArgs []string
	ClaudeEnv  []string
	ServerPort int
	BinaryPath string
}
```

- [ ] **Step 2: Add MCP config file generation to buildClaudeArgs**

In `server/api/managed_sessions.go`, update `buildClaudeArgs` to accept the manager config and generate MCP args:

```go
func buildClaudeArgs(sess *db.Session, message string, cfg managed.Config) []string {
	var args []string
	args = append(args, "-p", message)

	resumeID := sess.ID
	if sess.ClaudeSessionID != "" {
		resumeID = sess.ClaudeSessionID
	}
	if sess.Initialized {
		args = append(args, "--resume", resumeID)
	} else {
		args = append(args, "--session-id", resumeID)
	}

	args = append(args, "--output-format", "stream-json", "--verbose")

	if sess.AllowedTools != "" {
		var tools []string
		json.Unmarshal([]byte(sess.AllowedTools), &tools)
		if len(tools) > 0 {
			args = append(args, "--allowedTools", strings.Join(tools, ","))
		}
	}

	if sess.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", sess.MaxBudgetUSD))
	}

	// Add MCP permission prompt config if binary path is available
	if cfg.BinaryPath != "" && cfg.ServerPort > 0 {
		mcpConfig := map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"controller": map[string]interface{}{
					"command": cfg.BinaryPath,
					"args":   []string{"mcp-bridge", "--session-id", sess.ID, "--port", fmt.Sprintf("%d", cfg.ServerPort)},
				},
			},
		}
		mcpJSON, err := json.Marshal(mcpConfig)
		if err == nil {
			tmpFile := fmt.Sprintf("/tmp/claude-mcp-%s.json", sess.ID)
			if os.WriteFile(tmpFile, mcpJSON, 0644) == nil {
				args = append(args, "--permission-prompt-tool", "mcp__controller__permission_prompt")
				args = append(args, "--mcp-config", tmpFile)
			}
		}
	}

	return args
}
```

Add `"os"` to the imports at the top of `managed_sessions.go`.

- [ ] **Step 3: Update all buildClaudeArgs call sites**

In `handleSendMessage`, update the call to pass the manager config. The `Server` struct already has `s.manager` — we need to expose the config. Add a getter to the Manager:

In `server/managed/manager.go`, add:

```go
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}
```

Then in `server/api/managed_sessions.go`, update the call in `handleSendMessage`:

```go
args := buildClaudeArgs(sess, currentMessage, s.manager.Config())
```

- [ ] **Step 4: Set ServerPort and BinaryPath in main.go**

In `server/main.go`, after port is determined and before `managed.NewManager`:

```go
binaryPath, _ := os.Executable()
managedCfg := managed.Config{
	ClaudeBin:  envOrDefault("CLAUDE_BIN", "claude"),
	ClaudeArgs: strings.Fields(os.Getenv("CLAUDE_ARGS")),
	ClaudeEnv:  splitEnv(os.Getenv("CLAUDE_ENV")),
	ServerPort: *port,
	BinaryPath: binaryPath,
}
```

- [ ] **Step 5: Clean up temp MCP config on process exit**

In `handleSendMessage`, after `<-proc.Done`, add cleanup:

```go
// Clean up temp MCP config file
tmpFile := fmt.Sprintf("/tmp/claude-mcp-%s.json", sessionID)
os.Remove(tmpFile)
```

Add this right after `<-proc.Done` in the goroutine (around line 202).

- [ ] **Step 6: Verify build and tests pass**

Run: `cd server && go build -o claude-controller . && go test ./... -v`
Expected: Build succeeds, all tests pass

- [ ] **Step 7: Commit**

```bash
git add server/managed/manager.go server/api/managed_sessions.go server/main.go
git commit -m "feat: generate MCP config and pass --permission-prompt-tool on spawn"
```

---

### Task 4: Frontend — Permission Modal UI

**Files:**
- Modify: `server/web/static/app.js:1321-1485` (SSE handler — add input_request case)
- Modify: `server/web/static/app.js` (add pendingPermission state and respondToPermission method)
- Modify: `server/web/static/index.html` (add permission modal template)
- Modify: `server/web/static/style.css:191-202` (add input_needed status dot style)

- [ ] **Step 1: Add pendingPermission state to Alpine.js data**

In `server/web/static/app.js`, find the Alpine data object (the `return {` block) and add:

```javascript
pendingPermission: null,
```

- [ ] **Step 2: Add SSE handler for input_request events**

In the `startSessionSSE` method's `onmessage` handler, add after the `auto_continue_exhausted` block (around line 1389) and before the `done` check:

```javascript
if (data.type === 'input_request') {
    this.pendingPermission = data;
    return;
}
```

Also, in the `done` handler (around line 1405), clear the pending permission:

```javascript
if (data.type === 'done') {
    this.pendingPermission = null;
    this.stopSessionSSE();
    return;
}
```

- [ ] **Step 3: Add respondToPermission method**

Add a new method to the Alpine component:

```javascript
async respondToPermission(decision) {
    if (!this.pendingPermission || !this.selectedSessionId) return;
    try {
        await fetch(`/api/sessions/${this.selectedSessionId}/permission-respond`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.apiKey}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ decision })
        });
    } catch (e) {
        console.error('Failed to respond to permission:', e);
    }
    this.pendingPermission = null;
},
```

- [ ] **Step 4: Check for pending permission on SSE reconnect**

In the `startSessionSSE` method, add a check right after creating the EventSource:

```javascript
// Check for pending permission on reconnect
fetch(`/api/sessions/${sessionId}/pending-permission`, {
    headers: { 'Authorization': `Bearer ${this.apiKey}` }
}).then(r => r.json()).then(data => {
    if (data.pending) {
        this.pendingPermission = data;
    }
}).catch(() => {});
```

- [ ] **Step 5: Add permission modal to index.html**

Add after the existing settings modal (around line 1070, before the closing `</div>` of the main Alpine app):

```html
<!-- Permission Prompt Modal -->
<div x-show="pendingPermission" x-cloak
     style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:200; display:flex; align-items:center; justify-content:center;">
  <div @click.stop class="modal-inner" style="background:var(--bg); border-radius:12px; padding:24px; width:500px; max-width:90vw; border:1px solid var(--border); box-shadow:0 20px 60px rgba(0,0,0,0.5);">
    <h3 style="margin-top:0; margin-bottom:16px; display:flex; align-items:center; gap:8px;">
      <span style="color:var(--yellow);">&#9888;</span> Permission Required
    </h3>
    <template x-if="pendingPermission">
      <div>
        <div style="margin-bottom:12px;">
          <span style="font-weight:600;" x-text="pendingPermission.tool_name || 'Tool'"></span>
          <span style="color:var(--text-muted); margin-left:8px;" x-text="pendingPermission.description || ''"></span>
        </div>
        <template x-if="pendingPermission.input">
          <pre style="background:var(--input-bg); border:1px solid var(--border); border-radius:6px; padding:12px; font-size:12px; overflow-x:auto; max-height:200px; overflow-y:auto; margin-bottom:16px; white-space:pre-wrap; word-break:break-all;"
               x-text="typeof pendingPermission.input === 'string' ? pendingPermission.input : JSON.stringify(pendingPermission.input, null, 2)"></pre>
        </template>
        <div style="display:flex; gap:8px; justify-content:flex-end;">
          <button class="btn" @click="respondToPermission('deny')"
                  style="background:var(--red); color:white; border:none; padding:8px 16px; border-radius:6px; cursor:pointer;">
            Deny
          </button>
          <button class="btn" @click="respondToPermission('allow_always')"
                  style="background:var(--input-bg); color:var(--text); border:1px solid var(--border); padding:8px 16px; border-radius:6px; cursor:pointer;">
            Allow Always
          </button>
          <button class="btn" @click="respondToPermission('allow')"
                  style="background:var(--green); color:white; border:none; padding:8px 16px; border-radius:6px; cursor:pointer;">
            Allow
          </button>
        </div>
      </div>
    </template>
  </div>
</div>
```

- [ ] **Step 6: Add input_needed status dot style**

In `server/web/static/style.css`, add after the `.status-dot.idle` rule (line 202):

```css
.status-dot.input_needed {
  background: var(--yellow);
  animation: status-pulse 0.8s ease-in-out infinite;
}
```

- [ ] **Step 7: Update sessionStatus() to handle input_needed**

In `server/web/static/app.js`, update the `sessionStatus` method:

```javascript
sessionStatus(session) {
    if (session.mode === 'managed') {
        const state = session.activity_state || 'idle';
        if (state === 'working') return 'active';
        if (state === 'waiting') return 'waiting';
        if (state === 'input_needed') return 'input_needed';
        return 'idle';
    }
    // ... rest of existing code
},
```

Also update the `:title` attribute on the status dot in the session list (line 82) to include input_needed:

```html
:title="sessionStatus(session) === 'active' ? 'Working' : sessionStatus(session) === 'waiting' ? 'Waiting for input' : sessionStatus(session) === 'input_needed' ? 'Permission needed' : 'Idle'"
```

- [ ] **Step 8: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html server/web/static/style.css
git commit -m "feat: add permission prompt modal to web UI with SSE integration"
```

---

### Task 5: Integration Test — End-to-End Permission Flow

**Files:**
- Modify: `server/api/permissions_test.go` (add integration test)

- [ ] **Step 1: Write an integration test that simulates the full flow**

```go
// Add to server/api/permissions_test.go
func TestPermissionRequestBroadcastsSSE(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	// Verify activity state starts as empty
	s, _ := store.GetSessionByID(sess.ID)
	if s.ActivityState != "" {
		t.Errorf("initial activity_state=%s, want empty", s.ActivityState)
	}

	// Start permission request in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		body := `{"tool_name":"Edit","description":"Edit file","input":{"file_path":"/tmp/test.go"}}`
		req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-request", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-api-key")
		req.Header.Set("Content-Type", "application/json")
		http.DefaultClient.Do(req)
	}()

	time.Sleep(100 * time.Millisecond)

	// Verify pending-permission endpoint shows the request
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/pending-permission", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var pending map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&pending)
	if pending["pending"] != true {
		t.Errorf("expected pending=true, got %v", pending["pending"])
	}
	if pending["tool_name"] != "Edit" {
		t.Errorf("tool_name=%v, want Edit", pending["tool_name"])
	}

	// Verify activity state changed to input_needed
	s, _ = store.GetSessionByID(sess.ID)
	if s.ActivityState != "input_needed" {
		t.Errorf("activity_state=%s, want input_needed", s.ActivityState)
	}

	// Respond
	respondBody := `{"decision":"deny"}`
	req2, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-respond", strings.NewReader(respondBody))
	req2.Header.Set("Authorization", "Bearer test-api-key")
	req2.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(req2)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("permission-request goroutine did not finish")
	}

	// Verify pending cleared
	req3, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/pending-permission", nil)
	req3.Header.Set("Authorization", "Bearer test-api-key")
	resp3, _ := http.DefaultClient.Do(req3)
	defer resp3.Body.Close()

	var cleared map[string]interface{}
	json.NewDecoder(resp3.Body).Decode(&cleared)
	if cleared["pending"] != false {
		t.Errorf("expected pending=false after respond, got %v", cleared["pending"])
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd server && go test ./api/ -v -run TestPermission`
Expected: All permission tests pass

- [ ] **Step 3: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add server/api/permissions_test.go
git commit -m "test: add integration test for permission request broadcast and lifecycle"
```

---

### Task 6: Manual Testing & Schema Adjustment

This task uses the schema discovered in Task 0 to verify and adjust the implementation.

- [ ] **Step 1: Build the server**

```bash
cd server && go build -o claude-controller .
```

- [ ] **Step 2: Start the server and create a managed session**

```bash
cd server && ./claude-controller --port 8080
```

In another terminal, create a session and send a message that should trigger a permission prompt (e.g., a Bash command):

```bash
curl -X POST http://localhost:8080/api/sessions/create \
  -H "Authorization: Bearer $(cat ~/.claude-controller/api.key)" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/tmp/test-project"}'
```

- [ ] **Step 3: Send a message that triggers permission**

```bash
SESSION_ID=<id from above>
curl -X POST http://localhost:8080/api/sessions/$SESSION_ID/message \
  -H "Authorization: Bearer $(cat ~/.claude-controller/api.key)" \
  -H "Content-Type: application/json" \
  -d '{"message": "Run the bash command: echo hello world"}'
```

- [ ] **Step 4: Verify the modal appears in the web UI**

Open `http://localhost:8080` in a browser, select the session, and verify:
- Permission modal appears with tool details
- Clicking "Allow" sends the response and Claude continues
- Activity state shows orange dot during pending

- [ ] **Step 5: Adjust MCP bridge schema if needed**

If the `tools/call` payload from Claude differs from what the bridge expects, update `server/mcp/bridge.go` accordingly. The logging script from Task 0 should have captured the exact format.

- [ ] **Step 6: Commit any adjustments**

```bash
git add -A
git commit -m "fix: adjust MCP bridge schema based on empirical testing"
```

---

### Task 7: Create Feature Branch and Draft PR

- [ ] **Step 1: Create feature branch from current branch**

```bash
git checkout -b feat/permission-prompt-handling
```

- [ ] **Step 2: Verify all tests pass**

```bash
cd server && go test ./... -v
```

- [ ] **Step 3: Create draft PR**

```bash
gh pr create --draft --title "feat: permission prompt handling for managed sessions" --body "$(cat <<'EOF'
## Summary
- Adds MCP bridge subcommand (`mcp-bridge`) to forward permission prompts from Claude Code to the Go server
- New `/api/sessions/:id/permission-request` and `/api/sessions/:id/permission-respond` endpoints with blocking channel pattern
- Permission modal in web UI with Allow/Deny/Allow Always buttons
- New `input_needed` activity state with orange pulsing indicator

Closes #58

## Test plan
- [ ] Unit tests for MCP bridge JSON-RPC parsing
- [ ] Integration tests for permission request/respond blocking flow
- [ ] Manual test: trigger permission prompt in managed session, verify modal appears
- [ ] Manual test: click Allow, verify Claude continues
- [ ] Manual test: click Deny, verify Claude handles gracefully
- [ ] Manual test: verify timeout (5min) auto-denies
- [ ] Manual test: verify SSE reconnect restores pending modal
EOF
)"
```
