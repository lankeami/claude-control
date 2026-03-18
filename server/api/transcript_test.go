package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGetTranscript(t *testing.T) {
	ts, store := newTestServer(t)

	// Create a test JSONL file
	tmpDir := t.TempDir()
	transcriptFile := filepath.Join(tmpDir, "test.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Hello"}]},"timestamp":"2026-03-18T10:00:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]},"timestamp":"2026-03-18T10:00:01Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},"timestamp":"2026-03-18T10:00:02Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"timestamp":"2026-03-18T10:00:03Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done!"}]},"timestamp":"2026-03-18T10:00:04Z"}`,
	}
	var content []byte
	for _, l := range lines {
		content = append(content, []byte(l+"\n")...)
	}
	os.WriteFile(transcriptFile, content, 0644)

	session, _ := store.UpsertSession("mac1", "/proj", transcriptFile)

	req := authReq("GET", ts.URL+"/api/sessions/"+session.ID+"/transcript", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
	}
	json.NewDecoder(resp.Body).Decode(&messages)

	// Should have 3 messages: user "Hello", assistant "Hi there!", assistant "Done!"
	// tool_use-only assistant and tool_result user should be skipped
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(messages), messages)
	}
	if messages[0].Role != "user" || messages[0].Content != "Hello" {
		t.Errorf("msg 0: %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Hi there!" {
		t.Errorf("msg 1: %+v", messages[1])
	}
	if messages[2].Role != "assistant" || messages[2].Content != "Done!" {
		t.Errorf("msg 2: %+v", messages[2])
	}
}

func TestGetTranscript_NoPath(t *testing.T) {
	ts, store := newTestServer(t)
	session, _ := store.UpsertSession("mac1", "/proj", "")

	req := authReq("GET", ts.URL+"/api/sessions/"+session.ID+"/transcript", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var messages []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&messages)
	if len(messages) != 0 {
		t.Errorf("expected empty array, got %d", len(messages))
	}
}
