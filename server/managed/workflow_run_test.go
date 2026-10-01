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

	// agent1: started + result in journal, has meta + transcript (label source).
	// agent2: started only — running.
	// agent3: meta file only, no journal entry yet — pending.
	writeFileAppend(t, filepath.Join(runDir, "journal.jsonl"),
		`{"type":"started","key":"v2:k1","agentId":"agent1"}`+"\n"+
			`{"type":"result","key":"v2:k1","agentId":"agent1","result":{"issueNumber":292}}`+"\n"+
			`{"type":"started","key":"v2:k2","agentId":"agent2"}`+"\n")
	writeFileAppend(t, filepath.Join(runDir, "agent-agent1.meta.json"), `{"agentType":"workflow-subagent","spawnDepth":1}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent3.meta.json"), `{"agentType":"workflow-subagent","spawnDepth":1}`)
	writeFileAppend(t, filepath.Join(runDir, "agent-agent1.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"agentId":"agent1","type":"user","message":{"role":"user","content":"Create a GitHub issue for: \"filter textboxes\".\nInvoke Skill(\"git-issue-create\")."}}`+"\n")

	snap := readWorkflowRun(runDir, "wf_s1")

	if len(snap.Agents) != 3 {
		t.Fatalf("agents=%d (%+v), want 3", len(snap.Agents), snap.Agents)
	}
	a1, a2, a3 := snap.Agents[0], snap.Agents[1], snap.Agents[2]

	if a1.ID != "agent1" || a1.Status != "complete" {
		t.Errorf("agent1=%+v, want complete", a1)
	}
	if a1.AgentType != "workflow-subagent" {
		t.Errorf("agent1 agent_type=%q, want workflow-subagent (from meta.json)", a1.AgentType)
	}
	if !strings.Contains(a1.Label, "Create a GitHub issue") {
		t.Errorf("agent1 label=%q, want prompt-derived label", a1.Label)
	}
	if strings.Contains(a1.Label, "\n") {
		t.Errorf("agent1 label=%q, must be single-line", a1.Label)
	}

	if a2.ID != "agent2" || a2.Status != "running" {
		t.Errorf("agent2=%+v, want running", a2)
	}

	if a3.ID != "agent3" || a3.Status != "pending" {
		t.Errorf("agent3=%+v, want pending (meta only, not started)", a3)
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
