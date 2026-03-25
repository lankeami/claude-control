package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func setupTestServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}

	cfg := managed.Config{
		ClaudeBin:  "echo",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	mgr := managed.NewManager(cfg)
	router := NewRouter(store, "test-api-key", mgr, filepath.Join(dir, ".env"))
	ts := httptest.NewServer(router)
	return ts, store
}

func TestCreateManagedSessionAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"cwd": "/tmp/test-project"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	if sess["mode"] != "managed" {
		t.Errorf("mode=%v, want managed", sess["mode"])
	}
	if sess["cwd"] != "/tmp/test-project" {
		t.Errorf("cwd=%v, want /tmp/test-project", sess["cwd"])
	}
}

func TestCreateDuplicateManagedSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"cwd": "/tmp/test-project"}`

	req1, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req1.Header.Set("Authorization", "Bearer test-api-key")
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := http.DefaultClient.Do(req1)
	resp1.Body.Close()

	req2, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer test-api-key")
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := http.DefaultClient.Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != 409 {
		t.Errorf("status=%d, want 409 for duplicate cwd", resp2.StatusCode)
	}
}

func TestListMessagesAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", `{"type":"assistant","content":"hi"}`)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var msgs []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 2 {
		t.Errorf("got %d messages, want 2", len(msgs))
	}
}

func TestShellExecuteAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["id"] == nil || result["id"] == "" {
		t.Error("expected non-empty command id in response")
	}
}

func TestShellExecuteRejectsEmptyCommand(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": ""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestShellExecuteRejectsHookSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// UpsertSession creates a hook-mode session (mode defaults to "hook")
	hookSess, _ := store.UpsertSession("hook-sess", "/tmp", "/tmp/transcript")

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+hookSess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400 for hook session", resp.StatusCode)
	}
}

func TestShellExecuteRejectsNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"command": "echo hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status=%d, want 404", resp.StatusCode)
	}
}

func TestShellExecutePersistsMessages(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0)

	body := `{"command": "echo hello", "timeout": 5}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/shell", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Poll for shell_output message (avoids flaky time.Sleep)
	deadline := time.Now().Add(10 * time.Second)
	var foundShell, foundOutput bool
	for time.Now().Before(deadline) {
		msgs, err := store.ListMessages(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		foundShell = false
		foundOutput = false
		for _, m := range msgs {
			if m.Role == "shell" && m.Content == "echo hello" {
				foundShell = true
			}
			if m.Role == "shell_output" {
				foundOutput = true
			}
		}
		if foundShell && foundOutput {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !foundShell {
		t.Error("expected shell command message to be persisted")
	}
	if !foundOutput {
		t.Error("expected shell_output message to be persisted")
	}
}

func TestAutoContinueThresholdCalculation(t *testing.T) {
	// threshold = floor(0.8 * 50) = 40
	val := float64(50) * 0.8
	threshold := int(val)
	if threshold != 40 {
		t.Errorf("expected 40, got %d", threshold)
	}

	// threshold = floor(0.8 * 5) = 4
	val = float64(5) * 0.8
	threshold = int(val)
	if threshold != 4 {
		t.Errorf("expected 4, got %d", threshold)
	}

	// Edge: threshold = floor(0.8 * 1) = 0
	val = float64(1) * 0.8
	threshold = int(val)
	if threshold != 0 {
		t.Errorf("expected 0, got %d", threshold)
	}
}

func TestRecentDirsAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Empty response
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/recent-dirs", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result struct {
		Directories []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"directories"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Directories) != 0 {
		t.Errorf("expected 0 dirs, got %d", len(result.Directories))
	}

	// Create sessions, then check
	store.CreateManagedSession("/tmp/project-a", `["Bash"]`, 50, 5.0)
	store.CreateManagedSession("/tmp/project-b", `["Bash"]`, 50, 5.0)

	req2, _ := http.NewRequest("GET", ts.URL+"/api/sessions/recent-dirs", nil)
	req2.Header.Set("Authorization", "Bearer test-api-key")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	json.NewDecoder(resp2.Body).Decode(&result)
	if len(result.Directories) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(result.Directories))
	}
	if result.Directories[0].Path != "/tmp/project-b" {
		t.Errorf("first dir = %s, want /tmp/project-b", result.Directories[0].Path)
	}
	if result.Directories[0].Name != "project-b" {
		t.Errorf("first name = %s, want project-b", result.Directories[0].Name)
	}
}
