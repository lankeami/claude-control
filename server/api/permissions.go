package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type PendingPermission struct {
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	ResponseCh  chan string     `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
}

type PermissionManager struct {
	mu      sync.Mutex
	pending map[string]*PendingPermission
}

func NewPermissionManager() *PermissionManager {
	return &PermissionManager{
		pending: make(map[string]*PendingPermission),
	}
}

func (pm *PermissionManager) Set(sessionID string, p *PendingPermission) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pending[sessionID] = p
}

func (pm *PermissionManager) Get(sessionID string) *PendingPermission {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.pending[sessionID]
}

func (pm *PermissionManager) Delete(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.pending, sessionID)
}

const permissionTimeout = 5 * time.Minute

func (s *Server) handlePermissionRequest(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		ToolName    string          `json:"tool_name"`
		Description string          `json:"description"`
		Input       json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	pending := &PendingPermission{
		ToolName:    req.ToolName,
		Description: req.Description,
		Input:       req.Input,
		ResponseCh:  make(chan string, 1),
		CreatedAt:   time.Now(),
	}

	s.permissions.Set(sessionID, pending)
	_ = s.store.UpdateActivityState(sessionID, "input_needed")

	broadcaster := s.manager.GetBroadcaster(sessionID)
	eventJSON, _ := json.Marshal(map[string]interface{}{
		"type":        "input_request",
		"tool_name":   req.ToolName,
		"description": req.Description,
		"input":       req.Input,
	})
	broadcaster.Send(string(eventJSON))

	select {
	case decision := <-pending.ResponseCh:
		s.permissions.Delete(sessionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"decision": decision})
	case <-time.After(permissionTimeout):
		s.permissions.Delete(sessionID)
		_ = s.store.UpdateActivityState(sessionID, "working")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"decision": "deny", "reason": "timeout"})
	case <-r.Context().Done():
		s.permissions.Delete(sessionID)
		return
	}
}

func (s *Server) handlePermissionRespond(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Decision == "" {
		http.Error(w, "decision is required", http.StatusBadRequest)
		return
	}

	pending := s.permissions.Get(sessionID)
	if pending == nil {
		http.Error(w, "no pending permission request", http.StatusNotFound)
		return
	}

	pending.ResponseCh <- req.Decision
	_ = s.store.UpdateActivityState(sessionID, "working")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handlePendingPermission(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	pending := s.permissions.Get(sessionID)

	w.Header().Set("Content-Type", "application/json")
	if pending == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"pending": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pending":     true,
		"tool_name":   pending.ToolName,
		"description": pending.Description,
		"input":       pending.Input,
		"created_at":  pending.CreatedAt,
	})
}
