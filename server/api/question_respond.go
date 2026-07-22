package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// handleQuestionRespond sends the user's AskUserQuestion selection to the
// Claude Code TUI via PTY keystrokes. The TUI presents options as an
// interactive list; we navigate with arrow-down and confirm with Enter.
func (s *Server) handleQuestionRespond(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		OptionIndex int    `json:"option_index"`
		OptionCount int    `json:"option_count"`
		Text        string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

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

	isOther := req.OptionIndex < 0 || req.OptionIndex >= req.OptionCount

	s.pendingQuestions.Delete(sessionID)
	s.manager.GetBroadcaster(sessionID).Send(`{"type":"question_cleared"}`)

	if isOther {
		// Navigate past all options to "Other", select it, type text, confirm
		if err := s.sendQuestionKeys(sessionID, req.OptionCount, true, req.Text); err != nil {
			http.Error(w, fmt.Sprintf("failed to send keystrokes: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := s.sendQuestionKeys(sessionID, req.OptionIndex, false, ""); err != nil {
			http.Error(w, fmt.Sprintf("failed to send keystrokes: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// sendQuestionKeys navigates the Claude Code TUI's option selector via PTY
// keystrokes: arrowCount down-arrows followed by Enter. For "Other" mode,
// it additionally types the free-text answer and submits.
func (s *Server) sendQuestionKeys(sessionID string, arrowCount int, isOther bool, text string) error {
	const keyDelay = 30 * time.Millisecond

	// Navigate down to the target option
	for i := 0; i < arrowCount; i++ {
		if err := s.manager.SendKeys(sessionID, "\x1b[B"); err != nil {
			return err
		}
		time.Sleep(keyDelay)
	}

	// Confirm selection
	if err := s.manager.SendKeys(sessionID, "\r"); err != nil {
		return err
	}

	if isOther && text != "" {
		// Wait for the text input to appear, then type the answer
		time.Sleep(100 * time.Millisecond)
		if err := s.manager.SendKeys(sessionID, text); err != nil {
			return err
		}
		time.Sleep(keyDelay)
		if err := s.manager.SendKeys(sessionID, "\r"); err != nil {
			return err
		}
	}

	return nil
}
