package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	router := NewRouter(store, "test-api-key", mgr)
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
