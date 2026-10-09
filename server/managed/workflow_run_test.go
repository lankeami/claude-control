package managed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collectSnapshots runs WatchWorkflowRuns in a goroutine and returns a channel
// of emitted snapshots plus a cancel func.
func collectSnapshots(t *testing.T, dir string) (<-chan WorkflowRunSnapshot, context.CancelFunc) {
	t.Helper()
	ch := make(chan WorkflowRunSnapshot, 32)
	ctx, cancel := context.WithCancel(context.Background())
	go WatchWorkflowRuns(ctx, dir, func(snap WorkflowRunSnapshot) {
		ch <- snap
	})
	return ch, cancel
}

func waitForSnapshot(t *testing.T, ch <-chan WorkflowRunSnapshot, match func(WorkflowRunSnapshot) bool) WorkflowRunSnapshot {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case snap := <-ch:
			if match(snap) {
				return snap
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching workflow run snapshot")
		}
	}
}

func TestWorkflowRunWatcherDetectsRunDir(t *testing.T) {
	oldInterval := WorkflowRunPollInterval
	WorkflowRunPollInterval = 20 * time.Millisecond
	defer func() { WorkflowRunPollInterval = oldInterval }()

	// workflowsDir doesn't exist yet — the watcher must tolerate that.
	base := t.TempDir()
	workflowsDir := filepath.Join(base, "subagents", "workflows")

	ch, cancel := collectSnapshots(t, workflowsDir)
	defer cancel()

	// Create a run dir after the watcher is already running.
	runDir := filepath.Join(workflowsDir, "wf_abc123-1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	snap := waitForSnapshot(t, ch, func(s WorkflowRunSnapshot) bool {
		return s.RunID == "wf_abc123-1"
	})
	if snap.Type != "workflow_run" {
		t.Errorf("type=%q, want workflow_run", snap.Type)
	}
	if snap.Status != "running" {
		t.Errorf("status=%q, want running", snap.Status)
	}
	if len(snap.Agents) != 0 {
		t.Errorf("agents=%d, want 0 for a fresh run dir", len(snap.Agents))
	}
}

func TestWorkflowRunWatcherIgnoresNonRunEntries(t *testing.T) {
	oldInterval := WorkflowRunPollInterval
	WorkflowRunPollInterval = 20 * time.Millisecond
	defer func() { WorkflowRunPollInterval = oldInterval }()

	workflowsDir := t.TempDir()
	// A stray file (not a dir) must not be treated as a run.
	if err := os.WriteFile(filepath.Join(workflowsDir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ch, cancel := collectSnapshots(t, workflowsDir)
	defer cancel()

	select {
	case snap := <-ch:
		t.Fatalf("unexpected snapshot for non-dir entry: %+v", snap)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWorkflowRunWorkflowsDirForTranscript(t *testing.T) {
	got := WorkflowsDirForTranscript("/home/u/.claude/projects/-proj/abcd-1234.jsonl")
	want := filepath.Join("/home/u/.claude/projects/-proj/abcd-1234", "subagents", "workflows")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func writeFileAppend(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestWorkflowRunJournalTailStartedAndResult(t *testing.T) {
	oldInterval := WorkflowRunPollInterval
	WorkflowRunPollInterval = 20 * time.Millisecond
	defer func() { WorkflowRunPollInterval = oldInterval }()

	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_j1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(runDir, "journal.jsonl")
	writeFileAppend(t, journal, `{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n")

	ch, cancel := collectSnapshots(t, workflowsDir)
	defer cancel()

	snap := waitForSnapshot(t, ch, func(s WorkflowRunSnapshot) bool {
		return s.RunID == "wf_j1" && len(s.Agents) == 1
	})
	if snap.Agents[0].ID != "agent1" || snap.Agents[0].Status != "running" {
		t.Errorf("agent=%+v, want id=agent1 status=running", snap.Agents[0])
	}
	if snap.Status != "running" {
		t.Errorf("run status=%q, want running", snap.Status)
	}

	// Append the result while the watcher is live — agent flips to complete
	// with the structured result attached, and the run completes.
	writeFileAppend(t, journal, `{"type":"result","key":"v2:k1","agentId":"agent1","result":{"issueUrl":"https://github.com/o/r/issues/5"}}`+"\n")

	snap = waitForSnapshot(t, ch, func(s WorkflowRunSnapshot) bool {
		return s.RunID == "wf_j1" && len(s.Agents) == 1 && s.Agents[0].Status == "complete"
	})
	if !strings.Contains(string(snap.Agents[0].Result), "issues/5") {
		t.Errorf("result=%s, want issueUrl payload", snap.Agents[0].Result)
	}
	if snap.Status != "completed" {
		t.Errorf("run status=%q, want completed", snap.Status)
	}
}

func TestWorkflowRunJournalTailPartialLine(t *testing.T) {
	oldInterval := WorkflowRunPollInterval
	WorkflowRunPollInterval = 20 * time.Millisecond
	defer func() { WorkflowRunPollInterval = oldInterval }()

	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_j2")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(runDir, "journal.jsonl")
	writeFileAppend(t, journal, `{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n")
	// Simulate catching the writer mid-append: a trailing line with no newline.
	writeFileAppend(t, journal, `{"type":"started","key":"v2:k2","agentI`)

	ch, cancel := collectSnapshots(t, workflowsDir)
	defer cancel()

	// Only the complete line must be parsed — the partial one is ignored.
	snap := waitForSnapshot(t, ch, func(s WorkflowRunSnapshot) bool {
		return s.RunID == "wf_j2" && len(s.Agents) >= 1
	})
	if len(snap.Agents) != 1 || snap.Agents[0].ID != "agent1" {
		t.Fatalf("agents=%+v, want only agent1 (partial line ignored)", snap.Agents)
	}

	// Finish the partial line — the second agent appears.
	writeFileAppend(t, journal, `d":"agent2"}`+"\n")
	snap = waitForSnapshot(t, ch, func(s WorkflowRunSnapshot) bool {
		return s.RunID == "wf_j2" && len(s.Agents) == 2
	})
	if snap.Agents[1].ID != "agent2" || snap.Agents[1].Status != "running" {
		t.Errorf("agent2=%+v, want id=agent2 status=running", snap.Agents[1])
	}
}

func TestWorkflowRunJournalIgnoresUnknownEntries(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_j3")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(runDir, "journal.jsonl")
	writeFileAppend(t, journal,
		`{"type":"log","message":"future shape"}`+"\n"+
			`not json at all`+"\n"+
			`{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n")

	snap := readWorkflowRun(runDir, "wf_j3")
	if len(snap.Agents) != 1 || snap.Agents[0].ID != "agent1" {
		t.Errorf("agents=%+v, want only agent1 (unknown lines skipped)", snap.Agents)
	}
}

func TestWorkflowRunAgentStatusDerivation(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_s1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// agent1: journal label "issue" + meta description + transcript → journal label wins.
	// agent2: no journal label, meta description "branch setup" → meta wins.
	// agent3: meta file only, no journal entry yet — pending, meta description as label.
	// agent4: no journal label, no meta description → transcript fallback.
	writeFileAppend(t, filepath.Join(runDir, "journal.jsonl"),
		`{"type":"started","key":"v2:k1","agentId":"agent1","label":"issue","phase":"Issue"}`+"\n"+
			`{"type":"result","key":"v2:k1","agentId":"agent1","result":{"issueNumber":292}}`+"\n"+
			`{"type":"started","key":"v2:k2","agentId":"agent2"}`+"\n"+
			`{"type":"started","key":"v2:k4","agentId":"agent4"}`+"\n")
	writeFileAppend(t, filepath.Join(runDir, "agent-agent1.meta.json"), `{"agentType":"workflow-subagent","description":"create issue","workflowPhase":"Issue","spawnDepth":1}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent2.meta.json"), `{"agentType":"workflow-subagent","description":"branch setup","spawnDepth":1}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent3.meta.json"), `{"agentType":"workflow-subagent","description":"pending step","spawnDepth":1}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent1.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"agentId":"agent1","type":"user","message":{"role":"user","content":"Create a GitHub issue for: \"filter textboxes\".\nInvoke Skill(\"git-issue-create\")."}}`+"\n")
	writeFileAppend(t, filepath.Join(runDir, "agent-agent4.jsonl"),
		`{"type":"user","message":{"role":"user","content":"Run the test suite and report results."}}`+"\n")

	snap := readWorkflowRun(runDir, "wf_s1")

	if len(snap.Agents) != 4 {
		t.Fatalf("agents=%d (%+v), want 4", len(snap.Agents), snap.Agents)
	}
	// Journal-ordered agents come first (agent1, agent2, agent4), then
	// meta-only pending agents (agent3) from directory scan.
	a1, a2 := snap.Agents[0], snap.Agents[1]
	a4, a3 := snap.Agents[2], snap.Agents[3]

	// agent1: journal label takes priority over meta description and transcript.
	if a1.ID != "agent1" || a1.Status != "complete" {
		t.Errorf("agent1=%+v, want complete", a1)
	}
	if a1.AgentType != "workflow-subagent" {
		t.Errorf("agent1 agent_type=%q, want workflow-subagent (from meta.json)", a1.AgentType)
	}
	if a1.Label != "issue" {
		t.Errorf("agent1 label=%q, want %q (from journal)", a1.Label, "issue")
	}
	if a1.Phase != "Issue" {
		t.Errorf("agent1 phase=%q, want %q (from journal)", a1.Phase, "Issue")
	}

	// agent2: no journal label → meta description used.
	if a2.ID != "agent2" || a2.Status != "running" {
		t.Errorf("agent2=%+v, want running", a2)
	}
	if a2.Label != "branch setup" {
		t.Errorf("agent2 label=%q, want %q (from meta description)", a2.Label, "branch setup")
	}

	// agent3: pending (meta only), label from meta description.
	if a3.ID != "agent3" || a3.Status != "pending" {
		t.Errorf("agent3=%+v, want pending (meta only, not started)", a3)
	}
	if a3.Label != "pending step" {
		t.Errorf("agent3 label=%q, want %q (from meta description)", a3.Label, "pending step")
	}

	// agent4: no journal label, no meta description → transcript fallback.
	if a4.ID != "agent4" || a4.Status != "running" {
		t.Errorf("agent4=%+v, want running", a4)
	}
	if !strings.Contains(a4.Label, "Run the test suite") {
		t.Errorf("agent4 label=%q, want transcript-derived label containing %q", a4.Label, "Run the test suite")
	}

	// A pending agent means the run is still in flight.
	if snap.Status != "running" {
		t.Errorf("run status=%q, want running", snap.Status)
	}
}

func TestWorkflowRunAgentLabelTruncated(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_s2")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("implement the feature and ", 20) // ~520 chars
	writeFileAppend(t, filepath.Join(runDir, "journal.jsonl"),
		`{"type":"started","key":"v2:k1","agentId":"agentX"}`+"\n")
	writeFileAppend(t, filepath.Join(runDir, "agent-agentX.jsonl"),
		`{"type":"user","message":{"role":"user","content":"`+long+`"}}`+"\n")

	snap := readWorkflowRun(runDir, "wf_s2")
	if len(snap.Agents) != 1 {
		t.Fatalf("agents=%d, want 1", len(snap.Agents))
	}
	if got := len(snap.Agents[0].Label); got == 0 || got > 140 {
		t.Errorf("label length=%d, want 1..140", got)
	}
}

// Bug 2 (issue #307): a run whose journal has agents started but no result,
// and whose journal file hasn't been modified for over WorkflowRunStaleAfter,
// must be reported as "stale" instead of running forever.
func TestWorkflowRunStaleJournalMarksRunStale(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_stale1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(runDir, "journal.jsonl")
	writeFileAppend(t, journal, `{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n")

	// Fresh journal: still running, not stale.
	snap := readWorkflowRun(runDir, "wf_stale1")
	if snap.Status != "running" {
		t.Fatalf("fresh journal status=%q, want running", snap.Status)
	}

	// Age the journal past the staleness threshold.
	old := time.Now().Add(-WorkflowRunStaleAfter - time.Minute)
	if err := os.Chtimes(journal, old, old); err != nil {
		t.Fatal(err)
	}

	snap = readWorkflowRun(runDir, "wf_stale1")
	if snap.Status != "stale" {
		t.Errorf("aged journal status=%q, want stale", snap.Status)
	}
	if len(snap.Agents) != 1 || snap.Agents[0].Status != "running" {
		t.Errorf("agents=%+v, want agent1 still reported running", snap.Agents)
	}
}

// A completed run must never be reported stale, no matter how old the journal.
func TestWorkflowRunCompletedNeverStale(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_stale2")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(runDir, "journal.jsonl")
	writeFileAppend(t, journal,
		`{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n"+
			`{"type":"result","key":"v2:k1","agentId":"agent1","result":{"ok":true}}`+"\n")
	old := time.Now().Add(-WorkflowRunStaleAfter - time.Hour)
	if err := os.Chtimes(journal, old, old); err != nil {
		t.Fatal(err)
	}

	snap := readWorkflowRun(runDir, "wf_stale2")
	if snap.Status != "completed" {
		t.Errorf("status=%q, want completed (staleness must not apply)", snap.Status)
	}
}

// Bug 2b (issue #307): a run dir with zero agents (no journal, no meta files)
// currently reports running indefinitely. A fresh zero-agent dir is a run
// that just started — still running — but once the dir itself is older than
// the staleness threshold it must flip to stale.
func TestWorkflowRunZeroAgentDirGoesStale(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_stale3")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Fresh empty run dir: running (existing contract).
	snap := readWorkflowRun(runDir, "wf_stale3")
	if snap.Status != "running" {
		t.Fatalf("fresh zero-agent status=%q, want running", snap.Status)
	}

	old := time.Now().Add(-WorkflowRunStaleAfter - time.Minute)
	if err := os.Chtimes(runDir, old, old); err != nil {
		t.Fatal(err)
	}

	snap = readWorkflowRun(runDir, "wf_stale3")
	if snap.Status != "stale" {
		t.Errorf("aged zero-agent status=%q, want stale", snap.Status)
	}
	if len(snap.Agents) != 0 {
		t.Errorf("agents=%d, want 0", len(snap.Agents))
	}
}

// Issue #316: Agent description must be propagated as a separate field,
// not only used as a label fallback. When a meta description exists it
// should appear in Description regardless of whether the label comes
// from the journal or the transcript.
func TestAgentDescriptionPropagation(t *testing.T) {
	workflowsDir := t.TempDir()
	runDir := filepath.Join(workflowsDir, "wf_desc1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// agent1: journal label "issue" + meta description "create issue" → label from journal, description from meta
	// agent2: no journal label, meta description "branch setup" → label from meta (fallback), description from meta
	// agent3: no meta at all → description empty
	writeFileAppend(t, filepath.Join(runDir, "journal.jsonl"),
		`{"type":"started","key":"v2:k1","agentId":"agent1","label":"issue"}`+"\n"+
			`{"type":"started","key":"v2:k2","agentId":"agent2"}`+"\n"+
			`{"type":"started","key":"v2:k3","agentId":"agent3"}`+"\n")
	writeFileAppend(t, filepath.Join(runDir, "agent-agent1.meta.json"), `{"agentType":"workflow-subagent","description":"create issue"}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent2.meta.json"), `{"agentType":"workflow-subagent","description":"branch setup"}`)

	snap := readWorkflowRun(runDir, "wf_desc1")
	if len(snap.Agents) != 3 {
		t.Fatalf("agents=%d, want 3", len(snap.Agents))
	}

	a1, a2, a3 := snap.Agents[0], snap.Agents[1], snap.Agents[2]

	// agent1: label from journal, description always from meta
	if a1.Label != "issue" {
		t.Errorf("agent1 label=%q, want %q", a1.Label, "issue")
	}
	if a1.Description != "create issue" {
		t.Errorf("agent1 description=%q, want %q (always from meta)", a1.Description, "create issue")
	}

	// agent2: label falls back to meta description, but description field is also populated
	if a2.Label != "branch setup" {
		t.Errorf("agent2 label=%q, want %q", a2.Label, "branch setup")
	}
	if a2.Description != "branch setup" {
		t.Errorf("agent2 description=%q, want %q", a2.Description, "branch setup")
	}

	// agent3: no meta file → empty description
	if a3.Description != "" {
		t.Errorf("agent3 description=%q, want empty (no meta)", a3.Description)
	}
}
