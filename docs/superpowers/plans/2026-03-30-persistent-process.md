# Persistent Process Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-turn `claude -p` process spawning with a single long-lived process per session using `--input-format stream-json`, eliminating startup latency and fragile `--resume` chaining.

**Architecture:** Keep the Go server. Add `Stdin io.WriteCloser` and `LastActivity time.Time` to `Process`. New `EnsureProcess` lazily spawns or returns a warm process. New `SendTurn` writes user JSON to stdin. `StreamNDJSON` stays alive across turns, using `result` messages as turn delimiters instead of EOF. `--resume` becomes a recovery-only fallback.

**Tech Stack:** Go, `claude -p --input-format stream-json --output-format stream-json`

**Spec:** `docs/superpowers/specs/2026-03-30-persistent-process-design.md`

---

### Task 1: Add Stdin and LastActivity to Process struct

**Files:**
- Modify: `server/managed/manager.go:26-33` (Process struct)
- Modify: `server/managed/manager.go:82-138` (Spawn method — add Stdin pipe)
- Test: `server/managed/manager_test.go`

- [ ] **Step 1: Write failing test — Spawn exposes Stdin**

Add to `server/managed/manager_test.go`:

```go
func TestSpawnExposesStdin(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.Spawn("stdin-test", SpawnOpts{
		Args: []string{},
		CWD:  "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Teardown("stdin-test", 2*time.Second)

	if proc.Stdin == nil {
		t.Fatal("proc.Stdin should not be nil")
	}

	// Write to stdin and close — cat should echo it back via stdout
	_, err = proc.Stdin.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("write to stdin: %v", err)
	}
	proc.Stdin.Close()

	<-proc.Done
	if proc.ExitCode != 0 {
		t.Errorf("exit code=%d, want 0", proc.ExitCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestSpawnExposesStdin`
Expected: FAIL — `proc.Stdin undefined (type *Process has no field or method Stdin)`

- [ ] **Step 3: Add Stdin and LastActivity to Process, pipe stdin in Spawn**

In `server/managed/manager.go`, update the `Process` struct:

```go
type Process struct {
	Cmd          *exec.Cmd
	Stdin        io.WriteCloser
	Stdout       io.ReadCloser
	Stderr       io.ReadCloser
	Done         chan struct{}
	ExitCode     int
	TimedOut     bool
	LastActivity time.Time
}
```

In the `Spawn` method, after creating `cmd` and before `cmd.StdoutPipe()`, add:

```go
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
```

And set it on the Process:

```go
	proc := &Process{
		Cmd:          cmd,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		Done:         make(chan struct{}),
		LastActivity: time.Now(),
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestSpawnExposesStdin`
Expected: PASS

- [ ] **Step 5: Run all existing tests to ensure nothing broke**

Run: `cd server && go test ./managed/ -v`
Expected: All tests PASS (existing tests don't reference Stdin, so they're unaffected)

- [ ] **Step 6: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat(managed): add Stdin and LastActivity fields to Process struct"
```

---

### Task 2: Add TurnDone channel to StreamNDJSON for turn delimiting

**Files:**
- Modify: `server/managed/stream.go:69-115` (StreamNDJSON function)
- Test: `server/managed/stream_test.go`

Currently `StreamNDJSON` blocks until EOF. For persistent processes, the caller needs to know when a turn is done (a `result` message arrived) without the stream closing. We add an optional `TurnDone chan struct{}` that fires on each `result` message.

- [ ] **Step 1: Write failing test — StreamNDJSON signals TurnDone on result**

Add to `server/managed/stream_test.go`:

```go
func TestStreamNDJSON_SignalsTurnDone(t *testing.T) {
	pr, pw := io.Pipe()
	b := NewBroadcaster()

	turnDone := make(chan struct{}, 4)
	var persisted []string
	onLine := func(line string) {
		persisted = append(persisted, line)
	}

	// Run StreamNDJSON in a goroutine since it blocks until EOF
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		StreamNDJSON(pr, b, onLine, turnDone)
	}()

	// Send first turn
	pw.Write([]byte(`{"type":"assistant","message":{"content":"hello"}}` + "\n"))
	pw.Write([]byte(`{"type":"result","session_id":"abc"}` + "\n"))

	select {
	case <-turnDone:
		// good — turn delimiter received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first TurnDone signal")
	}

	// Send second turn
	pw.Write([]byte(`{"type":"assistant","message":{"content":"world"}}` + "\n"))
	pw.Write([]byte(`{"type":"result","session_id":"abc"}` + "\n"))

	select {
	case <-turnDone:
		// good — second turn delimiter received
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second TurnDone signal")
	}

	// Close pipe to end the stream
	pw.Close()
	<-streamDone

	if len(persisted) != 4 {
		t.Errorf("got %d persisted lines, want 4", len(persisted))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestStreamNDJSON_SignalsTurnDone`
Expected: FAIL — `too many arguments in call to StreamNDJSON`

- [ ] **Step 3: Add TurnDone parameter to StreamNDJSON**

Update the `StreamNDJSON` signature and body in `server/managed/stream.go`:

```go
// StreamNDJSON reads NDJSON lines from r, broadcasts each via b, and calls onLine for each.
// onLine is called synchronously per line (use for persistence/turn counting).
// If turnDone is non-nil, a signal is sent on each "result" message (turn delimiter).
// A heartbeat JSON message is broadcast at HeartbeatInterval; onLine is NOT called for heartbeats.
// Blocks until r is closed/EOF.
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string), turnDone chan<- struct{}) {
```

Inside the line handling case, after `onLine(res.line)`, add:

```go
			if turnDone != nil {
				var obj struct{ Type string `json:"type"` }
				if json.Unmarshal([]byte(res.line), &obj) == nil && obj.Type == "result" {
					select {
					case turnDone <- struct{}{}:
					default:
					}
				}
			}
```

Add `"encoding/json"` to the import block in `stream.go`.

- [ ] **Step 4: Fix all existing callers to pass nil for turnDone**

In `server/api/managed_sessions.go:236`, update:

```go
managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, nil)
```

- [ ] **Step 5: Fix all existing tests to pass nil for turnDone**

In `server/managed/stream_test.go`, update every existing call to `StreamNDJSON` to add `nil` as the fourth argument:

- `TestStreamNDJSON` (line 85): `StreamNDJSON(strings.NewReader(input), b, onLine, nil)`
- `TestStreamNDJSONCountsAssistantTurns` (line 112): `StreamNDJSON(strings.NewReader(input), b, onLine, nil)`
- `TestStreamNDJSON_SendsHeartbeats` (line 156): `StreamNDJSON(pr, b, nil, nil)`
- `TestStreamNDJSON_OnLineNotCalledForHeartbeats` (line 201): `StreamNDJSON(pr, b, onLine, nil)`
- `TestStreamNDJSON_BroadcastsLines` (line 226): `StreamNDJSON(strings.NewReader(input), b, nil, nil)`

- [ ] **Step 6: Run all tests**

Run: `cd server && go test ./managed/ -v && go test ./api/ -v`
Expected: All tests PASS including the new `TestStreamNDJSON_SignalsTurnDone`

- [ ] **Step 7: Commit**

```bash
git add server/managed/stream.go server/managed/stream_test.go server/api/managed_sessions.go
git commit -m "feat(managed): add TurnDone channel to StreamNDJSON for turn delimiting"
```

---

### Task 3: Add EnsureProcess and SendTurn to Manager

**Files:**
- Modify: `server/managed/manager.go`
- Test: `server/managed/manager_test.go`

- [ ] **Step 1: Write failing test — EnsureProcess spawns on first call, reuses on second**

Add to `server/managed/manager_test.go`:

```go
func TestEnsureProcessSpawnsAndReuses(t *testing.T) {
	// Use 'cat' as a persistent process that reads stdin and writes to stdout
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc1, err := m.EnsureProcess("reuse-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if proc1 == nil {
		t.Fatal("proc1 is nil")
	}

	proc2, err := m.EnsureProcess("reuse-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	if proc1 != proc2 {
		t.Error("EnsureProcess should return the same process on second call")
	}

	m.Teardown("reuse-test", 2*time.Second)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestEnsureProcessSpawnsAndReuses`
Expected: FAIL — `m.EnsureProcess undefined`

- [ ] **Step 3: Implement EnsureProcess**

Add to `server/managed/manager.go`:

```go
// EnsureProcess returns an existing warm process for the session, or spawns a new one.
// Unlike Spawn, calling EnsureProcess when a process already exists is not an error.
func (m *Manager) EnsureProcess(sessionID string, opts SpawnOpts) (*Process, error) {
	m.mu.Lock()
	if proc, ok := m.procs[sessionID]; ok {
		proc.LastActivity = time.Now()
		m.mu.Unlock()
		return proc, nil
	}
	m.mu.Unlock()

	return m.Spawn(sessionID, opts)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestEnsureProcessSpawnsAndReuses`
Expected: PASS

- [ ] **Step 5: Write failing test — SendTurn writes to stdin**

Add to `server/managed/manager_test.go`:

```go
func TestSendTurn(t *testing.T) {
	// Use 'cat' — it echoes stdin to stdout
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("send-turn-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	msg := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`
	err = m.SendTurn("send-turn-test", msg)
	if err != nil {
		t.Fatalf("SendTurn failed: %v", err)
	}

	// Read what cat echoed back
	buf := make([]byte, 4096)
	n, err := proc.Stdout.Read(buf)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	got := strings.TrimSpace(string(buf[:n]))
	if got != msg {
		t.Errorf("got %q, want %q", got, msg)
	}

	m.Teardown("send-turn-test", 2*time.Second)
}
```

Add `"strings"` to the import block in `manager_test.go` if not already present.

- [ ] **Step 6: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestSendTurn`
Expected: FAIL — `m.SendTurn undefined`

- [ ] **Step 7: Implement SendTurn**

Add to `server/managed/manager.go`:

```go
// SendTurn writes a user message JSON line to the process's stdin.
// Returns an error if no process exists for the session.
func (m *Manager) SendTurn(sessionID string, messageJSON string) error {
	m.mu.Lock()
	proc, ok := m.procs[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running process for session %s", sessionID)
	}

	proc.LastActivity = time.Now()
	_, err := fmt.Fprintf(proc.Stdin, "%s\n", messageJSON)
	return err
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestSendTurn`
Expected: PASS

- [ ] **Step 9: Run all managed tests**

Run: `cd server && go test ./managed/ -v`
Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat(managed): add EnsureProcess and SendTurn methods"
```

---

### Task 4: Add idle timeout reaper

**Files:**
- Modify: `server/managed/manager.go`
- Test: `server/managed/manager_test.go`

- [ ] **Step 1: Write failing test — idle process gets reaped**

Add to `server/managed/manager_test.go`:

```go
func TestIdleTimeoutReapsProcess(t *testing.T) {
	cfg := Config{
		ClaudeBin:  "cat",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("idle-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	// Backdate LastActivity to trigger immediate reap
	m.mu.Lock()
	proc.LastActivity = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	m.ReapIdle(1 * time.Millisecond)

	// Process should be gone
	select {
	case <-proc.Done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("process was not reaped after idle timeout")
	}

	if m.IsRunning("idle-test") {
		t.Error("session should not be running after reap")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestIdleTimeoutReapsProcess`
Expected: FAIL — `m.ReapIdle undefined`

- [ ] **Step 3: Implement ReapIdle**

Add to `server/managed/manager.go`:

```go
// ReapIdle closes stdin on any process that has been idle longer than maxIdle.
// This lets the process exit gracefully. Call this periodically from a goroutine.
func (m *Manager) ReapIdle(maxIdle time.Duration) {
	m.mu.Lock()
	var toReap []string
	now := time.Now()
	for id, proc := range m.procs {
		if now.Sub(proc.LastActivity) > maxIdle {
			toReap = append(toReap, id)
		}
	}
	m.mu.Unlock()

	for _, id := range toReap {
		m.mu.Lock()
		proc, ok := m.procs[id]
		m.mu.Unlock()
		if ok && proc.Stdin != nil {
			log.Printf("reaping idle process for session %s", id)
			proc.Stdin.Close()
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestIdleTimeoutReapsProcess`
Expected: PASS

- [ ] **Step 5: Run all managed tests**

Run: `cd server && go test ./managed/ -v`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go
git commit -m "feat(managed): add ReapIdle for idle timeout cleanup"
```

---

### Task 5: Add idle timeout configuration and reaper goroutine

**Files:**
- Modify: `server/managed/manager.go` (Config struct, NewManager, StartReaper)
- Modify: `server/main.go` or wherever the manager is created (to call StartReaper)
- Test: `server/managed/manager_test.go`

- [ ] **Step 1: Add IdleTimeoutMinutes to Config and start reaper**

In `server/managed/manager.go`, update the Config struct:

```go
type Config struct {
	ClaudeBin         string
	ClaudeArgs        []string
	ClaudeEnv         []string
	ServerPort        int
	BinaryPath        string
	IdleTimeoutMinutes int
}
```

Add a `StartReaper` method:

```go
// StartReaper starts a background goroutine that periodically calls ReapIdle.
// If IdleTimeoutMinutes is 0, defaults to 30 minutes.
func (m *Manager) StartReaper() {
	m.mu.Lock()
	timeout := m.cfg.IdleTimeoutMinutes
	m.mu.Unlock()

	if timeout <= 0 {
		timeout = 30
	}
	maxIdle := time.Duration(timeout) * time.Minute

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.ReapIdle(maxIdle)
		}
	}()
}
```

- [ ] **Step 2: Wire StartReaper into server startup**

Find where `NewManager` is called (likely in `server/main.go` or `server/cmd/`). After `NewManager(cfg)`, add:

```go
mgr.StartReaper()
```

This is a low-risk addition — the reaper only affects processes with `LastActivity` older than the timeout, and no processes exist at startup.

- [ ] **Step 3: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS. The new `IdleTimeoutMinutes` field defaults to zero which is fine — existing tests don't use it.

- [ ] **Step 4: Commit**

```bash
git add server/managed/manager.go server/main.go
git commit -m "feat(managed): add configurable idle timeout with background reaper"
```

---

### Task 6: Integrate persistent process into handleSendMessage

**Files:**
- Modify: `server/api/managed_sessions.go:116-349` (handleSendMessage and buildClaudeArgs)

This is the critical integration task. The `handleSendMessage` handler currently builds args with the user message baked in (`-p "message"`), spawns a new process, and reads until EOF. We need to:

1. Build args **without** the message (the message goes via stdin)
2. Use `EnsureProcess` instead of `Spawn`
3. On first message, add `--input-format stream-json`
4. Write the user message as JSON to stdin via `SendTurn`
5. Wait for `TurnDone` instead of EOF

- [ ] **Step 1: Create buildPersistentArgs helper**

Add to `server/api/managed_sessions.go`, below the existing `buildClaudeArgs`:

```go
// buildPersistentArgs builds CLI args for a persistent process (no message in args).
// The message will be sent via stdin as stream-json.
func buildPersistentArgs(sess *db.Session, cfg managed.Config) []string {
	var args []string
	args = append(args, "-p")

	resumeID := sess.ID
	if sess.ClaudeSessionID != "" {
		resumeID = sess.ClaudeSessionID
	}
	if sess.Initialized {
		args = append(args, "--resume", resumeID)
	} else {
		args = append(args, "--session-id", resumeID)
	}

	args = append(args, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")

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

- [ ] **Step 2: Create formatUserTurn helper**

Add to `server/api/managed_sessions.go`:

```go
// formatUserTurn formats a user message as a stream-json input line.
func formatUserTurn(message string) string {
	turn := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]string{
				{"type": "text", "text": message},
			},
		},
	}
	b, _ := json.Marshal(turn)
	return string(b)
}
```

- [ ] **Step 3: Rewrite handleSendMessage to use persistent process**

Replace the goroutine body in `handleSendMessage` (the `go func()` at line 154). The key design change: `StreamNDJSON` runs once per process lifetime, so the `onLine` callback and `turnDone` channel must be created **outside** the auto-continue loop. Per-turn mutable state (`turnsSinceLastContinue`, `autoInterrupting`, `executionErrored`) is protected by a mutex so the long-lived callback can safely read/write it across turns.

```go
	go func() {
		ctx := context.Background()
		continuationCount := 0
		threshold := int(sess.AutoContinueThreshold * float64(sess.MaxTurns))
		currentMessage := req.Message

		// --- Per-turn mutable state, shared with onLine callback ---
		// onLine runs in the StreamNDJSON goroutine, which lives for the
		// process lifetime. These vars are reset at the top of each turn.
		var mu sync.Mutex
		var turnsSinceLastContinue int
		var autoInterrupting bool
		var executionErrored bool

		// turnDone is signaled by StreamNDJSON on each "result" message.
		// Buffered so the signal isn't lost if we're between select calls.
		turnDone := make(chan struct{}, 4)

		// streamDone closes when the StreamNDJSON goroutine exits (process EOF).
		streamDone := make(chan struct{})

		// onLine persists for the process lifetime — references mutable state via mu.
		onLine := func(line string) {
			role := parseRole(line)
			if role == "heartbeat" {
				return
			}
			if role == "result" {
				var res struct {
					Subtype string   `json:"subtype"`
					IsError bool     `json:"is_error"`
					Errors  []string `json:"errors"`
				}
				if json.Unmarshal([]byte(line), &res) == nil && res.Subtype == "error_during_execution" {
					mu.Lock()
					executionErrored = true
					mu.Unlock()
					if len(res.Errors) > 0 {
						errText := strings.Join(res.Errors, "; ")
						_, _ = s.store.CreateMessage(sessionID, "assistant", errText)
					}
				}
			}
			if role == "assistant" {
				text := extractAssistantText(line)
				if text != "" {
					_, _ = s.store.CreateMessage(sessionID, role, text)
				}
				for _, toolName := range extractToolNames(line) {
					_, _ = s.store.CreateMessage(sessionID, "activity", toolName)
				}
				mu.Lock()
				turnsSinceLastContinue++
				mu.Unlock()
				count, _ := s.store.IncrementTurnCount(sessionID)
				mu.Lock()
				if count >= threshold && !autoInterrupting {
					autoInterrupting = true
					mu.Unlock()
					log.Printf("session %s hit auto-continue threshold (%d/%d), interrupting", sessionID, count, sess.MaxTurns)
					_ = s.manager.Interrupt(sessionID)
				} else {
					mu.Unlock()
				}
			}
			extractSessionFiles(line, sessionID, s.store)
		}

		// Track whether we've started the stream reader for the current process.
		var proc *managed.Process
		var procStarted bool

		for {
			select {
			case <-ctx.Done():
				log.Printf("session %s: client disconnected, stopping auto-continue", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			default:
			}

			// Reset per-turn state
			_ = s.store.ResetTurnCount(sessionID)
			mu.Lock()
			turnsSinceLastContinue = 0
			autoInterrupting = false
			executionErrored = false
			mu.Unlock()

			// Drain any stale turnDone signals from previous turns
			for {
				select {
				case <-turnDone:
				default:
					goto drained
				}
			}
		drained:

			// Ensure we have a warm process (spawns on first call, reuses after)
			var err error
			proc, err = s.manager.EnsureProcess(sessionID, managed.SpawnOpts{
				Args: buildPersistentArgs(sess, s.manager.Config()),
				CWD:  sess.CWD,
			})
			if err != nil {
				log.Printf("ensure process error for session %s: %v", sessionID, err)
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to start process: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				break
			}

			// Start the stdout reader only once per process
			if !procStarted {
				procStarted = true
				go func() {
					defer close(streamDone)
					managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, turnDone)
				}()
			}

			// Send the user message via stdin
			userTurn := formatUserTurn(currentMessage)
			if err := s.manager.SendTurn(sessionID, userTurn); err != nil {
				log.Printf("send turn error for session %s: %v", sessionID, err)
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to send message: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				break
			}

			// Wait for either turn completion or process death
			select {
			case <-turnDone:
				// Turn completed normally
			case <-proc.Done:
				// Process died — fall through to recovery
			}

			mu.Lock()
			errored := executionErrored
			interrupted := autoInterrupting
			turns := turnsSinceLastContinue
			mu.Unlock()

			if !sess.Initialized && !errored {
				_ = s.store.SetInitialized(sessionID)
				sess.Initialized = true
			}

			// Clean up any pending permission request
			s.permissions.Delete(sessionID)

			// Check if process died
			select {
			case <-proc.Done:
				// Process exited — drain stream, clean up
				stderrBytes, _ := io.ReadAll(proc.Stderr)
				<-streamDone

				os.Remove(fmt.Sprintf("/tmp/claude-mcp-%s.json", sessionID))

				if proc.ExitCode != 0 && len(stderrBytes) > 0 {
					errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
					_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
					broadcaster.Send(errMsg)
				}

				if proc.ExitCode == 0 && !interrupted {
					_ = s.store.UpdateActivityState(sessionID, "waiting")
					doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
					broadcaster.Send(doneMsg)
					return
				}

				if !interrupted {
					_ = s.store.UpdateActivityState(sessionID, "idle")
					doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
					broadcaster.Send(doneMsg)
					return
				}

				// Process died during auto-interrupt — need fresh process for next turn.
				// Reset stream state so next EnsureProcess starts a new reader.
				procStarted = false
				turnDone = make(chan struct{}, 4)
				streamDone = make(chan struct{})

			default:
				// Process still alive — turn completed normally
				if !interrupted {
					_ = s.store.UpdateActivityState(sessionID, "waiting")
					doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
					broadcaster.Send(doneMsg)
					return
				}
			}

			// --- Auto-continue logic ---

			if turns < 2 {
				log.Printf("session %s not making progress (%d turns), stopping auto-continue", sessionID, turns)
				_, _ = s.store.CreateMessage(sessionID, "system", "Auto-continue stopped: not making progress")
				noProgressMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d,"reason":"no_progress"}`, continuationCount)
				broadcaster.Send(noProgressMsg)
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			continuationCount++

			if continuationCount > sess.MaxContinuations {
				log.Printf("session %s exhausted auto-continues (%d/%d)", sessionID, continuationCount, sess.MaxContinuations)
				exhaustedMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d}`, continuationCount)
				broadcaster.Send(exhaustedMsg)
				_, _ = s.store.CreateMessage(sessionID, "system",
					fmt.Sprintf("Auto-continue limit reached (%d/%d)", continuationCount, sess.MaxContinuations))
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			// Auto-continue
			continuingMsg := fmt.Sprintf(`{"type":"auto_continuing","continuation_count":%d,"max_continuations":%d}`,
				continuationCount, sess.MaxContinuations)
			broadcaster.Send(continuingMsg)
			_, _ = s.store.CreateMessage(sessionID, "system",
				fmt.Sprintf("Auto-continuing (%d/%d)...", continuationCount, sess.MaxContinuations))

			// Compact step
			if sess.CompactEveryNContinues > 0 && continuationCount%sess.CompactEveryNContinues == 0 {
				compactingMsg := fmt.Sprintf(`{"type":"compacting","continuation_count":%d}`, continuationCount)
				broadcaster.Send(compactingMsg)
				_, _ = s.store.CreateMessage(sessionID, "system", "Running /compact to reduce context size...")

				compactTurn := formatUserTurn("/compact")
				if err := s.manager.SendTurn(sessionID, compactTurn); err != nil {
					log.Printf("session %s: compact send failed: %v", sessionID, err)
					_, _ = s.store.CreateMessage(sessionID, "system", "Compact failed, continuing without it.")
				} else {
					select {
					case <-turnDone:
						log.Printf("session %s: compact completed", sessionID)
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact complete.")
					case <-proc.Done:
						log.Printf("session %s: process died during compact", sessionID)
					case <-time.After(2 * time.Minute):
						log.Printf("session %s: compact timed out", sessionID)
					}
				}
				compactCompleteMsg := fmt.Sprintf(`{"type":"compact_complete","continuation_count":%d}`, continuationCount)
				broadcaster.Send(compactCompleteMsg)
			}

			currentMessage = "You were interrupted due to turn limits. Continue where you left off."
		}
	}()
```

Note: add `"sync"` to the import block in `managed_sessions.go`.

- [ ] **Step 4: Update the conflict check at handler top**

The current check at line 144 (`s.manager.IsRunning(sessionID)`) should be updated. With persistent processes, a warm process in `waiting` state is fine — we just send it a new turn. The conflict is only when a turn is actively being processed. For now, keep the check but make it check activity state from the DB instead:

```go
	// A warm process in "waiting" state is fine — we'll send it a new turn.
	// Only block if actively processing.
	sess, err := s.store.GetSessionByID(sessionID)
	// ... existing error handling ...
	if sess.ActivityState == "working" {
		http.Error(w, "session is currently processing", http.StatusConflict)
		return
	}
```

Move the existing session fetch up before this check (it's currently at line 131) so it's available.

- [ ] **Step 5: Build to check compilation**

Run: `cd server && go build ./...`
Expected: No compilation errors

- [ ] **Step 6: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat(managed): integrate persistent process into handleSendMessage

Replace per-turn Spawn with EnsureProcess + SendTurn. The process stays
warm between turns, eliminating startup latency. --resume is only used
for recovery after process death or idle timeout. Compact is now sent
via stdin instead of spawning a separate process."
```

---

### Task 7: Manual integration test (no code changes)

**Files:** None — this is a verification task

- [ ] **Step 1: Build and start the server**

```bash
cd server && go build -o claude-controller . && ./claude-controller
```

- [ ] **Step 2: Create a managed session via the web UI or curl**

```bash
curl -X POST http://localhost:8080/api/managed-sessions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-api-key>" \
  -d '{"cwd":"/tmp"}'
```

- [ ] **Step 3: Send a first message — verify process spawns**

```bash
curl -X POST http://localhost:8080/api/managed-sessions/<id>/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-api-key>" \
  -d '{"message":"What is 2+2?"}'
```

Check server logs for the process spawn. Connect to SSE stream and verify output arrives.

- [ ] **Step 4: Send a second message — verify no new process spawn**

Send another message. Check server logs: there should be **no new process spawn**. The turn should start immediately via stdin write. Verify response arrives via SSE.

- [ ] **Step 5: Wait for idle timeout — verify process is reaped**

Wait for the idle timeout (or set it short for testing). Verify the process exits cleanly. Send another message and verify it spawns a new process with `--resume`.

- [ ] **Step 6: Test interrupt**

Send a long-running message, then call the interrupt endpoint. Verify the process receives SIGINT and the turn terminates.

---

### Task 8: Clean up old buildClaudeArgs if no longer needed

**Files:**
- Modify: `server/api/managed_sessions.go`

- [ ] **Step 1: Check if buildClaudeArgs is still used anywhere**

Run: `grep -rn "buildClaudeArgs" server/`

If it's only used in the old spawn path (which has been replaced), remove it.

- [ ] **Step 2: Remove buildClaudeArgs if unused**

Delete the function and any tests that reference it.

- [ ] **Step 3: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "refactor: remove unused buildClaudeArgs after persistent process migration"
```
