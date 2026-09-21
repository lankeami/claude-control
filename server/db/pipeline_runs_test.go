package db

import (
	"path/filepath"
	"testing"
)

func TestCreatePipelineRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	run, err := store.CreatePipelineRun("autoship-batch", "parallel")
	if err != nil {
		t.Fatalf("CreatePipelineRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if run.Name != "autoship-batch" {
		t.Errorf("expected name 'autoship-batch', got %q", run.Name)
	}
	if run.Status != "running" {
		t.Errorf("expected status 'running', got %q", run.Status)
	}
	if run.Mode != "parallel" {
		t.Errorf("expected mode 'parallel', got %q", run.Mode)
	}
}

func TestPipelineRunItems(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	run, _ := store.CreatePipelineRun("test-pipeline", "parallel")

	sess1, _ := store.CreateManagedSession("/tmp/pr-1", "[]", 50, 5.0, 0)
	sess2, _ := store.CreateManagedSession("/tmp/pr-2", "[]", 50, 5.0, 0)

	item1, err := store.CreatePipelineRunItem(run.ID, "Add dark mode", sess1.ID)
	if err != nil {
		t.Fatalf("CreatePipelineRunItem 1: %v", err)
	}
	item2, err := store.CreatePipelineRunItem(run.ID, "Add search", sess2.ID)
	if err != nil {
		t.Fatalf("CreatePipelineRunItem 2: %v", err)
	}

	if item1.FeatureLabel != "Add dark mode" {
		t.Errorf("item1 label: got %q", item1.FeatureLabel)
	}
	if item1.Status != "pending" {
		t.Errorf("item1 status: got %q", item1.Status)
	}

	items, err := store.GetPipelineRunItems(run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRunItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Update item status
	if err := store.UpdatePipelineRunItemStatus(item2.ID, "completed", nil); err != nil {
		t.Fatalf("UpdatePipelineRunItemStatus: %v", err)
	}

	items, _ = store.GetPipelineRunItems(run.ID)
	for _, it := range items {
		if it.ID == item2.ID && it.Status != "completed" {
			t.Errorf("expected item2 completed, got %q", it.Status)
		}
	}
}

func TestListPipelineRuns(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	store.CreatePipelineRun("run1", "parallel")
	store.CreatePipelineRun("run2", "parallel")

	runs, err := store.ListPipelineRuns()
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestUpdatePipelineRunStatus(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	run, _ := store.CreatePipelineRun("done-test", "parallel")
	if err := store.UpdatePipelineRunStatus(run.ID, "completed", nil); err != nil {
		t.Fatalf("UpdatePipelineRunStatus: %v", err)
	}

	got, _ := store.GetPipelineRun(run.ID)
	if got.Status != "completed" {
		t.Errorf("expected 'completed', got %q", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("expected finished_at to be set")
	}
}
