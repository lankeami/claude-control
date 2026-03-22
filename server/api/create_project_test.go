package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidProjectName(t *testing.T) {
	valid := []string{"a", "my-project", "hello.world", "test_123", "A1", "x", "go"}
	for _, name := range valid {
		if !isValidProjectName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"",                         // empty
		"-start",                   // starts with hyphen
		".start",                   // starts with dot
		"end-",                     // ends with hyphen
		"end.",                     // ends with dot
		"a.",                       // 2-char ending with dot
		"has space",                // space
		"semi;colon",               // shell metachar
		"pipe|here",                // shell metachar
		"dollar$sign",              // shell metachar
		"back`tick",                // shell metachar
		"amp&ersand",               // shell metachar
		"paren(s)",                 // shell metachar
		"slash/path",               // path separator
		"back\\slash",              // backslash
		string(make([]byte, 256)),  // too long (256 null bytes — also invalid chars)
	}
	for _, name := range invalid {
		if isValidProjectName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestCreateProjectAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	parentDir := t.TempDir()
	body := `{"parent_path":"` + parentDir + `","name":"test-project"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create-project", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("status=%d, want 201", resp.StatusCode)
	}

	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	if sess["mode"] != "managed" {
		t.Errorf("mode=%v, want managed", sess["mode"])
	}

	// Resolve symlinks on the parent (macOS /var -> /private/var)
	resolvedParent, _ := filepath.EvalSymlinks(parentDir)
	expectedCWD := filepath.Join(resolvedParent, "test-project")
	if sess["cwd"] != expectedCWD {
		t.Errorf("cwd=%v, want %s", sess["cwd"], expectedCWD)
	}

	// Verify directory exists with .git and .gitignore
	if _, err := os.Stat(filepath.Join(expectedCWD, ".git")); err != nil {
		t.Error("expected .git directory to exist")
	}
	if _, err := os.Stat(filepath.Join(expectedCWD, ".gitignore")); err != nil {
		t.Error("expected .gitignore to exist")
	}
}

func TestCreateProjectInvalidName(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	parentDir := t.TempDir()
	body := `{"parent_path":"` + parentDir + `","name":"bad;name"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create-project", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestCreateProjectDuplicateDirectory(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	parentDir := t.TempDir()
	os.Mkdir(filepath.Join(parentDir, "existing"), 0755)

	body := `{"parent_path":"` + parentDir + `","name":"existing"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create-project", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 409 {
		t.Errorf("status=%d, want 409", resp.StatusCode)
	}
}

func TestCreateProjectBadParentPath(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"parent_path":"/nonexistent/path/that/does/not/exist","name":"test-project"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create-project", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}

func TestCreateProjectMissingFields(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"parent_path":"","name":""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create-project", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400", resp.StatusCode)
	}
}
