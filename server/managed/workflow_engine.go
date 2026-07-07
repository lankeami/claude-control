package managed

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

// WorkflowEngine orchestrates multi-step workflow runs against managed sessions.
// Each run executes steps sequentially following the DAG defined by on_success /
// on_failure pointers. Pause/cancel are flag-based and are checked between steps.
type WorkflowEngine struct {
	store            *db.Store
	sendMessage      func(sessionID, prompt string) error
	getActivityState func(sessionID string) (string, error)
	interruptSession func(sessionID string) error

	mu           sync.Mutex
	broadcasters map[string]*Broadcaster
	pauseFlags   map[string]bool
	cancelFlags  map[string]bool
}

// NewWorkflowEngine constructs an engine with the given callbacks.
func NewWorkflowEngine(
	store *db.Store,
	sendMessage func(sessionID, prompt string) error,
	getActivityState func(sessionID string) (string, error),
	interruptSession func(sessionID string) error,
) *WorkflowEngine {
	return &WorkflowEngine{
		store:            store,
		sendMessage:      sendMessage,
		getActivityState: getActivityState,
		interruptSession: interruptSession,
		broadcasters:     make(map[string]*Broadcaster),
		pauseFlags:       make(map[string]bool),
		cancelFlags:      make(map[string]bool),
	}
}

// GetRunBroadcaster returns (or lazily creates) the SSE broadcaster for a run.
func (e *WorkflowEngine) GetRunBroadcaster(runID string) *Broadcaster {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.broadcasters[runID]
	if !ok {
		b = NewBroadcaster()
		e.broadcasters[runID] = b
	}
	return b
}

// broadcast marshals msg and sends it to all SSE subscribers for runID.
func (e *WorkflowEngine) broadcast(runID string, msg map[string]interface{}) {
	b := e.GetRunBroadcaster(runID)
	data, _ := json.Marshal(msg)
	b.Send(string(data))
}

// StartRun validates the run and launches its goroutine.
func (e *WorkflowEngine) StartRun(runID string) error {
	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		return fmt.Errorf("run not found: %s", runID)
	}

	active, _ := e.store.GetActiveRunForSession(run.SessionID)
	if active != nil && active.ID != runID {
		return fmt.Errorf("session already has active run: %s", active.ID)
	}

	go e.executeRun(runID)
	return nil
}

func (e *WorkflowEngine) executeRun(runID string) {
	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		log.Printf("workflow engine: run %s not found", runID)
		return
	}

	e.store.UpdateWorkflowRunStatus(runID, "running", nil)
	e.broadcast(runID, map[string]interface{}{"type": "run_started"})

	steps, err := e.store.GetWorkflowSteps(run.WorkflowID)
	if err != nil || len(steps) == 0 {
		errMsg := "no steps found"
		e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
		e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
		return
	}

	runSteps, _ := e.store.GetWorkflowRunSteps(runID)
	// Map workflow step ID -> run step ID
	stepToRunStep := make(map[string]string)
	for _, rs := range runSteps {
		stepToRunStep[rs.StepID] = rs.ID
	}

	// Build a map from step ID -> WorkflowStep for DAG traversal
	stepMap := make(map[string]db.WorkflowStep)
	for _, s := range steps {
		stepMap[s.ID] = s
	}

	// Determine starting step: resume from current_step_id if set, else first step
	currentStep := steps[0]
	if run.CurrentStepID != nil {
		if s, ok := stepMap[*run.CurrentStepID]; ok {
			currentStep = s
		}
	}

	for {
		e.mu.Lock()
		cancelled := e.cancelFlags[runID]
		paused := e.pauseFlags[runID]
		e.mu.Unlock()

		if cancelled {
			e.store.UpdateWorkflowRunStatus(runID, "cancelled", nil)
			e.broadcast(runID, map[string]interface{}{"type": "run_cancelled"})
			e.cleanup(runID)
			return
		}

		if paused {
			e.store.UpdateWorkflowRunStatus(runID, "paused", nil)
			e.broadcast(runID, map[string]interface{}{"type": "run_paused"})
			return
		}

		rsID := stepToRunStep[currentStep.ID]
		e.store.UpdateWorkflowRunCurrentStep(runID, currentStep.ID)
		e.store.UpdateWorkflowRunStepStatus(rsID, "running", 1, nil)
		e.broadcast(runID, map[string]interface{}{
			"type":      "step_started",
			"step_id":   currentStep.ID,
			"step_name": currentStep.Name,
			"attempt":   1,
		})

		success := e.executeStep(runID, run.SessionID, currentStep, rsID)

		// Check cancel after step returns (cancellation may have triggered inside executeStep)
		e.mu.Lock()
		cancelledAfterStep := e.cancelFlags[runID]
		e.mu.Unlock()
		if cancelledAfterStep {
			e.store.UpdateWorkflowRunStatus(runID, "cancelled", nil)
			e.broadcast(runID, map[string]interface{}{"type": "run_cancelled"})
			e.cleanup(runID)
			return
		}

		if !success {
			attempt := 1
			for attempt < currentStep.MaxRetries {
				attempt++
				e.store.UpdateWorkflowRunStepStatus(rsID, "running", attempt, nil)
				e.broadcast(runID, map[string]interface{}{
					"type":      "step_started",
					"step_id":   currentStep.ID,
					"step_name": currentStep.Name,
					"attempt":   attempt,
				})
				success = e.executeStep(runID, run.SessionID, currentStep, rsID)
				if success {
					break
				}
				// Check cancel after each retry too
				e.mu.Lock()
				retryCancel := e.cancelFlags[runID]
				e.mu.Unlock()
				if retryCancel {
					e.store.UpdateWorkflowRunStatus(runID, "cancelled", nil)
					e.broadcast(runID, map[string]interface{}{"type": "run_cancelled"})
					e.cleanup(runID)
					return
				}
			}
		}

		if success {
			e.store.UpdateWorkflowRunStepStatus(rsID, "completed", 0, nil)
			e.broadcast(runID, map[string]interface{}{
				"type":    "step_completed",
				"step_id": currentStep.ID,
				"status":  "completed",
			})

			if currentStep.OnSuccess == nil {
				// No next step — workflow done
				e.store.UpdateWorkflowRunStatus(runID, "completed", nil)
				e.broadcast(runID, map[string]interface{}{"type": "run_completed", "status": "completed"})
				e.cleanup(runID)
				return
			}
			next, ok := stepMap[*currentStep.OnSuccess]
			if !ok {
				e.store.UpdateWorkflowRunStatus(runID, "completed", nil)
				e.broadcast(runID, map[string]interface{}{"type": "run_completed", "status": "completed"})
				e.cleanup(runID)
				return
			}
			currentStep = next
		} else {
			errMsg := "step failed after retries"
			e.store.UpdateWorkflowRunStepStatus(rsID, "failed", 0, &errMsg)
			e.broadcast(runID, map[string]interface{}{
				"type":    "step_failed",
				"step_id": currentStep.ID,
				"error":   errMsg,
			})

			if currentStep.OnFailure == nil {
				e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
				e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
				e.cleanup(runID)
				return
			}
			next, ok := stepMap[*currentStep.OnFailure]
			if !ok {
				e.store.UpdateWorkflowRunStatus(runID, "failed", &errMsg)
				e.broadcast(runID, map[string]interface{}{"type": "run_failed", "error": errMsg})
				e.cleanup(runID)
				return
			}
			currentStep = next
		}
	}
}

// executeStep sends the step's prompt and polls until the session is no longer
// "working". Returns true on success, false on timeout or cancellation.
func (e *WorkflowEngine) executeStep(runID, sessionID string, step db.WorkflowStep, runStepID string) bool {
	if err := e.sendMessage(sessionID, step.Prompt); err != nil {
		log.Printf("workflow engine: sendMessage failed for step %s: %v", step.ID, err)
		return false
	}

	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.After(timeout)

	for {
		e.mu.Lock()
		cancelled := e.cancelFlags[runID]
		e.mu.Unlock()
		if cancelled {
			e.interruptSession(sessionID) //nolint:errcheck
			return false
		}

		state, err := e.getActivityState(sessionID)
		if err != nil {
			log.Printf("workflow engine: getActivityState error for session %s: %v", sessionID, err)
			return false
		}
		if state == "idle" || state == "waiting" {
			return true
		}

		select {
		case <-deadline:
			log.Printf("workflow engine: step %s timed out", step.ID)
			e.interruptSession(sessionID) //nolint:errcheck
			return false
		case <-time.After(500 * time.Millisecond):
			// poll again
		}
	}
}

// PauseRun flags the run to pause after the current step finishes.
func (e *WorkflowEngine) PauseRun(runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pauseFlags[runID] = true
	return nil
}

// ResumeRun clears the pause flag and re-spawns the execution goroutine.
func (e *WorkflowEngine) ResumeRun(runID string) error {
	e.mu.Lock()
	e.pauseFlags[runID] = false
	e.mu.Unlock()

	run, err := e.store.GetWorkflowRun(runID)
	if err != nil || run == nil {
		return fmt.Errorf("run not found")
	}
	if run.Status != "paused" {
		return fmt.Errorf("run is not paused (status: %s)", run.Status)
	}

	e.store.UpdateWorkflowRunStatus(runID, "running", nil)
	go e.executeRun(runID)
	return nil
}

// CancelRun flags the run for cancellation; the goroutine will pick it up at
// the next checkpoint (between steps or during the poll loop).
func (e *WorkflowEngine) CancelRun(runID string) error {
	e.mu.Lock()
	e.cancelFlags[runID] = true
	e.mu.Unlock()
	return nil
}

// cleanup removes control flags for a completed/failed/cancelled run.
func (e *WorkflowEngine) cleanup(runID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pauseFlags, runID)
	delete(e.cancelFlags, runID)
}

// RecoverStaleRuns marks any in-flight "running" runs as failed (called on server startup).
func (e *WorkflowEngine) RecoverStaleRuns() {
	if err := e.store.ResetStaleWorkflowRuns(); err != nil {
		log.Printf("workflow engine: RecoverStaleRuns: %v", err)
	}
}
