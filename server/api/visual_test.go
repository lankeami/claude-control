package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// visualTestDir creates a temporary .superpowers/brainstorm/ directory
// and configures the visual handler to use it. Returns the brainstorm dir path.
func visualTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".superpowers", "brainstorm")
	os.MkdirAll(dir, 0755)
	brainstormRootOverride = dir
	t.Cleanup(func() { brainstormRootOverride = "" })
	return dir
}

func TestVisualIndex_EmptyDir(t *testing.T) {
	ts, _ := newTestServer(t)
	visualTestDir(t)

	resp, err := http.Get(ts.URL + "/visual/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestVisualIndex_ListsSessions(t *testing.T) {
	ts, _ := newTestServer(t)

	brainstormDir := filepath.Join(visualTestDir(t), "test-session")
	os.MkdirAll(brainstormDir, 0755)
	os.WriteFile(filepath.Join(brainstormDir, "screen.html"), []byte("<h1>Test</h1>"), 0644)

	resp, err := http.Get(ts.URL + "/visual/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "test-session") {
		t.Errorf("expected session listing to contain 'test-session', got: %s", body[:200])
	}
}

func TestVisualSession_ServesNewestHTML(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess1")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "old.html"), []byte("<h1>Old</h1>"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "new.html"), []byte("<h1>New</h1>"), 0644)
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(sessionDir, "old.html"), past, past)

	resp, err := http.Get(ts.URL + "/visual/sess1/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "New") {
		t.Errorf("expected newest HTML content, got: %s", body[:200])
	}
}

func TestVisualSession_PrefersIndexHTML(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess2")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "other.html"), []byte("<h1>Other</h1>"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "index.html"), []byte("<h1>Index</h1>"), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess2/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "Index") {
		t.Errorf("expected index.html content, got: %s", body[:200])
	}
}

func TestVisualSession_WrapsFragments(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess3")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "frag.html"), []byte("<h1>Fragment</h1>"), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess3/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("expected fragment to be wrapped in full document")
	}
	if !strings.Contains(body, "Fragment") {
		t.Errorf("expected fragment content to be present")
	}
}

func TestVisualSession_ServesFullDocumentUnchanged(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess4")
	os.MkdirAll(sessionDir, 0755)
	fullDoc := "<!DOCTYPE html><html><body><h1>Full</h1></body></html>"
	os.WriteFile(filepath.Join(sessionDir, "full.html"), []byte(fullDoc), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess4/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(bodyBytes), "<h1>Full</h1>") {
		t.Errorf("expected full document served as-is")
	}
}

func TestVisualFile_ServesSpecificFile(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess5")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "style.css"), []byte("body { color: red; }"), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess5/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("expected CSS content type, got %s", ct)
	}
}

func TestVisualFile_BlocksPathTraversal(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess6")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "ok.html"), []byte("<h1>OK</h1>"), 0644)

	// Go's HTTP client resolves ../ before sending, so we use a raw request
	// to test server-side path traversal protection.
	// Test that a crafted sessionID with encoded traversal is blocked.
	req, _ := http.NewRequest("GET", ts.URL+"/visual/%2e%2e%2f%2e%2e%2fetc%2fpasswd/ok.html", nil)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("path traversal in sessionID should be blocked")
	}

	// Test that a crafted filename with encoded traversal is blocked.
	req, _ = http.NewRequest("GET", ts.URL+"/visual/sess6/%2e%2e%2f%2e%2e%2fetc%2fpasswd", nil)
	resp, err = http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("path traversal in filename should be blocked")
	}
}

func TestVisualSession_NotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	visualTestDir(t)

	resp, err := http.Get(ts.URL + "/visual/nonexistent/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestVisualFile_CacheHeaders(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess7")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "test.html"), []byte("<h1>Test</h1>"), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess7/test.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", cc)
	}
}

func TestVisualFile_CSPHeaders(t *testing.T) {
	ts, _ := newTestServer(t)

	sessionDir := filepath.Join(visualTestDir(t), "sess8")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "test.html"), []byte("<h1>Test</h1>"), 0644)

	resp, err := http.Get(ts.URL + "/visual/sess8/test.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if csp != "default-src 'self' 'unsafe-inline'" {
		t.Errorf("expected CSP header, got %q", csp)
	}
}

func TestVisualRoute_RegisteredBeforeSPA(t *testing.T) {
	ts, _ := newTestServer(t)
	visualTestDir(t)

	resp, err := http.Get(ts.URL + "/visual/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "Visual Companion") {
		t.Errorf("expected Visual Companion index page, got SPA fallback")
	}
}
