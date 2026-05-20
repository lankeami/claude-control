package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
