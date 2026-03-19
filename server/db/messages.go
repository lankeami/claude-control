package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Seq       int       `json:"seq"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateMessage(sessionID, role, content string) (*Message, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO messages (id, session_id, seq, role, content)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?), ?, ?)`,
		id, sessionID, sessionID, role, content)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	var msg Message
	err = s.db.QueryRow(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE id = ?`, id).
		Scan(&msg.ID, &msg.SessionID, &msg.Seq, &msg.Role, &msg.Content, &msg.ExitCode, &msg.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("read back message: %w", err)
	}
	return &msg, nil
}

func (s *Store) CreateMessageWithExitCode(sessionID, role, content string, exitCode int) (*Message, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO messages (id, session_id, seq, role, content, exit_code)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?), ?, ?, ?)`,
		id, sessionID, sessionID, role, content, exitCode)
	if err != nil {
		return nil, fmt.Errorf("insert message with exit code: %w", err)
	}
	var msg Message
	err = s.db.QueryRow(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE id = ?`, id).
		Scan(&msg.ID, &msg.SessionID, &msg.Seq, &msg.Role, &msg.Content, &msg.ExitCode, &msg.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("read back message: %w", err)
	}
	return &msg, nil
}

func (s *Store) ListMessages(sessionID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content, &m.ExitCode, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}
