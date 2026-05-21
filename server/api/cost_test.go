package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestHandleCostSummary_ValidSession(t *testing.T) {
	// Create test DB with a session and messages with costs
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("openTestDB failed: %v", err)
	}

	// Create a managed session
	sess, err := store.CreateManagedSession(tmpDir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession failed: %v", err)
	}

	// Create messages
	store.CreateMessage(sess.ID, "user", "hello", 0)
	store.CreateMessage(sess.ID, "assistant", "response", 0)

	// Create server with test store
	server := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/cost-summary", server.handleCostSummary)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Request with valid sessionId
	resp, err := http.Get(ts.URL + "/api/cost-summary?sessionId=" + sess.ID)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if _, ok := body["five_hour"]; !ok {
		t.Error("expected five_hour in response")
	}
	if _, ok := body["seven_day"]; !ok {
		t.Error("expected seven_day in response")
	}
}

func TestAggregateCosts_UsesSessionName(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("openTestDB failed: %v", err)
	}

	sess, err := store.CreateManagedSession(tmpDir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession failed: %v", err)
	}
	if err := store.UpdateSessionName(sess.ID, "my-project"); err != nil {
		t.Fatalf("UpdateSessionName failed: %v", err)
	}

	// Add a cost message so the session appears in results
	store.CreateMessage(sess.ID, "cost", "0.010000", 0.01)

	server := &Server{store: store, envPath: tmpDir + "/.env"}

	now := time.Now().UTC()
	summary, err := server.aggregateCosts(now.Add(-time.Hour), now.Add(time.Hour), 5.0)
	if err != nil {
		t.Fatalf("aggregateCosts failed: %v", err)
	}

	if len(summary.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(summary.Sessions))
	}
	if summary.Sessions[0].Name != "my-project" {
		t.Errorf("expected Name 'my-project', got %q", summary.Sessions[0].Name)
	}
}

func TestAggregateCosts_FallsBackToCWDBasename(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("openTestDB failed: %v", err)
	}

	// CreateManagedSession sets cwd = tmpDir, name stays empty
	sess, err := store.CreateManagedSession(tmpDir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession failed: %v", err)
	}

	store.CreateMessage(sess.ID, "cost", "0.010000", 0.01)

	server := &Server{store: store, envPath: tmpDir + "/.env"}

	now := time.Now().UTC()
	summary, err := server.aggregateCosts(now.Add(-time.Hour), now.Add(time.Hour), 5.0)
	if err != nil {
		t.Fatalf("aggregateCosts failed: %v", err)
	}

	if len(summary.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(summary.Sessions))
	}
	// Name should be the basename of tmpDir when session name is empty
	expectedName := filepath.Base(tmpDir)
	if summary.Sessions[0].Name != expectedName {
		t.Errorf("expected Name %q (cwd basename), got %q", expectedName, summary.Sessions[0].Name)
	}
}

func TestHandleCostSummary_MissingSession(t *testing.T) {
	// Request without sessionId should return 401
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("openTestDB failed: %v", err)
	}

	server := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/cost-summary", server.handleCostSummary)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/cost-summary")
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}
