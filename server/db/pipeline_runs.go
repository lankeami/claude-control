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
	WorkingDir string     `json:"working_dir"`
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

// ReconcileStalePipelineRuns marks running pipeline runs as completed/failed
// if all their items are in terminal states, or as failed if older than 24h.
func (s *Store) ReconcileStalePipelineRuns() {
	rows, err := s.db.Query(`SELECT id FROM pipeline_runs WHERE status = 'running'`)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		var total, terminal int
		s.db.QueryRow(`SELECT COUNT(*) FROM pipeline_run_items WHERE run_id = ?`, id).Scan(&total)
		s.db.QueryRow(
			`SELECT COUNT(*) FROM pipeline_run_items WHERE run_id = ? AND status IN ('completed','failed','skipped')`, id,
		).Scan(&terminal)
		if total > 0 && terminal == total {
			var anyFailed int
			s.db.QueryRow(`SELECT COUNT(*) FROM pipeline_run_items WHERE run_id = ? AND status = 'failed'`, id).Scan(&anyFailed)
			if anyFailed > 0 {
				errMsg := "one or more items failed"
				s.UpdatePipelineRunStatus(id, "failed", &errMsg)
			} else {
				s.UpdatePipelineRunStatus(id, "completed", nil)
			}
		} else {
			var createdAt time.Time
			s.db.QueryRow(`SELECT created_at FROM pipeline_runs WHERE id = ?`, id).Scan(&createdAt)
			if time.Since(createdAt) > 24*time.Hour {
				errMsg := "stale: timed out after 24h"
				s.UpdatePipelineRunStatus(id, "failed", &errMsg)
			}
		}
	}
}

func (s *Store) CreatePipelineRun(name, mode, workingDir string) (*PipelineRun, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO pipeline_runs (id, name, mode, working_dir) VALUES (?, ?, ?, ?)`, id, name, mode, workingDir)
	if err != nil {
		return nil, fmt.Errorf("create pipeline run: %w", err)
	}
	return s.GetPipelineRun(id)
}

func (s *Store) GetPipelineRun(id string) (*PipelineRun, error) {
	var r PipelineRun
	err := s.db.QueryRow(
		`SELECT id, name, mode, status, working_dir, created_at, finished_at, error FROM pipeline_runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &r.Mode, &r.Status, &r.WorkingDir, &r.CreatedAt, &r.FinishedAt, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline run: %w", err)
	}
	return &r, nil
}

func (s *Store) ListPipelineRuns() ([]PipelineRun, error) {
	rows, err := s.db.Query(`SELECT id, name, mode, status, working_dir, created_at, finished_at, error FROM pipeline_runs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		if err := rows.Scan(&r.ID, &r.Name, &r.Mode, &r.Status, &r.WorkingDir, &r.CreatedAt, &r.FinishedAt, &r.Error); err != nil {
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
		if err != nil {
			return err
		}
		s.maybeCompletePipelineRun(id)
		return nil
	}
	_, err := s.db.Exec(`UPDATE pipeline_run_items SET status = ?, error = ? WHERE id = ?`, status, itemErr, id)
	return err
}

// maybeCompletePipelineRun checks if all items for the parent run are terminal
// and auto-completes the run if so.
func (s *Store) maybeCompletePipelineRun(itemID string) {
	var runID string
	if err := s.db.QueryRow(`SELECT run_id FROM pipeline_run_items WHERE id = ?`, itemID).Scan(&runID); err != nil {
		return
	}
	var pending int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pipeline_run_items WHERE run_id = ? AND status NOT IN ('completed','failed','skipped')`,
		runID,
	).Scan(&pending); err != nil || pending > 0 {
		return
	}
	var anyFailed int
	s.db.QueryRow(`SELECT COUNT(*) FROM pipeline_run_items WHERE run_id = ? AND status = 'failed'`, runID).Scan(&anyFailed)
	if anyFailed > 0 {
		errMsg := "one or more items failed"
		s.UpdatePipelineRunStatus(runID, "failed", &errMsg)
	} else {
		s.UpdatePipelineRunStatus(runID, "completed", nil)
	}
}
