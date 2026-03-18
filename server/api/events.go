package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request, apiKey string) {
	// Auth via query param (EventSource can't send headers)
	token := r.URL.Query().Get("token")
	if token == "" || token != apiKey {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Send immediately, then every tick
	s.sendSSEState(w, flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			s.sendSSEState(w, flusher)
		}
	}
}

func (s *Server) sendSSEState(w http.ResponseWriter, flusher http.Flusher) {
	sessions, _ := s.store.ListSessions(false)
	prompts, _ := s.store.ListPrompts("", "")

	if sessions == nil {
		sessions = []db.Session{}
	}
	if prompts == nil {
		prompts = []db.Prompt{}
	}

	payload := map[string]interface{}{
		"sessions": sessions,
		"prompts":  prompts,
	}
	data, _ := json.Marshal(payload)

	fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
	flusher.Flush()
}
