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
