package api

import (
	"encoding/json"
	"fmt"
	"io"
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

// --- Persistent process simulation ---
//
// turnSpec describes what the process should output for a single turn.
type turnSpec struct {
	assistantCount int    // number of assistant lines to emit
	result         string // result line (resultLine, maxTurnsResultLine, or errorResultLine)
}

// persistentProc simulates a persistent Claude process that stays alive across turns.
// SendTurn triggers the next pre-configured turn output. GracefulShutdown exits.
type persistentProc struct {
	proc       *managed.Process
	pw         *io.PipeWriter
	triggerCh  chan struct{} // SendTurn signals this to produce next turn
	shutdownCh chan struct{} // GracefulShutdown signals this to exit
}

func newPersistentProc(turns []turnSpec) *persistentProc {
	proc, pw := makeProcess()
	pp := &persistentProc{
		proc:       proc,
		pw:         pw,
		triggerCh:  make(chan struct{}, 10),
		shutdownCh: make(chan struct{}),
	}

	go func() {
		defer func() {
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		}()
		turnIdx := 0
		for {
			select {
			case <-pp.shutdownCh:
				return
			case <-pp.triggerCh:
				if turnIdx >= len(turns) {
					// No more turns configured, just emit a natural result
					writeLine(pw, resultLine)
					time.Sleep(20 * time.Millisecond)
					continue
				}
				turn := turns[turnIdx]
				turnIdx++
				for i := 0; i < turn.assistantCount; i++ {
					writeLine(pw, assistantLine)
					time.Sleep(10 * time.Millisecond)
				}
				writeLine(pw, turn.result)
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	return pp
}

// --- Auto-Continue Tests (persistent process model) ---

// TestNaturalCompletion_NoAutoContinue verifies that when Claude finishes
// naturally (subtype "success"), no auto-continue is triggered even if
// max_continuations > 0.
func TestNaturalCompletion_NoAutoContinue(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-nat-no-ac", `["Bash"]`, 50, 5.0, 0)

	pp := newPersistentProc([]turnSpec{
		{assistantCount: 3, result: resultLine}, // Natural completion (success)
	})

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 10*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "What framework should I use?")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 10*time.Second)

	msgs := <-broadcastsCh
	if hasEvent(msgs, "auto_continuing") {
		t.Error("should NOT auto-continue when Claude finishes naturally")
	}
	if !hasEvent(msgs, "done") {
		t.Error("missing done event")
	}
}

// TestMaxTurnsHit_AutoContinues verifies that when Claude hits --max-turns
// (subtype "error_max_turns"), auto-continue triggers and sends a continuation.
func TestMaxTurnsHit_AutoContinues(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-maxturns-ac", `["Bash"]`, 50, 5.0, 0)

	pp := newPersistentProc([]turnSpec{
		{assistantCount: 5, result: maxTurnsResultLine}, // Hit max_turns → auto-continue
		{assistantCount: 2, result: resultLine},          // Natural completion → stop
	})

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 15*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "Build a feature")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 15*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "auto_continuing") {
		t.Error("missing auto_continuing event — should auto-continue on max_turns")
	}
	if !hasEvent(msgs, "done") {
		t.Error("missing done event")
	}
	evt := getEvent(msgs, "auto_continuing")
	if evt != nil && int(evt["continuation_count"].(float64)) != 1 {
		t.Errorf("continuation_count=%v, want 1", evt["continuation_count"])
	}
}

// TestAutoContinue_Exhaustion verifies that auto-continue stops after
// max_continuations is reached.
func TestAutoContinue_Exhaustion(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	// max_continuations defaults to 5
	sess, _ := store.CreateManagedSession("/tmp/test-ac-exhaust", `["Bash"]`, 50, 5.0, 0)

	// All turns hit max_turns — should exhaust after 5 continuations
	turns := make([]turnSpec, 7)
	for i := range turns {
		turns[i] = turnSpec{assistantCount: 3, result: maxTurnsResultLine}
	}

	pp := newPersistentProc(turns)

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

	broadcaster := mock.GetBroadcaster(sess.ID)
	broadcastsCh := make(chan []string, 1)
	go func() { broadcastsCh <- collectBroadcasts(broadcaster, 30*time.Second) }()

	resp, _ := sendMessage(ts, sess.ID, "Build everything")
	resp.Body.Close()

	pollActivityState(t, store, sess.ID, "waiting", 30*time.Second)

	msgs := <-broadcastsCh
	if !hasEvent(msgs, "auto_continue_exhausted") {
		t.Error("missing auto_continue_exhausted event")
	}
	evt := getEvent(msgs, "auto_continue_exhausted")
	if evt != nil {
		if _, has := evt["reason"]; has {
			t.Errorf("exhausted event should not have reason field, got %v", evt["reason"])
		}
	}
}

// TestAutoContinue_NoProgress verifies that auto-continue stops when
// Claude makes no progress (< 2 assistant events on a continuation turn).
func TestAutoContinue_NoProgress(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-ac-noprog", `["Bash"]`, 50, 5.0, 0)

	pp := newPersistentProc([]turnSpec{
		{assistantCount: 5, result: maxTurnsResultLine}, // First turn: hit max_turns, good progress
		{assistantCount: 1, result: maxTurnsResultLine}, // Second turn: only 1 event → no progress
	})

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

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

// TestAutoContinue_ExecutionError verifies that execution errors don't
// trigger auto-continue.
func TestAutoContinue_ExecutionError(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-ac-err", `["Bash"]`, 50, 5.0, 0)

	pp := newPersistentProc([]turnSpec{
		{assistantCount: 1, result: errorResultLine},
	})

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

	resp, _ := sendMessage(ts, sess.ID, "hello")
	resp.Body.Close()

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

// TestProcessDeath_NoAutoContinue verifies that unexpected process death
// (non-zero exit) doesn't trigger auto-continue.
func TestProcessDeath_NoAutoContinue(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-proc-death", `["Bash"]`, 50, 5.0, 0)

	proc, pw := makeProcess()
	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		// Simulate process crash after one assistant line
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
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

// --- Message Persistence Tests ---

func TestPersistence_AssistantText(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-persist-text", `["Bash"]`, 50, 5.0, 0)

	proc, pw := makeProcess()
	var shutdownOnce sync.Once
	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		go func() {
			writeLine(pw, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First response"}]}}`)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Second response"}]}}`)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
		}()
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		shutdownOnce.Do(func() {
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		})
		return nil
	}

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
	sess, _ := store.CreateManagedSession("/tmp/test-persist-sys", `["Bash"]`, 50, 5.0, 0)

	pp := newPersistentProc([]turnSpec{
		{assistantCount: 5, result: maxTurnsResultLine}, // Hit max_turns
		{assistantCount: 2, result: resultLine},          // Natural completion
	})

	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

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

	proc, pw := makeProcess()
	var shutdownOnce sync.Once
	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		go func() {
			writeLine(pw, toolUseLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
		}()
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		shutdownOnce.Do(func() {
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		})
		return nil
	}

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

	proc, pw := makeProcess()
	var shutdownOnce sync.Once
	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		go func() {
			writeLine(pw, assistantLine)
			time.Sleep(15 * time.Millisecond)
			writeLine(pw, resultLine)
		}()
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		shutdownOnce.Do(func() {
			pw.Close()
			proc.ExitCode = 0
			close(proc.Done)
		})
		return nil
	}

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

// --- Compact Integration Tests ---

func TestCompact_RunsAtInterval(t *testing.T) {
	old := managed.HeartbeatInterval
	managed.HeartbeatInterval = 100 * time.Millisecond
	defer func() { managed.HeartbeatInterval = old }()

	ts, store, mock := setupMockTestServer(t)
	sess, _ := store.CreateManagedSession("/tmp/test-compact-int", `["Bash"]`, 50, 5.0, 1)

	// Turn 1: max_turns (triggers auto-continue)
	// Turn 2 (/compact): emits result (compact turn)
	// Turn 3 (continuation): natural completion
	pp := newPersistentProc([]turnSpec{
		{assistantCount: 3, result: maxTurnsResultLine}, // User turn → max_turns
		{assistantCount: 0, result: resultLine},          // /compact turn
		{assistantCount: 2, result: resultLine},          // Continuation → natural end
	})

	var sendCount int32
	mock.OnEnsureProcess = func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
		return pp.proc, nil
	}
	mock.OnSendTurn = func(sessionID, msg string) error {
		atomic.AddInt32(&sendCount, 1)
		pp.triggerCh <- struct{}{}
		return nil
	}
	mock.OnGracefulShutdown = func(sessionID string, timeout time.Duration) error {
		close(pp.shutdownCh)
		return nil
	}

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
