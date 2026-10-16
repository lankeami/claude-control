package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMakefileTargets(t *testing.T) {
	dir := t.TempDir()
	makefilePath := filepath.Join(dir, "Makefile")
	content := `# My Project Makefile

PORT ?= 8080

.PHONY: build test run clean

##@ Build

build: ## Build the binary
	go build -o app .

test: build ## Run all tests
	go test ./...

run: build ## Start the server
	./app --port $(PORT)

clean: ## Clean build artifacts
	rm -f app

_internal:
	echo "internal"

deploy:
	echo "no description"
`
	if err := os.WriteFile(makefilePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := parseMakefileTargets(makefilePath)
	if err != nil {
		t.Fatal(err)
	}

	// Should find: build, test, run, clean, deploy (not .PHONY, not _internal)
	if len(targets) != 5 {
		t.Fatalf("got %d targets, want 5; targets: %+v", len(targets), targets)
	}

	byName := map[string]makeTarget{}
	for _, tgt := range targets {
		byName[tgt.Name] = tgt
	}

	// Check build target
	if b, ok := byName["build"]; !ok {
		t.Error("missing 'build' target")
	} else {
		if b.Description != "Build the binary" {
			t.Errorf("build description=%q, want 'Build the binary'", b.Description)
		}
		if b.Line == 0 {
			t.Error("build line should be non-zero")
		}
		if b.FilePath != makefilePath {
			t.Errorf("build filePath=%q, want %q", b.FilePath, makefilePath)
		}
	}

	// Check test target has description
	if tgt, ok := byName["test"]; !ok {
		t.Error("missing 'test' target")
	} else if tgt.Description != "Run all tests" {
		t.Errorf("test description=%q, want 'Run all tests'", tgt.Description)
	}

	// Check deploy target has no description
	if d, ok := byName["deploy"]; !ok {
		t.Error("missing 'deploy' target")
	} else if d.Description != "" {
		t.Errorf("deploy description=%q, want empty", d.Description)
	}

	// _internal and .PHONY should not appear
	if _, ok := byName["_internal"]; ok {
		t.Error("_internal target should be skipped")
	}
}

func TestParseMakefileTargets_NoFile(t *testing.T) {
	_, err := parseMakefileTargets("/nonexistent/Makefile")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseMakefileTargets_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	makefilePath := filepath.Join(dir, "Makefile")
	os.WriteFile(makefilePath, []byte(""), 0644)

	targets, err := parseMakefileTargets(makefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Errorf("got %d targets, want 0", len(targets))
	}
}

func TestListMakeTargets_WithMakefile(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	dir := t.TempDir()
	makefilePath := filepath.Join(dir, "Makefile")
	content := `build: ## Build
	go build .

test: ## Test
	go test ./...
`
	os.WriteFile(makefilePath, []byte(content), 0644)

	sess, err := store.CreateManagedSession(dir, "[]", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/make-targets", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var targets []makeTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}

	if targets[0].Name != "build" || targets[0].Description != "Build" {
		t.Errorf("first target: name=%q desc=%q", targets[0].Name, targets[0].Description)
	}
}

func TestListMakeTargets_NoMakefile(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	dir := t.TempDir()
	// No Makefile created

	sess, err := store.CreateManagedSession(dir, "[]", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateManagedSession: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/make-targets", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var targets []makeTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(targets) != 0 {
		t.Fatalf("got %d targets, want 0", len(targets))
	}
}

func TestListMakeTargets_SessionNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/nonexistent/make-targets", nil)
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
