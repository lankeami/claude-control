package managed

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	ClaudeBin  string
	ClaudeArgs []string
	ClaudeEnv  []string
	ServerPort int
	BinaryPath string
}

type SpawnOpts struct {
	Args []string
	CWD  string
}

type Process struct {
	Cmd      *exec.Cmd
	Stdout   io.ReadCloser
	Stderr   io.ReadCloser
	Done     chan struct{}
	ExitCode int
	TimedOut bool
}

type Manager struct {
	cfg          Config
	mu           sync.Mutex
	procs        map[string]*Process
	broadcasters map[string]*Broadcaster
	mutexes      map[string]*sync.Mutex
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:          cfg,
		procs:        make(map[string]*Process),
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

	if _, running := m.procs[sessionID]; running {
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}

	// Copy config under lock to prevent race with UpdateConfig
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	args := append(cfg.ClaudeArgs, opts.Args...)
	cmd := exec.Command(cfg.ClaudeBin, args...)
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), cfg.ClaudeEnv...)
	cmd.Env = append(cmd.Env, "CLAUDE_CONTROLLER_MANAGED=1")

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

	return proc, nil
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
	return proc.Cmd.Process.Signal(syscall.SIGINT)
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

	if _, running := m.procs[sessionID]; running {
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}

	cmd := exec.Command("sh", "-c", opts.Command)
	cmd.Dir = opts.CWD
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
				pgid, err := syscall.Getpgid(cmd.Process.Pid)
				if err != nil {
					cmd.Process.Kill()
					return
				}
				proc.TimedOut = true
				syscall.Kill(-pgid, syscall.SIGINT)
				select {
				case <-proc.Done:
					return
				case <-time.After(5 * time.Second):
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			case <-proc.Done:
				return
			}
		}()
	}

	return proc, nil
}

func (m *Manager) Teardown(sessionID string, timeout time.Duration) error {
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
