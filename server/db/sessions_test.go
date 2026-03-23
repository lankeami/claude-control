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
	s1, err := store.UpsertSession("mac1", "/project/a", "")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if s1.ComputerName != "mac1" || s1.ProjectPath != "/project/a" {
		t.Errorf("unexpected session: %+v", s1)
	}

	// Second upsert returns same ID, updates last_seen_at
	s2, err := store.UpsertSession("mac1", "/project/a", "")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if s2.ID != s1.ID {
		t.Errorf("expected same ID %s, got %s", s1.ID, s2.ID)
	}

	// Different project creates new session
	s3, err := store.UpsertSession("mac1", "/project/b", "")
	if err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	if s3.ID == s1.ID {
		t.Error("expected different ID for different project")
	}
}

func TestListSessions(t *testing.T) {
	store := newTestStore(t)

	store.UpsertSession("mac1", "/project/a", "")
	store.UpsertSession("mac1", "/project/b", "")

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

	s, _ := store.UpsertSession("mac1", "/project/a", "")
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

func TestCreateManagedSession(t *testing.T) {
	store := newTestStore(t)

	sess, err := store.CreateManagedSession("/tmp/project", `["Bash","Read"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Mode != "managed" {
		t.Errorf("mode=%s, want managed", sess.Mode)
	}
	if sess.CWD != "/tmp/project" {
		t.Errorf("cwd=%s, want /tmp/project", sess.CWD)
	}
	if sess.Initialized {
		t.Error("new session should not be initialized")
	}

	_, err = store.CreateManagedSession("/tmp/project", `["Bash"]`, 50, 5.0)
	if err == nil {
		t.Error("expected error for duplicate cwd, got nil")
	}
}

func TestHeartbeat(t *testing.T) {
	store := newTestStore(t)

	s, _ := store.UpsertSession("mac1", "/project/a", "")
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

func TestTurnCount(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-turns", `["Bash"]`, 50, 5.0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Initial turn_count is 0
	if sess.TurnCount != 0 {
		t.Errorf("expected initial turn_count=0, got %d", sess.TurnCount)
	}

	// Increment returns new count
	count, err := store.IncrementTurnCount(sess.ID)
	if err != nil {
		t.Fatalf("first increment: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	count, err = store.IncrementTurnCount(sess.ID)
	if err != nil {
		t.Fatalf("second increment: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}

	// Verify persisted via GetSessionByID
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.TurnCount != 2 {
		t.Errorf("expected persisted turn_count=2, got %d", updated.TurnCount)
	}

	// Reset
	if err := store.ResetTurnCount(sess.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	reset, _ := store.GetSessionByID(sess.ID)
	if reset.TurnCount != 0 {
		t.Errorf("expected turn_count=0 after reset, got %d", reset.TurnCount)
	}
}

func TestResumeSessionResetsTurnCount(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.CreateManagedSession("/tmp/test-resume-turns", `["Bash"]`, 50, 5.0)

	// Increment some turns
	store.IncrementTurnCount(sess.ID)
	store.IncrementTurnCount(sess.ID)

	// Resume should reset turn_count
	err := store.ResumeSession(sess.ID, "new-claude-session-id")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumed, _ := store.GetSessionByID(sess.ID)
	if resumed.TurnCount != 0 {
		t.Errorf("expected turn_count=0 after resume, got %d", resumed.TurnCount)
	}
}
