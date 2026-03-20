# `/resume` Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/resume` command to the web UI that reads Claude Code's native session index and lets users resume a previous CLI session in their current managed session.

**Architecture:** New `claude_session_id` column on sessions table decouples the managed session's stable ID from the Claude Code session being resumed. Two new API endpoints (GET resumable list, POST resume) read `~/.claude/projects/<encoded-cwd>/sessions-index.json` and switch the active CLI session. Web UI intercepts `/resume` in chat input and shows a picker overlay.

**Tech Stack:** Go (server), SQLite (DB), Alpine.js (web UI)

**Spec:** `docs/superpowers/specs/2026-03-19-resume-command-design.md`

---

### Task 1: Add `claude_session_id` DB column and session field

**Files:**
- Modify: `server/db/db.go:87` (add migration)
- Modify: `server/db/sessions.go:11-26` (add field to struct)
- Modify: `server/db/sessions.go:28` (add to sessionColumns)
- Modify: `server/db/sessions.go:30-44` (update scanSession)

- [ ] **Step 1: Add migration in `db.go`**

Add to the `migrations` slice at line 87 (before the closing `}`):

```go
`ALTER TABLE sessions ADD COLUMN claude_session_id TEXT`,
```

- [ ] **Step 2: Add field to `Session` struct in `sessions.go`**

Add after line 25 (`Initialized bool`):

```go
ClaudeSessionID string `json:"claude_session_id,omitempty"`
```

- [ ] **Step 3: Update `sessionColumns` constant**

Change line 28 to add `COALESCE(claude_session_id,'')` at the end:

```go
const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized, COALESCE(claude_session_id,'')`
```

- [ ] **Step 4: Update `scanSession` to scan the new field**

Add `&sess.ClaudeSessionID` at the end of the `Scan` call (after `&initialized`):

```go
err := scanner.Scan(
    &sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
    &sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
    &sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
    &sess.ClaudeSessionID,
)
```

- [ ] **Step 5: Add `ResumeSession` transactional method to `sessions.go`**

This wraps the three DB operations (set claude_session_id + initialized, delete messages, set status) in a single transaction to prevent inconsistent state if one fails mid-way:

```go
func (s *Store) ResumeSession(id, claudeSessionID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin resume transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 1, status = 'idle' WHERE id = ?`, claudeSessionID, id); err != nil {
		return fmt.Errorf("set claude_session_id: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return tx.Commit()
}
```

- [ ] **Step 6: Build and verify**

Run: `cd server && go build ./...`
Expected: compiles with no errors

- [ ] **Step 7: Commit**

```bash
git add server/db/db.go server/db/sessions.go
git commit -m "feat: add claude_session_id column and DB helpers for /resume"
```

---

### Task 2: Update `handleSendMessage` to use `claude_session_id`

**Files:**
- Modify: `server/api/managed_sessions.go:87-91`

- [ ] **Step 1: Update the session ID used for `--resume`/`--session-id`**

Replace lines 87-91 in `managed_sessions.go`:

```go
// Before:
if sess.Initialized {
    args = append(args, "--resume", sessionID)
} else {
    args = append(args, "--session-id", sessionID)
}
```

With:

```go
// Use claude_session_id if set (resumed session), otherwise use managed session's own ID
resumeID := sessionID
if sess.ClaudeSessionID != "" {
    resumeID = sess.ClaudeSessionID
}
if sess.Initialized {
    args = append(args, "--resume", resumeID)
} else {
    args = append(args, "--session-id", resumeID)
}
```

- [ ] **Step 2: Build and verify**

Run: `cd server && go build ./...`
Expected: compiles with no errors

- [ ] **Step 3: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "feat: use claude_session_id for --resume when set"
```

---

### Task 3: Add `GET /api/sessions/{id}/resumable` endpoint

**Files:**
- Create: `server/api/resume.go`
- Modify: `server/api/router.go:53` (add route)

- [ ] **Step 1: Create `server/api/resume.go` with path encoding and handler**

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeProjectDir encodes a CWD to match Claude Code's project directory naming.
// Replaces /, _, and . with -.
func claudeProjectDir(cwd string) string {
	r := strings.NewReplacer("/", "-", "_", "-", ".", "-")
	return r.Replace(cwd)
}

func sessionsIndexPath(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "projects", claudeProjectDir(cwd), "sessions-index.json"), nil
}

type sessionsIndex struct {
	Version int             `json:"version"`
	Entries []sessionEntry  `json:"entries"`
}

type sessionEntry struct {
	SessionID    string `json:"sessionId"`
	FirstPrompt  string `json:"firstPrompt"`
	Summary      string `json:"summary"`
	MessageCount int    `json:"messageCount"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"gitBranch"`
	IsSidechain  bool   `json:"isSidechain"`
}

type resumableSession struct {
	SessionID    string `json:"session_id"`
	Summary      string `json:"summary"`
	FirstPrompt  string `json:"first_prompt"`
	MessageCount int    `json:"message_count"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"git_branch"`
}

func (s *Server) handleResumableList(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

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

	indexPath, err := sessionsIndexPath(sess.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "no CLI sessions found for this project", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("read sessions index: %v", err), http.StatusInternalServerError)
		}
		return
	}

	var index sessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		http.Error(w, fmt.Sprintf("parse sessions index: %v", err), http.StatusInternalServerError)
		return
	}

	var results []resumableSession
	for _, e := range index.Entries {
		if e.IsSidechain {
			continue
		}
		// Filter out the currently active claude session
		if sess.ClaudeSessionID != "" && e.SessionID == sess.ClaudeSessionID {
			continue
		}
		prompt := e.FirstPrompt
		if len(prompt) > 80 {
			prompt = prompt[:80] + "…"
		}
		results = append(results, resumableSession{
			SessionID:    e.SessionID,
			Summary:      e.Summary,
			FirstPrompt:  prompt,
			MessageCount: e.MessageCount,
			Created:      e.Created,
			Modified:     e.Modified,
			GitBranch:    e.GitBranch,
		})
	}

	// Sort by modified descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Modified > results[j].Modified
	})

	// Limit to 20
	if len(results) > 20 {
		results = results[:20]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"sessions": results})
}
```

- [ ] **Step 2: Register route in `router.go`**

Add after line 53 (`apiMux.HandleFunc("GET /api/sessions/{id}/messages", s.handleListMessages)`):

```go
apiMux.HandleFunc("GET /api/sessions/{id}/resumable", s.handleResumableList)
```

- [ ] **Step 3: Build and verify**

Run: `cd server && go build ./...`
Expected: compiles with no errors

- [ ] **Step 4: Commit**

```bash
git add server/api/resume.go server/api/router.go
git commit -m "feat: add GET /api/sessions/{id}/resumable endpoint"
```

---

### Task 4: Add `POST /api/sessions/{id}/resume` endpoint

**Files:**
- Modify: `server/api/resume.go` (add handler)
- Modify: `server/api/router.go` (add route)

- [ ] **Step 1: Add `handleResumeSession` to `resume.go`**

Append to `server/api/resume.go`:

```go
func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
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

	// Validate that session_id exists in the resumable list
	indexPath, err := sessionsIndexPath(sess.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		http.Error(w, "cannot read sessions index", http.StatusInternalServerError)
		return
	}
	var index sessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		http.Error(w, "cannot parse sessions index", http.StatusInternalServerError)
		return
	}
	found := false
	for _, e := range index.Entries {
		if e.SessionID == req.SessionID && !e.IsSidechain {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "session_id not found in resumable sessions", http.StatusBadRequest)
		return
	}

	// Teardown any running process
	if s.manager.IsRunning(sessionID) {
		if err := s.manager.Teardown(sessionID, 5*time.Second); err != nil {
			http.Error(w, fmt.Sprintf("failed to stop running process: %v", err), http.StatusConflict)
			return
		}
	}

	// Atomically: set claude_session_id, initialized, delete old messages, set idle
	if err := s.store.ResumeSession(sessionID, req.SessionID); err != nil {
		http.Error(w, fmt.Sprintf("failed to resume session: %v", err), http.StatusInternalServerError)
		return
	}

	// Re-fetch and return updated session
	updated, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}
```

- [ ] **Step 2: Add `time` to imports in `resume.go`**

Add `"time"` to the import block in `resume.go`.

- [ ] **Step 3: Register route in `router.go`**

Add after the resumable route:

```go
apiMux.HandleFunc("POST /api/sessions/{id}/resume", s.handleResumeSession)
```

- [ ] **Step 4: Build and verify**

Run: `cd server && go build ./...`
Expected: compiles with no errors

- [ ] **Step 5: Commit**

```bash
git add server/api/resume.go server/api/router.go
git commit -m "feat: add POST /api/sessions/{id}/resume endpoint"
```

---

### Task 5: Add `/resume` command interception and picker in web UI

**Files:**
- Modify: `server/web/static/app.js` (add state, interception, picker logic)
- Modify: `server/web/static/index.html` (add picker overlay, update placeholder)

- [ ] **Step 1: Add resume state variables in `app.js`**

Add after the `browseLoading: false,` line (around line 37):

```js
// Resume picker state
showResumePicker: false,
resumableSessions: [],
resumeLoading: false,
```

- [ ] **Step 2: Update `handleInput()` to intercept `/resume`**

Replace the existing `handleInput()` method:

```js
async handleInput() {
  if (!this.selectedSessionId || !this.inputText.trim()) return;
  const sess = this.currentSession;

  // Intercept /resume command for managed sessions
  if (sess && sess.mode === 'managed' && this.inputText.trim().toLowerCase() === '/resume') {
    this.inputText = '';
    await this.openResumePicker();
    return;
  }

  if (sess && sess.mode === 'managed') {
    await this.sendManagedMessage();
  } else {
    await this.sendInstruction();
  }
},
```

- [ ] **Step 3: Add resume picker methods in `app.js`**

Add after the `interruptSession()` method:

```js
async openResumePicker() {
  this.resumeLoading = true;
  this.showResumePicker = true;
  this.resumableSessions = [];
  try {
    const res = await fetch(`/api/sessions/${this.selectedSessionId}/resumable`, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (res.status === 404) {
      this.toast('No previous CLI sessions found for this project');
      this.showResumePicker = false;
      this.resumeLoading = false;
      return;
    }
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    this.resumableSessions = data.sessions || [];
  } catch (e) {
    this.toast('Error: ' + e.message);
    this.showResumePicker = false;
  }
  this.resumeLoading = false;
},

async resumeSession(claudeSessionId, summary) {
  try {
    const res = await fetch(`/api/sessions/${this.selectedSessionId}/resume`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: claudeSessionId })
    });
    if (!res.ok) throw new Error(await res.text());
    this.showResumePicker = false;
    this.chatMessages = [];
    this.toast('Resumed: ' + (summary || 'session'));
  } catch (e) {
    this.toast('Error: ' + e.message);
  }
},
```

- [ ] **Step 4: Update the input placeholder in `index.html`**

Replace line 142:

```html
:placeholder="currentSession?.mode === 'managed' ? 'Send message...' : 'Send instruction (delivered on next stop)...'"
```

With:

```html
:placeholder="currentSession?.mode === 'managed' ? 'Send a message... (type /resume to continue a previous session)' : 'Send instruction (delivered on next stop)...'"
```

- [ ] **Step 5: Add resume picker modal markup in `index.html`**

Add after the New Session Modal closing `</div>` (after line 207), before the Toast div:

```html
<!-- Resume Picker Modal -->
<div x-show="showResumePicker" x-cloak
     style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:100; display:flex; align-items:center; justify-content:center;"
     @click.self="showResumePicker = false">
  <div style="background:var(--bg); border-radius:12px; padding:24px; width:500px; max-width:90vw; border:1px solid var(--border); display:flex; flex-direction:column; max-height:70vh;">
    <h3 style="margin-top:0; margin-bottom:12px;">Resume a Session</h3>

    <template x-if="resumeLoading">
      <div style="padding:2rem; text-align:center; color:var(--text-muted); font-size:13px;">Loading sessions...</div>
    </template>

    <template x-if="!resumeLoading && resumableSessions.length === 0">
      <div style="padding:2rem; text-align:center; color:var(--text-muted); font-size:13px;">No sessions to resume</div>
    </template>

    <div style="overflow-y:auto; flex:1; min-height:0;">
      <template x-for="rs in resumableSessions" :key="rs.session_id">
        <div @click="resumeSession(rs.session_id, rs.summary)"
             style="padding:10px 12px; border-bottom:1px solid var(--border); cursor:pointer; transition:background 0.15s;"
             onmouseenter="this.style.background='var(--hover)'"
             onmouseleave="this.style.background='transparent'">
          <div style="font-weight:600; font-size:14px; margin-bottom:2px;" x-text="rs.summary || 'Untitled session'"></div>
          <div style="font-size:12px; color:var(--text-muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="rs.first_prompt"></div>
          <div style="display:flex; gap:8px; margin-top:4px; font-size:11px; color:var(--text-muted);">
            <span x-show="rs.git_branch" x-text="rs.git_branch" style="background:var(--input-bg); padding:1px 5px; border-radius:3px;"></span>
            <span x-text="rs.message_count + ' messages'"></span>
            <span x-text="timeAgo(rs.modified)"></span>
          </div>
        </div>
      </template>
    </div>

    <div style="display:flex; gap:8px; justify-content:flex-end; margin-top:12px; flex-shrink:0;">
      <button class="btn btn-sm" @click="showResumePicker = false">Cancel</button>
    </div>
  </div>
</div>
```

- [ ] **Step 6: Build server and verify**

Run: `cd server && go build ./...`
Expected: compiles (JS/HTML changes don't need compilation but the embedded static files need the server to rebuild if using `embed`)

- [ ] **Step 7: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: add /resume command with picker overlay in web UI"
```

---

### Task 6: Manual integration test

**Files:** None (testing only)

- [ ] **Step 1: Start the server**

Run: `cd server && go run .`
Expected: Server starts on :8080

- [ ] **Step 2: Open web UI, create a managed session pointing to a project that has CLI session history**

Navigate to `http://localhost:8080`, authenticate, click "+ New Session", browse to a directory that has prior Claude Code sessions (e.g., a project you've used `claude` CLI in before).

- [ ] **Step 3: Type `/resume` in the chat input**

Expected: A modal picker appears with a list of recent CLI sessions showing summary, first prompt, branch, message count, and relative time.

- [ ] **Step 4: Click on a session to resume it**

Expected: Modal closes, chat clears, toast shows "Resumed: <summary>", input is ready for next message.

- [ ] **Step 5: Send a message in the resumed session**

Expected: The message is sent with `--resume <chosen-uuid>` and Claude continues the previous conversation context.
