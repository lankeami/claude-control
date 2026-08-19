package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestViewportMetaAllowsZoom(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if strings.Contains(html, "maximum-scale=1.0") {
		t.Error("viewport meta contains maximum-scale=1.0 — pinch-to-zoom is blocked on mobile")
	}
	if !strings.Contains(html, `name="viewport"`) {
		t.Error("viewport meta tag not found")
	}
}

func TestSandboxAllowsSameOrigin(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	js := string(body)

	if strings.Contains(js, `sandbox=""`) {
		t.Error("iframe sandbox is maximally restrictive (sandbox=\"\") — breaks touch scrolling on iOS")
	}
	if !strings.Contains(js, `sandbox="allow-same-origin"`) {
		t.Error("iframe sandbox should use allow-same-origin for proper touch interaction")
	}
}

func TestMobilePreviewTouchAction(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	css := string(body)

	if !strings.Contains(css, "html-preview-frame") {
		t.Fatal("html-preview-frame class not found in CSS")
	}
	if !strings.Contains(css, "pinch-zoom") {
		t.Error("CSS should include touch-action with pinch-zoom on .html-preview-frame for mobile scroll/zoom support")
	}
}
