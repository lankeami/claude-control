package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func setupInteractiveTestServer(t *testing.T) (*httptest.Server, *db.Store, *MockManager) {
	t.Helper()
	ts, store, mock := setupMockTestServer(t)
	mock.ConfigValue = managed.Config{Mode: "interactive", BinaryPath: "/usr/local/bin/claude-controller", ServerPort: 8080}
	return ts, store, mock
}

func waitForTranscriptFn(t *testing.T, mock *MockManager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		fn := mock.TranscriptLineFns[sessionID]
		mock.mu.Unlock()
		if fn != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("transcript callback never registered (EnsureInteractive not called?)")
}

func assistantTranscriptLine(text string, in, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-12T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":%q}],"usage":{"input_tokens":%d,"output_tokens":%d}}}`, text, in, out)
}

func TestInteractiveTurnCompletesOnStopHook(t *testing.T) {
	ts, store, mock := setupInteractiveTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/int-basic", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	resp, err := sendMessage(ts, sess.ID, "do something")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	waitForTranscriptFn(t, mock, sess.ID)
	mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("working on it", 100, 50))

	// Wait for the prompt to be typed before signaling stop
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(mock.SentPromptsCopy()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	prompts := mock.SentPromptsCopy()
	if len(prompts) != 1 || prompts[0] != "do something" {
		t.Fatalf("prompts = %v", prompts)
	}

	mock.SignalStop(sess.ID)

	var sawResult, sawDone bool
	timeout := time.After(3 * time.Second)
	for !sawDone {
		select {
		case msg := <-ch:
			var obj map[string]any
			json.Unmarshal([]byte(msg), &obj)
			if obj["type"] == "result" {
				sawResult = true
				if obj["subtype"] != "success" {
					t.Errorf("result subtype = %v", obj["subtype"])
				}
			}
			if obj["type"] == "done" {
				sawDone = true
			}
		case <-timeout:
			t.Fatal("never saw done event")
		}
	}
	if !sawResult {
		t.Error("no result event before done")
	}

	pollActivityState(t, store, sess.ID, "waiting", 2*time.Second)

	msgs, _ := store.ListMessages(sess.ID)
	var sawAssistant bool
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "working on it") {
			sawAssistant = true
		}
	}
	if !sawAssistant {
		t.Error("assistant transcript text not persisted")
	}
}

func TestInteractiveMaxTurnsInterruptsAndAutoContinues(t *testing.T) {
	old := escStopFallback
	escStopFallback = 200 * time.Millisecond
	defer func() { escStopFallback = old }()

	ts, store, mock := setupInteractiveTestServer(t)
	// max_turns = 2; max_continuations defaults to 5 so auto-continue is allowed
	sess, err := store.CreateManagedSession("/tmp/int-maxturns", `["Bash"]`, 2, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	resp, err := sendMessage(ts, sess.ID, "long task")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	waitForTranscriptFn(t, mock, sess.ID)
	mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("step 1", 10, 10))
	mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("step 2", 10, 10))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && mock.InterruptInteractiveCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if mock.InterruptInteractiveCount() != 1 {
		t.Fatalf("InterruptInteractive calls = %d, want 1", mock.InterruptInteractiveCount())
	}

	// No Stop hook arrives (interrupt) — the ESC fallback timer should end
	// the turn, then auto-continue should type the continuation prompt.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(mock.SentPromptsCopy()) < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	prompts := mock.SentPromptsCopy()
	if len(prompts) < 2 || !strings.Contains(prompts[1], "Continue where you left off") {
		t.Fatalf("prompts = %v", prompts)
	}

	var sawAutoContinuing, sawMaxTurnsResult bool
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case msg := <-ch:
			var obj map[string]any
			json.Unmarshal([]byte(msg), &obj)
			if obj["type"] == "auto_continuing" {
				sawAutoContinuing = true
				break drain
			}
			if obj["type"] == "result" && obj["subtype"] == "error_max_turns" {
				sawMaxTurnsResult = true
			}
		case <-timeout:
			break drain
		}
	}
	if !sawMaxTurnsResult {
		t.Error("no error_max_turns result event")
	}
	if !sawAutoContinuing {
		t.Error("no auto_continuing event")
	}
}

func TestInteractiveBudgetExceededStopsSession(t *testing.T) {
	ts, store, mock := setupInteractiveTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/int-budget", `["Bash"]`, 50, 0.01, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-existing spend over the $0.01 budget
	store.CreateMessage(sess.ID, "cost", "0.50", 0.50)

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	resp, err := sendMessage(ts, sess.ID, "one more thing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	waitForTranscriptFn(t, mock, sess.ID)
	mock.SignalStop(sess.ID)

	var sawBudget bool
	timeout := time.After(3 * time.Second)
	for !sawBudget {
		select {
		case msg := <-ch:
			if strings.Contains(msg, `"budget_exceeded"`) {
				sawBudget = true
			}
		case <-timeout:
			t.Fatal("no budget_exceeded event")
		}
	}
	pollActivityState(t, store, sess.ID, "waiting", 2*time.Second)
}

func TestInterruptRoutesToInteractive(t *testing.T) {
	ts, store, mock := setupInteractiveTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/int-interrupt", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mock.SetInteractiveRunning(sess.ID, true)

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/interrupt", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if mock.InterruptInteractiveCount() != 1 {
		t.Fatalf("InterruptInteractive calls = %d", mock.InterruptInteractiveCount())
	}
}

func TestInteractiveBusySessionReturns409(t *testing.T) {
	ts, store, mock := setupInteractiveTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/int-busy", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mock.SetInteractiveRunning(sess.ID, true)
	store.UpdateActivityState(sess.ID, "working")

	resp, err := sendMessage(ts, sess.ID, "another message")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}
