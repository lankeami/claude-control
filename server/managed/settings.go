package managed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookMatcher struct {
	Hooks []hookCmd `json:"hooks"`
}

// WriteSessionSettings generates a Claude Code settings file for a managed
// interactive session: turn-lifecycle hooks pointing back at the controller
// server, plus permission allow rules mapped from the session's allowed tools
// (parity with the legacy --allowedTools flag).
func WriteSessionSettings(dir, binaryPath, sessionID string, port int, allowedToolsJSON string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	mk := func(event string) []hookMatcher {
		cmd := fmt.Sprintf("%q hook-signal --event %s --session-id %s --port %d", binaryPath, event, sessionID, port)
		return []hookMatcher{{Hooks: []hookCmd{{Type: "command", Command: cmd}}}}
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": mk("session_start"),
			"Stop":         mk("stop"),
			"Notification": mk("notification"),
		},
	}
	var tools []string
	if allowedToolsJSON != "" {
		_ = json.Unmarshal([]byte(allowedToolsJSON), &tools)
	}
	if len(tools) > 0 {
		settings["permissions"] = map[string]any{"allow": tools}
	}
	path := filepath.Join(dir, "settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

// SessionDir returns the per-session scratch directory for generated files
// (settings, MCP config). Lives under the user's home so it isn't
// world-readable like /tmp.
func SessionDir(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "claude-controller", "sessions", sessionID)
	}
	return filepath.Join(home, ".claude-controller", "sessions", sessionID)
}
