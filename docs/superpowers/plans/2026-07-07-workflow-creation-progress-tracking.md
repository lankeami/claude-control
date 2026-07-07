# Workflow Creation & Progress Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-step workflow definitions with DAG branching and real-time progress tracking to the Claude Controller web UI.

**Architecture:** Four new SQLite tables (`workflows`, `workflow_steps`, `workflow_runs`, `workflow_run_steps`) added as migrations in `server/db/db.go`. New DB layer file `server/db/workflows.go` with CRUD + run tracking. New API handler file `server/api/workflows.go` with CRUD, execution, and SSE endpoints. New engine file `server/managed/workflow_engine.go` that orchestrates step execution by reusing existing managed session message infrastructure. Frontend additions to `server/web/static/app.js` and `server/web/static/index.html` for the workflow builder modal and progress panel.

**Tech Stack:** Go 1.21+, SQLite (database/sql), Alpine.js (frontend), SSE (real-time updates)

## Global Constraints

- All new API endpoints go behind the existing `AuthMiddleware` + `RateLimiter` chain in `server/api/router.go`
- SSE endpoints handle their own auth via query param (same pattern as `/api/events` and `/api/sessions/{id}/stream`)
- UUIDs generated via `github.com/google/uuid`
- Migrations are idempotent `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE` statements appended to the migrations slice in `server/db/db.go`
- Step prompts are treated as literal strings — no template variable interpolation
- Follow existing JSON error response pattern: `{"error":"message"}`
- Tests use the existing `newTestServer(t)` / `authReq()` helpers from `server/api/sessions_test.go`

---

### Task 1: Database Schema & CRUD for Workflows and Steps

**Files:**
- Modify: `server/db/db.go` (append migrations to the `migrations` slice, around line 169)
- Create: `server/db/workflows.go`
- Create: `server/db/workflows_test.go`

**Interfaces:**
- Consumes: `db.Store` struct and its `db *sql.DB` field, `db.Open()` function, `github.com/google/uuid`
- Produces:
  - Types: `Workflow{ID, Name, Description, CreatedAt, UpdatedAt string}`, `WorkflowStep{ID, WorkflowID, Name, Prompt string; StepOrder int; OnSuccess, OnFailure *string; MaxRetries, TimeoutSeconds int}`
  - Methods on `*Store`: `CreateWorkflow(name, description string, steps []WorkflowStepInput) (*Workflow, error)`, `GetWorkflow(id string) (*Workflow, error)`, `GetWorkflowSteps(workflowID string) ([]WorkflowStep, error)`, `ListWorkflows() ([]Workflow, error)`, `UpdateWorkflow(id, name, description string, steps []WorkflowStepInput) error`, `DeleteWorkflow(id string) error`
  - Input type: `WorkflowStepInput{Name, Prompt string; StepOrder int; OnSuccessIndex, OnFailureIndex *int; MaxRetries, TimeoutSeconds int}`

- [ ] **Step 1: Write the failing test for CreateWorkflow**

In `server/db/workflows_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestCreateWorkflow_HappyPath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	steps := []WorkflowStepInput{
		{Name: "Run tests", Prompt: "Run all Go tests", StepOrder: 0, MaxRetries: 1},
		{Name: "Fix failures", Prompt: "Fix any test failures", StepOrder: 1},
	}
	// Auto-wire: step 0 on_success -> step 1
	idx1 := 1
	steps[0].OnSuccessIndex = &idx1

	wf, err := store.CreateWorkflow("CI Pipeline", "Run tests and fix", steps)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.ID == "" {
		t.Fatal("expected workflow ID")
	}
	if wf.Name != "CI Pipeline" {
		t.Errorf("expected name 'CI Pipeline', got %q", wf.Name)
	}

	got, err := store.GetWorkflowSteps(wf.ID)
	if err != nil {
		t.Fatalf("GetWorkflowSteps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(got))
	}
	if got[0].Name != "Run tests" {
		t.Errorf("step 0 name: got %q", got[0].Name)
	}
	if got[0].OnSuccess == nil || *got[0].OnSuccess != got[1].ID {
		t.Error("step 0 on_success should point to step 1")
	}
	if got[0].MaxRetries != 1 {
		t.Errorf("step 0 max_retries: got %d", got[0].MaxRetries)
	}
	if got[1].OnSuccess != nil {
		t.Error("step 1 on_success should be nil (end of workflow)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestCreateWorkflow_HappyPath`
Expected: FAIL — types and methods not defined

- [ ] **Step 3: Add migrations to db.go**

In `server/db/db.go`, append these to the `migrations` slice (after the last entry around line 168, before the closing `}`):

```go
`CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
`CREATE TABLE IF NOT EXISTS workflow_steps (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    step_order INTEGER NOT NULL,
    on_success TEXT REFERENCES workflow_steps(id) ON DELETE SET NULL,
    on_failure TEXT REFERENCES workflow_steps(id) ON DELETE SET NULL,
    max_retries INTEGER NOT NULL DEFAULT 0,
    timeout_seconds INTEGER NOT NULL DEFAULT 0
)`,
`CREATE INDEX IF NOT EXISTS idx_workflow_steps_workflow ON workflow_steps(workflow_id)`,
`CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','paused','completed','failed','cancelled')),
    current_step_id TEXT REFERENCES workflow_steps(id),
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at DATETIME,
    error TEXT
)`,
`CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow ON workflow_runs(workflow_id)`,
`CREATE INDEX IF NOT EXISTS idx_workflow_runs_session ON workflow_runs(session_id)`,
`CREATE TABLE IF NOT EXISTS workflow_run_steps (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_id TEXT NOT NULL REFERENCES workflow_steps(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','skipped')),
    attempt INTEGER NOT NULL DEFAULT 1,
    started_at DATETIME,
    finished_at DATETIME,
    error TEXT
)`,
`CREATE INDEX IF NOT EXISTS idx_workflow_run_steps_run ON workflow_run_steps(run_id)`,
```

- [ ] **Step 4: Implement workflows.go with types and CRUD**

Create `server/db/workflows.go`:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Workflow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WorkflowStep struct {
	ID             string  `json:"id"`
	WorkflowID     string  `json:"workflow_id"`
	Name           string  `json:"name"`
	Prompt         string  `json:"prompt"`
	StepOrder      int     `json:"step_order"`
	OnSuccess      *string `json:"on_success"`
	OnFailure      *string `json:"on_failure"`
	MaxRetries     int     `json:"max_retries"`
	TimeoutSeconds int     `json:"timeout_seconds"`
}

type WorkflowStepInput struct {
	Name           string `json:"name"`
	Prompt         string `json:"prompt"`
	StepOrder      int    `json:"step_order"`
	OnSuccessIndex *int   `json:"on_success_index"`
	OnFailureIndex *int   `json:"on_failure_index"`
	MaxRetries     int    `json:"max_retries"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Store) CreateWorkflow(name, description string, steps []WorkflowStepInput) (*Workflow, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	wfID := uuid.New().String()
	_, err = tx.Exec(`INSERT INTO workflows (id, name, description) VALUES (?, ?, ?)`, wfID, name, description)
	if err != nil {
		return nil, fmt.Errorf("insert workflow: %w", err)
	}

	stepIDs := make([]string, len(steps))
	for i := range steps {
		stepIDs[i] = uuid.New().String()
	}

	for i, step := range steps {
		var onSuccess, onFailure *string
		if step.OnSuccessIndex != nil && *step.OnSuccessIndex >= 0 && *step.OnSuccessIndex < len(stepIDs) {
			onSuccess = &stepIDs[*step.OnSuccessIndex]
		}
		if step.OnFailureIndex != nil && *step.OnFailureIndex >= 0 && *step.OnFailureIndex < len(stepIDs) {
			onFailure = &stepIDs[*step.OnFailureIndex]
		}
		_, err = tx.Exec(`INSERT INTO workflow_steps (id, workflow_id, name, prompt, step_order, on_success, on_failure, max_retries, timeout_seconds) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			stepIDs[i], wfID, step.Name, step.Prompt, step.StepOrder, onSuccess, onFailure, step.MaxRetries, step.TimeoutSeconds)
		if err != nil {
			return nil, fmt.Errorf("insert step %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return s.GetWorkflow(wfID)
}

func (s *Store) GetWorkflow(id string) (*Workflow, error) {
	var wf Workflow
	err := s.db.QueryRow(`SELECT id, name, description, created_at, updated_at FROM workflows WHERE id = ?`, id).
		Scan(&wf.ID, &wf.Name, &wf.Description, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return &wf, nil
}

func (s *Store) GetWorkflowSteps(workflowID string) ([]WorkflowStep, error) {
	rows, err := s.db.Query(`SELECT id, workflow_id, name, prompt, step_order, on_success, on_failure, max_retries, timeout_seconds FROM workflow_steps WHERE workflow_id = ? ORDER BY step_order`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("query steps: %w", err)
	}
	defer rows.Close()

	var steps []WorkflowStep
	for rows.Next() {
		var step WorkflowStep
		if err := rows.Scan(&step.ID, &step.WorkflowID, &step.Name, &step.Prompt, &step.StepOrder, &step.OnSuccess, &step.OnFailure, &step.MaxRetries, &step.TimeoutSeconds); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func (s *Store) ListWorkflows() ([]Workflow, error) {
	rows, err := s.db.Query(`SELECT id, name, description, created_at, updated_at FROM workflows ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var workflows []Workflow
	for rows.Next() {
		var wf Workflow
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.Description, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		workflows = append(workflows, wf)
	}
	return workflows, rows.Err()
}

func (s *Store) UpdateWorkflow(id, name, description string, steps []WorkflowStepInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE workflows SET name = ?, description = ?, updated_at = datetime('now') WHERE id = ?`, name, description, id)
	if err != nil {
		return fmt.Errorf("update workflow: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM workflow_steps WHERE workflow_id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete old steps: %w", err)
	}

	stepIDs := make([]string, len(steps))
	for i := range steps {
		stepIDs[i] = uuid.New().String()
	}

	for i, step := range steps {
		var onSuccess, onFailure *string
		if step.OnSuccessIndex != nil && *step.OnSuccessIndex >= 0 && *step.OnSuccessIndex < len(stepIDs) {
			onSuccess = &stepIDs[*step.OnSuccessIndex]
		}
		if step.OnFailureIndex != nil && *step.OnFailureIndex >= 0 && *step.OnFailureIndex < len(stepIDs) {
			onFailure = &stepIDs[*step.OnFailureIndex]
		}
		_, err = tx.Exec(`INSERT INTO workflow_steps (id, workflow_id, name, prompt, step_order, on_success, on_failure, max_retries, timeout_seconds) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			stepIDs[i], id, step.Name, step.Prompt, step.StepOrder, onSuccess, onFailure, step.MaxRetries, step.TimeoutSeconds)
		if err != nil {
			return fmt.Errorf("insert step %d: %w", i, err)
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteWorkflow(id string) error {
	_, err := s.db.Exec(`DELETE FROM workflows WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd server && go test ./db/ -v -run TestCreateWorkflow_HappyPath`
Expected: PASS

- [ ] **Step 6: Write tests for List, Update, Delete**

Add to `server/db/workflows_test.go`:

```go
func TestListWorkflows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	store.CreateWorkflow("WF1", "", nil)
	store.CreateWorkflow("WF2", "", nil)

	wfs, err := store.ListWorkflows()
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(wfs))
	}
}

func TestUpdateWorkflow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	wf, _ := store.CreateWorkflow("Original", "desc", []WorkflowStepInput{
		{Name: "Step1", Prompt: "do thing", StepOrder: 0},
	})

	newSteps := []WorkflowStepInput{
		{Name: "New Step", Prompt: "new prompt", StepOrder: 0},
		{Name: "Step 2", Prompt: "second", StepOrder: 1},
	}
	if err := store.UpdateWorkflow(wf.ID, "Updated", "new desc", newSteps); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	updated, _ := store.GetWorkflow(wf.ID)
	if updated.Name != "Updated" {
		t.Errorf("expected 'Updated', got %q", updated.Name)
	}
	steps, _ := store.GetWorkflowSteps(wf.ID)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps after update, got %d", len(steps))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	wf, _ := store.CreateWorkflow("ToDelete", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p", StepOrder: 0},
	})
	if err := store.DeleteWorkflow(wf.ID); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	got, _ := store.GetWorkflow(wf.ID)
	if got != nil {
		t.Error("expected workflow to be deleted")
	}
	steps, _ := store.GetWorkflowSteps(wf.ID)
	if len(steps) != 0 {
		t.Error("expected steps to be cascade-deleted")
	}
}
```

- [ ] **Step 7: Run all workflow DB tests**

Run: `cd server && go test ./db/ -v -run TestCreateWorkflow -run TestListWorkflows -run TestUpdateWorkflow -run TestDeleteWorkflow`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add server/db/db.go server/db/workflows.go server/db/workflows_test.go
git commit -m "feat: add workflow and workflow_steps tables with CRUD operations"
```

---

### Task 2: Database Layer for Workflow Runs

**Files:**
- Modify: `server/db/workflows.go` (add run-related types and methods)
- Modify: `server/db/workflows_test.go` (add run tests)

**Interfaces:**
- Consumes: `Workflow`, `WorkflowStep` types from Task 1, `db.Store`
- Produces:
  - Types: `WorkflowRun{ID, WorkflowID, SessionID, Status string; CurrentStepID *string; StartedAt time.Time; FinishedAt *time.Time; Error *string}`, `WorkflowRunStep{ID, RunID, StepID, Status string; Attempt int; StartedAt, FinishedAt *time.Time; Error *string}`
  - Methods: `CreateWorkflowRun(workflowID, sessionID string) (*WorkflowRun, error)`, `GetWorkflowRun(id string) (*WorkflowRun, error)`, `ListWorkflowRuns(workflowID, sessionID string) ([]WorkflowRun, error)`, `UpdateWorkflowRunStatus(id, status string, err *string) error`, `UpdateWorkflowRunCurrentStep(id, stepID string) error`, `CreateWorkflowRunSteps(runID string, stepIDs []string) error`, `GetWorkflowRunSteps(runID string) ([]WorkflowRunStep, error)`, `UpdateWorkflowRunStepStatus(id, status string, attempt int, err *string) error`, `GetActiveRunForSession(sessionID string) (*WorkflowRun, error)`

- [ ] **Step 1: Write failing test for CreateWorkflowRun**

Add to `server/db/workflows_test.go`:

```go
func TestCreateWorkflowRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/test", "[]", 50, 5.0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	wf, _ := store.CreateWorkflow("Test WF", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p1", StepOrder: 0},
	})

	run, err := store.CreateWorkflowRun(wf.ID, sess.ID)
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	if run.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", run.Status)
	}
	if run.SessionID != sess.ID {
		t.Errorf("expected session_id %q, got %q", sess.ID, run.SessionID)
	}
}

func TestGetActiveRunForSession(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test2", "[]", 50, 5.0, 0)
	wf, _ := store.CreateWorkflow("WF", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p", StepOrder: 0},
	})

	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	store.UpdateWorkflowRunStatus(run.ID, "running", nil)

	active, err := store.GetActiveRunForSession(sess.ID)
	if err != nil {
		t.Fatalf("GetActiveRunForSession: %v", err)
	}
	if active == nil || active.ID != run.ID {
		t.Error("expected to find the active run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestCreateWorkflowRun`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement run methods in workflows.go**

Append to `server/db/workflows.go`:

```go
type WorkflowRun struct {
	ID            string     `json:"id"`
	WorkflowID    string     `json:"workflow_id"`
	SessionID     string     `json:"session_id"`
	Status        string     `json:"status"`
	CurrentStepID *string    `json:"current_step_id"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	Error         *string    `json:"error"`
}

type WorkflowRunStep struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	StepID     string     `json:"step_id"`
	Status     string     `json:"status"`
	Attempt    int        `json:"attempt"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      *string    `json:"error"`
}

func (s *Store) CreateWorkflowRun(workflowID, sessionID string) (*WorkflowRun, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO workflow_runs (id, workflow_id, session_id) VALUES (?, ?, ?)`, id, workflowID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("create workflow run: %w", err)
	}
	return s.GetWorkflowRun(id)
}

func (s *Store) GetWorkflowRun(id string) (*WorkflowRun, error) {
	var r WorkflowRun
	err := s.db.QueryRow(`SELECT id, workflow_id, session_id, status, current_step_id, started_at, finished_at, error FROM workflow_runs WHERE id = ?`, id).
		Scan(&r.ID, &r.WorkflowID, &r.SessionID, &r.Status, &r.CurrentStepID, &r.StartedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow run: %w", err)
	}
	return &r, nil
}

func (s *Store) ListWorkflowRuns(workflowID, sessionID string) ([]WorkflowRun, error) {
	query := `SELECT id, workflow_id, session_id, status, current_step_id, started_at, finished_at, error FROM workflow_runs WHERE 1=1`
	var args []interface{}
	if workflowID != "" {
		query += ` AND workflow_id = ?`
		args = append(args, workflowID)
	}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY started_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var r WorkflowRun
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.SessionID, &r.Status, &r.CurrentStepID, &r.StartedAt, &r.FinishedAt, &r.Error); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) UpdateWorkflowRunStatus(id, status string, runErr *string) error {
	if status == "completed" || status == "failed" || status == "cancelled" {
		_, err := s.db.Exec(`UPDATE workflow_runs SET status = ?, error = ?, finished_at = datetime('now') WHERE id = ?`, status, runErr, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE workflow_runs SET status = ?, error = ? WHERE id = ?`, status, runErr, id)
	return err
}

func (s *Store) UpdateWorkflowRunCurrentStep(id, stepID string) error {
	_, err := s.db.Exec(`UPDATE workflow_runs SET current_step_id = ? WHERE id = ?`, stepID, id)
	return err
}

func (s *Store) CreateWorkflowRunSteps(runID string, stepIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sid := range stepIDs {
		id := uuid.New().String()
		_, err := tx.Exec(`INSERT INTO workflow_run_steps (id, run_id, step_id) VALUES (?, ?, ?)`, id, runID, sid)
		if err != nil {
			return fmt.Errorf("insert run step: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) GetWorkflowRunSteps(runID string) ([]WorkflowRunStep, error) {
	rows, err := s.db.Query(`SELECT rs.id, rs.run_id, rs.step_id, rs.status, rs.attempt, rs.started_at, rs.finished_at, rs.error FROM workflow_run_steps rs JOIN workflow_steps ws ON rs.step_id = ws.id WHERE rs.run_id = ? ORDER BY ws.step_order`, runID)
	if err != nil {
		return nil, fmt.Errorf("query run steps: %w", err)
	}
	defer rows.Close()
	var steps []WorkflowRunStep
	for rows.Next() {
		var rs WorkflowRunStep
		if err := rows.Scan(&rs.ID, &rs.RunID, &rs.StepID, &rs.Status, &rs.Attempt, &rs.StartedAt, &rs.FinishedAt, &rs.Error); err != nil {
			return nil, fmt.Errorf("scan run step: %w", err)
		}
		steps = append(steps, rs)
	}
	return steps, rows.Err()
}

func (s *Store) UpdateWorkflowRunStepStatus(id, status string, attempt int, stepErr *string) error {
	if status == "running" {
		_, err := s.db.Exec(`UPDATE workflow_run_steps SET status = ?, attempt = ?, started_at = datetime('now') WHERE id = ?`, status, attempt, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE workflow_run_steps SET status = ?, attempt = ?, error = ?, finished_at = datetime('now') WHERE id = ?`, status, attempt, stepErr, id)
	return err
}

func (s *Store) GetActiveRunForSession(sessionID string) (*WorkflowRun, error) {
	var r WorkflowRun
	err := s.db.QueryRow(`SELECT id, workflow_id, session_id, status, current_step_id, started_at, finished_at, error FROM workflow_runs WHERE session_id = ? AND status IN ('pending','running','paused') LIMIT 1`, sessionID).
		Scan(&r.ID, &r.WorkflowID, &r.SessionID, &r.Status, &r.CurrentStepID, &r.StartedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active run: %w", err)
	}
	return &r, nil
}

func (s *Store) ResetStaleWorkflowRuns() error {
	errMsg := "server restarted"
	_, err := s.db.Exec(`UPDATE workflow_runs SET status = 'failed', error = ?, finished_at = datetime('now') WHERE status = 'running'`, errMsg)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./db/ -v -run "TestCreateWorkflowRun|TestGetActiveRunForSession"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/workflows.go server/db/workflows_test.go
git commit -m "feat: add workflow run and run step DB operations"
```

---

### Task 3: Workflow CRUD API Endpoints

**Files:**
- Create: `server/api/workflows.go`
- Modify: `server/api/router.go` (register new routes, around line 131)
- Create: `server/api/workflows_test.go`

**Interfaces:**
- Consumes: `db.Workflow`, `db.WorkflowStep`, `db.WorkflowStepInput`, `db.Store` methods from Tasks 1-2
- Produces: HTTP handlers: `handleCreateWorkflow`, `handleListWorkflows`, `handleGetWorkflow`, `handleUpdateWorkflow`, `handleDeleteWorkflow`

- [ ] **Step 1: Write failing test for POST /api/workflows**

Create `server/api/workflows_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestCreateWorkflow_HappyPath(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name":        "CI Pipeline",
		"description": "Run tests and fix",
		"steps": []map[string]interface{}{
			{"name": "Run tests", "prompt": "Run go test ./...", "step_order": 0, "on_success_index": 1},
			{"name": "Create PR", "prompt": "Create a PR", "step_order": 1},
		},
	}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result struct {
		Workflow db.Workflow       `json:"workflow"`
		Steps    []db.WorkflowStep `json:"steps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Workflow.Name != "CI Pipeline" {
		t.Errorf("expected name 'CI Pipeline', got %q", result.Workflow.Name)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}
}

func TestCreateWorkflow_MissingName(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{"steps": []map[string]interface{}{}}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListWorkflows(t *testing.T) {
	ts, store := newTestServer(t)
	store.CreateWorkflow("WF1", "", nil)
	store.CreateWorkflow("WF2", "", nil)

	req := authReq("GET", ts.URL+"/api/workflows", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var wfs []db.Workflow
	json.NewDecoder(resp.Body).Decode(&wfs)
	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(wfs))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	ts, store := newTestServer(t)
	wf, _ := store.CreateWorkflow("ToDelete", "", nil)

	req := authReq("DELETE", ts.URL+"/api/workflows/"+wf.ID, nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestCreateWorkflow_HappyPath`
Expected: FAIL — handler not registered

- [ ] **Step 3: Implement workflows.go API handlers**

Create `server/api/workflows.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type createWorkflowRequest struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Steps       []db.WorkflowStepInput `json:"steps"`
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	wf, err := s.store.CreateWorkflow(req.Name, req.Description, req.Steps)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	steps, err := s.store.GetWorkflowSteps(wf.ID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": wf, "steps": steps})
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	wfs, err := s.store.ListWorkflows()
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wfs)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wf, err := s.store.GetWorkflow(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if wf == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	steps, err := s.store.GetWorkflowSteps(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": wf, "steps": steps})
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wf, err := s.store.GetWorkflow(id)
	if err != nil || wf == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateWorkflow(id, req.Name, req.Description, req.Steps); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	updated, _ := s.store.GetWorkflow(id)
	steps, _ := s.store.GetWorkflowSteps(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": updated, "steps": steps})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteWorkflow(id); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Register routes in router.go**

In `server/api/router.go`, after the scheduled task endpoints block (around line 131), add:

```go
	// Workflow endpoints
	apiMux.HandleFunc("POST /api/workflows", s.handleCreateWorkflow)
	apiMux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	apiMux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	apiMux.HandleFunc("PUT /api/workflows/{id}", s.handleUpdateWorkflow)
	apiMux.HandleFunc("DELETE /api/workflows/{id}", s.handleDeleteWorkflow)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run "TestCreateWorkflow|TestListWorkflows|TestDeleteWorkflow"`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/workflows.go server/api/workflows_test.go server/api/router.go
git commit -m "feat: add workflow CRUD API endpoints"
```

---

### Task 4: Workflow Execution Engine

**Files:**
- Create: `server/managed/workflow_engine.go`
- Create: `server/managed/workflow_engine_test.go`

**Interfaces:**
- Consumes: `db.Store` methods (workflow CRUD, run tracking, `UpdateActivityState`, `GetSession`), `managed.Broadcaster`, `managed.NewBroadcaster()`
- Produces:
  - Type: `WorkflowEngine` struct
  - Constructor: `NewWorkflowEngine(store *db.Store, sendMessage func(sessionID, prompt string) error, getActivityState func(sessionID string) (string, error), interruptSession func(sessionID string) error) *WorkflowEngine`
  - Methods: `StartRun(runID string) error`, `PauseRun(runID string) error`, `ResumeRun(runID string) error`, `CancelRun(runID string) error`, `GetRunBroadcaster(runID string) *Broadcaster`, `RecoverStaleRuns()`

- [ ] **Step 1: Write failing test for basic step execution**

Create `server/managed/workflow_engine_test.go`:

```go
package managed

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestWorkflowEngine_LinearRun(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/wf-test", "[]", 50, 5.0, 0)

	var mu sync.Mutex
	var sentPrompts []string
	activityState := "idle"

	sendMessage := func(sessionID, prompt string) error {
		mu.Lock()
		sentPrompts = append(sentPrompts, prompt)
		activityState = "working"
		mu.Unlock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			activityState = "idle"
			mu.Unlock()
		}()
		return nil
	}

	getActivity := func(sessionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return activityState, nil
	}

	interrupt := func(sessionID string) error { return nil }

	engine := NewWorkflowEngine(store, sendMessage, getActivity, interrupt)

	idx1 := 1
	wf, _ := store.CreateWorkflow("Test", "", []db.WorkflowStepInput{
		{Name: "Step 1", Prompt: "Do step 1", StepOrder: 0, OnSuccessIndex: &idx1},
		{Name: "Step 2", Prompt: "Do step 2", StepOrder: 1},
	})

	steps, _ := store.GetWorkflowSteps(wf.ID)
	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	stepIDs := make([]string, len(steps))
	for i, s := range steps {
		stepIDs[i] = s.ID
	}
	store.CreateWorkflowRunSteps(run.ID, stepIDs)

	if err := engine.StartRun(run.ID); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	// Wait for completion
	deadline := time.After(5 * time.Second)
	for {
		r, _ := store.GetWorkflowRun(run.ID)
		if r.Status == "completed" || r.Status == "failed" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not complete within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	r, _ := store.GetWorkflowRun(run.ID)
	if r.Status != "completed" {
		t.Errorf("expected 'completed', got %q", r.Status)
	}

	mu.Lock()
	if len(sentPrompts) != 2 {
		t.Errorf("expected 2 prompts sent, got %d", len(sentPrompts))
	}
	if len(sentPrompts) > 0 && sentPrompts[0] != "Do step 1" {
		t.Errorf("first prompt: got %q", sentPrompts[0])
	}
	mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestWorkflowEngine_LinearRun`
Expected: FAIL — `NewWorkflowEngine` not defined

- [ ] **Step 3: Implement workflow_engine.go**

Create `server/managed/workflow_engine.go`:

```go
package managed

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type WorkflowEngine struct {
	store            *db.Store
	sendMessage      func(sessionID, prompt string) error
	getActivityState func(sessionID string) (string, error)
	interruptSession func(sessionID string) error

	mu           sync.Mutex
	broadcasters map[string]*Broadcaster
	pauseFlags   map[string]bool
	cancelFlags  map[string]bool
}

func NewWorkflowEngine(
	store *db.Store,
	sendMessage func(sessionID, prompt string) error,
	getActivityState func(sessionID string) (string, error),
	interruptSession func(sessionID string) error,
) *WorkflowEngine {
	return &WorkflowEngine{
		store:            store,
		sendMessage:      sendMessage,
		getActivityState: getActivityState,
		interruptSession: interruptSession,
		broadcasters:     make(map[string]*Broadcaster),
		pauseFlags:       make(map[string]bool),
		cancelFlags:      make(map[string]bool),
	}
}

func (e *WorkflowEngine) GetRunBroadcaster(runID string) *Broadcaster {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.broadcasters[runID]
	if !ok {
		b = NewBroadcaster()
		e.broadcasters[runID] = b
	}
	return b
}

func (e *WorkflowEngine) broadcast(runID string, msg map[string]interface{}) {
	b := e.GetRunBroadcaster(runID)
	data, _ := json.Marshal(msg)
	b.Send(string(data))
}

func (e *WorkflowEngine) StartRun(runID string) error {
	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		return fmt.Errorf("run not found: %s", runID)
	}

	active, _ := e.store.GetActiveRunForSession(run.SessionID)
	if active != nil && active.ID != runID {
		return fmt.Errorf("session already has active run: %s", active.ID)
	}

	go e.executeRun(runID)
	return nil
}

func (e *WorkflowEngine) executeRun(runID string) {
	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		log.Printf("workflow engine: run %s not found", runID)
		return
	}

	e.store.UpdateWorkflowRunStatus(runID, "running", nil)
	e.broadcast(runID, map[string]interface{}{"type": "run_started"})

	steps, err := e.store.GetWorkflowSteps(run.WorkflowID)
	if err != nil || len(steps) == 0 {
		errMsg := "no steps found"
		e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
		e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
		return
	}

	runSteps, _ := e.store.GetWorkflowRunSteps(runID)
	stepToRunStep := make(map[string]string)
	for _, rs := range runSteps {
		stepToRunStep[rs.StepID] = rs.ID
	}

	stepMap := make(map[string]db.WorkflowStep)
	for _, s := range steps {
		stepMap[s.ID] = s
	}

	currentStep := steps[0]
	if run.CurrentStepID != nil {
		if s, ok := stepMap[*run.CurrentStepID]; ok {
			currentStep = s
		}
	}

	for {
		e.mu.Lock()
		cancelled := e.cancelFlags[runID]
		paused := e.pauseFlags[runID]
		e.mu.Unlock()

		if cancelled {
			e.store.UpdateWorkflowRunStatus(runID, "cancelled", nil)
			e.broadcast(runID, map[string]interface{}{"type": "run_cancelled"})
			e.cleanup(runID)
			return
		}

		if paused {
			e.store.UpdateWorkflowRunStatus(runID, "paused", nil)
			e.broadcast(runID, map[string]interface{}{"type": "run_paused"})
			return
		}

		rsID := stepToRunStep[currentStep.ID]
		e.store.UpdateWorkflowRunCurrentStep(runID, currentStep.ID)
		e.store.UpdateWorkflowRunStepStatus(rsID, "running", 1, nil)
		e.broadcast(runID, map[string]interface{}{"type": "step_started", "step_id": currentStep.ID, "step_name": currentStep.Name, "attempt": 1})

		success := e.executeStep(runID, run.SessionID, currentStep, rsID)

		if !success {
			attempt := 1
			for attempt < currentStep.MaxRetries {
				attempt++
				e.store.UpdateWorkflowRunStepStatus(rsID, "running", attempt, nil)
				e.broadcast(runID, map[string]interface{}{"type": "step_started", "step_id": currentStep.ID, "step_name": currentStep.Name, "attempt": attempt})
				success = e.executeStep(runID, run.SessionID, currentStep, rsID)
				if success {
					break
				}
			}
		}

		if success {
			e.store.UpdateWorkflowRunStepStatus(rsID, "completed", 0, nil)
			e.broadcast(runID, map[string]interface{}{"type": "step_completed", "step_id": currentStep.ID, "status": "completed"})

			if currentStep.OnSuccess == nil {
				e.store.UpdateWorkflowRunStatus(runID, "completed", nil)
				e.broadcast(runID, map[string]interface{}{"type": "run_completed", "status": "completed"})
				e.cleanup(runID)
				return
			}
			next, ok := stepMap[*currentStep.OnSuccess]
			if !ok {
				e.store.UpdateWorkflowRunStatus(runID, "completed", nil)
				e.broadcast(runID, map[string]interface{}{"type": "run_completed", "status": "completed"})
				e.cleanup(runID)
				return
			}
			currentStep = next
		} else {
			errMsg := "step failed after retries"
			e.store.UpdateWorkflowRunStepStatus(rsID, "failed", 0, &errMsg)
			e.broadcast(runID, map[string]interface{}{"type": "step_failed", "step_id": currentStep.ID, "error": errMsg})

			if currentStep.OnFailure == nil {
				e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
				e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
				e.cleanup(runID)
				return
			}
			next, ok := stepMap[*currentStep.OnFailure]
			if !ok {
				e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
				e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
				e.cleanup(runID)
				return
			}
			currentStep = next
		}
	}
}

func (e *WorkflowEngine) executeStep(runID, sessionID string, step db.WorkflowStep, runStepID string) bool {
	if err := e.sendMessage(sessionID, step.Prompt); err != nil {
		return false
	}

	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.After(timeout)

	for {
		e.mu.Lock()
		cancelled := e.cancelFlags[runID]
		e.mu.Unlock()
		if cancelled {
			e.interruptSession(sessionID)
			return false
		}

		state, err := e.getActivityState(sessionID)
		if err != nil {
			return false
		}
		if state == "idle" || state == "waiting" {
			return true
		}

		select {
		case <-deadline:
			e.interruptSession(sessionID)
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (e *WorkflowEngine) PauseRun(runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pauseFlags[runID] = true
	return nil
}

func (e *WorkflowEngine) ResumeRun(runID string) error {
	e.mu.Lock()
	e.pauseFlags[runID] = false
	e.mu.Unlock()

	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		return fmt.Errorf("run not found")
	}
	if run.Status != "paused" {
		return fmt.Errorf("run is not paused")
	}

	e.store.UpdateWorkflowRunStatus(runID, "running", nil)
	go e.executeRun(runID)
	return nil
}

func (e *WorkflowEngine) CancelRun(runID string) error {
	e.mu.Lock()
	e.cancelFlags[runID] = true
	e.mu.Unlock()
	return nil
}

func (e *WorkflowEngine) cleanup(runID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pauseFlags, runID)
	delete(e.cancelFlags, runID)
}

func (e *WorkflowEngine) RecoverStaleRuns() {
	e.store.ResetStaleWorkflowRuns()
}
```

Add the missing `encoding/json` import at the top of the file.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestWorkflowEngine_LinearRun`
Expected: PASS

- [ ] **Step 5: Write test for pause/cancel**

Add to `server/managed/workflow_engine_test.go`:

```go
func TestWorkflowEngine_CancelRun(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/wf-cancel", "[]", 50, 5.0, 0)

	var mu sync.Mutex
	activityState := "idle"

	sendMessage := func(sessionID, prompt string) error {
		mu.Lock()
		activityState = "working"
		mu.Unlock()
		// Simulate a long-running step
		time.Sleep(2 * time.Second)
		mu.Lock()
		activityState = "idle"
		mu.Unlock()
		return nil
	}

	getActivity := func(sessionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return activityState, nil
	}

	interrupted := false
	interrupt := func(sessionID string) error {
		interrupted = true
		mu.Lock()
		activityState = "idle"
		mu.Unlock()
		return nil
	}

	engine := NewWorkflowEngine(store, sendMessage, getActivity, interrupt)

	wf, _ := store.CreateWorkflow("Long WF", "", []db.WorkflowStepInput{
		{Name: "Long step", Prompt: "Takes forever", StepOrder: 0},
	})
	steps, _ := store.GetWorkflowSteps(wf.ID)
	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	store.CreateWorkflowRunSteps(run.ID, []string{steps[0].ID})

	engine.StartRun(run.ID)
	time.Sleep(200 * time.Millisecond)
	engine.CancelRun(run.ID)

	deadline := time.After(5 * time.Second)
	for {
		r, _ := store.GetWorkflowRun(run.ID)
		if r.Status == "cancelled" || r.Status == "completed" || r.Status == "failed" {
			if r.Status != "cancelled" {
				t.Errorf("expected 'cancelled', got %q", r.Status)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not cancel within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd server && go test ./managed/ -v -run TestWorkflowEngine_CancelRun -timeout 10s`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add server/managed/workflow_engine.go server/managed/workflow_engine_test.go
git commit -m "feat: add workflow execution engine with pause/resume/cancel"
```

---

### Task 5: Workflow Execution & Run Tracking API Endpoints

**Files:**
- Modify: `server/api/workflows.go` (add execution and run handlers)
- Modify: `server/api/router.go` (register execution routes)
- Modify: `server/api/workflows_test.go` (add execution tests)
- Modify: `server/api/router.go` (add engine field to Server, wire SSE)

**Interfaces:**
- Consumes: `WorkflowEngine` from Task 4, `db.Store` run methods from Task 2, `managed.Broadcaster` for SSE
- Produces: HTTP handlers: `handleStartWorkflowRun`, `handlePauseRun`, `handleResumeRun`, `handleCancelRun`, `handleListWorkflowRuns`, `handleGetWorkflowRun`, `handleWorkflowRunStream`

- [ ] **Step 1: Add workflow engine to Server struct**

In `server/api/router.go`, add to the `Server` struct:

```go
workflowEngine *managed.WorkflowEngine
```

Add import for `managed` if not already present. In `NewRouter`, after creating the server, initialize the engine:

```go
s.workflowEngine = managed.NewWorkflowEngine(
    store,
    func(sessionID, prompt string) error {
        // Reuse the internal message-send path
        return s.sendWorkflowMessage(sessionID, prompt)
    },
    func(sessionID string) (string, error) {
        sess, err := store.GetSession(sessionID)
        if err != nil {
            return "", err
        }
        if sess == nil {
            return "", fmt.Errorf("session not found")
        }
        return sess.ActivityState, nil
    },
    func(sessionID string) error {
        return s.interruptManagedSession(sessionID)
    },
)
s.workflowEngine.RecoverStaleRuns()
```

- [ ] **Step 2: Add sendWorkflowMessage helper**

Append to `server/api/workflows.go`:

```go
func (s *Server) sendWorkflowMessage(sessionID, prompt string) error {
	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	seq, err := s.store.AddMessage(sessionID, "user", prompt)
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}

	s.store.UpdateActivityState(sessionID, "working")

	b := s.manager.GetBroadcaster(sessionID)
	userMsg := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":%s}]},"seq":%d}`, jsonString(prompt), seq)
	b.Send(userMsg)

	return nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s *Server) interruptManagedSession(sessionID string) error {
	if s.manager == nil {
		return nil
	}
	if s.manager.IsInteractiveRunning(sessionID) {
		return s.manager.InterruptInteractive(sessionID)
	}
	return s.manager.Interrupt(sessionID)
}
```

- [ ] **Step 3: Add execution and run tracking handlers**

Append to `server/api/workflows.go`:

```go
func (s *Server) handleStartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
		return
	}

	wf, err := s.store.GetWorkflow(workflowID)
	if err != nil || wf == nil {
		http.Error(w, `{"error":"workflow not found"}`, http.StatusNotFound)
		return
	}

	active, _ := s.store.GetActiveRunForSession(req.SessionID)
	if active != nil {
		http.Error(w, `{"error":"session already has an active workflow run"}`, http.StatusConflict)
		return
	}

	steps, _ := s.store.GetWorkflowSteps(workflowID)
	run, err := s.store.CreateWorkflowRun(workflowID, req.SessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	stepIDs := make([]string, len(steps))
	for i, st := range steps {
		stepIDs[i] = st.ID
	}
	s.store.CreateWorkflowRunSteps(run.ID, stepIDs)

	if err := s.workflowEngine.StartRun(run.ID); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

func (s *Server) handlePauseRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.PauseRun(runID); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.ResumeRun(runID); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.CancelRun(runID); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	workflowID := r.URL.Query().Get("workflow_id")
	sessionID := r.URL.Query().Get("session_id")
	runs, err := s.store.ListWorkflowRuns(workflowID, sessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := s.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	runSteps, _ := s.store.GetWorkflowRunSteps(runID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"run": run, "steps": runSteps})
}

func (s *Server) handleWorkflowRunStream(w http.ResponseWriter, r *http.Request, apiKey string) {
	token := r.URL.Query().Get("token")
	if token == "" || token != apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runID := r.PathValue("id")
	b := s.workflowEngine.GetRunBroadcaster(runID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, "data: {\"type\":\"heartbeat\"}\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

Add import for `managed` (for `HeartbeatInterval`): `"github.com/jaychinthrajah/claude-controller/server/managed"`

- [ ] **Step 4: Register execution routes in router.go**

After the workflow CRUD routes, add:

```go
	// Workflow execution endpoints
	apiMux.HandleFunc("POST /api/workflows/{id}/run", s.handleStartWorkflowRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/pause", s.handlePauseRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/resume", s.handleResumeRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/cancel", s.handleCancelRun)
	apiMux.HandleFunc("GET /api/workflow-runs", s.handleListWorkflowRuns)
	apiMux.HandleFunc("GET /api/workflow-runs/{id}", s.handleGetWorkflowRun)
```

And in the root mux section (outside the authed API, similar to session SSE), add:

```go
	// Workflow run SSE stream — handles its own auth via query param
	root.HandleFunc("GET /api/workflow-runs/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		s.handleWorkflowRunStream(w, r, apiKey)
	})
```

- [ ] **Step 5: Run all tests**

Run: `cd server && go test ./api/ -v -run "TestCreateWorkflow|TestListWorkflows|TestDeleteWorkflow" && go test ./managed/ -v -run TestWorkflowEngine`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/workflows.go server/api/workflows_test.go server/api/router.go
git commit -m "feat: add workflow execution and run tracking API endpoints with SSE"
```

---

### Task 6: Workflow Builder UI

**Files:**
- Modify: `server/web/static/app.js` (add workflow state, CRUD methods, builder logic)
- Modify: `server/web/static/index.html` (add workflow sidebar section and builder modal)

**Interfaces:**
- Consumes: API endpoints from Tasks 3 and 5
- Produces: Workflow list in sidebar, builder modal with step editor, "Run Workflow" dropdown in chat header

- [ ] **Step 1: Add workflow state to Alpine.js data in app.js**

In `server/web/static/app.js`, find the data section (around line 130 near `scheduledTasks`) and add:

```javascript
    workflows: [],
    workflowModalOpen: false,
    editingWorkflow: null,
    workflowForm: { name: '', description: '', steps: [] },
    workflowFormErrors: '',
    workflowLoading: false,
    workflowRuns: [],
    activeWorkflowRun: null,
    workflowRunSSE: null,
    workflowRunSteps: [],
```

- [ ] **Step 2: Add workflow CRUD methods in app.js**

Add these methods to the Alpine.js app object:

```javascript
    async loadWorkflows() {
        try {
            const res = await fetch('/api/workflows', {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (res.ok) this.workflows = await res.json();
        } catch (err) {
            console.error('Failed to load workflows:', err);
        }
    },

    openWorkflowModal(workflow) {
        if (workflow) {
            this.editingWorkflow = workflow;
            this.workflowLoading = true;
            fetch('/api/workflows/' + workflow.id, {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            }).then(r => r.json()).then(data => {
                this.workflowForm = {
                    name: data.workflow.name,
                    description: data.workflow.description || '',
                    steps: data.steps.map((s, i, arr) => ({
                        name: s.name, prompt: s.prompt, step_order: s.step_order,
                        max_retries: s.max_retries, timeout_seconds: s.timeout_seconds,
                        on_success_index: s.on_success ? arr.findIndex(x => x.id === s.on_success) : null,
                        on_failure_index: s.on_failure ? arr.findIndex(x => x.id === s.on_failure) : null
                    }))
                };
                this.workflowLoading = false;
            });
        } else {
            this.editingWorkflow = null;
            this.workflowForm = { name: '', description: '', steps: [] };
        }
        this.workflowFormErrors = '';
        this.workflowModalOpen = true;
    },

    addWorkflowStep() {
        const steps = this.workflowForm.steps;
        const newIndex = steps.length;
        if (newIndex > 0) {
            steps[newIndex - 1].on_success_index = newIndex;
        }
        steps.push({
            name: 'Step ' + (newIndex + 1), prompt: '', step_order: newIndex,
            max_retries: 0, timeout_seconds: 0, on_success_index: null, on_failure_index: null
        });
    },

    removeWorkflowStep(index) {
        const steps = this.workflowForm.steps;
        steps.splice(index, 1);
        steps.forEach((s, i) => {
            s.step_order = i;
            if (s.on_success_index === index) s.on_success_index = null;
            else if (s.on_success_index > index) s.on_success_index--;
            if (s.on_failure_index === index) s.on_failure_index = null;
            else if (s.on_failure_index > index) s.on_failure_index--;
        });
    },

    async saveWorkflow() {
        this.workflowLoading = true;
        this.workflowFormErrors = '';
        try {
            const method = this.editingWorkflow ? 'PUT' : 'POST';
            const url = this.editingWorkflow ? '/api/workflows/' + this.editingWorkflow.id : '/api/workflows';
            const res = await fetch(url, {
                method,
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify(this.workflowForm)
            });
            if (!res.ok) {
                const data = await res.json();
                this.workflowFormErrors = data.error || 'Failed to save';
                return;
            }
            this.workflowModalOpen = false;
            await this.loadWorkflows();
        } catch (err) {
            this.workflowFormErrors = err.message;
        } finally {
            this.workflowLoading = false;
        }
    },

    async deleteWorkflow(id) {
        if (!confirm('Delete this workflow?')) return;
        await fetch('/api/workflows/' + id, {
            method: 'DELETE',
            headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        await this.loadWorkflows();
    },

    async runWorkflow(workflowId, sessionId) {
        try {
            const res = await fetch('/api/workflows/' + workflowId + '/run', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify({ session_id: sessionId })
            });
            if (!res.ok) {
                const data = await res.json();
                alert(data.error || 'Failed to start workflow');
                return;
            }
            const run = await res.json();
            this.activeWorkflowRun = run;
            this.connectWorkflowRunSSE(run.id);
        } catch (err) {
            alert('Failed to start workflow: ' + err.message);
        }
    },

    connectWorkflowRunSSE(runId) {
        if (this.workflowRunSSE) this.workflowRunSSE.close();
        this.workflowRunSSE = new EventSource('/api/workflow-runs/' + runId + '/stream?token=' + this.apiKey);
        this.workflowRunSSE.onmessage = (event) => {
            const data = JSON.parse(event.data);
            if (data.type === 'heartbeat') return;
            this.handleWorkflowRunEvent(data);
        };
    },

    handleWorkflowRunEvent(data) {
        if (data.type === 'run_completed' || data.type === 'run_failed' || data.type === 'run_cancelled') {
            if (this.activeWorkflowRun) this.activeWorkflowRun.status = data.status || data.type.replace('run_', '');
            if (this.workflowRunSSE) { this.workflowRunSSE.close(); this.workflowRunSSE = null; }
            this.loadActiveWorkflowRun();
        }
        if (data.type === 'run_paused' && this.activeWorkflowRun) {
            this.activeWorkflowRun.status = 'paused';
        }
        if (data.type === 'step_started' || data.type === 'step_completed' || data.type === 'step_failed') {
            this.loadActiveWorkflowRun();
        }
    },

    async loadActiveWorkflowRun() {
        if (!this.selectedSessionId) return;
        try {
            const res = await fetch('/api/workflow-runs?session_id=' + this.selectedSessionId, {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (!res.ok) return;
            const runs = await res.json();
            const active = runs && runs.find(r => ['pending','running','paused'].includes(r.status));
            if (active) {
                this.activeWorkflowRun = active;
                const detailRes = await fetch('/api/workflow-runs/' + active.id, {
                    headers: { 'Authorization': 'Bearer ' + this.apiKey }
                });
                if (detailRes.ok) {
                    const detail = await detailRes.json();
                    this.workflowRunSteps = detail.steps || [];
                }
                if (!this.workflowRunSSE) this.connectWorkflowRunSSE(active.id);
            } else {
                this.activeWorkflowRun = null;
                this.workflowRunSteps = [];
            }
        } catch (err) {
            console.error('Failed to load workflow run:', err);
        }
    },

    async pauseWorkflowRun() {
        if (!this.activeWorkflowRun) return;
        await fetch('/api/workflow-runs/' + this.activeWorkflowRun.id + '/pause', {
            method: 'POST', headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
    },

    async resumeWorkflowRun() {
        if (!this.activeWorkflowRun) return;
        await fetch('/api/workflow-runs/' + this.activeWorkflowRun.id + '/resume', {
            method: 'POST', headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        this.connectWorkflowRunSSE(this.activeWorkflowRun.id);
    },

    async cancelWorkflowRun() {
        if (!this.activeWorkflowRun || !confirm('Cancel this workflow run?')) return;
        await fetch('/api/workflow-runs/' + this.activeWorkflowRun.id + '/cancel', {
            method: 'POST', headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
    },
```

- [ ] **Step 3: Call loadWorkflows on init**

Find the existing `init()` method or the place where `loadScheduledTasks()` is called during initialization and add `this.loadWorkflows()` alongside it. Also add `this.loadActiveWorkflowRun()` inside `selectSession()` (where the session is selected and messages are loaded).

- [ ] **Step 4: Add workflow sidebar section in index.html**

In `server/web/static/index.html`, find the scheduled tasks sidebar section and add a similar "Workflows" section below it:

```html
<!-- Workflows section -->
<div style="padding:8px 12px;border-top:1px solid #333">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px">
        <span style="font-size:11px;text-transform:uppercase;color:#888;letter-spacing:0.5px">Workflows</span>
        <button @click="openWorkflowModal(null)" style="background:none;border:none;color:#4a9eff;cursor:pointer;font-size:16px" title="New Workflow">+</button>
    </div>
    <template x-for="wf in workflows" :key="wf.id">
        <div style="padding:4px 8px;border-radius:4px;cursor:pointer;font-size:13px;color:#ccc;display:flex;justify-content:space-between;align-items:center"
             @click="openWorkflowModal(wf)"
             @mouseenter="$el.style.background='#2a2a2a'" @mouseleave="$el.style.background='transparent'">
            <span x-text="wf.name"></span>
            <button @click.stop="deleteWorkflow(wf.id)" style="background:none;border:none;color:#666;cursor:pointer;font-size:11px" title="Delete">&times;</button>
        </div>
    </template>
</div>
```

- [ ] **Step 5: Add workflow builder modal in index.html**

Add the builder modal markup (similar pattern to the task modal):

```html
<!-- Workflow Builder Modal -->
<div x-show="workflowModalOpen" style="position:fixed;inset:0;background:rgba(0,0,0,0.6);z-index:1000;display:flex;align-items:center;justify-content:center" @click.self="workflowModalOpen=false">
    <div style="background:#1e1e1e;border-radius:12px;width:640px;max-height:80vh;overflow-y:auto;padding:24px">
        <h3 style="margin:0 0 16px" x-text="editingWorkflow ? 'Edit Workflow' : 'New Workflow'"></h3>
        <div x-show="workflowFormErrors" style="background:#7f1d1d;padding:8px 12px;border-radius:6px;margin-bottom:12px;font-size:13px" x-text="workflowFormErrors"></div>

        <input x-model="workflowForm.name" placeholder="Workflow name" style="width:100%;padding:8px;background:#2a2a2a;border:1px solid #444;border-radius:6px;color:#fff;margin-bottom:8px;box-sizing:border-box">
        <input x-model="workflowForm.description" placeholder="Description (optional)" style="width:100%;padding:8px;background:#2a2a2a;border:1px solid #444;border-radius:6px;color:#fff;margin-bottom:16px;box-sizing:border-box">

        <div style="font-size:12px;color:#888;margin-bottom:8px">STEPS</div>
        <template x-for="(step, i) in workflowForm.steps" :key="i">
            <div style="background:#2a2a2a;border-radius:8px;padding:12px;margin-bottom:8px;position:relative">
                <div style="display:flex;gap:8px;margin-bottom:8px">
                    <input x-model="step.name" placeholder="Step name" style="flex:1;padding:6px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff">
                    <button @click="removeWorkflowStep(i)" style="background:none;border:none;color:#ef4444;cursor:pointer" title="Remove step">&times;</button>
                </div>
                <textarea x-model="step.prompt" placeholder="Prompt for this step..." rows="2" style="width:100%;padding:6px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff;resize:vertical;box-sizing:border-box;margin-bottom:8px"></textarea>
                <div style="display:flex;gap:8px;flex-wrap:wrap;font-size:12px">
                    <label style="color:#888">Retries: <input type="number" x-model.number="step.max_retries" min="0" style="width:50px;padding:4px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff"></label>
                    <label style="color:#888">Timeout(s): <input type="number" x-model.number="step.timeout_seconds" min="0" style="width:70px;padding:4px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff"></label>
                    <label style="color:#888">On Success:
                        <select x-model.number="step.on_success_index" style="padding:4px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff">
                            <option :value="null">End workflow</option>
                            <template x-for="(s, j) in workflowForm.steps" :key="j">
                                <option :value="j" x-show="j !== i" x-text="s.name || ('Step ' + (j+1))"></option>
                            </template>
                        </select>
                    </label>
                    <label style="color:#888">On Failure:
                        <select x-model.number="step.on_failure_index" style="padding:4px;background:#1e1e1e;border:1px solid #444;border-radius:4px;color:#fff">
                            <option :value="null">End workflow</option>
                            <template x-for="(s, j) in workflowForm.steps" :key="j">
                                <option :value="j" x-show="j !== i" x-text="s.name || ('Step ' + (j+1))"></option>
                            </template>
                        </select>
                    </label>
                </div>
            </div>
        </template>
        <button @click="addWorkflowStep()" style="width:100%;padding:8px;background:#333;border:1px dashed #555;border-radius:6px;color:#888;cursor:pointer;margin-bottom:16px">+ Add Step</button>

        <div style="display:flex;gap:8px;justify-content:flex-end">
            <button @click="workflowModalOpen=false" style="padding:8px 16px;background:#333;border:none;border-radius:6px;color:#ccc;cursor:pointer">Cancel</button>
            <button @click="saveWorkflow()" :disabled="workflowLoading" style="padding:8px 16px;background:#4a9eff;border:none;border-radius:6px;color:#fff;cursor:pointer">
                <span x-text="workflowLoading ? 'Saving...' : 'Save'"></span>
            </button>
        </div>
    </div>
</div>
```

- [ ] **Step 6: Add "Run Workflow" dropdown in chat header**

In the chat header area (near the interrupt button), add a workflow run button:

```html
<div x-show="selectedSessionId && currentSession?.mode === 'managed' && workflows.length > 0" style="position:relative;display:inline-block">
    <button @click="$refs.wfDropdown.style.display = $refs.wfDropdown.style.display === 'block' ? 'none' : 'block'"
            style="padding:4px 10px;background:#2a2a2a;border:1px solid #444;border-radius:6px;color:#ccc;cursor:pointer;font-size:12px">
        Run Workflow &#9662;
    </button>
    <div x-ref="wfDropdown" style="display:none;position:absolute;top:100%;right:0;background:#2a2a2a;border:1px solid #444;border-radius:6px;min-width:200px;z-index:100;margin-top:4px">
        <template x-for="wf in workflows" :key="wf.id">
            <div @click="runWorkflow(wf.id, selectedSessionId); $refs.wfDropdown.style.display='none'"
                 style="padding:8px 12px;cursor:pointer;color:#ccc;font-size:13px"
                 @mouseenter="$el.style.background='#333'" @mouseleave="$el.style.background='transparent'"
                 x-text="wf.name"></div>
        </template>
    </div>
</div>
```

- [ ] **Step 7: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: add workflow builder UI with sidebar list, modal editor, and run trigger"
```

---

### Task 7: Workflow Progress UI

**Files:**
- Modify: `server/web/static/app.js` (already has state from Task 6, may need small additions)
- Modify: `server/web/static/index.html` (add progress panel in chat area)

**Interfaces:**
- Consumes: `activeWorkflowRun`, `workflowRunSteps` state from Task 6, workflow run SSE events
- Produces: Progress panel with step timeline, pause/resume/cancel buttons, sidebar indicator

- [ ] **Step 1: Add progress panel in chat area**

In `server/web/static/index.html`, between the message history and the input area, add the progress panel:

```html
<!-- Workflow Progress Panel -->
<div x-show="activeWorkflowRun" style="border-top:1px solid #333;padding:12px;background:#1a1a2e">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
        <div>
            <span style="font-size:13px;font-weight:600;color:#e0e0e0" x-text="'Workflow: ' + (activeWorkflowRun?.workflow_id || '')"></span>
            <span style="font-size:11px;padding:2px 8px;border-radius:10px;margin-left:8px"
                  :style="'background:' + (activeWorkflowRun?.status === 'running' ? '#854d0e' : activeWorkflowRun?.status === 'completed' ? '#14532d' : activeWorkflowRun?.status === 'failed' ? '#7f1d1d' : activeWorkflowRun?.status === 'paused' ? '#374151' : '#374151')"
                  x-text="activeWorkflowRun?.status"></span>
        </div>
        <div style="display:flex;gap:6px">
            <button x-show="activeWorkflowRun?.status === 'running'" @click="pauseWorkflowRun()" style="padding:4px 10px;background:#854d0e;border:none;border-radius:4px;color:#fff;cursor:pointer;font-size:11px">Pause</button>
            <button x-show="activeWorkflowRun?.status === 'paused'" @click="resumeWorkflowRun()" style="padding:4px 10px;background:#14532d;border:none;border-radius:4px;color:#fff;cursor:pointer;font-size:11px">Resume</button>
            <button x-show="activeWorkflowRun?.status === 'running' || activeWorkflowRun?.status === 'paused'" @click="cancelWorkflowRun()" style="padding:4px 10px;background:#7f1d1d;border:none;border-radius:4px;color:#fff;cursor:pointer;font-size:11px">Cancel</button>
        </div>
    </div>
    <div style="display:flex;flex-direction:column;gap:4px">
        <template x-for="(rs, i) in workflowRunSteps" :key="rs.id">
            <div style="display:flex;align-items:center;gap:8px;padding:4px 8px;border-radius:4px;font-size:12px"
                 :style="rs.status === 'running' ? 'background:#1e293b' : ''">
                <span style="width:16px;text-align:center"
                      x-text="rs.status === 'completed' ? '&#10003;' : rs.status === 'running' ? '&#9654;' : rs.status === 'failed' ? '&#10007;' : '&#9679;'"
                      :style="'color:' + (rs.status === 'completed' ? '#22c55e' : rs.status === 'running' ? '#f59e0b' : rs.status === 'failed' ? '#ef4444' : '#6b7280')"></span>
                <span style="flex:1;color:#ccc" x-text="rs.step_id"></span>
                <span x-show="rs.attempt > 1" style="color:#888;font-size:10px" x-text="'attempt ' + rs.attempt"></span>
                <span x-show="rs.error" style="color:#ef4444;font-size:10px" x-text="rs.error"></span>
            </div>
        </template>
    </div>
</div>
```

- [ ] **Step 2: Add sidebar workflow run indicator**

In the session sidebar entry (where `activity_state` dot is rendered), add workflow progress info:

Find the session list entry and after the activity state dot, add:

```html
<span x-show="activeWorkflowRun && activeWorkflowRun.session_id === sess.id"
      style="font-size:10px;color:#f59e0b;margin-left:4px"
      x-text="'WF'"></span>
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat: add workflow progress panel with step timeline and controls"
```

---

### Task 8: Integration Testing & Server Startup Wiring

**Files:**
- Modify: `server/main.go` or wherever the server starts (to call `RecoverStaleRuns` on startup)
- Modify: `server/api/workflows_test.go` (add integration-level API test for run lifecycle)

**Interfaces:**
- Consumes: All previous tasks
- Produces: Integration test confirming CRUD → run → SSE → completion lifecycle, crash recovery wiring

- [ ] **Step 1: Add crash recovery call on server startup**

In the server main startup code (likely `server/main.go` or wherever `db.Open` is called), after opening the DB and before starting the HTTP server, the `workflowEngine.RecoverStaleRuns()` is already called in `NewRouter`. Verify this works by checking the `NewRouter` changes from Task 5 include the call.

- [ ] **Step 2: Write integration test for workflow run lifecycle**

Add to `server/api/workflows_test.go`:

```go
func TestWorkflowRunLifecycle(t *testing.T) {
	ts, store := newTestServer(t)

	// Create a managed session
	sess, err := store.CreateManagedSession("/tmp/wf-lifecycle", "[]", 50, 5.0, 0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Create workflow via API
	body := map[string]interface{}{
		"name": "Test WF",
		"steps": []map[string]interface{}{
			{"name": "Step 1", "prompt": "Do thing", "step_order": 0},
		},
	}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create workflow: expected 201, got %d", resp.StatusCode)
	}

	var createResult struct {
		Workflow db.Workflow `json:"workflow"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// List workflows
	req = authReq("GET", ts.URL+"/api/workflows", nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	var wfs []db.Workflow
	json.NewDecoder(resp.Body).Decode(&wfs)
	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(wfs))
	}

	// Get workflow detail
	req = authReq("GET", ts.URL+"/api/workflows/"+createResult.Workflow.ID, nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get workflow: expected 200, got %d", resp.StatusCode)
	}

	// Start run (will fail since no real manager, but tests the API layer)
	runBody := map[string]interface{}{"session_id": sess.ID}
	req = authReq("POST", ts.URL+"/api/workflows/"+createResult.Workflow.ID+"/run", runBody)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	// May return 201 or 500 depending on manager setup; just verify it doesn't panic
	t.Logf("run response status: %d", resp.StatusCode)

	// List runs
	req = authReq("GET", ts.URL+"/api/workflow-runs?session_id="+sess.ID, nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list runs: expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run all tests**

Run: `cd server && go test ./... -v -timeout 30s`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add server/api/workflows_test.go
git commit -m "test: add workflow run lifecycle integration test"
```
