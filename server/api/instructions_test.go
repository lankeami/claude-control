package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestQueueAndFetchInstructionAPI(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	// Queue instruction from iOS
	body := map[string]string{"message": "Run tests"}
	req := authReq("POST", ts.URL+"/api/sessions/"+sess.ID+"/instruct", body)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("instruct: expected 200, got %d", resp.StatusCode)
	}

	// Fetch from hook
	req = authReq("GET", ts.URL+"/api/sessions/"+sess.ID+"/instructions", nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var instr map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&instr)
	if instr["message"] != "Run tests" {
		t.Errorf("expected 'Run tests', got %v", instr)
	}
}

func TestFetchInstructionEmpty(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj", "")

	req := authReq("GET", ts.URL+"/api/sessions/"+sess.ID+"/instructions", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}
