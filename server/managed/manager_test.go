package managed

import (
	"os"
	"path/filepath"
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

func TestShutdownAll_NoProcesses(t *testing.T) {
	mgr := NewManager(Config{})
	mgr.ShutdownAll(5 * time.Second)
	// Should not panic or hang
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

func TestAgentDispatch_ClaudeUsesExistingBackend(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	// Claude agent sessions use the existing Spawn path unchanged
	proc, err := m.Spawn("claude-dispatch", SpawnOpts{Args: []string{"hello"}, CWD: "/tmp"})
	if err != nil {
		t.Fatalf("claude spawn: %v", err)
	}
	<-proc.Done
	if proc.ExitCode != 0 {
		t.Errorf("exit code=%d, want 0", proc.ExitCode)
	}
}

func TestAgentDispatch_ConfigHasCodexBin(t *testing.T) {
	cfg := Config{ClaudeBin: "claude", CodexBin: "codex"}
	m := NewManager(cfg)
	got := m.Config()
	if got.CodexBin != "codex" {
		t.Errorf("CodexBin=%q, want codex", got.CodexBin)
	}
}

// --- Issue #314: background-work-aware idle reaper ---

func TestReapIdleSkipsSessionWithRecentBackgroundActivity(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("bg-active", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Teardown("bg-active", 2*time.Second)

	// Turn-idle clock says this session is long past the timeout...
	m.mu.Lock()
	proc.LastActivity = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	// ...but the background-activity probe reports work 1 minute ago.
	m.SetBackgroundActivityFn(func(sessionID string) time.Time {
		if sessionID != "bg-active" {
			t.Errorf("probe called with session %q, want bg-active", sessionID)
		}
		return time.Now().Add(-1 * time.Minute)
	})

	m.ReapIdle(30 * time.Minute)

	select {
	case <-proc.Done:
		t.Fatal("session with recent background activity was reaped")
	case <-time.After(300 * time.Millisecond):
	}
	if !m.IsRunning("bg-active") {
		t.Error("session should still be running")
	}
}

func TestReapIdleReapsSessionWithoutBackgroundActivity(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("bg-idle", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	proc.LastActivity = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	// Probe reports background activity, but it's older than the idle window.
	m.SetBackgroundActivityFn(func(sessionID string) time.Time {
		return time.Now().Add(-45 * time.Minute)
	})

	var reapedID string
	var reapedIdle time.Duration
	reaped := make(chan struct{})
	m.SetReapCallback(func(sessionID string, idleFor time.Duration) {
		reapedID = sessionID
		reapedIdle = idleFor
		close(reaped)
	})

	m.ReapIdle(30 * time.Minute)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("genuinely idle session was not reaped")
	}
	select {
	case <-reaped:
	case <-time.After(2 * time.Second):
		t.Fatal("reap callback never fired")
	}
	if reapedID != "bg-idle" {
		t.Errorf("reap callback session=%q, want bg-idle", reapedID)
	}
	if reapedIdle <= 0 {
		t.Errorf("reap callback idleFor=%v, want > 0", reapedIdle)
	}
}

func TestReapIdleHardCapOverridesBackgroundActivity(t *testing.T) {
	cfg := Config{ClaudeBin: "cat", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	proc, err := m.EnsureProcess("bg-capped", SpawnOpts{CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	// Past the 24h hard cap: probe activity must not keep it alive.
	m.mu.Lock()
	proc.LastActivity = time.Now().Add(-25 * time.Hour)
	m.mu.Unlock()

	m.SetBackgroundActivityFn(func(sessionID string) time.Time {
		return time.Now()
	})

	m.ReapIdle(30 * time.Minute)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("session past the 24h hard cap was not reaped")
	}
}

func TestLatestBackgroundActivityScansWorkflowAndTaskFiles(t *testing.T) {
	dir := t.TempDir()

	if got := LatestBackgroundActivity(dir); !got.IsZero() {
		t.Errorf("empty session dir: got %v, want zero time", got)
	}

	wfDir := filepath.Join(dir, "subagents", "workflows", "run-1")
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		t.Fatal(err)
	}
	wfFile := filepath.Join(wfDir, "agent.jsonl")
	if err := os.WriteFile(wfFile, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(wfFile, old, old); err != nil {
		t.Fatal(err)
	}

	got := LatestBackgroundActivity(dir)
	if got.IsZero() {
		t.Fatal("workflow transcript not detected")
	}
	if diff := got.Sub(old); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("got %v, want ~%v", got, old)
	}

	// A newer task-output file wins.
	taskDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	taskFile := filepath.Join(taskDir, "t1.output")
	if err := os.WriteFile(taskFile, []byte("out"), 0644); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().Add(-1 * time.Minute)
	if err := os.Chtimes(taskFile, newer, newer); err != nil {
		t.Fatal(err)
	}

	got = LatestBackgroundActivity(dir)
	if diff := got.Sub(newer); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("got %v, want ~%v (newest file wins)", got, newer)
	}
}

func TestIdleTimeoutConfigReachesManager(t *testing.T) {
	m := NewManager(Config{IdleTimeoutMinutes: 120})
	if got := m.Config().IdleTimeoutMinutes; got != 120 {
		t.Errorf("cfg.IdleTimeoutMinutes=%d, want 120", got)
	}
	if got := m.IdleTimeout(); got != 120*time.Minute {
		t.Errorf("IdleTimeout()=%v, want 120m", got)
	}
}

func TestIdleTimeoutConfigZeroDefaultsTo30(t *testing.T) {
	m := NewManager(Config{})
	if got := m.IdleTimeout(); got != 30*time.Minute {
		t.Errorf("IdleTimeout()=%v, want 30m", got)
	}
}
