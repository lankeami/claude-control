# Issue Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub issue picker to the web UI's right sidebar that lets users browse, search, and select issues, then generate a structured prompt for Claude to work on.

**Architecture:** New Go handler file (`github.go`) calls the GitHub REST API directly using `net/http`, authenticated with a `GITHUB_TOKEN` stored in the server's `.env` file. Repo detection uses `git remote get-url origin` with regex parsing. Frontend adds an issues section below the git branch info in the right sidebar using Alpine.js state management. "Generate Prompt" populates the existing chat textarea.

**Tech Stack:** Go (backend handlers, `net/http` for GitHub REST API), Alpine.js (frontend state), HTML/CSS (UI), marked.js (markdown rendering, already loaded).

**Spec:** `docs/superpowers/specs/2026-03-22-issue-picker-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `server/api/github.go` | Create | GitHub issue list/detail handlers, GitHub REST API interaction, response structs |
| `server/api/github_test.go` | Create | Tests for GitHub handlers (session mode guard, param validation) |
| `server/api/router.go` | Modify (lines 57-61) | Register two new routes |
| `server/web/static/index.html` | Modify (after line 248) | Issues section HTML in right sidebar |
| `server/web/static/app.js` | Modify | Alpine.js state, fetch methods, prompt generation |
| `server/web/static/style.css` | Modify | CSS for issues section, rows, detail panel |

---

### Task 1: Backend — GitHub Issue List Handler

**Files:**
- Create: `server/api/github.go`
- Create: `server/api/github_test.go`

- [ ] **Step 1: Write the test for listing issues**

```go
// server/api/github_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListGithubIssuesRequiresManagedSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()

	// Register a hook-mode session (no CWD)
	store.RegisterSession("hook-session", "host1", "/some/path")

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/hook-session/github/issues", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for hook session, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestListGithubIssuesRequiresManagedSession`
Expected: FAIL — `handleListGithubIssues` does not exist yet

- [ ] **Step 3: Create `github.go` with structs and list handler**

Uses GitHub REST API directly via `net/http` with a `GITHUB_TOKEN` from the `.env` file. Repo detection parses `git remote get-url origin` output with a regex supporting HTTPS and SSH URLs. Search uses the GitHub Search API (`/search/issues`) with `{ items: [...] }` unwrapping. See `server/api/github.go` for the full implementation.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./api/ -v -run TestListGithubIssuesRequiresManagedSession`
Expected: PASS

- [ ] **Step 5: Register route in router.go**

Add after line 61 in `server/api/router.go` (after the file browser endpoints):

```go
	// GitHub issue endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues", s.handleListGithubIssues)
```

- [ ] **Step 6: Run all tests to verify nothing is broken**

Run: `cd server && go test ./api/ -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add server/api/github.go server/api/github_test.go server/api/router.go
git commit -m "feat: add GitHub issue list API endpoint"
```

---

### Task 2: Backend — GitHub Issue Detail Handler

**Files:**
- Modify: `server/api/github.go`
- Modify: `server/api/github_test.go`
- Modify: `server/api/router.go`

- [ ] **Step 1: Write the test for issue detail**

Append to `server/api/github_test.go`:

```go
func TestGetGithubIssueRequiresManagedSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()

	store.RegisterSession("hook-session", "host1", "/some/path")

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/hook-session/github/issues/1", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for hook session, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestGetGithubIssueRequiresManagedSession`
Expected: FAIL — `handleGetGithubIssue` does not exist yet

- [ ] **Step 3: Add detail handler to `github.go`**

Calls `GET /repos/{owner}/{repo}/issues/{number}` on the GitHub REST API. Returns 400 if no token configured, 401 on auth failure, 404 if issue not found. See `server/api/github.go` for the full implementation.

- [ ] **Step 4: Register the route in router.go**

Add after the issues list route:

```go
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/{number}", s.handleGetGithubIssue)
```

- [ ] **Step 5: Run all tests**

Run: `cd server && go test ./api/ -v`
Expected: All PASS

- [ ] **Step 6: Build to verify compilation**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles successfully

- [ ] **Step 7: Commit**

```bash
git add server/api/github.go server/api/github_test.go server/api/router.go
git commit -m "feat: add GitHub issue detail API endpoint"
```

---

### Task 3: Frontend — CSS Styles for Issues Section

**Files:**
- Modify: `server/web/static/style.css` (append after the git status bar styles around line 553)

- [ ] **Step 1: Add CSS for the issues section**

Append after the `.git-clean` block (around line 553) in `style.css`:

```css
/* GitHub Issues Section */
.issues-section {
    padding: 8px 12px;
    border-top: 1px solid var(--border);
}

.issues-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
}

.issues-title {
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text);
}

.issue-state-toggles {
    display: flex;
    gap: 4px;
}

.issue-state-toggle {
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 10px;
    cursor: pointer;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    transition: all 0.15s;
}

.issue-state-toggle.active-open {
    background: #238636;
    border-color: #238636;
    color: #fff;
}

.issue-state-toggle.active-closed {
    background: #8957e5;
    border-color: #8957e5;
    color: #fff;
}

.issues-search {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 5px 8px;
    color: var(--text);
    font-size: 11px;
    margin-bottom: 8px;
}

.issues-search::placeholder {
    color: var(--text-muted);
}

.issue-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.issue-row {
    padding: 6px 8px;
    border-radius: 4px;
    cursor: pointer;
    display: flex;
    gap: 6px;
    align-items: flex-start;
    transition: background 0.1s;
}

.issue-row:hover {
    background: var(--bg-secondary);
}

.issue-dot {
    font-size: 14px;
    line-height: 1;
    flex-shrink: 0;
    margin-top: 1px;
}

.issue-dot.open { color: #238636; }
.issue-dot.closed { color: #8957e5; }

.issue-row-title {
    font-size: 11px;
    line-height: 1.3;
    color: var(--text);
}

.issue-row-meta {
    font-size: 10px;
    color: var(--text-muted);
    margin-top: 2px;
}

.issues-show-more {
    font-size: 10px;
    color: var(--text-muted);
    text-align: center;
    margin-top: 8px;
    cursor: pointer;
}

.issues-show-more:hover {
    color: var(--accent);
}

.issues-loading, .issues-error, .issues-empty {
    font-size: 11px;
    color: var(--text-muted);
    text-align: center;
    padding: 12px 0;
}

.issues-error {
    color: var(--danger, #c05050);
}

/* Issue Detail Panel */
.issue-detail {
    padding: 4px 0;
}

.issue-detail-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 8px;
}

.issue-detail-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text);
    line-height: 1.3;
}

.issue-detail-meta {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
}

.issue-detail-close {
    color: var(--text-muted);
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 0 4px;
}

.issue-detail-close:hover {
    color: var(--text);
}

.issue-labels {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 10px;
}

.issue-label {
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 10px;
    font-weight: 500;
}

.issue-body {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text);
    max-height: 200px;
    overflow-y: auto;
    margin-bottom: 12px;
}

.issue-body p { margin: 0 0 8px 0; }
.issue-body p:last-child { margin-bottom: 0; }
.issue-body code { background: var(--bg); padding: 1px 4px; border-radius: 3px; font-size: 11px; }
.issue-body pre { background: var(--bg); padding: 8px; border-radius: 4px; overflow-x: auto; }

.generate-prompt-btn {
    width: 100%;
    background: #238636;
    color: #fff;
    border: none;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
}

.generate-prompt-btn:hover {
    background: #2ea043;
}

.generate-prompt-hint {
    font-size: 10px;
    color: var(--text-muted);
    text-align: center;
    margin-top: 4px;
}
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat: add CSS styles for GitHub issues section"
```

---

### Task 4: Frontend — Alpine.js State and Methods

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add state properties**

Add these properties to the Alpine.js `data()` return object (around where `gitInfo: null` is defined, near line 48):

```javascript
githubIssues: [],
githubIssuesState: 'open',
githubIssuesSearch: '',
githubIssuesLimit: 10,
githubIssuesHasMore: false,
githubIssuesLoading: false,
githubIssuesError: null,
selectedIssue: null,
selectedIssueLoading: false,
```

- [ ] **Step 2: Add fetch methods**

Add these methods to the Alpine.js component (near the `loadSessionFiles` method, around line 794):

```javascript
async fetchGithubIssues(sessionId) {
  if (!sessionId) return;
  this.githubIssuesLoading = true;
  this.githubIssuesError = null;
  try {
    const params = new URLSearchParams({
      state: this.githubIssuesState,
      limit: this.githubIssuesLimit.toString(),
    });
    if (this.githubIssuesSearch) {
      params.set('search', this.githubIssuesSearch);
    }
    const res = await fetch(`/api/sessions/${sessionId}/github/issues?${params}`, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      this.githubIssuesError = data.error || 'Failed to load issues';
      return;
    }
    const data = await res.json();
    this.githubIssues = data.issues || [];
    this.githubIssuesHasMore = data.has_more || false;
  } catch (e) {
    this.githubIssuesError = 'Failed to load issues';
  } finally {
    this.githubIssuesLoading = false;
  }
},

async fetchIssueDetail(sessionId, number) {
  this.selectedIssueLoading = true;
  try {
    const res = await fetch(`/api/sessions/${sessionId}/github/issues/${number}`, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!res.ok) return;
    this.selectedIssue = await res.json();
  } catch (e) {
    // ignore
  } finally {
    this.selectedIssueLoading = false;
  }
},

toggleIssueState(state) {
  this.githubIssuesState = state;
  this.githubIssuesLimit = 10;
  this.selectedIssue = null;
  this.fetchGithubIssues(this.selectedSessionId);
},

searchIssuesDebounced: null,

searchIssues() {
  clearTimeout(this.searchIssuesDebounced);
  this.searchIssuesDebounced = setTimeout(() => {
    this.githubIssuesLimit = 10;
    this.selectedIssue = null;
    this.fetchGithubIssues(this.selectedSessionId);
  }, 300);
},

loadMoreIssues() {
  this.githubIssuesLimit += 10;
  this.fetchGithubIssues(this.selectedSessionId);
},

generateIssuePrompt(issue) {
  const prompt = `Work on GitHub issue #${issue.number}: "${issue.title}"

Requirements:
${issue.body || '(No description provided)'}

Create a feature branch, implement the solution, and open a draft PR linking to issue #${issue.number}.`;
  this.inputText = prompt;
  // Trigger textarea resize
  this.$nextTick(() => {
    const ta = this.$el.querySelector('.instruction-bar textarea');
    if (ta) {
      ta.style.height = 'auto';
      ta.style.height = Math.min(ta.scrollHeight, 150) + 'px';
    }
  });
},

issueTimeAgo(dateStr) {
  const now = new Date();
  const date = new Date(dateStr);
  const diffMs = now - date;
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return diffMins + 'm ago';
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return diffHours + 'h ago';
  const diffDays = Math.floor(diffHours / 24);
  return diffDays + 'd ago';
},

issueLabelStyle(label) {
  if (!label.color) return '';
  const r = parseInt(label.color.substring(0, 2), 16);
  const g = parseInt(label.color.substring(2, 4), 16);
  const b = parseInt(label.color.substring(4, 6), 16);
  // Use label color at low opacity for background, full for text
  return `background:rgba(${r},${g},${b},0.2);color:#${label.color};border:1px solid rgba(${r},${g},${b},0.4)`;
},
```

- [ ] **Step 3: Trigger issue loading on session select**

In the `selectSession` method (around line 366), add after `this.loadSessionFiles(this.selectedSessionId);`:

```javascript
// Load GitHub issues for managed sessions
if (sess && sess.mode === 'managed') {
  this.githubIssues = [];
  this.githubIssuesState = 'open';
  this.githubIssuesSearch = '';
  this.githubIssuesLimit = 10;
  this.selectedIssue = null;
  this.githubIssuesError = null;
  this.fetchGithubIssues(this.selectedSessionId);
}
```

- [ ] **Step 4: Verify the app loads without errors**

Run: `cd server && go build -o claude-controller . && ./claude-controller`
Expected: Server starts, web UI loads without JS console errors

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add Alpine.js state and methods for GitHub issues"
```

---

### Task 5: Frontend — HTML Issues Section

**Files:**
- Modify: `server/web/static/index.html` (insert after line 248, before the closing `</div>` of the sidebar)

- [ ] **Step 1: Add the issues section HTML**

Insert after line 248 (after the `git-status-bar` div closing tag) and before line 249 (`</div>` sidebar close):

```html
        <!-- GitHub Issues Section -->
        <div class="issues-section" x-show="gitInfo && currentSession?.mode === 'managed'">
          <template x-if="!selectedIssue">
            <div>
              <div class="issues-header">
                <span class="issues-title">Issues</span>
                <div class="issue-state-toggles">
                  <span class="issue-state-toggle" :class="githubIssuesState === 'open' ? 'active-open' : ''"
                        @click="toggleIssueState('open')">Open</span>
                  <span class="issue-state-toggle" :class="githubIssuesState === 'closed' ? 'active-closed' : ''"
                        @click="toggleIssueState('closed')">Closed</span>
                </div>
              </div>
              <input class="issues-search" type="text" placeholder="Search issues..."
                     x-model="githubIssuesSearch" @input="searchIssues()">
              <div x-show="githubIssuesLoading" class="issues-loading">Loading issues...</div>
              <div x-show="githubIssuesError && !githubIssuesLoading" class="issues-error">
                <span x-text="githubIssuesError"></span>
                <span style="cursor:pointer;text-decoration:underline;margin-left:4px"
                      @click="fetchGithubIssues(selectedSessionId)">Retry</span>
              </div>
              <div x-show="!githubIssuesLoading && !githubIssuesError && githubIssues.length === 0" class="issues-empty">
                No issues found
              </div>
              <div class="issue-list" x-show="!githubIssuesLoading && !githubIssuesError">
                <template x-for="issue in githubIssues" :key="issue.number">
                  <div class="issue-row" @click="fetchIssueDetail(selectedSessionId, issue.number)">
                    <span class="issue-dot" :class="issue.state === 'OPEN' ? 'open' : 'closed'"
                          x-text="issue.state === 'OPEN' ? '●' : '●'"></span>
                    <div>
                      <div class="issue-row-title" x-text="issue.title"></div>
                      <div class="issue-row-meta">
                        <span x-text="'#' + issue.number"></span> ·
                        <span x-text="issueTimeAgo(issue.created_at)"></span>
                      </div>
                    </div>
                  </div>
                </template>
              </div>
              <div class="issues-show-more" x-show="githubIssuesHasMore && !githubIssuesLoading"
                   @click="loadMoreIssues()">Show more ↓</div>
            </div>
          </template>
          <template x-if="selectedIssue">
            <div class="issue-detail">
              <div class="issue-detail-header">
                <div>
                  <div class="issue-detail-title" x-text="selectedIssue.title"></div>
                  <div class="issue-detail-meta">
                    <span x-text="'#' + selectedIssue.number"></span> ·
                    opened by <span x-text="selectedIssue.author"></span> ·
                    <span x-text="issueTimeAgo(selectedIssue.created_at)"></span>
                  </div>
                </div>
                <span class="issue-detail-close" @click="selectedIssue = null">✕</span>
              </div>
              <div class="issue-labels" x-show="selectedIssue.labels && selectedIssue.labels.length > 0">
                <template x-for="label in selectedIssue.labels" :key="label.name">
                  <span class="issue-label" :style="issueLabelStyle(label)" x-text="label.name"></span>
                </template>
              </div>
              <div class="issue-body" x-html="marked.parse(selectedIssue.body || '*No description*')"></div>
              <button class="generate-prompt-btn" @click="generateIssuePrompt(selectedIssue)">
                Generate Prompt →
              </button>
              <div class="generate-prompt-hint">Pastes into the message input below</div>
            </div>
          </template>
          <div x-show="selectedIssueLoading" class="issues-loading">Loading issue...</div>
        </div>
```

- [ ] **Step 2: Build and verify**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles successfully (Go embeds static files)

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat: add GitHub issues section HTML to right sidebar"
```

---

### Task 6: Integration Test and Polish

**Files:**
- All files from previous tasks

- [ ] **Step 1: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 2: Build the server**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles successfully

- [ ] **Step 3: Manual smoke test**

Start the server, open the web UI, select a managed session that points to a repo with GitHub issues.

Verify:
1. Issues section appears below the git branch info
2. Open issues listed by default
3. Clicking "Closed" toggle switches to closed issues
4. Search box filters issues
5. Clicking an issue shows the detail view with title, labels, body
6. Clicking "Generate Prompt" populates the chat textarea
7. Clicking ✕ returns to the issue list
8. "Show more" loads additional issues (if the repo has >10)
9. Hook-mode sessions do NOT show the issues section

- [ ] **Step 4: Final commit if any polish needed**

```bash
git add -A
git commit -m "feat: issue picker polish and fixes"
```
