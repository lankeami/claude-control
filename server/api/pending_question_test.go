package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestExtractAskUserQuestion_ValidQuestion(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_123","name":"AskUserQuestion","input":{"questions":[{"question":"Which approach?","header":"Approach","options":[{"label":"Option A","description":"First option"},{"label":"Option B","description":"Second option"}],"multiSelect":false}]}}]}}`
	pq := extractAskUserQuestion(line)
	if pq == nil {
		t.Fatal("expected non-nil PendingQuestion")
	}
	if pq.ToolUseID != "toolu_123" {
		t.Errorf("ToolUseID = %q, want %q", pq.ToolUseID, "toolu_123")
	}
	if len(pq.Questions) != 1 {
		t.Fatalf("len(Questions) = %d, want 1", len(pq.Questions))
	}
	if pq.Questions[0].Question != "Which approach?" {
		t.Errorf("Question = %q, want %q", pq.Questions[0].Question, "Which approach?")
	}
	if len(pq.Questions[0].Options) != 2 {
		t.Errorf("len(Options) = %d, want 2", len(pq.Questions[0].Options))
	}
}

func TestExtractAskUserQuestion_NoQuestions(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_456","name":"AskUserQuestion","input":{"questions":[]}}]}}`
	pq := extractAskUserQuestion(line)
	if pq != nil {
		t.Error("expected nil for empty questions array")
	}
}

func TestExtractAskUserQuestion_MissingInput(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_789","name":"AskUserQuestion","input":{}}]}}`
	pq := extractAskUserQuestion(line)
	if pq != nil {
		t.Error("expected nil for missing questions field")
	}
}

func TestExtractAskUserQuestion_NotAskUserQuestion(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_abc","name":"Read","input":{"file_path":"/tmp/test.txt"}}]}}`
	pq := extractAskUserQuestion(line)
	if pq != nil {
		t.Error("expected nil for non-AskUserQuestion tool")
	}
}

func TestExtractAskUserQuestion_MalformedJSON(t *testing.T) {
	pq := extractAskUserQuestion(`not json`)
	if pq != nil {
		t.Error("expected nil for malformed JSON")
	}
}

func TestExtractAskUserQuestion_TextOnlyAssistant(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello!"}]}}`
	pq := extractAskUserQuestion(line)
	if pq != nil {
		t.Error("expected nil for text-only assistant message")
	}
}

func TestPendingQuestionManager(t *testing.T) {
	mgr := NewPendingQuestionManager()

	if mgr.Get("sess1") != nil {
		t.Error("expected nil for unknown session")
	}

	pq := &PendingQuestion{ToolUseID: "toolu_1", Questions: []PendingQuestionItem{{Question: "test?"}}}
	mgr.Set("sess1", pq)

	got := mgr.Get("sess1")
	if got == nil || got.ToolUseID != "toolu_1" {
		t.Error("expected stored question")
	}

	mgr.Delete("sess1")
	if mgr.Get("sess1") != nil {
		t.Error("expected nil after delete")
	}
}

func TestPendingQuestionWaitForClear(t *testing.T) {
	mgr := NewPendingQuestionManager()
	pq := &PendingQuestion{ToolUseID: "toolu_w1", Questions: []PendingQuestionItem{{Question: "test?"}}}
	mgr.Set("sess1", pq)

	ch := mgr.WaitForClear("sess1")

	// Channel should not be ready yet
	select {
	case <-ch:
		t.Fatal("WaitForClear fired before Delete")
	default:
	}

	// Delete should signal the channel
	mgr.Delete("sess1")

	select {
	case <-ch:
		// expected
	default:
		t.Fatal("WaitForClear did not fire after Delete")
	}
}

func TestPendingQuestionWaitForClearWithoutSet(t *testing.T) {
	mgr := NewPendingQuestionManager()
	ch := mgr.WaitForClear("sess2")

	// Delete on a non-existent session should still signal the waiter
	mgr.Delete("sess2")

	select {
	case <-ch:
		// expected
	default:
		t.Fatal("WaitForClear did not fire for Delete on empty session")
	}
}

func TestHandlePendingQuestion_NoPending(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0, 0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/pending-question", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["pending"] != false {
		t.Errorf("pending=%v, want false", result["pending"])
	}
}

func TestHandleDismissQuestion(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0, 0)

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/question-dismiss", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func TestHandleDismissQuestion_NotManaged(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.UpsertSession("computer1", "/tmp/proj", "")

	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/question-dismiss", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400 for non-managed session", resp.StatusCode)
	}
}
