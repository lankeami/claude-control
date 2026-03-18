package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStaticFilesNoAuth(t *testing.T) {
	ts, _ := newTestServer(t)

	// Static files should be accessible without auth
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAPIStillRequiresAuth(t *testing.T) {
	ts, _ := newTestServer(t)

	// API endpoints should still require auth
	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSSEEvents_RequiresAuth(t *testing.T) {
	ts, _ := newTestServer(t)

	// No token — should get 401
	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSSEEvents_StreamsState(t *testing.T) {
	ts, store := newTestServer(t)

	// Seed data
	session, _ := store.UpsertSession("mac1", "/proj/a", "")
	store.CreatePrompt(session.ID, "Which DB?", "prompt")

	// Connect to SSE with token
	req, _ := http.NewRequest("GET", ts.URL+"/api/events?token=test-key", nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// Read first event
	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	if dataLine == "" {
		t.Fatal("no data line received")
	}

	var payload struct {
		Sessions []json.RawMessage `json:"sessions"`
		Prompts  []json.RawMessage `json:"prompts"`
	}
	if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(payload.Sessions))
	}
	if len(payload.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(payload.Prompts))
	}
}
