package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type PendingQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type PendingQuestionItem struct {
	Question string                  `json:"question"`
	Header   string                  `json:"header,omitempty"`
	Options  []PendingQuestionOption `json:"options"`
}

type PendingQuestion struct {
	ToolUseID string                `json:"tool_use_id"`
	Questions []PendingQuestionItem `json:"questions"`
	CreatedAt time.Time             `json:"created_at"`
}

type PendingQuestionManager struct {
	mu      sync.Mutex
	pending map[string]*PendingQuestion
	waiters map[string]chan struct{}
}

func NewPendingQuestionManager() *PendingQuestionManager {
	return &PendingQuestionManager{
		pending: make(map[string]*PendingQuestion),
		waiters: make(map[string]chan struct{}),
	}
}

func (pq *PendingQuestionManager) Set(sessionID string, q *PendingQuestion) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.pending[sessionID] = q
}

func (pq *PendingQuestionManager) Get(sessionID string) *PendingQuestion {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.pending[sessionID]
}

// WaitForClear returns a channel that receives a value when the pending
// question for sessionID is deleted (via Delete). Used by the turn loop
// to detect when the user responds to an AskUserQuestion.
func (pq *PendingQuestionManager) WaitForClear(sessionID string) <-chan struct{} {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	ch := make(chan struct{}, 1)
	pq.waiters[sessionID] = ch
	return ch
}

func (pq *PendingQuestionManager) Delete(sessionID string) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	delete(pq.pending, sessionID)
	if ch, ok := pq.waiters[sessionID]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
		delete(pq.waiters, sessionID)
	}
}

// extractAskUserQuestion checks an assistant transcript line for an
// AskUserQuestion tool_use block and returns the parsed question data.
// Returns nil if the line does not contain a valid AskUserQuestion.
func extractAskUserQuestion(line string) *PendingQuestion {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil
	}
	var blocks []struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		return nil
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name != "AskUserQuestion" {
			continue
		}
		var input struct {
			Questions []PendingQuestionItem `json:"questions"`
		}
		if err := json.Unmarshal(b.Input, &input); err != nil || len(input.Questions) == 0 {
			continue
		}
		return &PendingQuestion{
			ToolUseID: b.ID,
			Questions: input.Questions,
			CreatedAt: time.Now(),
		}
	}
	return nil
}

func (s *Server) handlePendingQuestion(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	pending := s.pendingQuestions.Get(sessionID)

	w.Header().Set("Content-Type", "application/json")
	if pending == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"pending": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pending":     true,
		"tool_use_id": pending.ToolUseID,
		"questions":   pending.Questions,
		"created_at":  pending.CreatedAt,
	})
}

// handleDismissQuestion clears a pending question without sending a response
// to the PTY. Used by the UI's "skip" action when a question is stuck.
func (s *Server) handleDismissQuestion(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}

	s.pendingQuestions.Delete(sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
