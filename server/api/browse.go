package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type dirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"is_git_repo"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
			return
		}
		dirPath = home
	}

	// Resolve to absolute and clean
	dirPath, err := filepath.Abs(dirPath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		http.Error(w, "path is not a directory", http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusForbidden)
		return
	}

	var dirs []dirEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip hidden directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(dirPath, name)
		gitDir := filepath.Join(fullPath, ".git")
		_, gitErr := os.Stat(gitDir)
		dirs = append(dirs, dirEntry{
			Name:      name,
			Path:      fullPath,
			IsGitRepo: gitErr == nil,
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current": dirPath,
		"entries": dirs,
	})
}
