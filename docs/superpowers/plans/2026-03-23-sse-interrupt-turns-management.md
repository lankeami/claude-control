# SSE Interrupt for Turns Management — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-continue managed sessions when turns run low, preventing Claude from stalling due to turn limits.

**Architecture:** Extend `handleSendMessage`'s streaming goroutine with a loop that detects threshold crossings, sends SIGINT, and re-spawns `claude -p --resume` with a continuation prompt. Two new DB columns store configuration; continuation count is tracked in-memory. The web UI handles two new SSE event types to render auto-continue status.

**Tech Stack:** Go (server), SQLite (DB), Alpine.js (web UI), SSE (streaming)

**Spec:** `docs/superpowers/specs/2026-03-23-sse-interrupt-turns-management-design.md`

---

### Task 1: Add DB columns and update Session struct

**Files:**
- Modify: `server/db/db.go:97` (add migrations)
- Modify: `server/db/sessions.go:11-28` (Session struct, columns, scanner)

- [ ] **Step 1: Add migration statements**

In `server/db/db.go`, add two new `ALTER TABLE` entries to the `migrations` slice (after the existing `turn_count` migration at line 97):

```go
`ALTER TABLE sessions ADD COLUMN auto_continue_threshold REAL NOT NULL DEFAULT 0.8`,
`ALTER TABLE sessions ADD COLUMN max_continuations INTEGER NOT NULL DEFAULT 5`,
```

- [ ] **Step 2: Update Session struct**

In `server/db/sessions.go`, add fields to the `Session` struct:

```go
AutoContinueThreshold float64 `json:"auto_continue_threshold"`
MaxContinuations      int     `json:"max_continuations"`
```

- [ ] **Step 3: Update sessionColumns and scanSession**

Update `sessionColumns` constant to include the new columns:

```go
const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized, COALESCE(claude_session_id,''), turn_count, auto_continue_threshold, max_continuations`
```

Update `scanSession` to scan the two new fields:

```go
err := scanner.Scan(
    &sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
    &sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
    &sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
    &sess.ClaudeSessionID, &sess.TurnCount, &sess.AutoContinueThreshold, &sess.MaxContinuations,
)
```

- [ ] **Step 4: Run existing tests to verify no regressions**

Run: `cd server && go test ./db/ -v`
Expected: All existing tests PASS (migrations are additive, defaults handle existing rows)

- [ ] **Step 5: Commit**

```bash
git add server/db/db.go server/db/sessions.go
git commit -m "feat: add auto_continue_threshold and max_continuations columns"
```

---

### Task 2: Add DB test for new columns

**Files:**
- Modify: `server/db/sessions_test.go`

- [ ] **Step 1: Write test for auto-continue defaults**

Add a test that creates a managed session and verifies the default values:

```go
func TestAutoContinueDefaults(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-ac", `["Bash"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if sess.AutoContinueThreshold != 0.8 {
		t.Errorf("expected threshold 0.8, got %f", sess.AutoContinueThreshold)
	}
	if sess.MaxContinuations != 5 {
		t.Errorf("expected max_continuations 5, got %d", sess.MaxContinuations)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd server && go test ./db/ -v -run TestAutoContinueDefaults`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add server/db/sessions_test.go
git commit -m "test: verify auto_continue defaults on managed sessions"
```

---

### Task 3: Refactor handleSendMessage into auto-continue loop

This is the core change. The streaming goroutine currently runs once; we wrap it in a loop.

**Files:**
- Modify: `server/api/managed_sessions.go:56-190`

- [ ] **Step 1: Extract arg-building into a helper**

Create a helper function `buildClaudeArgs` that builds the `claude -p` args from session + message. This will be called once for the initial message, and again for each continuation (with the continuation prompt).

Add this before `handleSendMessage`:

```go
func buildClaudeArgs(sess *db.Session, message string) []string {
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

	return args
}
```

- [ ] **Step 2: Replace inline arg-building in handleSendMessage**

Replace lines 86-112 in `handleSendMessage` with:

```go
args := buildClaudeArgs(sess, req.Message)
```

- [ ] **Step 3: Run tests to verify no regression**

Run: `cd server && go test ./api/ -v`
Expected: All existing API tests PASS

- [ ] **Step 4: Commit refactor**

```bash
git add server/api/managed_sessions.go
git commit -m "refactor: extract buildClaudeArgs helper from handleSendMessage"
```

---

### Task 4: Implement the auto-continue streaming loop

**Files:**
- Modify: `server/api/managed_sessions.go` (the goroutine in `handleSendMessage`)

- [ ] **Step 1: Rewrite the streaming goroutine**

Replace the goroutine (lines 137-186) with the auto-continue loop. Key changes:

1. Wrap spawn + stream in a `for` loop
2. Track `continuationCount` and `autoInterrupting` as local variables
3. Track `turnsSinceLastContinue` for minimum-progress guard
4. In `onLine`, when threshold is hit: set `autoInterrupting = true`, call `Interrupt()`
5. After `StreamNDJSON` returns and process exits, check whether to auto-continue
6. Only send `done` event when the loop terminates

```go
go func() {
	defer func() {
		_ = s.store.SetSessionStatus(sessionID, "idle")
	}()

	ctx := r.Context()
	continuationCount := 0
	// int() truncates toward zero, same as floor() for positive numbers
	threshold := int(sess.AutoContinueThreshold * float64(sess.MaxTurns))
	currentMessage := req.Message

	for {
		// Check context cancellation before each spawn (SSE client disconnect)
		select {
		case <-ctx.Done():
			log.Printf("session %s: client disconnected, stopping auto-continue", sessionID)
			doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
			broadcaster.Send(doneMsg)
			return
		default:
		}

		_ = s.store.ResetTurnCount(sessionID)
		turnsSinceLastContinue := 0
		autoInterrupting := false

		args := buildClaudeArgs(sess, currentMessage)
		proc, err := s.manager.Spawn(sessionID, managed.SpawnOpts{
			Args: args,
			CWD:  sess.CWD,
		})
		if err != nil {
			log.Printf("auto-continue spawn error for session %s: %v", sessionID, err)
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to spawn process: %s"}`, err.Error())
			broadcaster.Send(errMsg)
			break
		}

		// After first spawn, ensure subsequent spawns use --resume
		if !sess.Initialized {
			_ = s.store.SetInitialized(sessionID)
			sess.Initialized = true
		}

		// onLine runs synchronously inside StreamNDJSON's read loop.
		// autoInterrupting is safe to read after StreamNDJSON returns
		// because StreamNDJSON blocks until stdout EOF, providing a
		// happens-before guarantee with <-proc.Done.
		onLine := func(line string) {
			if parseRole(line) == "heartbeat" {
				return
			}
			role := parseRole(line)
			if role == "assistant" {
				text := extractAssistantText(line)
				if text != "" {
					_, _ = s.store.CreateMessage(sessionID, role, text)
				}
				for _, toolName := range extractToolNames(line) {
					_, _ = s.store.CreateMessage(sessionID, "activity", toolName)
				}
				turnsSinceLastContinue++
				count, _ := s.store.IncrementTurnCount(sessionID)
				if count >= threshold && !autoInterrupting {
					autoInterrupting = true
					log.Printf("session %s hit auto-continue threshold (%d/%d), interrupting", sessionID, count, sess.MaxTurns)
					_ = s.manager.Interrupt(sessionID)
				}
			}
			extractSessionFiles(line, sessionID, s.store)
		}

		managed.StreamNDJSON(proc.Stdout, broadcaster, onLine)

		stderrBytes, _ := io.ReadAll(proc.Stderr)
		<-proc.Done

		if proc.ExitCode != 0 && len(stderrBytes) > 0 {
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
			_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
			broadcaster.Send(errMsg)
		}

		// Natural exit (code 0) — Claude finished on its own, no auto-continue needed.
		// This check must come before autoInterrupting, because if Claude exits
		// naturally at the exact moment we set autoInterrupting, we should not
		// auto-continue — the work is done.
		if proc.ExitCode == 0 && !autoInterrupting {
			doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
			broadcaster.Send(doneMsg)
			break
		}

		// Process exited without our threshold SIGINT — manual interrupt or error
		if !autoInterrupting {
			doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
			broadcaster.Send(doneMsg)
			break
		}

		// Minimum progress guard: need at least 2 turns of work
		if turnsSinceLastContinue < 2 {
			log.Printf("session %s not making progress (%d turns), stopping auto-continue", sessionID, turnsSinceLastContinue)
			_, _ = s.store.CreateMessage(sessionID, "system", "Auto-continue stopped: not making progress")
			noProgressMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d,"reason":"no_progress"}`, continuationCount)
			broadcaster.Send(noProgressMsg)
			doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
			broadcaster.Send(doneMsg)
			break
		}

		continuationCount++

		if continuationCount > sess.MaxContinuations {
			log.Printf("session %s exhausted auto-continues (%d/%d)", sessionID, continuationCount, sess.MaxContinuations)
			exhaustedMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d}`, continuationCount)
			broadcaster.Send(exhaustedMsg)
			_, _ = s.store.CreateMessage(sessionID, "system",
				fmt.Sprintf("Auto-continue limit reached (%d/%d)", continuationCount, sess.MaxContinuations))
			doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
			broadcaster.Send(doneMsg)
			break
		}

		// Auto-continue
		continuingMsg := fmt.Sprintf(`{"type":"auto_continuing","continuation_count":%d,"max_continuations":%d}`,
			continuationCount, sess.MaxContinuations)
		broadcaster.Send(continuingMsg)
		_, _ = s.store.CreateMessage(sessionID, "system",
			fmt.Sprintf("Auto-continuing (%d/%d)...", continuationCount, sess.MaxContinuations))

		currentMessage = "You were interrupted due to turn limits. Continue where you left off."
	}
}()
```

- [ ] **Step 2: Remove old arg-building and persistence code that was replaced**

Remove the old `if !sess.Initialized` block (lines 127-129), the old `CreateMessage` and `SetSessionStatus` calls before the goroutine (lines 131-132), since these are now handled inside the loop. Move the user message persistence and status-setting to before the goroutine starts:

```go
_, _ = s.store.CreateMessage(sessionID, "user", req.Message)
_ = s.store.SetSessionStatus(sessionID, "running")
```

These should remain before the goroutine — they only happen once per user-initiated message.

Also move the initial `Spawn` and `SetInitialized` into the loop (they're handled in the new goroutine code above). The `handleSendMessage` function body before the goroutine should be simplified to:
1. Parse request, validate session
2. Do the first spawn to verify it works (or move spawn into the goroutine — see step 1 code)
3. Persist user message, set status to running
4. Launch goroutine with the loop

**Important:** The first spawn should happen inside the goroutine (as shown in step 1) since subsequent spawns also happen there. The HTTP handler returns `{"status":"started"}` immediately. The only check needed before starting the goroutine is that no process is already running — use `s.manager.IsRunning(sessionID)` check.

Update `handleSendMessage` to:

```go
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
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
		http.Error(w, "session already has a running process", http.StatusConflict)
		return
	}

	_, _ = s.store.CreateMessage(sessionID, "user", req.Message)
	_ = s.store.SetSessionStatus(sessionID, "running")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	// Auto-continue loop goroutine (from step 1)
	go func() { /* ... the loop from step 1 ... */ }()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd server && go build ./...`
Expected: No errors

- [ ] **Step 4: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat: implement auto-continue loop in handleSendMessage"
```

---

### Task 5: Add auto-continue integration test

**Files:**
- Modify: `server/api/managed_sessions_test.go`

- [ ] **Step 1: Write test for auto-continue trigger**

This test needs to simulate Claude output that triggers the threshold. Since `Spawn` starts a real process, the test should mock it. Check existing test patterns in `managed_sessions_test.go` for how tests handle process spawning.

If the tests use a real `Manager`, write a test that:
1. Creates a session with `max_turns=5`, `auto_continue_threshold=0.8` (threshold at turn 4)
2. Sends a message
3. Verifies that when turn count hits 4, the auto-continue SSE event is broadcast

If direct mocking isn't practical, write a unit test for the threshold calculation:

```go
func TestAutoContinueThresholdCalculation(t *testing.T) {
	// threshold = floor(0.8 * 50) = 40
	threshold := int(0.8 * float64(50))
	if threshold != 40 {
		t.Errorf("expected 40, got %d", threshold)
	}

	// threshold = floor(0.8 * 5) = 4
	threshold = int(0.8 * float64(5))
	if threshold != 4 {
		t.Errorf("expected 4, got %d", threshold)
	}

	// Edge: threshold = floor(0.8 * 1) = 0 — would trigger immediately, but minimum progress guard prevents tight loop
	threshold = int(0.8 * float64(1))
	if threshold != 0 {
		t.Errorf("expected 0, got %d", threshold)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd server && go test ./api/ -v -run TestAutoContinue`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add server/api/managed_sessions_test.go
git commit -m "test: add auto-continue threshold calculation test"
```

---

### Task 6: Update web UI to handle auto-continue events

**Files:**
- Modify: `server/web/static/app.js` (SSE handler in `startSessionSSE`)

- [ ] **Step 1: Handle `auto_continuing` event**

In the `startSessionSSE` method's `onmessage` handler (around line 825, where `done` is handled), add handling for the new event types. Add this *before* the `done` check:

```javascript
if (data.type === 'auto_continuing') {
  this.chatMessages.push({
    id: 'auto-continue-' + Date.now(),
    role: 'system',
    content: `Auto-continuing (${data.continuation_count}/${data.max_continuations})...`,
    isAutoContinue: true
  });
  // Reset turn threshold tracker since turn count will reset
  this.lastTurnThreshold = 0;
  this.$nextTick(() => this.scrollToBottom(true));
  return;
}

if (data.type === 'auto_continue_exhausted') {
  this.chatMessages.push({
    id: 'auto-exhausted-' + Date.now(),
    role: 'system',
    content: data.reason === 'no_progress'
      ? 'Auto-continue stopped: not making progress. Send a message to continue.'
      : `Auto-continue limit reached (${data.continuation_count}). Send a message to continue.`,
    isAutoContinue: true
  });
  this.$nextTick(() => this.scrollToBottom(true));
  // Don't return — let the done handler below close the SSE
}
```

- [ ] **Step 2: Add CSS for auto-continue system messages**

In `server/web/static/app.js` or `index.html`, the system messages rendered in chat should have a distinct style. Check how existing system/error messages are rendered in `index.html` and add a similar template for `isAutoContinue` messages. Look for the chat message rendering template and add:

```html
<!-- Auto-continue system message -->
<template x-if="msg.isAutoContinue">
  <div class="chat-msg system-msg auto-continue-msg" style="text-align:center;opacity:0.7;font-style:italic;padding:8px 0;">
    <span x-text="msg.content"></span>
  </div>
</template>
```

- [ ] **Step 3: Update turn toast logic**

In the global SSE handler (around line 161-180), update the 100% threshold toast to account for auto-continue. When `turn_count` resets to 0, the existing code already resets `lastTurnThreshold`. No changes needed — the existing logic handles the reset correctly.

However, update the 100% toast message to be more accurate when auto-continue is enabled:

```javascript
if (pct >= 100 && this.lastTurnThreshold < 100) {
  this.toast(`Turn limit reached (${sess.turn_count}/${sess.max_turns}) — auto-continuing if enabled`, 8000, 'info');
  this.lastTurnThreshold = 100;
}
```

- [ ] **Step 4: Verify manually** (skip in automated tests — UI changes)

Open the web UI, create a managed session, and verify that auto-continue events render correctly. This is a manual verification step.

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: handle auto-continue SSE events in web UI"
```

---

### Task 7: Handle manual interrupt during auto-continue

**Files:**
- Modify: `server/api/managed_sessions.go` (the auto-continue loop)

The current implementation already handles this correctly: `autoInterrupting` is a goroutine-local variable. If a manual `POST /interrupt` fires, the process exits with SIGINT but `autoInterrupting` is `false`, so the loop breaks and sends `done`.

- [ ] **Step 1: Verify the logic by tracing the code path**

Read through the loop and confirm:
1. `autoInterrupting` starts as `false` each iteration
2. Only set to `true` by the `onLine` callback when threshold is hit
3. After `<-proc.Done`, if `!autoInterrupting` → break (manual interrupt or natural exit)
4. If `autoInterrupting` → auto-continue

This is already correct in the Task 4 implementation. No code changes needed.

- [ ] **Step 2: Commit** (skip if no changes)

---

### Task 8: Run full test suite and verify

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 2: Build the server**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles with no errors

- [ ] **Step 3: Commit any final fixes**

If any tests fail, fix and commit.

---

### Task 9: Update CLAUDE.md spec references

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add spec and plan references**

Add to the "Spec & Plan" section in `CLAUDE.md`:

```markdown
- SSE interrupt / auto-continue spec: `docs/superpowers/specs/2026-03-23-sse-interrupt-turns-management-design.md`
- SSE interrupt / auto-continue plan: `docs/superpowers/plans/2026-03-23-sse-interrupt-turns-management.md`
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add SSE interrupt spec and plan references to CLAUDE.md"
```
