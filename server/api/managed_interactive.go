package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// escStopFallback is how long we wait for the Stop hook after sending an ESC
// interrupt before treating the turn as ended anyway (the hook may not fire
// on interrupts). Variable so tests can shorten it.
var escStopFallback = 10 * time.Second

// localCommandFallback is how long we wait before concluding a slash command
// was handled locally by the CLI (no model turn started). Variable for tests.
var localCommandFallback = 5 * time.Second

// transcriptDiscoveryFallback is how long we wait for the SessionStart hook
// to report the transcript path before computing it locally. Kept short so
// new-directory sessions (where the trust dialog delays the hook) still get
// transcript tailing before the first turn completes.
var transcriptDiscoveryFallback = 10 * time.Second

// compactTimeout bounds how long we wait for /compact to finish.
var compactTimeout = 5 * time.Minute

// promptEchoTimeout is how long we wait for the typed prompt to appear as a
// user entry in the transcript before re-sending Enter (the paste may land
// before the TUI is accepting the submit keypress).
var promptEchoTimeout = 15 * time.Second

// promptEchoRetries is how many extra Enter presses we attempt.
const promptEchoRetries = 2

// buildInteractiveArgs builds CLI args for a long-lived interactive process.
// Tool restrictions and lifecycle hooks travel via the generated settings
// file rather than -p-only flags.
func buildInteractiveArgs(sess *db.Session, settingsPath, trustPrompt string) []string {
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
	if trustPrompt != "" {
		args = append(args, "--append-system-prompt", trustPrompt)
	}
	return args
}

// turnEndReason distinguishes why waitForTurnEnd unblocked.
type turnEndReason int

const (
	turnEndStop           turnEndReason = iota // Stop hook fired (normal turn completion)
	turnEndDied                                // Process exited
	turnEndQuestion                            // AskUserQuestion detected in transcript
	turnEndUnknownCommand                      // CLI rejected the prompt as an unknown slash command
	turnEndLocalCommand                        // Slash command processed locally by the CLI (no model turn)
	turnEndSuperseded                          // A new message arrived; this goroutine must exit silently
	turnEndPromptFailed                        // Prompt was never confirmed after retries
)

// sessionTokenTotals tracks cumulative token counts across all turns for a
// managed session. Updated from the transcript tailing callback.
type sessionTokenTotals struct {
	mu           sync.Mutex
	inputTokens  int
	outputTokens int
}

func (t *sessionTokenTotals) add(in, out int) {
	t.mu.Lock()
	t.inputTokens += in
	t.outputTokens += out
	t.mu.Unlock()
}

func (t *sessionTokenTotals) snapshot() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inputTokens, t.outputTokens
}

// interactiveTurnState tracks per-turn counters shared between the transcript
// callback (tailer goroutine) and the orchestrator goroutine.
type interactiveTurnState struct {
	mu             sync.Mutex
	assistantCount int
	inputTokens    int
	outputTokens   int
	interruptedFor string // "" | "max_turns" | "budget"
	promptEchoed   bool   // the typed prompt appeared in the transcript
	questionCh      chan struct{}
	unknownCmd      string // slash command the CLI rejected ("Unknown command: ...")
	unknownCmdCh    chan struct{}
	promptFailedCh  chan struct{}
	slashCmd       bool   // prompt starts with "/" — enables local command fallback
	// seenMsgIDs tracks assistant message IDs already counted this turn.
	// Native transcripts split one API response into multiple JSONL entries
	// (one per content block) that share a message ID and repeat the same
	// usage stats — counting each entry doubles turn counts and token sums.
	seenMsgIDs map[string]bool
}

func (t *interactiveTurnState) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.assistantCount = 0
	t.inputTokens = 0
	t.outputTokens = 0
	t.interruptedFor = ""
	t.promptEchoed = false
	t.seenMsgIDs = make(map[string]bool)
	t.unknownCmd = ""
	// Drain any stale signals
	select {
	case <-t.questionCh:
	default:
	}
	select {
	case <-t.unknownCmdCh:
	default:
	}
	select {
	case <-t.promptFailedCh:
	default:
	}
}

// markUnknownCommand records that the CLI rejected the prompt as an unknown
// slash command and unblocks the turn loop.
func (t *interactiveTurnState) markUnknownCommand(cmd string) {
	t.mu.Lock()
	t.unknownCmd = cmd
	t.mu.Unlock()
	select {
	case t.unknownCmdCh <- struct{}{}:
	default:
	}
}

func (t *interactiveTurnState) unknownCommand() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.unknownCmd
}

func (t *interactiveTurnState) markPromptEchoed() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.promptEchoed = true
}

// submitted reports whether there is transcript evidence the prompt reached
// Claude: either the user-entry echo or any assistant activity.
func (t *interactiveTurnState) submitted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.promptEchoed || t.assistantCount > 0
}

func (t *interactiveTurnState) snapshot() (count, in, out int, interrupted string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.assistantCount, t.inputTokens, t.outputTokens, t.interruptedFor
}

// interactiveTranscriptEntry is a tolerant parse of a transcript JSONL line. Unknown
// fields pass through untouched because we forward the raw line.
type interactiveTranscriptEntry struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	Content     json.RawMessage `json:"content"` // top-level content of system entries
	Message     struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// isCliNoiseMessage returns true for known CLI-internal messages that should
// not surface in the web UI (e.g. "No response requested." when input arrives
// in an unexpected state).
func isCliNoiseMessage(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "No response requested." || trimmed == "No response requested"
}

// interactiveStdinCommands lists shell programs that require interactive stdin
// and do not work well over the managed session PTY relay.
var interactiveStdinCommands = map[string]bool{
	"aws":       true, // aws configure
	"ssh":       true,
	"vim":       true,
	"vi":        true,
	"nano":      true,
	"emacs":     true,
	"less":      true,
	"more":      true,
	"top":       true,
	"htop":      true,
	"mysql":     true,
	"psql":      true,
	"mongo":     true,
	"mongosh":   true,
	"redis-cli": true,
	"python":    true,
	"python3":   true,
	"node":      true,
	"irb":       true,
	"ghci":      true,
	"telnet":    true,
	"ftp":       true,
	"sftp":      true,
	"nslookup":  true,
	"bc":        true,
	"gdb":       true,
	"lldb":      true,
}

// interactiveStdinWarning checks whether message is a "!" shell command whose
// program typically requires interactive stdin (aws configure, ssh, vim, etc.).
// Returns a user-facing warning string, or "" if no warning is needed.
func interactiveStdinWarning(message string) string {
	trimmed := strings.TrimSpace(message)
	if !strings.HasPrefix(trimmed, "!") {
		return ""
	}
	// Strip the "!" prefix and any leading whitespace to get the command.
	cmd := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	if cmd == "" {
		return ""
	}
	// Extract the base command name (first word).
	parts := strings.Fields(cmd)
	base := parts[0]
	// Handle paths like /usr/bin/vim → vim
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}

	if !interactiveStdinCommands[base] {
		return ""
	}

	return fmt.Sprintf("Warning: '%s' is an interactive command that requires stdin input. "+
		"Interactive prompts don't work well in managed sessions — the command may hang or behave unexpectedly. "+
		"Consider using non-interactive alternatives instead.", base)
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
	return managed.WriteSessionSettings(managed.SessionDir(sess.ID), cfg.BinaryPath, sess.ID, cfg.ServerPort, sess.AllowedTools, cfg.KeyFilePath)
}

func interactiveOpts(sess *db.Session, settingsPath, trustPrompt string, onLine, onWorkflowEvent func(string)) managed.InteractiveOpts {
	return managed.InteractiveOpts{
		Args:             buildInteractiveArgs(sess, settingsPath, trustPrompt),
		CWD:              sess.CWD,
		OnTranscriptLine: onLine,
		OnWorkflowEvent:  onWorkflowEvent,
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

	// Warn the user if the message looks like a "!" shell command that
	// requires interactive stdin (aws configure, ssh, vim, etc.). The
	// managed session's PTY relay cannot forward interactive prompts.
	if warn := interactiveStdinWarning(message); warn != "" {
		log.Printf("session %s: interactive stdin warning for %q", sessionID, message)
		_, _ = s.store.CreateMessage(sessionID, "system", warn, 0)
		warnEvt, _ := json.Marshal(map[string]interface{}{"type": "system", "message": warn})
		s.manager.GetBroadcaster(sessionID).Send(string(warnEvt))
	}

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

	// Skill enforcement: if the message is a registered skill invocation,
	// wrap it with a mandatory Skill() instruction prefix to prevent the
	// model from rationalizing around the skill pipeline.
	enforcedMessage := message
	cwd := sess.EffectiveCWD()
	if cwd == "" {
		cwd = sess.CWD
	}
	if cwd != "" {
		registry := skillNamesForCWD(cwd)
		if skill, args, isSkill := ParseSkillInvocation(message, registry); isSkill {
			enforcedMessage = EnforcementPrefix(skill, args)
			log.Printf("session %s: skill enforcement applied for /%s", sessionID, skill)
		}
	}

	s.sendMessageInteractive(sess, enforcedMessage, imagePaths)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// sendMessageInteractive runs one user turn against the session's long-lived
// interactive Claude Code process. It mirrors the print-mode flow but derives
// turn boundaries from Stop hooks and message content from the transcript.
func (s *Server) sendMessageInteractive(sess *db.Session, message string, imagePaths []string) {
	sessionID := sess.ID
	broadcaster := s.manager.GetBroadcaster(sessionID)
	// Register this message's turn state so the long-lived transcript
	// callback (registered at spawn, possibly by an earlier message's
	// goroutine) feeds counters into the current turn.
	turn := &interactiveTurnState{questionCh: make(chan struct{}, 1), unknownCmdCh: make(chan struct{}, 1), promptFailedCh: make(chan struct{}, 1)}
	s.interactiveTurns.Store(sessionID, turn)

	// Cancel any previous turn goroutine for this session.
	supersedeCh := make(chan struct{})
	if old, loaded := s.supersedeChs.Swap(sessionID, supersedeCh); loaded {
		close(old.(chan struct{}))
	}

	prompt := message
	for _, p := range imagePaths {
		prompt += fmt.Sprintf("\n\n[Attached image: %s] — use the Read tool to view it.", p)
	}

	SafeGo("interactive:"+sessionID, func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("session %s: interactive goroutine panicked: %v", sessionID, r)
				_ = s.store.UpdateActivityState(sessionID, "idle")
				broadcaster.Send(`{"type":"done","exit_code":1}`)
				return
			}
			// Safety net: if the goroutine exits normally but state is still
			// "working", something prevented the normal state transition. Skip
			// cleanup when superseded — the replacement goroutine owns the state.
			select {
			case <-supersedeCh:
				return
			default:
			}
			if cur, err := s.store.GetSession(sessionID); err == nil && cur != nil && cur.ActivityState == "working" {
				log.Printf("session %s: interactive goroutine exited with activity_state still 'working' — resetting to 'waiting'", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "waiting")
				broadcaster.Send(`{"type":"done","exit_code":0}`)
			}
		}()

		modelEvt, _ := json.Marshal(map[string]string{"type": "model_selected", "model": sess.Model, "reason": "session"})
		broadcaster.Send(string(modelEvt))

		s.runInteractiveTurns(sess, prompt, turn, broadcaster, 0, supersedeCh)
	})
}

// isSessionIDConflict reports whether the process died because the CLI
// session ID we passed can't be used (registered by a previous spawn whose
// turn never completed, or a --resume of a session with no transcript).
func isSessionIDConflict(out string) bool {
	return strings.Contains(out, "already in use") || strings.Contains(out, "No conversation found")
}

// retryAfterSessionIDConflict recovers a session wedged on an unusable CLI
// session ID: rotate to a fresh ID and re-run the turn once. Returns true if
// the retry was started (the caller must return without further handling).
func (s *Server) retryAfterSessionIDConflict(sess *db.Session, prompt string, turn *interactiveTurnState, broadcaster *managed.Broadcaster, proc *managed.InteractiveProc, attempt int, supersedeCh <-chan struct{}) bool {
	if attempt > 0 {
		return false
	}
	select {
	case <-proc.Done:
	default:
		return false
	}
	if !isSessionIDConflict(proc.LastOutput()) {
		return false
	}
	newID, err := s.store.RotateClaudeSessionID(sess.ID)
	if err != nil {
		log.Printf("session %s: rotate claude_session_id failed: %v", sess.ID, err)
		return false
	}
	sess.ClaudeSessionID = newID
	sess.Initialized = false
	log.Printf("session %s: CLI session ID conflict, rotated to %s and retrying", sess.ID, newID)
	_, _ = s.store.CreateMessage(sess.ID, "system", "Claude session ID conflict detected — retrying with a fresh session.", 0)
	s.runInteractiveTurns(sess, prompt, turn, broadcaster, 1, supersedeCh)
	return true
}

// runInteractiveTurns spawns (or reuses) the session's interactive process
// and drives prompt turns until the conversation goes idle. attempt guards
// the single session-ID-conflict retry.
func (s *Server) runInteractiveTurns(sess *db.Session, prompt string, turn *interactiveTurnState, broadcaster *managed.Broadcaster, attempt int, supersedeCh <-chan struct{}) {
	sessionID := sess.ID
	maxTurns := sess.MaxTurns
	log.Printf("session %s: runInteractiveTurns starting, prompt=%q", sessionID, prompt[:min(len(prompt), 60)])

	// onTranscriptLine runs on the tailer goroutine for the process
	// lifetime — across many turns. It forwards chat entries to SSE,
	// persists them, and enforces the max-turns limit by interrupting
	// mid-turn. Turn counters are looked up via s.interactiveTurns so
	// the callback always feeds the turn currently in flight.
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
			if text := extractAssistantText(line); isCliNoiseMessage(text) {
				log.Printf("session %s: suppressed CLI noise message: %q", sessionID, text)
				return
			}
			broadcaster.Send(line)
			if text := extractAssistantText(line); text != "" {
				_, _ = s.store.CreateMessage(sessionID, "assistant", text, 0)
			}
			for _, toolName := range extractToolNames(line) {
				_, _ = s.store.CreateMessage(sessionID, "activity", toolName, 0)
			}
			// Fallback question detection: the CLI only flushes this entry
			// once the question is resolved, so normally the PreToolUse hook
			// has already registered it and this is a deduped no-op.
			if pq := extractAskUserQuestion(line); pq != nil {
				s.registerPendingQuestion(sessionID, pq, "transcript")
			}
			extractSessionFiles(line, sessionID, s.store)
			s.detectWorktreeToolUse(line, sessionID)

			v, ok := s.interactiveTurns.Load(sessionID)
			if !ok {
				return
			}
			cur := v.(*interactiveTurnState)

			cur.mu.Lock()
			if cur.seenMsgIDs == nil {
				cur.seenMsgIDs = make(map[string]bool)
			}
			// Count each API response once, not once per content-block entry.
			firstEntry := entry.Message.ID == "" || !cur.seenMsgIDs[entry.Message.ID]
			if firstEntry {
				if entry.Message.ID != "" {
					cur.seenMsgIDs[entry.Message.ID] = true
				}
				cur.assistantCount++
				cur.inputTokens += entry.Message.Usage.InputTokens
				cur.outputTokens += entry.Message.Usage.OutputTokens
				// Also accumulate into session-level cumulative totals.
				if in, out := entry.Message.Usage.InputTokens, entry.Message.Usage.OutputTokens; in > 0 || out > 0 {
					totalsVal, _ := s.sessionTokenTotals.LoadOrStore(sessionID, &sessionTokenTotals{})
					totalsVal.(*sessionTokenTotals).add(in, out)
				}
			}
			count := cur.assistantCount
			alreadyInterrupted := cur.interruptedFor != ""
			if maxTurns > 0 && count >= maxTurns && !alreadyInterrupted {
				cur.interruptedFor = "max_turns"
			}
			shouldInterrupt := cur.interruptedFor == "max_turns" && !alreadyInterrupted
			cur.mu.Unlock()

			if shouldInterrupt {
				log.Printf("session %s: hit max turns (%d), interrupting", sessionID, maxTurns)
				_ = s.manager.InterruptInteractive(sessionID)
			}
		case "user":
			if !hasOnlyTextContent(entry.Message.Content) {
				broadcaster.Send(line)
				s.processWorktreeResults(entry.Message.Content, sessionID, broadcaster)
				// A tool_result for the pending question means it was answered
				// outside the web UI (TUI/terminal) — clear the card and let
				// the turn loop resume via the WaitForClear channel.
				if pq := s.pendingQuestions.Get(sessionID); pq != nil && hasToolResultFor(entry.Message.Content, pq.ToolUseID) {
					log.Printf("session %s: pending question %s answered outside the UI, clearing", sessionID, pq.ToolUseID)
					s.pendingQuestions.Delete(sessionID)
					broadcaster.Send(`{"type":"question_cleared"}`)
				}
			} else if v, ok := s.interactiveTurns.Load(sessionID); ok {
				v.(*interactiveTurnState).markPromptEchoed()
			}
		case "system":
			// The CLI rejects a typo'd slash command with a system entry and
			// never starts a turn — surface it instead of hanging until the
			// generic "not confirmed" warning.
			var text string
			if json.Unmarshal(entry.Content, &text) == nil && strings.HasPrefix(text, "Unknown command:") {
				cmd := strings.TrimSpace(strings.TrimPrefix(text, "Unknown command:"))
				if v, ok := s.interactiveTurns.Load(sessionID); ok {
					v.(*interactiveTurnState).markUnknownCommand(cmd)
				}
			}
		}
	}

	settingsPath, err := managedSessionSettings(s.manager.Config(), sess)
	if err != nil {
		log.Printf("session %s: settings generation failed: %v", sessionID, err)
	}

	// Workflow tool run snapshots (autoship pipelines etc.) — recorded for
	// late-connecting clients and broadcast on the session stream.
	onWorkflowEvent := func(line string) {
		s.emitWorkflowRun(sessionID, line)
	}

	spawned := !s.manager.IsInteractiveRunning(sessionID)
	proc, err := s.manager.EnsureInteractive(sessionID, interactiveOpts(sess, settingsPath, s.trustedSkillsPrompt(), onTranscriptLine, onWorkflowEvent))
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
		turn.slashCmd = strings.HasPrefix(currentPrompt, "/")
		// Drain stale stop signals from previous turns
		for {
			select {
			case <-stopCh:
				continue
			default:
			}
			break
		}

		// Guard: verify the process is still alive before injecting the
		// prompt. The process may have died between EnsureInteractive and
		// now, and writing to a stale PTY can produce CLI noise like
		// "No response requested." instead of a clear error.
		select {
		case <-proc.Done:
			if s.retryAfterSessionIDConflict(sess, prompt, turn, broadcaster, proc, attempt, supersedeCh) {
				return
			}
			state := "waiting"
			if proc.ExitCode != 0 {
				state = "idle"
			}
			errMsg := `{"type":"system","error":true,"message":"Session process exited unexpectedly. Please send your message again."}`
			broadcaster.Send(errMsg)
			_, _ = s.store.CreateMessage(sessionID, "system", "Session process exited unexpectedly. Please send your message again.", 0)
			_ = s.store.UpdateActivityState(sessionID, state)
			broadcaster.Send(fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode))
			return
		default:
		}

		if turn.slashCmd {
			log.Printf("session %s: slash command detected, sending prompt %q", sessionID, currentPrompt)
		}

		if err := s.manager.SendPrompt(sessionID, currentPrompt); err != nil {
			// The process may have died with a session-ID conflict before
			// the prompt could be typed (PTY write fails on a dead pty).
			if s.retryAfterSessionIDConflict(sess, prompt, turn, broadcaster, proc, attempt, supersedeCh) {
				return
			}
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"message":"Failed to send prompt: %s"}`, err.Error())
			broadcaster.Send(errMsg)
			_ = s.store.UpdateActivityState(sessionID, "idle")
			broadcaster.Send(`{"type":"done","exit_code":1}`)
			return
		}

		// Confirm the prompt actually submitted (the Enter keypress can be
		// lost if the TUI wasn't ready); runs alongside waitForTurnEnd so a
		// turn that ends quickly isn't delayed by confirmation.
		turnOver := make(chan struct{})
		SafeGo("prompt-confirm:"+sessionID, func() {
			s.confirmPromptSubmission(sessionID, turn, proc.Done, turnOver, broadcaster)
		})

		reason := s.waitForTurnEnd(sessionID, stopCh, proc.Done, turn, supersedeCh)

		if turn.slashCmd {
			log.Printf("session %s: slash command waitForTurnEnd returned reason=%d", sessionID, reason)
		}

		// A new message arrived — this goroutine is stale. Exit without
		// emitting result/done; the new goroutine owns the session now.
		if reason == turnEndSuperseded {
			close(turnOver)
			log.Printf("session %s: turn superseded by new message, exiting silently", sessionID)
			return
		}

		// Slash command processed locally by the CLI — no model turn, no
		// cost, no result to synthesize. Return to the input prompt.
		if reason == turnEndLocalCommand {
			close(turnOver)
			log.Printf("session %s: local command completed, returning to prompt", sessionID)
			s.finishInteractiveTurn(sessionID, broadcaster)
			return
		}

		// The CLI rejected the prompt as an unknown slash command — no turn
		// ever started, so skip result/cost synthesis and finish cleanly.
		if reason == turnEndUnknownCommand {
			close(turnOver)
			msg := fmt.Sprintf("The CLI rejected `/%s` as an unknown command — check the skill or command name.",
				strings.TrimPrefix(turn.unknownCommand(), "/"))
			log.Printf("session %s: %s", sessionID, msg)
			_, _ = s.store.CreateMessage(sessionID, "system", msg, 0)
			evt, _ := json.Marshal(map[string]any{"type": "system", "error": true, "message": msg})
			broadcaster.Send(string(evt))
			s.finishInteractiveTurn(sessionID, broadcaster)
			return
		}

		if reason == turnEndPromptFailed {
			close(turnOver)
			log.Printf("session %s: prompt never confirmed, ending turn", sessionID)
			s.finishInteractiveTurn(sessionID, broadcaster)
			return
		}

		// AskUserQuestion detected — transition to waiting, then resume when
		// the user responds (or abort if Stop/death occurs while waiting).
		for reason == turnEndQuestion {
			log.Printf("session %s: AskUserQuestion unblocked turn loop, transitioning to waiting", sessionID)
			_ = s.store.UpdateActivityState(sessionID, "waiting")
			questionEvt, _ := json.Marshal(map[string]string{"type": "question_ready"})
			broadcaster.Send(string(questionEvt))

			respondedCh := s.pendingQuestions.WaitForClear(sessionID)

			// If the question was already answered (race), skip the wait.
			if s.pendingQuestions.Get(sessionID) == nil {
				log.Printf("session %s: pending question already cleared, resuming turn", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "working")
				reason = s.waitForTurnEnd(sessionID, stopCh, proc.Done, turn, supersedeCh)
				continue
			}

			select {
			case <-supersedeCh:
				log.Printf("session %s: superseded during question wait", sessionID)
				s.pendingQuestions.Delete(sessionID)
				reason = turnEndSuperseded
			case <-respondedCh:
				log.Printf("session %s: question answered, resuming turn", sessionID)
				_ = s.store.UpdateActivityState(sessionID, "working")
				reason = s.waitForTurnEnd(sessionID, stopCh, proc.Done, turn, supersedeCh)
			case <-stopCh:
				log.Printf("session %s: stop during question wait, ending turn", sessionID)
				s.pendingQuestions.Delete(sessionID)
				reason = turnEndStop
			case <-proc.Done:
				log.Printf("session %s: process died during question wait", sessionID)
				s.pendingQuestions.Delete(sessionID)
				reason = turnEndDied
			}
		}

		if reason == turnEndSuperseded {
			close(turnOver)
			log.Printf("session %s: turn superseded (from question wait), exiting silently", sessionID)
			return
		}

		close(turnOver)

		procDied := reason == turnEndDied
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

		if totalCost, err := s.store.SessionCostTotal(sessionID); err == nil {
			cumIn, cumOut := 0, 0
			if tv, ok := s.sessionTokenTotals.Load(sessionID); ok {
				cumIn, cumOut = tv.(*sessionTokenTotals).snapshot()
			}
			modEvt, _ := json.Marshal(map[string]any{
				"type": "mod_state", "panel_id": "tokens", "panel_title": "Tokens", "slot": "bar",
				"data": map[string]any{"input": cumIn, "output": cumOut, "cost": fmt.Sprintf("$%.2f", totalCost)},
			})
			broadcaster.Send(string(modEvt))
		}

		if newCount, err := s.store.IncrementTurnCount(sessionID); err == nil {
			turnMsg, _ := json.Marshal(map[string]any{"type": "turn_count", "turn_count": newCount})
			broadcaster.Send(string(turnMsg))
		}

		if procDied {
			if proc.ExitCode != 0 && s.retryAfterSessionIDConflict(sess, prompt, turn, broadcaster, proc, attempt, supersedeCh) {
				return
			}
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

		// Budget is informational for interactive sessions — they bill the
		// subscription and the accumulated cost is an estimate. Warn once,
		// never pause the session or block auto-continue.
		if sess.MaxBudgetUSD > 0 {
			if total, err := s.store.SessionCostTotal(sessionID); err == nil && total > sess.MaxBudgetUSD {
				if _, warned := s.budgetWarned.LoadOrStore(sessionID, true); !warned {
					log.Printf("session %s: cost estimate exceeded budget ($%.4f > $%.2f), warning only", sessionID, total, sess.MaxBudgetUSD)
					budgetEvt, _ := json.Marshal(map[string]any{"type": "budget_exceeded", "total": total, "budget": sess.MaxBudgetUSD})
					broadcaster.Send(string(budgetEvt))
					_, _ = s.store.CreateMessage(sessionID, "system",
						fmt.Sprintf("Cost estimate ($%.2f) has exceeded this session's budget ($%.2f). Interactive sessions bill your subscription, so this is informational only.", total, sess.MaxBudgetUSD), 0)
				}
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
				case <-supersedeCh:
					log.Printf("session %s: superseded during compact wait, exiting silently", sessionID)
					return
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
}

// confirmPromptSubmission waits for transcript evidence that the typed prompt
// reached Claude (user-entry echo or assistant activity). If none appears it
// re-sends Enter — the paste text is still sitting in the input box — and
// after the retries are exhausted it surfaces a visible warning instead of
// letting the session hang silently.
func (s *Server) confirmPromptSubmission(sessionID string, turn *interactiveTurnState, procDone, turnOver <-chan struct{}, broadcaster *managed.Broadcaster) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for attempt := 0; ; attempt++ {
		deadline := time.Now().Add(promptEchoTimeout)
		for time.Now().Before(deadline) {
			select {
			case <-procDone:
				return
			case <-turnOver:
				return
			case <-ticker.C:
			}
			if turn.submitted() {
				return
			}
		}
		if attempt >= promptEchoRetries {
			break
		}
		log.Printf("session %s: prompt not confirmed after %s, re-sending Enter (attempt %d/%d)",
			sessionID, promptEchoTimeout, attempt+1, promptEchoRetries)
		_ = s.manager.SendKeys(sessionID, "\r")
	}
	warn := "Prompt submission not confirmed — the Claude TUI may be blocked by a dialog. Try Interrupt, or /clear to restart the session."
	log.Printf("session %s: %s", sessionID, warn)
	_, _ = s.store.CreateMessage(sessionID, "system", warn, 0)
	evt, _ := json.Marshal(map[string]any{"type": "system", "error": true, "message": warn})
	broadcaster.Send(string(evt))
	select {
	case turn.promptFailedCh <- struct{}{}:
	default:
	}
}

// waitForTurnEnd blocks until the Stop hook fires, the process dies, an
// AskUserQuestion is detected in the transcript, or — if the turn was
// interrupted via ESC — a fallback timer expires (the Stop hook may not fire
// on interrupts).
func (s *Server) waitForTurnEnd(sessionID string, stopCh <-chan struct{}, done <-chan struct{}, turn *interactiveTurnState, supersedeCh <-chan struct{}) turnEndReason {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	turnStart := time.Now()
	var interruptedAt time.Time
	for {
		select {
		case <-supersedeCh:
			return turnEndSuperseded
		case <-stopCh:
			return turnEndStop
		case <-done:
			return turnEndDied
		case <-turn.questionCh:
			return turnEndQuestion
		case <-turn.unknownCmdCh:
			return turnEndUnknownCommand
		case <-turn.promptFailedCh:
			return turnEndPromptFailed
		case <-ticker.C:
			s.manager.TouchInteractive(sessionID)
			_, _, _, interrupted := turn.snapshot()
			if interrupted != "" && interruptedAt.IsZero() {
				interruptedAt = time.Now()
			}
			if !interruptedAt.IsZero() && time.Since(interruptedAt) > escStopFallback {
				log.Printf("session %s: no Stop hook after interrupt, assuming turn ended", sessionID)
				return turnEndStop
			}
			if turn.slashCmd && time.Since(turnStart) > localCommandFallback {
				count, _, _, _ := turn.snapshot()
				if count == 0 {
					log.Printf("session %s: slash command completed locally (no assistant activity after %s)", sessionID, localCommandFallback)
					return turnEndLocalCommand
				}
			}
		}
	}
}

// registerPendingQuestion stores a detected AskUserQuestion, broadcasts the
// question card to the UI, and unblocks the turn loop. Duplicate detections
// of the same tool_use_id (PreToolUse hook + transcript flush at resolution)
// are dropped by the manager.
func (s *Server) registerPendingQuestion(sessionID string, pq *PendingQuestion, source string) {
	if !s.pendingQuestions.Set(sessionID, pq) {
		return
	}
	log.Printf("session %s: AskUserQuestion detected via %s, storing pending question (tool_use_id=%s)", sessionID, source, pq.ToolUseID)
	if qData, err := json.Marshal(map[string]interface{}{
		"type":        "pending_question",
		"pending":     true,
		"tool_use_id": pq.ToolUseID,
		"questions":   pq.Questions,
		"created_at":  pq.CreatedAt,
	}); err == nil {
		s.manager.GetBroadcaster(sessionID).Send(string(qData))
	}
	if v, ok := s.interactiveTurns.Load(sessionID); ok {
		t := v.(*interactiveTurnState)
		// A question proves the prompt reached Claude even when the CLI
		// hasn't flushed the transcript yet — without this, the prompt
		// confirmer re-sends Enter and answers the dialog's default option.
		t.markPromptEchoed()
		select {
		case t.questionCh <- struct{}{}:
		default:
		}
	}
}

// hasToolResultFor reports whether a user entry's content contains a
// tool_result block for the given tool_use_id.
func hasToolResultFor(content json.RawMessage, toolUseID string) bool {
	if toolUseID == "" {
		return false
	}
	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID == toolUseID {
			return true
		}
	}
	return false
}

type pendingWorktreeOp struct {
	SessionID string
	ToolName  string // "EnterWorktree" or "ExitWorktree"
}

// detectWorktreeToolUse scans an assistant transcript entry for
// EnterWorktree/ExitWorktree tool_use blocks and records their IDs so
// the matching tool_result can update the session CWD.
func (s *Server) detectWorktreeToolUse(line, sessionID string) {
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil || msg.Message.Content == nil {
		return
	}
	var blocks []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(msg.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.ID == "" {
			continue
		}
		if b.Name == "EnterWorktree" || b.Name == "ExitWorktree" {
			s.pendingWorktreeEnters.Store(b.ID, pendingWorktreeOp{
				SessionID: sessionID,
				ToolName:  b.Name,
			})
		}
	}
}

// processWorktreeResults checks user transcript entries for tool_results
// that match pending EnterWorktree/ExitWorktree calls. On a match it
// extracts the worktree path, updates the session's worktree_cwd, and
// emits a cwd_change SSE event so the frontend refreshes its file tree.
func (s *Server) processWorktreeResults(content json.RawMessage, sessionID string, broadcaster *managed.Broadcaster) {
	var blocks []struct {
		Type      string          `json:"type"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		val, ok := s.pendingWorktreeEnters.LoadAndDelete(b.ToolUseID)
		if !ok {
			continue
		}
		op := val.(pendingWorktreeOp)
		if op.SessionID != sessionID {
			continue
		}

		if op.ToolName == "ExitWorktree" {
			if err := s.store.ClearWorktreeCWD(sessionID); err != nil {
				log.Printf("session %s: failed to clear worktree_cwd: %v", sessionID, err)
				continue
			}
			log.Printf("session %s: exited worktree", sessionID)
			sess, err := s.store.GetSessionByID(sessionID)
			if err == nil {
				cwdMsg := fmt.Sprintf(`{"type":"cwd_change","cwd":%s}`, jsonString(sess.CWD))
				broadcaster.Send(cwdMsg)
			}
			continue
		}

		// EnterWorktree — extract path from tool_result content
		wtPath := extractPathFromToolResult(b.Content)
		if wtPath == "" {
			log.Printf("session %s: EnterWorktree result contained no extractable path", sessionID)
			continue
		}
		if err := s.store.UpdateWorktreeCWD(sessionID, wtPath); err != nil {
			log.Printf("session %s: failed to update worktree_cwd: %v", sessionID, err)
			continue
		}
		log.Printf("session %s: entered worktree at %s", sessionID, wtPath)
		cwdMsg := fmt.Sprintf(`{"type":"cwd_change","cwd":%s}`, jsonString(wtPath))
		broadcaster.Send(cwdMsg)
	}
}

// extractPathFromToolResult tries to find an absolute directory path in a
// tool_result content field. The content may be a JSON string, an array
// of text blocks, or a nested content wrapper.
func extractPathFromToolResult(raw json.RawMessage) string {
	// Try as a plain string
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if p := findWorktreePath(text); p != "" {
			return p
		}
	}
	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" || b.Text != "" {
				if p := findWorktreePath(b.Text); p != "" {
					return p
				}
			}
		}
	}
	return ""
}

var absPathRe = regexp.MustCompile(`(/[^\s"',;:]+)`)

// findWorktreePath extracts the first absolute filesystem path from text
// that exists as a directory.
func findWorktreePath(text string) string {
	for _, m := range absPathRe.FindAllString(text, 10) {
		m = strings.TrimRight(m, ".!?)")
		if filepath.IsAbs(m) {
			if info, err := os.Stat(m); err == nil && info.IsDir() {
				return m
			}
		}
	}
	return ""
}

func (s *Server) finishInteractiveTurn(sessionID string, broadcaster *managed.Broadcaster) {
	s.pendingQuestions.Delete(sessionID)
	_ = s.store.UpdateActivityState(sessionID, "waiting")
	broadcaster.Send(`{"type":"done","exit_code":0}`)
}
