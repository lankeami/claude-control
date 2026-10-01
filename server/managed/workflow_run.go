package managed

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkflowRunPollInterval controls how often the workflow-run watcher scans
// for new run directories and journal appends. Overridden in tests.
var WorkflowRunPollInterval = 500 * time.Millisecond

// WorkflowAgentStatus is the derived state of one subagent in a Workflow
// tool run.
type WorkflowAgentStatus struct {
	ID        string          `json:"id"`
	Label     string          `json:"label,omitempty"`
	AgentType string          `json:"agent_type,omitempty"`
	Status    string          `json:"status"` // "pending" | "running" | "complete"
	Result    json.RawMessage `json:"result,omitempty"`
}

// WorkflowRunSnapshot is the full state of one Workflow tool run, emitted on
// every observed change. Shapes match what the web UI renders from the
// per-session SSE stream.
type WorkflowRunSnapshot struct {
	Type   string                `json:"type"` // always "workflow_run"
	RunID  string                `json:"run_id"`
	Status string                `json:"status"` // "running" | "completed"
	Agents []WorkflowAgentStatus `json:"agents"`
}

// WorkflowsDirForTranscript maps a session transcript JSONL path to the
// sidecar directory where the Claude Code Workflow tool writes per-run
// subagent transcripts and journals.
func WorkflowsDirForTranscript(transcriptPath string) string {
	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	return filepath.Join(base, "subagents", "workflows")
}

// WatchWorkflowRuns polls workflowsDir for Workflow tool run directories and
// emits a full WorkflowRunSnapshot whenever a run's derived state changes.
// Tolerates the directory not existing yet (nothing has run). Blocks until
// ctx is done; callers run it on a goroutine.
func WatchWorkflowRuns(ctx context.Context, workflowsDir string, emit func(WorkflowRunSnapshot)) {
	lastEmitted := map[string]string{} // runID -> serialized last snapshot
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(WorkflowRunPollInterval):
		}

		entries, err := os.ReadDir(workflowsDir)
		if err != nil {
			continue // dir doesn't exist yet, or transient error — keep polling
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			runID := e.Name()
			snap := readWorkflowRun(filepath.Join(workflowsDir, runID), runID)
			key, err := json.Marshal(snap)
			if err != nil {
				continue
			}
			if lastEmitted[runID] == string(key) {
				continue
			}
			lastEmitted[runID] = string(key)
			emit(snap)
		}
	}
}

// readWorkflowRun derives the current snapshot for one run directory.
func readWorkflowRun(runDir, runID string) WorkflowRunSnapshot {
	agents := readWorkflowAgents(runDir)
	status := "running"
	if len(agents) > 0 {
		status = "completed"
		for _, a := range agents {
			if a.Status != "complete" {
				status = "running"
				break
			}
		}
	}
	if agents == nil {
		agents = []WorkflowAgentStatus{}
	}
	return WorkflowRunSnapshot{
		Type:   "workflow_run",
		RunID:  runID,
		Status: status,
		Agents: agents,
	}
}

// workflowJournalEntry is a tolerant parse of one journal.jsonl line.
type workflowJournalEntry struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agentId"`
	Result  json.RawMessage `json:"result"`
}

// readWorkflowAgents derives per-agent status from the run's journal.jsonl.
// The journal is tiny (one started + one result line per stage), so it is
// re-read whole on each change instead of tailed incrementally. A trailing
// line without a newline is a write caught mid-append — skipped now, parsed
// on the next poll. Unknown or unparseable lines are skipped, never fatal.
func readWorkflowAgents(runDir string) []WorkflowAgentStatus {
	data, err := os.ReadFile(filepath.Join(runDir, "journal.jsonl"))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if !strings.HasSuffix(string(data), "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1] // drop trailing partial line
	}

	var agents []WorkflowAgentStatus
	index := map[string]int{} // agentID -> position in agents
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry workflowJournalEntry
		if json.Unmarshal([]byte(line), &entry) != nil || entry.AgentID == "" {
			continue
		}
		switch entry.Type {
		case "started":
			if _, ok := index[entry.AgentID]; !ok {
				index[entry.AgentID] = len(agents)
				agents = append(agents, WorkflowAgentStatus{ID: entry.AgentID, Status: "running"})
			}
		case "result":
			i, ok := index[entry.AgentID]
			if !ok {
				i = len(agents)
				index[entry.AgentID] = i
				agents = append(agents, WorkflowAgentStatus{ID: entry.AgentID})
			}
			agents[i].Status = "complete"
			agents[i].Result = entry.Result
		}
	}

	// Agents with a meta file but no journal entry yet were spawned but
	// haven't reported — surface them as pending, after journal-ordered ones.
	entries, err := os.ReadDir(runDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".meta.json") {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".meta.json")
			if _, ok := index[id]; !ok {
				index[id] = len(agents)
				agents = append(agents, WorkflowAgentStatus{ID: id, Status: "pending"})
			}
		}
	}

	for i := range agents {
		agents[i].AgentType = readAgentType(runDir, agents[i].ID)
		agents[i].Label = readAgentLabel(runDir, agents[i].ID)
	}
	return agents
}

// readAgentType returns agentType from agent-<id>.meta.json, or "".
func readAgentType(runDir, agentID string) string {
	data, err := os.ReadFile(filepath.Join(runDir, "agent-"+agentID+".meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		AgentType string `json:"agentType"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.AgentType
}

const workflowAgentLabelMax = 120

// readAgentLabel derives a human-readable stage label from the first line of
// the agent's transcript — the prompt the workflow script gave it. Only a
// bounded prefix of the file is read; the label is the prompt's first line,
// truncated.
func readAgentLabel(runDir, agentID string) string {
	f, err := os.Open(filepath.Join(runDir, "agent-"+agentID+".jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 16*1024)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}
	line := buf[:n]
	if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	var entry struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &entry) != nil {
		return ""
	}
	text := extractPromptText(entry.Message.Content)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > workflowAgentLabelMax {
		text = string(runes[:workflowAgentLabelMax]) + "…"
	}
	return text
}

// extractPromptText handles both content shapes: a plain string or an array
// of content blocks with text fields.
func extractPromptText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}
