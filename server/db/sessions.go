package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID             string    `json:"id"`
	ComputerName   string    `json:"computer_name"`
	ProjectPath    string    `json:"project_path"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	Archived       bool      `json:"archived"`
	Mode           string    `json:"mode"`
	CWD            string    `json:"cwd,omitempty"`
	AllowedTools   string    `json:"allowed_tools,omitempty"`
	MaxTurns       int       `json:"max_turns"`
	MaxBudgetUSD   float64   `json:"max_budget_usd"`
	Initialized     bool   `json:"initialized"`
	ClaudeSessionID       string  `json:"claude_session_id,omitempty"`
	TurnCount             int     `json:"turn_count"`
	AutoContinueThreshold float64 `json:"auto_continue_threshold"`
	MaxContinuations      int     `json:"max_continuations"`
}

const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized, COALESCE(claude_session_id,''), turn_count, auto_continue_threshold, max_continuations`

func scanSession(scanner interface{ Scan(...interface{}) error }) (Session, error) {
	var sess Session
	var archived, initialized int
	err := scanner.Scan(
		&sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
		&sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
		&sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
		&sess.ClaudeSessionID, &sess.TurnCount, &sess.AutoContinueThreshold, &sess.MaxContinuations,
	)
	if err != nil {
		return sess, err
	}
	sess.Archived = archived != 0
	sess.Initialized = initialized != 0
	return sess, nil
}

func (s *Store) UpsertSession(computerName, projectPath, transcriptPath string) (*Session, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, computer_name, project_path, transcript_path, status, created_at, last_seen_at, archived)
		VALUES (?, ?, ?, ?, 'active', datetime('now'), datetime('now'), 0)
		ON CONFLICT(computer_name, project_path) DO UPDATE SET
			last_seen_at = datetime('now'),
			status = 'active',
			transcript_path = ?
	`, id, computerName, projectPath, transcriptPath, transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("upsert session: %w", err)
	}

	return s.getSessionByKey(computerName, projectPath)
}

func (s *Store) getSessionByKey(computerName, projectPath string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE computer_name = ? AND project_path = ?`,
		computerName, projectPath)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := "SELECT " + sessionColumns + " FROM sessions"
	if !includeArchived {
		query += " WHERE archived = 0"
	}
	query += " ORDER BY last_seen_at DESC"
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) GetSessionByID(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
	}
	return &sess, nil
}

func (s *Store) CreateManagedSession(cwd, allowedTools string, maxTurns int, maxBudgetUSD float64) (*Session, error) {
	id := uuid.New().String()
	// Use "__managed__" as computer_name and cwd as project_path to avoid
	// colliding with the existing UNIQUE(computer_name, project_path) constraint.
	_, err := s.db.Exec(`INSERT INTO sessions (id, computer_name, project_path, mode, cwd, allowed_tools, max_turns, max_budget_usd, status)
		VALUES (?, '__managed__', ?, 'managed', ?, ?, ?, ?, 'idle')`,
		id, cwd, cwd, allowedTools, maxTurns, maxBudgetUSD)
	if err != nil {
		return nil, fmt.Errorf("create managed session: %w", err)
	}
	return s.GetSessionByID(id)
}

func (s *Store) SetInitialized(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET initialized = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) ResumeSession(id, claudeSessionID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin resume transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 1, status = 'idle', turn_count = 0 WHERE id = ?`, claudeSessionID, id); err != nil {
		return fmt.Errorf("set claude_session_id: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetTranscriptPath(sessionID string) (string, error) {
	var path sql.NullString
	err := s.db.QueryRow("SELECT transcript_path FROM sessions WHERE id = ?", sessionID).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("get transcript path: %w", err)
	}
	if path.Valid {
		return path.String, nil
	}
	return "", nil
}

func (s *Store) Heartbeat(id string) error {
	res, err := s.db.Exec("UPDATE sessions SET last_seen_at = datetime('now') WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func (s *Store) SetArchived(id string, archived bool) error {
	val := 0
	if archived {
		val = 1
	}
	_, err := s.db.Exec("UPDATE sessions SET archived = ? WHERE id = ?", val, id)
	return err
}

func (s *Store) SetSessionStatus(id, status string) error {
	_, err := s.db.Exec("UPDATE sessions SET status = ? WHERE id = ?", status, id)
	return err
}

func (s *Store) IncrementTurnCount(id string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`UPDATE sessions SET turn_count = turn_count + 1 WHERE id = ? RETURNING turn_count`, id,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("increment turn count: %w", err)
	}
	return count, nil
}

func (s *Store) ResetTurnCount(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET turn_count = 0 WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete transaction: %w", err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		"DELETE FROM messages WHERE session_id = ?",
		"DELETE FROM prompts WHERE session_id = ?",
		"DELETE FROM instructions WHERE session_id = ?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return fmt.Errorf("cascade delete: %w", err)
		}
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return tx.Commit()
}
