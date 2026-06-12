package db

import (
	"path/filepath"
	"testing"
)

func TestCreateAndListMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Bash","Read","Edit"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	msg1, err := store.CreateMessage(sess.ID, "user", `{"type":"user","content":"hello"}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if msg1.Role != "user" || msg1.Seq != 1 {
		t.Errorf("msg1: role=%s seq=%d, want user/1", msg1.Role, msg1.Seq)
	}

	msg2, err := store.CreateMessage(sess.ID, "assistant", `{"type":"assistant","content":"hi"}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if msg2.Seq != 2 {
		t.Errorf("msg2 seq=%d, want 2", msg2.Seq)
	}

	msgs, err := store.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Seq != 1 || msgs[1].Seq != 2 {
		t.Error("messages not ordered by seq")
	}
}

func TestDeleteSessionCascadesMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Read"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	store.CreateMessage(sess.ID, "user", "hello", 0)
	store.CreateMessage(sess.ID, "assistant", "hi", 0)

	err = store.DeleteSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	// After soft-delete, the session cannot be retrieved (deleted_at IS NOT NULL)
	retrieved, _ := store.GetSessionByID(sess.ID)
	if retrieved != nil {
		t.Error("session should not be retrievable after deletion")
	}

	// But messages are preserved (for cost tracking)
	msgs, _ := store.ListMessages(sess.ID)
	if len(msgs) != 2 {
		t.Errorf("got %d messages after delete, want 2 (preserved for cost tracking)", len(msgs))
	}
}

func TestCreateMessage_WithCost(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Read"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := store.CreateMessage(sess.ID, "assistant", "Hello", 0.05)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	if msg.Cost != 0.05 {
		t.Errorf("expected cost 0.05, got %v", msg.Cost)
	}
}

func TestSessionCostTotal(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/cost-project", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}

	total, err := store.SessionCostTotal(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("empty session total = %f, want 0", total)
	}

	store.CreateMessage(sess.ID, "cost", "0.5", 0.5)
	store.CreateMessage(sess.ID, "cost", "0.25", 0.25)
	store.CreateMessage(sess.ID, "assistant", "not a cost", 99) // wrong role, excluded

	total, err = store.SessionCostTotal(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0.75 {
		t.Errorf("total = %f, want 0.75", total)
	}
}

func TestUpdateClaudeSessionID(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/csid-project", `["Bash"]`, 50, 5.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateClaudeSessionID(sess.ID, "new-cli-uuid"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSessionByID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeSessionID != "new-cli-uuid" {
		t.Errorf("claude_session_id = %s, want new-cli-uuid", got.ClaudeSessionID)
	}
}
