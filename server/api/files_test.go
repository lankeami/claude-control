package api

import (
	"encoding/json"
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
	router := NewRouter(store, "test-key", nil, envPath, nil, "test-server-id", nil, "default")

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

	t.Run("serves file outside session CWD", func(t *testing.T) {
		outsideDir := t.TempDir()
		outsidePath := filepath.Join(outsideDir, "outside.txt")
		os.WriteFile(outsidePath, []byte("outside content"), 0644)

		req := httptest.NewRequest("GET", "/api/files/raw?path="+outsidePath+"&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if w.Body.String() != "outside content" {
			t.Fatalf("expected 'outside content', got %q", w.Body.String())
		}
	})

	t.Run("serves audio with correct content-type", func(t *testing.T) {
		mp3Path := filepath.Join(dir, "test.mp3")
		os.WriteFile(mp3Path, []byte{0xFF, 0xFB, 0x90, 0x00}, 0644)

		req := httptest.NewRequest("GET", "/api/files/raw?path="+mp3Path+"&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "audio/mpeg" {
			t.Fatalf("expected Content-Type audio/mpeg, got %s", ct)
		}
	})

	t.Run("serves video with correct content-type", func(t *testing.T) {
		mp4Path := filepath.Join(dir, "test.mp4")
		os.WriteFile(mp4Path, []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}, 0644)

		req := httptest.NewRequest("GET", "/api/files/raw?path="+mp4Path+"&session_id="+sessID+"&key=test-key", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if ct != "video/mp4" {
			t.Fatalf("expected Content-Type video/mp4, got %s", ct)
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

func TestHandleGetFileContent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	envPath := filepath.Join(dir, ".env")
	router := NewRouter(store, "test-key", nil, envPath, nil, "test-server-id", nil, "default")

	sess, err := store.CreateManagedSession(dir, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}
	sessID := sess.ID

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/files/content?path="+path+"&session_id="+sessID, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("returns content for file inside CWD", func(t *testing.T) {
		p := filepath.Join(dir, "inside.txt")
		os.WriteFile(p, []byte("inside content"), 0644)

		w := get(p)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp fileContentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Exists || resp.Content != "inside content" {
			t.Fatalf("expected content 'inside content', got exists=%v content=%q", resp.Exists, resp.Content)
		}
	})

	t.Run("returns content for file outside session CWD", func(t *testing.T) {
		outsideDir := t.TempDir()
		p := filepath.Join(outsideDir, "outside.txt")
		os.WriteFile(p, []byte("outside content"), 0644)

		w := get(p)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp fileContentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Exists || resp.Content != "outside content" {
			t.Fatalf("expected content 'outside content', got exists=%v content=%q", resp.Exists, resp.Content)
		}
	})

	t.Run("nonexistent file reports exists=false", func(t *testing.T) {
		w := get(filepath.Join(dir, "nope.txt"))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp fileContentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Exists {
			t.Fatalf("expected exists=false")
		}
	})
}
