package managed

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// --- JSON-RPC 2.0 types ---

type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// --- CodexProc: wraps a codex app-server JSON-RPC 2.0 process ---

type CodexOpts struct {
	CWD            string
	OnNotification func(method string, params json.RawMessage)
	OnApproval     func(toolName, description string, input json.RawMessage) bool
}

type CodexProc struct {
	Cmd          *exec.Cmd
	Done         chan struct{}
	ExitCode     int
	LastActivity time.Time

	opts    CodexOpts
	mu      sync.Mutex
	nextID  int
	pending map[int]chan *jsonrpcMessage
	writer  io.WriteCloser
	closed  bool

	threadOnce sync.Once
	threadErr  error
}

// NewCodexProc creates a CodexProc from raw reader/writer. Testable with io.Pipe.
func NewCodexProc(reader io.Reader, writer io.WriteCloser, done chan struct{}, opts CodexOpts) *CodexProc {
	p := &CodexProc{
		Done:         done,
		LastActivity: time.Now(),
		opts:         opts,
		pending:      make(map[int]chan *jsonrpcMessage),
		writer:       writer,
	}
	go p.readLoop(reader)
	return p
}

func (p *CodexProc) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg jsonrpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("codex: invalid JSON-RPC: %v", err)
			continue
		}
		p.dispatch(&msg)
	}
	p.mu.Lock()
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	p.mu.Unlock()
}

func (p *CodexProc) dispatch(msg *jsonrpcMessage) {
	if msg.ID != nil && msg.Method == "" {
		p.mu.Lock()
		ch, ok := p.pending[*msg.ID]
		if ok {
			delete(p.pending, *msg.ID)
		}
		p.mu.Unlock()
		if ok {
			ch <- msg
		}
		return
	}
	if msg.ID != nil && msg.Method != "" {
		go p.handleServerRequest(msg)
		return
	}
	if p.opts.OnNotification != nil {
		p.opts.OnNotification(msg.Method, msg.Params)
	}
}

func (p *CodexProc) handleServerRequest(msg *jsonrpcMessage) {
	if msg.Method == "approvals/request" {
		var params struct {
			ToolName    string          `json:"tool_name"`
			Description string          `json:"description"`
			Input       json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			p.sendResponse(*msg.ID, map[string]interface{}{"approved": false})
			return
		}
		approved := false
		if p.opts.OnApproval != nil {
			approved = p.opts.OnApproval(params.ToolName, params.Description, params.Input)
		}
		p.sendResponse(*msg.ID, map[string]interface{}{"approved": approved})
		return
	}
	p.sendResponse(*msg.ID, nil)
}

func (p *CodexProc) sendResponse(id int, result interface{}) error {
	resp := struct {
		JSONRPC string      `json:"jsonrpc"`
		Result  interface{} `json:"result"`
		ID      int         `json:"id"`
	}{"2.0", result, id}
	return p.writeJSON(resp)
}

// Call sends a JSON-RPC request and blocks until the response arrives.
func (p *CodexProc) Call(method string, params interface{}) (json.RawMessage, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("codex process closed")
	}
	p.nextID++
	id := p.nextID
	ch := make(chan *jsonrpcMessage, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	req := struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
		ID      int         `json:"id"`
	}{"2.0", method, params, id}

	if err := p.writeJSON(req); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}
	p.LastActivity = time.Now()

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("codex process closed")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-p.Done:
		return nil, fmt.Errorf("codex process exited")
	}
}

// EnsureThread calls thread/start once per process lifetime.
func (p *CodexProc) EnsureThread(cwd string) error {
	p.threadOnce.Do(func() {
		_, p.threadErr = p.Call("thread/start", map[string]interface{}{"cwd": cwd})
	})
	return p.threadErr
}

func (p *CodexProc) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err = fmt.Fprintf(p.writer, "%s\n", data)
	return err
}

func (p *CodexProc) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	return p.writer.Close()
}

// --- Manager methods for codex processes ---

func (m *Manager) EnsureCodex(sessionID string, opts CodexOpts) (*CodexProc, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	if proc, ok := m.cprocs[sessionID]; ok {
		proc.LastActivity = time.Now()
		m.mu.Unlock()
		return proc, nil
	}
	cfg := m.cfg
	m.mu.Unlock()

	if cfg.CodexBin == "" {
		return nil, fmt.Errorf("codex binary not configured")
	}

	cmd := exec.Command(cfg.CodexBin, "app-server")
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), "CLAUDE_CONTROLLER_MANAGED=1")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex: %w", err)
	}

	done := make(chan struct{})
	proc := NewCodexProc(stdout, stdin, done, opts)
	proc.Cmd = cmd

	m.mu.Lock()
	m.cprocs[sessionID] = proc
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		m.mu.Lock()
		if m.cprocs[sessionID] == proc {
			delete(m.cprocs, sessionID)
		}
		m.mu.Unlock()
		close(done)
	}()

	return proc, nil
}

func (m *Manager) IsCodexRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.cprocs[sessionID]
	return ok
}

func (m *Manager) InterruptCodex(sessionID string) error {
	m.mu.Lock()
	proc, ok := m.cprocs[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no codex process for session %s", sessionID)
	}
	_, err := proc.Call("turn/interrupt", nil)
	return err
}

func (m *Manager) ShutdownCodex(sessionID string, timeout time.Duration) error {
	m.mu.Lock()
	proc, ok := m.cprocs[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	proc.Close()
	select {
	case <-proc.Done:
		return nil
	case <-time.After(timeout):
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			proc.Cmd.Process.Kill()
		}
		<-proc.Done
		return nil
	}
}

// --- Stream adapter: codex notifications → Claude stream-json ---

// AdaptCodexNotification translates a codex app-server JSON-RPC notification
// into a Claude stream-json line the existing SSE pipeline can consume.
// Returns "" for notifications with no stream-json equivalent.
func AdaptCodexNotification(method string, params json.RawMessage) string {
	switch method {
	case "item/agentMessage/delta":
		var p struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(params, &p) != nil || p.Text == "" {
			return ""
		}
		out, _ := json.Marshal(map[string]interface{}{
			"type": "assistant",
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": []map[string]string{{"type": "text", "text": p.Text}},
			},
		})
		return string(out)

	case "item/started":
		var p struct {
			Type string `json:"type"`
			Name string `json:"name"`
			ID   string `json:"call_id"`
		}
		if json.Unmarshal(params, &p) != nil || p.Type != "function_call" || p.Name == "" {
			return ""
		}
		out, _ := json.Marshal(map[string]interface{}{
			"type": "assistant",
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": []map[string]interface{}{{"type": "tool_use", "name": p.Name, "id": p.ID}},
			},
		})
		return string(out)

	default:
		return ""
	}
}

// AdaptCodexRolloutLine translates a codex rollout JSONL entry into a Claude
// stream-json line. Returns "" for entries with no stream-json equivalent.
func AdaptCodexRolloutLine(line string) string {
	var entry struct {
		Type    string `json:"type"`
		Payload struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Message string          `json:"message"`
			Error   *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &entry) != nil {
		return ""
	}

	switch entry.Type {
	case "response_item":
		if entry.Payload.Type == "message" && entry.Payload.Role == "assistant" {
			out, _ := json.Marshal(map[string]interface{}{
				"type": "assistant",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": entry.Payload.Content,
				},
			})
			return string(out)
		}

	case "event_msg":
		switch entry.Payload.Type {
		case "agent_message":
			if entry.Payload.Message != "" {
				out, _ := json.Marshal(map[string]interface{}{
					"type": "assistant",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": []map[string]string{{"type": "text", "text": entry.Payload.Message}},
					},
				})
				return string(out)
			}
		case "task_complete":
			subtype := "success"
			if entry.Payload.Error != nil {
				subtype = "error_during_execution"
			}
			out, _ := json.Marshal(map[string]interface{}{
				"type":    "result",
				"subtype": subtype,
			})
			return string(out)
		}
	}
	return ""
}

// MakeResultEvent produces a stream-json result event with optional usage data.
func MakeResultEvent(subtype string, inputTokens, outputTokens int) string {
	evt := map[string]interface{}{
		"type":    "result",
		"subtype": subtype,
	}
	if inputTokens > 0 || outputTokens > 0 {
		evt["usage"] = map[string]int{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		}
	}
	out, _ := json.Marshal(evt)
	return string(out)
}
