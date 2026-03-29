package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var projectNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,253}[a-zA-Z0-9])?$`)

func isValidProjectName(name string) bool {
	return projectNameRegex.MatchString(name)
}

var defaultGitignore = `# OS
.DS_Store
Thumbs.db

# Environment
.env
.env.*

# IDE
.idea/
.vscode/

# Dependencies (common)
node_modules/
vendor/
__pycache__/
*.pyc
.venv/

# Build output
dist/
build/

# Logs
*.log
`

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentPath string `json:"parent_path"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ParentPath == "" || req.Name == "" {
		http.Error(w, "parent_path and name are required", http.StatusBadRequest)
		return
	}

	if !isValidProjectName(req.Name) {
		http.Error(w, "invalid project name: use letters, numbers, hyphens, dots, or underscores", http.StatusBadRequest)
		return
	}

	// Resolve parent path with symlink resolution
	absParent, err := filepath.Abs(req.ParentPath)
	if err != nil {
		http.Error(w, "invalid parent path", http.StatusBadRequest)
		return
	}
	absParent, err = filepath.EvalSymlinks(absParent)
	if err != nil {
		http.Error(w, "parent path does not exist", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absParent)
	if err != nil || !info.IsDir() {
		http.Error(w, "parent path is not a directory", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(absParent, req.Name)

	// Check target doesn't already exist
	if _, err := os.Stat(fullPath); err == nil {
		http.Error(w, "directory already exists", http.StatusConflict)
		return
	}

	// Create directory (not MkdirAll — fail atomically if parent missing)
	if err := os.Mkdir(fullPath, 0755); err != nil {
		http.Error(w, "failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Git init
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = fullPath
	if out, err := gitCmd.CombinedOutput(); err != nil {
		os.RemoveAll(fullPath)
		http.Error(w, fmt.Sprintf("git init failed: %s", string(out)), http.StatusInternalServerError)
		return
	}

	// Write .gitignore
	if err := os.WriteFile(filepath.Join(fullPath, ".gitignore"), []byte(defaultGitignore), 0644); err != nil {
		os.RemoveAll(fullPath)
		http.Error(w, "failed to write .gitignore", http.StatusInternalServerError)
		return
	}

	// Create managed session
	sess, err := s.store.CreateManagedSession(
		fullPath,
		`["Bash","Read","Edit","Write","Glob","Grep"]`,
		50,
		5.0,
		0,
	)
	if err != nil {
		os.RemoveAll(fullPath)
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "session already exists for this directory", http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}
