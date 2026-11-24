package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillNames_ProjectSkills(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir()

	// Create two project-level skills
	for _, name := range []string{"autoship", "git-issue-create"} {
		skillDir := filepath.Join(dir, ".claude", "skills", name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\nBody"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	names := DiscoverSkillNames(dir)
	if len(names) != 2 {
		t.Fatalf("got %d skills, want 2: %v", len(names), names)
	}
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	if !has["autoship"] || !has["git-issue-create"] {
		t.Errorf("missing expected skills: got %v", names)
	}
}

func TestDiscoverSkillNames_FallsBackToDirName(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir()

	// Skill with no frontmatter name — should use directory name
	skillDir := filepath.Join(dir, ".claude", "skills", "my-custom-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("No frontmatter here"), 0644); err != nil {
		t.Fatal(err)
	}

	names := DiscoverSkillNames(dir)
	if len(names) != 1 || names[0] != "my-custom-skill" {
		t.Errorf("got %v, want [my-custom-skill]", names)
	}
}

func TestDiscoverSkillNames_DeduplicatesAcrossScopes(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	projectDir := t.TempDir()

	// Same skill name in both project and user level
	for _, base := range []string{
		filepath.Join(projectDir, ".claude", "skills", "deploy"),
		filepath.Join(fakeHome, ".claude", "skills", "deploy"),
	} {
		if err := os.MkdirAll(base, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "SKILL.md"), []byte("---\nname: deploy\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	names := DiscoverSkillNames(projectDir)
	count := 0
	for _, n := range names {
		if n == "deploy" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("deploy appeared %d times, want 1 (dedup); names=%v", count, names)
	}
}

func TestDiscoverSkillNames_SkipsMissingSKILLmd(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir()

	// Directory exists but no SKILL.md
	skillDir := filepath.Join(dir, ".claude", "skills", "broken")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	names := DiscoverSkillNames(dir)
	if len(names) != 0 {
		t.Errorf("got %v, want empty", names)
	}
}

func TestDiscoverSkillNames_EmptyOnMissingDirs(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir() // no .claude/skills/ inside

	names := DiscoverSkillNames(dir)
	if len(names) != 0 {
		t.Errorf("got %v, want empty", names)
	}
}

func TestDiscoverSkillNames_IncludesUserAndBBScopes(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	projectDir := t.TempDir()

	// User-level skill
	userSkill := filepath.Join(fakeHome, ".claude", "skills", "user-skill")
	if err := os.MkdirAll(userSkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("---\nname: user-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// BB-level skill
	bbSkill := filepath.Join(fakeHome, ".claud-bb", "skills", "bb-skill")
	if err := os.MkdirAll(bbSkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bbSkill, "SKILL.md"), []byte("---\nname: bb-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	names := DiscoverSkillNames(projectDir)
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	if !has["user-skill"] {
		t.Errorf("missing user-skill in %v", names)
	}
	if !has["bb-skill"] {
		t.Errorf("missing bb-skill in %v", names)
	}
}

func TestDiscoverSkillNames_FrontmatterNameOverridesDirName(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir()

	// Directory name differs from frontmatter name
	skillDir := filepath.Join(dir, ".claude", "skills", "dir-name")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: frontmatter-name\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	names := DiscoverSkillNames(dir)
	if len(names) != 1 || names[0] != "frontmatter-name" {
		t.Errorf("got %v, want [frontmatter-name]", names)
	}
}
