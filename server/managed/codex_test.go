package managed

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeAppServer simulates a codex app-server over io.Pipe pairs.
type fakeAppServer struct {
	clientReader *io.PipeReader // client reads server output from here
	clientWriter *io.PipeWriter // client writes requests to here
	serverReader *io.PipeReader // server reads client requests from here
	serverWriter *io.PipeWriter // server writes output to client here
}

func newFakeAppServer() *fakeAppServer {
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	return &fakeAppServer{
		clientReader: cr,
		clientWriter: cw,
		serverReader: sr,
		serverWriter: sw,
	}
}

func (f *fakeAppServer) readRequest(t *testing.T) map[string]interface{} {
	t.Helper()
	buf := make([]byte, 64*1024)
	n, err := f.serverReader.Read(buf)
	if err != nil {
		t.Fatalf("readRequest: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		t.Fatalf("readRequest unmarshal: %v (raw: %s)", err, string(buf[:n]))
	}
	return msg
}

func (f *fakeAppServer) sendResponse(t *testing.T, id int, result interface{}) {
	t.Helper()
	resp := map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(f.serverWriter, "%s\n", data)
}

func (f *fakeAppServer) sendNotification(t *testing.T, method string, params interface{}) {
	t.Helper()
	msg := map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(f.serverWriter, "%s\n", data)
}

func (f *fakeAppServer) sendRequest(t *testing.T, id int, method string, params interface{}) {
	t.Helper()
	msg := map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": id}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(f.serverWriter, "%s\n", data)
}

func (f *fakeAppServer) readResponse(t *testing.T) map[string]interface{} {
	t.Helper()
	buf := make([]byte, 64*1024)
	n, err := f.serverReader.Read(buf)
	if err != nil {
		t.Fatalf("readResponse: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		t.Fatalf("readResponse unmarshal: %v (raw: %s)", err, string(buf[:n]))
	}
	return msg
}

func (f *fakeAppServer) close() {
	f.serverWriter.Close()
	f.clientWriter.Close()
}

// --- TestCodexBackend: JSON-RPC client lifecycle ---

func TestCodexBackend_CallAndResponse(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{})
	defer func() { proc.Close(); fake.close() }()

	resultCh := make(chan json.RawMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := proc.Call("thread/start", map[string]string{"cwd": "/tmp"})
		resultCh <- r
		errCh <- err
	}()

	req := fake.readRequest(t)
	if req["method"] != "thread/start" {
		t.Fatalf("method=%v, want thread/start", req["method"])
	}
	id := int(req["id"].(float64))
	fake.sendResponse(t, id, map[string]string{"status": "ok"})

	result := <-resultCh
	err := <-errCh
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	var r map[string]string
	json.Unmarshal(result, &r)
	if r["status"] != "ok" {
		t.Errorf("result status=%v, want ok", r["status"])
	}
}

func TestCodexBackend_CallErrorResponse(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{})
	defer func() { proc.Close(); fake.close() }()

	errCh := make(chan error, 1)
	go func() {
		_, err := proc.Call("thread/start", nil)
		errCh <- err
	}()

	req := fake.readRequest(t)
	id := int(req["id"].(float64))
	errResp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": -32600, "message": "invalid request"},
	}
	data, _ := json.Marshal(errResp)
	fmt.Fprintf(fake.serverWriter, "%s\n", data)

	err := <-errCh
	if err == nil {
		t.Fatal("expected error from Call")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Errorf("error=%v, want to contain 'invalid request'", err)
	}
}

func TestCodexBackend_NotificationCallback(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})

	notifCh := make(chan string, 10)
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{
		OnNotification: func(method string, params json.RawMessage) {
			notifCh <- method
		},
	})
	defer func() { proc.Close(); fake.close() }()

	fake.sendNotification(t, "item/agentMessage/delta", map[string]string{"text": "hello"})
	fake.sendNotification(t, "item/started", map[string]string{"type": "function_call", "name": "Bash"})

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	var methods []string
	for i := 0; i < 2; i++ {
		select {
		case m := <-notifCh:
			methods = append(methods, m)
		case <-timer.C:
			t.Fatal("timeout waiting for notifications")
		}
	}
	if methods[0] != "item/agentMessage/delta" || methods[1] != "item/started" {
		t.Errorf("methods=%v", methods)
	}
}

func TestCodexBackend_EnsureThreadOnce(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{})
	defer func() { proc.Close(); fake.close() }()

	go func() {
		req := fake.readRequest(t)
		id := int(req["id"].(float64))
		fake.sendResponse(t, id, map[string]string{"status": "ok"})
	}()

	if err := proc.EnsureThread("/tmp"); err != nil {
		t.Fatal(err)
	}
	if err := proc.EnsureThread("/tmp"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexBackend_ProcessCloseCleansUpPending(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{})

	errCh := make(chan error, 1)
	go func() {
		_, err := proc.Call("turn/start", nil)
		errCh <- err
	}()

	fake.readRequest(t)
	fake.close()
	close(done)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after process closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Call to return after close")
	}
}

// --- TestCodexApproval: server-initiated approval request ---

func TestCodexApproval_Approved(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{
		OnApproval: func(toolName, description string, input json.RawMessage) bool {
			return toolName == "Bash"
		},
	})
	defer func() { proc.Close(); fake.close() }()

	fake.sendRequest(t, 99, "approvals/request", map[string]interface{}{
		"tool_name":   "Bash",
		"description": "run ls",
		"input":       map[string]string{"command": "ls"},
	})

	resp := fake.readResponse(t)
	id := int(resp["id"].(float64))
	if id != 99 {
		t.Errorf("response id=%d, want 99", id)
	}
	result := resp["result"].(map[string]interface{})
	if result["approved"] != true {
		t.Errorf("approved=%v, want true", result["approved"])
	}
}

func TestCodexApproval_Denied(t *testing.T) {
	fake := newFakeAppServer()
	done := make(chan struct{})
	proc := NewCodexProc(fake.clientReader, fake.clientWriter, done, CodexOpts{
		OnApproval: func(toolName, description string, input json.RawMessage) bool {
			return false
		},
	})
	defer func() { proc.Close(); fake.close() }()

	fake.sendRequest(t, 100, "approvals/request", map[string]interface{}{
		"tool_name":   "Write",
		"description": "write file",
		"input":       map[string]string{"path": "/etc/passwd"},
	})

	resp := fake.readResponse(t)
	result := resp["result"].(map[string]interface{})
	if result["approved"] != false {
		t.Errorf("approved=%v, want false", result["approved"])
	}
}

// --- TestCodexAdapter: notification → stream-json translation ---

func TestCodexAdapter_AgentMessageDelta(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"text": "hello world"})
	out := AdaptCodexNotification("item/agentMessage/delta", params)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if parsed["type"] != "assistant" {
		t.Errorf("type=%v, want assistant", parsed["type"])
	}
	msg := parsed["message"].(map[string]interface{})
	content := msg["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if block["text"] != "hello world" {
		t.Errorf("text=%v, want 'hello world'", block["text"])
	}
}

func TestCodexAdapter_ItemStartedFunctionCall(t *testing.T) {
	params, _ := json.Marshal(map[string]interface{}{
		"type":    "function_call",
		"name":    "Read",
		"call_id": "call_123",
	})
	out := AdaptCodexNotification("item/started", params)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	msg := parsed["message"].(map[string]interface{})
	content := msg["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("type=%v, want tool_use", block["type"])
	}
	if block["name"] != "Read" {
		t.Errorf("name=%v, want Read", block["name"])
	}
}

func TestCodexAdapter_IgnoresUnknownMethods(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"foo": "bar"})
	out := AdaptCodexNotification("unknown/method", params)
	if out != "" {
		t.Errorf("expected empty for unknown method, got %q", out)
	}
}

func TestCodexAdapter_EmptyTextReturnsEmpty(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"text": ""})
	out := AdaptCodexNotification("item/agentMessage/delta", params)
	if out != "" {
		t.Errorf("expected empty for empty text, got %q", out)
	}
}

func TestCodexAdapter_RolloutResponseItem(t *testing.T) {
	line := `{"timestamp":"2026-08-12T00:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}}`
	out := AdaptCodexRolloutLine(line)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if parsed["type"] != "assistant" {
		t.Errorf("type=%v, want assistant", parsed["type"])
	}
}

func TestCodexAdapter_RolloutTaskComplete(t *testing.T) {
	line := `{"timestamp":"2026-08-12T00:00:00Z","type":"event_msg","payload":{"type":"task_complete"}}`
	out := AdaptCodexRolloutLine(line)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if parsed["type"] != "result" {
		t.Errorf("type=%v, want result", parsed["type"])
	}
	if parsed["subtype"] != "success" {
		t.Errorf("subtype=%v, want success", parsed["subtype"])
	}
}

func TestCodexAdapter_RolloutTaskCompleteWithError(t *testing.T) {
	line := `{"timestamp":"2026-08-12T00:00:00Z","type":"event_msg","payload":{"type":"task_complete","error":{"message":"boom"}}}`
	out := AdaptCodexRolloutLine(line)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if parsed["subtype"] != "error_during_execution" {
		t.Errorf("subtype=%v, want error_during_execution", parsed["subtype"])
	}
}

func TestCodexAdapter_MakeResultEventWithUsage(t *testing.T) {
	out := MakeResultEvent("success", 100, 50)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if parsed["type"] != "result" {
		t.Errorf("type=%v, want result", parsed["type"])
	}
	usage := parsed["usage"].(map[string]interface{})
	if int(usage["input_tokens"].(float64)) != 100 {
		t.Errorf("input_tokens=%v, want 100", usage["input_tokens"])
	}
	if int(usage["output_tokens"].(float64)) != 50 {
		t.Errorf("output_tokens=%v, want 50", usage["output_tokens"])
	}
}

func TestCodexAdapter_MakeResultEventNoUsage(t *testing.T) {
	out := MakeResultEvent("success", 0, 0)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(out), &parsed)
	if _, ok := parsed["usage"]; ok {
		t.Error("expected no usage field when tokens are 0")
	}
}
