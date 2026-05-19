package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultUsageUpstreamURL = "https://api.anthropic.com/api/oauth/usage"

// UsageCache holds cached usage data with timestamp for TTL checking.
type UsageCache struct {
	Data      []byte    // Raw JSON from Anthropic
	Timestamp time.Time
}

const UsageCacheTTL = 60 * time.Second // Cache for 60 seconds

func (s *Server) getUsageUpstreamURL() string {
	if s.usageUpstreamURL != "" {
		return s.usageUpstreamURL
	}
	return defaultUsageUpstreamURL
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	// Extract sessionId from query params or cookies
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		// Try cookie fallback (if browser sets one)
		if cookie, err := r.Cookie("sessionId"); err == nil {
			sessionID = cookie.Value
		}
	}

	// Verify session exists (user is authorized)
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"missing_session"}`))
		return
	}

	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_session"}`))
		return
	}

	// Check cache first (server-side caching, not client-side)
	s.usageCacheMu.RLock()
	if s.usageCache != nil && time.Since(s.usageCache.Timestamp) < UsageCacheTTL {
		cachedData := s.usageCache.Data
		s.usageCacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Write(cachedData)
		return
	}
	s.usageCacheMu.RUnlock()

	// Fetch from Anthropic (server-to-server, using Keychain token)
	token := s.resolveOAuthToken()
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"no_oauth_token"}`))
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, s.getUsageUpstreamURL(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Cache the result
	s.usageCacheMu.Lock()
	s.usageCache = &UsageCache{
		Data:      body,
		Timestamp: time.Now(),
	}
	s.usageCacheMu.Unlock()

	// Return to client
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
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
