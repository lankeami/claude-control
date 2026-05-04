//go:build windows

package managed

import (
	"os"
	"os/exec"
	"time"
)

func interruptProcess(p *os.Process) error {
	return p.Signal(os.Interrupt)
}

func newShellCmd(command string) *exec.Cmd {
	return exec.Command("cmd", "/c", command)
}

// killWithTimeout kills the process immediately on Windows (no process groups).
func killWithTimeout(cmd *exec.Cmd, proc *Process, _ time.Duration) {
	proc.TimedOut = true
	cmd.Process.Kill()
}
