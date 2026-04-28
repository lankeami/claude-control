package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleBrowseSearch(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a temp directory tree: base/workspaces/_personal_/claude-control
	base := t.TempDir()
	personal := filepath.Join(base, "workspaces", "_personal_", "claude-control")
	if err := os.MkdirAll(personal, 0755); err != nil {
		t.Fatal(err)
	}
	// Also create a non-matching dir
	if err := os.MkdirAll(filepath.Join(base, "workspaces", "other"), 0755); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET",
		ts.URL+"/api/browse/search?path="+base+"&q=claude",
		nil)
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

	entries, ok := result["entries"].([]interface{})
	if !ok {
		t.Fatal("entries field missing or wrong type")
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d, want 1", len(entries))
	}

	entry := entries[0].(map[string]interface{})
	if entry["name"] != "claude-control" {
		t.Errorf("name=%v, want claude-control", entry["name"])
	}
}

func TestHandleBrowseSearchEmptyQuery(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/browse/search?q=", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestHandleBrowseSearchDepthCap(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a dir 6 levels deep — should NOT appear in results (cap is 5)
	base := t.TempDir()
	deep := filepath.Join(base, "a", "b", "c", "d", "e", "target-deep")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a dir 4 levels deep — SHOULD appear
	shallow := filepath.Join(base, "a", "b", "c", "target-shallow")
	if err := os.MkdirAll(shallow, 0755); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET",
		ts.URL+"/api/browse/search?path="+base+"&q=target",
		nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	entries := result["entries"].([]interface{})

	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d, want 1 (only shallow should match)", len(entries))
	}
	entry := entries[0].(map[string]interface{})
	if entry["name"] != "target-shallow" {
		t.Errorf("name=%v, want target-shallow", entry["name"])
	}
}
