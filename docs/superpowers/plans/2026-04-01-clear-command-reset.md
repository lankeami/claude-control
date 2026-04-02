# `/clear` Command Session Reset — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/clear` reset the managed session in-place so it starts a fresh Claude CLI conversation, persisting across page refresh.

**Architecture:** Add a `ClearSession` DB method that deletes messages and resets session state in a transaction. Add a `POST /api/sessions/{id}/clear` endpoint that calls it after tearing down any warm process. Update the frontend `/clear` handler to call the API instead of just clearing the array.

**Tech Stack:** Go (server), SQLite (DB), Alpine.js (frontend)

---

### Task 1: DB layer — `ClearSession` method

**Files:**
- Modify: `server/db/sessions.go` (after `ResumeSession` at line 150)
- Test: `server/db/sessions_test.go`

- [ ] **Step 1: Write the failing test**

Add to `server/db/sessions_test.go`:

```go
func TestClearSession(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-clear", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate some usage: add messages, set initialized, increment turns
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", "hi there")
	store.SetInitialized(sess.ID)
	store.IncrementTurnCount(sess.ID)
	store.IncrementTurnCount(sess.ID)
	store.UpdateActivityState(sess.ID, "waiting")

	oldSessionID := sess.ClaudeSessionID

	// Clear the session
	if err := store.ClearSession(sess.ID); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	// Verify messages deleted
	msgs, err := store.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs))
	}

	// Verify session state reset
	updated, err := store.GetSessionByID(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if updated.Initialized {
		t.Error("expected initialized=false after clear")
	}
	if updated.TurnCount != 0 {
		t.Errorf("expected turn_count=0, got %d", updated.TurnCount)
	}
	if updated.ActivityState != "idle" {
		t.Errorf("expected activity_state='idle', got %q", updated.ActivityState)
	}
	if updated.ClaudeSessionID == oldSessionID {
		t.Error("expected new claude_session_id after clear")
	}
	if updated.ClaudeSessionID == "" {
		t.Error("expected non-empty claude_session_id after clear")
	}

	// Verify settings preserved
	if updated.CWD != "/tmp/test-clear" {
		t.Errorf("expected cwd preserved, got %q", updated.CWD)
	}
	if updated.AllowedTools != `["Bash"]` {
		t.Errorf("expected allowed_tools preserved, got %q", updated.AllowedTools)
	}
	if updated.MaxTurns != 50 {
		t.Errorf("expected max_turns preserved, got %d", updated.MaxTurns)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestClearSession`
Expected: FAIL — `store.ClearSession` does not exist.

- [ ] **Step 3: Implement `ClearSession`**

Add to `server/db/sessions.go` after the `ResumeSession` method (line 150):

```go
func (s *Store) ClearSession(id string) error {
	newClaudeSessionID := uuid.New().String()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin clear transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 0, turn_count = 0, activity_state = 'idle' WHERE id = ?`, newClaudeSessionID, id); err != nil {
		return fmt.Errorf("reset session state: %w", err)
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./db/ -v -run TestClearSession`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/sessions.go server/db/sessions_test.go
git commit -m "feat: add ClearSession DB method for /clear command"
```

---

### Task 2: API handler — `POST /api/sessions/{id}/clear`

**Files:**
- Modify: `server/api/managed_sessions.go` (add `handleClearSession` method)
- Modify: `server/api/router.go:60` (add route)
- Test: `server/api/managed_sessions_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `server/api/managed_sessions_test.go`:

```go
func TestClearSessionAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-clear-api", `["Read"]`, 50, 5.0, 0)
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", "hi")
	store.SetInitialized(sess.ID)

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	// Verify messages cleared
	msgs, _ := store.ListMessages(sess.ID)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}

	// Verify session reset
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.Initialized {
		t.Error("expected initialized=false")
	}
	if updated.TurnCount != 0 {
		t.Errorf("expected turn_count=0, got %d", updated.TurnCount)
	}
}

func TestClearSessionAPI_RejectsWorking(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-clear-working", `["Read"]`, 50, 5.0, 0)
	store.UpdateActivityState(sess.ID, "working")

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 409 {
		t.Fatalf("status=%d, want 409 for working session", resp.StatusCode)
	}
}

func TestClearSessionAPI_NotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./api/ -v -run TestClearSession`
Expected: FAIL — handler does not exist, route not registered.

- [ ] **Step 3: Add the route**

In `server/api/router.go`, add after line 60 (`POST /api/sessions/{id}/resume`):

```go
	apiMux.HandleFunc("POST /api/sessions/{id}/clear", s.handleClearSession)
```

- [ ] **Step 4: Implement the handler**

Add to `server/api/managed_sessions.go`:

```go
func (s *Server) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}
	if sess.ActivityState == "working" {
		http.Error(w, "cannot clear while session is working", http.StatusConflict)
		return
	}

	// Tear down any warm process so the old CLI session doesn't linger
	s.manager.Teardown(sessionID, 5*time.Second)

	if err := s.store.ClearSession(sessionID); err != nil {
		log.Printf("clear session %s: %v", sessionID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestClearSession`
Expected: All 3 tests PASS.

- [ ] **Step 6: Run all server tests to check for regressions**

Run: `cd server && go test ./... -v`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add server/api/managed_sessions.go server/api/router.go server/api/managed_sessions_test.go
git commit -m "feat: add POST /api/sessions/{id}/clear endpoint"
```

---

### Task 3: Frontend — wire `/clear` to the API

**Files:**
- Modify: `server/web/static/app.js` (lines 874-876, the `/clear` case in `executeSlashCommand`)

- [ ] **Step 1: Update the `/clear` command handler**

In `server/web/static/app.js`, replace the existing `/clear` case (lines 874-876):

```javascript
        case '/clear':
          this.chatMessages = [];
          break;
```

With:

```javascript
        case '/clear': {
          try {
            const resp = await fetch(`/api/sessions/${this.selectedSessionId}/clear`, {
              method: 'POST',
              headers: { 'Authorization': 'Bearer ' + this.apiKey },
            });
            if (!resp.ok) {
              const errText = await resp.text();
              this.chatMessages.push({ role: 'system', content: `Clear failed: ${errText}`, msg_type: 'text', timestamp: new Date().toISOString() });
            } else {
              this.chatMessages = [];
            }
          } catch (e) {
            this.chatMessages.push({ role: 'system', content: `Clear failed: ${e.message}`, msg_type: 'text', timestamp: new Date().toISOString() });
          }
          break;
        }
```

- [ ] **Step 2: Update the `/clear` command description**

In `server/api/commands.go`, line 22, update the description:

```go
	{Name: "/clear", Description: "Clear chat and start fresh conversation", Source: "builtin"},
```

- [ ] **Step 3: Manual test**

1. Start the server: `cd server && go run .`
2. Open the web UI, select a managed session with some messages
3. Type `/clear` — chat should clear
4. Refresh the page — chat should remain empty (no old messages)
5. Send a new message — should start a fresh Claude CLI session

- [ ] **Step 4: Commit**

```bash
git add server/web/static/app.js server/api/commands.go
git commit -m "feat: wire /clear to session reset API for persistent clear"
```

---

### Task 4: Create feature branch and draft PR

- [ ] **Step 1: Create feature branch from current state**

```bash
git checkout -b feat/clear-session-reset main
```

Note: If all commits were already made on the current branch, cherry-pick or rebase them onto the feature branch instead.

- [ ] **Step 2: Push and create draft PR**

```bash
git push -u origin feat/clear-session-reset
gh pr create --draft --title "feat: /clear resets session for fresh conversation" --body "$(cat <<'EOF'
## Summary

Fixes #78. `/clear` now resets the managed session in-place:

- Deletes all messages from the database
- Generates a new Claude CLI session ID (next turn uses `--session-id` instead of `--resume`)
- Resets `initialized`, `turn_count`, and `activity_state`
- Tears down any warm CLI process
- Settings (cwd, allowed_tools, max_turns, budget) are preserved

Previously, `/clear` only cleared the frontend array — messages reloaded on page refresh and Claude retained full conversation context.

## Test plan

- [ ] `go test ./db/ -run TestClearSession` passes
- [ ] `go test ./api/ -run TestClearSession` passes (3 sub-tests: success, rejects working, not found)
- [ ] `go test ./...` — no regressions
- [ ] Manual: run `/clear`, refresh page — chat stays empty
- [ ] Manual: send message after `/clear` — new conversation (no prior context)
EOF
)"
```
