package db

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestUpsertSession(t *testing.T) {
	store := newTestStore(t)

	// First upsert creates
	s1, err := store.UpsertSession("mac1", "/project/a")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if s1.ComputerName != "mac1" || s1.ProjectPath != "/project/a" {
		t.Errorf("unexpected session: %+v", s1)
	}

	// Second upsert returns same ID, updates last_seen_at
	s2, err := store.UpsertSession("mac1", "/project/a")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if s2.ID != s1.ID {
		t.Errorf("expected same ID %s, got %s", s1.ID, s2.ID)
	}

	// Different project creates new session
	s3, err := store.UpsertSession("mac1", "/project/b")
	if err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	if s3.ID == s1.ID {
		t.Error("expected different ID for different project")
	}
}

func TestListSessions(t *testing.T) {
	store := newTestStore(t)

	store.UpsertSession("mac1", "/project/a")
	store.UpsertSession("mac1", "/project/b")

	sessions, err := store.ListSessions(false)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestArchiveSession(t *testing.T) {
	store := newTestStore(t)

	s, _ := store.UpsertSession("mac1", "/project/a")
	err := store.SetArchived(s.ID, true)
	if err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	// Archived sessions excluded by default
	sessions, _ := store.ListSessions(false)
	if len(sessions) != 0 {
		t.Errorf("expected 0 non-archived sessions, got %d", len(sessions))
	}

	// Included when requested
	sessions, _ = store.ListSessions(true)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session with archived, got %d", len(sessions))
	}
}

func TestHeartbeat(t *testing.T) {
	store := newTestStore(t)

	s, _ := store.UpsertSession("mac1", "/project/a")
	err := store.Heartbeat(s.ID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Heartbeat for nonexistent session
	err = store.Heartbeat("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}
