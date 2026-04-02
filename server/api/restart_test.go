package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestHandleRestart_Success(t *testing.T) {
	var called atomic.Bool
	s := &Server{
		shutdownFunc: func() { called.Store(true) },
	}

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Give goroutine time to call shutdownFunc
	time.Sleep(600 * time.Millisecond)
	if !called.Load() {
		t.Fatal("expected shutdownFunc to be called")
	}
}

func TestHandleRestart_ConcurrentBlocked(t *testing.T) {
	s := &Server{
		shutdownFunc: func() {},
	}
	s.restartInProgress.Store(true)

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestHandleRestart_SetsActivityStatesToWaiting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a managed session with working state
	sess, err := store.CreateManagedSession("/tmp", "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	store.UpdateActivityState(sess.ID, "working")

	var called atomic.Bool
	s := &Server{
		store:        store,
		shutdownFunc: func() { called.Store(true) },
	}

	req := httptest.NewRequest("POST", "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Activity state update happens synchronously before response
	updated, err := store.GetSessionByID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActivityState != "waiting" {
		t.Fatalf("expected activity_state 'waiting', got '%s'", updated.ActivityState)
	}
}
