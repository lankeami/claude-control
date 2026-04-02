package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func TestHandleGetFileRaw(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	envPath := filepath.Join(dir, ".env")
	router := NewRouter(store, "test-key", nil, envPath, nil, "test-server-id")

	// Create a managed session with CWD set to dir
	sess, err := store.CreateManagedSession(dir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}
	sessID := sess.ID

	// Create a test PNG file (minimal PNG header bytes)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	}
	pngPath := filepath.Join(dir, "test.png")
	os.WriteFile(pngPath, pngData, 0644)

	// Create a test text file
	txtPath := filepath.Join(dir, "test.txt")
	os.WriteFile(txtPath, []byte("hello world"), 0644)

	t.Run("serves image with correct content-type", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/raw?path="+pngPath+"&session_id="+sessID+"&key=test-key", nil)
		// auth via query param (no Authorization header for <video>/<img> tags)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		ct := w.Header().Get("Content-Type")
		if ct != "image/png" {
			t.Fatalf("expected Content-Type image/png, got %s", ct)
		}
		if w.Body.Len() != len(pngData) {
			t.Fatalf("expected %d bytes, got %d", len(pngData), w.Body.Len())
		}
	})

	t.Run("missing key returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/raw?path="+pngPath+"&session_id="+sessID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("file outside CWD returns 403", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/raw?path=/etc/passwd&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("nonexistent file returns 404", func(t *testing.T) {
		noFile := filepath.Join(dir, "nope.png")
		req := httptest.NewRequest("GET", "/api/files/raw?path="+noFile+"&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("serves text file with correct content-type", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files/raw?path="+txtPath+"&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Body.String() != "hello world" {
			t.Fatalf("expected 'hello world', got %q", w.Body.String())
		}
	})
}
