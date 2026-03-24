package scheduler

import (
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
	task, _ := store.CreateScheduledTask(sess.ID, "Echo test", "shell", "echo hello-from-scheduler", "/tmp", "* * * * *")
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
	task, _ := store.CreateScheduledTask(sess.ID, "Slow task", "shell", "sleep 3", "/tmp", "* * * * *")
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

func TestReconcileStaleRuns(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/tmp", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "* * * * *")
	run, _ := store.CreateTaskRun(task.ID)

	s := New(store)
	s.Reconcile()

	got, _ := store.GetTaskRunByID(run.ID)
	if got.Status != "failed" {
		t.Errorf("stale run should be marked failed, got %q", got.Status)
	}
}
