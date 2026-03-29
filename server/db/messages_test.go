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

	msg1, err := store.CreateMessage(sess.ID, "user", `{"type":"user","content":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if msg1.Role != "user" || msg1.Seq != 1 {
		t.Errorf("msg1: role=%s seq=%d, want user/1", msg1.Role, msg1.Seq)
	}

	msg2, err := store.CreateMessage(sess.ID, "assistant", `{"type":"assistant","content":"hi"}`)
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
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", "hi")

	err = store.DeleteSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	msgs, _ := store.ListMessages(sess.ID)
	if len(msgs) != 0 {
		t.Errorf("got %d messages after delete, want 0", len(msgs))
	}
}
