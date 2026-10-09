package managed

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// LatestBackgroundActivity scans a Claude per-session directory
// (<config>/projects/<munged-cwd>/<claude_session_id>/) for evidence of
// background work still running after the last turn ended:
//
//   - subagents/** transcripts (covers workflow runs under subagents/workflows/**)
//   - tasks/*.output files written by background Bash tasks
//
// It returns the newest mtime found, or the zero time when the directory is
// missing or holds no such files.
func LatestBackgroundActivity(sessionDir string) time.Time {
	var newest time.Time

	consider := func(info fs.FileInfo) {
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
	}

	subagents := filepath.Join(sessionDir, "subagents")
	filepath.WalkDir(subagents, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			consider(info)
		}
		return nil
	})

	outputs, _ := filepath.Glob(filepath.Join(sessionDir, "tasks", "*.output"))
	for _, p := range outputs {
		if info, err := os.Stat(p); err == nil {
			consider(info)
		}
	}

	return newest
}
