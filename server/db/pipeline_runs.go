package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type PipelineRun struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Mode       string     `json:"mode"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      *string    `json:"error"`
}

type PipelineRunItem struct {
	ID           string     `json:"id"`
	RunID        string     `json:"run_id"`
	FeatureLabel string     `json:"feature_label"`
	SessionID    *string    `json:"session_id"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Error        *string    `json:"error"`
}

func (s *Store) CreatePipelineRun(name, mode string) (*PipelineRun, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO pipeline_runs (id, name, mode) VALUES (?, ?, ?)`, id, name, mode)
	if err != nil {
		return nil, fmt.Errorf("create pipeline run: %w", err)
	}
	return s.GetPipelineRun(id)
}

func (s *Store) GetPipelineRun(id string) (*PipelineRun, error) {
	var r PipelineRun
	err := s.db.QueryRow(
		`SELECT id, name, mode, status, created_at, finished_at, error FROM pipeline_runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &r.Mode, &r.Status, &r.CreatedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline run: %w", err)
	}
	return &r, nil
}

func (s *Store) ListPipelineRuns() ([]PipelineRun, error) {
	rows, err := s.db.Query(`SELECT id, name, mode, status, created_at, finished_at, error FROM pipeline_runs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		if err := rows.Scan(&r.ID, &r.Name, &r.Mode, &r.Status, &r.CreatedAt, &r.FinishedAt, &r.Error); err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) UpdatePipelineRunStatus(id, status string, runErr *string) error {
	if status == "completed" || status == "failed" || status == "cancelled" {
		_, err := s.db.Exec(
			`UPDATE pipeline_runs SET status = ?, error = ?, finished_at = datetime('now') WHERE id = ?`,
			status, runErr, id,
		)
		return err
	}
	_, err := s.db.Exec(`UPDATE pipeline_runs SET status = ?, error = ? WHERE id = ?`, status, runErr, id)
	return err
}

func (s *Store) CreatePipelineRunItem(runID, featureLabel, sessionID string) (*PipelineRunItem, error) {
	id := uuid.New().String()
	var sessPtr *string
	if sessionID != "" {
		sessPtr = &sessionID
	}
	_, err := s.db.Exec(
		`INSERT INTO pipeline_run_items (id, run_id, feature_label, session_id) VALUES (?, ?, ?, ?)`,
		id, runID, featureLabel, sessPtr,
	)
	if err != nil {
		return nil, fmt.Errorf("create pipeline run item: %w", err)
	}
	return s.getPipelineRunItem(id)
}

func (s *Store) getPipelineRunItem(id string) (*PipelineRunItem, error) {
	var it PipelineRunItem
	err := s.db.QueryRow(
		`SELECT id, run_id, feature_label, session_id, status, created_at, finished_at, error FROM pipeline_run_items WHERE id = ?`, id,
	).Scan(&it.ID, &it.RunID, &it.FeatureLabel, &it.SessionID, &it.Status, &it.CreatedAt, &it.FinishedAt, &it.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline run item: %w", err)
	}
	return &it, nil
}

func (s *Store) GetPipelineRunItems(runID string) ([]PipelineRunItem, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, feature_label, session_id, status, created_at, finished_at, error FROM pipeline_run_items WHERE run_id = ? ORDER BY created_at`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipeline run items: %w", err)
	}
	defer rows.Close()

	var items []PipelineRunItem
	for rows.Next() {
		var it PipelineRunItem
		if err := rows.Scan(&it.ID, &it.RunID, &it.FeatureLabel, &it.SessionID, &it.Status, &it.CreatedAt, &it.FinishedAt, &it.Error); err != nil {
			return nil, fmt.Errorf("scan pipeline run item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) UpdatePipelineRunItemStatus(id, status string, itemErr *string) error {
	if status == "completed" || status == "failed" || status == "skipped" {
		_, err := s.db.Exec(
			`UPDATE pipeline_run_items SET status = ?, error = ?, finished_at = datetime('now') WHERE id = ?`,
			status, itemErr, id,
		)
		return err
	}
	_, err := s.db.Exec(`UPDATE pipeline_run_items SET status = ?, error = ? WHERE id = ?`, status, itemErr, id)
	return err
}
