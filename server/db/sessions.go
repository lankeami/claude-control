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
	var sess Session
	var archived int
	var transcriptPath sql.NullString
	err := s.db.QueryRow(`
		SELECT id, computer_name, project_path, transcript_path, status, created_at, last_seen_at, archived
		FROM sessions WHERE computer_name = ? AND project_path = ?
	`, computerName, projectPath).Scan(
		&sess.ID, &sess.ComputerName, &sess.ProjectPath, &transcriptPath, &sess.Status,
		&sess.CreatedAt, &sess.LastSeenAt, &archived,
	)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.Archived = archived != 0
	if transcriptPath.Valid {
		sess.TranscriptPath = transcriptPath.String
	}
	return &sess, nil
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := "SELECT id, computer_name, project_path, transcript_path, status, created_at, last_seen_at, archived FROM sessions"
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
		var sess Session
		var archived int
		var transcriptPath sql.NullString
		if err := rows.Scan(&sess.ID, &sess.ComputerName, &sess.ProjectPath, &transcriptPath, &sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.Archived = archived != 0
		if transcriptPath.Valid {
			sess.TranscriptPath = transcriptPath.String
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
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
