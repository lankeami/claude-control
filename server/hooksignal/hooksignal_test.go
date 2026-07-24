package hooksignal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunPostsHookEvent(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	keyFile := filepath.Join(t.TempDir(), "api.key")
	os.WriteFile(keyFile, []byte("sk-test-key"), 0600)

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	stdin := strings.NewReader(`{"hook_event_name":"Stop","session_id":"cli-uuid","transcript_path":"/tmp/t.jsonl","message":"hello"}`)
	if err := Run("stop", "managed-1", port, keyFile, stdin); err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/sessions/managed-1/hook-event" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("auth = %s", gotAuth)
	}
	if gotBody["event"] != "stop" || gotBody["claude_session_id"] != "cli-uuid" ||
		gotBody["transcript_path"] != "/tmp/t.jsonl" || gotBody["message"] != "hello" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestRunToleratesMalformedStdin(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	keyFile := filepath.Join(t.TempDir(), "api.key")
	os.WriteFile(keyFile, []byte("k"), 0600)

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	if err := Run("notification", "m1", port, keyFile, strings.NewReader("not json")); err != nil {
		t.Fatal(err)
	}
	if gotBody["event"] != "notification" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestRunMissingKeyFileReturnsError(t *testing.T) {
	err := Run("stop", "m1", 1, filepath.Join(t.TempDir(), "nope.key"), strings.NewReader("{}"))
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

const permReqStdin = `{"hook_event_name":"PermissionRequest","session_id":"cli-uuid","tool_name":"Bash","tool_input":{"command":"curl example.com","description":"Fetch example"}}`

func permReqServer(t *testing.T, decision string) (*httptest.Server, int, *map[string]json.RawMessage, *string, *string) {
	t.Helper()
	var gotBody map[string]json.RawMessage
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(decision))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return srv, port, &gotBody, &gotPath, &gotAuth
}

func writeKey(t *testing.T) string {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "api.key")
	os.WriteFile(keyFile, []byte("sk-test-key"), 0600)
	return keyFile
}

type hookDecisionOutput struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Decision      struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

func TestRunPermissionRequestAllow(t *testing.T) {
	_, port, gotBody, gotPath, gotAuth := permReqServer(t, `{"decision":"allow"}`)

	var stdout strings.Builder
	if err := RunPermissionRequest("m1", port, writeKey(t), strings.NewReader(permReqStdin), &stdout); err != nil {
		t.Fatal(err)
	}

	if *gotPath != "/api/sessions/m1/permission-request" {
		t.Errorf("path = %s", *gotPath)
	}
	if *gotAuth != "Bearer sk-test-key" {
		t.Errorf("auth = %s", *gotAuth)
	}
	var toolName, desc string
	json.Unmarshal((*gotBody)["tool_name"], &toolName)
	json.Unmarshal((*gotBody)["description"], &desc)
	if toolName != "Bash" {
		t.Errorf("tool_name = %s", toolName)
	}
	if desc != "Fetch example" {
		t.Errorf("description = %s", desc)
	}
	if !strings.Contains(string((*gotBody)["input"]), "curl example.com") {
		t.Errorf("input not forwarded: %s", (*gotBody)["input"])
	}

	var out hookDecisionOutput
	if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
		t.Fatalf("stdout not valid JSON: %v: %s", err, stdout.String())
	}
	if out.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Errorf("hookEventName = %s", out.HookSpecificOutput.HookEventName)
	}
	if out.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("behavior = %s", out.HookSpecificOutput.Decision.Behavior)
	}
}

func TestRunPermissionRequestAllowAlways(t *testing.T) {
	_, port, _, _, _ := permReqServer(t, `{"decision":"allow_always"}`)

	var stdout strings.Builder
	if err := RunPermissionRequest("m1", port, writeKey(t), strings.NewReader(permReqStdin), &stdout); err != nil {
		t.Fatal(err)
	}
	var out hookDecisionOutput
	if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
		t.Fatalf("stdout not valid JSON: %v", err)
	}
	if out.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("behavior = %s", out.HookSpecificOutput.Decision.Behavior)
	}
}

func TestRunPermissionRequestDeny(t *testing.T) {
	_, port, _, _, _ := permReqServer(t, `{"decision":"deny","reason":"timeout"}`)

	var stdout strings.Builder
	if err := RunPermissionRequest("m1", port, writeKey(t), strings.NewReader(permReqStdin), &stdout); err != nil {
		t.Fatal(err)
	}
	var out hookDecisionOutput
	if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
		t.Fatalf("stdout not valid JSON: %v", err)
	}
	if out.HookSpecificOutput.Decision.Behavior != "deny" {
		t.Errorf("behavior = %s", out.HookSpecificOutput.Decision.Behavior)
	}
	if !strings.Contains(out.HookSpecificOutput.Decision.Message, "timeout") {
		t.Errorf("message should include reason: %s", out.HookSpecificOutput.Decision.Message)
	}
}

func TestRunPermissionRequestServerErrorWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	var stdout strings.Builder
	err := RunPermissionRequest("m1", port, writeKey(t), strings.NewReader(permReqStdin), &stdout)
	if err == nil {
		t.Fatal("expected error for server 500")
	}
	// Fail-open: no stdout output means Claude Code falls back to its TUI dialog.
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on error, got: %s", stdout.String())
	}
}

func TestRunPermissionRequestUnknownDecisionWritesNothing(t *testing.T) {
	_, port, _, _, _ := permReqServer(t, `{"decision":"shrug"}`)

	var stdout strings.Builder
	if err := RunPermissionRequest("m1", port, writeKey(t), strings.NewReader(permReqStdin), &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty for unknown decision, got: %s", stdout.String())
	}
}
