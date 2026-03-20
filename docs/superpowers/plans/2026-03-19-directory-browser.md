# Directory Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-side directory browser so users can pick project folders visually instead of typing full paths.

**Architecture:** New `GET /api/browse` endpoint returns child directories for a given path, with git repo detection. Web UI replaces the plain text input in the New Session modal with a clickable folder browser + breadcrumb navigation.

**Tech Stack:** Go (net/http, os, path/filepath), Alpine.js (existing), HTML/CSS (existing patterns)

---

### Task 1: Go API endpoint — `GET /api/browse`

**Files:**
- Create: `server/api/browse.go`
- Modify: `server/api/router.go:21` (add route)

- [ ] **Step 1: Create `server/api/browse.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type dirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"is_git_repo"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
			return
		}
		dirPath = home
	}

	// Resolve to absolute and clean
	dirPath, err := filepath.Abs(dirPath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		http.Error(w, "path is not a directory", http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusForbidden)
		return
	}

	var dirs []dirEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip hidden directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(dirPath, name)
		gitDir := filepath.Join(fullPath, ".git")
		_, gitErr := os.Stat(gitDir)
		dirs = append(dirs, dirEntry{
			Name:      name,
			Path:      fullPath,
			IsGitRepo: gitErr == nil,
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current": dirPath,
		"entries": dirs,
	})
}
```

- [ ] **Step 2: Register the route in `router.go`**

Add after the existing API routes (line ~44, before managed session endpoints):

```go
// Browse endpoint
apiMux.HandleFunc("GET /api/browse", s.handleBrowse)
```

- [ ] **Step 3: Test the endpoint**

```bash
cd server && go build -o claude-controller . && echo "Build OK"
```

Then manually test:
```bash
curl -s -H "Authorization: Bearer <key>" "http://localhost:8080/api/browse" | jq .
curl -s -H "Authorization: Bearer <key>" "http://localhost:8080/api/browse?path=/Users/jaychinthrajah/workspaces" | jq .
```

Expected: JSON with `current` (string) and `entries` (array of `{name, path, is_git_repo}`), no hidden dirs.

- [ ] **Step 4: Commit**

```bash
git add server/api/browse.go server/api/router.go
git commit -m "feat: add GET /api/browse endpoint for directory listing"
```

---

### Task 2: Web UI — folder browser in New Session modal

**Files:**
- Modify: `server/web/static/app.js` (add browse state + methods)
- Modify: `server/web/static/index.html` (replace modal content)
- Modify: `server/web/static/style.css` (add browser styles)

- [ ] **Step 1: Add browse state and methods to `app.js`**

Add these properties to the Alpine data object (after `newSessionCWD: ''`):

```javascript
// Directory browser state
browsePath: '',
browseEntries: [],
browseLoading: false,
```

Add these methods:

```javascript
async openNewSessionModal() {
  this.showNewSessionModal = true;
  this.newSessionCWD = '';
  this.browsePath = '';
  this.browseEntries = [];
  await this.browseTo('');
},

async browseTo(path) {
  this.browseLoading = true;
  try {
    const url = '/api/browse' + (path ? '?path=' + encodeURIComponent(path) : '');
    const res = await fetch(url, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    this.browsePath = data.current;
    this.browseEntries = data.entries || [];
    this.newSessionCWD = data.current;
  } catch (e) {
    this.toast('Error browsing: ' + e.message);
  }
  this.browseLoading = false;
},

get breadcrumbs() {
  if (!this.browsePath) return [];
  const home = this.browsePath.split('/').slice(0, 3).join('/'); // e.g. /Users/username
  const parts = this.browsePath.split('/').filter(Boolean);
  const crumbs = [];
  let accumulated = '';
  for (let i = 0; i < parts.length; i++) {
    accumulated += '/' + parts[i];
    const isHome = accumulated === home;
    crumbs.push({
      label: isHome ? '~' : parts[i],
      path: accumulated,
      skipPrior: isHome // collapse everything before ~ into the ~ crumb
    });
  }
  // Only show from ~ onward
  const homeIdx = crumbs.findIndex(c => c.skipPrior);
  return homeIdx >= 0 ? crumbs.slice(homeIdx) : crumbs;
},
```

- [ ] **Step 2: Update the "New Session" button to call `openNewSessionModal()`**

In `index.html`, change:
```html
@click="showNewSessionModal = true"
```
to:
```html
@click="openNewSessionModal()"
```

- [ ] **Step 3: Replace the New Session modal body in `index.html`**

Replace the entire modal `<div>` content with the folder browser UI:

```html
<!-- New Session Modal -->
<div x-show="showNewSessionModal" x-cloak
     style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:100; display:flex; align-items:center; justify-content:center;"
     @click.self="showNewSessionModal = false">
  <div style="background:var(--bg); border-radius:12px; padding:24px; width:500px; max-width:90vw; border:1px solid var(--border); display:flex; flex-direction:column; max-height:70vh;">
    <h3 style="margin-top:0; margin-bottom:12px;">New Managed Session</h3>

    <!-- Manual path input -->
    <div style="display:flex; gap:8px; margin-bottom:12px;">
      <input x-model="newSessionCWD" type="text" placeholder="/path/to/project"
             style="flex:1; padding:8px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); font-size:13px; box-sizing:border-box;"
             @keydown.enter="createManagedSession()">
      <button class="btn btn-sm" @click="browseTo(newSessionCWD)" style="white-space:nowrap;">Go</button>
    </div>

    <!-- Breadcrumb navigation -->
    <div class="browse-breadcrumbs">
      <template x-for="(crumb, i) in breadcrumbs" :key="crumb.path">
        <span>
          <span x-show="i > 0" style="color:var(--text-muted); margin:0 2px;">/</span>
          <a href="#" @click.prevent="browseTo(crumb.path)" x-text="crumb.label"
             style="color:var(--accent); text-decoration:none; font-size:13px;"></a>
        </span>
      </template>
    </div>

    <!-- Directory list -->
    <div class="browse-list">
      <template x-if="browseLoading">
        <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">Loading...</div>
      </template>
      <template x-if="!browseLoading && browseEntries.length === 0">
        <div style="padding:1rem; text-align:center; color:var(--text-muted); font-size:13px;">No subdirectories</div>
      </template>
      <template x-for="entry in browseEntries" :key="entry.path">
        <div class="browse-item" @click="browseTo(entry.path)">
          <span class="browse-icon" x-text="entry.is_git_repo ? '&#9679;' : '&#128193;'"
                :style="entry.is_git_repo ? 'color:var(--green)' : 'color:var(--text-muted)'"></span>
          <span style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="entry.name"></span>
          <span x-show="entry.is_git_repo" style="font-size:10px; color:var(--green); flex-shrink:0;">git</span>
        </div>
      </template>
    </div>

    <!-- Actions -->
    <div style="display:flex; gap:8px; justify-content:flex-end; margin-top:12px; flex-shrink:0;">
      <button class="btn btn-sm" @click="showNewSessionModal = false">Cancel</button>
      <button class="btn btn-sm btn-primary" @click="createManagedSession()" :disabled="!newSessionCWD.trim()">
        Select This Folder
      </button>
    </div>
  </div>
</div>
```

- [ ] **Step 4: Add browse styles to `style.css`**

Append to the end of the file (before the mobile media query):

```css
/* Directory browser */
.browse-breadcrumbs {
  padding: 6px 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  margin-bottom: 8px;
  min-height: 28px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0;
}

.browse-list {
  flex: 1;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 6px;
  min-height: 200px;
}

.browse-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  font-size: 13px;
  border-bottom: 1px solid var(--border);
  transition: background 0.1s;
}
.browse-item:last-child { border-bottom: none; }
.browse-item:hover { background: var(--bg-secondary); }

.browse-icon {
  flex-shrink: 0;
  font-size: 14px;
  width: 18px;
  text-align: center;
}
```

- [ ] **Step 5: Test the full flow**

1. Build and run: `cd server && go build -o claude-controller . && go run .`
2. Open the web UI, click "+ New Session"
3. Verify: modal opens with `~` contents, breadcrumbs show `~`
4. Click a directory — verify navigation works
5. Click a breadcrumb segment — verify it jumps back
6. Verify git repos show green dot + "git" label
7. Type a path in the text input, click Go — verify it navigates
8. Click "Select This Folder" — verify session creation

- [ ] **Step 6: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html server/web/static/style.css
git commit -m "feat: add folder browser UI for session creation"
```
