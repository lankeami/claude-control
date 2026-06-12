//go:build !windows

package managed

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// startPTY launches cmd attached to a new pseudo-terminal and returns the
// master side. Wide window avoids the TUI wrapping long prompts.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 200})
}
