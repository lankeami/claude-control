package managed

import (
	"strings"
	"testing"
	"time"
)

func TestManagerSpawnAndInterrupt(t *testing.T) {
	cfg := Config{
		ClaudeBin:  "sleep",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	m := NewManager(cfg)

	proc, err := m.Spawn("test-session-1", SpawnOpts{
		Args: []string{"60"},
		CWD:  "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil {
		t.Fatal("proc is nil")
	}
	if !m.IsRunning("test-session-1") {
		t.Error("session should be running")
	}

	_, err = m.Spawn("test-session-1", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err == nil {
		t.Error("expected error for duplicate spawn")
	}

	err = m.Interrupt("test-session-1")
	if err != nil {
		t.Fatalf("interrupt failed: %v", err)
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after interrupt")
	}

	if m.IsRunning("test-session-1") {
		t.Error("session should not be running after interrupt")
	}
}

func TestManagerInterruptNonexistent(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	err := m.Interrupt("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestManagerTeardown(t *testing.T) {
	cfg := Config{ClaudeBin: "sleep", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	_, err := m.Spawn("sess-1", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	err = m.Teardown("sess-1", 2*time.Second)
	if err != nil {
		t.Fatalf("teardown failed: %v", err)
	}
	if m.IsRunning("sess-1") {
		t.Error("session should not be running after teardown")
	}
}

func TestManagerSpawnShell(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.SpawnShell("shell-test-1", ShellOpts{
		Command: "echo hello",
		CWD:     "/tmp",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil {
		t.Fatal("proc is nil")
	}
	if !m.IsRunning("shell-test-1") {
		t.Error("session should be running during shell execution")
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("shell process did not complete")
	}

	if proc.ExitCode != 0 {
		t.Errorf("exit code=%d, want 0", proc.ExitCode)
	}
	if m.IsRunning("shell-test-1") {
		t.Error("session should not be running after shell completes")
	}
}

func TestManagerSpawnShellBlocksConcurrent(t *testing.T) {
	cfg := Config{ClaudeBin: "sleep", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	_, err := m.Spawn("sess-concurrent", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Teardown("sess-concurrent", 2*time.Second)

	_, err = m.SpawnShell("sess-concurrent", ShellOpts{
		Command: "echo blocked",
		CWD:     "/tmp",
		Timeout: 5 * time.Second,
	})
	if err == nil {
		t.Error("expected error when Claude process is running")
	}
}

func TestManagerSpawnShellTimeout(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.SpawnShell("shell-timeout", ShellOpts{
		Command: "sleep 60",
		CWD:     "/tmp",
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-proc.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("shell process did not exit after timeout")
	}

	if m.IsRunning("shell-timeout") {
		t.Error("session should not be running after timeout")
	}
}

func TestSpawnExposesStdin(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.Spawn("stdin-test", SpawnOpts{
		Args: []string{},
		CWD:  "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Teardown("stdin-test", 2*time.Second)

	if proc.Stdin == nil {
		t.Fatal("proc.Stdin should not be nil")
	}

	// Write to stdin and close — cat should echo it back via stdout
	_, err = proc.Stdin.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("write to stdin: %v", err)
	}
	proc.Stdin.Close()

	<-proc.Done
	if proc.ExitCode != 0 {
		t.Errorf("exit code=%d, want 0", proc.ExitCode)
	}
}

func TestEnsureProcessSpawnsAndReuses(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc1, err := m.EnsureProcess("reuse-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if proc1 == nil {
		t.Fatal("proc1 is nil")
	}

	proc2, err := m.EnsureProcess("reuse-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	if proc1 != proc2 {
		t.Error("EnsureProcess should return the same process on second call")
	}

	m.Teardown("reuse-test", 2*time.Second)
}

func TestSendTurn(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("send-turn-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	msg := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`
	err = m.SendTurn("send-turn-test", msg)
	if err != nil {
		t.Fatalf("SendTurn failed: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := proc.Stdout.Read(buf)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	got := strings.TrimSpace(string(buf[:n]))
	if got != msg {
		t.Errorf("got %q, want %q", got, msg)
	}

	m.Teardown("send-turn-test", 2*time.Second)
}

func TestIdleTimeoutReapsProcess(t *testing.T) {
	cfg := Config{
		ClaudeBin:  "cat",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("idle-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	// Backdate LastActivity to trigger immediate reap
	m.mu.Lock()
	proc.LastActivity = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	m.ReapIdle(1 * time.Millisecond)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("process was not reaped after idle timeout")
	}

	if m.IsRunning("idle-test") {
		t.Error("session should not be running after reap")
	}
}

func TestSpawnAfterProcessExits(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc1, err := m.Spawn("race-test", SpawnOpts{Args: []string{"hello"}, CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	<-proc1.Done

	proc2, err := m.Spawn("race-test", SpawnOpts{Args: []string{"world"}, CWD: "/tmp"})
	if err != nil {
		t.Fatalf("second spawn should succeed after process exited: %v", err)
	}
	<-proc2.Done
}

func TestGracefulShutdown(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.Spawn("graceful-test", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsRunning("graceful-test") {
		t.Fatal("should be running")
	}

	err = m.GracefulShutdown("graceful-test", 5*time.Second)
	if err != nil {
		t.Fatalf("graceful shutdown failed: %v", err)
	}

	select {
	case <-proc.Done:
	default:
		t.Error("process should be done after graceful shutdown")
	}

	if m.IsRunning("graceful-test") {
		t.Error("should not be running after graceful shutdown")
	}
}

func TestGracefulShutdownNoProcess(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	err := m.GracefulShutdown("nonexistent", 5*time.Second)
	if err != nil {
		t.Errorf("expected nil error for nonexistent session, got: %v", err)
	}
}

func TestUpdateConfig(t *testing.T) {
	mgr := NewManager(Config{ClaudeBin: "old-bin", ClaudeArgs: []string{"--old"}, ClaudeEnv: []string{"OLD=1"}})

	mgr.UpdateConfig(Config{ClaudeBin: "new-bin", ClaudeArgs: []string{"--new"}, ClaudeEnv: []string{"NEW=1"}})

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.cfg.ClaudeBin != "new-bin" {
		t.Errorf("expected new-bin, got %s", mgr.cfg.ClaudeBin)
	}
	if len(mgr.cfg.ClaudeArgs) != 1 || mgr.cfg.ClaudeArgs[0] != "--new" {
		t.Errorf("expected [--new], got %v", mgr.cfg.ClaudeArgs)
	}
	if len(mgr.cfg.ClaudeEnv) != 1 || mgr.cfg.ClaudeEnv[0] != "NEW=1" {
		t.Errorf("expected [NEW=1], got %v", mgr.cfg.ClaudeEnv)
	}
}
