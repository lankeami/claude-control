package api

import (
	"os"
	"path/filepath"
)

// DiscoverSkillNames returns deduplicated skill names available for a session
// rooted at cwd. It scans three directories in priority order:
//
//  1. <cwd>/.claude/skills/   (project-level)
//  2. ~/.claude/skills/       (user-level)
//  3. ~/.claud-bb/skills/     (bb-level)
//
// Each directory is a flat list of subdirectories. A subdirectory is a skill
// when it contains a SKILL.md file. The skill name is the frontmatter "name"
// field if present, otherwise the directory name. The first occurrence of a
// name wins (project overrides user overrides bb).
func DiscoverSkillNames(cwd string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	dirs := []string{
		filepath.Join(cwd, ".claude", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".claud-bb", "skills"),
	}

	seen := map[string]bool{}
	var names []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // directory doesn't exist or isn't readable
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue // no SKILL.md — not a valid skill
			}
			name, _ := parseSkillFrontmatter(data)
			if name == "" {
				name = entry.Name()
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}

	return names
}
