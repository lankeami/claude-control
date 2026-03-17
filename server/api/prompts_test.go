package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCreatePrompt(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	body := map[string]string{"session_id": sess.ID, "claude_message": "Which DB?", "type": "prompt"}
	req := authReq("POST", ts.URL+"/api/prompts", body)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var prompt map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&prompt)
	if prompt["claude_message"] != "Which DB?" {
		t.Errorf("unexpected: %v", prompt)
	}
}

func TestRespondAndGetResponse(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	prompt, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	// Respond
	body := map[string]string{"response": "SQLite"}
	req := authReq("POST", ts.URL+"/api/prompts/"+prompt.ID+"/respond", body)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("respond: expected 200, got %d", resp.StatusCode)
	}

	// Get response (should return immediately since already answered)
	req = authReq("GET", ts.URL+"/api/prompts/"+prompt.ID+"/response", nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get response: expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "SQLite" {
		t.Errorf("expected 'SQLite', got %v", result["response"])
	}
}

func TestLongPollTimeout(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	prompt, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	// Long-poll with short timeout should return pending
	start := time.Now()
	req := authReq("GET", ts.URL+"/api/prompts/"+prompt.ID+"/response?timeout=1", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected ~1s wait, got %v", elapsed)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "pending" {
		t.Errorf("expected 'pending', got %v", result["status"])
	}
}

func TestListPendingPrompts(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	store.CreatePrompt(sess.ID, "Q1", "prompt")
	store.CreatePrompt(sess.ID, "Q2", "prompt")

	req := authReq("GET", ts.URL+"/api/prompts?status=pending", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var prompts []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&prompts)
	if len(prompts) != 2 {
		t.Errorf("expected 2, got %d", len(prompts))
	}
}
