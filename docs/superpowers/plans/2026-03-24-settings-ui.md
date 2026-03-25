# Settings UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a settings page to manage `.env` variables via the web UI, with first-run setup and hot-reload for managed session config.

**Architecture:** New `GET/PUT /api/settings` endpoints read/write the `.env` file and hot-reload `managed.Manager` config. Frontend adds a gear icon in sidebar header, a settings modal, and a first-run setup modal. `NewRouter` gains an `envPath` parameter.

**Tech Stack:** Go (net/http handlers), Alpine.js (frontend modals), SQLite (unchanged)

**Spec:** `docs/superpowers/specs/2026-03-24-settings-ui-design.md`

---

### Task 1: Add `UpdateConfig` to Manager + fix Spawn race

**Files:**
- Modify: `server/managed/manager.go:68-81`

- [ ] **Step 1: Write the failing test**

Create `server/managed/manager_test.go`:

```go
package managed

import (
	"testing"
)

func TestUpdateConfig(t *testing.T) {
	mgr := NewManager(Config{ClaudeBin: "old-bin", ClaudeArgs: []string{"--old"}, ClaudeEnv: []string{"OLD=1"}})

	mgr.UpdateConfig(Config{ClaudeBin: "new-bin", ClaudeArgs: []string{"--new"}, ClaudeEnv: []string{"NEW=1"}})

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.cfg.ClaudeBin != "new-bin" {
		t.Errorf("expected new-bin, got %s", mgr.cfg.ClaudeBin)
	}
	if len(mgr.cfg.ClaudeArgs) != 1 || mgr.cfg.ClaudeArgs[0] != "--new" {
		t.Errorf("expected [--new], got %v", mgr.cfg.ClaudeArgs)
	}
	if len(mgr.cfg.ClaudeEnv) != 1 || mgr.cfg.ClaudeEnv[0] != "NEW=1" {
		t.Errorf("expected [NEW=1], got %v", mgr.cfg.ClaudeEnv)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestUpdateConfig`
Expected: FAIL — `mgr.UpdateConfig undefined`

- [ ] **Step 3: Implement UpdateConfig method**

Add to `server/managed/manager.go` after `GetBroadcaster`:

```go
func (m *Manager) UpdateConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}
```

- [ ] **Step 4: Fix Spawn to copy config under lock**

In `server/managed/manager.go`, replace lines 68-81 of `Spawn`:

```go
func (m *Manager) Spawn(sessionID string, opts SpawnOpts) (*Process, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	if _, running := m.procs[sessionID]; running {
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}

	// Copy config under lock to prevent race with UpdateConfig
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	args := append(cfg.ClaudeArgs, opts.Args...)
	cmd := exec.Command(cfg.ClaudeBin, args...)
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), cfg.ClaudeEnv...)
	cmd.Env = append(cmd.Env, "CLAUDE_CONTROLLER_MANAGED=1")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./managed/ -v -run TestUpdateConfig`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat: add UpdateConfig to Manager and fix Spawn config race"
```

---

### Task 2: Update `NewRouter` signature to accept `envPath`

**Files:**
- Modify: `server/api/router.go:11-17`
- Modify: `server/api/sessions_test.go:14-26`
- Modify: `server/main.go:69-77`

- [ ] **Step 1: Update Server struct and NewRouter**

In `server/api/router.go`, add `envPath` field to `Server` and update `NewRouter`:

```go
type Server struct {
	store   *db.Store
	manager *managed.Manager
	envPath string
}

func NewRouter(store *db.Store, apiKey string, mgr *managed.Manager, envPath string) http.Handler {
	s := &Server{store: store, manager: mgr, envPath: envPath}
```

- [ ] **Step 2: Fix test helper**

In `server/api/sessions_test.go`, update `newTestServer`:

```go
func newTestServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	router := NewRouter(store, "test-key", nil, filepath.Join(t.TempDir(), ".env"))
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store
}
```

- [ ] **Step 3: Update main.go**

In `server/main.go`, add `"path/filepath"` to imports (already imported), resolve path and pass to NewRouter:

```go
	loadDotEnv(".env")
	envPath, _ := filepath.Abs(".env")
	managedCfg := managed.Config{
		ClaudeBin:  envOrDefault("CLAUDE_BIN", "claude"),
		ClaudeArgs: strings.Fields(os.Getenv("CLAUDE_ARGS")),
		ClaudeEnv:  splitEnv(os.Getenv("CLAUDE_ENV")),
	}
	mgr := managed.NewManager(managedCfg)

	router := api.NewRouter(store, apiKey, mgr, envPath)
```

- [ ] **Step 4: Run all tests to verify nothing broke**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/router.go server/api/sessions_test.go server/main.go
git commit -m "feat: add envPath parameter to NewRouter for settings handlers"
```

---

### Task 3: Implement settings API handlers

**Files:**
- Create: `server/api/settings.go`
- Create: `server/api/settings_test.go`
- Modify: `server/api/router.go` (register routes)

- [ ] **Step 1: Write the failing tests**

Create `server/api/settings_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func newTestServerWithManager(t *testing.T) (*httptest.Server, *db.Store, *managed.Manager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	mgr := managed.NewManager(managed.Config{ClaudeBin: "claude"})
	envPath := filepath.Join(tmpDir, ".env")
	router := NewRouter(store, "test-key", mgr, envPath)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store, mgr, envPath
}

func TestSettingsExists_NoFile(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	req := authReq("GET", ts.URL+"/api/settings/exists", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)
	if body["exists"] {
		t.Error("expected exists=false")
	}
}

func TestSettingsExists_WithFile(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("PORT=8080\n"), 0600)

	req := authReq("GET", ts.URL+"/api/settings/exists", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)
	if !body["exists"] {
		t.Error("expected exists=true")
	}
}

func TestGetSettings_NoFile(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["port"] != "" || body["claude_bin"] != "" {
		t.Errorf("expected empty fields, got %v", body)
	}
}

func TestPutSettings_CreatesFile(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)

	settings := map[string]string{
		"port":             "9090",
		"ngrok_authtoken":  "tok_abc123",
		"claude_bin":       "/usr/bin/claude",
		"claude_args":      "--flag1 --flag2",
		"claude_env":       "K1=V1,K2=V2",
	}
	body, _ := json.Marshal(settings)
	req := authReq("PUT", ts.URL+"/api/settings", nil)
	req.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify file was created
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "PORT=9090") {
		t.Errorf("expected PORT=9090 in file, got: %s", content)
	}
	if !strings.Contains(content, "CLAUDE_BIN=/usr/bin/claude") {
		t.Errorf("expected CLAUDE_BIN in file, got: %s", content)
	}
}

func TestPutSettings_MaskedAuthtoken(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	// Pre-create .env with a token
	os.WriteFile(envPath, []byte("NGROK_AUTHTOKEN=secret_token_12345\n"), 0600)

	// PUT with masked token — should preserve original
	settings := map[string]string{
		"ngrok_authtoken": "****2345",
		"claude_bin":      "claude",
	}
	body, _ := json.Marshal(settings)
	req := authReq("PUT", ts.URL+"/api/settings", nil)
	req.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "NGROK_AUTHTOKEN=secret_token_12345") {
		t.Errorf("expected original token preserved, got: %s", string(data))
	}
}

func TestPutSettings_RestartRequired(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("PORT=8080\n"), 0600)

	settings := map[string]string{"port": "9090"}
	body, _ := json.Marshal(settings)
	req := authReq("PUT", ts.URL+"/api/settings", nil)
	req.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]bool
	json.NewDecoder(resp.Body).Decode(&result)
	if !result["restart_required"] {
		t.Error("expected restart_required=true when PORT changed")
	}
}

func TestPutSettings_InvalidPort(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	settings := map[string]string{"port": "abc"}
	body, _ := json.Marshal(settings)
	req := authReq("PUT", ts.URL+"/api/settings", nil)
	req.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetSettings_MasksAuthtoken(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("NGROK_AUTHTOKEN=secret_token_12345\nPORT=8080\n"), 0600)

	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["ngrok_authtoken"] != "****2345" {
		t.Errorf("expected masked token ****2345, got %s", body["ngrok_authtoken"])
	}
	if body["port"] != "8080" {
		t.Errorf("expected port 8080, got %s", body["port"])
	}
}
```

Note: Add `"io"` and `"strings"` to imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./api/ -v -run TestSettings`
Expected: FAIL — handlers not defined

- [ ] **Step 3: Create settings.go with handlers**

Create `server/api/settings.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jaychinthrajah/claude-controller/server/managed"
)

type settingsPayload struct {
	Port           string `json:"port"`
	NgrokAuthtoken string `json:"ngrok_authtoken"`
	ClaudeBin      string `json:"claude_bin"`
	ClaudeArgs     string `json:"claude_args"`
	ClaudeEnv      string `json:"claude_env"`
}

func (s *Server) handleSettingsExists(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(s.envPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"exists": err == nil})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	vals := readEnvFile(s.envPath)

	// Mask authtoken
	if tok := vals["NGROK_AUTHTOKEN"]; len(tok) > 4 {
		vals["NGROK_AUTHTOKEN"] = "****" + tok[len(tok)-4:]
	} else if tok != "" {
		vals["NGROK_AUTHTOKEN"] = "****"
	}

	resp := settingsPayload{
		Port:           vals["PORT"],
		NgrokAuthtoken: vals["NGROK_AUTHTOKEN"],
		ClaudeBin:      vals["CLAUDE_BIN"],
		ClaudeArgs:     vals["CLAUDE_ARGS"],
		ClaudeEnv:      vals["CLAUDE_ENV"],
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var payload settingsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate port
	if payload.Port != "" {
		p, err := strconv.Atoi(payload.Port)
		if err != nil || p < 1 || p > 65535 {
			http.Error(w, "PORT must be a number between 1 and 65535", http.StatusBadRequest)
			return
		}
	}

	// Read current values for comparison and sentinel handling
	current := readEnvFile(s.envPath)

	// Handle masked authtoken sentinel
	if strings.HasPrefix(payload.NgrokAuthtoken, "****") {
		payload.NgrokAuthtoken = current["NGROK_AUTHTOKEN"]
	}

	// Check if restart-requiring fields changed
	restartRequired := (payload.Port != current["PORT"]) ||
		(payload.NgrokAuthtoken != current["NGROK_AUTHTOKEN"])

	// Write .env file atomically
	content := formatEnvFile(payload)
	tmpPath := s.envPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		http.Error(w, fmt.Sprintf("write error: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, s.envPath); err != nil {
		http.Error(w, fmt.Sprintf("rename error: %v", err), http.StatusInternalServerError)
		return
	}

	// Hot-reload manager config
	if s.manager != nil {
		s.manager.UpdateConfig(managed.Config{
			ClaudeBin:  orDefault(payload.ClaudeBin, "claude"),
			ClaudeArgs: strings.Fields(payload.ClaudeArgs),
			ClaudeEnv:  splitEnv(payload.ClaudeEnv),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"restart_required": restartRequired})
}

func readEnvFile(path string) map[string]string {
	vals := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return vals
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return vals
}

func formatEnvFile(p settingsPayload) string {
	var b strings.Builder
	b.WriteString("# Server\n")
	b.WriteString("PORT=" + p.Port + "\n")
	b.WriteString("NGROK_AUTHTOKEN=" + p.NgrokAuthtoken + "\n")
	b.WriteString("\n# Managed session config\n")
	b.WriteString("CLAUDE_BIN=" + p.ClaudeBin + "\n")
	b.WriteString("CLAUDE_ARGS=" + p.ClaudeArgs + "\n")
	b.WriteString("CLAUDE_ENV=" + p.ClaudeEnv + "\n")
	return b.String()
}

func orDefault(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

func splitEnv(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
```

- [ ] **Step 4: Register routes in router.go**

In `server/api/router.go`, add after the GitHub endpoints block (before scheduled task endpoints):

```go
	// Settings endpoints
	apiMux.HandleFunc("GET /api/settings/exists", s.handleSettingsExists)
	apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
	apiMux.HandleFunc("PUT /api/settings", s.handlePutSettings)
```

Also add the `managed` import to router.go if not already present (it's not currently imported there — check first, it may not be needed since `settings.go` handles the managed import).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestSettings`
Expected: All PASS

- [ ] **Step 6: Run all tests to verify nothing broke**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add server/api/settings.go server/api/settings_test.go server/api/router.go
git commit -m "feat: add settings API endpoints (GET/PUT /api/settings)"
```

---

### Task 4: Add settings gear icon to sidebar header

**Files:**
- Modify: `server/web/static/index.html:53-61` (sidebar header)
- Modify: `server/web/static/style.css` (gear button style)

- [ ] **Step 1: Add gear icon to sidebar header**

In `server/web/static/index.html`, replace the sidebar-header div (lines 53-61):

```html
        <div class="sidebar-header">
          <button x-show="!leftCollapsed" class="settings-gear-btn" @click="openSettingsModal()" title="Settings">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <path d="M8 4.754a3.246 3.246 0 100 6.492 3.246 3.246 0 000-6.492zM5.754 8a2.246 2.246 0 114.492 0 2.246 2.246 0 01-4.492 0z"/>
              <path d="M9.796 1.343c-.527-1.79-3.065-1.79-3.592 0l-.094.319a.873.873 0 01-1.255.52l-.292-.16c-1.64-.892-3.433.902-2.54 2.541l.159.292a.873.873 0 01-.52 1.255l-.319.094c-1.79.527-1.79 3.065 0 3.592l.319.094a.873.873 0 01.52 1.255l-.16.292c-.892 1.64.902 3.434 2.541 2.54l.292-.159a.873.873 0 011.255.52l.094.319c.527 1.79 3.065 1.79 3.592 0l.094-.319a.873.873 0 011.255-.52l.292.16c1.64.893 3.434-.902 2.54-2.541l-.159-.292a.873.873 0 01.52-1.255l.319-.094c1.79-.527 1.79-3.065 0-3.592l-.319-.094a.873.873 0 01-.52-1.255l.16-.292c.893-1.64-.902-3.433-2.541-2.54l-.292.159a.873.873 0 01-1.255-.52l-.094-.319zm-2.633.283c.246-.835 1.428-.835 1.674 0l.094.319a1.873 1.873 0 002.693 1.115l.291-.16c.764-.415 1.6.42 1.184 1.185l-.159.292a1.873 1.873 0 001.116 2.692l.318.094c.835.246.835 1.428 0 1.674l-.319.094a1.873 1.873 0 00-1.115 2.693l.16.291c.415.764-.42 1.6-1.185 1.184l-.291-.159a1.873 1.873 0 00-2.693 1.116l-.094.318c-.246.835-1.428.835-1.674 0l-.094-.319a1.873 1.873 0 00-2.692-1.115l-.292.16c-.764.415-1.6-.42-1.184-1.185l.159-.291a1.873 1.873 0 00-1.116-2.693l-.318-.094c-.835-.246-.835-1.428 0-1.674l.319-.094a1.873 1.873 0 001.115-2.692l-.16-.292c-.415-.764.42-1.6 1.185-1.184l.292.159a1.873 1.873 0 002.692-1.116l.094-.318z"/>
            </svg>
          </button>
          <span x-show="!leftCollapsed" style="font-size:0.875rem;font-weight:600;">Sessions</span>
          <button x-show="!leftCollapsed" class="btn btn-sm" @click="disconnect" style="background:transparent;color:var(--text-muted)">
            Disconnect
          </button>
          <button class="collapse-toggle" @click="leftCollapsed = !leftCollapsed" :title="leftCollapsed ? 'Expand sessions' : 'Collapse sessions'">
            <span x-text="leftCollapsed ? '▶' : '◀'"></span>
          </button>
        </div>
```

- [ ] **Step 2: Add CSS for gear button**

In `server/web/static/style.css`, add after the `.sidebar-header h2` rule (around line 124):

```css
.settings-gear-btn {
  background: transparent;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  padding: 4px;
  border-radius: 4px;
  display: flex;
  align-items: center;
}
.settings-gear-btn:hover {
  background: var(--bg-secondary);
  color: var(--text);
}
```

- [ ] **Step 3: Verify build**

Run: `cd server && go build -o /dev/null .`
Expected: Build succeeds (static files embedded at build time)

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html server/web/static/style.css
git commit -m "feat: add settings gear icon to sidebar header"
```

---

### Task 5: Add settings modal HTML + CSS

**Files:**
- Modify: `server/web/static/index.html` (add modal after existing modals)
- Modify: `server/web/static/style.css` (restart banner)

- [ ] **Step 1: Add settings modal HTML**

In `server/web/static/index.html`, add before the closing `</template>` of the authenticated block (find the last modal, add after it):

```html
  <!-- Settings Modal -->
  <div x-show="showSettingsModal" x-cloak
       style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:100; display:flex; align-items:center; justify-content:center;"
       @click.self="showSettingsModal = false">
    <div class="modal-inner" style="background:var(--bg); border-radius:12px; padding:24px; width:500px; max-width:90vw; border:1px solid var(--border); display:flex; flex-direction:column; max-height:80vh; overflow-y:auto;">
      <div class="mobile-detail-header mobile-modal-header">
        <button @click="showSettingsModal = false" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title" x-text="settingsFirstRun ? 'Welcome — Configure Claude Controller' : 'Settings'"></span>
      </div>
      <h3 class="desktop-modal-title" style="margin-top:0; margin-bottom:16px;" x-text="settingsFirstRun ? 'Welcome — Configure Claude Controller' : 'Settings'"></h3>

      <div style="display:flex; flex-direction:column; gap:12px;">
        <div>
          <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:4px;">PORT</label>
          <input x-model="settingsForm.port" type="text" placeholder="8080"
                 style="width:100%; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;">
          <div style="font-size:11px; color:var(--text-muted); margin-top:2px;">Server port (requires restart)</div>
        </div>

        <div>
          <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:4px;">NGROK_AUTHTOKEN</label>
          <input x-model="settingsForm.ngrok_authtoken" type="password" placeholder="ngrok auth token"
                 style="width:100%; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;">
          <div style="font-size:11px; color:var(--text-muted); margin-top:2px;">For remote access via ngrok tunnel (requires restart)</div>
        </div>

        <div>
          <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:4px;">CLAUDE_BIN</label>
          <input x-model="settingsForm.claude_bin" type="text" placeholder="claude"
                 style="width:100%; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;">
          <div style="font-size:11px; color:var(--text-muted); margin-top:2px;">Path to Claude CLI binary</div>
        </div>

        <div>
          <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:4px;">CLAUDE_ARGS</label>
          <input x-model="settingsForm.claude_args" type="text" placeholder="--dangerously-skip-permissions"
                 style="width:100%; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;">
          <div style="font-size:11px; color:var(--text-muted); margin-top:2px;">Space-separated CLI flags for managed sessions</div>
        </div>

        <div>
          <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:4px;">CLAUDE_ENV</label>
          <input x-model="settingsForm.claude_env" type="text" placeholder="CLAUDE_CONFIG_DIR=/path"
                 style="width:100%; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;">
          <div style="font-size:11px; color:var(--text-muted); margin-top:2px;">Comma-separated KEY=VALUE pairs for managed session environment</div>
        </div>
      </div>

      <p class="error-msg" x-show="settingsError" x-text="settingsError" style="margin-top:8px;"></p>

      <div style="display:flex; gap:8px; justify-content:flex-end; margin-top:16px;">
        <button class="btn btn-sm" @click="showSettingsModal = false; settingsFirstRun = false;" x-text="settingsFirstRun ? 'Skip' : 'Cancel'"></button>
        <button class="btn btn-sm btn-primary" @click="saveSettings()" :disabled="settingsSaving">
          <span x-show="!settingsSaving" x-text="settingsFirstRun ? 'Save & Continue' : 'Save'"></span>
          <span x-show="settingsSaving">Saving...</span>
        </button>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: Add restart banner HTML**

In `server/web/static/index.html`, add right after the connection banner (after line ~21):

```html
  <!-- Restart required banner -->
  <div class="restart-banner" x-show="settingsRestartRequired" x-cloak>
    Server restart required for PORT/NGROK changes to take effect.
  </div>
```

- [ ] **Step 3: Add restart banner CSS**

In `server/web/static/style.css`, add after the `.connection-banner` rule:

```css
.restart-banner {
  background: #f59e0b;
  color: #000;
  text-align: center;
  padding: 6px 12px;
  font-size: 13px;
  font-weight: 500;
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 999;
}
```

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html server/web/static/style.css
git commit -m "feat: add settings modal and restart banner HTML/CSS"
```

---

### Task 6: Add settings JavaScript logic

**Files:**
- Modify: `server/web/static/app.js` (state + methods)

- [ ] **Step 1: Add settings state variables**

In `server/web/static/app.js`, add after the toast state block (after `toastType: 'info',` around line 103):

```javascript
    // Settings state
    showSettingsModal: false,
    settingsFirstRun: false,
    settingsForm: { port: '', ngrok_authtoken: '', claude_bin: '', claude_args: '', claude_env: '' },
    settingsError: '',
    settingsSaving: false,
    settingsRestartRequired: false,
```

- [ ] **Step 2: Add first-run check to init**

In `server/web/static/app.js`, in the `init()` method, add after `await this.loadScheduledTasks();` (around line 185):

```javascript
        await this.checkSettingsFirstRun();
```

- [ ] **Step 3: Add settings methods**

In `server/web/static/app.js`, add the settings methods after the `toast()` method (after line 402):

```javascript
    async checkSettingsFirstRun() {
      try {
        const res = await fetch('/api/settings/exists', {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) return;
        const data = await res.json();
        if (!data.exists) {
          this.settingsFirstRun = true;
          this.showSettingsModal = true;
        }
      } catch (e) {}
    },

    async openSettingsModal() {
      this.settingsError = '';
      this.settingsFirstRun = false;
      try {
        const res = await fetch('/api/settings', {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.settingsForm = {
          port: data.port || '',
          ngrok_authtoken: data.ngrok_authtoken || '',
          claude_bin: data.claude_bin || '',
          claude_args: data.claude_args || '',
          claude_env: data.claude_env || '',
        };
      } catch (e) {
        this.settingsForm = { port: '', ngrok_authtoken: '', claude_bin: '', claude_args: '', claude_env: '' };
      }
      this.showSettingsModal = true;
    },

    async saveSettings() {
      this.settingsError = '';
      this.settingsSaving = true;
      try {
        const res = await fetch('/api/settings', {
          method: 'PUT',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify(this.settingsForm)
        });
        if (!res.ok) {
          const errText = await res.text();
          this.settingsError = errText;
          this.settingsSaving = false;
          return;
        }
        const data = await res.json();
        if (data.restart_required) {
          this.settingsRestartRequired = true;
        }
        this.showSettingsModal = false;
        this.settingsFirstRun = false;
        this.toast('Settings saved');
      } catch (e) {
        this.settingsError = 'Error: ' + e.message;
      }
      this.settingsSaving = false;
    },
```

- [ ] **Step 4: Verify build**

Run: `cd server && go build -o /dev/null .`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add settings modal JavaScript logic with first-run detection"
```

---

### Task 7: Manual smoke test + final verification

- [ ] **Step 1: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Build the server**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds with no errors

- [ ] **Step 3: Commit any remaining changes**

If there are fixups from testing, commit them:

```bash
git add -A
git commit -m "fix: address issues found during smoke testing"
```
