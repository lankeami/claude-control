package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handlePairing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"paired":true}`))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"server_id": s.serverID,
	})
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"name":  s.instanceName,
		"theme": getThemeForInstance(s.instanceName),
	})
}

func getThemeForInstance(instanceName string) string {
	switch instanceName {
	case "work":
		return "forest"
	default:
		return "ocean"
	}
}
