package api

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
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

// buildPersistentArgs builds CLI args for a persistent process (no message in args).
// The message will be sent via stdin as stream-json.
func buildPersistentArgs(sess *db.Session, cfg managed.Config) []string {
	var args []string
	args = append(args, "-p")

	resumeID := sess.ID
	if sess.ClaudeSessionID != "" {
		resumeID = sess.ClaudeSessionID
	}
	if sess.Initialized {
		args = append(args, "--resume", resumeID)
	} else {
		args = append(args, "--session-id", resumeID)
	}

	args = append(args, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")

	if sess.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", sess.MaxTurns))
	}

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

	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}

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

// formatUserTurn formats a user message as a stream-json input line.
func formatUserTurn(message string) string {
	turn := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]string{
				{"type": "text", "text": message},
			},
		},
	}
	b, _ := json.Marshal(turn)
	return string(b)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Message string `json:"message"`
		ImageID string `json:"image_id"`
		Model   string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Message == "" && req.ImageID == "" {
		http.Error(w, "message or image_id is required", http.StatusBadRequest)
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
	// A warm process in "waiting" state is fine — we'll send it a new turn.
	// Only block if actively processing. Check the actual process state too,
	// because activity_state can be stale — the cleanup goroutine updates the
	// DB after GracefulShutdown (up to 10s), or the goroutine may have exited
	// without updating state (panic, unexpected error path).
	if sess.ActivityState == "working" {
		if s.manager.IsRunning(sessionID) {
			http.Error(w, "session is currently processing (may be auto-continuing — wait for it to finish or interrupt first)", http.StatusConflict)
			return
		}
		// No process running — state is stale. Reset and proceed.
		log.Printf("session %s: activity_state is 'working' but no process running, resetting to allow new message", sessionID)
		_ = s.store.UpdateActivityState(sessionID, "waiting")
	}

	// Load uploaded image if provided
	var imageBase64, imageMediaType string
	if req.ImageID != "" {
		imgPath, mediaType := findUploadedImage(sess.CWD, sessionID, req.ImageID)
		if imgPath != "" {
			imgData, readErr := os.ReadFile(imgPath)
			if readErr == nil {
				imageBase64 = base64.StdEncoding.EncodeToString(imgData)
				imageMediaType = mediaType
			} else {
				log.Printf("session %s: failed to read image %s: %v", sessionID, req.ImageID, readErr)
			}
		} else {
			log.Printf("session %s: uploaded image %s not found", sessionID, req.ImageID)
		}
	}

	displayMsg := req.Message
	if imageBase64 != "" {
		displayMsg = formatImageUploadMessage(req.Message, req.ImageID)
	}
	_, _ = s.store.CreateMessage(sessionID, "user", displayMsg)
	_ = s.store.Heartbeat(sessionID) // Update last_seen_at so sidebar highlights recently active sessions
	_ = s.store.UpdateActivityState(sessionID, "working")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	go func() {
		// Safety net: if the goroutine panics, reset activity state so the
		// session doesn't get permanently stuck in "working".
		defer func() {
			if r := recover(); r != nil {
				log.Printf("session %s: message goroutine panicked: %v", sessionID, r)
				_ = s.store.UpdateActivityState(sessionID, "idle")
			}
		}()

		ctx := context.Background()
		continuationCount := 0
		currentMessage := req.Message
		sess.Model = req.Model

		// --- Per-turn mutable state, shared with onLine callback ---
		// onLine runs in the StreamNDJSON goroutine, which lives for the
		// process lifetime. These vars are reset at the top of each turn.
		var mu sync.Mutex
		var assistantEventCount int
		var executionErrored bool
		var hitMaxTurns bool

		// turnDone is signaled by StreamNDJSON on each "result" message.
		// Buffered so the signal isn't lost if we're between select calls.
		turnDone := make(chan struct{}, 4)

		// streamDone closes when the StreamNDJSON goroutine exits (process EOF).
		streamDone := make(chan struct{})

		// onLine persists for the process lifetime — references mutable state via mu.
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
				if json.Unmarshal([]byte(line), &res) == nil {
					if res.Subtype == "error_during_execution" {
						mu.Lock()
						executionErrored = true
						mu.Unlock()
						if len(res.Errors) > 0 {
							errText := strings.Join(res.Errors, "; ")
							_, _ = s.store.CreateMessage(sessionID, "assistant", errText)
						}
					}
					if res.Subtype == "error_max_turns" {
						mu.Lock()
						hitMaxTurns = true
						mu.Unlock()
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
				mu.Lock()
				assistantEventCount++
				mu.Unlock()
			}
			extractSessionFiles(line, sessionID, s.store)
		}

		// Track whether we've started the stream reader for the current process.
		var proc *managed.Process
		var procStarted bool

		for {
			select {
			case <-ctx.Done():
				log.Printf("session %s: client disconnected, stopping auto-continue", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			default:
			}

			// Reset per-turn state
			mu.Lock()
			assistantEventCount = 0
			executionErrored = false
			hitMaxTurns = false
			mu.Unlock()

			// Drain any stale turnDone signals from previous turns
			for {
				select {
				case <-turnDone:
				default:
					goto drained
				}
			}
		drained:

			// Ensure we have a warm process (spawns on first call, reuses after)
			var err error
			proc, err = s.manager.EnsureProcess(sessionID, managed.SpawnOpts{
				Args: buildPersistentArgs(sess, s.manager.Config()),
				CWD:  sess.CWD,
			})
			if err != nil {
				log.Printf("ensure process error for session %s: %v", sessionID, err)
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to start process: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				break
			}

			// Start the stdout reader only once per process
			if !procStarted {
				procStarted = true
				SafeGo("StreamNDJSON:"+sessionID, func() {
					defer close(streamDone)
					managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, turnDone)
				})
			}

			// Send the user message via stdin
			var userTurn string
			if imageBase64 != "" {
				userTurn = formatUserTurnWithImages(currentMessage, []imageData{{base64: imageBase64, mediaType: imageMediaType}})
				imageBase64 = "" // Only include image on the first turn, not auto-continues
			} else {
				userTurn = formatUserTurn(currentMessage)
			}
			if err := s.manager.SendTurn(sessionID, userTurn); err != nil {
				log.Printf("send turn error for session %s: %v", sessionID, err)
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to send message: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				break
			}

			// Wait for turn completion (result event) or process death.
			// With --max-turns, Claude finishes its current turn gracefully
			// and emits a "result" event — no SIGINT needed.
			select {
			case <-turnDone:
				// Turn completed — Claude emitted a "result" event
			case <-proc.Done:
				// Process died unexpectedly
			}

			mu.Lock()
			errored := executionErrored
			events := assistantEventCount
			reachedMaxTurns := hitMaxTurns
			mu.Unlock()

			if !sess.Initialized && !errored {
				_ = s.store.SetInitialized(sessionID)
				sess.Initialized = true
			}

			// Clean up any pending permission request
			s.permissions.Delete(sessionID)

			// Check if process died
			select {
			case <-proc.Done:
				// Process exited — drain stream, clean up
				stderrBytes, _ := io.ReadAll(proc.Stderr)
				<-streamDone

				os.Remove(fmt.Sprintf("/tmp/claude-mcp-%s.json", sessionID))

				if proc.ExitCode != 0 && len(stderrBytes) > 0 {
					errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
					_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
					broadcaster.Send(errMsg)
				}

				// Process died — don't auto-continue on unexpected exits
				state := "waiting"
				if proc.ExitCode != 0 {
					state = "idle"
				}
				_ = s.store.UpdateActivityState(sessionID, state)
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
				broadcaster.Send(doneMsg)
				return

			default:
				// Process still alive — turn completed normally via result event.
				// This is the graceful path: Claude hit --max-turns and emitted
				// a result, but the process stays alive in persistent mode.
			}

			// --- Auto-continue decision ---
			// The process is still alive. Claude completed a turn (hit --max-turns
			// or finished naturally). Only auto-continue when Claude was interrupted
			// by --max-turns (subtype "error_max_turns"). If Claude finished
			// naturally (subtype "success"), it may be asking for user input —
			// don't auto-continue in that case.

			if !reachedMaxTurns {
				log.Printf("session %s: Claude finished naturally (not max_turns), waiting for user input", sessionID)
				_ = s.manager.GracefulShutdown(sessionID, 10*time.Second)
				<-streamDone
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			// First turn (user's original message) — if max_continuations is 0, just finish
			if continuationCount == 0 && sess.MaxContinuations <= 0 {
				_ = s.manager.GracefulShutdown(sessionID, 10*time.Second)
				<-streamDone
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			// Progress guard: if Claude produced < 2 assistant events, it's done or stuck
			if continuationCount > 0 && events < 2 {
				log.Printf("session %s not making progress (%d events), stopping auto-continue", sessionID, events)
				_, _ = s.store.CreateMessage(sessionID, "system", "Auto-continue stopped: not making progress")
				noProgressMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d,"reason":"no_progress"}`, continuationCount)
				broadcaster.Send(noProgressMsg)
				_ = s.manager.GracefulShutdown(sessionID, 10*time.Second)
				<-streamDone
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			continuationCount++

			if continuationCount > sess.MaxContinuations {
				log.Printf("session %s exhausted auto-continues (%d/%d)", sessionID, continuationCount, sess.MaxContinuations)
				exhaustedMsg := fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d}`, continuationCount)
				broadcaster.Send(exhaustedMsg)
				_, _ = s.store.CreateMessage(sessionID, "system",
					fmt.Sprintf("Auto-continue limit reached (%d/%d)", continuationCount, sess.MaxContinuations))
				_ = s.manager.GracefulShutdown(sessionID, 10*time.Second)
				<-streamDone
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, 0)
				broadcaster.Send(doneMsg)
				return
			}

			// Auto-continue: notify via SSE
			continuingMsg := fmt.Sprintf(`{"type":"auto_continuing","continuation_count":%d,"max_continuations":%d}`,
				continuationCount, sess.MaxContinuations)
			broadcaster.Send(continuingMsg)
			_, _ = s.store.CreateMessage(sessionID, "system",
				fmt.Sprintf("Auto-continuing (%d/%d)...", continuationCount, sess.MaxContinuations))

			// Compact step — send /compact as a user turn via stdin on the warm process.
			// No need to spawn a separate process since the warm process is still alive.
			if sess.CompactEveryNContinues > 0 && continuationCount%sess.CompactEveryNContinues == 0 {
				compactingMsg := fmt.Sprintf(`{"type":"compacting","continuation_count":%d}`, continuationCount)
				broadcaster.Send(compactingMsg)
				_, _ = s.store.CreateMessage(sessionID, "system", "Running /compact to reduce context size...")

				compactTurn := formatUserTurn("/compact")
				if err := s.manager.SendTurn(sessionID, compactTurn); err != nil {
					log.Printf("session %s: compact send failed: %v", sessionID, err)
					_, _ = s.store.CreateMessage(sessionID, "system", fmt.Sprintf("Compact failed: %v, continuing without it.", err))
				} else {
					// Wait for compact to complete (result event)
					select {
					case <-turnDone:
						log.Printf("session %s: compact completed", sessionID)
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact complete.")
					case <-proc.Done:
						log.Printf("session %s: process died during compact", sessionID)
						_ = s.store.UpdateActivityState(sessionID, "idle")
						doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
						broadcaster.Send(doneMsg)
						return
					}
				}
				compactCompleteMsg := fmt.Sprintf(`{"type":"compact_complete","continuation_count":%d}`, continuationCount)
				broadcaster.Send(compactCompleteMsg)
			}

			// Send continuation prompt via stdin on the same warm process
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

	// Flush headers immediately so the browser's EventSource fires onopen
	// before any shell commands are sent. Without this, fast shell commands
	// can broadcast events before the SSE subscriber is ready.
	flusher.Flush()

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
		ID      string `json:"id"`
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

	// Allow client-provided ID so frontend can set up SSE listener and
	// placeholder messages before sending the command (avoids race condition).
	commandID := req.ID
	if commandID == "" {
		commandID = fmt.Sprintf("shell-%d", time.Now().UnixNano())
	}

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

	SafeGo("shell:"+commandID, func() {
		var stdout, stderr strings.Builder
		const maxOutput = 1024 * 1024

		stdoutDone := make(chan struct{})
		SafeGo("shell-stdout:"+commandID, func() {
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
		})

		stderrDone := make(chan struct{})
		SafeGo("shell-stderr:"+commandID, func() {
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
		})

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
	})

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

func (s *Server) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}
	if sess.ActivityState == "working" {
		http.Error(w, "cannot clear while session is working", http.StatusConflict)
		return
	}

	s.manager.Teardown(sessionID, 5*time.Second)

	if err := s.store.ClearSession(sessionID); err != nil {
		log.Printf("clear session %s: %v", sessionID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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
