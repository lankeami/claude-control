package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

// openTestDB is a minimal helper used by usage tests to get a *db.Store.
func openTestDB(t *testing.T, dir string) (*db.Store, error) {
	t.Helper()
	return db.Open(filepath.Join(dir, "test.db"))
}

// newUsageTestServer creates a test server with a mock upstream usage URL injected.
// Returns the test server and the session ID for requests.
func newUsageTestServer(t *testing.T, upstreamURL string) (*httptest.Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	envPath := filepath.Join(tmpDir, ".env")
	s := &Server{store: store, envPath: envPath, skipKeychain: true, usageCache: &UsageCache{}, usageCacheMu: sync.RWMutex{}}
	s.usageUpstreamURL = upstreamURL

	// Create a test session
	sess, err := store.CreateManagedSession(tmpDir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, sess.ID
}

func TestUsage_NoToken(t *testing.T) {
	t.Setenv("CLAUDE_OAUTH_TOKEN", "")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when no token")
	}))
	defer upstream.Close()

	ts, sessionID := newUsageTestServer(t, upstream.URL)
	resp, err := http.Get(ts.URL + "/api/usage?sessionId=" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "no_oauth_token" {
		t.Errorf("expected error=no_oauth_token, got %q", body["error"])
	}
}

func TestUsage_EnvToken_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-oauth-token" {
			t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Errorf("unexpected anthropic-beta: %s", r.Header.Get("anthropic-beta"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"five_hour":{"utilization":0.42,"resets_at":"2026-05-16T18:00:00.000Z"}}`))
	}))
	defer upstream.Close()

	t.Setenv("CLAUDE_OAUTH_TOKEN", "test-oauth-token")
	ts, sessionID := newUsageTestServer(t, upstream.URL)

	resp, err := http.Get(ts.URL + "/api/usage?sessionId=" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if _, ok := body["five_hour"]; !ok {
		t.Error("expected five_hour in response")
	}
}

func TestUsage_EnvToken_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	t.Setenv("CLAUDE_OAUTH_TOKEN", "expired-token")
	ts, sessionID := newUsageTestServer(t, upstream.URL)

	resp, err := http.Get(ts.URL + "/api/usage?sessionId=" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "upstream_error" {
		t.Errorf("expected error=upstream_error, got %v", body["error"])
	}
}

func TestHandleUsage_MissingSession(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	envPath := filepath.Join(tmpDir, ".env")
	s := &Server{store: store, envPath: envPath, skipKeychain: true}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleUsage_ValidSession(t *testing.T) {
	mockAnthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"five_hour":{"utilization":0.42,"resets_at":"2026-05-19T18:00:00.000Z"}}`))
	}))
	defer mockAnthropicServer.Close()

	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	envPath := filepath.Join(tmpDir, ".env")

	sess, err := store.CreateManagedSession(tmpDir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	s := &Server{
		store:            store,
		envPath:          envPath,
		usageUpstreamURL: mockAnthropicServer.URL,
		skipKeychain:     true,
		usageCache:       &UsageCache{},
		usageCacheMu:     sync.RWMutex{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Setenv("CLAUDE_OAUTH_TOKEN", "test-token")
	resp, err := http.Get(ts.URL + "/api/usage?sessionId=" + sess.ID)
	if err != nil {
		t.Fatal(err)
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
}
