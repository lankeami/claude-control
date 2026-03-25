package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type slashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "builtin", "project", "user"
	HasArg      bool   `json:"hasArg"`
	ArgHint     string `json:"argHint,omitempty"`
}

var builtinCommands = []slashCommand{
	{Name: "/clear", Description: "Clear chat display", Source: "builtin"},
	{Name: "/compact", Description: "Compact conversation context", Source: "builtin"},
	{Name: "/cost", Description: "Show session cost info", Source: "builtin"},
	{Name: "/help", Description: "Show available commands", Source: "builtin"},
	{Name: "/resume", Description: "Continue a previous CLI session", Source: "builtin"},
}

type commandMeta struct {
	Name         string
	Description  string
	ArgumentHint string
}

// parseFrontmatter extracts YAML frontmatter fields and body from markdown content.
// Returns empty meta if no frontmatter delimiters found.
func parseFrontmatter(content string) (commandMeta, string) {
	if !strings.HasPrefix(content, "---\n") {
		return commandMeta{}, content
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return commandMeta{}, content
	}
	fm := content[4 : 4+end]
	body := strings.TrimLeft(content[4+end+4:], "\n")

	var meta commandMeta
	for _, line := range strings.Split(fm, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "name":
			meta.Name = v
		case "description":
			meta.Description = v
		case "argument-hint":
			meta.ArgumentHint = v
		}
	}
	return meta, body
}

// discoverCommands walks a directory for .md command files and returns slash commands.
func discoverCommands(dir, source string) []slashCommand {
	var cmds []slashCommand
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		meta, _ := parseFrontmatter(string(data))

		name := meta.Name
		if name == "" {
			rel, _ := filepath.Rel(dir, path)
			name = strings.TrimSuffix(rel, ".md")
			name = strings.ReplaceAll(name, string(filepath.Separator), ":")
		}

		cmds = append(cmds, slashCommand{
			Name:        "/" + name,
			Description: meta.Description,
			Source:      source,
			HasArg:      meta.ArgumentHint != "",
			ArgHint:     meta.ArgumentHint,
		})
		return nil
	})
	return cmds
}

func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
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

	seen := map[string]bool{}
	var all []slashCommand
	for _, cmd := range builtinCommands {
		all = append(all, cmd)
		seen[cmd.Name] = true
	}

	// Project-level commands (take priority over user-level)
	if sess.CWD != "" {
		for _, cmd := range discoverCommands(filepath.Join(sess.CWD, ".claude", "commands"), "project") {
			if !seen[cmd.Name] {
				all = append(all, cmd)
				seen[cmd.Name] = true
			}
		}
	}

	// User-level commands
	configDir, err := claudeConfigDir()
	if err == nil {
		for _, cmd := range discoverCommands(filepath.Join(configDir, "commands"), "user") {
			if !seen[cmd.Name] {
				all = append(all, cmd)
				seen[cmd.Name] = true
			}
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(all)
}

func (s *Server) handleCommandContent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name query parameter required", http.StatusBadRequest)
		return
	}
	name = strings.TrimPrefix(name, "/")

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

	// Convert command name to file path (: → path separator)
	relPath := strings.ReplaceAll(name, ":", string(filepath.Separator)) + ".md"

	// Search project-level first, then user-level
	searchDirs := []string{}
	if sess.CWD != "" {
		searchDirs = append(searchDirs, filepath.Join(sess.CWD, ".claude", "commands"))
	}
	if configDir, err := claudeConfigDir(); err == nil {
		searchDirs = append(searchDirs, filepath.Join(configDir, "commands"))
	}

	for _, dir := range searchDirs {
		fpath := filepath.Join(dir, relPath)
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		_, body := parseFrontmatter(string(data))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": body})
		return
	}

	http.Error(w, "command not found", http.StatusNotFound)
}
