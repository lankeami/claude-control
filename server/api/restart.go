package api

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !s.restartInProgress.CompareAndSwap(false, true) {
		http.Error(w, "restart already in progress", http.StatusConflict)
		return
	}

	// Set working sessions to waiting before shutdown
	if s.store != nil {
		s.store.SetWorkingToWaiting()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		s.shutdownFunc()
	}()
}
