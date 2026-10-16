package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// makeTarget represents a single Makefile target with metadata.
type makeTarget struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Line        int    `json:"line"` // 1-indexed line number in the Makefile
	FilePath    string `json:"filePath"`
}

// parseMakefileTargets reads a Makefile and extracts targets with their
// descriptions (## comments) and line numbers.
// Targets are matched by the pattern: <name>: [deps] [## description]
// It skips variable assignments, .PHONY, and internal targets (starting with _).
func parseMakefileTargets(path string) ([]makeTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Matches lines like:  target-name: deps ## Description text
	// Also matches:        target-name: ## Description text
	// Also matches:        target-name:
	targetRe := regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9._-]*)\s*:`)
	descRe := regexp.MustCompile(`##\s*(.+)$`)

	var targets []makeTarget
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip blank lines, comments, variable assignments, conditionals
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Skip variable assignments (=, :=, ?=, +=)
		if strings.Contains(line, "=") && !strings.Contains(line, ":") {
			continue
		}

		match := targetRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := match[1]

		// Skip special targets and internal targets
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		// Deduplicate
		if seen[name] {
			continue
		}
		seen[name] = true

		desc := ""
		if descMatch := descRe.FindStringSubmatch(line); descMatch != nil {
			desc = strings.TrimSpace(descMatch[1])
		}

		targets = append(targets, makeTarget{
			Name:        name,
			Description: desc,
			Line:        lineNum,
			FilePath:    path,
		})
	}

	return targets, scanner.Err()
}

// handleListMakeTargets returns make targets for the session's working directory.
// GET /api/sessions/{id}/make-targets
func (s *Server) handleListMakeTargets(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		cwd = sess.ProjectPath
	}
	if cwd == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]makeTarget{})
		return
	}

	makefilePath := filepath.Join(cwd, "Makefile")
	targets, err := parseMakefileTargets(makefilePath)
	if err != nil {
		// No Makefile or unreadable — return empty list, not an error
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]makeTarget{})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targets)
}
