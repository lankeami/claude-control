package api

import (
	"encoding/json"
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type createPipelineRunRequest struct {
	Name       string                     `json:"name"`
	Mode       string                     `json:"mode"`
	WorkingDir string                     `json:"working_dir"`
	Items      []createPipelineRunItemReq `json:"items"`
}

type createPipelineRunItemReq struct {
	FeatureLabel string `json:"feature_label"`
	SessionID    string `json:"session_id"`
}

func (s *Server) handleCreatePipelineRun(w http.ResponseWriter, r *http.Request) {
	var req createPipelineRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = "parallel"
	}

	run, err := s.store.CreatePipelineRun(req.Name, req.Mode, req.WorkingDir)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	var items []interface{}
	for _, it := range req.Items {
		item, err := s.store.CreatePipelineRunItem(run.ID, it.FeatureLabel, it.SessionID)
		if err != nil {
			continue
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"run": run, "items": items})
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListPipelineRuns()
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.PipelineRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleGetPipelineRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.store.GetPipelineRun(id)
	if err != nil || run == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	items, _ := s.store.GetPipelineRunItems(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"run": run, "items": items})
}

func (s *Server) handleUpdatePipelineRunItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdatePipelineRunItemStatus(id, req.Status, req.Error); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleUpdatePipelineRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdatePipelineRunStatus(id, req.Status, req.Error); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}
