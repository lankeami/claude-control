package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	content := "---\nname: test:cmd\ndescription: A test command\nargument-hint: <file>\n---\nPrompt body here"
	meta, body := parseFrontmatter(content)
	if meta.Name != "test:cmd" {
		t.Errorf("name=%q, want test:cmd", meta.Name)
	}
	if meta.Description != "A test command" {
		t.Errorf("description=%q, want 'A test command'", meta.Description)
	}
	if meta.ArgumentHint != "<file>" {
		t.Errorf("argument-hint=%q, want '<file>'", meta.ArgumentHint)
	}
	if body != "Prompt body here" {
		t.Errorf("body=%q, want 'Prompt body here'", body)
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	content := "Just a plain prompt"
	meta, body := parseFrontmatter(content)
	if meta.Name != "" {
		t.Errorf("expected empty name, got %q", meta.Name)
	}
	if body != "Just a plain prompt" {
		t.Errorf("body=%q, want full content", body)
	}
}

func TestDiscoverCommands(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "deploy.md"),
		[]byte("---\nname: deploy\ndescription: Deploy the app\n---\nDeploy prompt"), 0644)

	sub := filepath.Join(dir, "gsd")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "help.md"),
		[]byte("---\nname: gsd:help\ndescription: Show help\nargument-hint: [topic]\n---\nHelp prompt"), 0644)

	// File with no frontmatter — should use filename as name
	os.WriteFile(filepath.Join(dir, "quick.md"), []byte("No frontmatter prompt"), 0644)

	cmds := discoverCommands(dir, "project")
	if len(cmds) != 3 {
		t.Fatalf("got %d commands, want 3", len(cmds))
	}

	byName := map[string]slashCommand{}
	for _, c := range cmds {
		byName[c.Name] = c
	}

	if c, ok := byName["/gsd:help"]; !ok {
		t.Error("/gsd:help not found")
	} else {
		if c.Description != "Show help" {
			t.Errorf("description=%q", c.Description)
		}
		if !c.HasArg || c.ArgHint != "[topic]" {
			t.Errorf("hasArg=%v, argHint=%q", c.HasArg, c.ArgHint)
		}
		if c.Source != "project" {
			t.Errorf("source=%q, want project", c.Source)
		}
	}

	if c, ok := byName["/quick"]; !ok {
		t.Error("/quick not found (should derive name from filename)")
	} else if c.Description != "" {
		t.Errorf("expected empty description for no-frontmatter file, got %q", c.Description)
	}
}

func TestHandleListCommands(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, ".claude", "commands")
	os.MkdirAll(cmdDir, 0755)
	os.WriteFile(filepath.Join(cmdDir, "test.md"),
		[]byte("---\nname: test\ndescription: Test cmd\n---\ntest body"), 0644)

	sess, _ := store.CreateManagedSession(tmpDir, `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var cmds []slashCommand
	json.NewDecoder(resp.Body).Decode(&cmds)

	// Should have 5 built-in + 1 custom
	if len(cmds) < 6 {
		t.Errorf("got %d commands, want at least 6", len(cmds))
	}

	foundBuiltin, foundCustom := false, false
	for _, c := range cmds {
		if c.Name == "/help" && c.Source == "builtin" {
			foundBuiltin = true
		}
		if c.Name == "/test" && c.Source == "project" {
			foundCustom = true
		}
	}
	if !foundBuiltin {
		t.Error("/help builtin not found")
	}
	if !foundCustom {
		t.Error("/test custom not found")
	}
}

func TestHandleCommandContent(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, ".claude", "commands")
	os.MkdirAll(cmdDir, 0755)
	os.WriteFile(filepath.Join(cmdDir, "greet.md"),
		[]byte("---\nname: greet\ndescription: Greet someone\nargument-hint: [name]\n---\nHello, please greet $ARGUMENTS warmly."), 0644)

	sess, _ := store.CreateManagedSession(tmpDir, `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands/content?name=/greet", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result struct {
		Content string `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Content != "Hello, please greet $ARGUMENTS warmly." {
		t.Errorf("content=%q", result.Content)
	}
}

func TestHandleCommandContentNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/nonexistent", `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands/content?name=/nope", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}
