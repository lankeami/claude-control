package api

import (
	"encoding/json"
	"net/http"
)

type instructRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleInstruct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req instructRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	instr, err := s.store.QueueInstruction(id, req.Message)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instr)
}

func (s *Server) handleFetchInstructions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	instr, err := s.store.FetchNextInstruction(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if instr == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instr)
}
