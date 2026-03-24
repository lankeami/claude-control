package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestCreateTask_HappyPath(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name":              "Test Task",
		"task_type":         "shell",
		"command":           "echo hello",
		"working_directory": "/tmp",
		"cron_expression":   "* * * * *",
	}
	req := authReq("POST", ts.URL+"/api/tasks", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var task db.ScheduledTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected task ID to be set")
	}
	if task.Name != "Test Task" {
		t.Errorf("expected name 'Test Task', got %q", task.Name)
	}
	if task.NextRunAt == nil {
		t.Error("expected next_run_at to be set")
	}
}

func TestCreateTask_InvalidCron(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]interface{}{
		"name":              "Bad Cron",
		"task_type":         "shell",
		"command":           "echo hello",
		"working_directory": "/tmp",
		"cron_expression":   "not-a-cron",
	}
	req := authReq("POST", ts.URL+"/api/tasks", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateTask_MissingFields(t *testing.T) {
	ts, _ := newTestServer(t)

	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing name", map[string]interface{}{"task_type": "shell", "command": "echo", "working_directory": "/tmp", "cron_expression": "* * * * *"}},
		{"missing command", map[string]interface{}{"name": "x", "task_type": "shell", "working_directory": "/tmp", "cron_expression": "* * * * *"}},
		{"missing working_directory", map[string]interface{}{"name": "x", "task_type": "shell", "command": "echo", "cron_expression": "* * * * *"}},
		{"invalid task_type", map[string]interface{}{"name": "x", "task_type": "invalid", "command": "echo", "working_directory": "/tmp", "cron_expression": "* * * * *"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authReq("POST", ts.URL+"/api/tasks", tc.body)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestListTasks(t *testing.T) {
	ts, store := newTestServer(t)

	// Create a task directly via store
	_, err := store.CreateScheduledTask("", "Task A", "shell", "ls", "/tmp", "0 * * * *")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}

	req := authReq("GET", ts.URL+"/api/tasks", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tasks []db.ScheduledTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "Task A" {
		t.Errorf("expected name 'Task A', got %q", tasks[0].Name)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	ts, _ := newTestServer(t)

	req := authReq("GET", ts.URL+"/api/tasks/nonexistent-id", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTaskFullLifecycle(t *testing.T) {
	ts, _ := newTestServer(t)

	// Create
	createBody := map[string]interface{}{
		"name":              "Lifecycle Task",
		"task_type":         "shell",
		"command":           "echo lifecycle",
		"working_directory": "/tmp",
		"cron_expression":   "*/5 * * * *",
	}
	req := authReq("POST", ts.URL+"/api/tasks", createBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	var task db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&task)
	taskID := task.ID

	// Get
	req = authReq("GET", ts.URL+"/api/tasks/"+taskID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}

	// Update
	updateBody := map[string]interface{}{
		"name":              "Updated Task",
		"task_type":         "claude",
		"command":           "summarize logs",
		"working_directory": "/var/log",
		"cron_expression":   "0 9 * * *",
		"enabled":           true,
	}
	req = authReq("PUT", ts.URL+"/api/tasks/"+taskID, updateBody)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", resp.StatusCode)
	}
	var updated db.ScheduledTask
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated.Name != "Updated Task" {
		t.Errorf("expected name 'Updated Task', got %q", updated.Name)
	}
	if updated.TaskType != "claude" {
		t.Errorf("expected task_type 'claude', got %q", updated.TaskType)
	}
	if !updated.Enabled {
		t.Error("expected enabled=true after update")
	}

	// Trigger
	req = authReq("POST", ts.URL+"/api/tasks/"+taskID+"/trigger", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("trigger request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("trigger: expected 201, got %d", resp.StatusCode)
	}
	var run db.TaskRun
	json.NewDecoder(resp.Body).Decode(&run)
	if run.ID == "" {
		t.Fatal("expected run ID to be set")
	}
	if run.TaskID != taskID {
		t.Errorf("expected task_id %q, got %q", taskID, run.TaskID)
	}
	if run.Status != "running" {
		t.Errorf("expected status 'running', got %q", run.Status)
	}

	// List runs
	req = authReq("GET", ts.URL+"/api/tasks/"+taskID+"/runs", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list runs request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list runs: expected 200, got %d", resp.StatusCode)
	}
	var runs []db.TaskRun
	json.NewDecoder(resp.Body).Decode(&runs)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	// Get run
	req = authReq("GET", ts.URL+"/api/tasks/"+taskID+"/runs/"+run.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get run request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get run: expected 200, got %d", resp.StatusCode)
	}

	// Delete
	req = authReq("DELETE", ts.URL+"/api/tasks/"+taskID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", resp.StatusCode)
	}

	// Confirm deleted
	req = authReq("GET", ts.URL+"/api/tasks/"+taskID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get after delete failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

