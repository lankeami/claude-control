package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func (s *Server) handleCreateManagedSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD          string  `json:"cwd"`
		AllowedTools string  `json:"allowed_tools"`
		MaxTurns     int     `json:"max_turns"`
		MaxBudgetUSD float64 `json:"max_budget_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.CWD == "" {
		http.Error(w, "cwd is required", http.StatusBadRequest)
		return
	}
	if req.AllowedTools == "" {
		req.AllowedTools = `["Bash","Read","Edit","Write","Glob","Grep"]`
	}
	if req.MaxTurns == 0 {
		req.MaxTurns = 50
	}
	if req.MaxBudgetUSD == 0 {
		req.MaxBudgetUSD = 5.0
	}

	sess, err := s.store.CreateManagedSession(req.CWD, req.AllowedTools, req.MaxTurns, req.MaxBudgetUSD)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "session already exists for this directory", http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
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

	// Build claude args
	var args []string
	args = append(args, "-p", req.Message)

	if sess.Initialized {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}

	args = append(args, "--output-format", "stream-json", "--verbose")

	if sess.AllowedTools != "" {
		var tools []string
		json.Unmarshal([]byte(sess.AllowedTools), &tools)
		if len(tools) > 0 {
			args = append(args, "--allowedTools", strings.Join(tools, ","))
		}
	}

	if sess.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", sess.MaxBudgetUSD))
	}

	proc, err := s.manager.Spawn(sessionID, managed.SpawnOpts{
		Args: args,
		CWD:  sess.CWD,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already has a running process") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if !sess.Initialized {
		_ = s.store.SetInitialized(sessionID)
	}

	_, _ = s.store.CreateMessage(sessionID, "user", req.Message)
	_ = s.store.SetSessionStatus(sessionID, "running")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	// Background: stream stdout, persist inline, cleanup
	go func() {
		turnCount := 0

		onLine := func(line string) {
			role := parseRole(line)
			_, _ = s.store.CreateMessage(sessionID, role, line)

			if role == "assistant" {
				turnCount++
				if turnCount >= sess.MaxTurns {
					log.Printf("session %s hit turn limit (%d), interrupting", sessionID, sess.MaxTurns)
					_ = s.manager.Interrupt(sessionID)
				}
			}
		}

		managed.StreamNDJSON(proc.Stdout, broadcaster, onLine)

		stderrBytes, _ := io.ReadAll(proc.Stderr)

		<-proc.Done

		if proc.ExitCode != 0 && len(stderrBytes) > 0 {
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
			_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
			broadcaster.Send(errMsg)
		}

		doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
		broadcaster.Send(doneMsg)

		_ = s.store.SetSessionStatus(sessionID, "idle")
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if err := s.manager.Interrupt(sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "interrupted"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	msgs, err := s.store.ListMessages(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []db.Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request, apiKey string) {
	token := r.URL.Query().Get("token")
	if token != apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	broadcaster := s.manager.GetBroadcaster(sessionID)
	ch := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(ch)

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func parseRole(line string) string {
	var obj struct {
		Type string `json:"type"`
	}
	json.Unmarshal([]byte(line), &obj)
	if obj.Type == "" {
		return "system"
	}
	return obj.Type
}
