package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type registerRequest struct {
	ComputerName   string `json:"computer_name"`
	ProjectPath    string `json:"project_path"`
	TranscriptPath string `json:"transcript_path"`
}

func (s *Server) handleRegisterSession(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	session, err := s.store.UpsertSession(req.ComputerName, req.ProjectPath, req.TranscriptPath)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Heartbeat(id); err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	sessions, err := s.store.ListSessions(includeArchived)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []db.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

type archiveRequest struct {
	Archived bool `json:"archived"`
}

type renameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleUpdateSessionName(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateSessionName(id, req.Name); err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	session, err := s.store.GetSessionByID(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Clean up uploaded images before deleting session
	sess, err := s.store.GetSessionByID(id)
	if err == nil && sess != nil && sess.Mode == "managed" && sess.CWD != "" {
		cleanupSessionUploads(sess.CWD, id)
	}

	// Teardown managed process if running
	if s.manager != nil {
		s.manager.Teardown(id, 5*time.Second)
	}
	if err := s.store.DeleteSession(id); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleSetArchived(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req archiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.SetArchived(id, req.Archived); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
