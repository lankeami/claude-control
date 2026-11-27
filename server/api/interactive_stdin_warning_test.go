package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestInteractiveStdinWarning verifies that sending a prompt starting with "!"
// followed by a known interactive command (one that needs stdin, like
// "aws configure", "ssh", "vim", etc.) injects a warning into the SSE stream
// as a system-level message BEFORE or instead of normal execution. The managed
// session's PTY relay cannot forward interactive stdin prompts, so the user
// must be warned at the API level rather than relying on Claude to explain it.
func TestInteractiveStdinWarning(t *testing.T) {
	ts, store, mock := setupInteractiveTestServer(t)
	sess, err := store.CreateManagedSession("/tmp/int-stdin-warn", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	b := mock.GetBroadcaster(sess.ID)
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	// Send a "!" prefixed message with an interactive command that needs stdin.
	resp, err := sendMessage(ts, sess.ID, "! aws configure")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Wait for the transcript callback to be registered so we can emit
	// assistant activity and end the turn.
	waitForTranscriptFn(t, mock, sess.ID)
	mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("running aws configure", 100, 50))
	mock.SignalStop(sess.ID)

	// Collect broadcast events until done.
	var warningFound bool
	var warningText string
	timeout := time.After(3 * time.Second)
	for {
		select {
		case msg := <-ch:
			var obj map[string]interface{}
			if json.Unmarshal([]byte(msg), &obj) != nil {
				continue
			}
			// Look for a system-level warning about interactive stdin commands.
			if obj["type"] == "system" || obj["type"] == "stdin_warning" {
				if text, ok := obj["message"].(string); ok {
					if strings.Contains(strings.ToLower(text), "interactive") {
						warningFound = true
						warningText = text
					}
				}
			}
			if obj["type"] == "done" {
				goto checkResults
			}
		case <-timeout:
			goto checkResults
		}
	}
checkResults:

	if !warningFound {
		t.Fatal("no interactive stdin warning was broadcast in the SSE stream for '! aws configure'")
	}

	// The warning text should mention that interactive commands don't work well.
	if !strings.Contains(strings.ToLower(warningText), "interactive") {
		t.Errorf("warning text %q does not mention 'interactive'", warningText)
	}

	// Verify the warning was also persisted to the messages table.
	msgs, _ := store.ListMessages(sess.ID)
	var persistedWarning bool
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(strings.ToLower(m.Content), "interactive") {
			persistedWarning = true
		}
	}
	if !persistedWarning {
		t.Error("interactive stdin warning was not persisted as a system message")
	}
}

// TestInteractiveStdinWarningVariousCommands checks that the warning fires for
// several known interactive commands (ssh, vim, mysql, python REPL, etc.).
func TestInteractiveStdinWarningVariousCommands(t *testing.T) {
	interactiveCommands := []string{
		"! ssh user@host",
		"! vim file.txt",
		"! nano file.txt",
		"! mysql -u root",
		"! psql",
		"! python",
		"! node",
		"! irb",
		"! top",
		"! htop",
	}

	for _, cmd := range interactiveCommands {
		t.Run(cmd, func(t *testing.T) {
			ts, store, mock := setupInteractiveTestServer(t)
			sess, err := store.CreateManagedSession(t.TempDir(), `["Bash"]`, 50, 5.0, 0)
			if err != nil {
				t.Fatal(err)
			}

			b := mock.GetBroadcaster(sess.ID)
			ch := b.Subscribe()
			defer b.Unsubscribe(ch)

			resp, err := sendMessage(ts, sess.ID, cmd)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			waitForTranscriptFn(t, mock, sess.ID)
			mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("executing", 10, 10))
			mock.SignalStop(sess.ID)

			var warningFound bool
			timeout := time.After(3 * time.Second)
			for {
				select {
				case msg := <-ch:
					var obj map[string]interface{}
					if json.Unmarshal([]byte(msg), &obj) != nil {
						continue
					}
					if obj["type"] == "system" || obj["type"] == "stdin_warning" {
						if text, ok := obj["message"].(string); ok {
							if strings.Contains(strings.ToLower(text), "interactive") {
								warningFound = true
							}
						}
					}
					if obj["type"] == "done" {
						goto check
					}
				case <-timeout:
					goto check
				}
			}
		check:
			if !warningFound {
				t.Errorf("no interactive stdin warning for %q", cmd)
			}
		})
	}
}

// TestInteractiveStdinWarningNotForSafeCommands verifies that non-interactive
// "!" commands (like "! ls", "! cat file.txt", "! git status") do NOT trigger
// the warning.
func TestInteractiveStdinWarningNotForSafeCommands(t *testing.T) {
	safeCommands := []string{
		"! ls -la",
		"! cat file.txt",
		"! git status",
		"! echo hello",
		"! grep -r foo .",
		"! pwd",
	}

	for _, cmd := range safeCommands {
		t.Run(cmd, func(t *testing.T) {
			ts, store, mock := setupInteractiveTestServer(t)
			sess, err := store.CreateManagedSession(t.TempDir(), `["Bash"]`, 50, 5.0, 0)
			if err != nil {
				t.Fatal(err)
			}

			b := mock.GetBroadcaster(sess.ID)
			ch := b.Subscribe()
			defer b.Unsubscribe(ch)

			resp, err := sendMessage(ts, sess.ID, cmd)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			waitForTranscriptFn(t, mock, sess.ID)
			mock.EmitTranscriptLine(sess.ID, assistantTranscriptLine("done", 10, 10))
			mock.SignalStop(sess.ID)

			var warningFound bool
			timeout := time.After(3 * time.Second)
			for {
				select {
				case msg := <-ch:
					var obj map[string]interface{}
					if json.Unmarshal([]byte(msg), &obj) != nil {
						continue
					}
					if obj["type"] == "system" || obj["type"] == "stdin_warning" {
						if text, ok := obj["message"].(string); ok {
							if strings.Contains(strings.ToLower(text), "interactive") {
								warningFound = true
							}
						}
					}
					if obj["type"] == "done" {
						goto check
					}
				case <-timeout:
					goto check
				}
			}
		check:
			if warningFound {
				t.Errorf("unexpected interactive stdin warning for safe command %q", cmd)
			}
		})
	}
}
