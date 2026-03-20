package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeProjectDir encodes a CWD to match Claude Code's project directory naming.
// Replaces /, _, and . with -.
func claudeProjectDir(cwd string) string {
	r := strings.NewReplacer("/", "-", "_", "-", ".", "-")
	return r.Replace(cwd)
}

func sessionsIndexPath(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "projects", claudeProjectDir(cwd), "sessions-index.json"), nil
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

	indexPath, err := sessionsIndexPath(sess.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "no CLI sessions found for this project", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("read sessions index: %v", err), http.StatusInternalServerError)
		}
		return
	}

	var index sessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		http.Error(w, fmt.Sprintf("parse sessions index: %v", err), http.StatusInternalServerError)
		return
	}

	var results []resumableSession
	for _, e := range index.Entries {
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
