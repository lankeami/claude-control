# Session Activity State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persisted `activity_state` field to sessions so the web UI can show at a glance whether Claude is working, waiting for input, or idle.

**Architecture:** New `activity_state` column in SQLite sessions table, updated by managed session handlers at process start/exit. Frontend reads it via the global events endpoint and maps to existing status-dot CSS classes with a new pulse animation.

**Tech Stack:** Go (server), SQLite (db), Alpine.js + CSS (frontend)

**Spec:** `docs/superpowers/specs/2026-03-23-session-activity-state-design.md`

---

### Task 1: Database — Add `activity_state` column and `UpdateActivityState` method

**Files:**
- Modify: `server/db/db.go:97-100` (add migration)
- Modify: `server/db/sessions.go:11-30` (add field to struct)
- Modify: `server/db/sessions.go:32` (update `sessionColumns`)
- Modify: `server/db/sessions.go:34-49` (update `scanSession`)
- Modify: `server/db/sessions.go:175-178` (add new method after `SetSessionStatus`)
- Test: `server/db/sessions_test.go`

- [ ] **Step 1: Write the failing test for `UpdateActivityState`**

Add to `server/db/sessions_test.go`:

```go
func TestUpdateActivityState(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-activity", `["Bash"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	// Default activity_state is "idle"
	if sess.ActivityState != "idle" {
		t.Errorf("expected initial activity_state='idle', got %q", sess.ActivityState)
	}

	// Update to working
	if err := store.UpdateActivityState(sess.ID, "working"); err != nil {
		t.Fatalf("update to working: %v", err)
	}
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.ActivityState != "working" {
		t.Errorf("expected activity_state='working', got %q", updated.ActivityState)
	}

	// Update to waiting
	if err := store.UpdateActivityState(sess.ID, "waiting"); err != nil {
		t.Fatalf("update to waiting: %v", err)
	}
	updated, _ = store.GetSessionByID(sess.ID)
	if updated.ActivityState != "waiting" {
		t.Errorf("expected activity_state='waiting', got %q", updated.ActivityState)
	}

	// Update to idle
	if err := store.UpdateActivityState(sess.ID, "idle"); err != nil {
		t.Fatalf("update to idle: %v", err)
	}
	updated, _ = store.GetSessionByID(sess.ID)
	if updated.ActivityState != "idle" {
		t.Errorf("expected activity_state='idle', got %q", updated.ActivityState)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestUpdateActivityState`
Expected: Compilation error — `ActivityState` field and `UpdateActivityState` method don't exist.

- [ ] **Step 3: Add migration to `db.go`**

In `server/db/db.go`, add to the `migrations` slice (after line 99, before the closing `]`):

```go
`ALTER TABLE sessions ADD COLUMN activity_state TEXT NOT NULL DEFAULT 'idle'`,
```

- [ ] **Step 4: Add `ActivityState` field to Session struct**

In `server/db/sessions.go`, add after `MaxContinuations` field (line 29):

```go
ActivityState string `json:"activity_state"`
```

- [ ] **Step 5: Update `sessionColumns` constant**

In `server/db/sessions.go` line 32, append `, COALESCE(activity_state,'idle')` to the end of the string.

- [ ] **Step 6: Update `scanSession` to read the new column**

In `server/db/sessions.go`, add `&sess.ActivityState` as the last argument to `scanner.Scan()` (after `&sess.MaxContinuations` on line 41).

- [ ] **Step 7: Add `UpdateActivityState` method**

In `server/db/sessions.go`, add after `SetSessionStatus` (after line 178):

```go
func (s *Store) UpdateActivityState(id, state string) error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = ? WHERE id = ?", state, id)
	return err
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd server && go test ./db/ -v -run TestUpdateActivityState`
Expected: PASS

- [ ] **Step 9: Run all DB tests to check for regressions**

Run: `cd server && go test ./db/ -v`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add server/db/db.go server/db/sessions.go server/db/sessions_test.go
git commit -m "feat(db): add activity_state column and UpdateActivityState method"
```

---

### Task 2: Add `ResetStaleActivityStates` method (TDD)

**Files:**
- Modify: `server/db/sessions.go` (add method after `UpdateActivityState`)
- Test: `server/db/sessions_test.go`

- [ ] **Step 1: Write the failing test**

Add to `server/db/sessions_test.go`:

```go
func TestResetStaleActivityStates(t *testing.T) {
	store := newTestStore(t)

	s1, _ := store.CreateManagedSession("/tmp/stale1", `["Bash"]`, 50, 5.0)
	s2, _ := store.CreateManagedSession("/tmp/stale2", `["Bash"]`, 50, 5.0)

	// Set s1 to working, s2 to waiting
	store.UpdateActivityState(s1.ID, "working")
	store.UpdateActivityState(s2.ID, "waiting")

	// Reset stale states
	if err := store.ResetStaleActivityStates(); err != nil {
		t.Fatalf("reset stale: %v", err)
	}

	got1, _ := store.GetSessionByID(s1.ID)
	if got1.ActivityState != "idle" {
		t.Errorf("s1: expected 'idle', got %q", got1.ActivityState)
	}

	got2, _ := store.GetSessionByID(s2.ID)
	if got2.ActivityState != "waiting" {
		t.Errorf("s2: expected 'waiting' (unchanged), got %q", got2.ActivityState)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestResetStaleActivityStates`
Expected: Compilation error — `ResetStaleActivityStates` method doesn't exist.

- [ ] **Step 3: Add `ResetStaleActivityStates` method**

In `server/db/sessions.go`, add after `UpdateActivityState`:

```go
func (s *Store) ResetStaleActivityStates() error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = 'idle' WHERE activity_state = 'working'")
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./db/ -v -run TestResetStaleActivityStates`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/sessions.go server/db/sessions_test.go
git commit -m "feat(db): add ResetStaleActivityStates method"
```

---

### Task 3: Server startup — reset stale activity states

**Files:**
- Modify: `server/main.go:51-54` (add reset call after store creation)

- [ ] **Step 1: Add reset call in `main.go`**

In `server/main.go`, add after `defer store.Close()` (line 55):

```go
if err := store.ResetStaleActivityStates(); err != nil {
	log.Printf("Warning: failed to reset stale activity states: %v", err)
}
```

- [ ] **Step 2: Verify build compiles**

Run: `cd server && go build -o /dev/null .`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add server/main.go
git commit -m "feat: reset stale working activity states on server startup"
```

---

### Task 4: Backend — wire activity state transitions in managed session handlers

**Files:**
- Modify: `server/api/managed_sessions.go:122` (replace `SetSessionStatus("running")` with `UpdateActivityState("working")`)
- Modify: `server/api/managed_sessions.go:128` (replace `SetSessionStatus("idle")` in defer)
- Modify: `server/api/managed_sessions.go:211-216` (set `waiting` on clean exit)
- Modify: `server/api/managed_sessions.go:219-223` (set `idle` on error exit)
- Modify: `server/api/managed_sessions.go:473` (replace `SetSessionStatus("running")` in shell handler)
- Modify: `server/api/managed_sessions.go:540` (replace `SetSessionStatus("idle")` in shell handler)

- [ ] **Step 1: Update `handleSendMessage` — set `working` on message send**

In `server/api/managed_sessions.go` line 122, replace:
```go
_ = s.store.SetSessionStatus(sessionID, "running")
```
with:
```go
_ = s.store.UpdateActivityState(sessionID, "working")
```

- [ ] **Step 2: Remove the defer that sets status in `handleSendMessage` goroutine**

In `server/api/managed_sessions.go`, remove the entire defer block (lines 127-129):
```go
defer func() {
	_ = s.store.SetSessionStatus(sessionID, "idle")
}()
```

**Why:** The defer unconditionally sets `idle`, which would clobber the `waiting` state set by clean-exit paths (Steps 3, 5, 6). The explicit state transitions before each `break` handle all exit paths. The `ResetStaleActivityStates` on server startup handles the crash-recovery edge case.

- [ ] **Step 3: Set `idle` on client disconnect**

In `server/api/managed_sessions.go`, inside the `case <-ctx.Done():` block (around line 143), add before the `return`:
```go
_ = s.store.UpdateActivityState(sessionID, "idle")
```

- [ ] **Step 4: Set `idle` on spawn error**

In `server/api/managed_sessions.go`, inside the spawn error block (around line 163), add before the `break`:
```go
_ = s.store.UpdateActivityState(sessionID, "idle")
```

- [ ] **Step 5: Set `waiting` on clean exit (code 0, no auto-interrupt)**

In `server/api/managed_sessions.go`, just before the `break` on line 215 (inside the `if proc.ExitCode == 0 && !autoInterrupting` block), add:
```go
_ = s.store.UpdateActivityState(sessionID, "waiting")
```

- [ ] **Step 6: Set `idle` on non-zero exit without auto-interrupt**

In `server/api/managed_sessions.go`, just before the `break` on line 222 (inside the `if !autoInterrupting` block), add:
```go
_ = s.store.UpdateActivityState(sessionID, "idle")
```

- [ ] **Step 7: Set `waiting` when auto-continue exhausted (no progress)**

In `server/api/managed_sessions.go`, just before the `break` on line 233 (inside `turnsSinceLastContinue < 2` block), add:
```go
_ = s.store.UpdateActivityState(sessionID, "waiting")
```

- [ ] **Step 8: Set `waiting` when auto-continue limit reached**

In `server/api/managed_sessions.go`, just before the `break` on line 246 (inside `continuationCount > sess.MaxContinuations` block), add:
```go
_ = s.store.UpdateActivityState(sessionID, "waiting")
```

- [ ] **Step 9: Update `handleShellExecute` — set `working` on shell start**

In `server/api/managed_sessions.go` line 473, replace:
```go
_ = s.store.SetSessionStatus(sessionID, "running")
```
with:
```go
_ = s.store.UpdateActivityState(sessionID, "working")
```

- [ ] **Step 10: Update shell exit — set state based on exit code**

In `server/api/managed_sessions.go` line 540, replace:
```go
_ = s.store.SetSessionStatus(sessionID, "idle")
```
with:
```go
if proc.ExitCode == 0 {
	_ = s.store.UpdateActivityState(sessionID, "waiting")
} else {
	_ = s.store.UpdateActivityState(sessionID, "idle")
}
```

- [ ] **Step 11: Verify build compiles**

Run: `cd server && go build -o /dev/null .`
Expected: Compiles without errors.

- [ ] **Step 12: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests pass.

- [ ] **Step 13: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat: wire activity_state transitions in managed session handlers"
```

---

### Task 5: Frontend — update `sessionStatus()` and CSS

**Files:**
- Modify: `server/web/static/app.js:408-416` (update `sessionStatus()`)
- Modify: `server/web/static/style.css:183-185` (add pulse animation)

- [ ] **Step 1: Update `sessionStatus()` in `app.js`**

In `server/web/static/app.js`, replace lines 408-416:

```javascript
sessionStatus(session) {
  if (session.mode === 'managed') {
    return session.status; // managed sessions have accurate status (idle/running)
  }
  const lastSeen = new Date(session.last_seen_at);
  const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
  if (lastSeen < fiveMinAgo) return 'idle';
  return session.status;
},
```

with:

```javascript
sessionStatus(session) {
  if (session.mode === 'managed') {
    const state = session.activity_state || 'idle';
    if (state === 'working') return 'active';
    if (state === 'waiting') return 'waiting';
    return 'idle';
  }
  const lastSeen = new Date(session.last_seen_at);
  const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
  if (lastSeen < fiveMinAgo) return 'idle';
  return session.status;
},
```

- [ ] **Step 2: Add pulse animation to CSS**

In `server/web/static/style.css`, replace lines 183-185:

```css
.status-dot.waiting { background: var(--green); }
.status-dot.active { background: var(--yellow); }
.status-dot.idle { background: var(--gray); }
```

with:

```css
.status-dot.waiting { background: var(--green); }
.status-dot.active {
  background: var(--yellow);
  animation: status-pulse 1.5s ease-in-out infinite;
}
.status-dot.idle { background: var(--gray); }

@keyframes status-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
```

- [ ] **Step 3: Verify build compiles**

Run: `cd server && go build -o /dev/null .`
Expected: Compiles (static files are embedded).

- [ ] **Step 4: Commit**

```bash
git add server/web/static/app.js server/web/static/style.css
git commit -m "feat(ui): show activity state indicators in session list"
```

---

### Task 6: Manual smoke test

- [ ] **Step 1: Start the server**

Run: `cd server && go run .`

- [ ] **Step 2: Create a managed session and send a message via the web UI**

Open the web UI, create a managed session, and send a message. Verify:
- The status dot turns yellow and pulses while Claude is working
- The status dot turns green when Claude finishes (exits code 0)
- The dot stays green until you send another message

- [ ] **Step 3: Verify idle state**

Create a new managed session without sending any messages. Verify the dot is gray (idle).
