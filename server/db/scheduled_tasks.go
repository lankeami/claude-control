package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// nullString converts a sql.NullString to a plain string (empty if NULL).
func nullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

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
	Model            string     `json:"model"`
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

const taskColumns = `id, session_id, name, task_type, command, working_directory, cron_expression, enabled, last_run_at, next_run_at, created_at, updated_at, COALESCE(model,'')`

func scanTask(scanner interface{ Scan(...interface{}) error }) (ScheduledTask, error) {
	var t ScheduledTask
	var enabled int
	err := scanner.Scan(
		&t.ID, &t.SessionID, &t.Name, &t.TaskType, &t.Command,
		&t.WorkingDirectory, &t.CronExpression, &enabled,
		&t.LastRunAt, &t.NextRunAt, &t.CreatedAt, &t.UpdatedAt, &t.Model,
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
	var output sql.NullString
	err := scanner.Scan(
		&r.ID, &r.TaskID, &r.StartedAt, &r.FinishedAt,
		&r.ExitCode, &output, &r.Status,
	)
	if err != nil {
		return r, err
	}
	r.Output = nullString(output)
	return r, nil
}

// CreateScheduledTask creates a new scheduled task. Pass empty string for sessionID to create a session-less task.
func (s *Store) CreateScheduledTask(sessionID, name, taskType, command, workingDir, cronExpr, model string) (*ScheduledTask, error) {
	id := uuid.New().String()
	var sessPtr *string
	if sessionID != "" {
		sessPtr = &sessionID
	}
	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, session_id, name, task_type, command, working_directory, cron_expression, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
	`, id, sessPtr, name, taskType, command, workingDir, cronExpr, model)
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

func (s *Store) UpdateScheduledTask(id, name, taskType, command, workingDir, cronExpr, model string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`
		UPDATE scheduled_tasks
		SET name = ?, task_type = ?, command = ?, working_directory = ?, cron_expression = ?, model = ?, enabled = ?, updated_at = datetime('now')
		WHERE id = ?
	`, name, taskType, command, workingDir, cronExpr, model, enabledInt, id)
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

// TaskRun methods

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

// Scheduler helpers

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
		"SELECT " + taskColumns + " FROM scheduled_tasks WHERE enabled = 1",
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
