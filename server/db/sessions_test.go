package db

import (
	"fmt"
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

	sess, err := store.CreateManagedSession("/tmp/project", `["Bash","Read"]`, 50, 5.0, 0)
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

	_, err = store.CreateManagedSession("/tmp/project", `["Bash"]`, 50, 5.0, 0)
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
	sess, err := store.CreateManagedSession("/tmp/test-turns", `["Bash"]`, 50, 5.0, 0)
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
	sess, _ := store.CreateManagedSession("/tmp/test-resume-turns", `["Bash"]`, 50, 5.0, 0)

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

func TestAutoContinueDefaults(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-ac", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sess.AutoContinueThreshold != 0.8 {
		t.Errorf("expected threshold 0.8, got %f", sess.AutoContinueThreshold)
	}
	if sess.MaxContinuations != 5 {
		t.Errorf("expected max_continuations 5, got %d", sess.MaxContinuations)
	}
}

func TestUpdateActivityState(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.CreateManagedSession("/tmp/test-activity", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActivityState != "idle" {
		t.Errorf("expected initial activity_state='idle', got %q", sess.ActivityState)
	}
	if err := store.UpdateActivityState(sess.ID, "working"); err != nil {
		t.Fatalf("update to working: %v", err)
	}
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.ActivityState != "working" {
		t.Errorf("expected activity_state='working', got %q", updated.ActivityState)
	}
	if err := store.UpdateActivityState(sess.ID, "waiting"); err != nil {
		t.Fatalf("update to waiting: %v", err)
	}
	updated, _ = store.GetSessionByID(sess.ID)
	if updated.ActivityState != "waiting" {
		t.Errorf("expected activity_state='waiting', got %q", updated.ActivityState)
	}
	if err := store.UpdateActivityState(sess.ID, "idle"); err != nil {
		t.Fatalf("update to idle: %v", err)
	}
	updated, _ = store.GetSessionByID(sess.ID)
	if updated.ActivityState != "idle" {
		t.Errorf("expected activity_state='idle', got %q", updated.ActivityState)
	}
}

func TestRecentDirectories(t *testing.T) {
	store := newTestStore(t)

	// Empty store returns empty slice
	dirs, err := store.RecentDirectories(5)
	if err != nil {
		t.Fatalf("RecentDirectories: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs, got %d", len(dirs))
	}

	// Create some managed sessions
	store.CreateManagedSession("/projects/alpha", `["Bash"]`, 50, 5.0, 0)
	store.CreateManagedSession("/projects/beta", `["Bash"]`, 50, 5.0, 0)

	dirs, err = store.RecentDirectories(5)
	if err != nil {
		t.Fatalf("RecentDirectories: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	// Most recent first
	if dirs[0].Path != "/projects/beta" {
		t.Errorf("dirs[0].Path = %s, want /projects/beta", dirs[0].Path)
	}
	if dirs[0].Name != "beta" {
		t.Errorf("dirs[0].Name = %s, want beta", dirs[0].Name)
	}
	if dirs[1].Path != "/projects/alpha" {
		t.Errorf("dirs[1].Path = %s, want /projects/alpha", dirs[1].Path)
	}
}

func TestRecentDirectories_Limit(t *testing.T) {
	store := newTestStore(t)

	for i := 0; i < 7; i++ {
		store.CreateManagedSession(fmt.Sprintf("/projects/p%d", i), `["Bash"]`, 50, 5.0, 0)
	}

	dirs, err := store.RecentDirectories(5)
	if err != nil {
		t.Fatalf("RecentDirectories: %v", err)
	}
	if len(dirs) != 5 {
		t.Errorf("expected 5 dirs, got %d", len(dirs))
	}
}

func TestRecentDirectories_IncludesArchived(t *testing.T) {
	store := newTestStore(t)

	sess, _ := store.CreateManagedSession("/projects/archived-proj", `["Bash"]`, 50, 5.0, 0)
	store.SetArchived(sess.ID, true)

	dirs, err := store.RecentDirectories(5)
	if err != nil {
		t.Fatalf("RecentDirectories: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir (archived included), got %d", len(dirs))
	}
	if dirs[0].Path != "/projects/archived-proj" {
		t.Errorf("path = %s, want /projects/archived-proj", dirs[0].Path)
	}
}

func TestRecentDirectories_DeduplicatesCWD(t *testing.T) {
	store := newTestStore(t)

	// Create first session, then delete it so the unique constraint allows a second
	sess1, _ := store.CreateManagedSession("/projects/same", `["Bash"]`, 50, 5.0, 0)
	store.DeleteSession(sess1.ID)
	store.CreateManagedSession("/projects/same", `["Bash"]`, 50, 5.0, 0)

	dirs, err := store.RecentDirectories(5)
	if err != nil {
		t.Fatalf("RecentDirectories: %v", err)
	}
	if len(dirs) != 1 {
		t.Errorf("expected 1 deduplicated dir, got %d", len(dirs))
	}
}

func TestResetStaleActivityStates(t *testing.T) {
	store := newTestStore(t)
	s1, _ := store.CreateManagedSession("/tmp/stale1", `["Bash"]`, 50, 5.0, 0)
	s2, _ := store.CreateManagedSession("/tmp/stale2", `["Bash"]`, 50, 5.0, 0)
	store.UpdateActivityState(s1.ID, "working")
	store.UpdateActivityState(s2.ID, "waiting")
	if err := store.ResetStaleActivityStates(); err != nil {
		t.Fatalf("reset stale: %v", err)
	}
	got1, _ := store.GetSessionByID(s1.ID)
	if got1.ActivityState != "idle" {
		t.Errorf("s1: expected 'idle', got %q", got1.ActivityState)
	}
	got2, _ := store.GetSessionByID(s2.ID)
	if got2.ActivityState != "waiting" {
		t.Errorf("s2: expected 'waiting' (unchanged), got %q", got2.ActivityState)
	}
}
