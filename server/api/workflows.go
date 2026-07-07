package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

type createWorkflowRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       []db.WorkflowStepInput `json:"steps"`
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	wf, err := s.store.CreateWorkflow(req.Name, req.Description, req.Steps)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	steps, err := s.store.GetWorkflowSteps(wf.ID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": wf, "steps": steps})
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	wfs, err := s.store.ListWorkflows()
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if wfs == nil {
		wfs = []db.Workflow{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wfs)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wf, err := s.store.GetWorkflow(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if wf == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	steps, err := s.store.GetWorkflowSteps(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": wf, "steps": steps})
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wf, err := s.store.GetWorkflow(id)
	if err != nil || wf == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateWorkflow(id, req.Name, req.Description, req.Steps); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	updated, _ := s.store.GetWorkflow(id)
	steps, _ := s.store.GetWorkflowSteps(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": updated, "steps": steps})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteWorkflow(id); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Workflow execution and run tracking handlers (Task 5)
// ---------------------------------------------------------------------------

func (s *Server) handleStartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
		return
	}

	wf, err := s.store.GetWorkflow(workflowID)
	if err != nil || wf == nil {
		http.Error(w, `{"error":"workflow not found"}`, http.StatusNotFound)
		return
	}

	active, _ := s.store.GetActiveRunForSession(req.SessionID)
	if active != nil {
		http.Error(w, `{"error":"session already has an active workflow run"}`, http.StatusConflict)
		return
	}

	steps, _ := s.store.GetWorkflowSteps(workflowID)
	run, err := s.store.CreateWorkflowRun(workflowID, req.SessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	stepIDs := make([]string, len(steps))
	for i, st := range steps {
		stepIDs[i] = st.ID
	}
	s.store.CreateWorkflowRunSteps(run.ID, stepIDs)

	if err := s.workflowEngine.StartRun(run.ID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

func (s *Server) handlePauseRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.PauseRun(runID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.ResumeRun(runID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if err := s.workflowEngine.CancelRun(runID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	workflowID := r.URL.Query().Get("workflow_id")
	sessionID := r.URL.Query().Get("session_id")
	runs, err := s.store.ListWorkflowRuns(workflowID, sessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.WorkflowRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := s.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	runSteps, _ := s.store.GetWorkflowRunSteps(runID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"run": run, "steps": runSteps})
}

func (s *Server) handleWorkflowRunStream(w http.ResponseWriter, r *http.Request, apiKey string) {
	token := r.URL.Query().Get("token")
	if token == "" || token != apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runID := r.PathValue("id")
	b := s.workflowEngine.GetRunBroadcaster(runID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(managed.HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, "data: {\"type\":\"heartbeat\"}\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helpers used by the workflow engine callbacks
// ---------------------------------------------------------------------------

// sendWorkflowMessage persists a user message and sets activity_state to
// "working". It does NOT actually send the prompt to a running Claude process
// — that path is through the interactive/print backend managed by manager.
// The workflow engine uses this to record the message in the DB and signal that
// work is in progress; the session's SSE subscriber will see the user message.
func (s *Server) sendWorkflowMessage(sessionID, prompt string) error {
	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	msg, err := s.store.CreateMessage(sessionID, "user", prompt, 0)
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}

	s.store.UpdateActivityState(sessionID, "working")

	// Broadcast the user message on the session stream (non-fatal if manager is nil)
	if s.manager != nil {
		b := s.manager.GetBroadcaster(sessionID)
		userMsg := fmt.Sprintf(
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":%s}]},"seq":%d}`,
			jsonQuote(prompt), msg.Seq,
		)
		b.Send(userMsg)
	}

	return nil
}

// jsonQuote returns a JSON-encoded string for safe embedding in a JSON literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// interruptManagedSession interrupts the running Claude process for a session.
func (s *Server) interruptManagedSession(sessionID string) error {
	if s.manager == nil {
		return nil
	}
	if s.manager.IsInteractiveRunning(sessionID) {
		return s.manager.InterruptInteractive(sessionID)
	}
	return s.manager.Interrupt(sessionID)
}
