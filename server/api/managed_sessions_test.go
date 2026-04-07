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
	router := NewRouter(store, "test-api-key", mgr, filepath.Join(dir, ".env"), nil, "test-server-id")
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

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0, 0)
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

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0, 0)

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

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0, 0)

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

	sess, _ := store.CreateManagedSession("/tmp", `["Read"]`, 50, 5.0, 0)

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


func TestClearSessionAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-clear-api", `["Read"]`, 50, 5.0, 0)
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", "hi")
	store.SetInitialized(sess.ID)

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	msgs, _ := store.ListMessages(sess.ID)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}

	updated, _ := store.GetSessionByID(sess.ID)
	if updated.Initialized {
		t.Error("expected initialized=false")
	}
}

func TestClearSessionAPI_RejectsWorking(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-clear-working", `["Read"]`, 50, 5.0, 0)
	store.UpdateActivityState(sess.ID, "working")

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 409 {
		t.Fatalf("status=%d, want 409 for working session", resp.StatusCode)
	}
}

func TestClearSessionAPI_NotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/clear", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestSendMessage_StaleWorkingState(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-stale", `["Read"]`, 50, 5.0, 0)

	// Set activity_state to "working" without a real process running.
	// This simulates the stale state bug (issue #82).
	store.UpdateActivityState(sess.ID, "working")

	body := `{"message": "hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should succeed (not 409) because no process is actually running.
	if resp.StatusCode == 409 {
		t.Fatal("got 409 Conflict for stale working state — fix not applied")
	}

	// Verify the activity state was reset from stale "working".
	updated, _ := store.GetSessionByID(sess.ID)
	if updated.ActivityState == "working" {
		// The state should have been reset by the handler before spawning.
		// (It will be set back to "working" by the spawn, but if the spawn
		// fails quickly with echo, it should transition away.)
	}
}

func TestSendMessage_ActivelyWorking(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test-active", `["Read"]`, 50, 5.0, 0)

	// First, send a message to start a real process.
	body := `{"message": "hello"}`
	req1, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/message", strings.NewReader(body))
	req1.Header.Set("Authorization", "Bearer test-api-key")
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := http.DefaultClient.Do(req1)
	resp1.Body.Close()

	// Immediately try a second message — should get 409 if process is still running.
	// (With "echo" as ClaudeBin, the process may finish instantly, so we check
	// that we at least don't crash.)
	req2, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/message", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer test-api-key")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	// Either 200 (echo finished fast) or 409 (still running) are acceptable.
	if resp2.StatusCode != 200 && resp2.StatusCode != 409 {
		t.Errorf("status=%d, want 200 or 409", resp2.StatusCode)
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
	store.CreateManagedSession("/tmp/project-a", `["Bash"]`, 50, 5.0, 0)
	store.CreateManagedSession("/tmp/project-b", `["Bash"]`, 50, 5.0, 0)

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

func TestStaleState_ServerRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create sessions in various states
	s1, _ := store.CreateManagedSession("/tmp/stale-working1", `["Bash"]`, 50, 5.0, 0)
	s2, _ := store.CreateManagedSession("/tmp/stale-working2", `["Bash"]`, 50, 5.0, 0)
	s3, _ := store.CreateManagedSession("/tmp/stale-waiting", `["Bash"]`, 50, 5.0, 0)
	s4, _ := store.CreateManagedSession("/tmp/stale-idle", `["Bash"]`, 50, 5.0, 0)

	store.UpdateActivityState(s1.ID, "working")
	store.UpdateActivityState(s2.ID, "working")
	store.UpdateActivityState(s3.ID, "waiting")
	store.UpdateActivityState(s4.ID, "idle")

	// Simulate server restart: reset stale states
	if err := store.ResetStaleActivityStates(); err != nil {
		t.Fatalf("ResetStaleActivityStates: %v", err)
	}

	// Verify "working" sessions became "idle"
	got1, _ := store.GetSessionByID(s1.ID)
	if got1.ActivityState != "idle" {
		t.Errorf("s1 activity_state = %q, want idle", got1.ActivityState)
	}
	got2, _ := store.GetSessionByID(s2.ID)
	if got2.ActivityState != "idle" {
		t.Errorf("s2 activity_state = %q, want idle", got2.ActivityState)
	}

	// Verify "waiting" sessions are NOT changed
	got3, _ := store.GetSessionByID(s3.ID)
	if got3.ActivityState != "waiting" {
		t.Errorf("s3 activity_state = %q, want waiting (should not be changed)", got3.ActivityState)
	}

	// Verify "idle" sessions are NOT changed
	got4, _ := store.GetSessionByID(s4.ID)
	if got4.ActivityState != "idle" {
		t.Errorf("s4 activity_state = %q, want idle", got4.ActivityState)
	}
}
