package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// --- Test helpers ---

func setupMockTestServer(t *testing.T) (*httptest.Server, *db.Store, *MockManager) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	mock := NewMockManager()
	envPath := filepath.Join(dir, ".env")
	router := NewRouter(store, "test-api-key", mock, envPath, nil, "test-server-id")
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store, mock
}

func sendMessage(ts *httptest.Server, sessionID, message string) (*http.Response, error) {
	body := fmt.Sprintf(`{"message": %q}`, message)
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func collectBroadcasts(b *managed.Broadcaster, timeout time.Duration) []string {
	ch := b.Subscribe()
	var msgs []string
	deadline := time.After(timeout)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return msgs
			}
			msgs = append(msgs, msg)
			var obj map[string]interface{}
			if json.Unmarshal([]byte(msg), &obj) == nil && obj["type"] == "done" {
				b.Unsubscribe(ch)
				return msgs
			}
		case <-deadline:
			b.Unsubscribe(ch)
			return msgs
		}
	}
}

func pollActivityState(t *testing.T, store *db.Store, sessionID, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sess, err := store.GetSessionByID(sessionID)
		if err == nil && sess.ActivityState == expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	sess, _ := store.GetSessionByID(sessionID)
	t.Fatalf("activity_state=%q after %v, want %q", sess.ActivityState, timeout, expected)
}

func hasEvent(msgs []string, eventType string) bool {
	for _, msg := range msgs {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(msg), &obj) == nil && obj["type"] == eventType {
			return true
		}
	}
	return false
}

func getEvent(msgs []string, eventType string) map[string]interface{} {
	for _, msg := range msgs {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(msg), &obj) == nil && obj["type"] == eventType {
			return obj
		}
	}
	return nil
}

func countEvents(msgs []string, eventType string) int {
	count := 0
	for _, msg := range msgs {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(msg), &obj) == nil && obj["type"] == eventType {
			count++
		}
	}
	return count
}

// interruptableProcess creates a mock process where writing stops when interruptCh is closed.
// It writes numTurns assistant lines + a result line, then exits.
// If interrupted mid-write, it stops and exits immediately.
func interruptableProcess(numTurns int, exitCode int) (*managed.Process, chan struct{}) {
	proc, pw := makeProcess()
	interruptCh := make(chan struct{})

	go func() {
		defer func() {
			pw.Close()
			proc.ExitCode = exitCode
			close(proc.Done)
		}()
		for i := 0; i < numTurns; i++ {
			select {
			case <-interruptCh:
				return
			default:
			}
			writeLine(pw, assistantLine)
			// Give StreamNDJSON time to process
			select {
			case <-interruptCh:
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
		writeLine(pw, resultLine)
		// Give StreamNDJSON time to process result
		select {
		case <-interruptCh:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}()

	return proc, interruptCh
}

// interruptCloser is a helper that safely closes the most recent interrupt channel.
type interruptCloser struct {
	mu   sync.Mutex
	chs  []chan struct{}
}

func (ic *interruptCloser) add(ch chan struct{}) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.chs = append(ic.chs, ch)
}

func (ic *interruptCloser) closeLast() {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if len(ic.chs) > 0 {
		ch := ic.chs[len(ic.chs)-1]
		select {
		case <-ch: // already closed
		default:
			close(ch)
		}
	}
}

// --- Auto-Continue Core Loop Tests ---

func TestAutoContinue_HappyPath(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	// maxTurns=5, threshold=0.8 → trigger at turn 4
	sess, _ := store.CreateManagedSession("/tmp/test-ac-happy", `["Bash"]`, 5, 5.0, 0)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(5, 0)
			ic.add(ich)
			return proc, nil
		}
		// Second process: 2 turns, exits normally
		proc, ich := interruptableProcess(2, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 15*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	if got := atomic.LoadInt32(&ensureCount); got != 2 {
		t.Errorf("EnsureProcess called %d times, want 2", got)
	}

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "auto_continuing") {
		t.Error("missing auto_continuing SSE event")
	}
	if !hasEvent(msgs, "done") {
		t.Error("missing done SSE event")
	}
	evt := getEvent(msgs, "auto_continuing")
	if evt != nil && int(evt["continuation_count"].(float64)) != 1 {
		t.Errorf("continuation_count=%v, want 1", evt["continuation_count"])
	}
}

func TestAutoContinue_MultipleContinuations(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	// maxTurns=3, threshold=0.8 → trigger at 2
	sess, _ := store.CreateManagedSession("/tmp/test-ac-multi", `["Bash"]`, 3, 5.0, 0)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call <= 3 {
			proc, ich := interruptableProcess(3, 0)
			ic.add(ich)
			return proc, nil
		}
		// Process 4: 1 turn, exits normally
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 25*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 25*time.Second)

	msgs := <-broadcastsCh
	if c := countEvents(msgs, "auto_continuing"); c < 3 {
		t.Errorf("got %d auto_continuing events, want >= 3", c)
	}
}

func TestAutoContinue_Exhaustion(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	// maxTurns=5, threshold=0.8 → trigger at 4. Default maxContinuations=5.
	sess, _ := store.CreateManagedSession("/tmp/test-ac-exhaust", `["Bash"]`, 5, 5.0, 0)

	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, ich := interruptableProcess(5, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 45*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 45*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "auto_continue_exhausted") {
		t.Error("missing auto_continue_exhausted SSE event")
	}
	evt := getEvent(msgs, "auto_continue_exhausted")
	if evt != nil {
		if _, has := evt["reason"]; has {
			t.Errorf("exhausted event should not have reason field, got %v", evt["reason"])
		}
	}
}

func TestAutoContinue_NoProgress(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	// maxTurns=2, threshold=0.8 → floor(0.8*2)=1 → trigger at 1 assistant turn.
	// First process: 2 turns (>=2, passes progress check).
	// Second process: 1 turn (triggers at 1, but turnsSinceLastContinue=1 < 2 → no progress).
	sess, _ := store.CreateManagedSession("/tmp/test-ac-noprog", `["Bash"]`, 2, 5.0, 0)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			// 3 turns: threshold fires at 1, turnsSinceLastContinue=3 >= 2 → continues
			proc, ich := interruptableProcess(3, 0)
			ic.add(ich)
			return proc, nil
		}
		// Second: 1 turn, threshold fires at 1, turnsSinceLastContinue=1 < 2 → no progress
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 15*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	msgs := <-broadcastsCh
	evt := getEvent(msgs, "auto_continue_exhausted")
	if evt == nil {
		t.Fatal("missing auto_continue_exhausted event")
	}
	if evt["reason"] != "no_progress" {
		t.Errorf("reason=%v, want no_progress", evt["reason"])
	}
}

func TestAutoContinue_ExecutionError(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-ac-err", `["Bash"]`, 5, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, errorResultLine)
			time.Sleep(50 * time.Millisecond)
			pw.Close()
			proc.ExitCode = 1
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	// Execution errors with a natural exit (turnDone fires, process still alive)
	// go through the GracefulShutdown path → "waiting". The executionErrored flag
	// prevents auto-continue but doesn't change the normal exit state.
	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	dbMsgs, _ := store.ListMessages(sess.ID)
	var found bool
	for _, m := range dbMsgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "something failed") {
			found = true
		}
	}
	if !found {
		t.Error("expected execution error message to be persisted")
	}
}

// --- Manual Interrupt Tests ---

func TestManualInterrupt_NaturalExit(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-nat-exit", `["Bash"]`, 50, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
			time.Sleep(50 * time.Millisecond)
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 10*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	msgs := <-broadcastsCh
	if hasEvent(msgs, "auto_continuing") {
		t.Error("should not have auto_continuing event for natural exit")
	}
}

func TestManualInterrupt_ProcessDeath(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-proc-death", `["Bash"]`, 50, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			// Process crashes without emitting result — simulates unexpected death
			pw.Close()
			proc.ExitCode = 1
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 10*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "idle", 10*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "done") {
		t.Error("missing done event")
	}
	if hasEvent(msgs, "auto_continuing") {
		t.Error("should not auto-continue after non-zero exit")
	}
}

// --- Compact Integration Tests ---

func TestCompact_RunsAtInterval(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-compact-int", `["Bash"]`, 3, 5.0, 1)

	var compactCalls int32
	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(3, 0)
			ic.add(ich)
			return proc, nil
		}
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }
	mock.OnRunCompact = func(sessionID, resumeID, cwd string, timeout time.Duration) error {
		atomic.AddInt32(&compactCalls, 1)
		return nil
	}

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	if got := atomic.LoadInt32(&compactCalls); got < 1 {
		t.Errorf("RunCompact called %d times, want >= 1", got)
	}
}

func TestCompact_FailureContinues(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-compact-fail", `["Bash"]`, 3, 5.0, 1)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(3, 0)
			ic.add(ich)
			return proc, nil
		}
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }
	mock.OnRunCompact = func(sessionID, resumeID, cwd string, timeout time.Duration) error {
		return fmt.Errorf("compact failed: out of memory")
	}

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	dbMsgs, _ := store.ListMessages(sess.ID)
	var found bool
	for _, m := range dbMsgs {
		if m.Role == "system" && strings.Contains(m.Content, "Compact failed") {
			found = true
		}
	}
	if !found {
		t.Error("missing compact failure warning message")
	}
	if got := atomic.LoadInt32(&ensureCount); got < 2 {
		t.Errorf("EnsureProcess called %d times, want >= 2", got)
	}
}

func TestCompact_SSEEvents(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-compact-sse", `["Bash"]`, 3, 5.0, 1)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(3, 0)
			ic.add(ich)
			return proc, nil
		}
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }
	mock.OnRunCompact = func(sessionID, resumeID, cwd string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 15*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "compacting") {
		t.Error("missing compacting SSE event")
	}
	if !hasEvent(msgs, "compact_complete") {
		t.Error("missing compact_complete SSE event")
	}
}

// --- Message Persistence Tests ---

func TestPersistence_AssistantText(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-persist-text", `["Bash"]`, 50, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First response"}]}}`)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Second response"}]}}`)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
			time.Sleep(50 * time.Millisecond)
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	dbMsgs, _ := store.ListMessages(sess.ID)
	var texts []string
	for _, m := range dbMsgs {
		if m.Role == "assistant" {
			texts = append(texts, m.Content)
		}
	}
	if len(texts) < 2 {
		t.Fatalf("got %d assistant messages, want >= 2", len(texts))
	}
	if texts[0] != "First response" {
		t.Errorf("first=%q, want 'First response'", texts[0])
	}
	if texts[1] != "Second response" {
		t.Errorf("second=%q, want 'Second response'", texts[1])
	}
}

func TestPersistence_SystemMessages(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-persist-sys", `["Bash"]`, 5, 5.0, 0)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(5, 0)
			ic.add(ich)
			return proc, nil
		}
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	dbMsgs, _ := store.ListMessages(sess.ID)
	var found bool
	for _, m := range dbMsgs {
		if m.Role == "system" && strings.Contains(m.Content, "Auto-continuing (1/5)") {
			found = true
		}
	}
	if !found {
		t.Error("missing 'Auto-continuing (1/5)...' system message")
	}
}

func TestPersistence_ToolActivity(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-persist-tool", `["Bash"]`, 50, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, toolUseLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
			time.Sleep(50 * time.Millisecond)
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	dbMsgs, _ := store.ListMessages(sess.ID)
	var found bool
	for _, m := range dbMsgs {
		if m.Role == "activity" && strings.Contains(m.Content, "Read") {
			found = true
		}
	}
	if !found {
		t.Error("missing tool activity message for Read tool")
	}
}

// --- SSE Event Delivery Tests ---

func TestSSE_DoneEvent(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-sse-done", `["Bash"]`, 50, 5.0, 0)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		proc, pw := makeProcess()
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
			time.Sleep(50 * time.Millisecond)
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		}()
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 10*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	msgs := <-broadcastsCh
	evt := getEvent(msgs, "done")
	if evt == nil {
		t.Fatal("missing done SSE event")
	}
	if int(evt["exit_code"].(float64)) != 0 {
		t.Errorf("exit_code=%v, want 0", evt["exit_code"])
	}
}

func TestSSE_HeartbeatBetweenProcesses(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-sse-hb", `["Bash"]`, 5, 5.0, 0)

	var ensureCount int32
	ic := &interruptCloser{}

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		call := atomic.AddInt32(&ensureCount, 1)
		if call == 1 {
			proc, ich := interruptableProcess(5, 0)
			ic.add(ich)
			return proc, nil
		}
		proc, ich := interruptableProcess(1, 0)
		ic.add(ich)
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error { return nil }
	mock.OnInterrupt = func(sessionID string) error { ic.closeLast(); return nil }
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error { return nil }

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 15*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "heartbeat") {
		t.Error("missing heartbeat SSE event between process cycles")
	}
}
