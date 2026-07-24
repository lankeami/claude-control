// Package hooksignal implements the `claude-controller hook-signal`
// subcommand. It runs as a Claude Code hook inside managed interactive
// sessions and relays hook events (SessionStart, Stop, Notification) to the
// local controller server so it can drive the turn lifecycle.
package hooksignal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readAPIKey(keyFile string) (string, error) {
	if keyFile == "" {
		keyFile = os.Getenv("CLAUDE_CONTROLLER_KEY_FILE")
	}
	if keyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		keyFile = filepath.Join(home, ".claude-controller", "api.key")
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return "", fmt.Errorf("read api key: %w", err)
	}
	return strings.TrimSpace(string(keyData)), nil
}

// Run reads the Claude Code hook payload from stdin and POSTs a hook-event to
// the local server. Callers must treat errors as non-fatal: a broken relay
// must never block Claude (main.go ignores the returned error and exits 0).
func Run(event, sessionID string, port int, keyFile string, stdin io.Reader) error {
	apiKey, err := readAPIKey(keyFile)
	if err != nil {
		return err
	}

	var input struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
		Message        string `json:"message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(stdin, 1<<20))
	_ = json.Unmarshal(raw, &input) // tolerate malformed input

	body, err := json.Marshal(map[string]string{
		"event":             event,
		"claude_session_id": input.SessionID,
		"transcript_path":   input.TranscriptPath,
		"message":           input.Message,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%d/api/sessions/%s/hook-event", port, sessionID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

// RunPermissionRequest handles the PermissionRequest hook: it forwards the
// pending tool call to the local server, which long-polls the remote user for
// an allow/deny decision, then writes the corresponding hook decision JSON to
// stdout. On error or an unrecognized decision it writes nothing, so Claude
// Code falls back to its normal TUI permission dialog (fail-open).
func RunPermissionRequest(sessionID string, port int, keyFile string, stdin io.Reader, stdout io.Writer) error {
	apiKey, err := readAPIKey(keyFile)
	if err != nil {
		return err
	}

	var input struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	raw, _ := io.ReadAll(io.LimitReader(stdin, 1<<20))
	_ = json.Unmarshal(raw, &input) // tolerate malformed input

	var toolInputFields struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(input.ToolInput, &toolInputFields)

	body, err := json.Marshal(map[string]any{
		"tool_name":   input.ToolName,
		"description": toolInputFields.Description,
		"input":       input.ToolInput,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%d/api/sessions/%s/permission-request", port, sessionID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Must outlast the server's 5-minute permission long-poll.
	client := &http.Client{Timeout: 5*time.Minute + 30*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}

	var behavior, message string
	switch result.Decision {
	case "allow", "allow_always":
		behavior = "allow"
	case "deny":
		behavior = "deny"
		message = "Denied by user via Claude Controller"
		if result.Reason != "" {
			message += " (" + result.Reason + ")"
		}
	default:
		return nil // unknown decision: stay silent, TUI dialog takes over
	}

	decision := map[string]any{"behavior": behavior}
	if message != "" {
		decision["message"] = message
	}
	out, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision":      decision,
		},
	})
	if err != nil {
		return err
	}
	_, err = stdout.Write(out)
	return err
}
