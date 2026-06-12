# Interactive Managed Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite managed mode to drive a long-lived **interactive** Claude Code process per session (PTY + hooks + transcript tailing) instead of `claude -p`, so usage bills against the Claude Code subscription (issue #174).

**Architecture:** One interactive `claude` process per managed session, spawned under a PTY (`creack/pty`). Prompts are injected via bracketed-paste writes to the PTY. Structured output comes from tailing Claude Code's native transcript JSONL in `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl` (same shapes as stream-json assistant events, so the existing SSE pipeline and frontends keep working). Turn lifecycle (working → waiting) is driven by per-session Stop/SessionStart/Notification hooks injected via a generated `--settings` file; the hook command is a new `claude-controller hook-signal` subcommand that POSTs back to the server. The legacy `claude -p` path stays intact behind `MANAGED_MODE=print` for one release (rollback), and is the forced mode on Windows (no ConPTY in v1).

**Tech Stack:** Go 1.26, `github.com/creack/pty`, SQLite, Alpine.js web UI.

---

## Design notes (read first)

### Why hooks, not terminal output
PTY output is ANSI TUI rendering — unparseable. We **discard** PTY output (small ring buffer kept for error reporting) and use:
- **SessionStart hook** → tells the server the real `session_id` + `transcript_path` (critical: interactive `--resume` forks to a *new* session ID/file).
- **Stop hook** → turn completed → synthesize `result` + `done` SSE events, set `activity_state=waiting`.
- **Notification hook** → e.g. permission prompts → broadcast SSE `notification` event.

### Event-shape preservation
Transcript JSONL lines are `{"type":"assistant","message":{...},"timestamp":...,"isSidechain":...}` — same shape the web UI/iOS already parse from stream-json. We forward raw lines (tolerant passthrough per issue risk note), filtering:
- `isSidechain: true` → skip
- `type:"user"` with plain-text content (echo of our own prompt) → skip; tool_result-bearing user entries forward as-is
- entries with `timestamp` older than process spawn → skip (avoids replaying history on resume-fork)
- types other than user/assistant → skip broadcast

At turn end the server synthesizes `{"type":"result","subtype":"success","usage":{...},"cost":...,"model":...}` and `{"type":"done","exit_code":0}` so existing frontend done/cost handling is untouched.

### Feature-parity mapping (issue task 5)
| `-p` feature | Interactive replacement |
|---|---|
| `--allowedTools` | `permissions.allow` in generated per-session settings file |
| `--max-turns` + auto-continue | Server counts assistant transcript entries per turn; ≥ MaxTurns → write ESC to PTY → treat as `error_max_turns` → existing auto-continue logic |
| `--max-budget-usd` | Server sums per-turn cost (from transcript `message.usage`) into existing `cost` messages; total > budget → ESC + `budget_exceeded` event + refuse new sends |
| `--permission-prompt-tool` (MCP bridge) | **GAP (documented):** interactive mode cannot route permission prompts to the web UI. Mitigation: allowlisted tools never prompt; Notification hook surfaces the prompt text in the UI. |
| stdin stream-json images | **GAP (documented):** images are saved to disk (existing upload flow) and referenced by path in the prompt text ("Read the image at <path>"). |
| SIGINT interrupt | ESC keystroke to PTY (graceful turn interrupt; process survives) |
| `/compact` via stdin turn | type `/compact\r` into the PTY; wait for Stop hook or timeout |

### Process/state model
- `Manager` gets a second map `iprocs map[string]*InteractiveProc`. Existing `procs` map keeps shell one-shots (and legacy print mode). `IsRunning` keeps its current meaning (shell guard unaffected — the long-lived interactive proc must NOT block shell commands).
- Per-session hook routing lives in the Manager: `SignalSessionStart` (starts transcript tailer), `SignalStop` (forwards to per-session `stopCh`), notifications handled at the API layer.
- Settings/MCP temp files live under `~/.claude-controller/sessions/<id>/`.
- Reaper: interactive procs idle > timeout get graceful shutdown (`/exit` typed, then kill).

---

### Task 1: Dependency + transcript tailer

**Files:**
- Modify: `server/go.mod` (via `go get`)
- Create: `server/managed/transcript.go`
- Test: `server/managed/transcript_test.go`

- [ ] **Step 1:** `cd server && go get github.com/creack/pty@v1.1.24`
- [ ] **Step 2: Write failing tests**

```go
// transcript_test.go
package managed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func collectLines(t *testing.T, path string, offset int64, dur time.Duration) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		TailTranscript(ctx, path, offset, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		})
		close(done)
	}()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return got
}

func TestTailTranscriptReadsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	os.WriteFile(path, []byte("{\"a\":1}\n"), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("{\"b\":2}\n")
		f.Close()
	}()
	got := collectLines(t, path, 0, 700*time.Millisecond)
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != `{"b":2}` {
		t.Fatalf("got %v", got)
	}
}

func TestTailTranscriptWaitsForFileToExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "later.jsonl")
	go func() {
		time.Sleep(150 * time.Millisecond)
		os.WriteFile(path, []byte("{\"x\":1}\n"), 0644)
	}()
	got := collectLines(t, path, 0, 700*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %v", got)
	}
}

func TestTailTranscriptRespectsOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := "{\"old\":1}\n"
	os.WriteFile(path, []byte(content), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("{\"new\":2}\n")
		f.Close()
	}()
	got := collectLines(t, path, int64(len(content)), 600*time.Millisecond)
	if len(got) != 1 || got[0] != `{"new":2}` {
		t.Fatalf("got %v", got)
	}
}

func TestTailTranscriptHandlesPartialLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	os.WriteFile(path, []byte(`{"par`), 0644)
	go func() {
		time.Sleep(150 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("tial\":1}\n")
		f.Close()
	}()
	got := collectLines(t, path, 0, 700*time.Millisecond)
	if len(got) != 1 || got[0] != `{"partial":1}` {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 3:** Run `go test ./managed/ -run TestTailTranscript -v` — expect FAIL (undefined: TailTranscript)
- [ ] **Step 4: Implement**

```go
// transcript.go
package managed

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"
)

// TranscriptPollInterval controls tail polling frequency. Overridden in tests.
var TranscriptPollInterval = 200 * time.Millisecond

// TailTranscript polls path for appended JSONL lines starting at offset and
// calls emit for each complete line (without trailing newline). Tolerates the
// file not existing yet, and resets to the start if the file shrinks
// (truncate/recreate). Returns when ctx is done.
func TailTranscript(ctx context.Context, path string, offset int64, emit func(string)) {
	var partial bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(TranscriptPollInterval):
		}

		fi, err := os.Stat(path)
		if err != nil {
			continue // file doesn't exist yet
		}
		if fi.Size() < offset {
			offset = 0 // truncated/recreated
			partial.Reset()
		}
		if fi.Size() == offset {
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		offset += int64(len(data))
		partial.Write(data)

		for {
			idx := bytes.IndexByte(partial.Bytes(), '\n')
			if idx < 0 {
				break
			}
			line := string(partial.Next(idx + 1)[:idx])
			if line != "" {
				emit(line)
			}
		}
	}
}
```

- [ ] **Step 5:** Run tests → PASS. (Set `TranscriptPollInterval = 20ms` in tests via TestMain or per-test if flaky.)
- [ ] **Step 6:** Commit: `feat(managed): transcript JSONL tailer for interactive sessions`

---

### Task 2: Per-session settings file (hooks + permissions parity)

**Files:**
- Create: `server/managed/settings.go`
- Test: `server/managed/settings_test.go`

- [ ] **Step 1: Failing tests** — `WriteSessionSettings(dir, binaryPath, sessionID string, port int, allowedToolsJSON string) (string, error)` writes `<dir>/settings.json` containing SessionStart/Stop/Notification hooks invoking `<binaryPath> hook-signal --event <ev> --session-id <id> --port <port>` and `permissions.allow` parsed from the session's allowed_tools JSON array. Assert JSON unmarshals, hook commands contain quoted binary path and event names, allow list matches.
- [ ] **Step 2: Implement**

```go
// settings.go
package managed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}
type hookMatcher struct {
	Hooks []hookCmd `json:"hooks"`
}

// WriteSessionSettings generates a Claude Code settings file for a managed
// interactive session: turn-lifecycle hooks pointing back at the controller
// server, plus permission allow rules mapped from the session's allowed tools.
func WriteSessionSettings(dir, binaryPath, sessionID string, port int, allowedToolsJSON string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	mk := func(event string) []hookMatcher {
		cmd := fmt.Sprintf("%q hook-signal --event %s --session-id %s --port %d", binaryPath, event, sessionID, port)
		return []hookMatcher{{Hooks: []hookCmd{{Type: "command", Command: cmd}}}}
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": mk("session_start"),
			"Stop":         mk("stop"),
			"Notification": mk("notification"),
		},
	}
	var tools []string
	if allowedToolsJSON != "" {
		_ = json.Unmarshal([]byte(allowedToolsJSON), &tools)
	}
	if len(tools) > 0 {
		settings["permissions"] = map[string]any{"allow": tools}
	}
	path := filepath.Join(dir, "settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

// SessionDir returns the per-session scratch directory for generated files.
func SessionDir(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "claude-controller", sessionID)
	}
	return filepath.Join(home, ".claude-controller", "sessions", sessionID)
}
```

- [ ] **Step 3:** Tests pass; commit `feat(managed): per-session settings generator (hooks + permission parity)`

---

### Task 3: Interactive PTY process lifecycle

**Files:**
- Create: `server/managed/pty_unix.go` (`//go:build !windows`) — `func startPTY(cmd *exec.Cmd) (*os.File, error)` using `pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 200})`
- Create: `server/managed/pty_windows.go` (`//go:build windows`) — returns `ErrInteractiveUnsupported`
- Create: `server/managed/interactive.go`
- Test: `server/managed/interactive_test.go`

**Key API:**

```go
var ErrInteractiveUnsupported = errors.New("interactive managed mode is not supported on this platform; set MANAGED_MODE=print")

type InteractiveOpts struct {
	Args             []string // full claude args (no -p)
	CWD              string
	OnTranscriptLine func(line string) // called for each raw transcript line
}

type InteractiveProc struct {
	Cmd          *exec.Cmd
	PTY          *os.File
	Done         chan struct{}
	ExitCode     int
	LastActivity time.Time
	SpawnedAt    time.Time

	mu             sync.Mutex
	transcriptPath string
	tailCancel     context.CancelFunc
	stopCh         chan struct{} // buffered(4); signaled on Stop hook
	lastOutput     *ringBuffer   // last ~8KB of PTY output for error reporting
}
```

Manager additions (all on `*Manager`, `iprocs map[string]*InteractiveProc` guarded by `m.mu`, initialized in `NewManager`):
- `EnsureInteractive(sessionID string, opts InteractiveOpts) (*InteractiveProc, error)` — returns existing or spawns: `exec.Command(cfg.ClaudeBin, append(cfg.ClaudeArgs, opts.Args...)...)`, `cmd.Dir = opts.CWD`, env = `os.Environ()` + `cfg.ClaudeEnv` + `CLAUDE_CONTROLLER_MANAGED=1` + `TERM=xterm-256color`; `startPTY(cmd)`; goroutine drains PTY into ring buffer; goroutine `cmd.Wait()` → set ExitCode, cancel tailer, remove from `iprocs`, `close(Done)`.
- `IsInteractiveRunning(sessionID string) bool`
- `SendPrompt(sessionID, text string) error` — bracketed paste: write `"\x1b[200~" + text + "\x1b[201~"` then `"\r"` (two writes, 50ms apart, so the TUI registers paste before submit). Updates LastActivity.
- `SendKeys(sessionID, seq string) error` — raw PTY write (ESC = `"\x1b"`).
- `InterruptInteractive(sessionID string) error` — `SendKeys(sessionID, "\x1b")`.
- `SetTranscript(sessionID, path string) ` — if no tailer yet and proc alive: record path, `offset = current file size`, start `TailTranscript` goroutine with ctx cancelled on proc death; emit filter: parse `{"timestamp":...}` and drop entries with timestamp < SpawnedAt−5s, then call `opts.OnTranscriptLine`.
- `SignalStop(sessionID string)` — non-blocking send on `stopCh`.
- `StopEvents(sessionID string) <-chan struct{}` — returns proc's stopCh (nil-safe).
- `ShutdownInteractive(sessionID string, timeout time.Duration) error` — type `/exit\r`; wait Done or timeout → `cmd.Process.Kill()`.
- Wire into `ShutdownAll` and `ReapIdle` (interactive procs idle > maxIdle → `ShutdownInteractive(id, 10*time.Second)`).
- `Teardown` also tears down interactive procs (used by /clear and /resume).

**Tests** (use `/bin/cat` or a stub shell script as fake claude — PTY echo loopback):
- `TestEnsureInteractiveSpawnsOnce` — two calls return same proc.
- `TestSendPromptWritesBracketedPaste` — spawn `cat` under PTY, SendPrompt("hello"), read PTY master, assert output contains `\x1b[200~hello\x1b[201~`.
- `TestSignalStopDelivers` — SignalStop then receive on StopEvents.
- `TestSetTranscriptStartsTailer` — temp JSONL file, SetTranscript, append line with current timestamp, assert OnTranscriptLine receives it; append line with old timestamp, assert filtered.
- `TestShutdownInteractiveKillsAfterTimeout` — spawn `sleep 60` stub, shutdown with 100ms timeout, assert Done closes.

- [ ] Write failing tests → implement → `go test ./managed/ -v` → commit `feat(managed): interactive PTY process lifecycle`

---

### Task 4: `hook-signal` subcommand

**Files:**
- Create: `server/hooksignal/hooksignal.go`
- Test: `server/hooksignal/hooksignal_test.go`
- Modify: `server/main.go` (subcommand dispatch, next to `mcp-bridge`)

Behavior: read hook JSON from stdin (`{"hook_event_name":..., "session_id":..., "transcript_path":..., "message":...}`), POST to `http://localhost:<port>/api/sessions/<managed-id>/hook-event` with body `{"event":<flag>,"claude_session_id":...,"transcript_path":...,"message":...}` and `Authorization: Bearer <key>` where key is read from `~/.claude-controller/api.key` (flag `--key-file` overrides; env `CLAUDE_CONTROLLER_KEY_FILE` also honored). **Always exit 0** and print nothing on stdout errors — a broken hook must never block Claude. 3s HTTP timeout.

```go
package hooksignal

func Run(event, sessionID string, port int, keyFile string, stdin io.Reader) error
```

main.go dispatch:

```go
if len(os.Args) >= 2 && os.Args[1] == "hook-signal" {
	fs := flag.NewFlagSet("hook-signal", flag.ExitOnError)
	event := fs.String("event", "", "hook event name")
	sessionID := fs.String("session-id", "", "managed session ID")
	port := fs.Int("port", 8080, "server port")
	keyFile := fs.String("key-file", "", "path to api.key (default ~/.claude-controller/api.key)")
	fs.Parse(os.Args[2:])
	hooksignal.Run(*event, *sessionID, *port, *keyFile, os.Stdin) // errors intentionally ignored
	return
}
```

Tests: httptest server asserting method/path/auth-header/body; missing key file → no panic, returns error; malformed stdin → still POSTs with empty fields.

- [ ] TDD cycle → commit `feat: hook-signal subcommand relays Claude hooks to the server`

---

### Task 5: hook-event API endpoint

**Files:**
- Create: `server/api/hook_event.go`
- Test: `server/api/hook_event_test.go`
- Modify: `server/api/router.go` (+1 route), `server/api/interfaces.go`, `server/api/mock_manager_test.go`

Route: `apiMux.HandleFunc("POST /api/sessions/{id}/hook-event", s.handleHookEvent)` (authed — hook-signal sends the bearer key).

```go
func (s *Server) handleHookEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req struct {
		Event           string `json:"event"`
		ClaudeSessionID string `json:"claude_session_id"`
		TranscriptPath  string `json:"transcript_path"`
		Message         string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil || sess.Mode != "managed" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	switch req.Event {
	case "session_start":
		if req.ClaudeSessionID != "" && req.ClaudeSessionID != sess.ClaudeSessionID {
			_ = s.store.UpdateClaudeSessionID(sessionID, req.ClaudeSessionID)
		}
		if req.TranscriptPath != "" {
			s.manager.SetTranscript(sessionID, req.TranscriptPath)
		}
	case "stop":
		s.manager.SignalStop(sessionID)
	case "notification":
		evt, _ := json.Marshal(map[string]string{"type": "notification", "message": req.Message})
		s.manager.GetBroadcaster(sessionID).Send(string(evt))
		if req.Message != "" {
			_, _ = s.store.CreateMessage(sessionID, "system", req.Message, 0)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}
```

Interface additions to `SessionManager`: `EnsureInteractive`, `IsInteractiveRunning`, `SendPrompt`, `SendKeys`, `InterruptInteractive`, `SetTranscript`, `SignalStop`, `StopEvents`, `ShutdownInteractive`. Add no-op/recording impls to the mock.

DB: add `UpdateClaudeSessionID(id, claudeID string) error` to `server/db/sessions.go` (simple UPDATE) + test. Also add `SessionCostTotal(sessionID string) (float64, error)` in `server/db/messages.go` (`SELECT COALESCE(SUM(cost),0) FROM messages WHERE session_id = ? AND role = 'cost'`) + test — used by budget enforcement.

- [ ] TDD cycle → commit `feat(api): hook-event endpoint drives interactive turn lifecycle`

---

### Task 6: Interactive send-message orchestrator + mode dispatch

**Files:**
- Create: `server/api/managed_interactive.go`
- Test: `server/api/managed_interactive_test.go`
- Modify: `server/api/managed_sessions.go` (`handleSendMessage` dispatch + `handleInterrupt`)

**Dispatch:** at the top of the existing goroutine setup in `handleSendMessage`, after validation/persistence of the user message:

```go
if s.manager.Config().Mode == "interactive" {
	s.sendMessageInteractive(sess, req.Message, req.Model, imagePaths)
	// (writes the same {"status":"started"} response)
	return
}
// ... existing print-mode flow unchanged ...
```

(For images: collect `imagePaths` — absolute paths of uploaded files — instead of base64; interactive prompt appends `\n\n[Attached image: <path>] — use the Read tool to view it.` per image.)

**`sendMessageInteractive` flow (async goroutine, mirrors print-mode structure):**

1. Stale-state guard already handled by caller. Set `activity_state=working`, broadcast `model_selected` (model only applied at spawn; same semantics as print mode warm process).
2. Build args: `--session-id <id>` (new) or `--resume <claude-session-id>` (initialized), `--model`, `--settings <WriteSessionSettings(SessionDir(id), cfg.BinaryPath, id, cfg.ServerPort, sess.AllowedTools)>`.
3. `OnTranscriptLine` closure (registered once at spawn): tolerant parse; skip sidechain/plain-text-user/non-chat types; forward raw line via broadcaster; persist via existing `extractAssistantText`/`extractToolNames`/`extractSessionFiles`; accumulate per-turn `assistantCount` and `usage` (input/output tokens from `message.usage`) under a mutex; if `assistantCount >= sess.MaxTurns` → `InterruptInteractive` once + set `hitMaxTurns`.
4. `EnsureInteractive` → on spawn the SessionStart hook will call SetTranscript (tailer starts). Fallback: if no transcript after 30s, compute the path locally from `claudeProjectsDir(sess.CWD)` + resumeID and call SetTranscript ourselves.
5. `SendPrompt(sessionID, message)`.
6. `select` on: `StopEvents` (turn done), `proc.Done` (crash → state idle, broadcast stderr ring buffer + done), and — after an ESC interrupt — a 10s fallback timer (Stop hook may not fire on interrupt).
7. On turn end: increment turn count (existing store call + SSE `turn_count`), compute cost via `calcCost(model, usage)` → persist `cost` message → broadcast synthesized enriched `result` then `done`. Budget check: `SessionCostTotal > sess.MaxBudgetUSD` → broadcast `{"type":"budget_exceeded",...}` + system message + state `waiting` + return.
8. Auto-continue: same rules as print mode (`hitMaxTurns` required; progress guard `assistantCount >= 2`; `MaxContinuations`; `CompactEveryNContinues` → `SendPrompt("/compact")` + wait stop/5-min timeout + `compacting`/`compact_complete` events). Continuation message identical. Process stays alive between turns; state `waiting` whenever we return without auto-continuing.

**`handleInterrupt`:** if `s.manager.IsInteractiveRunning(id)` → `InterruptInteractive(id)`; else existing `Interrupt(id)`.

**Tests** (mock manager): stop signal → state waiting + result/done broadcast; budget exceeded → budget_exceeded event; max-turns transcript lines → InterruptInteractive called + auto-continue SendPrompt; interrupt endpoint routes to InterruptInteractive when interactive proc running.

- [ ] TDD cycle → commit `feat(api): interactive-mode send orchestrator with hook-driven turns`

---

### Task 7: Config flag, main.go, Windows fallback

**Files:**
- Modify: `server/managed/manager.go` (`Config.Mode string`)
- Modify: `server/main.go`

```go
managedCfg := managed.Config{
	...,
	Mode: managedMode(), // helper below
}

func managedMode() string {
	mode := envOrDefault("MANAGED_MODE", "interactive")
	if runtime.GOOS == "windows" && mode == "interactive" {
		log.Printf("interactive managed mode is not supported on Windows yet; falling back to print mode")
		return "print"
	}
	if mode != "interactive" && mode != "print" {
		log.Printf("unknown MANAGED_MODE %q, defaulting to interactive", mode)
		return "interactive"
	}
	return mode
}
```

- [ ] Build for darwin and windows (`GOOS=windows go build ./...`) → commit `feat: MANAGED_MODE flag (interactive default, print rollback path)`

---

### Task 8: Web UI events

**Files:**
- Modify: `server/web/static/app.js` (SSE handler, near the `auto_continue_exhausted` block ~line 2106)

```js
if (data.type === 'notification') {
  this.appendSystemMessage(data.message || 'Claude sent a notification');
  return;
}
if (data.type === 'budget_exceeded') {
  this.appendSystemMessage(`Budget limit reached ($${(data.budget || 0).toFixed(2)}). Session paused.`);
  this.isWorking = false;
  return;
}
```

(Match the file's actual local helper names — use whatever pattern the adjacent `auto_continue_exhausted` handler uses for system messages.)

- [ ] Manual check via `go run .` + browser if feasible; commit `feat(web): surface notification and budget_exceeded events`

---

### Task 9: Docs + finalization

**Files:**
- Modify: `CLAUDE.md` (Managed Mode section: interactive architecture, MANAGED_MODE flag, parity gaps)
- Modify: `docs/superpowers/specs/2026-03-19-managed-sessions-design.md` (addendum note pointing at this plan)

- [ ] `cd server && go test ./... ` — all green
- [ ] `go vet ./...` and `GOOS=windows go build ./...`
- [ ] Push branch, open **draft** PR: `Closes #174`, summary of architecture, parity-gap table, rollback instructions (`MANAGED_MODE=print`)

---

## Self-review checklist
- Spec coverage: issue tasks 2 (PTY lifecycle ✓ Task 3), 3 (transcript source ✓ Task 1), 4 (hook lifecycle ✓ Tasks 2/4/5), 5 (parity ✓ design table + Tasks 2/6), 6 (/compact + interrupt ✓ Task 6), 7 (frontend ✓ Task 8, iOS unaffected by shape preservation — verified event shapes), 8 (tests/docs ✓ throughout + Task 9), rollback flag (✓ Task 7). Spike (task 1 of issue) is partially covered: billing cannot be validated programmatically — noted as PR caveat; PTY drivability is validated by Task 3's tests.
- No placeholders: all code steps include concrete code or exact instructions referencing existing identifiers.
- Type consistency: `InteractiveProc`, `EnsureInteractive`, `SendPrompt`, `SendKeys`, `InterruptInteractive`, `SetTranscript`, `SignalStop`, `StopEvents`, `ShutdownInteractive`, `Config.Mode` used consistently across Tasks 3, 5, 6, 7.
