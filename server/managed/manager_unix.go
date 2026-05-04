//go:build !windows

package managed

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func interruptProcess(p *os.Process) error {
	return p.Signal(syscall.SIGINT)
}

func newShellCmd(command string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// killWithTimeout sends SIGINT to the process group, waits gracePeriod, then SIGKILL.
func killWithTimeout(cmd *exec.Cmd, proc *Process, gracePeriod time.Duration) {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		cmd.Process.Kill()
		return
	}
	proc.TimedOut = true
	syscall.Kill(-pgid, syscall.SIGINT)
	select {
	case <-proc.Done:
	case <-time.After(gracePeriod):
		syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
