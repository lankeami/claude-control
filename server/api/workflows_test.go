package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestCreateWorkflow_HappyPath(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name":        "CI Pipeline",
		"description": "Run tests and fix",
		"steps": []map[string]interface{}{
			{"name": "Run tests", "prompt": "Run go test ./...", "step_order": 0, "on_success_index": 1},
			{"name": "Create PR", "prompt": "Create a PR", "step_order": 1},
		},
	}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result struct {
		Workflow db.Workflow       `json:"workflow"`
		Steps    []db.WorkflowStep `json:"steps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Workflow.Name != "CI Pipeline" {
		t.Errorf("expected name 'CI Pipeline', got %q", result.Workflow.Name)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}
}

func TestCreateWorkflow_MissingName(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{"steps": []map[string]interface{}{}}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListWorkflows(t *testing.T) {
	ts, store := newTestServer(t)
	store.CreateWorkflow("WF1", "", nil)
	store.CreateWorkflow("WF2", "", nil)

	req := authReq("GET", ts.URL+"/api/workflows", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var wfs []db.Workflow
	json.NewDecoder(resp.Body).Decode(&wfs)
	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(wfs))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	ts, store := newTestServer(t)
	wf, _ := store.CreateWorkflow("ToDelete", "", nil)

	req := authReq("DELETE", ts.URL+"/api/workflows/"+wf.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestWorkflowRunLifecycle(t *testing.T) {
	ts, store := newTestServer(t)

	// Create a managed session
	sess, err := store.CreateManagedSession("/tmp/wf-lifecycle", "[]", 50, 5.0, 0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Create workflow via API
	body := map[string]interface{}{
		"name": "Test WF",
		"steps": []map[string]interface{}{
			{"name": "Step 1", "prompt": "Do thing", "step_order": 0},
		},
	}
	req := authReq("POST", ts.URL+"/api/workflows", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create workflow request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create workflow: expected 201, got %d", resp.StatusCode)
	}

	var createResult struct {
		Workflow db.Workflow `json:"workflow"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// List workflows
	req = authReq("GET", ts.URL+"/api/workflows", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list workflows request: %v", err)
	}
	defer resp.Body.Close()
	var wfs []db.Workflow
	json.NewDecoder(resp.Body).Decode(&wfs)
	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(wfs))
	}

	// Get workflow detail
	req = authReq("GET", ts.URL+"/api/workflows/"+createResult.Workflow.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get workflow request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get workflow: expected 200, got %d", resp.StatusCode)
	}

	// Start run (may fail since test server has no real managed session manager,
	// but tests that the API layer processes the request without panicking)
	runBody := map[string]interface{}{"session_id": sess.ID}
	req = authReq("POST", ts.URL+"/api/workflows/"+createResult.Workflow.ID+"/run", runBody)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("start run request: %v", err)
	}
	defer resp.Body.Close()
	t.Logf("run response status: %d", resp.StatusCode)

	// List runs — should return an empty array or the run we just created
	req = authReq("GET", ts.URL+"/api/workflow-runs?session_id="+sess.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list runs request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list runs: expected 200, got %d", resp.StatusCode)
	}
}
