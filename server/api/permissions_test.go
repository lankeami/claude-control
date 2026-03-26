package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPermissionRequestAndRespond(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	type result struct {
		status int
		body   string
	}
	ch := make(chan result, 1)
	go func() {
		body := `{"tool_name":"Bash","description":"Run echo hello","input":{"command":"echo hello"}}`
		req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-request", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-api-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- result{0, err.Error()}
			return
		}
		defer resp.Body.Close()
		var respBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&respBody)
		b, _ := json.Marshal(respBody)
		ch <- result{resp.StatusCode, string(b)}
	}()

	time.Sleep(100 * time.Millisecond)

	respondBody := `{"decision":"allow"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-respond", strings.NewReader(respondBody))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("respond status=%d, want 200", resp.StatusCode)
	}

	select {
	case r := <-ch:
		if r.status != 200 {
			t.Fatalf("permission-request status=%d, want 200, body=%s", r.status, r.body)
		}
		if !strings.Contains(r.body, "allow") {
			t.Errorf("expected allow in response, got %s", r.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("permission-request did not complete after respond")
	}
}

func TestPermissionRespondNoRequest(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	body := `{"decision":"allow"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/permission-respond", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status=%d, want 404 when no pending request", resp.StatusCode)
	}
}

func TestPermissionPendingEndpoint(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/pending-permission", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["pending"] != false {
		t.Errorf("expected pending=false, got %v", result["pending"])
	}
}
