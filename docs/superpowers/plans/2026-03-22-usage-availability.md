# Session Usage & Availability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real-time turn count tracking and threshold warnings to the web UI so users can see session usage and get alerted before hitting limits.

**Architecture:** Persist turn count to SQLite on each assistant message (replacing local variable), expose via existing SSE session payload, render in a sidebar usage panel with color-coded progress bars and typed toast notifications.

**Tech Stack:** Go (server/db), SQLite, Alpine.js, CSS

**Spec:** `docs/superpowers/specs/2026-03-22-usage-availability-design.md`

---

### Task 1: Add `turn_count` column and DB methods

**Files:**
- Modify: `server/db/db.go:97` (add migration)
- Modify: `server/db/sessions.go:11-46` (struct, columns, scan)
- Modify: `server/db/sessions.go:124-137` (ResumeSession reset)
- Test: `server/db/sessions_test.go`

- [ ] **Step 1: Write failing tests for IncrementTurnCount and ResetTurnCount**

Add to `server/db/sessions_test.go`:

```go
func TestTurnCount(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-turns", `["Bash"]`, 50, 5.0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Initial turn_count is 0
	if sess.TurnCount != 0 {
		t.Errorf("expected initial turn_count=0, got %d", sess.TurnCount)
	}

	// Increment returns new count
	count, err := store.IncrementTurnCount(sess.ID)
	if err != nil {
		t.Fatalf("first increment: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	count, err = store.IncrementTurnCount(sess.ID)
	if err != nil {
		t.Fatalf("second increment: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}

	// Verify persisted via GetSessionByID
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.TurnCount != 2 {
		t.Errorf("expected persisted turn_count=2, got %d", updated.TurnCount)
	}

	// Reset
	if err := store.ResetTurnCount(sess.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	reset, _ := store.GetSessionByID(sess.ID)
	if reset.TurnCount != 0 {
		t.Errorf("expected turn_count=0 after reset, got %d", reset.TurnCount)
	}
}

func TestResumeSessionResetsTurnCount(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.CreateManagedSession("/tmp/test-resume-turns", `["Bash"]`, 50, 5.0)

	// Increment some turns
	store.IncrementTurnCount(sess.ID)
	store.IncrementTurnCount(sess.ID)

	// Resume should reset turn_count
	err := store.ResumeSession(sess.ID, "new-claude-session-id")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumed, _ := store.GetSessionByID(sess.ID)
	if resumed.TurnCount != 0 {
		t.Errorf("expected turn_count=0 after resume, got %d", resumed.TurnCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./db/ -v -run TestTurnCount`
Expected: Compilation errors — `TurnCount` field and methods don't exist yet.

- [ ] **Step 3: Add migration to db.go**

In `server/db/db.go`, add this line to the `migrations` slice (after line 96, before the closing `}`):

```go
`ALTER TABLE sessions ADD COLUMN turn_count INTEGER NOT NULL DEFAULT 0`,
```

- [ ] **Step 4: Update Session struct and scan in sessions.go**

In `server/db/sessions.go`, add `TurnCount` field to Session struct (after line 26):

```go
TurnCount       int    `json:"turn_count"`
```

Update `sessionColumns` constant (line 29) to include `turn_count`:

```go
const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized, COALESCE(claude_session_id,''), turn_count`
```

Update `scanSession` function (line 34-38) to scan `TurnCount`:

```go
err := scanner.Scan(
    &sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
    &sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
    &sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
    &sess.ClaudeSessionID, &sess.TurnCount,
)
```

- [ ] **Step 5: Add IncrementTurnCount and ResetTurnCount methods**

Add to `server/db/sessions.go` (after `SetSessionStatus` method, around line 175):

```go
func (s *Store) IncrementTurnCount(id string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`UPDATE sessions SET turn_count = turn_count + 1 WHERE id = ? RETURNING turn_count`, id,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("increment turn count: %w", err)
	}
	return count, nil
}

func (s *Store) ResetTurnCount(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET turn_count = 0 WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 6: Update ResumeSession to reset turn_count**

In `server/db/sessions.go`, modify the `ResumeSession` method (line 130). Change the UPDATE statement to also reset turn_count:

```go
if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 1, status = 'idle', turn_count = 0 WHERE id = ?`, claudeSessionID, id); err != nil {
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd server && go test ./db/ -v -run TestTurnCount`
Expected: Both `TestTurnCount` and `TestResumeSessionResetsTurnCount` PASS.

- [ ] **Step 8: Run all DB tests to check for regressions**

Run: `cd server && go test ./db/ -v`
Expected: All tests PASS.

- [ ] **Step 9: Commit**

```bash
git add server/db/db.go server/db/sessions.go server/db/sessions_test.go
git commit -m "feat: add turn_count column and DB methods for usage tracking"
```

---

### Task 2: Wire turn count into managed session message handler

**Files:**
- Modify: `server/api/managed_sessions.go:136-161`

- [ ] **Step 1: Replace local turnCount with DB-backed tracking**

In `server/api/managed_sessions.go`, modify the goroutine in `handleSendMessage` (lines 135-184).

Replace `turnCount := 0` (line 136) with a call to reset:

```go
_ = s.store.ResetTurnCount(sessionID)
```

Replace the turn counting block (lines 157-161):

```go
				turnCount++
				if turnCount >= sess.MaxTurns {
					log.Printf("session %s hit turn limit (%d), interrupting", sessionID, sess.MaxTurns)
					_ = s.manager.Interrupt(sessionID)
				}
```

With:

```go
				count, _ := s.store.IncrementTurnCount(sessionID)
				if count >= sess.MaxTurns {
					log.Printf("session %s hit turn limit (%d), interrupting", sessionID, sess.MaxTurns)
					_ = s.manager.Interrupt(sessionID)
				}
```

- [ ] **Step 2: Build and verify compilation**

Run: `cd server && go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 3: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat: wire turn count to DB in managed session handler"
```

---

### Task 3: Add typed toast support to frontend

**Files:**
- Modify: `server/web/static/app.js:81-84,272-277`
- Modify: `server/web/static/style.css:334-354`
- Modify: `server/web/static/index.html:454-455`

- [ ] **Step 1: Add toastType state variable**

In `server/web/static/app.js`, after `toastTimer: null,` (line 84), add:

```javascript
    toastType: 'info',
```

- [ ] **Step 2: Update toast() method to accept type parameter**

In `server/web/static/app.js`, replace the `toast` method (lines 272-277):

```javascript
    toast(msg, duration = 4000, type = 'info') {
      this.toastMessage = msg;
      this.toastType = type;
      this.showToast = true;
      if (this.toastTimer) clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => { this.showToast = false; }, duration);
    },
```

- [ ] **Step 3: Update toast HTML element**

In `server/web/static/index.html`, replace line 455:

```html
  <div class="toast" :class="{ visible: showToast }" x-text="toastMessage"></div>
```

With:

```html
  <div class="toast" :class="{ visible: showToast, warning: toastType === 'warning', error: toastType === 'error' }" x-text="toastMessage"></div>
```

- [ ] **Step 4: Add warning and error toast CSS**

In `server/web/static/style.css`, after `.toast.visible` block (after line 354), add:

```css
.toast.warning {
  background: #f59e0b;
  color: #000;
  border-color: #d97706;
}
.toast.error {
  background: #ef4444;
  color: #fff;
  border-color: #dc2626;
}
```

- [ ] **Step 5: Verify visually**

Open `http://localhost:8080` in browser. Open browser console and run:
```javascript
document.querySelector('[x-data]').__x.$data.toast('Warning test', 4000, 'warning')
```
Then:
```javascript
document.querySelector('[x-data]').__x.$data.toast('Error test', 4000, 'error')
```
Expected: Yellow toast for warning, red toast for error.

- [ ] **Step 6: Commit**

```bash
git add server/web/static/app.js server/web/static/style.css server/web/static/index.html
git commit -m "feat: add typed toast notifications (info, warning, error)"
```

---

### Task 4: Add usage panel to sidebar

**Files:**
- Modify: `server/web/static/index.html:52-79` (sidebar section)
- Modify: `server/web/static/style.css` (new usage-panel styles)
- Modify: `server/web/static/app.js` (computed helpers + threshold tracking)

- [ ] **Step 1: Add computed helpers to app.js**

In `server/web/static/app.js`, add these after `toastType: 'info',` (from Task 3):

```javascript
    // Usage tracking
    lastTurnThreshold: 0,
    lastThresholdSessionId: null,
```

Add these computed/helper methods somewhere near the other computed properties (after the `pendingCountFor` method):

```javascript
    get selectedSession() {
      return this.sessions.find(s => s.id === this.selectedSessionId);
    },

    get turnPercent() {
      const sess = this.selectedSession;
      if (!sess || sess.mode !== 'managed' || !sess.max_turns) return 0;
      return Math.min(100, Math.round((sess.turn_count / sess.max_turns) * 100));
    },

    turnBarColor() {
      const pct = this.turnPercent;
      if (pct >= 90) return '#ef4444';
      if (pct >= 80) return '#f59e0b';
      return 'var(--accent)';
    },
```

- [ ] **Step 2: Add threshold check logic to SSE update handler**

In `server/web/static/app.js`, inside the SSE `update` event handler (around line 142-164), after `this.prompts = data.prompts || [];` (line 147), add threshold checking:

```javascript
          // Check turn count thresholds for toast warnings
          if (this.selectedSessionId) {
            const sess = (data.sessions || []).find(s => s.id === this.selectedSessionId);
            if (sess && sess.mode === 'managed' && sess.max_turns > 0) {
              const pct = (sess.turn_count / sess.max_turns) * 100;
              // Reset threshold tracker on session change or turn reset
              if (this.lastThresholdSessionId !== sess.id) {
                this.lastTurnThreshold = 0;
                this.lastThresholdSessionId = sess.id;
              }
              if (sess.turn_count === 0 && this.lastTurnThreshold > 0) {
                this.lastTurnThreshold = 0;
              }
              // Fire toasts at threshold crossings
              if (pct >= 100 && this.lastTurnThreshold < 100) {
                this.toast(`Session interrupted \u2014 turn limit reached (${sess.turn_count}/${sess.max_turns})`, 8000, 'error');
                this.lastTurnThreshold = 100;
              } else if (pct >= 90 && this.lastTurnThreshold < 90) {
                this.toast(`Turn limit critical (${sess.turn_count}/${sess.max_turns}) \u2014 session will be interrupted soon`, 6000, 'error');
                this.lastTurnThreshold = 90;
              } else if (pct >= 80 && this.lastTurnThreshold < 80) {
                this.toast(`Turn limit warning (${sess.turn_count}/${sess.max_turns}) \u2014 approaching session limit`, 6000, 'warning');
                this.lastTurnThreshold = 80;
              }
            }
          }
```

- [ ] **Step 3: Add usage panel HTML to sidebar**

In `server/web/static/index.html`, after the closing `</div>` of `.session-list` (line 78) and before the closing `</div>` of `.sidebar` (line 79), add:

```html
        <div class="usage-panel" x-show="selectedSession && selectedSession.mode === 'managed'" x-cloak>
          <div style="display:flex; justify-content:space-between; margin-bottom:6px;">
            <span style="color:var(--text-muted); font-size:12px;">Turns</span>
            <span style="font-size:12px; font-weight:600;"
                  :style="{ color: turnPercent >= 90 ? '#ef4444' : turnPercent >= 80 ? '#f59e0b' : 'var(--text)' }"
                  x-text="(selectedSession.turn_count || 0) + ' / ' + selectedSession.max_turns"></span>
          </div>
          <div class="usage-bar">
            <div class="usage-bar-fill" :style="{ width: turnPercent + '%', background: turnBarColor() }"></div>
          </div>
        </div>
```

- [ ] **Step 4: Add usage panel CSS**

In `server/web/static/style.css`, add after the `.session-item` styles (find a logical place near the sidebar styles):

```css
/* Usage panel */
.usage-panel {
  padding: 12px 12px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.usage-bar {
  background: var(--bg-secondary);
  border-radius: 4px;
  height: 6px;
  overflow: hidden;
}
.usage-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.3s ease, background 0.3s ease;
}
```

- [ ] **Step 5: Verify visually**

Open `http://localhost:8080`, select a managed session. The usage panel should appear at the bottom of the left sidebar showing "0 / 50" with an empty progress bar. Send a message and watch the turn count update via SSE.

- [ ] **Step 6: Commit**

```bash
git add server/web/static/app.js server/web/static/style.css server/web/static/index.html
git commit -m "feat: add usage panel with turn count and threshold toasts"
```

---

### Task 5: Final verification and cleanup

- [ ] **Step 1: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS.

- [ ] **Step 2: Build server**

Run: `cd server && go build -o claude-controller .`
Expected: Clean build.

- [ ] **Step 3: Manual end-to-end test**

1. Start server: `cd server && go run .`
2. Open `http://localhost:8080`, log in
3. Create a managed session with `max_turns: 5` (for quick testing)
4. Send a message, watch turn count increment in sidebar panel
5. Verify: bar turns yellow at 80% (turn 4/5), red at 90%+
6. Verify: toast fires at 80% threshold (yellow) and 90%+ (red)
7. Verify: session gets interrupted at turn 5/5

- [ ] **Step 4: Commit any fixes**

If any issues found in manual testing, fix and commit.
