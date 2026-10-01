package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func setupWorkflowRunTestServer(t *testing.T) (*Server, *httptest.Server, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	mgr := managed.NewManager(managed.Config{ClaudeBin: "echo"})
	s := &Server{
		store:            store,
		manager:          mgr,
		permissions:      NewPermissionManager(),
		pendingQuestions: NewPendingQuestionManager(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		s.handleSessionStream(w, r, "test-api-key")
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts, store
}

// streamLines opens the session SSE stream and returns a channel of data lines.
func streamLines(t *testing.T, url string) <-chan string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != 200 {
		t.Fatalf("stream status=%d, want 200", resp.StatusCode)
	}
	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	return lines
}

func expectSSELine(t *testing.T, lines <-chan string, substr string) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before seeing %q", substr)
			}
			if strings.Contains(line, substr) {
				return line
			}
		case <-deadline:
			t.Fatalf("no SSE line containing %q within 2s", substr)
		}
	}
}

func TestWorkflowRunEventsBroadcastOnSessionStream(t *testing.T) {
	s, ts, store := setupWorkflowRunTestServer(t)

	sess, err := store.CreateManagedSession("/tmp/wfrun-test", `["Read"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	lines := streamLines(t, ts.URL+"/api/sessions/"+sess.ID+"/stream?token=test-api-key")

	snapshot := `{"type":"workflow_run","run_id":"wf_live1","status":"running","agents":[{"id":"a1","status":"running"}]}`
	s.emitWorkflowRun(sess.ID, snapshot)

	line := expectSSELine(t, lines, `"workflow_run"`)
	if !strings.Contains(line, "wf_live1") {
		t.Errorf("line=%q, want run_id wf_live1", line)
	}
}

func TestWorkflowRunEventsReplayOnConnect(t *testing.T) {
	s, ts, store := setupWorkflowRunTestServer(t)

	sess, err := store.CreateManagedSession("/tmp/wfrun-replay", `["Read"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Two snapshots for the same run land before any client connects —
	// a late-connecting client must receive only the latest state.
	s.emitWorkflowRun(sess.ID, `{"type":"workflow_run","run_id":"wf_replay1","status":"running","agents":[]}`)
	s.emitWorkflowRun(sess.ID, `{"type":"workflow_run","run_id":"wf_replay1","status":"running","agents":[{"id":"a1","status":"complete","result":{"issueUrl":"https://github.com/o/r/issues/9"}}]}`)

	lines := streamLines(t, ts.URL+"/api/sessions/"+sess.ID+"/stream?token=test-api-key")

	line := expectSSELine(t, lines, "wf_replay1")
	if !strings.Contains(line, "issues/9") {
		t.Errorf("replayed line=%q, want the latest snapshot with the result payload", line)
	}
}

func TestWorkflowRunEventsIsolatedPerSession(t *testing.T) {
	s, ts, store := setupWorkflowRunTestServer(t)

	sessA, _ := store.CreateManagedSession("/tmp/wfrun-a", `["Read"]`, 50, 5.0, 0)
	sessB, _ := store.CreateManagedSession("/tmp/wfrun-b", `["Read"]`, 50, 5.0, 0)

	s.emitWorkflowRun(sessA.ID, `{"type":"workflow_run","run_id":"wf_sessA","status":"running","agents":[]}`)

	lines := streamLines(t, ts.URL+"/api/sessions/"+sessB.ID+"/stream?token=test-api-key")
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case line := <-lines:
			if strings.Contains(line, "wf_sessA") {
				t.Fatalf("session B stream received session A's workflow run: %q", line)
			}
		case <-deadline:
			return
		}
	}
}
