package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			computer_name TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
			archived INTEGER NOT NULL DEFAULT 0,
			UNIQUE(computer_name, project_path)
		)`,
		`CREATE TABLE IF NOT EXISTS prompts (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			claude_message TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'prompt',
			response TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			answered_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS instructions (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			message TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			delivered_at DATETIME
		)`,
		`ALTER TABLE sessions ADD COLUMN transcript_path TEXT`,
		`ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'hook'`,
		`ALTER TABLE sessions ADD COLUMN cwd TEXT`,
		`ALTER TABLE sessions ADD COLUMN allowed_tools TEXT`,
		`ALTER TABLE sessions ADD COLUMN max_turns INTEGER NOT NULL DEFAULT 50`,
		`ALTER TABLE sessions ADD COLUMN max_budget_usd REAL NOT NULL DEFAULT 5.0`,
		`ALTER TABLE sessions ADD COLUMN initialized INTEGER NOT NULL DEFAULT 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_cwd ON sessions(cwd) WHERE mode = 'managed'`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			exit_code INTEGER,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(session_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq)`,
		`ALTER TABLE sessions ADD COLUMN claude_session_id TEXT`,
		`CREATE TABLE IF NOT EXISTS session_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id),
    UNIQUE(session_id, file_path, action)
)`,
		`ALTER TABLE sessions ADD COLUMN turn_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN auto_continue_threshold REAL NOT NULL DEFAULT 0.8`,
		`ALTER TABLE sessions ADD COLUMN max_continuations INTEGER NOT NULL DEFAULT 5`,
		`ALTER TABLE sessions ADD COLUMN activity_state TEXT NOT NULL DEFAULT 'idle'`,
	}

	for _, m := range migrations {
		_, err := db.Exec(m)
		if err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration failed: %w", err)
			}
		}
	}
	return nil
}
