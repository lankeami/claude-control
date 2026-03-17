package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Prompt struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	ClaudeMessage string     `json:"claude_message"`
	Type          string     `json:"type"`
	Response      *string    `json:"response"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	AnsweredAt    *time.Time `json:"answered_at"`
}

func (s *Store) CreatePrompt(sessionID, claudeMessage, promptType string) (*Prompt, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO prompts (id, session_id, claude_message, type, status, created_at)
		VALUES (?, ?, ?, ?, 'pending', datetime('now'))
	`, id, sessionID, claudeMessage, promptType)
	if err != nil {
		return nil, fmt.Errorf("create prompt: %w", err)
	}

	return s.GetPromptByID(id)
}

func (s *Store) GetPromptByID(id string) (*Prompt, error) {
	var p Prompt
	err := s.db.QueryRow(`
		SELECT id, session_id, claude_message, type, response, status, created_at, answered_at
		FROM prompts WHERE id = ?
	`, id).Scan(&p.ID, &p.SessionID, &p.ClaudeMessage, &p.Type, &p.Response, &p.Status, &p.CreatedAt, &p.AnsweredAt)
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	return &p, nil
}

func (s *Store) RespondToPrompt(id, response string) error {
	res, err := s.db.Exec(`
		UPDATE prompts SET response = ?, status = 'answered', answered_at = datetime('now')
		WHERE id = ? AND status = 'pending'
	`, response, id)
	if err != nil {
		return fmt.Errorf("respond to prompt: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("prompt %s not found or already answered", id)
	}
	return nil
}

func (s *Store) GetPromptResponse(id string) (*string, error) {
	var response *string
	err := s.db.QueryRow("SELECT response FROM prompts WHERE id = ?", id).Scan(&response)
	if err != nil {
		return nil, fmt.Errorf("get response: %w", err)
	}
	return response, nil
}

func (s *Store) ListPrompts(sessionID, status string) ([]Prompt, error) {
	query := "SELECT id, session_id, claude_message, type, response, status, created_at, answered_at FROM prompts WHERE 1=1"
	var args []interface{}

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	defer rows.Close()

	var prompts []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.SessionID, &p.ClaudeMessage, &p.Type, &p.Response, &p.Status, &p.CreatedAt, &p.AnsweredAt); err != nil {
			return nil, fmt.Errorf("scan prompt: %w", err)
		}
		prompts = append(prompts, p)
	}
	return prompts, rows.Err()
}
