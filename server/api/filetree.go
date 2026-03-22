package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type fileTreeEntry struct {
	Path      string `json:"path"`
	GitStatus string `json:"git_status,omitempty"` // "M", "A", "D", "?", "R", "" (unmodified)
	Action    string `json:"action,omitempty"`      // "edit", "write", "read" (from session)
}

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		cwd = sess.ProjectPath
	}
	if cwd == "" {
		http.Error(w, `{"error":"session has no working directory"}`, http.StatusBadRequest)
		return
	}

	// Get all tracked files from git
	allFiles, err := gitListFiles(cwd)
	if err != nil {
		http.Error(w, `{"error":"failed to list files: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Get git status for changed files
	statusMap, err := gitStatus(cwd)
	if err != nil {
		// Non-fatal: continue without status
		statusMap = map[string]string{}
	}

	// Get session-touched files
	actionMap := map[string]string{}
	if sess.Mode == "managed" {
		dbFiles, err := s.store.ListSessionFiles(sessionID)
		if err == nil {
			for _, f := range dbFiles {
				rel, err := filepath.Rel(cwd, f.FilePath)
				if err == nil {
					actionMap[rel] = f.Action
				}
				// Also store absolute path mapping
				actionMap[f.FilePath] = f.Action
			}
		}
	} else if sess.TranscriptPath != "" {
		entries, err := extractFilesFromTranscript(sess.TranscriptPath)
		if err == nil {
			for _, e := range entries {
				rel, err := filepath.Rel(cwd, e.Path)
				if err == nil {
					actionMap[rel] = e.Action
				}
				actionMap[e.Path] = e.Action
			}
		}
	}

	// Build response
	var entries []fileTreeEntry
	for _, relPath := range allFiles {
		absPath := filepath.Join(cwd, relPath)
		entry := fileTreeEntry{
			Path:      absPath,
			GitStatus: statusMap[relPath],
		}
		// Check action by relative or absolute path
		if action, ok := actionMap[relPath]; ok {
			entry.Action = action
		} else if action, ok := actionMap[absPath]; ok {
			entry.Action = action
		}
		entries = append(entries, entry)
	}

	if entries == nil {
		entries = []fileTreeEntry{}
	}

	// Get git branch and summary info
	gitInfo := gitSummary(cwd)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"files": entries,
		"cwd":   cwd,
		"git":   gitInfo,
	})
}

func (s *Server) handleFileDiff(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")
	if filePath == "" || sessionID == "" {
		http.Error(w, `{"error":"path and session_id required"}`, http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		cwd = sess.ProjectPath
	}
	if cwd == "" {
		http.Error(w, `{"error":"session has no working directory"}`, http.StatusBadRequest)
		return
	}

	// Ensure the file is within the session's working directory
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, cwd) {
		http.Error(w, `{"error":"file outside session directory"}`, http.StatusForbidden)
		return
	}

	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		http.Error(w, `{"error":"failed to resolve path"}`, http.StatusInternalServerError)
		return
	}

	// Try staged + unstaged diff first, then check for untracked files
	cmd := exec.Command("git", "diff", "HEAD", "--", relPath)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		// Might be an untracked file or only staged changes — try just `git diff`
		cmd2 := exec.Command("git", "diff", "--", relPath)
		cmd2.Dir = cwd
		out2, _ := cmd2.Output()
		if len(out2) > 0 {
			out = out2
		}
	}

	// If still no diff, check if it's a new untracked file — show full content as added
	if len(out) == 0 {
		cmd3 := exec.Command("git", "status", "--porcelain", "--", relPath)
		cmd3.Dir = cwd
		statusOut, _ := cmd3.Output()
		statusLine := strings.TrimSpace(string(statusOut))
		if strings.HasPrefix(statusLine, "??") || strings.HasPrefix(statusLine, "A") {
			// Untracked or newly added — read file and format as "all new"
			content, readErr := os.ReadFile(absPath)
			if readErr == nil && len(content) > 0 {
				// Cap at 1MB
				if len(content) > 1024*1024 {
					content = content[:1024*1024]
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"diff":    "",
					"content": string(content),
					"status":  "new",
				})
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"diff":   string(out),
		"status": "modified",
	})
}

// gitListFiles returns all files known to git (tracked + untracked non-ignored)
func gitListFiles(cwd string) ([]string, error) {
	// Get tracked files
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	fileSet := map[string]struct{}{}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fileSet[line] = struct{}{}
		files = append(files, line)
	}

	// Get all untracked files (including ignored ones like .env)
	cmd2 := exec.Command("git", "ls-files", "--others")
	cmd2.Dir = cwd
	out2, err := cmd2.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out2)), "\n") {
			if line == "" {
				continue
			}
			if _, exists := fileSet[line]; !exists {
				fileSet[line] = struct{}{}
				files = append(files, line)
			}
		}
	}

	return files, nil
}

type gitSummaryInfo struct {
	Branch    string `json:"branch"`
	RepoName  string `json:"repo_name,omitempty"`
	RepoURL   string `json:"repo_url,omitempty"`
	Modified  int    `json:"modified"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Untracked int    `json:"untracked"`
	Staged    int    `json:"staged"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
}

func gitSummary(cwd string) gitSummaryInfo {
	info := gitSummaryInfo{}

	// Get branch name
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	if out, err := cmd.Output(); err == nil {
		info.Branch = strings.TrimSpace(string(out))
	}

	// Get remote URL and derive repo name + browsable URL
	cmdRemote := exec.Command("git", "remote", "get-url", "origin")
	cmdRemote.Dir = cwd
	if out, err := cmdRemote.Output(); err == nil {
		rawURL := strings.TrimSpace(string(out))
		info.RepoName, info.RepoURL = parseGitRemoteURL(rawURL)
	}

	// Get ahead/behind from tracking branch
	cmd2 := exec.Command("git", "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	cmd2.Dir = cwd
	if out, err := cmd2.Output(); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) == 2 {
			if n, err := parseInt(parts[0]); err == nil {
				info.Behind = n
			}
			if n, err := parseInt(parts[1]); err == nil {
				info.Ahead = n
			}
		}
	}

	// Parse porcelain status for file counts
	cmd3 := exec.Command("git", "status", "--porcelain")
	cmd3.Dir = cwd
	if out, err := cmd3.Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if len(line) < 3 {
				continue
			}
			x, y := line[0], line[1]
			// Staged changes (index column)
			if x == 'M' || x == 'A' || x == 'D' || x == 'R' {
				info.Staged++
			}
			// Working tree changes
			switch {
			case x == '?' && y == '?':
				info.Untracked++
			case y == 'M':
				info.Modified++
			case y == 'D':
				info.Deleted++
			case y == 'A':
				info.Added++
			}
		}
	}

	return info
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// parseGitRemoteURL converts a git remote URL (SSH or HTTPS) into a
// human-readable repo name (owner/repo) and a browsable HTTPS URL.
func parseGitRemoteURL(rawURL string) (name, browseURL string) {
	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		// git@github.com:owner/repo.git -> github.com/owner/repo
		trimmed := strings.TrimPrefix(rawURL, "git@")
		trimmed = strings.TrimSuffix(trimmed, ".git")
		trimmed = strings.Replace(trimmed, ":", "/", 1)
		parts := strings.SplitN(trimmed, "/", 3)
		if len(parts) == 3 {
			name = parts[1] + "/" + parts[2]
			browseURL = "https://" + trimmed
		}
		return
	}

	// HTTPS format: https://github.com/owner/repo.git
	if strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://") {
		trimmed := strings.TrimSuffix(rawURL, ".git")
		// Extract owner/repo from URL path
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 5 {
			name = parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
		browseURL = trimmed
		return
	}

	return "", ""
}

// gitStatus returns a map of relative path -> status code
func gitStatus(cwd string) (map[string]string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		path := strings.TrimSpace(line[3:])

		// Handle renames: "R  old -> new"
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}

		switch {
		case xy[0] == '?' || xy[1] == '?':
			result[path] = "?"
		case xy[0] == 'A' || xy[1] == 'A':
			result[path] = "A"
		case xy[0] == 'D' || xy[1] == 'D':
			result[path] = "D"
		case xy[0] == 'R' || xy[1] == 'R':
			result[path] = "R"
		case xy[0] == 'M' || xy[1] == 'M':
			result[path] = "M"
		}
	}

	return result, nil
}
