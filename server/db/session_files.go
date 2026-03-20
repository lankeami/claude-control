package db

import "time"

type SessionFile struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	FilePath  string    `json:"file_path"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertSessionFile records a file touched during a session. Ignores duplicates.
func (s *Store) InsertSessionFile(sessionID, filePath, action string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO session_files (session_id, file_path, action) VALUES (?, ?, ?)`,
		sessionID, filePath, action,
	)
	return err
}

// ListSessionFiles returns all files touched in a session.
func (s *Store) ListSessionFiles(sessionID string) ([]SessionFile, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, file_path, action, created_at FROM session_files WHERE session_id = ? ORDER BY created_at`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []SessionFile
	for rows.Next() {
		var f SessionFile
		if err := rows.Scan(&f.ID, &f.SessionID, &f.FilePath, &f.Action, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// SessionFileExists checks if a file path was touched in a session.
func (s *Store) SessionFileExists(sessionID, filePath string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_files WHERE session_id = ? AND file_path = ?`,
		sessionID, filePath,
	).Scan(&count)
	return count > 0, err
}
