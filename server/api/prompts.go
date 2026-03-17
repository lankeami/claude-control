package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type createPromptRequest struct {
	SessionID     string `json:"session_id"`
	ClaudeMessage string `json:"claude_message"`
	Type          string `json:"type"`
}

func (s *Server) handleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	var req createPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Update session status to waiting
	s.store.SetSessionStatus(req.SessionID, "waiting")

	prompt, err := s.store.CreatePrompt(req.SessionID, req.ClaudeMessage, req.Type)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prompt)
}

func (s *Server) handleGetPromptResponse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Parse timeout (default 30s, for testing allow override via query param)
	timeoutSec := 30
	if t := r.URL.Query().Get("timeout"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 && v <= 30 {
			timeoutSec = v
		}
	}

	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	for time.Now().Before(deadline) {
		response, err := s.store.GetPromptResponse(id)
		if err != nil {
			http.Error(w, `{"error":"prompt not found"}`, http.StatusNotFound)
			return
		}
		if response != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "answered",
				"response": *response,
			})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timeout — return pending
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}

type respondRequest struct {
	Response string `json:"response"`
}

func (s *Server) handleRespondToPrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req respondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.RespondToPrompt(id, req.Response); err != nil {
		http.Error(w, `{"error":"prompt not found or already answered"}`, http.StatusNotFound)
		return
	}

	// Get prompt to find session ID and reset session status to active
	prompt, err := s.store.GetPromptByID(id)
	if err == nil && prompt != nil {
		s.store.SetSessionStatus(prompt.SessionID, "active")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	status := r.URL.Query().Get("status")

	prompts, err := s.store.ListPrompts(sessionID, status)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if prompts == nil {
		prompts = []db.Prompt{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prompts)
}
