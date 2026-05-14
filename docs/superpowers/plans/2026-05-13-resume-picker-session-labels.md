# Resume Picker Session Labels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the user-defined managed session name in the `/resume` CLI session picker so sessions can be identified at a glance.

**Architecture:** When `handleResumableList` builds the resume picker list, it bulk-looks up managed session names by matching `sessions.claude_session_id` against the CLI session IDs returned from `sessions-index.json`. The matched name is returned as a `label` field on each item. The frontend uses `rs.label || rs.summary || rs.first_prompt || 'Untitled session'` as the display chain. No new DB tables or API endpoints are required.

**Tech Stack:** Go 1.22+, SQLite (modernc.org/sqlite), Alpine.js 3.x

**Spec:** `docs/superpowers/specs/2026-05-13-resume-picker-session-labels-design.md`

---

## File Map

| File | Change |
|------|--------|
| `server/db/sessions.go` | Add `GetManagedSessionNamesByCliIDs` method |
| `server/db/sessions_test.go` | Add `TestGetManagedSessionNamesByCliIDs` |
| `server/api/resume.go` | Add `Label` to `resumableSession`; bulk-lookup in `handleResumableList` |
| `server/web/static/index.html` | Update display chain; add hint text |

---

## Task 1: DB Method — `GetManagedSessionNamesByCliIDs`

**Files:**
- Modify: `server/db/sessions.go`
- Test: `server/db/sessions_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/db/sessions_test.go`:

```go
func TestGetManagedSessionNamesByCliIDs(t *testing.T) {
	store := newTestStore(t)

	// Create managed session with a CLI session ID and a user-defined name
	sess1, err := store.CreateManagedSession("/tmp/proj-a", "", 10, 5.0, 0)
	if err != nil {
		t.Fatalf("create managed session 1: %v", err)
	}
	if _, err := store.db.Exec(
		`UPDATE sessions SET claude_session_id = ?, name = ? WHERE id = ?`,
		"cli-uuid-named", "auth JWT refactor", sess1.ID,
	); err != nil {
		t.Fatalf("set cli session id + name: %v", err)
	}

	// Create managed session with a CLI session ID but NO name
	sess2, err := store.CreateManagedSession("/tmp/proj-b", "", 10, 5.0, 0)
	if err != nil {
		t.Fatalf("create managed session 2: %v", err)
	}
	if _, err := store.db.Exec(
		`UPDATE sessions SET claude_session_id = ? WHERE id = ?`,
		"cli-uuid-unnamed", sess2.ID,
	); err != nil {
		t.Fatalf("set cli session id: %v", err)
	}

	t.Run("returns name for matching CLI session with name set", func(t *testing.T) {
		got, err := store.GetManagedSessionNamesByCliIDs([]string{"cli-uuid-named", "cli-uuid-unnamed", "cli-uuid-unknown"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d: %v", len(got), got)
		}
		if got["cli-uuid-named"] != "auth JWT refactor" {
			t.Errorf("expected 'auth JWT refactor', got %q", got["cli-uuid-named"])
		}
	})

	t.Run("returns empty map for empty input without querying", func(t *testing.T) {
		got, err := store.GetManagedSessionNamesByCliIDs([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}
	})

	t.Run("returns empty map when no IDs match", func(t *testing.T) {
		got, err := store.GetManagedSessionNamesByCliIDs([]string{"does-not-exist"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd server && go test ./db/ -run TestGetManagedSessionNamesByCliIDs -v
```

Expected: `FAIL` — `store.GetManagedSessionNamesByCliIDs undefined`

- [ ] **Step 3: Implement `GetManagedSessionNamesByCliIDs` in `server/db/sessions.go`**

Append after `UpdateSessionName` (around line 220):

```go
// GetManagedSessionNamesByCliIDs returns a map of claude_session_id → name
// for managed sessions that have a matching CLI session ID and a non-empty name.
// Returns an empty map immediately if ids is empty (SQLite IN () is a syntax error).
func (s *Store) GetManagedSessionNamesByCliIDs(ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(
		`SELECT claude_session_id, name FROM sessions WHERE claude_session_id IN (`+placeholders+`) AND name != ''`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get managed session names by cli ids: %w", err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var cliID, name string
		if err := rows.Scan(&cliID, &name); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result[cliID] = name
	}
	return result, rows.Err()
}
```

Verify `strings` is already imported in `sessions.go` — it is not. Add `"strings"` to the import block at the top of `server/db/sessions.go`:

```go
import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd server && go test ./db/ -run TestGetManagedSessionNamesByCliIDs -v
```

Expected:
```
--- PASS: TestGetManagedSessionNamesByCliIDs (0.00s)
    --- PASS: TestGetManagedSessionNamesByCliIDs/returns_name_for_matching_CLI_session_with_name_set (0.00s)
    --- PASS: TestGetManagedSessionNamesByCliIDs/returns_empty_map_for_empty_input_without_querying (0.00s)
    --- PASS: TestGetManagedSessionNamesByCliIDs/returns_empty_map_when_no_IDs_match (0.00s)
PASS
```

- [ ] **Step 5: Run the full DB test suite to catch regressions**

```bash
cd server && go test ./db/ -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/db/sessions.go server/db/sessions_test.go
git commit -m "feat(db): add GetManagedSessionNamesByCliIDs for resume picker labels"
```

---

## Task 2: API — Add `label` to Resume Picker Response

**Files:**
- Modify: `server/api/resume.go`

- [ ] **Step 1: Add `Label` field to `resumableSession` struct**

In `server/api/resume.go`, find the `resumableSession` struct (around line 73) and add the `Label` field:

```go
type resumableSession struct {
	SessionID    string `json:"session_id"`
	Summary      string `json:"summary"`
	FirstPrompt  string `json:"first_prompt"`
	Label        string `json:"label"`
	MessageCount int    `json:"message_count"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"git_branch"`
}
```

- [ ] **Step 2: Add bulk label lookup to `handleResumableList`**

In `server/api/resume.go`, find the block after the `for _, e := range entries` loop that builds `results` (the loop ends around line 278, just before the `// Sort by modified descending` comment). Insert the label lookup between the loop and the sort:

```go
	// Bulk-lookup managed session names by CLI session ID.
	// Non-fatal: if the lookup fails, proceed without labels.
	if len(results) > 0 {
		cliIDs := make([]string, len(results))
		for i, r := range results {
			cliIDs[i] = r.SessionID
		}
		nameMap, err := s.store.GetManagedSessionNamesByCliIDs(cliIDs)
		if err != nil {
			log.Printf("resume: label lookup failed: %v", err)
			nameMap = map[string]string{}
		}
		for i := range results {
			results[i].Label = nameMap[results[i].SessionID]
		}
	}

	// Sort by modified descending
```

- [ ] **Step 3: Build to verify no compile errors**

```bash
cd server && go build -o claude-controller .
```

Expected: no errors, binary produced.

- [ ] **Step 4: Run the full test suite**

```bash
cd server && go test ./... -v 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add server/api/resume.go
git commit -m "feat(api): surface managed session name as label in resume picker"
```

---

## Task 3: Frontend — Display Chain and Hint Text

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Update the session title display chain**

In `server/web/static/index.html`, find line ~1224 (inside the resume picker modal, the element that shows the session title):

```html
<div style="font-weight:600; font-size:14px; margin-bottom:2px;" x-text="rs.summary || 'Untitled session'"></div>
```

Replace with:

```html
<div style="font-weight:600; font-size:14px; margin-bottom:2px;" x-text="rs.label || rs.summary || rs.first_prompt || 'Untitled session'"></div>
```

- [ ] **Step 2: Add hint text below the modal heading**

Find the `<h3>Resume a Session</h3>` line (around line 1209) and add hint text immediately after it:

```html
        <h3>Resume a Session</h3>
        <p style="font-size:11px; color:var(--text-muted); margin:0 0 12px 0;">Rename a session from the sidebar to identify it here.</p>
```

- [ ] **Step 3: Build to embed the updated HTML**

```bash
cd server && go build -o claude-controller .
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): show session label in resume picker with first_prompt fallback"
```

---

## Task 4: Verify End-to-End

- [ ] **Step 1: Start the server**

```bash
cd server && ./claude-controller
```

Expected: server starts on `:8080`, no errors in stdout.

- [ ] **Step 2: Confirm existing rename works**

Open the web UI in a browser. Double-click a managed session name in the left sidebar. Rename it to something distinctive (e.g., "my test session"). Press Enter. Verify the new name persists after clicking away and reloading the page.

- [ ] **Step 3: Confirm label appears in resume picker**

In the renamed session's chat, type `/resume`. The resume picker modal should open. If the session has a `claude_session_id` set (i.e., at least one message has been sent), the matching CLI session entry should show "my test session" as its title instead of the summary/first_prompt/Untitled fallback.

- [ ] **Step 4: Confirm fallback chain**

Check that sessions without a matching managed session name still show `summary` if present, then `first_prompt`, then "Untitled session". The hint text "Rename a session from the sidebar to identify it here." should be visible below the modal heading.

- [ ] **Step 5: Clean up binary**

```bash
rm server/claude-controller
```
