package api

import "testing"

func TestClaudeProjectDir(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{
			name: "preserves underscores",
			cwd:  "/Users/jay/workspaces/_personal_/claude-control",
			want: "-Users-jay-workspaces-_personal_-claude-control",
		},
		{
			name: "preserves dots",
			cwd:  "/Users/jay/workspaces/_personal_/jay-day/.claude-worktrees/inspiring-panini",
			want: "-Users-jay-workspaces-_personal_-jay-day-.claude-worktrees-inspiring-panini",
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
