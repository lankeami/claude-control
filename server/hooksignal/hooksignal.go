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

// Run reads the Claude Code hook payload from stdin and POSTs a hook-event to
// the local server. Callers must treat errors as non-fatal: a broken relay
// must never block Claude (main.go ignores the returned error and exits 0).
func Run(event, sessionID string, port int, keyFile string, stdin io.Reader) error {
	if keyFile == "" {
		keyFile = os.Getenv("CLAUDE_CONTROLLER_KEY_FILE")
	}
	if keyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		keyFile = filepath.Join(home, ".claude-controller", "api.key")
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("read api key: %w", err)
	}
	apiKey := strings.TrimSpace(string(keyData))

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
