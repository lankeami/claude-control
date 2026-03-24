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

	task, err := store.CreateScheduledTask(sess.ID, "Daily backup", "shell", "tar -czf backup.tar.gz .", "/tmp/project", "0 2 * * *")
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
	task, err := store.CreateScheduledTask("", "Shell task", "shell", "echo hello", "/tmp", "*/5 * * * *")
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
	store.CreateScheduledTask(sess.ID, "Task A", "shell", "echo a", "/tmp", "0 * * * *")
	store.CreateScheduledTask(sess.ID, "Task B", "claude", "summarize", "/tmp", "0 9 * * *")
	store.CreateScheduledTask("", "Task C", "shell", "echo c", "/tmp", "*/5 * * * *")

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
	task, _ := store.CreateScheduledTask(sess.ID, "Old Name", "shell", "echo old", "/tmp", "0 * * * *")

	err := store.UpdateScheduledTask(task.ID, "New Name", "shell", "echo new", "/tmp/new", "0 2 * * *", false)
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
	task, _ := store.CreateScheduledTask(sess.ID, "To Delete", "shell", "echo x", "/tmp", "0 * * * *")

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
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *")

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
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "exit 1", "/tmp", "0 * * * *")

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

	task1, _ := store.CreateScheduledTask(sess.ID, "Due", "shell", "echo a", "/tmp", "0 * * * *")
	task2, _ := store.CreateScheduledTask(sess.ID, "Not Due", "shell", "echo b", "/tmp", "0 * * * *")

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
	task, _ := store.CreateScheduledTask(sess.ID, "Task", "shell", "echo hi", "/tmp", "0 * * * *")
	store.CreateTaskRun(task.ID)

	store.DeleteScheduledTask(task.ID)
	runs, _ := store.ListTaskRuns(task.ID, 20)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after cascade delete, got %d", len(runs))
	}
}
