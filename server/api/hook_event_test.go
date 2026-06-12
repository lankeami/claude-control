package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postHookEvent(ts *httptest.Server, sessionID, body string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/hook-event", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func TestHookEventStopSignalsManager(t *testing.T) {
	ts, store, mock := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/hook-stop", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mock.SetInteractiveRunning(sess.ID, true)

	resp, err := postHookEvent(ts, sess.ID, `{"event":"stop"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	select {
	case <-mock.StopEvents(sess.ID):
	case <-time.After(time.Second):
		t.Fatal("stop not signaled to manager")
	}
}

func TestHookEventSessionStartUpdatesIDAndTranscript(t *testing.T) {
	ts, store, mock := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/hook-start", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"event":"session_start","claude_session_id":"forked-uuid","transcript_path":"/tmp/forked.jsonl"}`
	resp, err := postHookEvent(ts, sess.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	updated, _ := store.GetSessionByID(sess.ID)
	if updated.ClaudeSessionID != "forked-uuid" {
		t.Errorf("claude_session_id = %s", updated.ClaudeSessionID)
	}
	mock.mu.Lock()
	calls := append([]string{}, mock.SetTranscriptCalls...)
	mock.mu.Unlock()
	if len(calls) != 1 || calls[0] != sess.ID+":/tmp/forked.jsonl" {
		t.Errorf("SetTranscript calls = %v", calls)
	}
}

func TestHookEventNotificationBroadcasts(t *testing.T) {
	ts, store, mock := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/hook-notif", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	resp, err := postHookEvent(ts, sess.ID, `{"event":"notification","message":"Claude needs permission to use WebFetch"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	select {
	case msg := <-ch:
		if !strings.Contains(msg, `"notification"`) || !strings.Contains(msg, "WebFetch") {
			t.Errorf("broadcast = %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not broadcast")
	}

	msgs, _ := store.ListMessages(sess.ID)
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, "WebFetch") {
			found = true
		}
	}
	if !found {
		t.Error("notification not persisted as system message")
	}
}

func TestHookEventUnknownSessionReturns404(t *testing.T) {
	ts, _, _ := setupMockTestServer(t)
	resp, err := postHookEvent(ts, "missing", `{"event":"stop"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHookEventUnknownEventReturns400(t *testing.T) {
	ts, store, _ := setupMockTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/hook-bad", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := postHookEvent(ts, sess.ID, `{"event":"mystery"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
