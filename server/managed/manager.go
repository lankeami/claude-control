package managed

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Config struct {
	ClaudeBin          string
	ClaudeArgs         []string
	ClaudeEnv          []string
	ServerPort         int
	BinaryPath         string
	// KeyFilePath is the api.key location hook-signal subprocesses must read
	// (follows the --db directory rather than the default home location).
	KeyFilePath        string
	IdleTimeoutMinutes int
	// Mode selects the managed-session backend: "interactive" (long-lived
	// interactive Claude Code under a PTY, billed via subscription) or
	// "print" (legacy per-message claude -p, billed via API).
	Mode    string
	CodexBin string
}

type SpawnOpts struct {
	Args []string
	CWD  string
}

type Process struct {
	Cmd          *exec.Cmd
	Stdin        io.WriteCloser
	Stdout       io.ReadCloser
	Stderr       io.ReadCloser
	Done         chan struct{}
	ExitCode     int
	TimedOut     bool
	LastActivity time.Time
}

type Manager struct {
	cfg          Config
	mu           sync.Mutex
	procs        map[string]*Process
	iprocs       map[string]*InteractiveProc
	cprocs       map[string]*CodexProc
	broadcasters map[string]*Broadcaster
	mutexes      map[string]*sync.Mutex

	// backgroundActivityFn reports the last known background activity
	// (workflow/subagent transcripts, background task output) for a session.
	// A zero time means no background activity is known. Consulted by
	// ReapIdle so sessions with in-flight background work are not killed.
	backgroundActivityFn func(sessionID string) time.Time

	// onReap is invoked after a session's process is reaped for idleness,
	// so callers can record the event durably (e.g. a system message row).
	onReap func(sessionID string, idleFor time.Duration)
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:          cfg,
		procs:        make(map[string]*Process),
		iprocs:       make(map[string]*InteractiveProc),
		cprocs:       make(map[string]*CodexProc),
		broadcasters: make(map[string]*Broadcaster),
		mutexes:      make(map[string]*sync.Mutex),
	}
}

func (m *Manager) sessionMutex(sessionID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mutexes[sessionID]; !ok {
		m.mutexes[sessionID] = &sync.Mutex{}
	}
	return m.mutexes[sessionID]
}

func (m *Manager) GetBroadcaster(sessionID string) *Broadcaster {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.broadcasters[sessionID]; !ok {
		m.broadcasters[sessionID] = NewBroadcaster()
	}
	return m.broadcasters[sessionID]
}

func (m *Manager) UpdateConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

func (m *Manager) Spawn(sessionID string, opts SpawnOpts) (*Process, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	if _, running := m.procs[sessionID]; running {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}
	cfg := m.cfg
	m.mu.Unlock()

	args := append(cfg.ClaudeArgs, opts.Args...)
	cmd := exec.Command(cfg.ClaudeBin, args...)
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), cfg.ClaudeEnv...)
	cmd.Env = append(cmd.Env, "CLAUDE_CONTROLLER_MANAGED=1")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	proc := &Process{
		Cmd:          cmd,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		Done:         make(chan struct{}),
		LastActivity: time.Now(),
	}

	m.mu.Lock()
	m.procs[sessionID] = proc
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		m.mu.Lock()
		delete(m.procs, sessionID)
		m.mu.Unlock()
		close(proc.Done)
	}()

	return proc, nil
}

// EnsureProcess returns an existing warm process for the session, or spawns a new one.
func (m *Manager) EnsureProcess(sessionID string, opts SpawnOpts) (*Process, error) {
	m.mu.Lock()
	if proc, ok := m.procs[sessionID]; ok {
		proc.LastActivity = time.Now()
		m.mu.Unlock()
		return proc, nil
	}
	m.mu.Unlock()

	return m.Spawn(sessionID, opts)
}

// SendTurn writes a user message JSON line to the process's stdin.
func (m *Manager) SendTurn(sessionID string, messageJSON string) error {
	m.mu.Lock()
	proc, ok := m.procs[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running process for session %s", sessionID)
	}

	proc.LastActivity = time.Now()
	_, err := fmt.Fprintf(proc.Stdin, "%s\n", messageJSON)
	return err
}

func (m *Manager) IsRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[sessionID]
	return ok
}

func (m *Manager) Interrupt(sessionID string) error {
	m.mu.Lock()
	proc, ok := m.procs[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running process for session %s", sessionID)
	}
	return interruptProcess(proc.Cmd.Process)
}

// SetBackgroundActivityFn installs the probe ReapIdle consults before
// reaping a session that is past the turn-idle threshold.
func (m *Manager) SetBackgroundActivityFn(fn func(sessionID string) time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backgroundActivityFn = fn
}

// BackgroundActivity reports the installed probe's result for a session, or
// the zero time when no probe is installed.
func (m *Manager) BackgroundActivity(sessionID string) time.Time {
	m.mu.Lock()
	fn := m.backgroundActivityFn
	m.mu.Unlock()
	if fn == nil {
		return time.Time{}
	}
	return fn(sessionID)
}

// SetReapCallback installs a callback invoked after each idle reap with the
// session ID and how long it had been idle.
func (m *Manager) SetReapCallback(fn func(sessionID string, idleFor time.Duration)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReap = fn
}

// reapHardCap is the absolute idle ceiling: past this, a session is reaped
// even if the background-activity probe reports recent activity, so phantom
// file churn can never leak processes forever.
const reapHardCap = 24 * time.Hour

// IdleTimeout returns the effective idle window: cfg.IdleTimeoutMinutes, or
// 30 minutes when unset/zero.
func (m *Manager) IdleTimeout() time.Duration {
	m.mu.Lock()
	timeout := m.cfg.IdleTimeoutMinutes
	m.mu.Unlock()
	if timeout <= 0 {
		timeout = 30
	}
	return time.Duration(timeout) * time.Minute
}

// ReapIdle shuts down any process that has been idle longer than maxIdle.
// A session past the turn-idle threshold is first checked against the
// background-activity probe (workflow/subagent transcripts, task output):
// recent background work keeps it alive, up to a 24h hard cap. Call this
// periodically from a goroutine.
func (m *Manager) ReapIdle(maxIdle time.Duration) {
	const (
		kindProcess = iota
		kindInteractive
		kindCodex
	)
	type candidate struct {
		id   string
		idle time.Duration
		kind int
	}

	now := time.Now()
	m.mu.Lock()
	probe := m.backgroundActivityFn
	onReap := m.onReap
	var candidates []candidate
	for id, proc := range m.procs {
		if idle := now.Sub(proc.LastActivity); idle > maxIdle {
			candidates = append(candidates, candidate{id, idle, kindProcess})
		}
	}
	for id, proc := range m.iprocs {
		if idle := now.Sub(proc.LastActivity); idle > maxIdle {
			candidates = append(candidates, candidate{id, idle, kindInteractive})
		}
	}
	for id, proc := range m.cprocs {
		if idle := now.Sub(proc.LastActivity); idle > maxIdle {
			candidates = append(candidates, candidate{id, idle, kindCodex})
		}
	}
	m.mu.Unlock()

	for _, c := range candidates {
		// Only sessions already past the turn-idle threshold are probed,
		// keeping per-tick I/O minimal.
		if probe != nil && c.idle <= reapHardCap {
			if bg := probe(c.id); !bg.IsZero() {
				bgIdle := now.Sub(bg)
				if bgIdle <= maxIdle {
					log.Printf("skipping reap of session %s: background activity %s ago (turn idle %s)",
						c.id, bgIdle.Round(time.Second), c.idle.Round(time.Second))
					continue
				}
				if bgIdle < c.idle {
					c.idle = bgIdle
				}
			}
		}

		reaped := false
		switch c.kind {
		case kindProcess:
			m.mu.Lock()
			proc, ok := m.procs[c.id]
			m.mu.Unlock()
			if ok && proc.Stdin != nil {
				log.Printf("reaping idle process for session %s (idle %s)", c.id, c.idle.Round(time.Second))
				proc.Stdin.Close()
				reaped = true
			}
		case kindInteractive:
			if m.IsInteractiveRunning(c.id) {
				log.Printf("reaping idle interactive process for session %s (idle %s)", c.id, c.idle.Round(time.Second))
				m.ShutdownInteractive(c.id, 10*time.Second)
				reaped = true
			}
		case kindCodex:
			if m.IsCodexRunning(c.id) {
				log.Printf("reaping idle codex process for session %s (idle %s)", c.id, c.idle.Round(time.Second))
				m.ShutdownCodex(c.id, 10*time.Second)
				reaped = true
			}
		}
		if reaped && onReap != nil {
			onReap(c.id, c.idle)
		}
	}
}

// StartReaper starts a background goroutine that periodically calls ReapIdle
// with the effective idle timeout (cfg.IdleTimeoutMinutes, default 30m).
func (m *Manager) StartReaper() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.ReapIdle(m.IdleTimeout())
		}
	}()
}

type ShellOpts struct {
	Command string
	CWD     string
	Timeout time.Duration
}

func (m *Manager) SpawnShell(sessionID string, opts ShellOpts) (*Process, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	if _, running := m.procs[sessionID]; running {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}
	m.mu.Unlock()

	cmd := newShellCmd(opts.Command)
	cmd.Dir = opts.CWD

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	proc := &Process{
		Cmd:    cmd,
		Stdout: stdout,
		Stderr: stderr,
		Done:   make(chan struct{}),
	}

	m.mu.Lock()
	m.procs[sessionID] = proc
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		m.mu.Lock()
		delete(m.procs, sessionID)
		m.mu.Unlock()
		close(proc.Done)
	}()

	if opts.Timeout > 0 {
		go func() {
			select {
			case <-time.After(opts.Timeout):
				killWithTimeout(cmd, proc, 5*time.Second)
			case <-proc.Done:
				return
			}
		}()
	}

	return proc, nil
}

func (m *Manager) Teardown(sessionID string, timeout time.Duration) error {
	if m.IsInteractiveRunning(sessionID) {
		if err := m.ShutdownInteractive(sessionID, timeout); err != nil {
			return err
		}
	}
	if m.IsCodexRunning(sessionID) {
		if err := m.ShutdownCodex(sessionID, timeout); err != nil {
			return err
		}
	}
	if !m.IsRunning(sessionID) {
		return nil
	}
	if err := m.Interrupt(sessionID); err != nil {
		return err
	}

	m.mu.Lock()
	proc := m.procs[sessionID]
	m.mu.Unlock()
	if proc == nil {
		return nil
	}

	select {
	case <-proc.Done:
		return nil
	case <-time.After(timeout):
		proc.Cmd.Process.Kill()
		<-proc.Done
		return nil
	}
}

// RunCompact spawns a one-shot `claude -p "/compact" --resume <id>` process
// and waits for it to exit. This is separate from the warm persistent process
// so it can run after the warm process has been interrupted.
func (m *Manager) RunCompact(sessionID string, resumeID string, cwd string, timeout time.Duration) error {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	args := append([]string{}, cfg.ClaudeArgs...)
	args = append(args, "-p", "/compact", "--resume", resumeID, "--output-format", "stream-json", "--verbose")

	cmd := exec.Command(cfg.ClaudeBin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), cfg.ClaudeEnv...)
	cmd.Env = append(cmd.Env, "CLAUDE_CONTROLLER_MANAGED=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("compact stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("compact start: %w", err)
	}

	// Stream compact output through the session's broadcaster so SSE clients
	// stay connected (heartbeats) and see compact progress.
	broadcaster := m.GetBroadcaster(sessionID)
	streamDone := make(chan struct{})
	go func() {
		StreamNDJSON(stdout, broadcaster, nil, nil)
		close(streamDone)
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		<-streamDone // wait for stream goroutine to finish
		return err
	case <-time.After(timeout):
		cmd.Process.Kill()
		<-done
		<-streamDone
		return fmt.Errorf("compact timed out after %v", timeout)
	}
}

// ShutdownAll gracefully shuts down all running processes.
// Closes stdin and waits up to timeout for each to exit, then kills.
func (m *Manager) ShutdownAll(timeout time.Duration) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	iids := make([]string, 0, len(m.iprocs))
	for id := range m.iprocs {
		iids = append(iids, id)
	}
	cids := make([]string, 0, len(m.cprocs))
	for id := range m.cprocs {
		cids = append(cids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		log.Printf("shutting down process for session %s", id)
		m.GracefulShutdown(id, timeout)
	}
	for _, id := range iids {
		log.Printf("shutting down interactive process for session %s", id)
		m.ShutdownInteractive(id, timeout)
	}
	for _, id := range cids {
		log.Printf("shutting down codex process for session %s", id)
		m.ShutdownCodex(id, timeout)
	}
}

// GracefulShutdown closes stdin to let the process exit naturally, then waits
// up to timeout for it to finish. Falls back to SIGKILL if it doesn't exit.
// No-op if no process is running for the session.
func (m *Manager) GracefulShutdown(sessionID string, timeout time.Duration) error {
	m.mu.Lock()
	proc, ok := m.procs[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil
	}

	if proc.Stdin != nil {
		proc.Stdin.Close()
	}

	select {
	case <-proc.Done:
		return nil
	case <-time.After(timeout):
		proc.Cmd.Process.Kill()
		<-proc.Done
		return nil
	}
}
