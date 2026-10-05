package web

import (
	"io/fs"
	"strings"
	"testing"
)

// Issue #305: clicking a pipeline run in the Pipeline Runs sidebar list must
// open a detail view that takes over the main chat pane (same pattern as
// selecting a session), not a bottom panel or modal.
func TestIndexHTMLPipelineRunTakeover(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal("failed to read index.html:", err)
	}
	html := string(data)

	if !strings.Contains(html, `id="pipeline-run-takeover"`) {
		t.Error("expected a pipeline-run-takeover view in the chat pane")
	}
	if !strings.Contains(html, "/pipeline-run-view.js") {
		t.Error("expected index.html to load pipeline-run-view.js")
	}
	// The chat empty-state must yield to the takeover view.
	if !strings.Contains(html, "!pipelineRunViewActive() && !selectedSessionId") {
		t.Error("expected chat empty-state to be guarded by !pipelineRunViewActive()")
	}
	// The old bottom-of-chat detail panels must be gone.
	if strings.Contains(html, "<!-- Pipeline Run Detail Panel -->") {
		t.Error("expected the bottom Pipeline Run Detail Panel to be replaced by the takeover view")
	}
	if strings.Contains(html, "<!-- Tool Workflow Run Detail Panel") {
		t.Error("expected the bottom Tool Workflow Run Detail Panel to be replaced by the takeover view")
	}
}

// The pipeline-run-view helper module must ship in the embedded static FS and
// expose the takeover-state helpers to both the browser and node tests.
func TestPipelineRunViewModuleEmbedded(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/pipeline-run-view.js")
	if err != nil {
		t.Fatal("failed to read pipeline-run-view.js:", err)
	}
	js := string(data)
	for _, fn := range []string{"pipelineRunViewActive", "pipelineRunLogEntries", "shouldPollPipelineRunDetail"} {
		if !strings.Contains(js, "export function "+fn) {
			t.Errorf("expected pipeline-run-view.js to export %s", fn)
		}
	}
	if !strings.Contains(js, "window._ccPipelineRunViewActive") {
		t.Error("expected pipeline-run-view.js to register a browser global for app.js")
	}
}

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
