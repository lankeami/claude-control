package api

import (
	"encoding/json"
	"sync"
)

// sessionWorkflowRuns holds the latest snapshot per Workflow tool run for one
// session, in first-seen order, so late-connecting SSE clients can catch up.
type sessionWorkflowRuns struct {
	mu     sync.Mutex
	order  []string
	latest map[string]string // runID -> latest snapshot JSON
}

// emitWorkflowRun records the latest snapshot for the run and broadcasts it
// on the session's SSE stream. Called by the managed layer's workflow-run
// watcher for every observed state change.
func (s *Server) emitWorkflowRun(sessionID, snapshotJSON string) {
	var snap struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal([]byte(snapshotJSON), &snap) == nil && snap.RunID != "" {
		v, _ := s.workflowRuns.LoadOrStore(sessionID, &sessionWorkflowRuns{latest: map[string]string{}})
		runs := v.(*sessionWorkflowRuns)
		runs.mu.Lock()
		if _, ok := runs.latest[snap.RunID]; !ok {
			runs.order = append(runs.order, snap.RunID)
		}
		runs.latest[snap.RunID] = snapshotJSON
		runs.mu.Unlock()
	}
	s.manager.GetBroadcaster(sessionID).Send(snapshotJSON)
}

// workflowRunSnapshots returns the latest snapshot of every run recorded for
// the session, in first-seen order.
func (s *Server) workflowRunSnapshots(sessionID string) []string {
	v, ok := s.workflowRuns.Load(sessionID)
	if !ok {
		return nil
	}
	runs := v.(*sessionWorkflowRuns)
	runs.mu.Lock()
	defer runs.mu.Unlock()
	out := make([]string, 0, len(runs.order))
	for _, id := range runs.order {
		out = append(out, runs.latest[id])
	}
	return out
}
