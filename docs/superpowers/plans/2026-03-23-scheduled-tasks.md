# Scheduled Tasks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a cron-style scheduled task system that runs shell commands or Claude prompts in the background, with a web UI for management and a task runs history panel.

**Architecture:** New `scheduler` package with a ticker goroutine checks for due tasks every 30s. Tasks stored in SQLite (`scheduled_tasks` + `task_runs` tables). CRUD API endpoints. Alpine.js UI with task list, create/edit modal, and runs panel. Uses `robfig/cron/v3` for cron parsing.

**Tech Stack:** Go, SQLite, Alpine.js, robfig/cron/v3

**Spec:** `docs/superpowers/specs/2026-03-23-scheduled-tasks-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `server/db/db.go` | Modify (lines 39-101) | Add migration for `scheduled_tasks` and `task_runs` tables |
| `server/db/scheduled_tasks.go` | Create | ScheduledTask + TaskRun structs, all CRUD + query methods |
| `server/db/scheduled_tasks_test.go` | Create | DB layer tests |
| `server/api/tasks.go` | Create | HTTP handlers for task CRUD, runs, and trigger |
| `server/api/tasks_test.go` | Create | API handler tests |
| `server/api/router.go` | Modify (lines 57-68) | Register task routes |
| `server/scheduler/scheduler.go` | Create | Scheduler tick loop, task execution, reconciliation |
| `server/scheduler/scheduler_test.go` | Create | Scheduler unit tests |
| `server/main.go` | Modify (lines 69-71) | Wire up scheduler |
| `server/web/static/app.js` | Modify | Add task state, fetch methods, CRUD helpers |
| `server/web/static/index.html` | Modify | Add task section in sidebar, create/edit modal, runs panel |
| `server/go.mod` | Modify | Add `robfig/cron/v3` dependency |

---

## Task 1: Add cron dependency

**Files:**
- Modify: `server/go.mod`

- [ ] **Step 1: Add robfig/cron/v3 dependency**

```bash
cd server && go get github.com/robfig/cron/v3
```

- [ ] **Step 2: Verify it installed**

```bash
cd server && grep robfig go.mod
```

Expected: line with `github.com/robfig/cron/v3`

- [ ] **Step 3: Commit**

```bash
git add server/go.mod server/go.sum
git commit -m "feat: add robfig/cron/v3 dependency for scheduled tasks"
```

---

## Task 2: Database schema — scheduled_tasks and task_runs tables

**Files:**
- Modify: `server/db/db.go:39-101` (add migrations to the `migrations` slice)

- [ ] **Step 1: Add scheduled_tasks migration**

In `server/db/db.go`, add these entries to the `migrations` slice (after line 100, before the closing `}`):

```go
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    task_type TEXT NOT NULL CHECK(task_type IN ('shell', 'claude')),
    command TEXT NOT NULL,
    working_directory TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_run_at DATETIME,
    next_run_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
		`CREATE TABLE IF NOT EXISTS task_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at DATETIME,
    exit_code INTEGER,
    output TEXT,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'success', 'failed'))
)`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_task_id ON task_runs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_started_at ON task_runs(started_at)`,
```

- [ ] **Step 2: Verify existing tests still pass**

```bash
cd server && go test ./db/ -v -count=1
```

Expected: all existing tests PASS

- [ ] **Step 3: Commit**

```bash
git add server/db/db.go
git commit -m "feat(db): add scheduled_tasks and task_runs table migrations"
```

---

## Task 3: DB layer — ScheduledTask and TaskRun structs + scan helpers

**Files:**
- Create: `server/db/scheduled_tasks.go`
- Create: `server/db/scheduled_tasks_test.go`

- [ ] **Step 1: Write the test for CreateScheduledTask and GetScheduledTaskByID**

Create `server/db/scheduled_tasks_test.go`:

```go
package db

import (
	"testing"
)

func TestCreateAndGetScheduledTask(t *testing.T) {
	store := newTestStore(t)

	sess, err := store.UpsertSession("mac1", "/project/a", "")
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	task, err := store.CreateScheduledTask(sess.ID, "Daily backup", "shell", "tar -czf backup.tar.gz .", "/tmp/project", "0 2 * * *")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.Name != "Daily backup" {
		t.Errorf("name: got %q, want %q", task.Name, "Daily backup")
	}
	if task.TaskType != "shell" {
		t.Errorf("task_type: got %q, want %q", task.TaskType, "shell")
	}
	if task.Command != "tar -czf backup.tar.gz ." {
		t.Errorf("command: got %q", task.Command)
	}
	if task.WorkingDirectory != "/tmp/project" {
		t.Errorf("working_directory: got %q", task.WorkingDirectory)
	}
	if task.CronExpression != "0 2 * * *" {
		t.Errorf("cron_expression: got %q", task.CronExpression)
	}
	if !task.Enabled {
		t.Error("expected enabled=true")
	}

	got, err := store.GetScheduledTaskByID(task.ID)
	if err != nil {
		t.Fatalf("GetScheduledTaskByID: %v", err)
	}
	if got.ID != task.ID {
		t.Errorf("id mismatch: got %q, want %q", got.ID, task.ID)
	}
}

func TestCreateScheduledTaskWithoutSession(t *testing.T) {
	store := newTestStore(t)

	task, err := store.CreateScheduledTask("", "Shell task", "shell", "echo hello", "/tmp", "*/5 * * * *")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.SessionID != nil {
		t.Errorf("expected nil session_id, got %v", task.SessionID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./db/ -v -run TestCreateAndGetScheduledTask -count=1
```

Expected: FAIL — `CreateScheduledTask` not defined

- [ ] **Step 3: Write the ScheduledTask struct and Create/Get methods**

Create `server/db/scheduled_tasks.go`:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ScheduledTask struct {
	ID               string     `json:"id"`
	SessionID        *string    `json:"session_id,omitempty"`
	Name             string     `json:"name"`
	TaskType         string     `json:"task_type"`
	Command          string     `json:"command"`
	WorkingDirectory string     `json:"working_directory"`
	CronExpression   string     `json:"cron_expression"`
	Enabled          bool       `json:"enabled"`
	LastRunAt        *time.Time `json:"last_run_at,omitempty"`
	NextRunAt        *time.Time `json:"next_run_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type TaskRun struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Output     string     `json:"output"`
	Status     string     `json:"status"`
}

const taskColumns = `id, session_id, name, task_type, command, working_directory, cron_expression, enabled, last_run_at, next_run_at, created_at, updated_at`

func scanTask(scanner interface{ Scan(...interface{}) error }) (ScheduledTask, error) {
	var t ScheduledTask
	var enabled int
	err := scanner.Scan(
		&t.ID, &t.SessionID, &t.Name, &t.TaskType, &t.Command,
		&t.WorkingDirectory, &t.CronExpression, &enabled,
		&t.LastRunAt, &t.NextRunAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return t, err
	}
	t.Enabled = enabled != 0
	return t, nil
}

const runColumns = `id, task_id, started_at, finished_at, exit_code, output, status`

func scanRun(scanner interface{ Scan(...interface{}) error }) (TaskRun, error) {
	var r TaskRun
	err := scanner.Scan(
		&r.ID, &r.TaskID, &r.StartedAt, &r.FinishedAt,
		&r.ExitCode, &r.Output, &r.Status,
	)
	return r, err
}

func (s *Store) CreateScheduledTask(sessionID, name, taskType, command, workingDir, cronExpr string) (*ScheduledTask, error) {
	id := uuid.New().String()
	var sessPtr *string
	if sessionID != "" {
		sessPtr = &sessionID
	}
	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, session_id, name, task_type, command, working_directory, cron_expression, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
	`, id, sessPtr, name, taskType, command, workingDir, cronExpr)
	if err != nil {
		return nil, fmt.Errorf("create scheduled task: %w", err)
	}
	return s.GetScheduledTaskByID(id)
}

func (s *Store) GetScheduledTaskByID(id string) (*ScheduledTask, error) {
	row := s.db.QueryRow("SELECT "+taskColumns+" FROM scheduled_tasks WHERE id = ?", id)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get scheduled task: %w", err)
	}
	return &task, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd server && go test ./db/ -v -run "TestCreate" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/scheduled_tasks.go server/db/scheduled_tasks_test.go
git commit -m "feat(db): add ScheduledTask struct with Create and Get methods"
```

---

## Task 4: DB layer — List, Update, Delete methods

**Files:**
- Modify: `server/db/scheduled_tasks.go`
- Modify: `server/db/scheduled_tasks_test.go`

- [ ] **Step 1: Write tests for List, Update, Delete**

Append to `server/db/scheduled_tasks_test.go`:

```go
func TestListScheduledTasks(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	store.CreateScheduledTask(sess.ID, "Task A", "shell", "echo a", "/tmp", "0 * * * *")
	store.CreateScheduledTask(sess.ID, "Task B", "claude", "summarize", "/tmp", "0 9 * * *")
	store.CreateScheduledTask("", "Task C", "shell", "echo c", "/tmp", "*/5 * * * *")

	// List all
	all, err := store.ListScheduledTasks("")
	if err != nil {
		t.Fatalf("ListScheduledTasks all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(all))
	}

	// List by session
	bySession, err := store.ListScheduledTasks(sess.ID)
	if err != nil {
		t.Fatalf("ListScheduledTasks session: %v", err)
	}
	if len(bySession) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(bySession))
	}
}

func TestUpdateScheduledTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Old Name", "shell", "echo old", "/tmp", "0 * * * *")

	err := store.UpdateScheduledTask(task.ID, "New Name", "shell", "echo new", "/tmp/new", "0 2 * * *", false)
	if err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}

	got, _ := store.GetScheduledTaskByID(task.ID)
	if got.Name != "New Name" {
		t.Errorf("name: got %q, want %q", got.Name, "New Name")
	}
	if got.Command != "echo new" {
		t.Errorf("command: got %q", got.Command)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDeleteScheduledTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "To Delete", "shell", "echo x", "/tmp", "0 * * * *")

	err := store.DeleteScheduledTask(task.ID)
	if err != nil {
		t.Fatalf("DeleteScheduledTask: %v", err)
	}

	got, _ := store.GetScheduledTaskByID(task.ID)
	if got != nil {
		t.Error("expected nil after delete")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./db/ -v -run "TestList|TestUpdate|TestDelete" -count=1
```

Expected: FAIL

- [ ] **Step 3: Implement List, Update, Delete methods**

Append to `server/db/scheduled_tasks.go`:

```go
func (s *Store) ListScheduledTasks(sessionID string) ([]ScheduledTask, error) {
	query := "SELECT " + taskColumns + " FROM scheduled_tasks WHERE 1=1"
	var args []interface{}

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scheduled task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateScheduledTask(id, name, taskType, command, workingDir, cronExpr string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`
		UPDATE scheduled_tasks
		SET name = ?, task_type = ?, command = ?, working_directory = ?, cron_expression = ?, enabled = ?, updated_at = datetime('now')
		WHERE id = ?
	`, name, taskType, command, workingDir, cronExpr, enabledInt, id)
	if err != nil {
		return fmt.Errorf("update scheduled task: %w", err)
	}
	return nil
}

func (s *Store) DeleteScheduledTask(id string) error {
	_, err := s.db.Exec("DELETE FROM scheduled_tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete scheduled task: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && go test ./db/ -v -run "TestList|TestUpdate|TestDelete" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/scheduled_tasks.go server/db/scheduled_tasks_test.go
git commit -m "feat(db): add List, Update, Delete methods for scheduled tasks"
```

---

## Task 5: DB layer — TaskRun methods + scheduler query helpers

**Files:**
- Modify: `server/db/scheduled_tasks.go`
- Modify: `server/db/scheduled_tasks_test.go`

- [ ] **Step 1: Write tests for TaskRun CRUD and GetTasksDueForExecution**

Append to `server/db/scheduled_tasks_test.go`:

```go
import "time"  // add to imports at top of file

func TestTaskRunLifecycle(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *")

	// Create run
	run, err := store.CreateTaskRun(task.ID)
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if run.Status != "running" {
		t.Errorf("status: got %q, want %q", run.Status, "running")
	}

	// Complete run
	err = store.CompleteTaskRun(run.ID, 0, "hello\n")
	if err != nil {
		t.Fatalf("CompleteTaskRun: %v", err)
	}

	got, err := store.GetTaskRunByID(run.ID)
	if err != nil {
		t.Fatalf("GetTaskRunByID: %v", err)
	}
	if got.Status != "success" {
		t.Errorf("status: got %q, want %q", got.Status, "success")
	}
	if *got.ExitCode != 0 {
		t.Errorf("exit_code: got %d, want 0", *got.ExitCode)
	}

	// List runs
	runs, err := store.ListTaskRuns(task.ID, 20)
	if err != nil {
		t.Fatalf("ListTaskRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}
}

func TestCompleteTaskRunFailed(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "exit 1", "/tmp", "0 * * * *")

	run, _ := store.CreateTaskRun(task.ID)
	err := store.CompleteTaskRun(run.ID, 1, "error output")
	if err != nil {
		t.Fatalf("CompleteTaskRun: %v", err)
	}

	got, _ := store.GetTaskRunByID(run.ID)
	if got.Status != "failed" {
		t.Errorf("status: got %q, want %q", got.Status, "failed")
	}
}

func TestGetTasksDueForExecution(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	task1, _ := store.CreateScheduledTask(sess.ID, "Due", "shell", "echo a", "/tmp", "0 * * * *")
	task2, _ := store.CreateScheduledTask(sess.ID, "Not Due", "shell", "echo b", "/tmp", "0 * * * *")

	past := time.Now().Add(-1 * time.Minute)
	future := time.Now().Add(1 * time.Hour)
	store.UpdateTaskNextRun(task1.ID, past)
	store.UpdateTaskNextRun(task2.ID, future)

	due, err := store.GetTasksDueForExecution(time.Now())
	if err != nil {
		t.Fatalf("GetTasksDueForExecution: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due task, got %d", len(due))
	}
	if due[0].ID != task1.ID {
		t.Errorf("wrong task: got %q, want %q", due[0].ID, task1.ID)
	}
}

func TestCascadeDeleteTaskRuns(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *")
	store.CreateTaskRun(task.ID)

	store.DeleteScheduledTask(task.ID)
	runs, _ := store.ListTaskRuns(task.ID, 20)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after cascade delete, got %d", len(runs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./db/ -v -run "TestTaskRun|TestGetTasksDue|TestCascade" -count=1
```

Expected: FAIL

- [ ] **Step 3: Implement TaskRun methods and scheduler helpers**

Append to `server/db/scheduled_tasks.go`:

```go
func (s *Store) CreateTaskRun(taskID string) (*TaskRun, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO task_runs (id, task_id, started_at, status)
		VALUES (?, ?, datetime('now'), 'running')
	`, id, taskID)
	if err != nil {
		return nil, fmt.Errorf("create task run: %w", err)
	}
	return s.GetTaskRunByID(id)
}

func (s *Store) CompleteTaskRun(id string, exitCode int, output string) error {
	status := "success"
	if exitCode != 0 {
		status = "failed"
	}
	_, err := s.db.Exec(`
		UPDATE task_runs
		SET finished_at = datetime('now'), exit_code = ?, output = ?, status = ?
		WHERE id = ?
	`, exitCode, output, status, id)
	if err != nil {
		return fmt.Errorf("complete task run: %w", err)
	}
	return nil
}

func (s *Store) CompleteTaskRunWithError(id string, errMsg string) error {
	_, err := s.db.Exec(`
		UPDATE task_runs
		SET finished_at = datetime('now'), output = ?, status = 'failed'
		WHERE id = ?
	`, errMsg, id)
	if err != nil {
		return fmt.Errorf("complete task run with error: %w", err)
	}
	return nil
}

func (s *Store) GetTaskRunByID(id string) (*TaskRun, error) {
	row := s.db.QueryRow("SELECT "+runColumns+" FROM task_runs WHERE id = ?", id)
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task run: %w", err)
	}
	return &run, nil
}

func (s *Store) ListTaskRuns(taskID string, limit int) ([]TaskRun, error) {
	rows, err := s.db.Query(
		"SELECT "+runColumns+" FROM task_runs WHERE task_id = ? ORDER BY started_at DESC LIMIT ?",
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list task runs: %w", err)
	}
	defer rows.Close()

	var runs []TaskRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) UpdateTaskNextRun(id string, nextRunAt time.Time) error {
	_, err := s.db.Exec("UPDATE scheduled_tasks SET next_run_at = ? WHERE id = ?", nextRunAt, id)
	if err != nil {
		return fmt.Errorf("update task next run: %w", err)
	}
	return nil
}

func (s *Store) UpdateTaskLastRun(id string, lastRunAt time.Time) error {
	_, err := s.db.Exec("UPDATE scheduled_tasks SET last_run_at = ? WHERE id = ?", lastRunAt, id)
	if err != nil {
		return fmt.Errorf("update task last run: %w", err)
	}
	return nil
}

func (s *Store) GetTasksDueForExecution(now time.Time) ([]ScheduledTask, error) {
	rows, err := s.db.Query(
		"SELECT "+taskColumns+" FROM scheduled_tasks WHERE next_run_at <= ? AND enabled = 1 ORDER BY next_run_at ASC",
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("get tasks due: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan due task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) MarkStaleRunsFailed() error {
	_, err := s.db.Exec(`
		UPDATE task_runs SET status = 'failed', finished_at = datetime('now'), output = 'Server restarted while task was running'
		WHERE status = 'running'
	`)
	if err != nil {
		return fmt.Errorf("mark stale runs failed: %w", err)
	}
	return nil
}

func (s *Store) CleanupOldRuns(taskID string, keepCount int) error {
	_, err := s.db.Exec(`
		DELETE FROM task_runs WHERE task_id = ? AND id NOT IN (
			SELECT id FROM task_runs WHERE task_id = ? ORDER BY started_at DESC LIMIT ?
		)
	`, taskID, taskID, keepCount)
	if err != nil {
		return fmt.Errorf("cleanup old runs: %w", err)
	}
	return nil
}

func (s *Store) GetEnabledTasks() ([]ScheduledTask, error) {
	rows, err := s.db.Query(
		"SELECT "+taskColumns+" FROM scheduled_tasks WHERE enabled = 1",
	)
	if err != nil {
		return nil, fmt.Errorf("get enabled tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan enabled task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
```

- [ ] **Step 4: Run all DB tests**

```bash
cd server && go test ./db/ -v -count=1
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/scheduled_tasks.go server/db/scheduled_tasks_test.go
git commit -m "feat(db): add TaskRun methods and scheduler query helpers"
```

---

## Task 6: API handlers — task CRUD endpoints

**Files:**
- Create: `server/api/tasks.go`
- Create: `server/api/tasks_test.go`
- Modify: `server/api/router.go:57-68`

- [ ] **Step 1: Write tests for create and list task API endpoints**

Create `server/api/tasks_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestCreateTaskAPI(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	body := map[string]interface{}{
		"session_id":        sess.ID,
		"name":              "Daily backup",
		"task_type":         "shell",
		"command":           "tar -czf backup.tar.gz .",
		"working_directory": "/tmp/project",
		"cron_expression":   "0 2 * * *",
	}

	req := authReq("POST", ts.URL+"/api/tasks", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var task db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&task)
	if task.Name != "Daily backup" {
		t.Errorf("name: got %q", task.Name)
	}
	if task.TaskType != "shell" {
		t.Errorf("task_type: got %q", task.TaskType)
	}
}

func TestCreateTaskInvalidCron(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name":              "Bad cron",
		"task_type":         "shell",
		"command":           "echo hi",
		"working_directory": "/tmp",
		"cron_expression":   "not a cron",
	}

	req := authReq("POST", ts.URL+"/api/tasks", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestListTasksAPI(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	store.CreateScheduledTask(sess.ID, "A", "shell", "echo a", "/tmp", "0 * * * *")
	store.CreateScheduledTask(sess.ID, "B", "shell", "echo b", "/tmp", "0 * * * *")

	req := authReq("GET", ts.URL+"/api/tasks", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var tasks []db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&tasks)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./api/ -v -run "TestCreateTask|TestListTasks" -count=1
```

Expected: FAIL

- [ ] **Step 3: Create the tasks handler file and register routes**

Create `server/api/tasks.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type createTaskRequest struct {
	SessionID        string `json:"session_id"`
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
}

type updateTaskRequest struct {
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
	Enabled          bool   `json:"enabled"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Command == "" || req.WorkingDirectory == "" {
		http.Error(w, `{"error":"name, command, and working_directory are required"}`, http.StatusBadRequest)
		return
	}
	if req.TaskType != "shell" && req.TaskType != "claude" {
		http.Error(w, `{"error":"task_type must be 'shell' or 'claude'"}`, http.StatusBadRequest)
		return
	}

	sched, err := cronParser.Parse(req.CronExpression)
	if err != nil {
		http.Error(w, `{"error":"invalid cron expression"}`, http.StatusBadRequest)
		return
	}

	task, err := s.store.CreateScheduledTask(req.SessionID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	// Set initial next_run_at
	nextRun := sched.Next(time.Now())
	s.store.UpdateTaskNextRun(task.ID, nextRun)
	task.NextRunAt = &nextRun

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	tasks, err := s.store.ListScheduledTasks(sessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []db.ScheduledTask{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Command == "" || req.WorkingDirectory == "" {
		http.Error(w, `{"error":"name, command, and working_directory are required"}`, http.StatusBadRequest)
		return
	}
	if req.TaskType != "shell" && req.TaskType != "claude" {
		http.Error(w, `{"error":"task_type must be 'shell' or 'claude'"}`, http.StatusBadRequest)
		return
	}

	sched, err := cronParser.Parse(req.CronExpression)
	if err != nil {
		http.Error(w, `{"error":"invalid cron expression"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateScheduledTask(taskID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression, req.Enabled); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	// Recompute next_run_at
	nextRun := sched.Next(time.Now())
	s.store.UpdateTaskNextRun(taskID, nextRun)

	task, _ := s.store.GetScheduledTaskByID(taskID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if err := s.store.DeleteScheduledTask(taskID); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListTaskRuns(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	runs, err := s.store.ListTaskRuns(taskID, 20)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.TaskRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleGetTaskRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")
	run, err := s.store.GetTaskRunByID(runID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (s *Server) handleTriggerTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil || task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	// Trigger is handled by the scheduler — we just need to set next_run_at to now
	s.store.UpdateTaskNextRun(taskID, time.Now().Add(-1*time.Second))

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true,"message":"task queued for immediate execution"}`))
}
```

- [ ] **Step 4: Register routes in router.go**

In `server/api/router.go`, add after line 67 (after GitHub endpoints, before `rl := NewRateLimiter`):

```go
	// Scheduled task endpoints
	apiMux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	apiMux.HandleFunc("GET /api/tasks", s.handleListTasks)
	apiMux.HandleFunc("GET /api/tasks/{taskId}", s.handleGetTask)
	apiMux.HandleFunc("PUT /api/tasks/{taskId}", s.handleUpdateTask)
	apiMux.HandleFunc("DELETE /api/tasks/{taskId}", s.handleDeleteTask)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs", s.handleListTaskRuns)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs/{runId}", s.handleGetTaskRun)
	apiMux.HandleFunc("POST /api/tasks/{taskId}/trigger", s.handleTriggerTask)
```

- [ ] **Step 5: Run API tests**

```bash
cd server && go test ./api/ -v -run "TestCreateTask|TestListTasks" -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/tasks.go server/api/tasks_test.go server/api/router.go
git commit -m "feat(api): add scheduled task CRUD and runs endpoints"
```

---

## Task 7: Scheduler package — tick loop and task execution

**Files:**
- Create: `server/scheduler/scheduler.go`
- Create: `server/scheduler/scheduler_test.go`

- [ ] **Step 1: Write scheduler test**

Create `server/scheduler/scheduler_test.go`:

```go
package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSchedulerExecutesShellTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")

	task, _ := store.CreateScheduledTask(sess.ID, "Echo test", "shell", "echo hello-from-scheduler", "/tmp", "* * * * *")
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))

	s := New(store)
	s.checkAndExecuteTasks()

	// Wait a moment for the goroutine to finish
	time.Sleep(2 * time.Second)

	runs, err := store.ListTaskRuns(task.ID, 10)
	if err != nil {
		t.Fatalf("ListTaskRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "success" {
		t.Errorf("status: got %q, want %q", runs[0].Status, "success")
	}
	if runs[0].Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestSchedulerSkipsConcurrentExecution(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")

	// Create a task that takes a moment
	task, _ := store.CreateScheduledTask(sess.ID, "Slow task", "shell", "sleep 3", "/tmp", "* * * * *")
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))

	s := New(store)
	s.checkAndExecuteTasks()
	time.Sleep(100 * time.Millisecond) // let goroutine start

	// Set next_run again and check — should skip
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))
	s.checkAndExecuteTasks()

	time.Sleep(100 * time.Millisecond)
	runs, _ := store.ListTaskRuns(task.ID, 10)
	if len(runs) != 1 {
		t.Errorf("expected 1 run (concurrent execution should be skipped), got %d", len(runs))
	}
}

func TestSchedulerReconciliation(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")

	task, _ := store.CreateScheduledTask(sess.ID, "Reconcile", "shell", "echo reconciled", "/tmp", "* * * * *")
	// Set next_run to 2 minutes ago (within 5-minute window)
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-2*time.Minute))

	s := New(store)
	s.Reconcile()

	time.Sleep(2 * time.Second)

	runs, _ := store.ListTaskRuns(task.ID, 10)
	if len(runs) != 1 {
		t.Errorf("expected reconciliation to trigger 1 run, got %d", len(runs))
	}
}

func TestReconcileStaleRuns(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "* * * * *")
	run, _ := store.CreateTaskRun(task.ID)

	// Run is in "running" state — simulate server crash
	s := New(store)
	s.Reconcile()

	got, _ := store.GetTaskRunByID(run.ID)
	if got.Status != "failed" {
		t.Errorf("stale run should be marked failed, got %q", got.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./scheduler/ -v -count=1
```

Expected: FAIL — package doesn't exist

- [ ] **Step 3: Create the scheduler package**

Create directory and file `server/scheduler/scheduler.go`:

```go
package scheduler

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/robfig/cron/v3"
)

const (
	tickInterval       = 30 * time.Second
	executionTimeout   = 1 * time.Hour
	missedTaskWindow   = 5 * time.Minute
	maxOutputBytes     = 10 * 1024 // 10KB
	keepRunsPerTask    = 50
	shutdownTimeout    = 30 * time.Second
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type Scheduler struct {
	store   *db.Store
	done    chan struct{}
	wg      sync.WaitGroup
	running sync.Map // map[taskID]bool
}

func New(store *db.Store) *Scheduler {
	return &Scheduler{
		store: store,
		done:  make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.run()
}

func (s *Scheduler) Stop() {
	close(s.done)

	// Wait for in-flight executions with timeout
	ch := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(ch)
	}()

	select {
	case <-ch:
	case <-time.After(shutdownTimeout):
		log.Println("scheduler: shutdown timeout, some tasks may still be running")
	}
}

func (s *Scheduler) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.checkAndExecuteTasks()
		}
	}
}

func (s *Scheduler) Reconcile() {
	// Mark stale runs as failed
	if err := s.store.MarkStaleRunsFailed(); err != nil {
		log.Printf("scheduler: failed to mark stale runs: %v", err)
	}

	tasks, err := s.store.GetEnabledTasks()
	if err != nil {
		log.Printf("scheduler: reconciliation failed: %v", err)
		return
	}

	now := time.Now()
	for _, task := range tasks {
		sched, err := cronParser.Parse(task.CronExpression)
		if err != nil {
			log.Printf("scheduler: invalid cron for task %q: %v", task.Name, err)
			continue
		}

		// Check if task was missed within the window
		if task.NextRunAt != nil && task.NextRunAt.Before(now) && now.Sub(*task.NextRunAt) <= missedTaskWindow {
			log.Printf("scheduler: running missed task %q (was due %v ago)", task.Name, now.Sub(*task.NextRunAt).Round(time.Second))
			s.spawnTask(task)
		} else if task.NextRunAt != nil && task.NextRunAt.Before(now) {
			log.Printf("scheduler: skipping stale task %q (missed by %v)", task.Name, now.Sub(*task.NextRunAt).Round(time.Second))
		}

		// Recompute next run
		nextRun := sched.Next(now)
		if err := s.store.UpdateTaskNextRun(task.ID, nextRun); err != nil {
			log.Printf("scheduler: failed to update next_run for task %q: %v", task.Name, err)
		}
	}
}

func (s *Scheduler) checkAndExecuteTasks() {
	tasks, err := s.store.GetTasksDueForExecution(time.Now())
	if err != nil {
		log.Printf("scheduler: failed to get due tasks: %v", err)
		return
	}

	for _, task := range tasks {
		// Skip if already running
		if _, loaded := s.running.LoadOrStore(task.ID, true); loaded {
			continue
		}

		// Synchronously update next_run_at before spawning
		sched, err := cronParser.Parse(task.CronExpression)
		if err != nil {
			log.Printf("scheduler: invalid cron for task %q: %v", task.Name, err)
			s.running.Delete(task.ID)
			continue
		}
		nextRun := sched.Next(time.Now())
		s.store.UpdateTaskNextRun(task.ID, nextRun)

		s.spawnTask(task)
	}
}

func (s *Scheduler) spawnTask(task db.ScheduledTask) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Delete(task.ID)
		s.executeTask(task)
	}()
}

func (s *Scheduler) executeTask(task db.ScheduledTask) {
	run, err := s.store.CreateTaskRun(task.ID)
	if err != nil {
		log.Printf("scheduler: failed to create run for task %q: %v", task.Name, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), executionTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch task.TaskType {
	case "shell":
		cmd = exec.CommandContext(ctx, "bash", "-c", task.Command)
	case "claude":
		cmd = exec.CommandContext(ctx, "claude", "-p", task.Command)
	default:
		s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("unknown task type: %s", task.TaskType))
		return
	}
	cmd.Dir = task.WorkingDirectory

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a start failure vs execution failure
		if cmd.ProcessState == nil {
			s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("failed to start: %v", err))
			s.store.UpdateTaskLastRun(task.ID, time.Now())
			return
		}
	}

	// Truncate output to last maxOutputBytes
	outputStr := string(output)
	if len(outputStr) > maxOutputBytes {
		outputStr = outputStr[len(outputStr)-maxOutputBytes:]
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	s.store.CompleteTaskRun(run.ID, exitCode, outputStr)
	s.store.UpdateTaskLastRun(task.ID, time.Now())
	s.store.CleanupOldRuns(task.ID, keepRunsPerTask)
}
```

- [ ] **Step 4: Run scheduler tests**

```bash
cd server && go test ./scheduler/ -v -count=1 -timeout 30s
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add server/scheduler/
git commit -m "feat: add scheduler package with tick loop, execution, and reconciliation"
```

---

## Task 8: Wire scheduler into main.go

**Files:**
- Modify: `server/main.go:57-71`

- [ ] **Step 1: Add scheduler import and initialization**

In `server/main.go`, add import:
```go
"github.com/jaychinthrajah/claude-controller/server/scheduler"
```

After line 59 (`store.ResetStaleActivityStates()` block), add:

```go
	sched := scheduler.New(store)
	sched.Reconcile()
	sched.Start()
	defer sched.Stop()
```

- [ ] **Step 2: Verify the server compiles**

```bash
cd server && go build -o /dev/null .
```

Expected: no errors

- [ ] **Step 3: Run all tests**

```bash
cd server && go test ./... -v -count=1 -timeout 60s
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/main.go
git commit -m "feat: wire scheduler into server startup with reconciliation"
```

---

## Task 9: Web UI — Alpine.js state and API methods

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add task state to Alpine.js data object**

In `server/web/static/app.js`, add to the data object (after the existing state variables):

```javascript
    // Scheduled tasks state
    scheduledTasks: [],
    selectedTask: null,
    taskRuns: [],
    taskModalOpen: false,
    editingTask: null,
    taskForm: { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '' },
    taskFormErrors: '',
    taskLoading: false,
    taskRunsLoading: false,
    tasksExpanded: true,
```

- [ ] **Step 2: Add task fetch/CRUD methods**

Add these methods to the app.js methods section:

```javascript
    async loadScheduledTasks() {
        try {
            const res = await fetch('/api/tasks', {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (res.ok) this.scheduledTasks = await res.json();
        } catch (err) {
            console.error('Failed to load tasks:', err);
        }
    },

    openTaskModal(task) {
        if (task) {
            this.editingTask = task;
            this.taskForm = {
                name: task.name,
                task_type: task.task_type,
                command: task.command,
                working_directory: task.working_directory,
                cron_expression: task.cron_expression,
                session_id: task.session_id || ''
            };
        } else {
            this.editingTask = null;
            this.taskForm = { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '' };
        }
        this.taskFormErrors = '';
        this.taskModalOpen = true;
    },

    async saveTask() {
        this.taskLoading = true;
        this.taskFormErrors = '';
        try {
            const method = this.editingTask ? 'PUT' : 'POST';
            const url = this.editingTask ? '/api/tasks/' + this.editingTask.id : '/api/tasks';
            const body = { ...this.taskForm };
            if (this.editingTask) body.enabled = this.editingTask.enabled;
            const res = await fetch(url, {
                method,
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify(body)
            });
            if (!res.ok) {
                const data = await res.json();
                this.taskFormErrors = data.error || 'Failed to save';
                return;
            }
            this.taskModalOpen = false;
            await this.loadScheduledTasks();
        } catch (err) {
            this.taskFormErrors = err.message;
        } finally {
            this.taskLoading = false;
        }
    },

    async deleteTask(taskId) {
        if (!confirm('Delete this scheduled task?')) return;
        try {
            await fetch('/api/tasks/' + taskId, {
                method: 'DELETE',
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (this.selectedTask && this.selectedTask.id === taskId) this.selectedTask = null;
            await this.loadScheduledTasks();
        } catch (err) {
            console.error('Failed to delete task:', err);
        }
    },

    async toggleTaskEnabled(task) {
        const body = {
            name: task.name,
            task_type: task.task_type,
            command: task.command,
            working_directory: task.working_directory,
            cron_expression: task.cron_expression,
            enabled: !task.enabled
        };
        try {
            await fetch('/api/tasks/' + task.id, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify(body)
            });
            await this.loadScheduledTasks();
        } catch (err) {
            console.error('Failed to toggle task:', err);
        }
    },

    async selectTask(task) {
        this.selectedTask = task;
        await this.loadTaskRuns(task.id);
    },

    async loadTaskRuns(taskId) {
        this.taskRunsLoading = true;
        try {
            const res = await fetch('/api/tasks/' + taskId + '/runs', {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (res.ok) this.taskRuns = await res.json();
        } catch (err) {
            console.error('Failed to load runs:', err);
        } finally {
            this.taskRunsLoading = false;
        }
    },

    async triggerTask(taskId) {
        try {
            await fetch('/api/tasks/' + taskId + '/trigger', {
                method: 'POST',
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            // Reload after a short delay to show the new run
            setTimeout(() => {
                this.loadScheduledTasks();
                if (this.selectedTask && this.selectedTask.id === taskId) this.loadTaskRuns(taskId);
            }, 2000);
        } catch (err) {
            console.error('Failed to trigger task:', err);
        }
    },

    formatCron(expr) {
        const presets = {
            '* * * * *': 'Every minute',
            '*/5 * * * *': 'Every 5 minutes',
            '*/15 * * * *': 'Every 15 minutes',
            '0 * * * *': 'Every hour',
            '0 */2 * * *': 'Every 2 hours',
            '0 0 * * *': 'Daily at midnight',
            '0 9 * * *': 'Daily at 9 AM',
            '0 9 * * 1-5': 'Weekdays at 9 AM',
            '0 0 * * 0': 'Weekly on Sunday',
            '0 0 1 * *': 'Monthly on the 1st',
        };
        return presets[expr] || expr;
    },

    formatRelativeTime(dateStr) {
        if (!dateStr) return 'never';
        const date = new Date(dateStr + 'Z');
        const now = new Date();
        const diff = now - date;
        const mins = Math.floor(diff / 60000);
        if (mins < 1) return 'just now';
        if (mins < 60) return mins + 'm ago';
        const hrs = Math.floor(mins / 60);
        if (hrs < 24) return hrs + 'h ago';
        const days = Math.floor(hrs / 24);
        return days + 'd ago';
    },
```

- [ ] **Step 3: Add task loading to init/polling**

Find the existing `init()` method and add `this.loadScheduledTasks()` call. Also add to any existing polling interval:

```javascript
// In the polling/refresh logic, add:
this.loadScheduledTasks();
```

- [ ] **Step 4: Verify the server compiles and runs**

```bash
cd server && go build -o /dev/null .
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(ui): add scheduled tasks Alpine.js state and API methods"
```

---

## Task 10: Web UI — HTML for task list, modal, and runs panel

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add Scheduled Tasks section to the sidebar**

In `server/web/static/index.html`, add after the session list section in the sidebar:

```html
<!-- Scheduled Tasks Section -->
<div x-show="authenticated && !leftCollapsed" style="border-top:1px solid var(--border); padding:8px;">
  <div style="display:flex; align-items:center; justify-content:space-between; cursor:pointer; padding:4px 0;" @click="tasksExpanded = !tasksExpanded">
    <span style="font-size:0.8rem; font-weight:600; color:var(--text-muted);">
      <span x-text="tasksExpanded ? '▼' : '▶'" style="font-size:0.65rem;"></span>
      Scheduled Tasks
    </span>
    <button class="btn btn-sm" @click.stop="openTaskModal()" style="font-size:0.7rem; padding:2px 6px;" title="New Task">+</button>
  </div>
  <div x-show="tasksExpanded" style="max-height:300px; overflow-y:auto;">
    <template x-if="scheduledTasks.length === 0">
      <div style="font-size:0.75rem; color:var(--text-muted); padding:8px 4px;">No scheduled tasks</div>
    </template>
    <template x-for="task in scheduledTasks" :key="task.id">
      <div class="session-item" :class="{ active: selectedTask && selectedTask.id === task.id }"
           @click="selectTask(task)" style="padding:6px 8px; font-size:0.8rem;">
        <div style="display:flex; align-items:center; gap:6px; width:100%;">
          <span style="width:8px; height:8px; border-radius:50%; flex-shrink:0;"
                :style="{ background: task.last_run_at ? (task.enabled ? 'var(--green, #22c55e)' : 'var(--text-muted)') : '#888' }"></span>
          <div style="flex:1; min-width:0;">
            <div style="display:flex; align-items:center; gap:4px;">
              <span style="font-weight:500; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="task.name"></span>
              <span style="font-size:0.65rem; padding:1px 4px; border-radius:3px; background:var(--bg-secondary, #333); color:var(--text-muted);"
                    x-text="task.task_type"></span>
            </div>
            <div style="font-size:0.7rem; color:var(--text-muted);" x-text="formatCron(task.cron_expression)"></div>
          </div>
          <div style="display:flex; gap:2px; flex-shrink:0;">
            <button @click.stop="triggerTask(task.id)" title="Run Now" style="background:none; border:none; cursor:pointer; font-size:0.75rem; color:var(--text-muted);">▶</button>
            <button @click.stop="openTaskModal(task)" title="Edit" style="background:none; border:none; cursor:pointer; font-size:0.75rem; color:var(--text-muted);">✎</button>
            <button @click.stop="deleteTask(task.id)" title="Delete" style="background:none; border:none; cursor:pointer; font-size:0.75rem; color:var(--text-muted);">×</button>
          </div>
        </div>
      </div>
    </template>
  </div>
</div>
```

- [ ] **Step 2: Add Task Create/Edit Modal**

Add after the existing modals in the HTML:

```html
<!-- Task Create/Edit Modal -->
<div x-show="taskModalOpen" x-cloak
     style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:200; display:flex; align-items:center; justify-content:center;"
     @click.self="taskModalOpen = false" @keydown.escape.window="taskModalOpen = false">
  <div style="background:var(--bg); border-radius:12px; padding:24px; width:500px; max-width:90vw; border:1px solid var(--border);">
    <h3 style="margin:0 0 16px 0; font-size:1rem;" x-text="editingTask ? 'Edit Scheduled Task' : 'New Scheduled Task'"></h3>
    <form @submit.prevent="saveTask()">
      <div style="margin-bottom:12px;">
        <label style="display:block; font-size:0.8rem; margin-bottom:4px; color:var(--text-muted);">Name</label>
        <input type="text" x-model="taskForm.name" required
               style="width:100%; padding:8px; background:var(--bg-secondary, #1a1a1a); color:var(--text); border:1px solid var(--border); border-radius:6px; box-sizing:border-box;"
               placeholder="e.g., Daily Backup">
      </div>
      <div style="margin-bottom:12px;">
        <label style="display:block; font-size:0.8rem; margin-bottom:4px; color:var(--text-muted);">Type</label>
        <select x-model="taskForm.task_type"
                style="width:100%; padding:8px; background:var(--bg-secondary, #1a1a1a); color:var(--text); border:1px solid var(--border); border-radius:6px;">
          <option value="shell">Shell Command</option>
          <option value="claude">Claude Command</option>
        </select>
      </div>
      <div style="margin-bottom:12px;">
        <label style="display:block; font-size:0.8rem; margin-bottom:4px; color:var(--text-muted);"
               x-text="taskForm.task_type === 'claude' ? 'Prompt' : 'Command'"></label>
        <textarea x-model="taskForm.command" required rows="3"
                  style="width:100%; padding:8px; background:var(--bg-secondary, #1a1a1a); color:var(--text); border:1px solid var(--border); border-radius:6px; font-family:monospace; font-size:0.85rem; resize:vertical; box-sizing:border-box;"
                  :placeholder="taskForm.task_type === 'claude' ? 'e.g., Summarize the latest changes' : 'e.g., tar -czf backup.tar.gz ./data'"></textarea>
      </div>
      <div style="margin-bottom:12px;">
        <label style="display:block; font-size:0.8rem; margin-bottom:4px; color:var(--text-muted);">Working Directory</label>
        <input type="text" x-model="taskForm.working_directory" required
               style="width:100%; padding:8px; background:var(--bg-secondary, #1a1a1a); color:var(--text); border:1px solid var(--border); border-radius:6px; font-family:monospace; font-size:0.85rem; box-sizing:border-box;"
               placeholder="/absolute/path/to/project">
      </div>
      <div style="margin-bottom:12px;">
        <label style="display:block; font-size:0.8rem; margin-bottom:4px; color:var(--text-muted);">Cron Expression</label>
        <input type="text" x-model="taskForm.cron_expression" required
               style="width:100%; padding:8px; background:var(--bg-secondary, #1a1a1a); color:var(--text); border:1px solid var(--border); border-radius:6px; font-family:monospace; font-size:0.85rem; box-sizing:border-box;"
               placeholder="0 9 * * *">
        <div style="font-size:0.7rem; color:var(--text-muted); margin-top:4px;">
          Format: minute hour day month weekday &nbsp;|&nbsp;
          <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 * * * *'">hourly</span> &nbsp;
          <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 9 * * *'">daily 9am</span> &nbsp;
          <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 9 * * 1-5'">weekdays</span> &nbsp;
          <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '*/5 * * * *'">every 5min</span>
        </div>
      </div>
      <div x-show="taskFormErrors" style="color:var(--red, #ef4444); font-size:0.8rem; margin-bottom:8px;" x-text="taskFormErrors"></div>
      <div style="display:flex; gap:8px; justify-content:flex-end;">
        <button type="button" class="btn btn-sm" @click="taskModalOpen = false">Cancel</button>
        <button type="submit" class="btn btn-sm btn-primary" :disabled="taskLoading">
          <span x-text="editingTask ? 'Update' : 'Create'"></span>
        </button>
      </div>
    </form>
  </div>
</div>
```

- [ ] **Step 3: Add Task Runs panel**

Add a runs panel that shows when a task is selected. This can go in the main content area or as a panel:

```html
<!-- Task Runs Panel — shows when a task is selected and no session chat is active -->
<div x-show="selectedTask && !selectedSessionId" style="flex:1; display:flex; flex-direction:column; overflow:hidden;">
  <div style="padding:12px 16px; border-bottom:1px solid var(--border); display:flex; align-items:center; justify-content:space-between;">
    <div>
      <h3 style="margin:0; font-size:1rem;" x-text="selectedTask?.name"></h3>
      <div style="font-size:0.8rem; color:var(--text-muted);">
        <span x-text="selectedTask?.task_type" style="text-transform:uppercase;"></span>
        &nbsp;·&nbsp;
        <span x-text="formatCron(selectedTask?.cron_expression || '')"></span>
        &nbsp;·&nbsp;
        <span x-text="selectedTask?.working_directory" style="font-family:monospace;"></span>
      </div>
    </div>
    <div style="display:flex; gap:8px;">
      <button class="btn btn-sm" @click="triggerTask(selectedTask.id)">Run Now</button>
      <button class="btn btn-sm" @click="openTaskModal(selectedTask)">Edit</button>
    </div>
  </div>
  <div style="padding:16px; overflow-y:auto; flex:1;">
    <h4 style="margin:0 0 12px 0; font-size:0.9rem; color:var(--text-muted);">Recent Runs</h4>
    <div x-show="taskRunsLoading" style="color:var(--text-muted); font-size:0.85rem;">Loading...</div>
    <div x-show="!taskRunsLoading && taskRuns.length === 0" style="color:var(--text-muted); font-size:0.85rem;">No runs yet</div>
    <template x-for="run in taskRuns" :key="run.id">
      <div style="border:1px solid var(--border); border-radius:8px; padding:12px; margin-bottom:8px;">
        <div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:6px;">
          <div style="display:flex; align-items:center; gap:8px;">
            <span style="width:8px; height:8px; border-radius:50;"
                  :style="{ background: run.status === 'success' ? 'var(--green, #22c55e)' : run.status === 'failed' ? 'var(--red, #ef4444)' : 'var(--yellow, #eab308)' }"></span>
            <span style="font-size:0.8rem; font-weight:500;" x-text="run.status"></span>
            <span style="font-size:0.75rem; color:var(--text-muted);" x-text="formatRelativeTime(run.started_at)"></span>
          </div>
          <div style="font-size:0.75rem; color:var(--text-muted);">
            <span x-show="run.exit_code !== null && run.exit_code !== undefined" x-text="'exit ' + run.exit_code"></span>
            <span x-show="run.finished_at && run.started_at" x-text="(() => { const d = (new Date(run.finished_at + 'Z') - new Date(run.started_at + 'Z')) / 1000; return d < 60 ? Math.round(d) + 's' : Math.round(d/60) + 'm'; })()"></span>
          </div>
        </div>
        <div x-show="run.output" style="background:var(--bg-secondary, #111); border-radius:4px; padding:8px; font-family:monospace; font-size:0.75rem; white-space:pre-wrap; word-break:break-all; max-height:200px; overflow-y:auto; color:var(--text-muted);"
             x-text="run.output?.substring(0, 500) + (run.output?.length > 500 ? '\n...(truncated)' : '')">
        </div>
      </div>
    </template>
  </div>
</div>
```

- [ ] **Step 4: Verify the server builds and the UI loads**

```bash
cd server && go build -o /dev/null .
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): add scheduled tasks list, create/edit modal, and runs panel"
```

---

## Task 11: Integration test — end-to-end task lifecycle

**Files:**
- Modify: `server/api/tasks_test.go`

- [ ] **Step 1: Write integration test covering full lifecycle**

Append to `server/api/tasks_test.go`:

```go
func TestTaskLifecycleAPI(t *testing.T) {
	ts, store := newTestServer(t)

	// Create
	body := map[string]interface{}{
		"name":              "Lifecycle test",
		"task_type":         "shell",
		"command":           "echo lifecycle",
		"working_directory": "/tmp",
		"cron_expression":   "0 * * * *",
	}
	req := authReq("POST", ts.URL+"/api/tasks", body)
	resp, _ := http.DefaultClient.Do(req)
	var task db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&task)
	resp.Body.Close()

	if task.ID == "" {
		t.Fatal("expected task ID")
	}

	// Get
	req = authReq("GET", ts.URL+"/api/tasks/"+task.ID, nil)
	resp, _ = http.DefaultClient.Do(req)
	var got db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Name != "Lifecycle test" {
		t.Errorf("get: name = %q", got.Name)
	}

	// Update
	updateBody := map[string]interface{}{
		"name":              "Updated name",
		"task_type":         "shell",
		"command":           "echo updated",
		"working_directory": "/tmp",
		"cron_expression":   "0 2 * * *",
		"enabled":           true,
	}
	req = authReq("PUT", ts.URL+"/api/tasks/"+task.ID, updateBody)
	resp, _ = http.DefaultClient.Do(req)
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Name != "Updated name" {
		t.Errorf("update: name = %q", got.Name)
	}

	// Trigger
	req = authReq("POST", ts.URL+"/api/tasks/"+task.ID+"/trigger", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("trigger: status = %d", resp.StatusCode)
	}

	// List runs (should be empty — trigger just sets next_run_at, scheduler hasn't run)
	req = authReq("GET", ts.URL+"/api/tasks/"+task.ID+"/runs", nil)
	resp, _ = http.DefaultClient.Do(req)
	var runs []db.TaskRun
	json.NewDecoder(resp.Body).Decode(&runs)
	resp.Body.Close()

	// Delete
	req = authReq("DELETE", ts.URL+"/api/tasks/"+task.ID, nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("delete: status = %d", resp.StatusCode)
	}

	// Verify deleted
	req = authReq("GET", ts.URL+"/api/tasks/"+task.ID, nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}

	_ = store // keep linter happy
}
```

- [ ] **Step 2: Run all tests**

```bash
cd server && go test ./... -v -count=1 -timeout 60s
```

Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/api/tasks_test.go
git commit -m "test: add end-to-end task lifecycle API test"
```

---

## Task 12: Final verification and cleanup

- [ ] **Step 1: Run the full test suite**

```bash
cd server && go test ./... -v -count=1 -timeout 60s
```

Expected: all PASS

- [ ] **Step 2: Build the server binary**

```bash
cd server && go build -o claude-controller .
```

Expected: binary created successfully

- [ ] **Step 3: Quick manual smoke test**

```bash
cd server && go run . &
sleep 2
# Test create task
curl -s -X POST http://localhost:8080/api/tasks \
  -H "Authorization: Bearer $(cat ~/.claude-controller/api.key)" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","task_type":"shell","command":"echo smoke-test","working_directory":"/tmp","cron_expression":"* * * * *"}' | head -c 200
echo
# Kill the server
kill %1
```

- [ ] **Step 4: Clean up build artifacts**

```bash
rm -f server/claude-controller
```

- [ ] **Step 5: Final commit if any cleanup needed**

Only if there were issues found and fixed during verification.
