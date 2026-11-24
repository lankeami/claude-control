package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillInvocation(t *testing.T) {
	registry := []string{"autoship", "git-issue-create", "git-ship-pr", "make"}

	tests := []struct {
		name      string
		message   string
		wantSkill string
		wantArgs  string
		wantIs    bool
	}{
		{
			name:      "simple skill invocation",
			message:   "/autoship",
			wantSkill: "autoship",
			wantArgs:  "",
			wantIs:    true,
		},
		{
			name:      "skill with args",
			message:   `/autoship "Add feature X"`,
			wantSkill: "autoship",
			wantArgs:  `"Add feature X"`,
			wantIs:    true,
		},
		{
			name:      "skill with hash args",
			message:   "/autoship #352",
			wantSkill: "autoship",
			wantArgs:  "#352",
			wantIs:    true,
		},
		{
			name:      "hyphenated skill name",
			message:   "/git-issue-create fix the bug",
			wantSkill: "git-issue-create",
			wantArgs:  "fix the bug",
			wantIs:    true,
		},
		{
			name:      "not a skill invocation (plain text)",
			message:   "please fix the bug",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "not in registry",
			message:   "/unknown-skill foo",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "empty message",
			message:   "",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "just a slash",
			message:   "/",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "empty registry",
			message:   "/autoship",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "case sensitive — exact match only",
			message:   "/Autoship",
			wantSkill: "",
			wantArgs:  "",
			wantIs:    false,
		},
		{
			name:      "skill name with trailing whitespace",
			message:   "/autoship   ",
			wantSkill: "autoship",
			wantArgs:  "",
			wantIs:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := registry
			if tt.name == "empty registry" {
				reg = nil
			}
			skill, args, isSkill := ParseSkillInvocation(tt.message, reg)
			if skill != tt.wantSkill {
				t.Errorf("skill=%q, want %q", skill, tt.wantSkill)
			}
			if args != tt.wantArgs {
				t.Errorf("args=%q, want %q", args, tt.wantArgs)
			}
			if isSkill != tt.wantIs {
				t.Errorf("isSkill=%v, want %v", isSkill, tt.wantIs)
			}
		})
	}
}

func TestEnforcementPrefix(t *testing.T) {
	tests := []struct {
		name       string
		skillName  string
		args       string
		wantSubstr []string // substrings the prefix must contain
	}{
		{
			name:      "autoship with args",
			skillName: "autoship",
			args:      "#352",
			wantSubstr: []string{
				`Skill("autoship")`,
				"#352",
				"MUST",
			},
		},
		{
			name:      "skill with no args",
			skillName: "git-ship-pr",
			args:      "",
			wantSubstr: []string{
				`Skill("git-ship-pr")`,
				"MUST",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnforcementPrefix(tt.skillName, tt.args)
			if result == "" {
				t.Fatal("EnforcementPrefix returned empty string")
			}
			for _, sub := range tt.wantSubstr {
				if !contains(result, sub) {
					t.Errorf("result missing %q;\ngot: %s", sub, result)
				}
			}
		})
	}
}

func TestBuiltinCommandsNotIntercepted(t *testing.T) {
	registry := []string{"autoship", "git-issue-create", "git-ship-pr", "make"}

	builtins := []string{
		"/compact",
		"/clear",
		"/exit",
		"/help",
		"/reload-skills",
		"/config",
		"/cost",
		"/doctor",
		"/init",
		"/login",
		"/logout",
		"/memory",
		"/model",
		"/permissions",
		"/review",
		"/status",
		"/terminal-setup",
		"/vim",
	}

	for _, cmd := range builtins {
		t.Run(cmd, func(t *testing.T) {
			_, _, isSkill := ParseSkillInvocation(cmd, registry)
			if isSkill {
				t.Errorf("builtin command %q was intercepted as a skill", cmd)
			}
		})
	}
}

func TestSkillNamesForCWD(t *testing.T) {
	dir := t.TempDir()

	// Create a project-level skill
	skillDir := filepath.Join(dir, ".claude", "skills", "autoship")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: autoship\n---\nBody"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another skill
	skillDir2 := filepath.Join(dir, ".claude", "skills", "deploy")
	if err := os.MkdirAll(skillDir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte("---\nname: deploy\n---\nBody"), 0644); err != nil {
		t.Fatal(err)
	}

	names := skillNamesForCWD(dir)
	if len(names) < 2 {
		t.Fatalf("expected at least 2 skill names, got %d: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["autoship"] {
		t.Error("missing 'autoship' in skill names")
	}
	if !found["deploy"] {
		t.Error("missing 'deploy' in skill names")
	}
}

func TestSkillEnforcementIntegration(t *testing.T) {
	// This tests the full flow: given a skill in the CWD, ParseSkillInvocation
	// identifies it and EnforcementPrefix wraps the message correctly.
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".claude", "skills", "autoship")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: autoship\n---\nBody"), 0644); err != nil {
		t.Fatal(err)
	}

	registry := skillNamesForCWD(dir)
	skill, args, isSkill := ParseSkillInvocation("/autoship #352", registry)

	if !isSkill {
		t.Fatal("expected isSkill=true")
	}
	if skill != "autoship" {
		t.Errorf("skill=%q, want 'autoship'", skill)
	}
	if args != "#352" {
		t.Errorf("args=%q, want '#352'", args)
	}

	prefix := EnforcementPrefix(skill, args)
	if !strings.Contains(prefix, `Skill("autoship")`) {
		t.Errorf("enforcement prefix missing Skill invocation:\n%s", prefix)
	}
	if !strings.Contains(prefix, "#352") {
		t.Errorf("enforcement prefix missing args:\n%s", prefix)
	}

	// Non-skill messages should pass through unchanged.
	_, _, isSkill2 := ParseSkillInvocation("fix a bug", registry)
	if isSkill2 {
		t.Error("plain text message should not be a skill invocation")
	}

	// Builtin commands should not be intercepted.
	_, _, isSkill3 := ParseSkillInvocation("/compact", registry)
	if isSkill3 {
		t.Error("/compact should not be intercepted as a skill")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
