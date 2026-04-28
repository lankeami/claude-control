# New Session Modal Usability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three usability issues in the New Session modal: new-folder input width, auto-navigation to newly created sessions, and recursive directory search.

**Architecture:** Four targeted changes — a new Go search endpoint in `browse.go`, a route registration in `router.go`, JS logic fixes + search state in `app.js`, and HTML restructuring + search UI in `index.html`. No database changes, no new CSS classes, no new files.

**Tech Stack:** Go 1.21+ (stdlib only), Alpine.js v3 (CDN), vanilla HTML/CSS

---

## File Map

| File | What changes |
|------|-------------|
| `server/api/browse.go` | Add `handleBrowseSearch` handler |
| `server/api/router.go` | Register `GET /api/browse/search` route |
| `server/api/browse_test.go` | New file — tests for `handleBrowseSearch` |
| `server/web/static/app.js` | Add `dirSearch`/`dirSearchResults`/`dirSearchLoading`/`_dirSearchTimer` state; add `onDirSearchInput()`; fix `createManagedSession()` and `createNewProject()` navigation; reset new state in `openNewSessionModal()` |
| `server/web/static/index.html` | Move new-folder section to full-width strip; add search input + conditional results/browse lists |

---

## Task 1: Backend — Add `handleBrowseSearch` + register route

**Files:**
- Modify: `server/api/browse.go`
- Create: `server/api/browse_test.go`
- Modify: `server/api/router.go`

- [ ] **Step 1: Create `server/api/browse_test.go` with a failing test**

```go
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
```

- [ ] **Step 2: Run the tests to confirm they fail (handler not yet registered)**

```bash
cd server && go test ./api/ -run TestHandleBrowseSearch -v
```

Expected: FAIL — `"connect: connection refused"` or 404/405 on the endpoint.

- [ ] **Step 3: Add `handleBrowseSearch` to `server/api/browse.go`**

Append after the closing `}` of `handleBrowse`:

```go
func (s *Server) handleBrowseSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	basePath := r.URL.Query().Get("path")
	if basePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
			return
		}
		basePath = home
	}

	basePath, err := filepath.Abs(basePath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		http.Error(w, "path is not a directory", http.StatusBadRequest)
		return
	}

	baseDepth := strings.Count(basePath, string(filepath.Separator))
	maxDepth := baseDepth + 5
	qLower := strings.ToLower(q)

	var results []dirEntry
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if path == basePath {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if strings.Count(path, string(filepath.Separator)) > maxDepth {
			return filepath.SkipDir
		}
		if strings.Contains(strings.ToLower(name), qLower) && len(results) < 50 {
			gitDir := filepath.Join(path, ".git")
			_, gitErr := os.Stat(gitDir)
			results = append(results, dirEntry{
				Name:      name,
				Path:      path,
				IsGitRepo: gitErr == nil,
			})
		}
		return nil
	})

	if results == nil {
		results = []dirEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": results,
	})
}
```

- [ ] **Step 4: Register the route in `server/api/router.go`**

Find the line:
```go
// Browse endpoint
apiMux.HandleFunc("GET /api/browse", s.handleBrowse)
```

Replace it with:
```go
// Browse endpoints
apiMux.HandleFunc("GET /api/browse", s.handleBrowse)
apiMux.HandleFunc("GET /api/browse/search", s.handleBrowseSearch)
```

- [ ] **Step 5: Run the tests and verify they pass**

```bash
cd server && go test ./api/ -run TestHandleBrowseSearch -v
```

Expected output:
```
--- PASS: TestHandleBrowseSearch (0.00s)
--- PASS: TestHandleBrowseSearchEmptyQuery (0.00s)
--- PASS: TestHandleBrowseSearchDepthCap (0.00s)
PASS
```

- [ ] **Step 6: Run the full test suite to verify nothing regressed**

```bash
cd server && go test ./... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git checkout -b feat/new-session-modal-usability
git add server/api/browse.go server/api/browse_test.go server/api/router.go
git commit -m "feat: add recursive directory search endpoint GET /api/browse/search"
```

---

## Task 2: Fix session navigation — `createManagedSession` and `createNewProject`

**Files:**
- Modify: `server/web/static/app.js`

These two functions create a session but never navigate to it. The fix is to parse the response JSON, call `pollState()` to refresh `this.sessions`, then call `selectSession(sess.id)`.

- [ ] **Step 1: Fix `createManagedSession` in `app.js`**

Find the existing function (around line 1484):
```js
async createManagedSession() {
  if (!this.newSessionCWD.trim()) return;
  try {
    const res = await fetch('/api/sessions/create', {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ cwd: this.newSessionCWD.trim() })
    });
    if (!res.ok) throw new Error(await res.text());
    this.showNewSessionModal = false;
    this.newSessionCWD = '';
    this.toast('Session created');
  } catch (e) {
    this.toast('Error: ' + e.message);
  }
},
```

Replace it with:
```js
async createManagedSession() {
  if (!this.newSessionCWD.trim()) return;
  try {
    const res = await fetch('/api/sessions/create', {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ cwd: this.newSessionCWD.trim() })
    });
    if (!res.ok) throw new Error(await res.text());
    const sess = await res.json();
    this.showNewSessionModal = false;
    this.newSessionCWD = '';
    await this.pollState();
    this.selectSession(sess.id);
    this.toast('Session created');
  } catch (e) {
    this.toast('Error: ' + e.message);
  }
},
```

- [ ] **Step 2: Fix `createNewProject` in `app.js`**

Find the existing function (around line 1529). The current success block is:
```js
    const sess = await res.json();
    this.showNewSessionModal = false;
    this.newProjectName = '';
    this.newProjectError = '';
    this.toast('Project created');
    await this.loadSessions();
    this.selectedSessionId = sess.id;
```

Replace just the success block (lines after `const sess = await res.json();`) with:
```js
    const sess = await res.json();
    this.showNewSessionModal = false;
    this.newProjectName = '';
    this.newProjectError = '';
    this.toast('Project created');
    await this.pollState();
    this.selectSession(sess.id);
```

- [ ] **Step 3: Verify the server builds cleanly**

```bash
cd server && go build -o /dev/null .
```

Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
git add server/web/static/app.js
git commit -m "fix: navigate to new session immediately after creation"
```

---

## Task 3: Move new-folder section to full-width strip

**Files:**
- Modify: `server/web/static/index.html`

The "New Folder" section currently lives inside `modal-columns-right` (~55% wide). Move it to a full-width strip between `modal-columns` and `modal-footer`.

- [ ] **Step 1: Remove the new-folder section from `modal-columns-right` in `index.html`**

Find and delete these lines inside `modal-columns-right` (around lines 1109–1128):
```html
          <!-- Collapsed New Folder -->
          <div class="new-folder-toggle" @click="showNewFolderInput = !showNewFolderInput" x-show="!showNewFolderInput">
            + New Folder
          </div>
          <div x-show="showNewFolderInput" style="margin-top:8px; padding-top:8px; border-top:1px solid var(--border);">
            <div style="display:flex; gap:8px;">
              <input x-model="newProjectName" type="text" placeholder="Folder name..."
                     class="modal-input" style="flex:1;"
                     @keydown.enter.prevent="createNewProject()"
                     @keydown.escape.prevent="showNewFolderInput = false; newProjectName = ''">
              <button class="btn btn-sm btn-primary" @click="createNewProject()"
                      :disabled="!isValidNewProjectName || newProjectCreating"
                      x-text="newProjectCreating ? 'Creating...' : 'Create'"></button>
              <button class="btn btn-sm" @click="showNewFolderInput = false; newProjectName = ''">&times;</button>
            </div>
            <div x-show="newProjectName && !isValidNewProjectName" style="font-size:11px; color:var(--red); margin-top:4px;">
              Use letters, numbers, hyphens, dots, or underscores.
            </div>
            <div x-show="newProjectError" style="font-size:11px; color:var(--red); margin-top:4px;" x-text="newProjectError"></div>
          </div>
```

- [ ] **Step 2: Insert the new-folder strip between `</div>` (closing `modal-columns`) and `<!-- Footer -->`**

After the line `      </div>` that closes `.modal-columns`, and before `      <!-- Footer -->`, insert:

```html
      <!-- New Folder strip (full-width) -->
      <div style="border-top:1px solid var(--border); padding:8px 24px 12px;">
        <div class="new-folder-toggle" @click="showNewFolderInput = !showNewFolderInput" x-show="!showNewFolderInput" style="margin-top:0; padding-top:0; border-top:none;">
          + New Folder
        </div>
        <div x-show="showNewFolderInput" style="padding-top:4px;">
          <div style="display:flex; gap:8px;">
            <input x-model="newProjectName" type="text" placeholder="Folder name..."
                   class="modal-input" style="flex:1; min-width:0;"
                   @keydown.enter.prevent="createNewProject()"
                   @keydown.escape.prevent="showNewFolderInput = false; newProjectName = ''">
            <button class="btn btn-sm btn-primary" @click="createNewProject()"
                    :disabled="!isValidNewProjectName || newProjectCreating"
                    x-text="newProjectCreating ? 'Creating...' : 'Create'"></button>
            <button class="btn btn-sm" @click="showNewFolderInput = false; newProjectName = ''">&times;</button>
          </div>
          <div x-show="newProjectName && !isValidNewProjectName" style="font-size:11px; color:var(--red); margin-top:4px;">
            Use letters, numbers, hyphens, dots, or underscores.
          </div>
          <div x-show="newProjectError" style="font-size:11px; color:var(--red); margin-top:4px;" x-text="newProjectError"></div>
        </div>
      </div>
```

- [ ] **Step 3: Verify the server builds cleanly**

```bash
cd server && go build -o /dev/null .
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "fix: move new-folder input to full-width strip matching path input width"
```

---

## Task 4: Add recursive directory search UI

**Files:**
- Modify: `server/web/static/app.js`
- Modify: `server/web/static/index.html`

Add three state fields, a debounced search function, reset on modal open, and the search input + conditional results display in the HTML.

- [ ] **Step 1: Add search state fields to the data object in `app.js`**

Find the "Directory browser state" comment block (around line 45):
```js
    // Directory browser state
    browsePath: '',
    browseEntries: [],
    browseLoading: false,
    browseFilter: '',
    browseConfirmed: false,
    newProjectName: '',
    newProjectError: '',
    newProjectCreating: false,
    showNewFolderInput: false,
    recentDirs: [],
```

Replace it with:
```js
    // Directory browser state
    browsePath: '',
    browseEntries: [],
    browseLoading: false,
    browseFilter: '',
    browseConfirmed: false,
    newProjectName: '',
    newProjectError: '',
    newProjectCreating: false,
    showNewFolderInput: false,
    recentDirs: [],
    dirSearch: '',
    dirSearchResults: [],
    dirSearchLoading: false,
    _dirSearchTimer: null,
```

- [ ] **Step 2: Reset the new search state in `openNewSessionModal` in `app.js`**

Find the `openNewSessionModal` function. After the existing resets (`this.showNewFolderInput = false;`), add:
```js
      this.dirSearch = '';
      this.dirSearchResults = [];
      this.dirSearchLoading = false;
```

- [ ] **Step 3: Add the `onDirSearchInput` method to `app.js`**

Add this method immediately after `openNewSessionModal` (before `async browseTo`):

```js
    onDirSearchInput() {
      clearTimeout(this._dirSearchTimer);
      const q = this.dirSearch.trim();
      if (!q) {
        this.dirSearchResults = [];
        this.dirSearchLoading = false;
        return;
      }
      this.dirSearchLoading = true;
      this._dirSearchTimer = setTimeout(async () => {
        try {
          const path = encodeURIComponent(this.browsePath || '');
          const res = await fetch(
            '/api/browse/search?path=' + path + '&q=' + encodeURIComponent(q),
            { headers: { 'Authorization': 'Bearer ' + this.apiKey } }
          );
          if (!res.ok) throw new Error(await res.text());
          const data = await res.json();
          this.dirSearchResults = data.entries || [];
        } catch (e) {
          this.dirSearchResults = [];
        } finally {
          this.dirSearchLoading = false;
        }
      }, 300);
    },
```

- [ ] **Step 4: Add the search input to `index.html` (inside `modal-columns-right`, below breadcrumbs)**

Find the breadcrumbs block (around line 1081):
```html
          <!-- Breadcrumbs -->
          <div class="browse-breadcrumbs" style="margin-bottom:10px;">
            ...
          </div>

          <!-- Directory list -->
```

Between the closing `</div>` of breadcrumbs and the `<!-- Directory list -->` comment, insert:

```html
          <!-- Directory search -->
          <div style="margin-bottom:8px;">
            <input x-model="dirSearch" type="text" placeholder="Search folders…"
                   class="modal-input" style="width:100%; box-sizing:border-box;"
                   @input="onDirSearchInput()">
          </div>
```

- [ ] **Step 5: Replace the directory list section in `index.html` with conditional browse/search display**

Find and replace the current `<!-- Directory list -->` block (lines ~1092–1107):
```html
          <!-- Directory list -->
          <div class="browse-list" style="flex:1; min-height:0;">
            <template x-if="browseLoading">
              <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">Loading...</div>
            </template>
            <template x-if="!browseLoading && filteredBrowseEntries.length === 0">
              <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;" x-text="browseFilter ? 'No matching directories' : 'No subdirectories'"></div>
            </template>
            <template x-for="entry in filteredBrowseEntries" :key="entry.path">
              <div class="browse-item" @click="browseFilter = ''; browseTo(entry.path)">
                <span class="browse-icon" x-text="entry.is_git_repo ? '&#9679;' : '&#128193;'"
                      :style="entry.is_git_repo ? 'color:var(--green)' : 'color:var(--text-muted)'"></span>
                <span style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="entry.name"></span>
                <span x-show="entry.is_git_repo" style="font-size:10px; color:var(--green); flex-shrink:0;">git</span>
              </div>
            </template>
          </div>
```

Replace with:
```html
          <!-- Directory list -->
          <div class="browse-list" style="flex:1; min-height:0;">
            <!-- Normal browse (no active search) -->
            <div x-show="!dirSearch">
              <template x-if="browseLoading">
                <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">Loading...</div>
              </template>
              <template x-if="!browseLoading && filteredBrowseEntries.length === 0">
                <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;" x-text="browseFilter ? 'No matching directories' : 'No subdirectories'"></div>
              </template>
              <template x-for="entry in filteredBrowseEntries" :key="entry.path">
                <div class="browse-item" @click="browseFilter = ''; browseTo(entry.path)">
                  <span class="browse-icon" x-text="entry.is_git_repo ? '&#9679;' : '&#128193;'"
                        :style="entry.is_git_repo ? 'color:var(--green)' : 'color:var(--text-muted)'"></span>
                  <span style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="entry.name"></span>
                  <span x-show="entry.is_git_repo" style="font-size:10px; color:var(--green); flex-shrink:0;">git</span>
                </div>
              </template>
            </div>
            <!-- Search results (active search) -->
            <div x-show="dirSearch">
              <template x-if="dirSearchLoading">
                <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">Searching…</div>
              </template>
              <template x-if="!dirSearchLoading && dirSearchResults.length === 0">
                <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">No matching directories</div>
              </template>
              <template x-for="entry in dirSearchResults" :key="entry.path">
                <div class="browse-item" @click="dirSearch = ''; dirSearchResults = []; browseTo(entry.path)">
                  <span class="browse-icon" x-text="entry.is_git_repo ? '&#9679;' : '&#128193;'"
                        :style="entry.is_git_repo ? 'color:var(--green)' : 'color:var(--text-muted)'"></span>
                  <div style="flex:1; min-width:0;">
                    <div style="font-weight:600; font-size:13px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="entry.name"></div>
                    <div style="font-size:10px; color:var(--text-muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="abbreviatePath(entry.path)"></div>
                  </div>
                  <span x-show="entry.is_git_repo" style="font-size:10px; color:var(--green); flex-shrink:0;">git</span>
                </div>
              </template>
            </div>
          </div>
```

- [ ] **Step 6: Verify the server builds cleanly**

```bash
cd server && go build -o /dev/null .
```

Expected: no output.

- [ ] **Step 7: Run the full test suite**

```bash
cd server && go test ./... -v 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 8: Start the server and manually verify all three fixes**

```bash
cd server && go run .
```

Then open the web UI and verify:

1. **Fix 1 — New folder width:** Open "New Session" modal → click "+ New Folder" → confirm the input expands to the same width as the path input at the top.
2. **Fix 2 — Navigation:** Create a new session via "Open" button → confirm the modal closes and the UI immediately navigates to the new session (it appears selected in the sidebar, messages panel is shown).
3. **Fix 3 — Search:** In the modal's right panel, type "claude" in the search box → confirm matching subdirectories appear recursively (e.g., `~/workspaces/_personal_/claude-control`). Click a result → confirm it navigates into that directory and clears the search.

- [ ] **Step 9: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: add recursive directory search with debounce to new session modal"
```

---

## Task 5: Push branch and open draft PR

- [ ] **Step 1: Push the feature branch**

```bash
git push -u origin feat/new-session-modal-usability
```

- [ ] **Step 2: Open draft PR linking to issue #126**

```bash
gh pr create --draft --title "fix: new session modal usability improvements" --body "$(cat <<'EOF'
## Summary

Fixes three usability issues in the New Session modal (#126):

- **New folder input width** — moved the New Folder section out of the constrained right column into a full-width strip, so its input matches the path input at the top of the modal.
- **Navigate to new session on creation** — `createManagedSession` and `createNewProject` now call `pollState()` then `selectSession()` after the API responds, immediately navigating the user to the new session. Also fixes an undefined `loadSessions()` call in `createNewProject`.
- **Recursive directory search** — new `GET /api/browse/search?path=&q=` endpoint walks up to 5 levels deep, skipping hidden directories, returning up to 50 matches. Frontend adds a debounced (300ms) search input above the directory list; results show full paths and git indicators.

## Test plan

- [ ] `cd server && go test ./api/ -run TestHandleBrowseSearch -v` passes
- [ ] `cd server && go test ./... -v` all pass
- [ ] Open New Session modal → click "+ New Folder" → input is same width as path input
- [ ] Create a session via "Open" → modal closes and new session is selected immediately
- [ ] Type "claude" in search box → `claude-control` and any other matching dirs appear
- [ ] Click a search result → navigates into that directory, search box clears

Closes #126
EOF
)"
```

Expected: PR URL printed to stdout.
