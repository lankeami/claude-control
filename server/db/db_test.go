package db

import (
	"os"
	"path/filepath"
	"testing"
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
