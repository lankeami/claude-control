# New Project Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to create a new project directory (with git init + .gitignore) directly from the browse modal, creating a managed session pointing at it.

**Architecture:** New `POST /api/sessions/create-project` endpoint validates the project name via regex whitelist, creates the directory, initializes git, writes a `.gitignore`, and delegates to the existing `CreateManagedSession` DB method. Frontend adds a "New Project" input row to the browse modal. Also includes a fix for file viewer scrolling.

**Tech Stack:** Go (server), Alpine.js (frontend), SQLite (db)

---

### Task 1: Fix file viewer scrolling bug

The `<template x-if>` wrapper `<div>` elements inside `.file-viewer-column` don't participate in flex layout, breaking `overflow-y: auto` on `.file-viewer-body`.

**Files:**
- Modify: `server/web/static/index.html:179` (file viewer wrapper div)
- Modify: `server/web/static/index.html:206` (issue viewer wrapper div)

- [ ] **Step 1: Add flex properties to file viewer wrapper div**

In `server/web/static/index.html`, line 179, change:
```html
<div>
```
to:
```html
<div style="display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden">
```

- [ ] **Step 2: Add flex properties to issue viewer wrapper div**

In `server/web/static/index.html`, line 206, change:
```html
<div>
```
to:
```html
<div style="display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden">
```

- [ ] **Step 3: Verify scrolling works**

Run the server (`cd server && go run .`), open the web UI, navigate to a managed session, click a file to open the file viewer, and confirm both diff and full views scroll properly for files longer than the viewport.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "fix: restore file viewer scrolling by adding flex properties to template wrapper divs"
```

---

### Task 2: Add project name validation and create-project handler

**Files:**
- Create: `server/api/create_project.go`
- Create: `server/api/create_project_test.go`

- [ ] **Step 1: Write failing tests for name validation**

Create `server/api/create_project_test.go`:

```go
package api

import (
	"testing"
)

func TestValidProjectName(t *testing.T) {
	valid := []string{"a", "my-project", "hello.world", "test_123", "A1", "x"}
	for _, name := range valid {
		if !isValidProjectName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"",                          // empty
		"-start",                    // starts with hyphen
		".start",                    // starts with dot
		"end-",                      // ends with hyphen
		"end.",                      // ends with dot
		"has space",                 // space
		"semi;colon",                // shell metachar
		"pipe|here",                 // shell metachar
		"dollar$sign",              // shell metachar
		"back`tick",                 // shell metachar
		"amp&ersand",               // shell metachar
		"paren(s)",                 // shell metachar
		"slash/path",               // path separator
		"back\\slash",              // backslash
		string(make([]byte, 256)),  // too long (256 chars)
	}
	for _, name := range invalid {
		if isValidProjectName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestValidProjectName`
Expected: FAIL — `isValidProjectName` is not defined

- [ ] **Step 3: Implement name validation and create-project handler**

Create `server/api/create_project.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var projectNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,253}[a-zA-Z0-9])?$`)

func isValidProjectName(name string) bool {
	return projectNameRegex.MatchString(name)
}

var defaultGitignore = `# OS
.DS_Store
Thumbs.db

# Environment
.env
.env.*

# IDE
.idea/
.vscode/

# Dependencies (common)
node_modules/
vendor/
__pycache__/
*.pyc
.venv/

# Build output
dist/
build/

# Logs
*.log
`

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentPath string `json:"parent_path"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ParentPath == "" || req.Name == "" {
		http.Error(w, "parent_path and name are required", http.StatusBadRequest)
		return
	}

	// Validate project name
	if !isValidProjectName(req.Name) {
		http.Error(w, "invalid project name: use letters, numbers, hyphens, dots, or underscores", http.StatusBadRequest)
		return
	}

	// Resolve parent path (with symlink resolution)
	absParent, err := filepath.Abs(req.ParentPath)
	if err != nil {
		http.Error(w, "invalid parent path", http.StatusBadRequest)
		return
	}
	absParent, err = filepath.EvalSymlinks(absParent)
	if err != nil {
		http.Error(w, "parent path does not exist", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absParent)
	if err != nil || !info.IsDir() {
		http.Error(w, "parent path is not a directory", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(absParent, req.Name)

	// Check target doesn't already exist
	if _, err := os.Stat(fullPath); err == nil {
		http.Error(w, "directory already exists", http.StatusConflict)
		return
	}

	// Create directory (not MkdirAll — fail if parent doesn't exist)
	if err := os.Mkdir(fullPath, 0755); err != nil {
		http.Error(w, "failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Git init
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = fullPath
	if out, err := gitCmd.CombinedOutput(); err != nil {
		os.RemoveAll(fullPath)
		http.Error(w, fmt.Sprintf("git init failed: %s", string(out)), http.StatusInternalServerError)
		return
	}

	// Write .gitignore
	if err := os.WriteFile(filepath.Join(fullPath, ".gitignore"), []byte(defaultGitignore), 0644); err != nil {
		os.RemoveAll(fullPath)
		http.Error(w, "failed to write .gitignore", http.StatusInternalServerError)
		return
	}

	// Create managed session
	sess, err := s.store.CreateManagedSession(
		fullPath,
		`["Bash","Read","Edit","Write","Glob","Grep"]`,
		50,
		5.0,
	)
	if err != nil {
		os.RemoveAll(fullPath)
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "session already exists for this directory", http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}
```

- [ ] **Step 4: Run validation tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestValidProjectName`
Expected: PASS

- [ ] **Step 5: Write API integration tests**

Add to `server/api/create_project_test.go`:

```go
import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	expectedCWD := filepath.Join(parentDir, "test-project")
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
```

- [ ] **Step 6: Register the route**

In `server/api/router.go`, add after line 50 (the `POST /api/sessions/create` line):

```go
apiMux.HandleFunc("POST /api/sessions/create-project", s.handleCreateProject)
```

- [ ] **Step 7: Run all tests**

Run: `cd server && go test ./api/ -v`
Expected: All tests PASS (including new create-project tests)

- [ ] **Step 8: Commit**

```bash
git add server/api/create_project.go server/api/create_project_test.go server/api/router.go
git commit -m "feat: add POST /api/sessions/create-project endpoint with name validation"
```

---

### Task 3: Add "New Project" UI to browse modal

**Files:**
- Modify: `server/web/static/index.html:386-394` (browse modal, before actions div)
- Modify: `server/web/static/app.js` (add `newProjectName`, `newProjectError`, `newProjectCreating` state + `createNewProject()` method)

- [ ] **Step 1: Add state variables to app.js**

In `server/web/static/app.js`, find the Alpine data initialization where `browseFilter`, `browseConfirmed` etc. are defined. Add these new state variables nearby:

```js
newProjectName: '',
newProjectError: '',
newProjectCreating: false,
```

- [ ] **Step 2: Add client-side name validation getter**

Add this computed property near `filteredBrowseEntries`:

```js
get isValidNewProjectName() {
  const name = this.newProjectName.trim();
  if (!name) return false;
  return /^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,253}[a-zA-Z0-9])?$/.test(name);
},
```

- [ ] **Step 3: Add createNewProject method**

Add this method near `createManagedSession()`:

```js
async createNewProject() {
  if (!this.isValidNewProjectName || this.newProjectCreating) return;
  this.newProjectCreating = true;
  this.newProjectError = '';
  try {
    const res = await fetch('/api/sessions/create-project', {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ parent_path: this.browsePath, name: this.newProjectName.trim() })
    });
    if (!res.ok) {
      const errText = await res.text();
      if (res.status === 409 && errText.includes('directory already exists')) {
        this.newProjectError = 'Directory already exists. Select it from the list above.';
      } else if (res.status === 400) {
        this.newProjectError = 'Invalid name. Use letters, numbers, hyphens, dots, or underscores.';
      } else {
        this.newProjectError = 'Failed to create project. Please try again.';
      }
      return;
    }
    this.showNewSessionModal = false;
    this.newProjectName = '';
    this.newProjectError = '';
    this.toast('Project created');
    await this.loadSessions();
  } catch (e) {
    this.newProjectError = 'Error: ' + e.message;
  } finally {
    this.newProjectCreating = false;
  }
},
```

- [ ] **Step 4: Add "New Project" input row to browse modal HTML**

In `server/web/static/index.html`, after the browse-list `</div>` (line 386) and before the actions div (line 388), add:

```html
      <!-- New Project -->
      <div style="border-top:1px solid var(--border); padding-top:10px; margin-top:8px;">
        <div style="font-size:11px; color:var(--text-muted); margin-bottom:6px;">Create new project in this folder:</div>
        <div style="display:flex; gap:8px;">
          <input x-model="newProjectName" type="text" placeholder="New project name..."
                 style="flex:1; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;"
                 @keydown.enter.prevent="createNewProject()">
          <button class="btn btn-sm btn-primary" @click="createNewProject()"
                  :disabled="!isValidNewProjectName || newProjectCreating"
                  x-text="newProjectCreating ? 'Creating...' : 'Create'"></button>
        </div>
        <div x-show="newProjectName && !isValidNewProjectName" style="font-size:11px; color:var(--red); margin-top:4px;">
          Use letters, numbers, hyphens, dots, or underscores. Must start/end with alphanumeric.
        </div>
        <div x-show="newProjectError" style="font-size:11px; color:var(--red); margin-top:4px;" x-text="newProjectError"></div>
      </div>
```

- [ ] **Step 5: Reset new project state when modal opens**

In the `openNewSessionModal()` method in app.js (where `showNewSessionModal = true` is set), add:

```js
this.newProjectName = '';
this.newProjectError = '';
this.newProjectCreating = false;
```

- [ ] **Step 6: Verify end-to-end**

Run the server (`cd server && go run .`), open the web UI:
1. Click "New Session" to open the browse modal
2. Navigate to a parent directory
3. Type a project name in the "New project name..." input
4. Verify client-side validation shows/hides the error hint
5. Click "Create" and verify a new session appears in the sidebar
6. Navigate into the created directory and verify `.git` and `.gitignore` exist

- [ ] **Step 7: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat: add New Project creation UI to browse modal"
```

---

### Task 4: Run full test suite and open PR

- [ ] **Step 1: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Build the server**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles successfully

- [ ] **Step 3: Create feature branch and push**

```bash
git checkout -b feat/new-project-creation
git push -u origin feat/new-project-creation
```

- [ ] **Step 4: Open draft PR**

```bash
gh pr create --draft --title "feat: add new project creation from browse modal" --body "$(cat <<'EOF'
## Summary
- Adds `POST /api/sessions/create-project` endpoint that creates a directory, runs `git init`, writes a default `.gitignore`, and creates a managed session
- Project name validated via regex whitelist (prevents injection/traversal)
- "New Project" input added to the browse modal UI with client-side validation and error handling
- Fixes file viewer scrolling bug (template wrapper divs missing flex properties)

Closes #3

## Test plan
- [ ] Run `cd server && go test ./... -v` — all pass
- [ ] Create a new project from the browse modal — verify directory created with `.git` and `.gitignore`
- [ ] Try invalid names (special chars, spaces) — verify rejected with error message
- [ ] Try creating in a directory that already exists — verify 409 error
- [ ] Verify file viewer scrolling still works for long files
EOF
)"
```
