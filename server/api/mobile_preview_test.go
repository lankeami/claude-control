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

func TestPreviewScrollWrapper(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	js := string(body)

	if !strings.Contains(js, "html-preview-scroll") {
		t.Error("iframe should be wrapped in html-preview-scroll div for cross-platform scrolling")
	}
	if !strings.Contains(js, "onload=") {
		t.Error("iframe should have onload handler to resize to content dimensions")
	}

	resp2, _ := http.Get(ts.URL + "/style.css")
	defer resp2.Body.Close()
	cssBody, _ := io.ReadAll(resp2.Body)
	css := string(cssBody)

	if !strings.Contains(css, "html-preview-scroll") {
		t.Error("CSS should define html-preview-scroll as the scroll container")
	}
}
