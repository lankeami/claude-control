package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func newTestServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	router := NewRouter(store, "test-key", nil, filepath.Join(t.TempDir(), ".env"))
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store
}

func authReq(method, url string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, url, &buf)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestRegisterSession(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]string{"computer_name": "mac1", "project_path": "/proj"}
	req := authReq("POST", ts.URL+"/api/sessions/register", body)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)
	if session["computer_name"] != "mac1" {
		t.Errorf("unexpected: %v", session)
	}
}

func TestListSessions(t *testing.T) {
	ts, store := newTestServer(t)
	store.UpsertSession("mac1", "/proj/a", "")
	store.UpsertSession("mac1", "/proj/b", "")

	req := authReq("GET", ts.URL+"/api/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var sessions []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}
