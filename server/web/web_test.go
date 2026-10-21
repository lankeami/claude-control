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

// Issue #310: pipeline runs must not disappear when switching sessions.
// The toolWorkflowRuns array is global and must not be cleared on session switch.
func TestPipelineRunsSurviveSessionSwitch(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/app.js")
	if err != nil {
		t.Fatal("failed to read app.js:", err)
	}
	js := string(data)

	// The selectSession method must NOT clear toolWorkflowRuns.
	// Look for the pattern: it should NOT contain "toolWorkflowRuns = []"
	// inside selectSession. We check that selectSession exists but does not
	// reset toolWorkflowRuns.
	if !strings.Contains(js, "async selectSession(") {
		t.Fatal("expected app.js to have a selectSession method")
	}

	// Find the selectSession method body and check it doesn't clear toolWorkflowRuns
	selectIdx := strings.Index(js, "async selectSession(")
	if selectIdx < 0 {
		t.Fatal("could not locate selectSession method")
	}
	// Look for the next method definition (roughly) — check ~2000 chars
	methodBody := js[selectIdx:]
	if len(methodBody) > 2000 {
		methodBody = methodBody[:2000]
	}
	if strings.Contains(methodBody, "toolWorkflowRuns = []") {
		t.Error("selectSession must NOT clear toolWorkflowRuns — pipeline runs should survive session switches (issue #310)")
	}
}

// Issue #310: pipeline runs section must have a refresh button.
func TestPipelineRunsRefreshButton(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal("failed to read index.html:", err)
	}
	html := string(data)

	if !strings.Contains(html, "refreshPipelineRuns") {
		t.Error("expected index.html to have a refreshPipelineRuns button/action (issue #310)")
	}
}

// Issue #310: app.js must have a refreshPipelineRuns method that calls the refresh endpoint.
func TestPipelineRunsRefreshMethod(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/app.js")
	if err != nil {
		t.Fatal("failed to read app.js:", err)
	}
	js := string(data)

	if !strings.Contains(js, "refreshPipelineRuns") {
		t.Error("expected app.js to define a refreshPipelineRuns method (issue #310)")
	}
	if !strings.Contains(js, "/api/pipeline-runs/refresh") {
		t.Error("expected app.js to call /api/pipeline-runs/refresh endpoint (issue #310)")
	}
}

// Issue #307: the pipeline runs list must surface the "stale" workflow-run
// status distinctly (dedicated pill style + label), not fall back to generic
// rendering.
func TestPipelineRunsListSurfacesStaleStatus(t *testing.T) {
	css, err := fs.ReadFile(staticFiles, "static/style.css")
	if err != nil {
		t.Fatal("failed to read style.css:", err)
	}
	if !strings.Contains(string(css), ".pipeline-status-pill.stale") {
		t.Error("expected style.css to define a .pipeline-status-pill.stale style")
	}

	app, err := fs.ReadFile(staticFiles, "static/app.js")
	if err != nil {
		t.Fatal("failed to read app.js:", err)
	}
	if !strings.Contains(string(app), "stale:") {
		t.Error("expected app.js pipelineStatusLabel to include a stale label")
	}
}

// Issue #318: Issues and Pull Requests sidebar panels must have refresh
// buttons and opening the accordion should trigger a data refresh.

func TestIssuesSectionHasRefreshButton(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal("failed to read index.html:", err)
	}
	html := string(data)

	// The Issues header must contain a refresh button (like Skills has)
	if !strings.Contains(html, "refreshIssues") {
		t.Error("expected index.html Issues section to have a refreshIssues button/action (issue #318)")
	}
}

func TestPullRequestsSectionHasRefreshButton(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatal("failed to read index.html:", err)
	}
	html := string(data)

	// The Pull Requests header must contain a refresh button (like Skills has)
	if !strings.Contains(html, "refreshPulls") {
		t.Error("expected index.html Pull Requests section to have a refreshPulls button/action (issue #318)")
	}
}

func TestAccordionExpandTriggersRefresh(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/app.js")
	if err != nil {
		t.Fatal("failed to read app.js:", err)
	}
	js := string(data)

	// app.js must define refreshIssues and refreshPulls methods
	if !strings.Contains(js, "refreshIssues(") {
		t.Error("expected app.js to define a refreshIssues method (issue #318)")
	}
	if !strings.Contains(js, "refreshPulls(") {
		t.Error("expected app.js to define a refreshPulls method (issue #318)")
	}

	// app.js must define expandIssues and expandPulls methods that fetch on expand
	if !strings.Contains(js, "expandIssues(") {
		t.Error("expected app.js to define an expandIssues method that fetches on expand (issue #318)")
	}
	if !strings.Contains(js, "expandPulls(") {
		t.Error("expected app.js to define an expandPulls method that fetches on expand (issue #318)")
	}
	// Skills accordion should also refresh on expand
	if !strings.Contains(js, "expandSkills(") {
		t.Error("expected app.js to define an expandSkills method that fetches on expand (issue #318)")
	}
}

// Issue #337: switchToPullBranch must run a direct shell command (git stash + checkout)
// instead of sending a prompt to Claude, which wastes credits.
func TestSwitchToPullBranchUsesShellNotPrompt(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/app.js")
	if err != nil {
		t.Fatal("failed to read app.js:", err)
	}
	js := string(data)

	// Find the switchToPullBranch method body
	idx := strings.Index(js, "switchToPullBranch(")
	if idx < 0 {
		t.Fatal("expected app.js to have a switchToPullBranch method")
	}
	methodBody := js[idx:]
	if len(methodBody) > 1500 {
		methodBody = methodBody[:1500]
	}

	// Must NOT send a Claude prompt via sendManagedMessage (wastes credits)
	if strings.Contains(methodBody, "sendManagedMessage") {
		t.Error("switchToPullBranch must NOT call sendManagedMessage — use a direct shell command instead (issue #337)")
	}

	// Must use the shell endpoint for managed sessions (executeShell or /shell API)
	if !strings.Contains(methodBody, "/shell") && !strings.Contains(methodBody, "executeShell") {
		t.Error("switchToPullBranch must execute a shell command via executeShell or the /shell API (issue #337)")
	}

	// Must stash uncommitted changes before switching
	if !strings.Contains(methodBody, "git stash") {
		t.Error("switchToPullBranch must stash uncommitted changes before switching branches (issue #337)")
	}

	// Must checkout the PR branch
	if !strings.Contains(methodBody, "git checkout") || !strings.Contains(methodBody, "git fetch") {
		t.Error("switchToPullBranch must fetch and checkout the PR branch (issue #337)")
	}
}
