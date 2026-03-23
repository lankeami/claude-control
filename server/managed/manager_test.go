package managed

import (
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
