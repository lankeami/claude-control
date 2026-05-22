package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCreatesTablesAndWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Verify WAL mode
	var journalMode string
	err = store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", journalMode)
	}

	// Verify tables exist
	tables := []string{"sessions", "prompts", "instructions"}
	for _, table := range tables {
		var name string
		err := store.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}

	// Verify unique constraint on sessions
	_, err = store.db.Exec(
		"INSERT INTO sessions (id, computer_name, project_path, status, created_at, last_seen_at, archived) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), 0)",
		"id1", "mac1", "/project", "active",
	)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	_, err = store.db.Exec(
		"INSERT INTO sessions (id, computer_name, project_path, status, created_at, last_seen_at, archived) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), 0)",
		"id2", "mac1", "/project", "active",
	)
	if err == nil {
		t.Error("expected unique constraint violation, got nil")
	}

	// Verify file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

// TestMigrate_RebuildsLegacyUniqueConstraint simulates upgrading a DB that
// was created with the original inline `UNIQUE(computer_name, project_path)`
// constraint, and verifies the rebuild drops the constraint, preserves data,
// and allows recreating managed sessions in a cwd whose prior session was
// soft-deleted.
func TestMigrate_RebuildsLegacyUniqueConstraint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Build the legacy schema by hand, including the inline UNIQUE constraint
	// and every ALTER TABLE ADD COLUMN that existed before this migration.
	legacy := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			computer_name TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
			archived INTEGER NOT NULL DEFAULT 0,
			UNIQUE(computer_name, project_path)
		)`,
		`ALTER TABLE sessions ADD COLUMN transcript_path TEXT`,
		`ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'hook'`,
		`ALTER TABLE sessions ADD COLUMN cwd TEXT`,
		`ALTER TABLE sessions ADD COLUMN allowed_tools TEXT`,
		`ALTER TABLE sessions ADD COLUMN max_turns INTEGER NOT NULL DEFAULT 50`,
		`ALTER TABLE sessions ADD COLUMN max_budget_usd REAL NOT NULL DEFAULT 5.0`,
		`ALTER TABLE sessions ADD COLUMN initialized INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN claude_session_id TEXT`,
		`ALTER TABLE sessions ADD COLUMN turn_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN auto_continue_threshold REAL NOT NULL DEFAULT 0.8`,
		`ALTER TABLE sessions ADD COLUMN max_continuations INTEGER NOT NULL DEFAULT 5`,
		`ALTER TABLE sessions ADD COLUMN activity_state TEXT NOT NULL DEFAULT 'idle'`,
		`ALTER TABLE sessions ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN compact_every_n_continues INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN deleted_at DATETIME`,
		// Seed a soft-deleted managed session at /projects/x and a live
		// hook session at (mac1,/p1), then close.
		`INSERT INTO sessions (id, computer_name, project_path, mode, cwd, deleted_at)
			VALUES ('s-deleted', '__managed__', '/projects/x', 'managed', '/projects/x', datetime('now'))`,
		`INSERT INTO sessions (id, computer_name, project_path, mode)
			VALUES ('s-hook', 'mac1', '/p1', 'hook')`,
	}
	for _, q := range legacy {
		if _, err := rawDB.Exec(q); err != nil {
			t.Fatalf("legacy setup %q: %v", q, err)
		}
	}
	rawDB.Close()

	// Now run the real Open which invokes migrate() and the rebuild.
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after legacy seed: %v", err)
	}
	defer store.Close()

	// Schema should no longer contain the inline UNIQUE constraint.
	var schema string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&schema); err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if strings.Contains(schema, "UNIQUE(computer_name, project_path)") {
		t.Errorf("legacy UNIQUE constraint still present in schema:\n%s", schema)
	}

	// Both seeded rows should still be in the table.
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 preserved rows, got %d", n)
	}

	// Creating a new managed session in the cwd of the soft-deleted row
	// should now succeed (the original bug).
	if _, err := store.CreateManagedSession("/projects/x", `["Bash"]`, 50, 5.0, 0); err != nil {
		t.Errorf("recreate managed session at soft-deleted cwd: %v", err)
	}

	// Hook-mode uniqueness should still hold: live hook session prevents
	// a second one with the same (computer_name, project_path).
	_, err = store.UpsertSession("mac1", "/p1", "")
	if err != nil {
		t.Errorf("upsert existing hook session should update, not fail: %v", err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE computer_name='mac1' AND project_path='/p1' AND deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count hook: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 live hook session, got %d", n)
	}
}
