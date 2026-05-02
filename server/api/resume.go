package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// claudeProjectDir encodes a CWD to match Claude Code's project directory naming.
// Replaces /, _, and . with -. Handles Windows paths (backslashes, drive letter colon).
func claudeProjectDir(cwd string) string {
	// Normalize Windows backslashes to forward slashes
	cwd = filepath.ToSlash(cwd)
	// Strip drive letter colon (e.g. "C:/Users/..." -> "C/Users/...")
	if len(cwd) >= 2 && cwd[1] == ':' {
		cwd = cwd[:1] + cwd[2:]
	}
	r := strings.NewReplacer("/", "-", "_", "-", ".", "-")
	return r.Replace(cwd)
}

func claudeConfigDir() (string, error) {
	// Check CLAUDE_CONFIG_DIR directly first
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	// Also check CLAUDE_ENV (comma-separated KEY=VAL pairs passed to managed sessions)
	for _, pair := range strings.Split(os.Getenv("CLAUDE_ENV"), ",") {
		if k, v, ok := strings.Cut(pair, "="); ok {
			if strings.TrimSpace(k) == "CLAUDE_CONFIG_DIR" {
				return strings.TrimSpace(v), nil
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

func claudeProjectsDir(cwd string) (string, error) {
	configDir, err := claudeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "projects", claudeProjectDir(cwd)), nil
}

type sessionsIndex struct {
	Version int            `json:"version"`
	Entries []sessionEntry `json:"entries"`
}

type sessionEntry struct {
	SessionID    string `json:"sessionId"`
	FirstPrompt  string `json:"firstPrompt"`
	Summary      string `json:"summary"`
	MessageCount int    `json:"messageCount"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"gitBranch"`
	IsSidechain  bool   `json:"isSidechain"`
}

type resumableSession struct {
	SessionID    string `json:"session_id"`
	Summary      string `json:"summary"`
	FirstPrompt  string `json:"first_prompt"`
	MessageCount int    `json:"message_count"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"git_branch"`
}

// loadSessionsFromIndex reads sessions-index.json if it exists.
func loadSessionsFromIndex(projectDir string) ([]sessionEntry, error) {
	indexPath := filepath.Join(projectDir, "sessions-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var index sessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse sessions index: %w", err)
	}
	return index.Entries, nil
}

// loadSessionsFromJSONL scans *.jsonl files directly (fallback when no sessions-index.json).
// Reads the first user message from each file for the prompt and uses file mtime as modified time.
func loadSessionsFromJSONL(projectDir string) ([]sessionEntry, error) {
	matches, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var entries []sessionEntry
	for _, fpath := range matches {
		base := filepath.Base(fpath)
		sessionID := strings.TrimSuffix(base, ".jsonl")

		info, err := os.Stat(fpath)
		if err != nil {
			continue
		}

		prompt := extractFirstPrompt(fpath)
		if prompt == "" {
			prompt = "No prompt"
		}

		entries = append(entries, sessionEntry{
			SessionID:    sessionID,
			FirstPrompt:  prompt,
			Summary:      "",
			MessageCount: 0,
			Created:      info.ModTime().UTC().Format(time.RFC3339),
			Modified:     info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return entries, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// extractRecentMessages reads a JSONL file and returns the last N user/assistant messages.
func extractRecentMessages(fpath string, n int) []chatMessage {
	f, err := os.Open(fpath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []chatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "user" && line.Type != "assistant" {
			continue
		}
		text := extractTextContent(line.Message.Content)
		if text == "" {
			continue
		}
		all = append(all, chatMessage{Role: line.Type, Content: text})
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// extractTextContent pulls text from either an array of content blocks or a plain string.
func extractTextContent(raw json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// extractFirstPrompt reads a JSONL file and returns the text of the first user message.
func extractFirstPrompt(fpath string) string {
	f, err := os.Open(fpath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "user" {
			continue
		}
		if text := extractTextContent(line.Message.Content); text != "" {
			return text
		}
	}
	return ""
}

func (s *Server) handleResumableList(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}

	projectDir, err := claudeProjectsDir(sess.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Try sessions-index.json first, fall back to scanning JSONL files
	entries, err := loadSessionsFromIndex(projectDir)
	if err != nil {
		entries, err = loadSessionsFromJSONL(projectDir)
		if err != nil || len(entries) == 0 {
			http.Error(w, "no CLI sessions found for this project", http.StatusNotFound)
			return
		}
	}

	var results []resumableSession
	for _, e := range entries {
		if e.IsSidechain {
			continue
		}
		// Filter out the currently active claude session
		if sess.ClaudeSessionID != "" && e.SessionID == sess.ClaudeSessionID {
			continue
		}
		prompt := e.FirstPrompt
		if len(prompt) > 80 {
			prompt = prompt[:80] + "…"
		}
		results = append(results, resumableSession{
			SessionID:    e.SessionID,
			Summary:      e.Summary,
			FirstPrompt:  prompt,
			MessageCount: e.MessageCount,
			Created:      e.Created,
			Modified:     e.Modified,
			GitBranch:    e.GitBranch,
		})
	}

	// Sort by modified descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Modified > results[j].Modified
	})

	// Limit to 20
	if len(results) > 20 {
		results = results[:20]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"sessions": results})
}

func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}

	// Validate that session_id exists in the resumable list
	projectDir, err := claudeProjectsDir(sess.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries, err := loadSessionsFromIndex(projectDir)
	if err != nil {
		entries, _ = loadSessionsFromJSONL(projectDir)
	}
	found := false
	for _, e := range entries {
		if e.SessionID == req.SessionID && !e.IsSidechain {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "session_id not found in resumable sessions", http.StatusBadRequest)
		return
	}

	// Teardown any running process
	if s.manager.IsRunning(sessionID) {
		if err := s.manager.Teardown(sessionID, 5*time.Second); err != nil {
			http.Error(w, fmt.Sprintf("failed to stop running process: %v", err), http.StatusConflict)
			return
		}
	}

	// Atomically: set claude_session_id, initialized, delete old messages, set idle
	if err := s.store.ResumeSession(sessionID, req.SessionID); err != nil {
		http.Error(w, fmt.Sprintf("failed to resume session: %v", err), http.StatusInternalServerError)
		return
	}

	// Re-fetch updated session
	updated, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Read recent messages from the JSONL file for context playback
	var recentMessages []chatMessage
	jsonlPath := filepath.Join(projectDir, req.SessionID+".jsonl")
	if _, err := os.Stat(jsonlPath); err == nil {
		recentMessages = extractRecentMessages(jsonlPath, 6)
		log.Printf("resume: loaded %d recent messages from %s", len(recentMessages), jsonlPath)
		// Persist to DB so they survive session switches
		for _, m := range recentMessages {
			_, _ = s.store.CreateMessage(sessionID, m.Role, m.Content)
		}
	} else {
		log.Printf("resume: JSONL not found at %s: %v", jsonlPath, err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"session":         updated,
		"recent_messages": recentMessages,
	})
}
