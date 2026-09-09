package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestIndexHTMLContainsSessionFilterClearButton(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal("failed to read index.html:", err)
	}
	html := string(data)

	if count := strings.Count(html, `sessionFilter = ''`); count < 2 {
		t.Errorf("expected at least 2 session filter clear handlers (desktop + mobile), found %d", count)
	}
}
