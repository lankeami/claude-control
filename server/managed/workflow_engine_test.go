package managed

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestWorkflowEngine_LinearRun(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/wf-test", "[]", 50, 5.0, 0)

	var mu sync.Mutex
	var sentPrompts []string
	activityState := "idle"

	sendMessage := func(sessionID, prompt string) error {
		mu.Lock()
		sentPrompts = append(sentPrompts, prompt)
		activityState = "working"
		mu.Unlock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			activityState = "idle"
			mu.Unlock()
		}()
		return nil
	}

	getActivity := func(sessionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return activityState, nil
	}

	interrupt := func(sessionID string) error { return nil }

	engine := NewWorkflowEngine(store, sendMessage, getActivity, interrupt)

	idx1 := 1
	wf, _ := store.CreateWorkflow("Test", "", []db.WorkflowStepInput{
		{Name: "Step 1", Prompt: "Do step 1", StepOrder: 0, OnSuccessIndex: &idx1},
		{Name: "Step 2", Prompt: "Do step 2", StepOrder: 1},
	})

	steps, _ := store.GetWorkflowSteps(wf.ID)
	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	stepIDs := make([]string, len(steps))
	for i, st := range steps {
		stepIDs[i] = st.ID
	}
	store.CreateWorkflowRunSteps(run.ID, stepIDs)

	if err := engine.StartRun(run.ID); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	// Wait for completion
	deadline := time.After(5 * time.Second)
	for {
		r, _ := store.GetWorkflowRun(run.ID)
		if r.Status == "completed" || r.Status == "failed" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not complete within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	r, _ := store.GetWorkflowRun(run.ID)
	if r.Status != "completed" {
		t.Errorf("expected 'completed', got %q", r.Status)
	}

	mu.Lock()
	if len(sentPrompts) != 2 {
		t.Errorf("expected 2 prompts sent, got %d", len(sentPrompts))
	}
	if len(sentPrompts) > 0 && sentPrompts[0] != "Do step 1" {
		t.Errorf("first prompt: got %q", sentPrompts[0])
	}
	mu.Unlock()
}

func TestWorkflowEngine_ParallelRun(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Create 3 sessions for parallel execution
	sess1, _ := store.CreateManagedSession("/tmp/wf-par-1", "[]", 50, 5.0, 0)
	sess2, _ := store.CreateManagedSession("/tmp/wf-par-2", "[]", 50, 5.0, 0)
	sess3, _ := store.CreateManagedSession("/tmp/wf-par-3", "[]", 50, 5.0, 0)

	var mu sync.Mutex
	activityStates := map[string]string{
		sess1.ID: "idle",
		sess2.ID: "idle",
		sess3.ID: "idle",
	}
	var concurrentMax int
	var concurrentNow int

	sendMessage := func(sessionID, prompt string) error {
		mu.Lock()
		activityStates[sessionID] = "working"
		concurrentNow++
		if concurrentNow > concurrentMax {
			concurrentMax = concurrentNow
		}
		mu.Unlock()
		go func() {
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			activityStates[sessionID] = "idle"
			concurrentNow--
			mu.Unlock()
		}()
		return nil
	}

	getActivity := func(sessionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return activityStates[sessionID], nil
	}

	interrupt := func(sessionID string) error { return nil }
	engine := NewWorkflowEngine(store, sendMessage, getActivity, interrupt)

	// Create a pipeline run to track this
	pipelineRun, _ := store.CreatePipelineRun("parallel-test", "parallel")

	items := []ParallelItem{
		{SessionID: sess1.ID, Prompt: "Build feature 1", Label: "feat-1"},
		{SessionID: sess2.ID, Prompt: "Build feature 2", Label: "feat-2"},
		{SessionID: sess3.ID, Prompt: "Build feature 3", Label: "feat-3"},
	}

	results, err := engine.RunParallel(pipelineRun.ID, items)
	if err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if !r.Success {
			t.Errorf("item %d failed: %v", i, r.Error)
		}
	}

	// Verify concurrent execution actually happened
	mu.Lock()
	maxConcurrent := concurrentMax
	mu.Unlock()
	if maxConcurrent < 2 {
		t.Errorf("expected concurrent execution (max concurrent >= 2), got %d", maxConcurrent)
	}

	// Verify pipeline run status updated
	run, _ := store.GetPipelineRun(pipelineRun.ID)
	if run.Status != "completed" {
		t.Errorf("expected pipeline run 'completed', got %q", run.Status)
	}
}

func TestWorkflowEngine_CancelRun(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/wf-cancel", "[]", 50, 5.0, 0)

	var mu sync.Mutex
	activityState := "idle"

	sendMessage := func(sessionID, prompt string) error {
		mu.Lock()
		activityState = "working"
		mu.Unlock()
		// Simulate a long-running step — will be interrupted by cancel
		time.Sleep(2 * time.Second)
		mu.Lock()
		activityState = "idle"
		mu.Unlock()
		return nil
	}

	getActivity := func(sessionID string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return activityState, nil
	}

	interrupt := func(sessionID string) error {
		mu.Lock()
		activityState = "idle"
		mu.Unlock()
		return nil
	}

	engine := NewWorkflowEngine(store, sendMessage, getActivity, interrupt)

	wf, _ := store.CreateWorkflow("Long WF", "", []db.WorkflowStepInput{
		{Name: "Long step", Prompt: "Takes forever", StepOrder: 0},
	})
	steps, _ := store.GetWorkflowSteps(wf.ID)
	run, _ := store.CreateWorkflowRun(wf.ID, sess.ID)
	store.CreateWorkflowRunSteps(run.ID, []string{steps[0].ID})

	engine.StartRun(run.ID)
	time.Sleep(200 * time.Millisecond)
	engine.CancelRun(run.ID)

	deadline := time.After(5 * time.Second)
	for {
		r, _ := store.GetWorkflowRun(run.ID)
		if r.Status == "cancelled" || r.Status == "completed" || r.Status == "failed" {
			if r.Status != "cancelled" {
				t.Errorf("expected 'cancelled', got %q", r.Status)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not cancel within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}
