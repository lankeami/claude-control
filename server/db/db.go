package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// sessionColumnList is the ordered list of every column on the `sessions`
// table. Used by the rebuild migration to copy rows column-by-column rather
// than relying on `SELECT *`, which is sensitive to the historical column
// order produced by successive ALTER TABLE ADD COLUMN statements.
var sessionColumnList = []string{
	"id", "computer_name", "project_path", "status", "created_at", "last_seen_at", "archived",
	"transcript_path", "mode", "cwd", "allowed_tools", "max_turns", "max_budget_usd", "initialized",
	"claude_session_id", "turn_count", "auto_continue_threshold", "max_continuations", "activity_state",
	"name", "compact_every_n_continues", "model", "deleted_at",
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	// modernc.org/sqlite does not honour mattn-style DSN pragma params,
	// so set them explicitly after opening.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
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

// QueryRows executes a query and returns the result rows
func (s *Store) QueryRows(query string, args ...interface{}) (*sql.Rows, error) {
	return s.db.Query(query, args...)
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
			archived INTEGER NOT NULL DEFAULT 0
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
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    task_type TEXT NOT NULL CHECK(task_type IN ('shell', 'claude')),
    command TEXT NOT NULL,
    working_directory TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_run_at DATETIME,
    next_run_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
		`CREATE TABLE IF NOT EXISTS task_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at DATETIME,
    exit_code INTEGER,
    output TEXT,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'success', 'failed'))
)`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_task_id ON task_runs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_started_at ON task_runs(started_at)`,
		`ALTER TABLE sessions ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS recent_directories (
			path TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_used_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`ALTER TABLE sessions ADD COLUMN compact_every_n_continues INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE scheduled_tasks ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN cost REAL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN cleared_at DATETIME`,
		`ALTER TABLE sessions ADD COLUMN deleted_at DATETIME`,
		`DROP INDEX IF EXISTS idx_managed_cwd`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_cwd ON sessions(cwd) WHERE mode = 'managed' AND deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_hook_computer_project ON sessions(computer_name, project_path) WHERE mode = 'hook' AND deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
		`CREATE TABLE IF NOT EXISTS workflow_steps (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    step_order INTEGER NOT NULL,
    on_success TEXT REFERENCES workflow_steps(id) ON DELETE SET NULL,
    on_failure TEXT REFERENCES workflow_steps(id) ON DELETE SET NULL,
    max_retries INTEGER NOT NULL DEFAULT 0,
    timeout_seconds INTEGER NOT NULL DEFAULT 0
)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_steps_workflow ON workflow_steps(workflow_id)`,
		`CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','paused','completed','failed','cancelled')),
    current_step_id TEXT REFERENCES workflow_steps(id),
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at DATETIME,
    error TEXT
)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow ON workflow_runs(workflow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_session ON workflow_runs(session_id)`,
		`CREATE TABLE IF NOT EXISTS workflow_run_steps (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_id TEXT NOT NULL REFERENCES workflow_steps(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','skipped')),
    attempt INTEGER NOT NULL DEFAULT 1,
    started_at DATETIME,
    finished_at DATETIME,
    error TEXT
)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_run_steps_run ON workflow_run_steps(run_id)`,
	}

	for _, m := range migrations {
		_, err := db.Exec(m)
		if err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration failed: %w", err)
			}
		}
	}
	if err := rebuildSessionsTableIfNeeded(db); err != nil {
		return fmt.Errorf("rebuild sessions table: %w", err)
	}
	return nil
}

// rebuildSessionsTableIfNeeded drops the legacy table-level
// `UNIQUE(computer_name, project_path)` constraint by rebuilding the table.
// The legacy constraint is not partial, so it spuriously blocks creating a
// new managed session in a cwd where a previous session was soft-deleted.
// Hook-mode uniqueness is preserved via the partial index
// `idx_hook_computer_project`. No-ops on fresh installs or DBs that have
// already been rebuilt.
func rebuildSessionsTableIfNeeded(db *sql.DB) error {
	var schema string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&schema)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sessions schema: %w", err)
	}
	if !strings.Contains(schema, "UNIQUE(computer_name, project_path)") {
		return nil
	}

	// PRAGMA foreign_keys can only be toggled outside a transaction.
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}
	defer func() { _, _ = db.Exec(`PRAGMA foreign_keys = ON`) }()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	cols := strings.Join(sessionColumnList, ", ")
	steps := []string{
		`DROP INDEX IF EXISTS idx_managed_cwd`,
		`DROP INDEX IF EXISTS idx_hook_computer_project`,
		`CREATE TABLE sessions_new (
			id TEXT PRIMARY KEY,
			computer_name TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
			archived INTEGER NOT NULL DEFAULT 0,
			transcript_path TEXT,
			mode TEXT NOT NULL DEFAULT 'hook',
			cwd TEXT,
			allowed_tools TEXT,
			max_turns INTEGER NOT NULL DEFAULT 50,
			max_budget_usd REAL NOT NULL DEFAULT 5.0,
			initialized INTEGER NOT NULL DEFAULT 0,
			claude_session_id TEXT,
			turn_count INTEGER NOT NULL DEFAULT 0,
			auto_continue_threshold REAL NOT NULL DEFAULT 0.8,
			max_continuations INTEGER NOT NULL DEFAULT 5,
			activity_state TEXT NOT NULL DEFAULT 'idle',
			name TEXT NOT NULL DEFAULT '',
			compact_every_n_continues INTEGER NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			deleted_at DATETIME
		)`,
		`INSERT INTO sessions_new (` + cols + `) SELECT ` + cols + ` FROM sessions`,
		`DROP TABLE sessions`,
		`ALTER TABLE sessions_new RENAME TO sessions`,
		`CREATE UNIQUE INDEX idx_managed_cwd ON sessions(cwd) WHERE mode = 'managed' AND deleted_at IS NULL`,
		`CREATE UNIQUE INDEX idx_hook_computer_project ON sessions(computer_name, project_path) WHERE mode = 'hook' AND deleted_at IS NULL`,
	}
	for _, s := range steps {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("rebuild step %q: %w", firstLine(s), err)
		}
	}

	// Note: we intentionally do not run PRAGMA foreign_key_check here.
	// The rebuild copies all rows atomically inside a single transaction;
	// any FK reference that was valid before stays valid after. Pre-existing
	// orphans (from sessions that were hard-deleted before the soft-delete
	// change) are not introduced by this migration and should not block it.
	return tx.Commit()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
