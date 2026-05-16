package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultUsageUpstreamURL = "https://api.anthropic.com/api/oauth/usage"

func (s *Server) getUsageUpstreamURL() string {
	if s.usageUpstreamURL != "" {
		return s.usageUpstreamURL
	}
	return defaultUsageUpstreamURL
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	token := s.resolveOAuthToken()
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"no_token"}`))
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.getUsageUpstreamURL(), nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := client.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream_error","status":%d}`, resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// resolveOAuthToken tries the macOS Keychain first, then CLAUDE_OAUTH_TOKEN env var.
func (s *Server) resolveOAuthToken() string {
	if !s.skipKeychain {
		if token := tokenFromKeychain(); token != "" {
			return token
		}
	}
	return os.Getenv("CLAUDE_OAUTH_TOKEN")
}

// tokenFromKeychain runs `security find-generic-password` and parses the JSON result.
func tokenFromKeychain() string {
	username := os.Getenv("USER")
	if username == "" {
		return ""
	}
	cmd := exec.Command("/usr/bin/security",
		"find-generic-password",
		"-a", username,
		"-s", "Claude Code-credentials",
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return ""
	}

	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return ""
	}
	return creds.ClaudeAiOauth.AccessToken
}
