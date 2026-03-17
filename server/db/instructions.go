package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Instruction struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	Message     string     `json:"message"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
}

func (s *Store) QueueInstruction(sessionID, message string) (*Instruction, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO instructions (id, session_id, message, status, created_at)
		VALUES (?, ?, ?, 'queued', datetime('now'))
	`, id, sessionID, message)
	if err != nil {
		return nil, fmt.Errorf("queue instruction: %w", err)
	}

	var instr Instruction
	err = s.db.QueryRow(`
		SELECT id, session_id, message, status, created_at, delivered_at
		FROM instructions WHERE id = ?
	`, id).Scan(&instr.ID, &instr.SessionID, &instr.Message, &instr.Status, &instr.CreatedAt, &instr.DeliveredAt)
	if err != nil {
		return nil, fmt.Errorf("get instruction: %w", err)
	}
	return &instr, nil
}

func (s *Store) FetchNextInstruction(sessionID string) (*Instruction, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var instr Instruction
	err = tx.QueryRow(`
		SELECT id, session_id, message, status, created_at, delivered_at
		FROM instructions WHERE session_id = ? AND status = 'queued'
		ORDER BY created_at ASC LIMIT 1
	`, sessionID).Scan(&instr.ID, &instr.SessionID, &instr.Message, &instr.Status, &instr.CreatedAt, &instr.DeliveredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch instruction: %w", err)
	}

	// Mark as delivered within same transaction
	_, err = tx.Exec(`
		UPDATE instructions SET status = 'delivered', delivered_at = datetime('now') WHERE id = ?
	`, instr.ID)
	if err != nil {
		return nil, fmt.Errorf("mark delivered: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	instr.Status = "delivered"
	return &instr, nil
}
