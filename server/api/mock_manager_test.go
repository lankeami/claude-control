package api

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// --- NDJSON test fixtures ---

const assistantLine = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`
const resultLine = `{"type":"result","subtype":"success","cost":0.01}`
const maxTurnsResultLine = `{"type":"result","subtype":"error_max_turns","cost":0.01}`
const errorResultLine = `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["something failed"]}`
const toolUseLine = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/test.go"}}]}}`

// --- MockManager ---

type MockManager struct {
	mu           sync.Mutex
	broadcasters map[string]*managed.Broadcaster
	running      map[string]bool

	OnEnsureProcess    func(sessionID string, opts managed.SpawnOpts) (*managed.Process, error)
	OnSendTurn         func(sessionID string, messageJSON string) error
	OnInterrupt        func(sessionID string) error
	OnGracefulShutdown func(sessionID string, timeout time.Duration) error
	OnRunCompact       func(sessionID string, resumeID string, cwd string, timeout time.Duration) error
	OnTeardown         func(sessionID string, timeout time.Duration) error
	OnSpawnShell       func(sessionID string, opts managed.ShellOpts) (*managed.Process, error)
	ConfigValue        managed.Config

	// Interactive mode
	interactiveRunning        map[string]bool
	stopChans                 map[string]chan struct{}
	TranscriptLineFns         map[string]func(string) // captured OnTranscriptLine per session
	SetTranscriptCalls        []string
	SentPrompts               []string
	SentKeys                  []string
	InterruptInteractiveCalls int
	OnEnsureInteractive       func(sessionID string, opts managed.InteractiveOpts) (*managed.InteractiveProc, error)
	OnSendPrompt              func(sessionID, text string) error
	OnShutdownInteractive     func(sessionID string, timeout time.Duration) error
}

func NewMockManager() *MockManager {
	return &MockManager{
		broadcasters:       make(map[string]*managed.Broadcaster),
		running:            make(map[string]bool),
		interactiveRunning: make(map[string]bool),
		stopChans:          make(map[string]chan struct{}),
		TranscriptLineFns:  make(map[string]func(string)),
	}
}

func (m *MockManager) GetBroadcaster(sessionID string) *managed.Broadcaster {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.broadcasters[sessionID]; ok {
		return b
	}
	b := managed.NewBroadcaster()
	m.broadcasters[sessionID] = b
	return b
}

func (m *MockManager) EnsureProcess(sessionID string, opts managed.SpawnOpts) (*managed.Process, error) {
	if m.OnEnsureProcess != nil {
		return m.OnEnsureProcess(sessionID, opts)
	}
	return nil, fmt.Errorf("EnsureProcess not configured")
}

func (m *MockManager) SendTurn(sessionID string, messageJSON string) error {
	if m.OnSendTurn != nil {
		return m.OnSendTurn(sessionID, messageJSON)
	}
	return nil
}

func (m *MockManager) Interrupt(sessionID string) error {
	if m.OnInterrupt != nil {
		return m.OnInterrupt(sessionID)
	}
	return nil
}

func (m *MockManager) IsRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running[sessionID]
}

func (m *MockManager) SetRunning(sessionID string, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running[sessionID] = running
}

func (m *MockManager) GracefulShutdown(sessionID string, timeout time.Duration) error {
	if m.OnGracefulShutdown != nil {
		return m.OnGracefulShutdown(sessionID, timeout)
	}
	return nil
}

func (m *MockManager) RunCompact(sessionID string, resumeID string, cwd string, timeout time.Duration) error {
	if m.OnRunCompact != nil {
		return m.OnRunCompact(sessionID, resumeID, cwd, timeout)
	}
	return nil
}

func (m *MockManager) Teardown(sessionID string, timeout time.Duration) error {
	if m.OnTeardown != nil {
		return m.OnTeardown(sessionID, timeout)
	}
	return nil
}

func (m *MockManager) SpawnShell(sessionID string, opts managed.ShellOpts) (*managed.Process, error) {
	if m.OnSpawnShell != nil {
		return m.OnSpawnShell(sessionID, opts)
	}
	return nil, fmt.Errorf("SpawnShell not configured")
}

func (m *MockManager) Config() managed.Config {
	return m.ConfigValue
}

func (m *MockManager) UpdateConfig(cfg managed.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConfigValue = cfg
}

// --- Interactive mode ---

func (m *MockManager) EnsureInteractive(sessionID string, opts managed.InteractiveOpts) (*managed.InteractiveProc, error) {
	m.mu.Lock()
	m.interactiveRunning[sessionID] = true
	m.TranscriptLineFns[sessionID] = opts.OnTranscriptLine
	if _, ok := m.stopChans[sessionID]; !ok {
		m.stopChans[sessionID] = make(chan struct{}, 4)
	}
	m.mu.Unlock()
	if m.OnEnsureInteractive != nil {
		return m.OnEnsureInteractive(sessionID, opts)
	}
	return &managed.InteractiveProc{Done: make(chan struct{})}, nil
}

func (m *MockManager) IsInteractiveRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.interactiveRunning[sessionID]
}

func (m *MockManager) SetInteractiveRunning(sessionID string, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interactiveRunning[sessionID] = running
	if _, ok := m.stopChans[sessionID]; !ok {
		m.stopChans[sessionID] = make(chan struct{}, 4)
	}
}

func (m *MockManager) SendPrompt(sessionID, text string) error {
	m.mu.Lock()
	m.SentPrompts = append(m.SentPrompts, text)
	m.mu.Unlock()
	if m.OnSendPrompt != nil {
		return m.OnSendPrompt(sessionID, text)
	}
	return nil
}

func (m *MockManager) SentPromptsCopy() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.SentPrompts...)
}

func (m *MockManager) SendKeys(sessionID, seq string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentKeys = append(m.SentKeys, seq)
	return nil
}

func (m *MockManager) InterruptInteractive(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InterruptInteractiveCalls++
	return nil
}

func (m *MockManager) InterruptInteractiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.InterruptInteractiveCalls
}

func (m *MockManager) SetTranscript(sessionID, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetTranscriptCalls = append(m.SetTranscriptCalls, sessionID+":"+path)
}

func (m *MockManager) SignalStop(sessionID string) {
	m.mu.Lock()
	ch, ok := m.stopChans[sessionID]
	m.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (m *MockManager) StopEvents(sessionID string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopChans[sessionID]
}

func (m *MockManager) ShutdownInteractive(sessionID string, timeout time.Duration) error {
	m.mu.Lock()
	m.interactiveRunning[sessionID] = false
	m.mu.Unlock()
	if m.OnShutdownInteractive != nil {
		return m.OnShutdownInteractive(sessionID, timeout)
	}
	return nil
}

// EmitTranscriptLine simulates the transcript tailer delivering a line.
func (m *MockManager) EmitTranscriptLine(sessionID, line string) {
	m.mu.Lock()
	fn := m.TranscriptLineFns[sessionID]
	m.mu.Unlock()
	if fn != nil {
		fn(line)
	}
}

// --- Process simulation helpers ---

func makeProcess() (*managed.Process, *io.PipeWriter) {
	pr, pw := io.Pipe()
	done := make(chan struct{})
	proc := &managed.Process{
		Stdout: pr,
		Stderr: io.NopCloser(&nopReader{}),
		Done:   done,
	}
	return proc, pw
}

func writeLine(pw *io.PipeWriter, line string) {
	pw.Write([]byte(line + "\n"))
}

type nopReader struct{}

func (nopReader) Read(p []byte) (int, error) { return 0, io.EOF }
