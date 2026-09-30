package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestListSkills_IncludesPath(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a temp directory with a skill
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".claude", "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: my-skill\ndescription: A test skill\n---\nBody"), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := store.CreateManagedSession(dir, "[]", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/skills", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var skills []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Dir         string `json:"dir"`
		Path        string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&skills); err != nil {
		t.Fatalf("decode: %v", err)
	}

	found := false
	for _, s := range skills {
		if s.Name == "my-skill" {
			found = true
			if s.Path == "" {
				t.Error("skill 'my-skill' has empty path, want non-empty")
			}
			if s.Path != skillFile {
				t.Errorf("path=%q, want %q", s.Path, skillFile)
			}
			if s.Description != "A test skill" {
				t.Errorf("description=%q, want 'A test skill'", s.Description)
			}
			break
		}
	}
	if !found {
		t.Errorf("skill 'my-skill' not found in response; got %d skills", len(skills))
	}
}
