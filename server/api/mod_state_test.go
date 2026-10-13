package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func postModState(ts *http.Client, baseURL, sessionID, body string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", baseURL+"/api/sessions/"+sessionID+"/mod-state", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	return ts.Do(req)
}

func TestModStateBroadcastsEvent(t *testing.T) {
	ts, store, mock := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/mod-test", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mock.SetInteractiveRunning(sess.ID, true)

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	body := `{"panel_id":"tokens","panel_title":"Tokens","slot":"bar","data":{"input":1200,"output":340}}`
	resp, err := postModState(http.DefaultClient, ts.URL, sess.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, respBody)
	}

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(msg), &evt); err != nil {
			t.Fatalf("invalid JSON broadcast: %v", err)
		}
		if evt["type"] != "mod_state" {
			t.Fatalf("expected type=mod_state, got %v", evt["type"])
		}
		if evt["panel_id"] != "tokens" {
			t.Fatalf("expected panel_id=tokens, got %v", evt["panel_id"])
		}
		if evt["slot"] != "bar" {
			t.Fatalf("expected slot=bar, got %v", evt["slot"])
		}
		data, ok := evt["data"].(map[string]interface{})
		if !ok {
			t.Fatal("expected data to be an object")
		}
		if data["input"] != float64(1200) {
			t.Fatalf("expected data.input=1200, got %v", data["input"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestModStateRejectsMissingPanelID(t *testing.T) {
	ts, store, _ := setupMockTestServer(t)
	_, err := store.CreateManagedSession("/tmp/mod-test2", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := postModState(http.DefaultClient, ts.URL, "nonexistent", `{"panel_id":"","data":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestModStateDefaultsSlotToBar(t *testing.T) {
	ts, store, mock := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/mod-test3", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mock.SetInteractiveRunning(sess.ID, true)

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	body := `{"panel_id":"cost","panel_title":"Cost","data":{"total":0.05}}`
	resp, err := postModState(http.DefaultClient, ts.URL, sess.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		json.Unmarshal([]byte(msg), &evt)
		if evt["slot"] != "bar" {
			t.Fatalf("expected default slot=bar, got %v", evt["slot"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast received")
	}
}
