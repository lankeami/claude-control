package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupFakeGH(t *testing.T, script string) (restore func()) {
	t.Helper()
	dir := t.TempDir()
	fakeGH := filepath.Join(dir, "gh")
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+orig)
	return func() { os.Setenv("PATH", orig) }
}

func initTestRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
		{"git", "-C", dir, "checkout", "-b", branch},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Logf("cmd %v: %s", args, out)
		}
	}
	return dir
}

func TestGitSummaryOpenPR(t *testing.T) {
	restore := setupFakeGH(t, "#!/bin/sh\necho '{\"number\":42,\"url\":\"https://github.com/owner/repo/pull/42\"}'\n")
	defer restore()

	repoDir := initTestRepo(t, "feat/test-pr-branch")

	info := gitSummary(repoDir)

	if info.OpenPRNumber != 42 {
		t.Errorf("OpenPRNumber: got %d, want 42", info.OpenPRNumber)
	}
	if info.OpenPRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("OpenPRURL: got %q, want %q", info.OpenPRURL, "https://github.com/owner/repo/pull/42")
	}
}

func TestGitSummaryNoPR(t *testing.T) {
	restore := setupFakeGH(t, "#!/bin/sh\nexit 1\n")
	defer restore()

	repoDir := initTestRepo(t, "feat/no-pr-branch")

	info := gitSummary(repoDir)

	if info.OpenPRNumber != 0 {
		t.Errorf("OpenPRNumber: got %d, want 0", info.OpenPRNumber)
	}
	if info.OpenPRURL != "" {
		t.Errorf("OpenPRURL: got %q, want empty", info.OpenPRURL)
	}
}
