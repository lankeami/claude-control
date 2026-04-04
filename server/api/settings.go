package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jaychinthrajah/claude-controller/server/managed"
)

type Shortcut struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type settingsPayload struct {
	Port                   string     `json:"port"`
	NgrokAuthtoken         string     `json:"ngrok_authtoken"`
	ClaudeBin              string     `json:"claude_bin"`
	ClaudeArgs             string     `json:"claude_args"`
	ClaudeEnv              string     `json:"claude_env"`
	CompactEveryNContinues string     `json:"compact_every_n_continues"`
	GithubToken            string     `json:"github_token"`
	Shortcuts              []Shortcut `json:"shortcuts"`
}

func shortcutsPath(envPath string) string {
	return strings.TrimSuffix(envPath, ".env") + "shortcuts.json"
}

func readShortcuts(envPath string) []Shortcut {
	data, err := os.ReadFile(shortcutsPath(envPath))
	if err != nil {
		return []Shortcut{}
	}
	var shortcuts []Shortcut
	if err := json.Unmarshal(data, &shortcuts); err != nil {
		return []Shortcut{}
	}
	return shortcuts
}

func writeShortcuts(envPath string, shortcuts []Shortcut) error {
	if shortcuts == nil {
		shortcuts = []Shortcut{}
	}
	data, err := json.MarshalIndent(shortcuts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(shortcutsPath(envPath), data, 0600)
}

func (s *Server) handleSettingsExists(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(s.envPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"exists": err == nil})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	vals := readEnvFile(s.envPath)

	// Mask secrets
	for _, key := range []string{"NGROK_AUTHTOKEN", "GITHUB_TOKEN"} {
		if tok := vals[key]; len(tok) > 4 {
			vals[key] = "****" + tok[len(tok)-4:]
		} else if tok != "" {
			vals[key] = "****"
		}
	}

	resp := settingsPayload{
		Port:                   vals["PORT"],
		NgrokAuthtoken:         vals["NGROK_AUTHTOKEN"],
		ClaudeBin:              vals["CLAUDE_BIN"],
		ClaudeArgs:             vals["CLAUDE_ARGS"],
		ClaudeEnv:              vals["CLAUDE_ENV"],
		CompactEveryNContinues: vals["COMPACT_EVERY_N_CONTINUES"],
		GithubToken:            vals["GITHUB_TOKEN"],
	}
	resp.Shortcuts = readShortcuts(s.envPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var payload settingsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate port
	if payload.Port != "" {
		p, err := strconv.Atoi(payload.Port)
		if err != nil || p < 1 || p > 65535 {
			http.Error(w, "PORT must be a number between 1 and 65535", http.StatusBadRequest)
			return
		}
	}

	// Read current values for comparison and sentinel handling
	current := readEnvFile(s.envPath)

	// Handle masked secret sentinels
	if strings.HasPrefix(payload.NgrokAuthtoken, "****") {
		payload.NgrokAuthtoken = current["NGROK_AUTHTOKEN"]
	}
	if strings.HasPrefix(payload.GithubToken, "****") {
		payload.GithubToken = current["GITHUB_TOKEN"]
	}

	// Check if restart-requiring fields changed
	restartRequired := (payload.Port != current["PORT"]) ||
		(payload.NgrokAuthtoken != current["NGROK_AUTHTOKEN"])

	// Write .env file atomically
	content := formatEnvFile(payload)
	tmpPath := s.envPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		http.Error(w, fmt.Sprintf("write error: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, s.envPath); err != nil {
		http.Error(w, fmt.Sprintf("rename error: %v", err), http.StatusInternalServerError)
		return
	}
	if err := writeShortcuts(s.envPath, payload.Shortcuts); err != nil {
		http.Error(w, fmt.Sprintf("shortcuts write error: %v", err), http.StatusInternalServerError)
		return
	}

	// Hot-reload manager config
	if s.manager != nil {
		claudeBin := payload.ClaudeBin
		if claudeBin == "" {
			claudeBin = "claude"
		}
		var claudeEnv []string
		if payload.ClaudeEnv != "" {
			claudeEnv = strings.Split(payload.ClaudeEnv, ",")
		}
		s.manager.UpdateConfig(managed.Config{
			ClaudeBin:  claudeBin,
			ClaudeArgs: strings.Fields(payload.ClaudeArgs),
			ClaudeEnv:  claudeEnv,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"restart_required": restartRequired})
}

func readEnvFile(path string) map[string]string {
	vals := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return vals
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return vals
}

func formatEnvFile(p settingsPayload) string {
	var b strings.Builder
	b.WriteString("# Server\n")
	if p.Port != "" {
		b.WriteString("PORT=" + p.Port + "\n")
	}
	if p.NgrokAuthtoken != "" {
		b.WriteString("NGROK_AUTHTOKEN=" + p.NgrokAuthtoken + "\n")
	}
	b.WriteString("\n# Managed session config\n")
	if p.ClaudeBin != "" {
		b.WriteString("CLAUDE_BIN=" + p.ClaudeBin + "\n")
	}
	if p.ClaudeArgs != "" {
		b.WriteString("CLAUDE_ARGS=" + p.ClaudeArgs + "\n")
	}
	if p.ClaudeEnv != "" {
		b.WriteString("CLAUDE_ENV=" + p.ClaudeEnv + "\n")
	}
	if p.CompactEveryNContinues != "" && p.CompactEveryNContinues != "0" {
		b.WriteString("COMPACT_EVERY_N_CONTINUES=" + p.CompactEveryNContinues + "\n")
	}
	b.WriteString("\n# GitHub\n")
	if p.GithubToken != "" {
		b.WriteString("GITHUB_TOKEN=" + p.GithubToken + "\n")
	}
	return b.String()
}
