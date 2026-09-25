package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestCreatePipelineRun_API(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name": "autoship-batch",
		"mode": "parallel",
		"items": []map[string]string{
			{"feature_label": "Add dark mode"},
			{"feature_label": "Add search"},
		},
	}
	req := authReq("POST", ts.URL+"/api/pipeline-runs", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result struct {
		Run   db.PipelineRun       `json:"run"`
		Items []db.PipelineRunItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Run.Name != "autoship-batch" {
		t.Errorf("expected name 'autoship-batch', got %q", result.Run.Name)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
}

func TestListPipelineRuns_API(t *testing.T) {
	ts, store := newTestServer(t)
	store.CreatePipelineRun("run1", "parallel", "")
	store.CreatePipelineRun("run2", "parallel", "")

	req := authReq("GET", ts.URL+"/api/pipeline-runs", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var runs []db.PipelineRun
	json.NewDecoder(resp.Body).Decode(&runs)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestGetPipelineRun_API(t *testing.T) {
	ts, store := newTestServer(t)
	run, _ := store.CreatePipelineRun("detail-test", "parallel", "")
	store.CreatePipelineRunItem(run.ID, "feat-1", "")
	store.CreatePipelineRunItem(run.ID, "feat-2", "")

	req := authReq("GET", ts.URL+"/api/pipeline-runs/"+run.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Run   db.PipelineRun       `json:"run"`
		Items []db.PipelineRunItem `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Run.Name != "detail-test" {
		t.Errorf("expected 'detail-test', got %q", result.Run.Name)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
}

func TestUpdatePipelineRunItem_API(t *testing.T) {
	ts, store := newTestServer(t)
	run, _ := store.CreatePipelineRun("update-test", "parallel", "")
	item, _ := store.CreatePipelineRunItem(run.ID, "feat-1", "")

	body := map[string]string{"status": "completed"}
	req := authReq("PATCH", ts.URL+"/api/pipeline-run-items/"+item.ID, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWorkflowVisibility(t *testing.T) {
	ts, store := newTestServer(t)

	// When autoship creates a pipeline run, it should be visible via both
	// the pipeline-runs endpoint AND listed alongside workflows
	run, _ := store.CreatePipelineRun("autoship: 3 features", "parallel", "/tmp/test-repo")
	store.CreatePipelineRunItem(run.ID, "Add dark mode", "")
	store.CreatePipelineRunItem(run.ID, "Add search", "")
	store.CreatePipelineRunItem(run.ID, "Add export", "")

	// Pipeline runs endpoint shows the run
	req := authReq("GET", ts.URL+"/api/pipeline-runs", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var runs []db.PipelineRun
	json.NewDecoder(resp.Body).Decode(&runs)
	if len(runs) != 1 {
		t.Fatalf("expected 1 pipeline run visible, got %d", len(runs))
	}
	if runs[0].Name != "autoship: 3 features" {
		t.Errorf("expected 'autoship: 3 features', got %q", runs[0].Name)
	}

	// Detail endpoint shows items
	req = authReq("GET", ts.URL+"/api/pipeline-runs/"+run.ID, nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("detail request: %v", err)
	}
	defer resp2.Body.Close()

	var detail struct {
		Run   db.PipelineRun       `json:"run"`
		Items []db.PipelineRunItem `json:"items"`
	}
	json.NewDecoder(resp2.Body).Decode(&detail)
	if len(detail.Items) != 3 {
		t.Fatalf("expected 3 items in detail, got %d", len(detail.Items))
	}
}
