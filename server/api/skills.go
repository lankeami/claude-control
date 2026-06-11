package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type skillResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dir         string `json:"dir"` // which directory it came from: "project", "user", or "bb"
}

// handleListSkills scans known skill directories and returns parsed skill metadata.
// Directories scanned (in order):
//   - <session-cwd>/.claude/skills/   (project-level)
//   - ~/.claude/skills/               (user-level)
//   - ~/.claud-bb/skills/             (bb-level)
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}

	// For managed sessions use CWD; for hook sessions use ProjectPath.
	cwd := sess.CWD
	if cwd == "" {
		cwd = sess.ProjectPath
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	dirs := []struct {
		path  string
		label string
	}{
		{filepath.Join(cwd, ".claude", "skills"), "project"},
		{filepath.Join(home, ".claude", "skills"), "user"},
		{filepath.Join(home, ".claud-bb", "skills"), "bb"},
	}

	seen := map[string]bool{} // deduplicate by skill name
	skills := []skillResponse{}

	for _, d := range dirs {
		entries, err := os.ReadDir(d.path)
		if err != nil {
			continue // directory doesn't exist or isn't readable — skip silently
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(d.path, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}
			name, desc := parseSkillFrontmatter(data)
			if name == "" {
				name = entry.Name()
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			skills = append(skills, skillResponse{Name: name, Description: desc, Dir: d.label})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skills)
}

// parseSkillFrontmatter extracts name and description from YAML frontmatter.
// Frontmatter is delimited by "---" lines. Returns empty strings on any parse error.
func parseSkillFrontmatter(data []byte) (name, description string) {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return
	}
	// Find closing ---
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, ":"); ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			// Strip surrounding quotes if present
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			}
			switch k {
			case "name":
				name = v
			case "description":
				description = v
			}
		}
	}
	return
}
