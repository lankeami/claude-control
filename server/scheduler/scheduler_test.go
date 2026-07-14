package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSchedulerExecutesShellTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Echo test", "shell", "echo hello-from-scheduler", "/tmp", "* * * * *", "")
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))

	s := New(store)
	s.checkAndExecuteTasks()
	time.Sleep(2 * time.Second)

	runs, err := store.ListTaskRuns(task.ID, 10)
	if err != nil {
		t.Fatalf("ListTaskRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "success" {
		t.Errorf("status: got %q, want success", runs[0].Status)
	}
	if runs[0].Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestSchedulerSkipsConcurrentExecution(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Slow task", "shell", "sleep 3", "/tmp", "* * * * *", "")
	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))

	s := New(store)
	s.checkAndExecuteTasks()
	time.Sleep(100 * time.Millisecond)

	store.UpdateTaskNextRun(task.ID, time.Now().Add(-1*time.Minute))
	s.checkAndExecuteTasks()
	time.Sleep(100 * time.Millisecond)

	runs, _ := store.ListTaskRuns(task.ID, 10)
	if len(runs) != 1 {
		t.Errorf("expected 1 run (concurrent skipped), got %d", len(runs))
	}
}

func TestTriggerExecutesShellTaskImmediately(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Manual echo", "shell", "echo triggered", "/tmp", "* * * * *", "")

	s := New(store)
	run, err := s.Trigger(*task)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run == nil || run.ID == "" {
		t.Fatal("Trigger should return the created run")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.GetTaskRunByID(run.ID)
		if got != nil && got.Status != "running" {
			if got.Status != "success" {
				t.Fatalf("status: got %q, want success (output: %s)", got.Status, got.Output)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("run never completed")
}

func TestTriggerRejectsConcurrentExecution(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Slow manual", "shell", "sleep 3", "/tmp", "* * * * *", "")

	s := New(store)
	if _, err := s.Trigger(*task); err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	if _, err := s.Trigger(*task); err != ErrAlreadyRunning {
		t.Errorf("second Trigger: got %v, want ErrAlreadyRunning", err)
	}
}

func TestEnsureManagedSessionPostsToCreateRoute(t *testing.T) {
	store := newTestStore(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// Mirror the real router: only /api/sessions/create exists.
		if r.URL.Path != "/api/sessions/create" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "sess-123"})
	}))
	defer srv.Close()

	s := New(store)
	s.SetLoopback(srv.URL, "test-key")
	task := db.ScheduledTask{ID: "t1", Name: "T", TaskType: "claude", Command: "hi", WorkingDirectory: filepath.Join(t.TempDir(), "no-session-here")}

	sessID, err := s.ensureManagedSession(task)
	if err != nil {
		t.Fatalf("ensureManagedSession: %v (posted to %q)", err, gotPath)
	}
	if sessID != "sess-123" {
		t.Errorf("session id: got %q, want sess-123", sessID)
	}
}

func TestReconcileStaleRuns(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "* * * * *", "")
	run, _ := store.CreateTaskRun(task.ID)

	s := New(store)
	s.Reconcile()

	got, _ := store.GetTaskRunByID(run.ID)
	if got.Status != "failed" {
		t.Errorf("stale run should be marked failed, got %q", got.Status)
	}
}
