package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleModState receives mod panel state updates and broadcasts them to
// the session's SSE stream as mod_state events. This allows the web UI to
// display mod panels (bar items, side panes) for managed sessions.
//
// POST /api/sessions/{id}/mod-state
// Body: {"panel_id": "...", "panel_title": "...", "slot": "bar"|"pane", "data": {...}}
func (s *Server) handleModState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		PanelID    string      `json:"panel_id"`
		PanelTitle string      `json:"panel_title"`
		Slot       string      `json:"slot"`
		Data       interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.PanelID == "" {
		http.Error(w, "panel_id required", http.StatusBadRequest)
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

	if req.Slot == "" {
		req.Slot = "bar"
	}

	evt, _ := json.Marshal(map[string]interface{}{
		"type":        "mod_state",
		"panel_id":    req.PanelID,
		"panel_title": req.PanelTitle,
		"slot":        req.Slot,
		"data":        req.Data,
	})
	s.manager.GetBroadcaster(sessionID).Send(string(evt))

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}
