package db

import (
	"path/filepath"
	"testing"
)

func TestCreateWorkflowRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/test", "[]", 50, 5.0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	wf, _ := store.CreateWorkflow("Test WF", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p1", StepOrder: 0},
	})

	run, err := store.CreateWorkflowRun(wf.ID, sess.ID)
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	if run.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", run.Status)
	}
	if run.SessionID != sess.ID {
		t.Errorf("expected session_id %q, got %q", sess.ID, run.SessionID)
	}
}

func TestGetActiveRunForSession(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test2", "[]", 50, 5.0, 0)
	wf, _ := store.CreateWorkflow("WF", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p", StepOrder: 0},
	})

	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	store.UpdateWorkflowRunStatus(run.ID, "running", nil)

	active, err := store.GetActiveRunForSession(sess.ID)
	if err != nil {
		t.Fatalf("GetActiveRunForSession: %v", err)
	}
	if active == nil || active.ID != run.ID {
		t.Error("expected to find the active run")
	}
}

func TestCreateWorkflow_HappyPath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	steps := []WorkflowStepInput{
		{Name: "Run tests", Prompt: "Run all Go tests", StepOrder: 0, MaxRetries: 1},
		{Name: "Fix failures", Prompt: "Fix any test failures", StepOrder: 1},
	}
	// Auto-wire: step 0 on_success -> step 1
	idx1 := 1
	steps[0].OnSuccessIndex = &idx1

	wf, err := store.CreateWorkflow("CI Pipeline", "Run tests and fix", steps)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.ID == "" {
		t.Fatal("expected workflow ID")
	}
	if wf.Name != "CI Pipeline" {
		t.Errorf("expected name 'CI Pipeline', got %q", wf.Name)
	}

	got, err := store.GetWorkflowSteps(wf.ID)
	if err != nil {
		t.Fatalf("GetWorkflowSteps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(got))
	}
	if got[0].Name != "Run tests" {
		t.Errorf("step 0 name: got %q", got[0].Name)
	}
	if got[0].OnSuccess == nil || *got[0].OnSuccess != got[1].ID {
		t.Error("step 0 on_success should point to step 1")
	}
	if got[0].MaxRetries != 1 {
		t.Errorf("step 0 max_retries: got %d", got[0].MaxRetries)
	}
	if got[1].OnSuccess != nil {
		t.Error("step 1 on_success should be nil (end of workflow)")
	}
}

func TestListWorkflows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	store.CreateWorkflow("WF1", "", nil)
	store.CreateWorkflow("WF2", "", nil)

	wfs, err := store.ListWorkflows()
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(wfs))
	}
}

func TestUpdateWorkflow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	wf, _ := store.CreateWorkflow("Original", "desc", []WorkflowStepInput{
		{Name: "Step1", Prompt: "do thing", StepOrder: 0},
	})

	newSteps := []WorkflowStepInput{
		{Name: "New Step", Prompt: "new prompt", StepOrder: 0},
		{Name: "Step 2", Prompt: "second", StepOrder: 1},
	}
	if err := store.UpdateWorkflow(wf.ID, "Updated", "new desc", newSteps); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	updated, _ := store.GetWorkflow(wf.ID)
	if updated.Name != "Updated" {
		t.Errorf("expected 'Updated', got %q", updated.Name)
	}
	steps, _ := store.GetWorkflowSteps(wf.ID)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps after update, got %d", len(steps))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	wf, _ := store.CreateWorkflow("ToDelete", "", []WorkflowStepInput{
		{Name: "S1", Prompt: "p", StepOrder: 0},
	})
	if err := store.DeleteWorkflow(wf.ID); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	got, _ := store.GetWorkflow(wf.ID)
	if got != nil {
		t.Error("expected workflow to be deleted")
	}
	steps, _ := store.GetWorkflowSteps(wf.ID)
	if len(steps) != 0 {
		t.Error("expected steps to be cascade-deleted")
	}
}
