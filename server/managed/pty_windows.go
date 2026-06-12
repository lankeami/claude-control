//go:build windows

package managed

import (
	"os"
	"os/exec"
)

// startPTY is unsupported on Windows (no ConPTY support yet). Managed mode
// falls back to print mode there — see managedMode in main.go.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	return nil, ErrInteractiveUnsupported
}
