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

	// Insert all steps first with NULL on_success/on_failure to avoid FK
	// self-reference violations (the referenced step may not exist yet).
	for i, step := range steps {
		_, err = tx.Exec(
			`INSERT INTO workflow_steps (id, workflow_id, name, prompt, step_order, max_retries, timeout_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			stepIDs[i], wfID, step.Name, step.Prompt, step.StepOrder, step.MaxRetries, step.TimeoutSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("insert step %d: %w", i, err)
		}
	}

	// Second pass: update on_success / on_failure now that all rows exist.
	for i, step := range steps {
		var onSuccess, onFailure *string
		if step.OnSuccessIndex != nil && *step.OnSuccessIndex >= 0 && *step.OnSuccessIndex < len(stepIDs) {
			onSuccess = &stepIDs[*step.OnSuccessIndex]
		}
		if step.OnFailureIndex != nil && *step.OnFailureIndex >= 0 && *step.OnFailureIndex < len(stepIDs) {
			onFailure = &stepIDs[*step.OnFailureIndex]
		}
		if onSuccess != nil || onFailure != nil {
			_, err = tx.Exec(
				`UPDATE workflow_steps SET on_success = ?, on_failure = ? WHERE id = ?`,
				onSuccess, onFailure, stepIDs[i],
			)
			if err != nil {
				return nil, fmt.Errorf("update step links %d: %w", i, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return s.GetWorkflow(wfID)
}

func (s *Store) GetWorkflow(id string) (*Workflow, error) {
	var wf Workflow
	err := s.db.QueryRow(
		`SELECT id, name, description, created_at, updated_at FROM workflows WHERE id = ?`, id,
	).Scan(&wf.ID, &wf.Name, &wf.Description, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return &wf, nil
}

func (s *Store) GetWorkflowSteps(workflowID string) ([]WorkflowStep, error) {
	rows, err := s.db.Query(
		`SELECT id, workflow_id, name, prompt, step_order, on_success, on_failure, max_retries, timeout_seconds FROM workflow_steps WHERE workflow_id = ? ORDER BY step_order`,
		workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("query steps: %w", err)
	}
	defer rows.Close()

	var steps []WorkflowStep
	for rows.Next() {
		var step WorkflowStep
		if err := rows.Scan(
			&step.ID, &step.WorkflowID, &step.Name, &step.Prompt, &step.StepOrder,
			&step.OnSuccess, &step.OnFailure, &step.MaxRetries, &step.TimeoutSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func (s *Store) ListWorkflows() ([]Workflow, error) {
	rows, err := s.db.Query(
		`SELECT id, name, description, created_at, updated_at FROM workflows ORDER BY updated_at DESC`,
	)
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

	_, err = tx.Exec(
		`UPDATE workflows SET name = ?, description = ?, updated_at = datetime('now') WHERE id = ?`,
		name, description, id,
	)
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

	// Insert all steps first with NULL on_success/on_failure to avoid FK
	// self-reference violations (the referenced step may not exist yet).
	for i, step := range steps {
		_, err = tx.Exec(
			`INSERT INTO workflow_steps (id, workflow_id, name, prompt, step_order, max_retries, timeout_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			stepIDs[i], id, step.Name, step.Prompt, step.StepOrder, step.MaxRetries, step.TimeoutSeconds,
		)
		if err != nil {
			return fmt.Errorf("insert step %d: %w", i, err)
		}
	}

	// Second pass: update on_success / on_failure now that all rows exist.
	for i, step := range steps {
		var onSuccess, onFailure *string
		if step.OnSuccessIndex != nil && *step.OnSuccessIndex >= 0 && *step.OnSuccessIndex < len(stepIDs) {
			onSuccess = &stepIDs[*step.OnSuccessIndex]
		}
		if step.OnFailureIndex != nil && *step.OnFailureIndex >= 0 && *step.OnFailureIndex < len(stepIDs) {
			onFailure = &stepIDs[*step.OnFailureIndex]
		}
		if onSuccess != nil || onFailure != nil {
			_, err = tx.Exec(
				`UPDATE workflow_steps SET on_success = ?, on_failure = ? WHERE id = ?`,
				onSuccess, onFailure, stepIDs[i],
			)
			if err != nil {
				return fmt.Errorf("update step links %d: %w", i, err)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteWorkflow(id string) error {
	_, err := s.db.Exec(`DELETE FROM workflows WHERE id = ?`, id)
	return err
}

// WorkflowRun represents a single execution of a Workflow.
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

// WorkflowRunStep tracks the execution state of one step within a run.
type WorkflowRunStep struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	StepID     string     `json:"step_id"`
	StepName   string     `json:"step_name"`
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
	err := s.db.QueryRow(
		`SELECT id, workflow_id, session_id, status, current_step_id, started_at, finished_at, error FROM workflow_runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.WorkflowID, &r.SessionID, &r.Status, &r.CurrentStepID, &r.StartedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow run: %w", err)
	}
	return &r, nil
}

// ListWorkflowRuns returns runs optionally filtered by workflowID and/or sessionID.
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

// UpdateWorkflowRunStatus updates status (and error). Sets finished_at for terminal states.
func (s *Store) UpdateWorkflowRunStatus(id, status string, runErr *string) error {
	if status == "completed" || status == "failed" || status == "cancelled" {
		_, err := s.db.Exec(
			`UPDATE workflow_runs SET status = ?, error = ?, finished_at = datetime('now') WHERE id = ?`,
			status, runErr, id,
		)
		return err
	}
	_, err := s.db.Exec(`UPDATE workflow_runs SET status = ?, error = ? WHERE id = ?`, status, runErr, id)
	return err
}

func (s *Store) UpdateWorkflowRunCurrentStep(id, stepID string) error {
	_, err := s.db.Exec(`UPDATE workflow_runs SET current_step_id = ? WHERE id = ?`, stepID, id)
	return err
}

// CreateWorkflowRunSteps bulk-inserts pending run-step rows for the given step IDs.
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

// GetWorkflowRunSteps returns run steps ordered by their parent step's step_order.
func (s *Store) GetWorkflowRunSteps(runID string) ([]WorkflowRunStep, error) {
	rows, err := s.db.Query(`
		SELECT rs.id, rs.run_id, rs.step_id, ws.name, rs.status, rs.attempt, rs.started_at, rs.finished_at, rs.error
		FROM workflow_run_steps rs
		JOIN workflow_steps ws ON rs.step_id = ws.id
		WHERE rs.run_id = ?
		ORDER BY ws.step_order`, runID)
	if err != nil {
		return nil, fmt.Errorf("query run steps: %w", err)
	}
	defer rows.Close()

	var steps []WorkflowRunStep
	for rows.Next() {
		var rs WorkflowRunStep
		if err := rows.Scan(&rs.ID, &rs.RunID, &rs.StepID, &rs.StepName, &rs.Status, &rs.Attempt, &rs.StartedAt, &rs.FinishedAt, &rs.Error); err != nil {
			return nil, fmt.Errorf("scan run step: %w", err)
		}
		steps = append(steps, rs)
	}
	return steps, rows.Err()
}

// UpdateWorkflowRunStepStatus updates a run step's status. Sets started_at when
// transitioning to "running"; sets finished_at for all other non-pending states.
func (s *Store) UpdateWorkflowRunStepStatus(id, status string, attempt int, stepErr *string) error {
	if status == "running" {
		_, err := s.db.Exec(
			`UPDATE workflow_run_steps SET status = ?, attempt = ?, started_at = datetime('now') WHERE id = ?`,
			status, attempt, id,
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE workflow_run_steps SET status = ?, attempt = ?, error = ?, finished_at = datetime('now') WHERE id = ?`,
		status, attempt, stepErr, id,
	)
	return err
}

// GetActiveRunForSession returns the first run for a session with status pending/running/paused.
func (s *Store) GetActiveRunForSession(sessionID string) (*WorkflowRun, error) {
	var r WorkflowRun
	err := s.db.QueryRow(`
		SELECT id, workflow_id, session_id, status, current_step_id, started_at, finished_at, error
		FROM workflow_runs
		WHERE session_id = ? AND status IN ('pending','running','paused')
		LIMIT 1`, sessionID,
	).Scan(&r.ID, &r.WorkflowID, &r.SessionID, &r.Status, &r.CurrentStepID, &r.StartedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active run: %w", err)
	}
	return &r, nil
}

// ResetStaleWorkflowRuns marks any in-flight "running" runs as failed (e.g. on server restart).
func (s *Store) ResetStaleWorkflowRuns() error {
	errMsg := "server restarted"
	_, err := s.db.Exec(
		`UPDATE workflow_runs SET status = 'failed', error = ?, finished_at = datetime('now') WHERE status = 'running'`,
		errMsg,
	)
	return err
}
