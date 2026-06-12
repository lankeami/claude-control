package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID                     string    `json:"id"`
	ComputerName           string    `json:"computer_name"`
	ProjectPath            string    `json:"project_path"`
	TranscriptPath         string    `json:"transcript_path,omitempty"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"created_at"`
	LastSeenAt             time.Time `json:"last_seen_at"`
	Archived               bool      `json:"archived"`
	Mode                   string    `json:"mode"`
	CWD                    string    `json:"cwd,omitempty"`
	AllowedTools           string    `json:"allowed_tools,omitempty"`
	MaxTurns               int       `json:"max_turns"`
	MaxBudgetUSD           float64   `json:"max_budget_usd"`
	Initialized            bool      `json:"initialized"`
	ClaudeSessionID        string    `json:"claude_session_id,omitempty"`
	MaxContinuations       int       `json:"max_continuations"`
	ActivityState          string    `json:"activity_state"`
	TurnCount              int       `json:"turn_count"`
	Name                   string    `json:"name"`
	CompactEveryNContinues int       `json:"compact_every_n_continues"`
	Model                  string    `json:"-"`
}

const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized, COALESCE(claude_session_id,''), max_continuations, COALESCE(activity_state,'idle'), turn_count, COALESCE(name,''), compact_every_n_continues, COALESCE(model,'')`

func scanSession(scanner interface{ Scan(...interface{}) error }) (Session, error) {
	var sess Session
	var archived, initialized int
	err := scanner.Scan(
		&sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
		&sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
		&sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
		&sess.ClaudeSessionID, &sess.MaxContinuations, &sess.ActivityState,
		&sess.TurnCount, &sess.Name, &sess.CompactEveryNContinues, &sess.Model,
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
	// Conflict target matches the partial index `idx_hook_computer_project`;
	// SQLite requires the partial index's WHERE clause to be restated here.
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, computer_name, project_path, transcript_path, status, created_at, last_seen_at, archived)
		VALUES (?, ?, ?, ?, 'active', datetime('now'), datetime('now'), 0)
		ON CONFLICT(computer_name, project_path) WHERE mode = 'hook' AND deleted_at IS NULL DO UPDATE SET
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
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE computer_name = ? AND project_path = ? AND deleted_at IS NULL`,
		computerName, projectPath)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := "SELECT " + sessionColumns + " FROM sessions WHERE deleted_at IS NULL"
	if !includeArchived {
		query += " AND archived = 0"
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
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ? AND deleted_at IS NULL`, id)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
	}
	return &sess, nil
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*Session, error) {
	return s.GetSessionByID(id)
}

func (s *Store) CreateManagedSession(cwd, allowedTools string, maxTurns int, maxBudgetUSD float64, compactEveryNContinues int) (*Session, error) {
	id := uuid.New().String()
	// Use "__managed__" as computer_name and cwd as project_path to avoid
	// colliding with the existing UNIQUE(computer_name, project_path) constraint.
	_, err := s.db.Exec(`INSERT INTO sessions (id, computer_name, project_path, mode, cwd, allowed_tools, max_turns, max_budget_usd, compact_every_n_continues, status)
		VALUES (?, '__managed__', ?, 'managed', ?, ?, ?, ?, ?, 'idle')`,
		id, cwd, cwd, allowedTools, maxTurns, maxBudgetUSD, compactEveryNContinues)
	if err != nil {
		return nil, fmt.Errorf("create managed session: %w", err)
	}
	// Track directory in recent_directories (persists after session deletion)
	_, _ = s.db.Exec(`INSERT INTO recent_directories (path, name, last_used_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET last_used_at = datetime('now')`,
		cwd, filepath.Base(cwd))
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
	if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 1, status = 'idle' WHERE id = ?`, claudeSessionID, id); err != nil {
		return fmt.Errorf("set claude_session_id: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return tx.Commit()
}

func (s *Store) ClearSession(id string) error {
	newClaudeSessionID := uuid.New().String()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin clear transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE messages SET cleared_at = datetime('now') WHERE session_id = ? AND cleared_at IS NULL`, id); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}
	if _, err := tx.Exec(`UPDATE sessions SET claude_session_id = ?, initialized = 0, activity_state = 'idle', turn_count = 0 WHERE id = ?`, newClaudeSessionID, id); err != nil {
		return fmt.Errorf("reset session state: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetTranscriptPath(sessionID string) (string, error) {
	var path sql.NullString
	err := s.db.QueryRow("SELECT transcript_path FROM sessions WHERE id = ? AND deleted_at IS NULL", sessionID).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("get transcript path: %w", err)
	}
	if path.Valid {
		return path.String, nil
	}
	return "", nil
}

func (s *Store) Heartbeat(id string) error {
	res, err := s.db.Exec("UPDATE sessions SET last_seen_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
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
	err := s.db.QueryRow(`UPDATE sessions SET turn_count = turn_count + 1 WHERE id = ? AND deleted_at IS NULL RETURNING turn_count`, id).Scan(&count)
	return count, err
}

func (s *Store) ResetTurnCount(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET turn_count = 0 WHERE id = ? AND deleted_at IS NULL`, id)
	return err
}

func (s *Store) UpdateActivityState(id, state string) error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = ? WHERE id = ? AND deleted_at IS NULL", state, id)
	return err
}

func (s *Store) UpdateClaudeSessionID(id, claudeSessionID string) error {
	_, err := s.db.Exec("UPDATE sessions SET claude_session_id = ? WHERE id = ? AND deleted_at IS NULL", claudeSessionID, id)
	return err
}

func (s *Store) UpdateSessionModel(id, model string) error {
	_, err := s.db.Exec("UPDATE sessions SET model = ? WHERE id = ?", model, id)
	return err
}

func (s *Store) UpdateSessionName(id, name string) error {
	res, err := s.db.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("update session name: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

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
		`SELECT claude_session_id, name FROM sessions WHERE claude_session_id IN (`+placeholders+`) AND name != '' AND deleted_at IS NULL`,
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

func (s *Store) ResetStaleActivityStates() error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = 'idle' WHERE activity_state = 'working' AND deleted_at IS NULL")
	return err
}

// SetWorkingToWaiting updates all sessions with activity_state='working' to 'waiting'.
// Used during graceful restart to preserve conversation continuity.
func (s *Store) SetWorkingToWaiting() error {
	_, err := s.db.Exec("UPDATE sessions SET activity_state = 'waiting' WHERE activity_state = 'working' AND deleted_at IS NULL")
	return err
}

func (s *Store) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete transaction: %w", err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		"DELETE FROM prompts WHERE session_id = ?",
		"DELETE FROM instructions WHERE session_id = ?",
		"DELETE FROM session_files WHERE session_id = ?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return fmt.Errorf("cascade delete: %w", err)
		}
	}
	// Soft-delete the session to preserve cost data in messages
	if _, err := tx.Exec("UPDATE sessions SET deleted_at = datetime('now') WHERE id = ?", id); err != nil {
		return fmt.Errorf("soft delete session: %w", err)
	}
	return tx.Commit()
}

type RecentDir struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (s *Store) RecentDirectories(limit int) ([]RecentDir, error) {
	rows, err := s.db.Query(`
		SELECT path, name
		FROM recent_directories
		ORDER BY last_used_at DESC, rowid DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent directories: %w", err)
	}
	defer rows.Close()

	var dirs []RecentDir
	for rows.Next() {
		var d RecentDir
		if err := rows.Scan(&d.Path, &d.Name); err != nil {
			return nil, fmt.Errorf("scan recent dir: %w", err)
		}
		dirs = append(dirs, d)
	}
	return dirs, rows.Err()
}
