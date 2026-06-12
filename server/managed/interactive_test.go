//go:build !windows

package managed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestManager(bin string, args ...string) *Manager {
	return NewManager(Config{ClaudeBin: bin, ClaudeArgs: args, Mode: "interactive"})
}

func waitFor(t *testing.T, dur time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestEnsureInteractiveSpawnsOnce(t *testing.T) {
	m := newTestManager("cat")
	p1, err := m.EnsureInteractive("s1", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s1", time.Second)
	p2, err := m.EnsureInteractive("s1", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatal("expected same process on second EnsureInteractive")
	}
	if !m.IsInteractiveRunning("s1") {
		t.Fatal("expected IsInteractiveRunning true")
	}
}

// fastReady shortens readiness tuning so tests with silent fake binaries
// (cat) don't block on the ready gate.
func fastReady(t *testing.T) {
	t.Helper()
	oldQ, oldT := interactiveReadyQuiescence, interactiveReadyTimeout
	interactiveReadyQuiescence = 30 * time.Millisecond
	interactiveReadyTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		interactiveReadyQuiescence = oldQ
		interactiveReadyTimeout = oldT
	})
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-claude.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSendPromptWaitsForBootQuiescence(t *testing.T) {
	oldQ, oldT := interactiveReadyQuiescence, interactiveReadyTimeout
	interactiveReadyQuiescence = 250 * time.Millisecond
	interactiveReadyTimeout = 5 * time.Second
	t.Cleanup(func() {
		interactiveReadyQuiescence = oldQ
		interactiveReadyTimeout = oldT
	})

	// Fake TUI: streams boot output for ~800ms, then goes quiet and echoes stdin.
	script := writeScript(t, `for i in $(seq 1 8); do echo "boot $i"; sleep 0.1; done
exec cat`)
	m := newTestManager("/bin/bash", script)
	proc, err := m.EnsureInteractive("rq1", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("rq1", time.Second)

	start := time.Now()
	if err := m.SendPrompt("rq1", "hello"); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 850*time.Millisecond {
		t.Fatalf("SendPrompt returned after %v; expected it to wait for boot output to go quiet (~1s)", elapsed)
	}
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(proc.LastOutput(), "\x1b[200~hello\x1b[201~")
	})
}

func TestSendPromptAutoAcceptsTrustDialog(t *testing.T) {
	oldQ, oldT := interactiveReadyQuiescence, interactiveReadyTimeout
	interactiveReadyQuiescence = 150 * time.Millisecond
	interactiveReadyTimeout = 5 * time.Second
	t.Cleanup(func() {
		interactiveReadyQuiescence = oldQ
		interactiveReadyTimeout = oldT
	})

	// Fake trust dialog with ANSI styling interleaved mid-phrase, waiting for
	// Enter before showing the input screen.
	script := writeScript(t, `printf 'Is this a project you created or one you \x1b[1mtrust\x1b[0m?\n'
printf '> 1. Yes, I \x1b[32mtrust this folder\x1b[0m\n  2. No, exit\n'
read -r _
echo "TRUSTED"
exec cat`)
	m := newTestManager("/bin/bash", script)
	proc, err := m.EnsureInteractive("rq2", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("rq2", time.Second)

	if err := m.SendPrompt("rq2", "hello"); err != nil {
		t.Fatal(err)
	}
	out := proc.LastOutput()
	if !strings.Contains(out, "TRUSTED") {
		t.Fatalf("trust dialog was not auto-accepted; output: %q", out)
	}
	waitFor(t, 2*time.Second, func() bool {
		out := proc.LastOutput()
		return strings.Index(out, "TRUSTED") < strings.Index(out, "\x1b[200~hello\x1b[201~")
	})
}

func TestSendPromptProceedsAfterReadyTimeout(t *testing.T) {
	oldQ, oldT := interactiveReadyQuiescence, interactiveReadyTimeout
	interactiveReadyQuiescence = 100 * time.Millisecond
	interactiveReadyTimeout = 300 * time.Millisecond
	t.Cleanup(func() {
		interactiveReadyQuiescence = oldQ
		interactiveReadyTimeout = oldT
	})

	// Fake TUI that never goes quiet: readiness must give up at the timeout
	// rather than block forever.
	script := writeScript(t, `while true; do echo busy; sleep 0.05; done`)
	m := newTestManager("/bin/bash", script)
	if _, err := m.EnsureInteractive("rq3", InteractiveOpts{CWD: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("rq3", time.Second)

	start := time.Now()
	if err := m.SendPrompt("rq3", "hello"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("SendPrompt blocked %v; expected it to proceed at the ready timeout", elapsed)
	}
}

func TestSendPromptReadyGateIsStickyPerProcess(t *testing.T) {
	oldQ, oldT := interactiveReadyQuiescence, interactiveReadyTimeout
	interactiveReadyQuiescence = 300 * time.Millisecond
	interactiveReadyTimeout = 5 * time.Second
	t.Cleanup(func() {
		interactiveReadyQuiescence = oldQ
		interactiveReadyTimeout = oldT
	})

	script := writeScript(t, `echo ready
exec cat`)
	m := newTestManager("/bin/bash", script)
	if _, err := m.EnsureInteractive("rq4", InteractiveOpts{CWD: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("rq4", time.Second)

	if err := m.SendPrompt("rq4", "first"); err != nil {
		t.Fatal(err)
	}
	// Second prompt must not pay the quiescence wait again (echo of the first
	// prompt counts as fresh output, so a non-sticky gate would re-block).
	start := time.Now()
	if err := m.SendPrompt("rq4", "second"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("second SendPrompt waited %v; readiness must be sticky", elapsed)
	}
}

func TestSendPromptWritesBracketedPaste(t *testing.T) {
	fastReady(t)
	m := newTestManager("cat")
	proc, err := m.EnsureInteractive("s2", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s2", time.Second)

	if err := m.SendPrompt("s2", "hello world"); err != nil {
		t.Fatal(err)
	}
	// cat echoes PTY input back to the master; ring buffer captures it.
	waitFor(t, 2*time.Second, func() bool {
		out := proc.LastOutput()
		return strings.Contains(out, "\x1b[200~hello world\x1b[201~")
	})
}

func TestSendKeysWritesRaw(t *testing.T) {
	m := newTestManager("cat")
	proc, err := m.EnsureInteractive("s3", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s3", time.Second)

	if err := m.SendKeys("s3", "xyz"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(proc.LastOutput(), "xyz")
	})
	// InterruptInteractive sends ESC; the TTY echoes it as caret notation
	// ("^[") so just verify it writes without error against a live PTY.
	if err := m.InterruptInteractive("s3"); err != nil {
		t.Fatal(err)
	}
}

func TestSignalStopDelivers(t *testing.T) {
	m := newTestManager("cat")
	_, err := m.EnsureInteractive("s4", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s4", time.Second)

	m.SignalStop("s4")
	select {
	case <-m.StopEvents("s4"):
	case <-time.After(time.Second):
		t.Fatal("stop signal not delivered")
	}
}

func TestTouchInteractiveUpdatesLastActivity(t *testing.T) {
	m := newTestManager("cat")
	_, err := m.EnsureInteractive("s-touch", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s-touch", time.Second)

	old := time.Now().Add(-time.Hour)
	m.mu.Lock()
	m.iprocs["s-touch"].LastActivity = old
	m.mu.Unlock()

	m.TouchInteractive("s-touch")

	m.mu.Lock()
	got := m.iprocs["s-touch"].LastActivity
	m.mu.Unlock()
	if !got.After(old) {
		t.Fatalf("LastActivity not updated: %v", got)
	}
}

func TestTouchInteractiveNoProcessIsNoop(t *testing.T) {
	m := newTestManager("cat")
	m.TouchInteractive("nope") // must not panic
}

func TestSignalStopNoProcessIsNoop(t *testing.T) {
	m := newTestManager("cat")
	m.SignalStop("nope") // must not panic
	if ch := m.StopEvents("nope"); ch != nil {
		t.Fatal("expected nil StopEvents for unknown session")
	}
}

func TestSetTranscriptStartsTailerAndFiltersOldEntries(t *testing.T) {
	old := TranscriptPollInterval
	TranscriptPollInterval = 20 * time.Millisecond
	defer func() { TranscriptPollInterval = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")

	var mu sync.Mutex
	var lines []string
	m := newTestManager("cat")
	_, err := m.EnsureInteractive("s5", InteractiveOpts{
		CWD: dir,
		OnTranscriptLine: func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s5", time.Second)

	m.SetTranscript("s5", path)
	// Second SetTranscript must not start a second tailer (no duplicate lines).
	m.SetTranscript("s5", path)

	oldEntry, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
	newEntry, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339)})
	noTS, _ := json.Marshal(map[string]any{"type": "assistant"})
	os.WriteFile(path, []byte(fmt.Sprintf("%s\n%s\n%s\n", oldEntry, newEntry, noTS)), 0644)

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(lines) >= 2
	})
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (old entry filtered, no-timestamp kept), got %d: %v", len(lines), lines)
	}
}

func TestShutdownInteractiveKillsAfterTimeout(t *testing.T) {
	m := newTestManager("sleep", "60")
	proc, err := m.EnsureInteractive("s6", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := m.ShutdownInteractive("s6", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not die")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("shutdown took too long")
	}
	if m.IsInteractiveRunning("s6") {
		t.Fatal("still marked running")
	}
}

func TestInteractiveDoesNotBlockShellGuard(t *testing.T) {
	m := newTestManager("cat")
	_, err := m.EnsureInteractive("s7", InteractiveOpts{CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.ShutdownInteractive("s7", time.Second)
	if m.IsRunning("s7") {
		t.Fatal("interactive proc must not count as a one-shot process (shell guard)")
	}
}
