package mcp

import (
	"encoding/json"
	"testing"
)

func TestParseRequest(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" {
		t.Errorf("method=%s, want initialize", req.Method)
	}
	if req.ID != 1 {
		t.Errorf("id=%v, want 1", req.ID)
	}
}

func TestParseRequestNotification(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "notifications/initialized" {
		t.Errorf("method=%s, want notifications/initialized", req.Method)
	}
	if req.ID != 0 {
		t.Errorf("id=%v, want 0 for notification", req.ID)
	}
}

func TestParseToolCallParams(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"permission_prompt","arguments":{"tool_name":"Bash","command":"echo hello"}}}`
	req, err := parseRequest([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Name != "permission_prompt" {
		t.Errorf("name=%s, want permission_prompt", params.Name)
	}
	if len(params.Arguments) == 0 {
		t.Error("expected non-empty arguments")
	}
}
