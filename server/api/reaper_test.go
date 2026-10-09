package api

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// Issue #314: every reap must leave a durable system message row.
func TestReapInsertsSystemMessage(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession(t.TempDir(), "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	mgr := managed.NewManager(managed.Config{ClaudeBin: "cat"})
	WireReaper(mgr, store)

	proc, err := mgr.EnsureProcess(sess.ID, managed.SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	proc.LastActivity = time.Now().Add(-1 * time.Hour)

	mgr.ReapIdle(30 * time.Minute)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("process was not reaped")
	}

	// The reap callback runs synchronously inside ReapIdle, but poll briefly
	// to stay robust if it ever moves to a goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for {
		msgs, err := store.ListMessages(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range msgs {
			if m.Role == "system" {
				if !strings.Contains(m.Content, sess.ID) {
					t.Errorf("system message %q does not name the session %s", m.Content, sess.ID)
				}
				if !strings.Contains(m.Content, "idle") {
					t.Errorf("system message %q does not mention idle duration", m.Content)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no system message recorded for reaped session; got %d messages", len(msgs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WireReaper must also give the manager a background-activity probe that keeps
// a session with no claude transcript mapping reapable (zero time).
func TestWireReaperSetsBackgroundProbe(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	mgr := managed.NewManager(managed.Config{ClaudeBin: "cat"})
	WireReaper(mgr, store)

	// Unknown session: probe must return the zero time, not panic.
	if got := mgr.BackgroundActivity("no-such-session"); !got.IsZero() {
		t.Errorf("probe for unknown session = %v, want zero time", got)
	}
}
