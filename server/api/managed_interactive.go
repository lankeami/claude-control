package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// escStopFallback is how long we wait for the Stop hook after sending an ESC
// interrupt before treating the turn as ended anyway (the hook may not fire
// on interrupts). Variable so tests can shorten it.
var escStopFallback = 10 * time.Second

// transcriptDiscoveryFallback is how long we wait for the SessionStart hook
// to report the transcript path before computing it locally.
var transcriptDiscoveryFallback = 30 * time.Second

// compactTimeout bounds how long we wait for /compact to finish.
var compactTimeout = 5 * time.Minute

// buildInteractiveArgs builds CLI args for a long-lived interactive process.
// Tool restrictions and lifecycle hooks travel via the generated settings
// file rather than -p-only flags.
func buildInteractiveArgs(sess *db.Session, settingsPath string) []string {
	var args []string
	resumeID := sess.ID
	if sess.ClaudeSessionID != "" {
		resumeID = sess.ClaudeSessionID
	}
	if sess.Initialized {
		args = append(args, "--resume", resumeID)
	} else {
		args = append(args, "--session-id", resumeID)
	}
	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}
	if settingsPath != "" {
		args = append(args, "--settings", settingsPath)
	}
	return args
}

// interactiveTurnState tracks per-turn counters shared between the transcript
// callback (tailer goroutine) and the orchestrator goroutine.
type interactiveTurnState struct {
	mu              sync.Mutex
	assistantCount  int
	inputTokens     int
	outputTokens    int
	interruptedFor  string // "" | "max_turns" | "budget"
}

func (t *interactiveTurnState) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.assistantCount = 0
	t.inputTokens = 0
	t.outputTokens = 0
	t.interruptedFor = ""
}

func (t *interactiveTurnState) snapshot() (count, in, out int, interrupted string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.assistantCount, t.inputTokens, t.outputTokens, t.interruptedFor
}

// interactiveTranscriptEntry is a tolerant parse of a transcript JSONL line. Unknown
// fields pass through untouched because we forward the raw line.
type interactiveTranscriptEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// hasOnlyTextContent reports whether a user entry's content is plain text
// (the echo of a prompt we sent) rather than tool results.
func hasOnlyTextContent(content json.RawMessage) bool {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return true
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return true
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
	}
	return true
}

// managedSessionSettings writes the per-session settings file (lifecycle
// hooks + permission allowlist) and returns its path.
func managedSessionSettings(cfg managed.Config, sess *db.Session) (string, error) {
	return managed.WriteSessionSettings(managed.SessionDir(sess.ID), cfg.BinaryPath, sess.ID, cfg.ServerPort, sess.AllowedTools)
}

func interactiveOpts(sess *db.Session, settingsPath string, onLine func(string)) managed.InteractiveOpts {
	return managed.InteractiveOpts{
		Args:             buildInteractiveArgs(sess, settingsPath),
		CWD:              sess.CWD,
		OnTranscriptLine: onLine,
	}
}

// handleSendMessageInteractive is the synchronous half of an interactive
// send: persist the user message, pick a model, kick off the turn goroutine,
// and reply {"status":"started"} like print mode does.
func (s *Server) handleSendMessageInteractive(w http.ResponseWriter, sess *db.Session, message, modelOverride string, imageIDs []string) {
	sessionID := sess.ID

	var imagePaths []string
	for _, id := range imageIDs {
		imgPath, _ := findUploadedImage(sess.CWD, sessionID, id)
		if imgPath == "" {
			log.Printf("session %s: uploaded image %s not found", sessionID, id)
			continue
		}
		imagePaths = append(imagePaths, imgPath)
	}

	displayMsg := message
	if len(imagePaths) > 0 {
		label := fmt.Sprintf("%d image", len(imagePaths))
		if len(imagePaths) > 1 {
			label += "s"
		}
		displayMsg = formatImageUploadMessage(message, label)
	}
	_, _ = s.store.CreateMessage(sessionID, "user", displayMsg, 0)
	_ = s.store.Heartbeat(sessionID)
	_ = s.store.UpdateActivityState(sessionID, "working")

	// Model is fixed at spawn time for the long-lived process; only apply
	// overrides/heuristics when no process exists yet.
	if !s.manager.IsInteractiveRunning(sessionID) {
		selected := modelOverride
		if selected == "" {
			selected = selectModel(message, len(imagePaths) > 0, "")
		}
		if selected != "" && sess.Model != selected {
			sess.Model = selected
			_ = s.store.UpdateSessionModel(sessionID, selected)
		}
	}

	s.sendMessageInteractive(sess, message, imagePaths)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// sendMessageInteractive runs one user turn against the session's long-lived
// interactive Claude Code process. It mirrors the print-mode flow but derives
// turn boundaries from Stop hooks and message content from the transcript.
func (s *Server) sendMessageInteractive(sess *db.Session, message string, imagePaths []string) {
	sessionID := sess.ID
	broadcaster := s.manager.GetBroadcaster(sessionID)
	turn := &interactiveTurnState{}

	maxTurns := sess.MaxTurns
	prompt := message
	for _, p := range imagePaths {
		prompt += fmt.Sprintf("\n\n[Attached image: %s] — use the Read tool to view it.", p)
	}

	SafeGo("interactive:"+sessionID, func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("session %s: interactive goroutine panicked: %v", sessionID, r)
				_ = s.store.UpdateActivityState(sessionID, "idle")
			}
		}()

		modelEvt, _ := json.Marshal(map[string]string{"type": "model_selected", "model": sess.Model, "reason": "session"})
		broadcaster.Send(string(modelEvt))

		// onTranscriptLine runs on the tailer goroutine for the process
		// lifetime. It forwards chat entries to SSE, persists them, and
		// enforces the max-turns limit by interrupting mid-turn.
		onTranscriptLine := func(line string) {
			var entry interactiveTranscriptEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return // not JSON we understand; skip broadcast, keep tailing
			}
			if entry.IsSidechain {
				return
			}
			switch entry.Type {
			case "assistant":
				broadcaster.Send(line)
				if text := extractAssistantText(line); text != "" {
					_, _ = s.store.CreateMessage(sessionID, "assistant", text, 0)
				}
				for _, toolName := range extractToolNames(line) {
					_, _ = s.store.CreateMessage(sessionID, "activity", toolName, 0)
				}
				extractSessionFiles(line, sessionID, s.store)

				turn.mu.Lock()
				turn.assistantCount++
				turn.inputTokens += entry.Message.Usage.InputTokens
				turn.outputTokens += entry.Message.Usage.OutputTokens
				count := turn.assistantCount
				alreadyInterrupted := turn.interruptedFor != ""
				if maxTurns > 0 && count >= maxTurns && !alreadyInterrupted {
					turn.interruptedFor = "max_turns"
				}
				shouldInterrupt := turn.interruptedFor == "max_turns" && !alreadyInterrupted
				turn.mu.Unlock()

				if shouldInterrupt {
					log.Printf("session %s: hit max turns (%d), interrupting", sessionID, maxTurns)
					_ = s.manager.InterruptInteractive(sessionID)
				}
			case "user":
				if !hasOnlyTextContent(entry.Message.Content) {
					broadcaster.Send(line)
				}
			}
		}

		settingsPath, err := managedSessionSettings(s.manager.Config(), sess)
		if err != nil {
			log.Printf("session %s: settings generation failed: %v", sessionID, err)
		}

		spawned := !s.manager.IsInteractiveRunning(sessionID)
		proc, err := s.manager.EnsureInteractive(sessionID, interactiveOpts(sess, settingsPath, onTranscriptLine))
		if err != nil {
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to start interactive session: %s"}`, err.Error())
			broadcaster.Send(errMsg)
			_, _ = s.store.CreateMessage(sessionID, "system", "Failed to start interactive session: "+err.Error(), 0)
			_ = s.store.UpdateActivityState(sessionID, "idle")
			broadcaster.Send(`{"type":"done","exit_code":1}`)
			return
		}

		// Fallback transcript discovery if the SessionStart hook never fires
		// (e.g. hooks misconfigured): compute the path from the CWD encoding.
		if spawned {
			SafeGo("transcript-fallback:"+sessionID, func() {
				select {
				case <-time.After(transcriptDiscoveryFallback):
				case <-proc.Done:
					return
				}
				resumeID := sess.ID
				if sess.ClaudeSessionID != "" {
					resumeID = sess.ClaudeSessionID
				}
				if dir, err := claudeProjectsDir(sess.CWD); err == nil {
					s.manager.SetTranscript(sessionID, filepath.Join(dir, resumeID+".jsonl"))
				}
			})
		}

		stopCh := s.manager.StopEvents(sessionID)
		continuationCount := 0
		currentPrompt := prompt

		for {
			turn.reset()
			// Drain stale stop signals from previous turns
			for {
				select {
				case <-stopCh:
					continue
				default:
				}
				break
			}

			if err := s.manager.SendPrompt(sessionID, currentPrompt); err != nil {
				errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to send prompt: %s"}`, err.Error())
				broadcaster.Send(errMsg)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				broadcaster.Send(`{"type":"done","exit_code":1}`)
				return
			}

			procDied := !s.waitForTurnEnd(sessionID, stopCh, proc.Done, turn)

			count, in, out, interrupted := turn.snapshot()

			if !sess.Initialized {
				_ = s.store.SetInitialized(sessionID)
				sess.Initialized = true
			}

			// Synthesize the result event print mode used to emit.
			subtype := "success"
			if interrupted == "max_turns" {
				subtype = "error_max_turns"
			}
			cost := calcCost(sess.Model, in, out)
			if cost > 0 {
				_, _ = s.store.CreateMessage(sessionID, "cost", fmt.Sprintf("%.6f", cost), cost)
			}
			resultEvt, _ := json.Marshal(map[string]any{
				"type": "result", "subtype": subtype,
				"usage": map[string]int{"input_tokens": in, "output_tokens": out},
				"cost":  cost, "model": sess.Model,
			})
			broadcaster.Send(string(resultEvt))

			if newCount, err := s.store.IncrementTurnCount(sessionID); err == nil {
				turnMsg, _ := json.Marshal(map[string]any{"type": "turn_count", "turn_count": newCount})
				broadcaster.Send(string(turnMsg))
			}

			if procDied {
				state := "waiting"
				if proc.ExitCode != 0 {
					state = "idle"
					errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, proc.LastOutput(), proc.ExitCode)
					_, _ = s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode, 0)
					broadcaster.Send(errMsg)
				}
				_ = s.store.UpdateActivityState(sessionID, state)
				broadcaster.Send(fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode))
				os.RemoveAll(filepath.Dir(settingsPath))
				return
			}

			// Budget enforcement from accumulated transcript usage.
			if sess.MaxBudgetUSD > 0 {
				if total, err := s.store.SessionCostTotal(sessionID); err == nil && total > sess.MaxBudgetUSD {
					log.Printf("session %s: budget exceeded ($%.4f > $%.2f)", sessionID, total, sess.MaxBudgetUSD)
					budgetEvt, _ := json.Marshal(map[string]any{"type": "budget_exceeded", "total": total, "budget": sess.MaxBudgetUSD})
					broadcaster.Send(string(budgetEvt))
					_, _ = s.store.CreateMessage(sessionID, "system",
						fmt.Sprintf("Budget limit reached ($%.2f of $%.2f). Session paused.", total, sess.MaxBudgetUSD), 0)
					s.finishInteractiveTurn(sessionID, broadcaster)
					return
				}
			}

			if interrupted != "max_turns" {
				// Claude finished naturally — wait for the next user message.
				s.finishInteractiveTurn(sessionID, broadcaster)
				return
			}

			// --- Auto-continue (same rules as print mode) ---
			if continuationCount == 0 && sess.MaxContinuations <= 0 {
				s.finishInteractiveTurn(sessionID, broadcaster)
				return
			}
			if continuationCount > 0 && count < 2 {
				log.Printf("session %s not making progress (%d events), stopping auto-continue", sessionID, count)
				_, _ = s.store.CreateMessage(sessionID, "system", "Auto-continue stopped: not making progress", 0)
				broadcaster.Send(fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d,"reason":"no_progress"}`, continuationCount))
				s.finishInteractiveTurn(sessionID, broadcaster)
				return
			}
			continuationCount++
			if continuationCount > sess.MaxContinuations {
				log.Printf("session %s exhausted auto-continues (%d/%d)", sessionID, continuationCount, sess.MaxContinuations)
				broadcaster.Send(fmt.Sprintf(`{"type":"auto_continue_exhausted","continuation_count":%d}`, continuationCount))
				_, _ = s.store.CreateMessage(sessionID, "system",
					fmt.Sprintf("Auto-continue limit reached (%d/%d)", continuationCount, sess.MaxContinuations), 0)
				s.finishInteractiveTurn(sessionID, broadcaster)
				return
			}

			broadcaster.Send(fmt.Sprintf(`{"type":"auto_continuing","continuation_count":%d,"max_continuations":%d}`,
				continuationCount, sess.MaxContinuations))
			_, _ = s.store.CreateMessage(sessionID, "system",
				fmt.Sprintf("Auto-continuing (%d/%d)...", continuationCount, sess.MaxContinuations), 0)

			if sess.CompactEveryNContinues > 0 && continuationCount%sess.CompactEveryNContinues == 0 {
				broadcaster.Send(fmt.Sprintf(`{"type":"compacting","continuation_count":%d}`, continuationCount))
				_, _ = s.store.CreateMessage(sessionID, "system", "Running /compact to reduce context size...", 0)
				if err := s.manager.SendPrompt(sessionID, "/compact"); err != nil {
					_, _ = s.store.CreateMessage(sessionID, "system", fmt.Sprintf("Compact failed: %v, continuing without it.", err), 0)
				} else {
					select {
					case <-stopCh:
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact complete.", 0)
					case <-proc.Done:
						_ = s.store.UpdateActivityState(sessionID, "idle")
						broadcaster.Send(fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode))
						return
					case <-time.After(compactTimeout):
						_, _ = s.store.CreateMessage(sessionID, "system", "Compact timed out, continuing.", 0)
					}
				}
				broadcaster.Send(fmt.Sprintf(`{"type":"compact_complete","continuation_count":%d}`, continuationCount))
			}

			currentPrompt = "You were interrupted due to turn limits. Continue where you left off."
		}
	})
}

// waitForTurnEnd blocks until the Stop hook fires, the process dies, or — if
// the turn was interrupted via ESC — a fallback timer expires (the Stop hook
// may not fire on interrupts). Returns false if the process died.
func (s *Server) waitForTurnEnd(sessionID string, stopCh <-chan struct{}, done <-chan struct{}, turn *interactiveTurnState) bool {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var interruptedAt time.Time
	for {
		select {
		case <-stopCh:
			return true
		case <-done:
			return false
		case <-ticker.C:
			_, _, _, interrupted := turn.snapshot()
			if interrupted != "" && interruptedAt.IsZero() {
				interruptedAt = time.Now()
			}
			if !interruptedAt.IsZero() && time.Since(interruptedAt) > escStopFallback {
				log.Printf("session %s: no Stop hook after interrupt, assuming turn ended", sessionID)
				return true
			}
		}
	}
}

func (s *Server) finishInteractiveTurn(sessionID string, broadcaster *managed.Broadcaster) {
	_ = s.store.UpdateActivityState(sessionID, "waiting")
	broadcaster.Send(`{"type":"done","exit_code":0}`)
}
