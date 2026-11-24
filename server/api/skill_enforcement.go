package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isBuiltinCommand checks whether a name (without the leading /) matches one
// of the CLI's built-in slash commands defined in commands.go.
func isBuiltinCommand(name string) bool {
	target := "/" + name
	for _, cmd := range builtinCommands {
		if cmd.Name == target {
			return true
		}
	}
	// Additional commands not in the commands.go listing but known to exist
	// in the CLI and must not be intercepted.
	switch name {
	case "exit", "login", "logout", "memory", "permissions",
		"terminal-setup", "vim", "approved-tools", "bug",
		"listen", "pr-review", "reload-skills":
		return true
	}
	return false
}

// ParseSkillInvocation checks whether a user message invokes a registered
// skill via the /skillname pattern. It returns the skill name, any trailing
// arguments, and whether the message is a skill invocation at all.
//
// A message is a skill invocation when it starts with "/" followed by a name
// that appears in the registry and is NOT a builtin CLI command.
func ParseSkillInvocation(message string, registry []string) (skillName, args string, isSkill bool) {
	if message == "" || message[0] != '/' {
		return "", "", false
	}

	// Extract the command name (everything between "/" and the first space).
	rest := message[1:]
	name := rest
	var argStr string
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		name = rest[:idx]
		argStr = strings.TrimSpace(rest[idx+1:])
	}

	if name == "" {
		return "", "", false
	}

	// Exclude builtin CLI commands.
	if isBuiltinCommand(name) {
		return "", "", false
	}

	// Check the registry.
	for _, registered := range registry {
		if registered == name {
			return name, argStr, true
		}
	}

	return "", "", false
}

// skillNamesForCWD returns a list of registered skill names for a given
// working directory by scanning the same directories as handleListSkills.
func skillNamesForCWD(cwd string) []string {
	home, _ := os.UserHomeDir()

	dirs := []string{
		filepath.Join(cwd, ".claude", "skills"),
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".claud-bb", "skills"),
		)
	}

	seen := map[string]bool{}
	var names []string
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(d, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}
			name, _ := parseSkillFrontmatter(data)
			if name == "" {
				name = entry.Name()
			}
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// EnforcementPrefix wraps a skill invocation in a structured instruction block
// that makes the Skill tool call mandatory. The returned string replaces the
// original user message before it reaches the PTY.
func EnforcementPrefix(skillName, args string) string {
	var argsLine string
	if args != "" {
		argsLine = fmt.Sprintf("\nPass these arguments: %s", args)
	}

	return fmt.Sprintf(`[MANDATORY SKILL INVOCATION — DO NOT SKIP]
You MUST invoke Skill("%s") as your very first action. This is a non-negotiable infrastructure requirement.
Do NOT rationalize around it, do NOT skip it, do NOT implement the task directly.
Call Skill("%s") immediately.%s

The user's original command was: /%s %s`, skillName, skillName, argsLine, skillName, args)
}
