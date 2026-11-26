package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeProjectDir(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{
			name: "replaces underscores",
			cwd:  "/Users/jay/workspaces/_personal_/claude-control",
			want: "-Users-jay-workspaces--personal--claude-control",
		},
		{
			name: "replaces dots",
			cwd:  "/Users/jay/workspaces/_personal_/jay-day/.claude-worktrees/inspiring-panini",
			want: "-Users-jay-workspaces--personal--jay-day--claude-worktrees-inspiring-panini",
		},
		{
			name: "plain path",
			cwd:  "/Users/jay/workspaces/404-checker",
			want: "-Users-jay-workspaces-404-checker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeProjectDir(tc.cwd)
			if got != tc.want {
				t.Errorf("claudeProjectDir(%q)\n  got  %q\n  want %q", tc.cwd, got, tc.want)
			}
		})
	}
}

func TestClaudeProjectDirResolvesSymlinks(t *testing.T) {
	// Create a real directory and a symlink pointing to it.
	real := t.TempDir()
	symlink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got := claudeProjectDir(symlink)
	want := claudeProjectDir(real)
	if got != want {
		t.Errorf("symlink and real dir produced different encodings:\n  symlink: %q\n  real:    %q", got, want)
	}
}
