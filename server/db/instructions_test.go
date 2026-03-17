package db

import (
	"testing"
)

func TestQueueAndFetchInstruction(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	instr, err := store.QueueInstruction(sess.ID, "Run the tests")
	if err != nil {
		t.Fatalf("QueueInstruction: %v", err)
	}
	if instr.Message != "Run the tests" || instr.Status != "queued" {
		t.Errorf("unexpected instruction: %+v", instr)
	}

	// Fetch queued instruction
	fetched, err := store.FetchNextInstruction(sess.ID)
	if err != nil {
		t.Fatalf("FetchNextInstruction: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected instruction, got nil")
	}
	if fetched.Message != "Run the tests" || fetched.Status != "delivered" {
		t.Errorf("unexpected fetched: %+v", fetched)
	}

	// No more instructions
	fetched2, err := store.FetchNextInstruction(sess.ID)
	if err != nil {
		t.Fatalf("FetchNextInstruction: %v", err)
	}
	if fetched2 != nil {
		t.Errorf("expected nil, got %+v", fetched2)
	}
}
