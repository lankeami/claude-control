package db

import (
	"testing"
)

func TestCreateAndGetPrompt(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	p, err := store.CreatePrompt(sess.ID, "Which DB?", "prompt")
	if err != nil {
		t.Fatalf("CreatePrompt: %v", err)
	}
	if p.ClaudeMessage != "Which DB?" || p.Status != "pending" || p.Type != "prompt" {
		t.Errorf("unexpected prompt: %+v", p)
	}
}

func TestRespondToPrompt(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	p, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	err := store.RespondToPrompt(p.ID, "SQLite")
	if err != nil {
		t.Fatalf("RespondToPrompt: %v", err)
	}

	response, err := store.GetPromptResponse(p.ID)
	if err != nil {
		t.Fatalf("GetPromptResponse: %v", err)
	}
	if response == nil || *response != "SQLite" {
		t.Errorf("expected 'SQLite', got %v", response)
	}
}

func TestGetPromptResponsePending(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	p, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	response, err := store.GetPromptResponse(p.ID)
	if err != nil {
		t.Fatalf("GetPromptResponse: %v", err)
	}
	if response != nil {
		t.Errorf("expected nil for pending prompt, got %v", response)
	}
}

func TestListPrompts(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	store.CreatePrompt(sess.ID, "Q1", "prompt")
	store.CreatePrompt(sess.ID, "Q2", "prompt")
	store.CreatePrompt(sess.ID, "Done", "notification")

	// All prompts for session
	prompts, err := store.ListPrompts(sess.ID, "")
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 3 {
		t.Errorf("expected 3, got %d", len(prompts))
	}

	// All 3 have status "pending" since that's the default
	prompts, _ = store.ListPrompts("", "pending")
	if len(prompts) != 3 {
		t.Errorf("expected 3 pending, got %d", len(prompts))
	}

	// Respond to one and verify count changes
	store.RespondToPrompt(prompts[0].ID, "answer")
	prompts, _ = store.ListPrompts("", "pending")
	if len(prompts) != 2 {
		t.Errorf("expected 2 pending after responding, got %d", len(prompts))
	}
}
