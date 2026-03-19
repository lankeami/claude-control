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
