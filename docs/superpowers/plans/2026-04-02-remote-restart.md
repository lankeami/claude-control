# Remote Server Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `POST /api/restart` endpoint that gracefully shuts down all managed sessions and re-execs the server binary, with a restart button in the settings modal.

**Architecture:** New `restart.go` handler receives the restart request, orchestrates graceful shutdown (manager → scheduler → listener → DB), then calls `syscall.Exec` to replace the process. The frontend adds a restart button in the settings modal's new "Actions" section and handles reconnection via polling.

**Tech Stack:** Go (net/http, syscall), Alpine.js, inline CSS

---

### Task 1: Add `ShutdownAll` to Manager

**Files:**
- Modify: `server/managed/manager.go:400-420` (after `GracefulShutdown`)
- Test: `server/managed/manager_test.go` (create if needed)

- [ ] **Step 1: Write the test for ShutdownAll**

Create `server/managed/manager_test.go`:

```go
package managed

import (
	"testing"
	"time"
)

func TestShutdownAll_NoProcesses(t *testing.T) {
	mgr := NewManager(Config{})
	mgr.ShutdownAll(5 * time.Second)
	// Should not panic or hang
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestShutdownAll`
Expected: FAIL — `ShutdownAll` not defined

- [ ] **Step 3: Implement ShutdownAll**

Add to the end of `server/managed/manager.go`:

```go
// ShutdownAll gracefully shuts down all running processes.
// Closes stdin and waits up to timeout for each to exit, then kills.
func (m *Manager) ShutdownAll(timeout time.Duration) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		log.Printf("shutting down process for session %s", id)
		m.GracefulShutdown(id, timeout)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestShutdownAll`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat: add ShutdownAll to managed session manager"
```

---

### Task 2: Add shutdown dependencies to Server struct

**Files:**
- Modify: `server/api/router.go:11-19` (Server struct and NewRouter)
- Modify: `server/main.go:97` (pass new deps)

- [ ] **Step 1: Add ShutdownFunc field to Server struct**

In `server/api/router.go`, change the `Server` struct:

```go
type Server struct {
	store        *db.Store
	manager      *managed.Manager
	envPath      string
	permissions  *PermissionManager
	shutdownFunc func() // called to trigger server restart
}
```

- [ ] **Step 2: Update NewRouter to accept shutdownFunc**

Change the `NewRouter` signature and constructor:

```go
func NewRouter(store *db.Store, apiKey string, mgr *managed.Manager, envPath string, shutdownFunc func()) http.Handler {
	s := &Server{store: store, manager: mgr, envPath: envPath, permissions: NewPermissionManager(), shutdownFunc: shutdownFunc}
```

- [ ] **Step 3: Update main.go to pass shutdownFunc**

In `server/main.go`, replace the current router creation and shutdown handling. First, create a channel and shutdown function before `NewRouter`:

```go
restartCh := make(chan struct{}, 1)
shutdownFunc := func() {
	select {
	case restartCh <- struct{}{}:
	default:
	}
}

router := api.NewRouter(store, apiKey, mgr, envPath, shutdownFunc)
```

Then replace the signal handling block (lines 139-145) with:

```go
// Handle shutdown via signal or restart request
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt)

restartRequested := false
select {
case <-sigCh:
	fmt.Println("\nShutting down...")
case <-restartCh:
	fmt.Println("\nRestarting server...")
	restartRequested = true
}

// Graceful shutdown sequence
mgr.ShutdownAll(5 * time.Second)
sched.Stop()
cancel()
localListener.Close()
store.Close()

if restartRequested {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Failed to find executable path: %v", err)
		os.Exit(0)
	}
	log.Printf("Re-execing %s %v", exe, os.Args)
	execErr := syscall.Exec(exe, os.Args, os.Environ())
	if execErr != nil {
		log.Printf("syscall.Exec failed: %v — exiting for wrapper to restart", execErr)
		os.Exit(0)
	}
}
```

Add `"syscall"` to the imports in main.go.

- [ ] **Step 4: Verify it compiles**

Run: `cd server && go build -o /dev/null .`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add server/api/router.go server/main.go
git commit -m "feat: wire shutdown dependencies through Server struct for restart"
```

---

### Task 3: Add `POST /api/restart` handler

**Files:**
- Create: `server/api/restart.go`
- Modify: `server/api/router.go` (add route)
- Test: `server/api/restart_test.go`

- [ ] **Step 1: Write the test**

Create `server/api/restart_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHandleRestart_Success(t *testing.T) {
	var called atomic.Bool
	s := &Server{
		shutdownFunc: func() { called.Store(true) },
	}

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called.Load() {
		t.Fatal("expected shutdownFunc to be called")
	}
}

func TestHandleRestart_ConcurrentBlocked(t *testing.T) {
	s := &Server{
		shutdownFunc: func() {},
	}
	s.restartInProgress.Store(true)

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestHandleRestart`
Expected: FAIL — `handleRestart` and `restartInProgress` not defined

- [ ] **Step 3: Add restartInProgress to Server struct**

In `server/api/router.go`, add to imports:

```go
"sync/atomic"
```

Update the Server struct:

```go
type Server struct {
	store             *db.Store
	manager           *managed.Manager
	envPath           string
	permissions       *PermissionManager
	shutdownFunc      func()
	restartInProgress atomic.Bool
}
```

- [ ] **Step 4: Create the restart handler**

Create `server/api/restart.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !s.restartInProgress.CompareAndSwap(false, true) {
		http.Error(w, "restart already in progress", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		s.shutdownFunc()
	}()
}
```

- [ ] **Step 5: Add route to router**

In `server/api/router.go`, add after the settings endpoints (after line 85):

```go
// Server management
apiMux.HandleFunc("POST /api/restart", s.handleRestart)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestHandleRestart`
Expected: PASS (both tests)

- [ ] **Step 7: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add server/api/restart.go server/api/restart_test.go server/api/router.go
git commit -m "feat: add POST /api/restart endpoint with concurrent-request guard"
```

---

### Task 4: Update activity states on restart

**Files:**
- Modify: `server/api/restart.go` (add DB update before shutdown)
- Modify: `server/api/restart_test.go` (test with mock store)

- [ ] **Step 1: Write the test**

Add to `server/api/restart_test.go`:

```go
func TestHandleRestart_SetsActivityStatesToWaiting(t *testing.T) {
	store := setupTestDB(t)
	defer store.Close()

	// Create a managed session with working state
	store.RegisterSession("sess1", "host", "/tmp", "managed")
	store.UpdateActivityState("sess1", "working")

	var called atomic.Bool
	s := &Server{
		store:        store,
		shutdownFunc: func() { called.Store(true) },
	}

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Give the goroutine time to execute
	time.Sleep(100 * time.Millisecond)

	// Check that activity state was set to waiting
	sess, _ := store.GetSession("sess1")
	if sess.ActivityState != "waiting" {
		t.Fatalf("expected activity_state 'waiting', got '%s'", sess.ActivityState)
	}
}
```

Note: Check if `setupTestDB` already exists in the test package. If so, use it. If not, create a helper:

```go
func setupTestDB(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestHandleRestart_SetsActivityStates`
Expected: FAIL — activity state not being set

- [ ] **Step 3: Add SetWorkingToWaiting DB method**

Check if a method like this exists in `server/db/sessions.go`. If not, add:

```go
// SetWorkingToWaiting updates all sessions with activity_state='working' to 'waiting'.
// Used during graceful restart to preserve conversation continuity.
func (s *Store) SetWorkingToWaiting() error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = 'waiting' WHERE activity_state = 'working'")
	return err
}
```

- [ ] **Step 4: Update restart handler to set activity states**

In `server/api/restart.go`, update the goroutine:

```go
go func() {
	time.Sleep(500 * time.Millisecond)
	if s.store != nil {
		s.store.SetWorkingToWaiting()
	}
	s.shutdownFunc()
}()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestHandleRestart`
Expected: PASS (all three tests)

- [ ] **Step 6: Commit**

```bash
git add server/api/restart.go server/api/restart_test.go server/db/sessions.go
git commit -m "feat: set working sessions to waiting state before restart"
```

---

### Task 5: Add restart button to settings modal

**Files:**
- Modify: `server/web/static/index.html:1130-1141` (settings modal, before closing div)
- Modify: `server/web/static/app.js` (add `restartServer` method and `serverRestarting` state)

- [ ] **Step 1: Add Actions section to settings modal HTML**

In `server/web/static/index.html`, insert the Actions section between the error message and the button row. Replace lines 1132-1140 with:

```html
      <p class="error-msg" x-show="settingsError" x-text="settingsError" style="margin-top:8px;"></p>

      <!-- Actions -->
      <div x-show="!settingsFirstRun" style="margin-top:20px; padding-top:16px; border-top:1px solid var(--border);">
        <label style="font-size:12px; font-weight:600; color:var(--text-muted); display:block; margin-bottom:8px;">ACTIONS</label>
        <button class="btn btn-sm" @click="restartServer()" :disabled="serverRestarting"
                style="background:var(--red); color:white; border:none; padding:8px 16px; border-radius:6px; cursor:pointer; font-size:13px;">
          <span x-show="!serverRestarting">Restart Server</span>
          <span x-show="serverRestarting">Restarting...</span>
        </button>
        <div style="font-size:11px; color:var(--text-muted); margin-top:4px;">Gracefully restarts the server, preserving all session state</div>
      </div>

      <div style="display:flex; gap:8px; justify-content:flex-end; margin-top:16px;">
        <button class="btn btn-sm" @click="showSettingsModal = false; settingsFirstRun = false;" x-text="settingsFirstRun ? 'Skip' : 'Cancel'"></button>
        <button class="btn btn-sm btn-primary" @click="saveSettings()" :disabled="settingsSaving">
          <span x-show="!settingsSaving" x-text="settingsFirstRun ? 'Save & Continue' : 'Save'"></span>
          <span x-show="settingsSaving">Saving...</span>
        </button>
      </div>
```

- [ ] **Step 2: Add state and method to app.js**

Find the Alpine.js data initialization (look for `return {` at the top of the app data function). Add `serverRestarting: false` to the state.

Then add the `restartServer` method near the settings-related methods:

```javascript
async restartServer() {
  if (!confirm('Restart the server? All sessions will be briefly disconnected.')) return;
  this.serverRestarting = true;
  try {
    const resp = await fetch('/api/restart', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${this.apiKey}` }
    });
    if (!resp.ok) {
      const text = await resp.text();
      this.toast(text || 'Restart failed', 4000, 'error');
      this.serverRestarting = false;
      return;
    }
    this.showSettingsModal = false;
    this.toast('Server restarting...', 10000, 'info');
    // Poll until server is back
    this.pollForRestart();
  } catch (e) {
    // Network error likely means server already shutting down — start polling
    this.showSettingsModal = false;
    this.toast('Server restarting...', 10000, 'info');
    this.pollForRestart();
  }
},

pollForRestart() {
  let attempts = 0;
  const maxAttempts = 30;
  const poll = setInterval(async () => {
    attempts++;
    try {
      const resp = await fetch(`/api/events?token=${encodeURIComponent(this.apiKey)}`);
      if (resp.ok) {
        clearInterval(poll);
        this.serverRestarting = false;
        this.toast('Server restarted successfully', 3000, 'info');
        // Reconnect SSE streams
        this.startSSE();
        if (this.selectedSessionId) {
          this.startSessionSSE(this.selectedSessionId);
        }
      }
    } catch (e) {
      // Server still down, keep polling
    }
    if (attempts >= maxAttempts) {
      clearInterval(poll);
      this.serverRestarting = false;
      this.toast('Could not reconnect to server', 5000, 'error');
    }
  }, 1000);
},
```

- [ ] **Step 3: Verify the server builds cleanly**

Run: `cd server && go build -o /dev/null .`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat: add restart button to settings modal with auto-reconnect"
```

---

### Task 6: End-to-end verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests pass

- [ ] **Step 2: Build the server**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds, binary produced

- [ ] **Step 3: Clean up build artifact**

Run: `rm server/claude-controller`

- [ ] **Step 4: Commit any remaining changes**

Only if there are fixups needed from the test run.
