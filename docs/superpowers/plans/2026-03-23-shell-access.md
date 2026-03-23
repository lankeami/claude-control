# Shell Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shell command execution to managed sessions, streaming output inline in the chat UI.

**Architecture:** Extend the existing `Manager` with a `SpawnShell` method that runs `sh -c "<command>"` in the session's CWD. Output streams through the session's existing `Broadcaster` → SSE channel. Shell messages are persisted in the `messages` table with `role='shell'` and `role='shell_output'`. The UI toggles between chat and shell mode via a `$` button.

**Tech Stack:** Go (server), Alpine.js (web UI), SQLite (persistence)

**Spec:** `docs/superpowers/specs/2026-03-23-shell-access-design.md`

---

### Task 1: Add `SpawnShell` to Manager

**Files:**
- Modify: `server/managed/manager.go`
- Test: `server/managed/manager_test.go`

- [ ] **Step 1: Write the failing test for SpawnShell**

Add to `server/managed/manager_test.go`:

```go
func TestManagerSpawnShell(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.SpawnShell("shell-test-1", ShellOpts{
		Command: "echo hello",
		CWD:     "/tmp",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil {
		t.Fatal("proc is nil")
	}
	if !m.IsRunning("shell-test-1") {
		t.Error("session should be running during shell execution")
	}

	// Wait for completion
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("shell process did not complete")
	}

	if proc.ExitCode != 0 {
		t.Errorf("exit code=%d, want 0", proc.ExitCode)
	}
	if m.IsRunning("shell-test-1") {
		t.Error("session should not be running after shell completes")
	}
}

func TestManagerSpawnShellBlocksConcurrent(t *testing.T) {
	cfg := Config{ClaudeBin: "sleep", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	// Start a Claude process
	_, err := m.Spawn("sess-concurrent", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Teardown("sess-concurrent", 2*time.Second)

	// Shell should be rejected
	_, err = m.SpawnShell("sess-concurrent", ShellOpts{
		Command: "echo blocked",
		CWD:     "/tmp",
		Timeout: 5 * time.Second,
	})
	if err == nil {
		t.Error("expected error when Claude process is running")
	}
}

func TestManagerSpawnShellTimeout(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.SpawnShell("shell-timeout", ShellOpts{
		Command: "sleep 60",
		CWD:     "/tmp",
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-proc.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("shell process did not exit after timeout")
	}

	if m.IsRunning("shell-timeout") {
		t.Error("session should not be running after timeout")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./managed/ -v -run "TestManagerSpawnShell"`
Expected: FAIL — `ShellOpts` and `SpawnShell` undefined

- [ ] **Step 3: Implement SpawnShell**

Add to `server/managed/manager.go`. First, ensure `"syscall"` is in the import block (it's already imported for `SIGINT` in `Interrupt`). Also add a `TimedOut` field to the `Process` struct:

```go
// Add TimedOut field to existing Process struct
type Process struct {
	Cmd      *exec.Cmd
	Stdout   io.ReadCloser
	Stderr   io.ReadCloser
	Done     chan struct{}
	ExitCode int
	TimedOut bool // Set by timeout goroutine before killing
}
```

Then add:

```go
type ShellOpts struct {
	Command string
	CWD     string
	Timeout time.Duration
}

func (m *Manager) SpawnShell(sessionID string, opts ShellOpts) (*Process, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	if _, running := m.procs[sessionID]; running {
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}

	cmd := exec.Command("sh", "-c", opts.Command)
	cmd.Dir = opts.CWD
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	proc := &Process{
		Cmd:    cmd,
		Stdout: stdout,
		Stderr: stderr,
		Done:   make(chan struct{}),
	}

	m.mu.Lock()
	m.procs[sessionID] = proc
	m.mu.Unlock()

	// Background: wait for exit, cleanup
	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		m.mu.Lock()
		delete(m.procs, sessionID)
		m.mu.Unlock()
		close(proc.Done)
	}()

	// Timeout: SIGINT → grace → SIGKILL (on process group)
	if opts.Timeout > 0 {
		go func() {
			select {
			case <-time.After(opts.Timeout):
				pgid, err := syscall.Getpgid(cmd.Process.Pid)
				if err != nil {
					cmd.Process.Kill()
					return
				}
				proc.TimedOut = true
				// SIGINT to process group
				syscall.Kill(-pgid, syscall.SIGINT)
				// Grace period
				select {
				case <-proc.Done:
					return
				case <-time.After(5 * time.Second):
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			case <-proc.Done:
				return
			}
		}()
	}

	return proc, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./managed/ -v -run "TestManagerSpawnShell"`
Expected: All 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat: add SpawnShell method to managed session Manager"
```

---

### Task 2: Add shell execute API handler

**Files:**
- Modify: `server/api/managed_sessions.go`
- Modify: `server/api/router.go`
- Test: `server/api/managed_sessions_test.go`

- [ ] **Step 1: Write the failing test**

Add to `server/api/managed_sessions_test.go`:

```go
func TestShellExecuteAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["id"] == nil || result["id"] == "" {
		t.Error("expected non-empty command id in response")
	}
}

func TestShellExecuteRejectsEmptyCommand(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": ""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestShellExecuteRejectsHookSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Register a hook-mode session
	store.RegisterSession("hook-sess", "idle", "/tmp", "test-model")

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/hook-sess/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400 for hook session", resp.StatusCode)
	}
}

func TestShellExecuteRejectsNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status=%d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./api/ -v -run "TestShellExecute"`
Expected: FAIL — handler not found (404 for all routes)

- [ ] **Step 3: Implement the handler**

Add to `server/api/managed_sessions.go`:

```go
func (s *Server) handleShellExecute(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Timeout > 300 {
		req.Timeout = 300
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}
	if s.manager.IsRunning(sessionID) {
		http.Error(w, "session has an active process", http.StatusConflict)
		return
	}

	commandID := fmt.Sprintf("shell-%d", time.Now().UnixNano())

	proc, err := s.manager.SpawnShell(sessionID, managed.ShellOpts{
		Command: req.Command,
		CWD:     sess.CWD,
		Timeout: time.Duration(req.Timeout) * time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already has a running process") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Persist command message
	_, _ = s.store.CreateMessage(sessionID, "shell", req.Command)
	_ = s.store.SetSessionStatus(sessionID, "running")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	// Broadcast shell_start
	startMsg := fmt.Sprintf(`{"type":"shell_start","command":%s,"id":%s,"cwd":%s}`,
		jsonString(req.Command), jsonString(commandID), jsonString(sess.CWD))
	broadcaster.Send(startMsg)

	// Background: stream output, persist, cleanup
	go func() {
		var stdout, stderr strings.Builder
		const maxOutput = 1024 * 1024 // 1MB cap per stream

		// Stream stdout
		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdoutDone)
			scanner := bufio.NewScanner(proc.Stdout)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if stdout.Len() < maxOutput {
					stdout.WriteString(line + "\n")
				}
				msg := fmt.Sprintf(`{"type":"shell_output","text":%s,"stream":"stdout","id":%s}`,
					jsonString(line+"\n"), jsonString(commandID))
				broadcaster.Send(msg)
			}
		}()

		// Stream stderr
		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			scanner := bufio.NewScanner(proc.Stderr)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if stderr.Len() < maxOutput {
					stderr.WriteString(line + "\n")
				}
				msg := fmt.Sprintf(`{"type":"shell_output","text":%s,"stream":"stderr","id":%s}`,
					jsonString(line+"\n"), jsonString(commandID))
				broadcaster.Send(msg)
			}
		}()

		<-stdoutDone
		<-stderrDone
		<-proc.Done

		timedOut := proc.TimedOut

		// Truncation markers
		stdoutStr := stdout.String()
		if stdout.Len() >= maxOutput {
			stdoutStr += "\n[truncated]"
		}
		stderrStr := stderr.String()
		if stderr.Len() >= maxOutput {
			stderrStr += "\n[truncated]"
		}

		// Persist output
		outputJSON := fmt.Sprintf(`{"stdout":%s,"stderr":%s,"exit_code":%d,"timed_out":%t}`,
			jsonString(stdoutStr), jsonString(stderrStr), proc.ExitCode, timedOut)
		_, _ = s.store.CreateMessage(sessionID, "shell_output", outputJSON)

		// Broadcast exit
		exitMsg := fmt.Sprintf(`{"type":"shell_exit","code":%d,"id":%s,"timeout":%t}`,
			proc.ExitCode, jsonString(commandID), timedOut)
		broadcaster.Send(exitMsg)

		_ = s.store.SetSessionStatus(sessionID, "idle")
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": commandID})
}

// jsonString returns a JSON-encoded string value (with quotes and escaping).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

Add the `"bufio"` import to the imports block at the top of `managed_sessions.go`.

- [ ] **Step 4: Register the route**

Add to `server/api/router.go` in the managed session endpoints section:

```go
apiMux.HandleFunc("POST /api/sessions/{id}/shell", s.handleShellExecute)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run "TestShellExecute"`
Expected: All 4 tests PASS

- [ ] **Step 6: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add server/api/managed_sessions.go server/api/router.go server/api/managed_sessions_test.go
git commit -m "feat: add POST /api/sessions/{id}/shell endpoint for shell execution"
```

---

### Task 3: Add shell UI — mode toggle and command sending

**Files:**
- Modify: `server/web/static/app.js`
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add shell state and method to app.js**

Add to Alpine.js state (after the `lastThresholdSessionId: null,` line around line 89):

```javascript
// Shell mode
shellMode: false,
activeShellId: null,
```

Add the `executeShell` method (after the `sendManagedMessage` method):

```javascript
async executeShell() {
  if (!this.inputText.trim() || !this.selectedSessionId) return;
  const cmd = this.inputText.trim();
  this.inputText = '';
  this.inputSending = true;

  try {
    const res = await fetch(`/api/sessions/${this.selectedSessionId}/shell`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: cmd, timeout: 30 })
    });
    if (!res.ok) throw new Error(await res.text());

    const data = await res.json();
    this.activeShellId = data.id;

    // Add shell command to chat
    this.chatMessages.push({
      role: 'shell',
      content: cmd,
      shellId: data.id,
      cwd: this.currentSession?.cwd || '',
      timestamp: new Date().toISOString()
    });

    // Add shell output placeholder
    this.chatMessages.push({
      role: 'shell_output',
      stdout: '',
      stderr: '',
      exitCode: null,
      timedOut: false,
      shellId: data.id,
      complete: false,
      timestamp: new Date().toISOString()
    });

    this.$nextTick(() => this.scrollToBottom(true));

    // Start SSE to listen for output
    this.startSessionSSE(this.selectedSessionId);
  } catch (e) {
    this.toast('Error: ' + e.message);
  }
  this.inputSending = false;
},
```

- [ ] **Step 2: Update handleInput to route to shell mode**

Modify the `handleInput` method in `app.js`. Change the block:

```javascript
if (sess && sess.mode === 'managed') {
  await this.sendManagedMessage();
}
```

to:

```javascript
if (sess && sess.mode === 'managed') {
  if (this.shellMode) {
    await this.executeShell();
  } else {
    await this.sendManagedMessage();
  }
}
```

- [ ] **Step 3: Update SSE handler for shell events**

In the `startSessionSSE` method, inside the `this.sessionSSE.onmessage` handler, add shell event handling. Before the existing `if (data.type === 'done' || data.type === 'result')` block, add:

```javascript
// Shell events
if (data.type === 'shell_start') {
  this.activeShellId = data.id;
  return;
}
if (data.type === 'shell_output') {
  const outputMsg = this.chatMessages.find(m => m.role === 'shell_output' && m.shellId === data.id);
  if (outputMsg) {
    if (data.stream === 'stderr') {
      outputMsg.stderr += data.text;
    } else {
      outputMsg.stdout += data.text;
    }
    this.$nextTick(() => this.scrollToBottom(false));
  }
  return;
}
if (data.type === 'shell_exit') {
  const outputMsg = this.chatMessages.find(m => m.role === 'shell_output' && m.shellId === data.id);
  if (outputMsg) {
    outputMsg.exitCode = data.code;
    outputMsg.timedOut = data.timeout || false;
    outputMsg.complete = true;
  }
  this.activeShellId = null;
  this.stopSessionSSE();
  return;
}
```

- [ ] **Step 4: Update fetchManagedMessages to include shell roles**

In the `fetchManagedMessages` method, update the filter on line 921:

Change:
```javascript
.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'activity' || (m.role === 'system' && m.content && m.content.includes('"error"')))
```

To:
```javascript
.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'activity' || m.role === 'shell' || m.role === 'shell_output' || (m.role === 'system' && m.content && m.content.includes('"error"')))
```

And add shell message mapping in the `.map()` callback, before the final `return` statement:

```javascript
if (m.role === 'shell') {
  return { role: 'shell', content: m.content, shellId: null, cwd: '', timestamp: m.created_at };
}
if (m.role === 'shell_output') {
  try {
    const parsed = JSON.parse(m.content);
    return {
      role: 'shell_output',
      stdout: parsed.stdout || '',
      stderr: parsed.stderr || '',
      exitCode: parsed.exit_code,
      timedOut: parsed.timed_out || false,
      shellId: null,
      complete: true,
      timestamp: m.created_at
    };
  } catch (e) {
    return { role: 'shell_output', stdout: m.content, stderr: '', exitCode: null, timedOut: false, shellId: null, complete: true, timestamp: m.created_at };
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add shell mode state, executeShell method, and SSE handler for shell events"
```

---

### Task 4: Add shell UI — HTML templates and input toggle

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add shell mode toggle button**

In `index.html`, find the instruction-bar div (around line 172). Add a shell toggle button before the textarea. Replace the instruction-bar div with:

```html
<div class="instruction-bar" x-show="selectedSessionId && !currentPendingPrompt">
  <button class="shell-toggle-btn"
          x-show="currentSession?.mode === 'managed'"
          :class="{ active: shellMode }"
          @click="shellMode = !shellMode"
          :title="shellMode ? 'Switch to chat mode' : 'Switch to shell mode'">$</button>
  <textarea x-model="inputText" rows="1"
         :placeholder="shellMode ? 'Run a command...' : (currentSession?.mode === 'managed' ? 'Send a message... (type /resume to continue a previous session)' : 'Send instruction (delivered on next stop)...')"
         :class="{ 'shell-input': shellMode }"
         @keydown="if(($event.metaKey || $event.ctrlKey) && $event.key === 'Enter') { $event.preventDefault(); handleInput(); }"
         @input="$el.style.height = 'auto'; $el.style.height = Math.min($el.scrollHeight, 150) + 'px'"
         :disabled="inputSending"></textarea>
  <button class="btn btn-primary btn-sm" :disabled="!inputText.trim() || inputSending"
          @click="handleInput()" style="width:auto">
    <span x-show="!inputSuccess">Send</span>
    <span x-show="inputSuccess" style="color:#16a34a">Sent!</span>
  </button>
  <span class="input-hint">⌘↵ to send</span>
</div>
```

- [ ] **Step 2: Add shell message templates**

In `index.html`, find where chat messages are rendered (look for `x-for="msg in chatMessages"`). First, update the existing `msg.role !== 'activity'` template guard to also exclude shell roles: change `x-if="msg.role !== 'activity'"` to `x-if="msg.role !== 'activity' && msg.role !== 'shell' && msg.role !== 'shell_output'"`. Then add shell message templates inside the loop, after the existing activity pill template:

```html
<!-- Shell command -->
<template x-if="msg.role === 'shell'">
  <div class="shell-command-block">
    <div class="shell-header">
      <span class="shell-prompt">$</span>
      <span class="shell-cmd" x-text="msg.content"></span>
    </div>
  </div>
</template>

<!-- Shell output -->
<template x-if="msg.role === 'shell_output'">
  <div class="shell-output-block">
    <pre class="shell-stdout" x-show="msg.stdout" x-text="msg.stdout"></pre>
    <pre class="shell-stderr" x-show="msg.stderr" x-text="msg.stderr"></pre>
    <div class="shell-exit" x-show="msg.complete">
      <span class="exit-badge" :class="msg.exitCode === 0 ? 'exit-success' : 'exit-error'"
            x-text="msg.timedOut ? 'timeout' : ('exit ' + msg.exitCode)"></span>
    </div>
  </div>
</template>
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat: add shell mode toggle and shell message templates to HTML"
```

---

### Task 5: Add shell CSS styling

**Files:**
- Modify: `server/web/static/style.css`

- [ ] **Step 1: Add shell styles**

Append to `server/web/static/style.css`:

```css
/* Shell mode toggle button */
.shell-toggle-btn {
  background: var(--bg-secondary, #f3f4f6);
  border: 1px solid var(--border, #d1d5db);
  border-radius: 4px;
  padding: 4px 8px;
  font-family: monospace;
  font-weight: bold;
  font-size: 14px;
  cursor: pointer;
  color: var(--text-secondary, #6b7280);
  flex-shrink: 0;
}
.shell-toggle-btn.active {
  background: #1a1a2e;
  color: #4ade80;
  border-color: #4ade80;
}

/* Shell input styling */
.shell-input {
  font-family: monospace !important;
  background: #1a1a2e !important;
  color: #e2e8f0 !important;
}

/* Shell command block */
.shell-command-block {
  background: #1a1a2e;
  border-radius: 6px;
  padding: 8px 12px;
  margin: 4px 0;
}
.shell-header {
  font-family: monospace;
  font-size: 13px;
  color: #e2e8f0;
}
.shell-prompt {
  color: #4ade80;
  font-weight: bold;
  margin-right: 6px;
}
.shell-cmd {
  color: #e2e8f0;
}

/* Shell output block */
.shell-output-block {
  background: #1a1a2e;
  border-radius: 6px;
  padding: 8px 12px;
  margin: 0 0 4px 0;
  border-top: 1px solid #2d2d44;
}
.shell-stdout {
  font-family: monospace;
  font-size: 12px;
  color: #e2e8f0;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  padding: 0;
}
.shell-stderr {
  font-family: monospace;
  font-size: 12px;
  color: #f87171;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  padding: 0;
}

/* Exit code badges */
.shell-exit {
  margin-top: 4px;
  padding-top: 4px;
  border-top: 1px solid #2d2d44;
}
.exit-badge {
  font-family: monospace;
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 3px;
}
.exit-success {
  background: #166534;
  color: #4ade80;
}
.exit-error {
  background: #7f1d1d;
  color: #f87171;
}
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat: add terminal-styled shell CSS for command and output rendering"
```

---

### Task 6: Integration test and manual verification

**Files:**
- Test: `server/api/managed_sessions_test.go`

- [ ] **Step 1: Write integration test for shell output persistence**

Add to `server/api/managed_sessions_test.go`:

Add `"time"` to the import block in `managed_sessions_test.go`. Then add:

```go
func TestShellExecutePersistsMessages(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": "echo hello", "timeout": 5}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Poll for shell_output message (avoids flaky time.Sleep)
	deadline := time.Now().Add(10 * time.Second)
	var foundShell, foundOutput bool
	for time.Now().Before(deadline) {
		msgs, err := store.ListMessages(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		foundShell = false
		foundOutput = false
		for _, m := range msgs {
			if m.Role == "shell" && m.Content == "echo hello" {
				foundShell = true
			}
			if m.Role == "shell_output" {
				foundOutput = true
			}
		}
		if foundShell && foundOutput {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !foundShell {
		t.Error("expected shell command message to be persisted")
	}
	if !foundOutput {
		t.Error("expected shell_output message to be persisted")
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd server && go test ./api/ -v -run "TestShellExecutePersistsMessages" -timeout 30s`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add server/api/managed_sessions_test.go
git commit -m "test: add integration test for shell command message persistence"
```

- [ ] **Step 5: Manual smoke test**

Run: `cd server && go run .`
Open the web UI, create a managed session, toggle to shell mode, run `ls -la` and `echo hello world`. Verify:
- Output streams in real-time
- Exit code badge appears
- Shell history persists on page reload
- Shell commands are rejected while Claude is running
