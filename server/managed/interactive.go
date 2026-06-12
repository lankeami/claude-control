package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var ErrInteractiveUnsupported = errors.New("interactive managed mode is not supported on this platform; set MANAGED_MODE=print")

// InteractiveOpts configures a long-lived interactive Claude Code process.
type InteractiveOpts struct {
	Args []string // claude CLI args (no -p)
	CWD  string
	// OnTranscriptLine is called for each raw transcript JSONL line appended
	// after the process spawned (older entries are filtered by timestamp).
	OnTranscriptLine func(line string)
}

// InteractiveProc is a persistent interactive Claude Code process driven via PTY.
type InteractiveProc struct {
	Cmd          *exec.Cmd
	PTY          *os.File
	Done         chan struct{}
	ExitCode     int
	SpawnedAt    time.Time
	LastActivity time.Time

	opts InteractiveOpts

	mu            sync.Mutex
	tailStarted   bool
	tailCancel    context.CancelFunc
	stopCh        chan struct{}
	lastOutput    []byte
	outTotal      int64
	lastOutputAt  time.Time
	readyDone     bool
	trustAnswered bool
}

const ptyRingSize = 8 * 1024

// Readiness tuning for the first prompt after spawn. The TUI takes a few
// seconds to boot; typing into it earlier loses the prompt. Variables so
// tests can shorten them.
var (
	interactiveReadyQuiescence = 600 * time.Millisecond
	interactiveReadyTimeout    = 15 * time.Second
	interactiveReadyPoll       = 25 * time.Millisecond
)

// LastOutput returns the most recent raw PTY output (up to 8KB), useful for
// error reporting when the process dies unexpectedly.
func (p *InteractiveProc) LastOutput() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.lastOutput)
}

func (p *InteractiveProc) appendOutput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastOutput = append(p.lastOutput, data...)
	if len(p.lastOutput) > ptyRingSize {
		p.lastOutput = p.lastOutput[len(p.lastOutput)-ptyRingSize:]
	}
	p.outTotal += int64(len(data))
	p.lastOutputAt = time.Now()
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?>]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][0-9A-Z]|\x1b.`)

// compactTerminalText strips ANSI escape sequences and all whitespace so
// dialog text can be matched even when the TUI interleaves styling or cursor
// movement mid-phrase.
func compactTerminalText(s string) string {
	s = ansiSeq.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func containsTrustDialog(out string) bool {
	c := compactTerminalText(out)
	return strings.Contains(c, "trustthisfolder") || strings.Contains(c, "Doyoutrustthefiles")
}

// waitReady blocks until the TUI looks ready for input: the process has
// produced output and then gone quiet. Auto-accepts the folder trust dialog
// (option 1 is preselected, Enter confirms). Gives up at
// interactiveReadyTimeout and proceeds best-effort. Sticky per process —
// only the first prompt after spawn pays this wait.
func (p *InteractiveProc) waitReady(sessionID string) {
	p.mu.Lock()
	if p.readyDone {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	deadline := time.Now().Add(interactiveReadyTimeout)
	for {
		select {
		case <-p.Done:
			return
		default:
		}

		p.mu.Lock()
		total, last := p.outTotal, p.lastOutputAt
		ring := string(p.lastOutput)
		answered := p.trustAnswered
		p.mu.Unlock()

		if !answered && containsTrustDialog(ring) {
			log.Printf("session %s: trust dialog detected, auto-accepting", sessionID)
			p.PTY.Write([]byte("\r"))
			p.mu.Lock()
			p.trustAnswered = true
			p.mu.Unlock()
			time.Sleep(interactiveReadyPoll)
			continue
		}
		if total > 0 && time.Since(last) >= interactiveReadyQuiescence {
			break
		}
		if time.Now().After(deadline) {
			log.Printf("session %s: TUI not quiescent after %s, sending prompt anyway", sessionID, interactiveReadyTimeout)
			break
		}
		time.Sleep(interactiveReadyPoll)
	}
	p.mu.Lock()
	p.readyDone = true
	p.mu.Unlock()
}

// EnsureInteractive returns the session's running interactive process, or
// spawns a new one under a PTY.
func (m *Manager) EnsureInteractive(sessionID string, opts InteractiveOpts) (*InteractiveProc, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	if proc, ok := m.iprocs[sessionID]; ok {
		proc.LastActivity = time.Now()
		m.mu.Unlock()
		return proc, nil
	}
	cfg := m.cfg
	m.mu.Unlock()

	args := append(append([]string{}, cfg.ClaudeArgs...), opts.Args...)
	cmd := exec.Command(cfg.ClaudeBin, args...)
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), cfg.ClaudeEnv...)
	cmd.Env = append(cmd.Env, "CLAUDE_CONTROLLER_MANAGED=1", "TERM=xterm-256color")

	ptmx, err := startPTY(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	now := time.Now()
	proc := &InteractiveProc{
		Cmd:          cmd,
		PTY:          ptmx,
		Done:         make(chan struct{}),
		SpawnedAt:    now,
		LastActivity: now,
		opts:         opts,
		stopCh:       make(chan struct{}, 4),
	}

	m.mu.Lock()
	m.iprocs[sessionID] = proc
	m.mu.Unlock()

	// Drain PTY output continuously — the child blocks if the master fills up.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				proc.appendOutput(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		proc.mu.Lock()
		if proc.tailCancel != nil {
			proc.tailCancel()
		}
		proc.mu.Unlock()
		ptmx.Close()
		m.mu.Lock()
		if m.iprocs[sessionID] == proc {
			delete(m.iprocs, sessionID)
		}
		m.mu.Unlock()
		close(proc.Done)
	}()

	return proc, nil
}

// TouchInteractive marks the session's interactive process as recently active
// so the idle reaper doesn't kill it while a turn is in flight.
func (m *Manager) TouchInteractive(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if proc, ok := m.iprocs[sessionID]; ok {
		proc.LastActivity = time.Now()
	}
}

func (m *Manager) getInteractive(sessionID string) *InteractiveProc {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.iprocs[sessionID]
}

func (m *Manager) IsInteractiveRunning(sessionID string) bool {
	return m.getInteractive(sessionID) != nil
}

// SendPrompt types a prompt into the interactive session using bracketed
// paste (so multi-line text isn't interpreted as separate submissions),
// followed by Enter.
func (m *Manager) SendPrompt(sessionID, text string) error {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return fmt.Errorf("no interactive process for session %s", sessionID)
	}
	proc.LastActivity = time.Now()
	proc.waitReady(sessionID)
	if _, err := proc.PTY.Write([]byte("\x1b[200~" + text + "\x1b[201~")); err != nil {
		return err
	}
	// Small delay so the TUI registers the paste before the submit keypress.
	time.Sleep(50 * time.Millisecond)
	_, err := proc.PTY.Write([]byte("\r"))
	return err
}

// SendKeys writes a raw key sequence to the session's PTY.
func (m *Manager) SendKeys(sessionID, seq string) error {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return fmt.Errorf("no interactive process for session %s", sessionID)
	}
	_, err := proc.PTY.Write([]byte(seq))
	return err
}

// InterruptInteractive sends ESC, which gracefully interrupts the current
// turn in interactive Claude Code without killing the process.
func (m *Manager) InterruptInteractive(sessionID string) error {
	return m.SendKeys(sessionID, "\x1b")
}

// SignalStop records a Stop-hook event for the session (non-blocking).
func (m *Manager) SignalStop(sessionID string) {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return
	}
	select {
	case proc.stopCh <- struct{}{}:
	default:
	}
}

// StopEvents returns the channel signaled on each Stop hook, or nil if the
// session has no interactive process.
func (m *Manager) StopEvents(sessionID string) <-chan struct{} {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return nil
	}
	return proc.stopCh
}

// SetTranscript starts tailing the given transcript JSONL (idempotent —
// subsequent calls are no-ops). Called when the SessionStart hook reports the
// real transcript path. Entries timestamped before the process spawned are
// filtered so resumed sessions don't replay history.
func (m *Manager) SetTranscript(sessionID, path string) {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return
	}
	proc.mu.Lock()
	if proc.tailStarted {
		proc.mu.Unlock()
		return
	}
	proc.tailStarted = true
	ctx, cancel := context.WithCancel(context.Background())
	proc.tailCancel = cancel
	proc.mu.Unlock()

	var offset int64
	if fi, err := os.Stat(path); err == nil {
		offset = fi.Size()
	}
	cutoff := proc.SpawnedAt.Add(-5 * time.Second)
	emit := func(line string) {
		var meta struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(line), &meta) == nil && meta.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, meta.Timestamp); err == nil && ts.Before(cutoff) {
				return
			}
		}
		if proc.opts.OnTranscriptLine != nil {
			proc.opts.OnTranscriptLine(line)
		}
	}
	go TailTranscript(ctx, path, offset, emit)
	log.Printf("session %s: tailing transcript %s from offset %d", sessionID, path, offset)
}

// ShutdownInteractive ends the interactive session gracefully by typing
// /exit, then kills the process if it doesn't exit within timeout.
// No-op if no interactive process is running.
func (m *Manager) ShutdownInteractive(sessionID string, timeout time.Duration) error {
	proc := m.getInteractive(sessionID)
	if proc == nil {
		return nil
	}
	proc.PTY.Write([]byte("\x1b")) // clear any in-progress turn or input
	time.Sleep(50 * time.Millisecond)
	proc.PTY.Write([]byte("/exit\r"))

	select {
	case <-proc.Done:
		return nil
	case <-time.After(timeout):
		proc.Cmd.Process.Kill()
		<-proc.Done
		return nil
	}
}
