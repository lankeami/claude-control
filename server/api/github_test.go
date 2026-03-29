package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListGithubIssues_HookModeReturns400(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// UpsertSession creates a hook-mode session
	sess, err := store.UpsertSession("test-computer", "/tmp/hook-test", "")
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestListGithubIssues_SessionNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/nonexistent-id/github/issues", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestListGithubIssues_ManagedNoCWDReturns400(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// CreateManagedSession with empty cwd
	sess, err := store.CreateManagedSession("", `[]`, 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestListGithubIssues_InvalidStateParam(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp", `[]`, 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues?state=invalid", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestGetGithubIssue_HookModeReturns400(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, err := store.UpsertSession("test-computer", "/tmp/hook-test", "")
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues/42", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestGetGithubIssue_SessionNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/nonexistent-id/github/issues/42", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestGetGithubIssue_InvalidNumberReturns400(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp", `[]`, 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues/notanumber", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestGetGithubIssue_ManagedNoCWDReturns400(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, err := store.CreateManagedSession("", `[]`, 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues/42", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestListGithubIssues_DefaultParamsAccepted(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp", `[]`, 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/github/issues", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Valid default params must not produce a 400 (could be 500 if gh is not available)
	if resp.StatusCode == http.StatusBadRequest {
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("got 400 with valid default params, body=%v", body)
	}
}
