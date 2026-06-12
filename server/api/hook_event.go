package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleHookEvent receives turn-lifecycle signals from the hook-signal
// subcommand running inside managed interactive Claude Code sessions.
func (s *Server) handleHookEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Event           string `json:"event"`
		ClaudeSessionID string `json:"claude_session_id"`
		TranscriptPath  string `json:"transcript_path"`
		Message         string `json:"message"`
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

	switch req.Event {
	case "session_start":
		// Interactive --resume forks to a new CLI session ID; track it so the
		// next spawn resumes the right session and /resume filtering works.
		if req.ClaudeSessionID != "" && req.ClaudeSessionID != sess.ClaudeSessionID {
			_ = s.store.UpdateClaudeSessionID(sessionID, req.ClaudeSessionID)
		}
		if req.TranscriptPath != "" {
			s.manager.SetTranscript(sessionID, req.TranscriptPath)
		}
	case "stop":
		s.manager.SignalStop(sessionID)
	case "notification":
		evt, _ := json.Marshal(map[string]string{"type": "notification", "message": req.Message})
		s.manager.GetBroadcaster(sessionID).Send(string(evt))
		if req.Message != "" {
			_, _ = s.store.CreateMessage(sessionID, "system", req.Message, 0)
		}
	default:
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}
