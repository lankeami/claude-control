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
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting", "server_id": s.serverID})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		s.shutdownFunc()
		// Safety net: if shutdown didn't kill the process within 10s, reset flag
		time.Sleep(10 * time.Second)
		s.restartInProgress.Store(false)
	}()
}
