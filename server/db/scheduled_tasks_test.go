package db

import (
	"testing"
	"time"
)

func TestCreateAndGetScheduledTask(t *testing.T) {
	store := newTestStore(t)
	sess, err := store.UpsertSession("mac1", "/project/a", "")
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	task, err := store.CreateScheduledTask(sess.ID, "Daily backup", "shell", "tar -czf backup.tar.gz .", "/tmp/project", "0 2 * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.Name != "Daily backup" {
		t.Errorf("name: got %q, want %q", task.Name, "Daily backup")
	}
	if task.TaskType != "shell" {
		t.Errorf("task_type: got %q, want %q", task.TaskType, "shell")
	}
	if task.Command != "tar -czf backup.tar.gz ." {
		t.Errorf("command: got %q", task.Command)
	}
	if !task.Enabled {
		t.Error("expected enabled=true")
	}

	got, err := store.GetScheduledTaskByID(task.ID)
	if err != nil {
		t.Fatalf("GetScheduledTaskByID: %v", err)
	}
	if got.ID != task.ID {
		t.Errorf("id mismatch: got %q, want %q", got.ID, task.ID)
	}
}

func TestCreateScheduledTaskWithoutSession(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "Shell task", "shell", "echo hello", "/tmp", "*/5 * * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.SessionID != nil {
		t.Errorf("expected nil session_id, got %v", task.SessionID)
	}
}

func TestListScheduledTasks(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	store.CreateScheduledTask(sess.ID, "Task A", "shell", "echo a", "/tmp", "0 * * * *", "")
	store.CreateScheduledTask(sess.ID, "Task B", "claude", "summarize", "/tmp", "0 9 * * *", "")
	store.CreateScheduledTask("", "Task C", "shell", "echo c", "/tmp", "*/5 * * * *", "")

	all, err := store.ListScheduledTasks("")
	if err != nil {
		t.Fatalf("ListScheduledTasks all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(all))
	}

	bySession, err := store.ListScheduledTasks(sess.ID)
	if err != nil {
		t.Fatalf("ListScheduledTasks session: %v", err)
	}
	if len(bySession) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(bySession))
	}
}

func TestUpdateScheduledTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Old Name", "shell", "echo old", "/tmp", "0 * * * *", "")

	err := store.UpdateScheduledTask(task.ID, "New Name", "shell", "echo new", "/tmp/new", "0 2 * * *", "", false)
	if err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}

	got, _ := store.GetScheduledTaskByID(task.ID)
	if got.Name != "New Name" {
		t.Errorf("name: got %q, want %q", got.Name, "New Name")
	}
	if got.Command != "echo new" {
		t.Errorf("command: got %q", got.Command)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDeleteScheduledTask(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "To Delete", "shell", "echo x", "/tmp", "0 * * * *", "")

	err := store.DeleteScheduledTask(task.ID)
	if err != nil {
		t.Fatalf("DeleteScheduledTask: %v", err)
	}

	got, _ := store.GetScheduledTaskByID(task.ID)
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestTaskRunLifecycle(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *", "")

	run, err := store.CreateTaskRun(task.ID)
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if run.Status != "running" {
		t.Errorf("status: got %q, want %q", run.Status, "running")
	}

	err = store.CompleteTaskRun(run.ID, 0, "hello\n")
	if err != nil {
		t.Fatalf("CompleteTaskRun: %v", err)
	}

	got, err := store.GetTaskRunByID(run.ID)
	if err != nil {
		t.Fatalf("GetTaskRunByID: %v", err)
	}
	if got.Status != "success" {
		t.Errorf("status: got %q, want %q", got.Status, "success")
	}
	if *got.ExitCode != 0 {
		t.Errorf("exit_code: got %d, want 0", *got.ExitCode)
	}

	runs, err := store.ListTaskRuns(task.ID, 20)
	if err != nil {
		t.Fatalf("ListTaskRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}
}

func TestCompleteTaskRunFailed(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "exit 1", "/tmp", "0 * * * *", "")

	run, _ := store.CreateTaskRun(task.ID)
	store.CompleteTaskRun(run.ID, 1, "error output")

	got, _ := store.GetTaskRunByID(run.ID)
	if got.Status != "failed" {
		t.Errorf("status: got %q, want %q", got.Status, "failed")
	}
}

func TestGetTasksDueForExecution(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	task1, _ := store.CreateScheduledTask(sess.ID, "Due", "shell", "echo a", "/tmp", "0 * * * *", "")
	task2, _ := store.CreateScheduledTask(sess.ID, "Not Due", "shell", "echo b", "/tmp", "0 * * * *", "")

	past := time.Now().Add(-1 * time.Minute)
	future := time.Now().Add(1 * time.Hour)
	store.UpdateTaskNextRun(task1.ID, past)
	store.UpdateTaskNextRun(task2.ID, future)

	due, err := store.GetTasksDueForExecution(time.Now())
	if err != nil {
		t.Fatalf("GetTasksDueForExecution: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due task, got %d", len(due))
	}
	if due[0].ID != task1.ID {
		t.Errorf("wrong task: got %q, want %q", due[0].ID, task1.ID)
	}
}

func TestCascadeDeleteTaskRuns(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *", "")
	store.CreateTaskRun(task.ID)

	store.DeleteScheduledTask(task.ID)
	runs, _ := store.ListTaskRuns(task.ID, 20)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after cascade delete, got %d", len(runs))
	}
}

func TestScheduledTaskModelField(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "Model task", "claude", "summarize", "/tmp", "0 9 * * *", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q, want %q", task.Model, "claude-opus-4-6")
	}
	task2, err := store.CreateScheduledTask("", "No model", "shell", "echo hi", "/tmp", "0 * * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task2.Model != "" {
		t.Errorf("model: got %q, want empty", task2.Model)
	}
}

func rawTaskColumn(t *testing.T, store *Store, column, taskID string) string {
	t.Helper()
	var raw string
	if err := store.db.QueryRow("SELECT CAST("+column+" AS TEXT) FROM scheduled_tasks WHERE id = ?", taskID).Scan(&raw); err != nil {
		t.Fatalf("read raw %s: %v", column, err)
	}
	return raw
}

func TestUpdateTaskNextRunStoresCanonicalUTC(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "TZ task", "shell", "echo hi", "/tmp", "31 9 * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	pdt := time.FixedZone("PDT", -7*3600)
	next := time.Date(2026, 8, 10, 9, 31, 0, 0, pdt) // 16:31 UTC
	if err := store.UpdateTaskNextRun(task.ID, next); err != nil {
		t.Fatalf("UpdateTaskNextRun: %v", err)
	}

	raw := rawTaskColumn(t, store, "next_run_at", task.ID)
	if raw != "2026-08-10 16:31:00" {
		t.Errorf("next_run_at raw value: got %q, want canonical UTC %q", raw, "2026-08-10 16:31:00")
	}

	got, err := store.GetScheduledTaskByID(task.ID)
	if err != nil {
		t.Fatalf("GetScheduledTaskByID: %v", err)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(next) {
		t.Errorf("next_run_at round-trip: got %v, want instant %v", got.NextRunAt, next)
	}
}

func TestUpdateTaskLastRunStoresCanonicalUTC(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "TZ task", "shell", "echo hi", "/tmp", "31 9 * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	edt := time.FixedZone("EDT", -4*3600)
	last := time.Date(2026, 8, 10, 12, 31, 42, 0, edt) // 16:31:42 UTC
	if err := store.UpdateTaskLastRun(task.ID, last); err != nil {
		t.Fatalf("UpdateTaskLastRun: %v", err)
	}

	raw := rawTaskColumn(t, store, "last_run_at", task.ID)
	if raw != "2026-08-10 16:31:42" {
		t.Errorf("last_run_at raw value: got %q, want canonical UTC %q", raw, "2026-08-10 16:31:42")
	}

	got, err := store.GetScheduledTaskByID(task.ID)
	if err != nil {
		t.Fatalf("GetScheduledTaskByID: %v", err)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(last) {
		t.Errorf("last_run_at round-trip: got %v, want instant %v", got.LastRunAt, last)
	}
}

// Regression for the 3-hour-late firing: a next_run_at written by a process
// in one timezone must compare correctly against a "now" expressed in another.
func TestGetTasksDueForExecutionCrossTimezone(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "Cross TZ", "shell", "echo hi", "/tmp", "31 9 * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	edt := time.FixedZone("EDT", -4*3600)
	pdt := time.FixedZone("PDT", -7*3600)
	due := time.Date(2026, 8, 10, 9, 31, 0, 0, edt) // 13:31 UTC
	if err := store.UpdateTaskNextRun(task.ID, due); err != nil {
		t.Fatalf("UpdateTaskNextRun: %v", err)
	}

	tasks, err := store.GetTasksDueForExecution(due.Add(time.Minute).In(pdt))
	if err != nil {
		t.Fatalf("GetTasksDueForExecution: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("one minute past due (cross-zone now): got %d tasks, want 1", len(tasks))
	}

	tasks, err = store.GetTasksDueForExecution(due.Add(-time.Minute).In(pdt))
	if err != nil {
		t.Fatalf("GetTasksDueForExecution: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("one minute before due (cross-zone now): got %d tasks, want 0", len(tasks))
	}
}
