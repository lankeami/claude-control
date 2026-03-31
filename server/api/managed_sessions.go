package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func (s *Server) handleCreateManagedSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD                    string  `json:"cwd"`
		AllowedTools           string  `json:"allowed_tools"`
		MaxTurns               int     `json:"max_turns"`
		MaxBudgetUSD           float64 `json:"max_budget_usd"`
		CompactEveryNContinues int     `json:"compact_every_n_continues"`
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
	if req.CompactEveryNContinues == 0 {
		if v, err := strconv.Atoi(readEnvFile(s.envPath)["COMPACT_EVERY_N_CONTINUES"]); err == nil && v > 0 {
			req.CompactEveryNContinues = v
		}
	}

	sess, err := s.store.CreateManagedSession(req.CWD, req.AllowedTools, req.MaxTurns, req.MaxBudgetUSD, req.CompactEveryNContinues)
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

func buildClaudeArgs(sess *db.Session, message string, cfg managed.Config) []string {
	var args []string
	args = append(args, "-p", message)

	resumeID := sess.ID
	if sess.ClaudeSessionID != "" {
		resumeID = sess.ClaudeSessionID
	}
	if sess.Initialized {
		args = append(args, "--resume", resumeID)
	} else {
		args = append(args, "--session-id", resumeID)
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

	// Add MCP permission prompt config if binary path is available
	if cfg.BinaryPath != "" && cfg.ServerPort > 0 {
		mcpConfig := map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"controller": map[string]interface{}{
					"command": cfg.BinaryPath,
					"args":   []string{"mcp-bridge", "--session-id", sess.ID, "--port", fmt.Sprintf("%d", cfg.ServerPort)},
				},
			},
		}
		mcpJSON, err := json.Marshal(mcpConfig)
		if err == nil {
			tmpFile := fmt.Sprintf("/tmp/claude-mcp-%s.json", sess.ID)
			if os.WriteFile(tmpFile, mcpJSON, 0644) == nil {
				args = append(args, "--permission-prompt-tool", "mcp__controller__permission_prompt")
				args = append(args, "--mcp-config", tmpFile)
			}
		}
	}

	return args
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
	if s.manager.IsRunning(sessionID) {
		http.Error(w, "session already has a running process", http.StatusConflict)
		return
	}

	_, _ = s.store.CreateMessage(sessionID, "user", req.Message)
	_ = s.store.UpdateActivityState(sessionID, "working")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	go func() {

		// Use a detached context — this goroutine outlives the HTTP request,
		// so r.Context() would be cancelled as soon as the handler returns.
		ctx := context.Background()
		continuationCount := 0
		// int() truncates toward zero, same as floor() for positive numbers
		threshold := int(sess.AutoContinueThreshold * float64(sess.MaxTurns))
		currentMessage := req.Message

		for {
			// Check context cancellation before each spawn (SSE client disconnect)
			select {
			case <-ctx.Done():
				log.Printf("session %s: client disconnected, stopping auto-continue", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			default:
			}

			_ = s.store.ResetTurnCount(sessionID)
			turnsSinceLastContinue := 0
			autoInterrupting := false

			args := buildClaudeArgs(sess, currentMessage, s.manager.Config())
			proc, err := s.manager.Spawn(sessionID, managed.SpawnOpts{
				Args: args,
				CWD:  sess.CWD,
			})
			if err != nil {
				log.Printf("auto-continue spawn error for session %s: %v", sessionID, err)
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to spawn process: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				break
			}

			// onLine runs synchronously inside StreamNDJSON's read loop.
			// autoInterrupting and executionErrored are safe to read after
			// StreamNDJSON returns because StreamNDJSON blocks until stdout
			// EOF, providing a happens-before guarantee with <-proc.Done.
			executionErrored := false
			onLine := func(line string) {
				role := parseRole(line)
				if role == "heartbeat" {
					return
				}
				if role == "result" {
					var res struct {
						Subtype string   `json:"subtype"`
						IsError bool     `json:"is_error"`
						Errors  []string `json:"errors"`
					}
					if json.Unmarshal([]byte(line), &res) == nil && res.Subtype == "error_during_execution" {
						executionErrored = true
						if len(res.Errors) > 0 {
							errText := strings.Join(res.Errors, "; ")
							_, _ = s.store.CreateMessage(sessionID, "assistant", errText)
						}
					}
				}
				if role == "assistant" {
					text := extractAssistantText(line)
					if text != "" {
						_, _ = s.store.CreateMessage(sessionID, role, text)
					}
					for _, toolName := range extractToolNames(line) {
						_, _ = s.store.CreateMessage(sessionID, "activity", toolName)
					}
					turnsSinceLastContinue++
					count, _ := s.store.IncrementTurnCount(sessionID)
					if count >= threshold && !autoInterrupting {
						autoInterrupting = true
						log.Printf("session %s hit auto-continue threshold (%d/%d), interrupting", sessionID, count, sess.MaxTurns)
						_ = s.manager.Interrupt(sessionID)
					}
				}
				extractSessionFiles(line, sessionID, s.store)
			}

			managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, nil)

			stderrBytes, _ := io.ReadAll(proc.Stderr)
			<-proc.Done

			// Mark session as initialized only after a successful execution.
			// If claude -p exits with error_during_execution (e.g. auth failure),
			// no conversation was created, so --resume would fail on next message.
			if !sess.Initialized && !executionErrored {
				_ = s.store.SetInitialized(sessionID)
				sess.Initialized = true
			}

			// Clean up temp MCP config file
			os.Remove(fmt.Sprintf("/tmp/claude-mcp-%s.json", sessionID))

			// Clean up any pending permission request
			s.permissions.Delete(sessionID)

			if proc.ExitCode != 0 && len(stderrBytes) > 0 {
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
				_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
				broadcaster.Send(errMsg)
			}

			// Natural exit (code 0) — Claude finished on its own, no auto-continue needed.
			if proc.ExitCode == 0 && !autoInterrupting {
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
				broadcaster.Send(doneMsg)
				break
			}

			// Process exited without our threshold SIGINT — manual interrupt or error
			if !autoInterrupting {
				_ = s.store.UpdateActivityState(sessionID, "idle")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
				broadcaster.Send(doneMsg)
				break
			}

			// Minimum progress guard: need at least 2 turns of work
			if turnsSinceLastContinue < 2 {
				log.Printf("session %s not making progress (%d turns), stopping auto-continue", sessionID, turnsSinceLastContinue)
				_, _ = s.store.CreateMessage(sessionID, "system", "Auto-continue stopped: not making progress")
				noProgressMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d,"reason":"no_progress"}`, continuationCount)
				broadcaster.Send(noProgressMsg)
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
				broadcaster.Send(doneMsg)
				break
			}

			continuationCount++

			if continuationCount > sess.MaxContinuations {
				log.Printf("session %s exhausted auto-continues (%d/%d)", sessionID, continuationCount, sess.MaxContinuations)
				exhaustedMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d}`, continuationCount)
				broadcaster.Send(exhaustedMsg)
				_, _ = s.store.CreateMessage(sessionID, "system",
					fmt.Sprintf("Auto-continue limit reached (%d/%d)", continuationCount, sess.MaxContinuations))
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
				broadcaster.Send(doneMsg)
				break
			}

			// Auto-continue
			continuingMsg := fmt.Sprintf(`{"type":"auto_continuing","continuation_count":%d,"max_continuations":%d}`,
				continuationCount, sess.MaxContinuations)
			broadcaster.Send(continuingMsg)
			_, _ = s.store.CreateMessage(sessionID, "system",
				fmt.Sprintf("Auto-continuing (%d/%d)...", continuationCount, sess.MaxContinuations))

			// Compact step: run /compact before resuming if configured
			if sess.CompactEveryNContinues > 0 && continuationCount%sess.CompactEveryNContinues == 0 {
				compactingMsg := fmt.Sprintf(`{"type":"compacting","continuation_count":%d}`, continuationCount)
				broadcaster.Send(compactingMsg)
				_, _ = s.store.CreateMessage(sessionID, "system", "Running /compact to reduce context size...")

				compactArgs := buildClaudeArgs(sess, "/compact", s.manager.Config())
				compactProc, compactErr := s.manager.Spawn(sessionID, managed.SpawnOpts{
					Args: compactArgs,
					CWD:  sess.CWD,
				})
				if compactErr != nil {
					log.Printf("session %s: compact spawn failed: %v", sessionID, compactErr)
					_, _ = s.store.CreateMessage(sessionID, "system", "Compact failed, continuing without it.")
				} else {
					// Drain stdout to avoid blocking, but don't process lines as regular output
					go io.Copy(io.Discard, compactProc.Stdout)
					go io.Copy(io.Discard, compactProc.Stderr)
					<-compactProc.Done
					os.Remove(fmt.Sprintf("/tmp/claude-mcp-%s.json", sessionID))

					if compactProc.ExitCode == 0 {
						log.Printf("session %s: compact completed successfully", sessionID)
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact complete.")
					} else {
						log.Printf("session %s: compact exited with code %d", sessionID, compactProc.ExitCode)
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact failed, continuing without it.")
					}
					compactCompleteMsg := fmt.Sprintf(`{"type":"compact_complete","continuation_count":%d}`, continuationCount)
					broadcaster.Send(compactCompleteMsg)
				}
			}

			currentMessage = "You were interrupted due to turn limits. Continue where you left off."
		}
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

// extractAssistantText pulls human-readable text from an assistant NDJSON line.
// Returns empty string if no text content is found.
func extractAssistantText(line string) string {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return ""
	}

	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	// Try as plain string
	var s string
	if err := json.Unmarshal(msg.Message.Content, &s); err == nil {
		return s
	}

	return ""
}

// extractToolNames returns tool names from tool_use content blocks in an assistant NDJSON line.
func extractToolNames(line string) []string {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		return nil
	}
	var names []string
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			label := b.Name
			// Extract context from input
			var input map[string]interface{}
			if json.Unmarshal(b.Input, &input) == nil {
				if fp, ok := input["file_path"].(string); ok {
					parts := strings.Split(fp, "/")
					label += " " + parts[len(parts)-1]
				} else if cmd, ok := input["command"].(string); ok {
					if len(cmd) > 30 {
						cmd = cmd[:30]
					}
					label += " " + cmd
				}
			}
			if len(label) > 40 {
				label = label[:37] + "..."
			}
			names = append(names, label)
		}
	}
	return names
}

func (s *Server) handleShellExecute(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Timeout > 300 {
		req.Timeout = 300
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
	if s.manager.IsRunning(sessionID) {
		http.Error(w, "session has an active process", http.StatusConflict)
		return
	}

	commandID := fmt.Sprintf("shell-%d", time.Now().UnixNano())

	proc, err := s.manager.SpawnShell(sessionID, managed.ShellOpts{
		Command: req.Command,
		CWD:     sess.CWD,
		Timeout: time.Duration(req.Timeout) * time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already has a running process") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	_, _ = s.store.CreateMessage(sessionID, "shell", req.Command)
	_ = s.store.UpdateActivityState(sessionID, "working")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	startMsg := fmt.Sprintf(`{"type":"shell_start","command":%s,"id":%s,"cwd":%s}`,
		jsonString(req.Command), jsonString(commandID), jsonString(sess.CWD))
	broadcaster.Send(startMsg)

	go func() {
		var stdout, stderr strings.Builder
		const maxOutput = 1024 * 1024

		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdoutDone)
			scanner := bufio.NewScanner(proc.Stdout)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if stdout.Len() < maxOutput {
					stdout.WriteString(line + "\n")
				}
				msg := fmt.Sprintf(`{"type":"shell_output","text":%s,"stream":"stdout","id":%s}`,
					jsonString(line+"\n"), jsonString(commandID))
				broadcaster.Send(msg)
			}
		}()

		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			scanner := bufio.NewScanner(proc.Stderr)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if stderr.Len() < maxOutput {
					stderr.WriteString(line + "\n")
				}
				msg := fmt.Sprintf(`{"type":"shell_output","text":%s,"stream":"stderr","id":%s}`,
					jsonString(line+"\n"), jsonString(commandID))
				broadcaster.Send(msg)
			}
		}()

		<-stdoutDone
		<-stderrDone
		<-proc.Done

		timedOut := proc.TimedOut

		stdoutStr := stdout.String()
		if stdout.Len() >= maxOutput {
			stdoutStr += "\n[truncated]"
		}
		stderrStr := stderr.String()
		if stderr.Len() >= maxOutput {
			stderrStr += "\n[truncated]"
		}

		outputJSON := fmt.Sprintf(`{"stdout":%s,"stderr":%s,"exit_code":%d,"timed_out":%t}`,
			jsonString(stdoutStr), jsonString(stderrStr), proc.ExitCode, timedOut)
		_, _ = s.store.CreateMessage(sessionID, "shell_output", outputJSON)

		exitMsg := fmt.Sprintf(`{"type":"shell_exit","code":%d,"id":%s,"timeout":%t}`,
			proc.ExitCode, jsonString(commandID), timedOut)
		broadcaster.Send(exitMsg)

		if proc.ExitCode == 0 {
			_ = s.store.UpdateActivityState(sessionID, "waiting")
		} else {
			_ = s.store.UpdateActivityState(sessionID, "idle")
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": commandID})
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// extractSessionFiles pulls file paths from tool_use content blocks in NDJSON lines.
func extractSessionFiles(line, sessionID string, store *db.Store) {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.Message.Content == nil {
		return
	}

	var blocks []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		var inp struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(b.Input, &inp) != nil || inp.FilePath == "" {
			continue
		}
		action := ""
		switch b.Name {
		case "Edit":
			action = "edit"
		case "Write":
			action = "write"
		case "Read":
			action = "read"
		}
		if action != "" {
			_ = store.InsertSessionFile(sessionID, inp.FilePath, action)
		}
	}
}

func (s *Server) handleRecentDirs(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.RecentDirectories(5)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dirs == nil {
		dirs = []db.RecentDir{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"directories": dirs,
	})
}
