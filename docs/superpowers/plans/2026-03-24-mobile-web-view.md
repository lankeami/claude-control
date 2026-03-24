# Mobile Web View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the web UI fully functional on mobile screens (<=768px) with a hamburger menu, bottom tab navigation, and full-screen overlays for detail views.

**Architecture:** CSS-first responsive approach with three new Alpine.js state variables (`mobileMenuOpen`, `mobileTab`, `mobileOverlay`). All changes confined to existing `index.html`, `app.js`, and `style.css`. Chat remains the home view; a full-screen hamburger overlay provides session switching and access to Files, Issues, and Tasks via bottom tabs.

**Tech Stack:** Alpine.js, vanilla CSS (media queries), inline SVG icons.

**Spec:** `docs/superpowers/specs/2026-03-24-mobile-web-view-design.md`

---

### Task 0: Create Feature Branch

- [ ] **Step 1: Create feature branch from main**

```bash
git checkout -b feat/mobile-web-view
```

- [ ] **Step 2: Verify branch**

```bash
git branch --show-current
```

Expected: `feat/mobile-web-view`

---

### Task 1: Add Mobile State Variables to Alpine.js

**Files:**
- Modify: `server/web/static/app.js:116-124` (after sidebar state block)

- [ ] **Step 1: Add mobile state variables**

Add after line 124 (`_resizeStartWidth: 0,`):

```javascript
    // Mobile menu state
    mobileMenuOpen: false,
    mobileTab: 'sessions',
    mobileOverlay: null, // null | 'file' | 'issue'
```

- [ ] **Step 2: Add isMobile helper method**

Add after the state variables, before `startResize`:

```javascript
    isMobile() {
      return window.innerWidth <= 768;
    },
```

- [ ] **Step 3: Add body scroll lock effect**

In the `init()` method (find `Alpine.effect` calls or add at end of `init`), add:

```javascript
      this.$watch('mobileMenuOpen', (open) => {
        document.body.style.overflow = open ? 'hidden' : '';
      });
```

- [ ] **Step 4: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(mobile): add mobile menu state variables and helpers"
```

---

### Task 2: Update selectSession, openFileViewer, fetchIssueDetail for Mobile

**Files:**
- Modify: `server/web/static/app.js:547` (selectSession method)
- Modify: `server/web/static/app.js:1416` (openFileViewer method)
- Modify: `server/web/static/app.js:1245` (fetchIssueDetail method)

- [ ] **Step 1: Update selectSession to close mobile menu**

At `server/web/static/app.js:547`, inside `selectSession`, add these lines **before** the early return guard (`if (this.selectedSessionId === id) return;` at line ~549) so the menu always closes, even when tapping the already-selected session:

```javascript
      this.mobileMenuOpen = false;
      this.mobileOverlay = null;
```

- [ ] **Step 2: Update openFileViewer to set mobile overlay**

At `server/web/static/app.js:1416`, inside `openFileViewer`, after setting `this.viewerFile = filePath;` (line ~1419), add:

```javascript
      if (this.isMobile()) {
        this.mobileOverlay = 'file';
      }
```

- [ ] **Step 3: Update fetchIssueDetail to set mobile overlay**

At `server/web/static/app.js:1245`, inside `fetchIssueDetail`, after `this.selectedIssueLoading = true;` (line ~1247), add:

```javascript
      if (this.isMobile()) {
        this.mobileOverlay = 'issue';
      }
```

- [ ] **Step 4: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(mobile): wire mobile overlay state into session/file/issue methods"
```

---

### Task 3: Add Hamburger Button and Mobile Menu HTML

**Files:**
- Modify: `server/web/static/index.html:116-135` (main header area + mobile session selector)

- [ ] **Step 1: Replace mobile session selector with hamburger button**

Replace the entire mobile session selector block (lines 127-135):

```html
        <!-- Mobile session selector -->
        <div class="mobile-session-select">
          <select x-model="selectedSessionId"
                  @change="selectedSessionId = $event.target.value || null">
            <template x-for="session in sessions" :key="session.id">
              <option :value="session.id" x-text="sessionName(session)"></option>
            </template>
          </select>
        </div>
```

With a hamburger button in the main header. Update the `.main-header` div (lines 116-125) to:

```html
        <div class="main-header">
          <button class="mobile-hamburger" @click="mobileMenuOpen = true"
                  aria-label="Open menu" :aria-expanded="mobileMenuOpen">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
              <rect x="2" y="4" width="16" height="2" rx="1"/>
              <rect x="2" y="9" width="16" height="2" rx="1"/>
              <rect x="2" y="14" width="16" height="2" rx="1"/>
            </svg>
          </button>
          <h2 x-text="selectedSessionId
            ? sessionName(sessions.find(s => s.id === selectedSessionId) || {computer_name:'',project_path:''})
            : 'Select a Session'"></h2>
          <button class="btn btn-sm" x-show="currentSession?.mode === 'managed' && currentSession?.status === 'running'"
                  @click="interruptSession()"
                  style="background:#e74c3c; color:white; margin-left:8px;">
            Stop
          </button>
        </div>
```

- [ ] **Step 2: Add mobile menu overlay HTML**

Add after the closing `</template>` of the dashboard (after line 410, before the New Session Modal), the full mobile menu overlay:

```html
  <!-- Mobile Menu Overlay -->
  <div class="mobile-menu-overlay" x-show="mobileMenuOpen" x-cloak
       :style="mobileMenuOpen ? 'transform:translateX(0)' : 'transform:translateX(-100%)'">
    <!-- Menu Header -->
    <div class="mobile-menu-header">
      <span style="font-weight:600;">Claude Controller</span>
      <button class="mobile-menu-close" @click="mobileMenuOpen = false; mobileOverlay = null" aria-label="Close menu">
        <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
          <path d="M5.3 5.3a1 1 0 011.4 0L10 8.6l3.3-3.3a1 1 0 111.4 1.4L11.4 10l3.3 3.3a1 1 0 01-1.4 1.4L10 11.4l-3.3 3.3a1 1 0 01-1.4-1.4L8.6 10 5.3 6.7a1 1 0 010-1.4z"/>
        </svg>
      </button>
    </div>

    <!-- Tab Content -->
    <div class="mobile-menu-content">
      <!-- Sessions Tab -->
      <div x-show="mobileTab === 'sessions'" style="flex:1;overflow-y:auto;padding:8px;">
        <div style="display:flex;gap:8px;margin-bottom:8px;">
          <button class="btn btn-sm btn-primary" @click="mobileMenuOpen=false;openNewSessionModal()" style="flex:1;font-size:13px;">+ New Session</button>
          <button class="btn btn-sm" @click="mobileMenuOpen=false;openResumePicker()" style="flex:1;font-size:13px;">Resume</button>
        </div>
        <template x-for="session in sessions" :key="session.id">
          <div class="session-item" :class="{ active: selectedSessionId === session.id }"
               @click="selectSession(session.id)">
            <span class="status-dot" :class="sessionStatus(session)"></span>
            <span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
                  x-text="sessionName(session)"></span>
            <span x-show="session.mode === 'managed'"
                  style="font-size:10px;background:var(--accent);color:white;padding:1px 4px;border-radius:3px;margin-left:4px;flex-shrink:0;">managed</span>
            <span class="badge" x-show="pendingCountFor(session.id) > 0"
                  x-text="pendingCountFor(session.id)"></span>
            <button class="session-delete" @click.stop="deleteSession(session.id)"
                    title="Remove session">&times;</button>
          </div>
        </template>
      </div>

      <!-- Files Tab -->
      <div x-show="mobileTab === 'files'" style="flex:1;overflow-y:auto;padding:8px;">
        <template x-if="!selectedSessionId">
          <div style="display:flex;align-items:center;justify-content:center;height:100%;color:var(--text-muted);font-size:14px;">
            Select a session to view files
          </div>
        </template>
        <template x-if="selectedSessionId">
          <div>
            <div style="display:flex;align-items:center;gap:6px;margin-bottom:8px;">
              <span style="font-weight:600;font-size:14px;">Files</span>
              <span class="file-count-badge" x-show="sessionFiles.length > 0" x-text="sessionFiles.length"></span>
              <button class="file-tree-refresh" @click="loadSessionFiles(selectedSessionId)" title="Refresh">↻</button>
            </div>
            <template x-if="visibleFileNodes.length === 0">
              <div style="color:var(--text-muted);font-size:13px;padding:8px;">No files found</div>
            </template>
            <template x-for="node in visibleFileNodes" :key="node.path + (node.action || '')">
              <div class="file-tree-item"
                   :class="{ 'is-dir': node.isDir, 'is-selected': viewerFile === node.path }"
                   :style="'padding-left: ' + (node.depth * 16 + 8) + 'px'"
                   @click="node.isDir ? toggleDir(node) : openFileViewer(node.path)">
                <span class="file-tree-icon" x-text="node.isDir ? (node.open ? '▾' : '▸') : ''"></span>
                <span class="file-tree-name" :class="{ 'name-modified': !node.isDir && node.gitStatus }" x-text="node.name"></span>
                <span class="file-tree-status" :class="'git-' + (node.gitStatus === '?' ? 'untracked' : (node.gitStatus || ''))"
                      x-show="!node.isDir && node.gitStatus" x-text="node.gitStatus"></span>
                <span class="file-tree-action" :class="'action-' + (node.action || '')"
                      :style="!node.gitStatus ? 'margin-left:auto' : ''"
                      x-show="!node.isDir && node.action"
                      x-text="node.action === 'edit' ? 'E' : node.action === 'write' ? 'W' : 'R'"></span>
              </div>
            </template>
            <!-- Git status -->
            <div class="git-status-bar" x-show="gitInfo" style="margin-top:8px;border-top:1px solid var(--border);padding-top:8px;">
              <div class="git-branch-row">
                <span class="git-branch-icon">⎇</span>
                <span class="git-branch-name" x-text="gitInfo?.branch || 'detached'"></span>
                <span class="git-ahead" x-show="gitInfo?.ahead > 0" x-text="'↑' + gitInfo?.ahead"></span>
                <span class="git-behind" x-show="gitInfo?.behind > 0" x-text="'↓' + gitInfo?.behind"></span>
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- Issues Tab -->
      <div x-show="mobileTab === 'issues'" style="flex:1;overflow-y:auto;padding:8px;">
        <template x-if="!selectedSessionId">
          <div style="display:flex;align-items:center;justify-content:center;height:100%;color:var(--text-muted);font-size:14px;">
            Select a session to view issues
          </div>
        </template>
        <template x-if="selectedSessionId">
          <div>
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px;">
              <span style="font-weight:600;font-size:14px;">Issues</span>
              <div class="issue-state-toggles" @click.stop>
                <button class="issue-state-toggle" :class="{ 'active-open': githubIssuesState === 'open' }"
                        @click="toggleIssueState('open')">Open</button>
                <button class="issue-state-toggle" :class="{ 'active-closed': githubIssuesState === 'closed' }"
                        @click="toggleIssueState('closed')">Closed</button>
              </div>
            </div>
            <input type="text" class="issues-search" placeholder="Search issues..."
                   x-model="githubIssuesSearch" @input="searchIssues()">
            <div x-show="githubIssuesLoading" class="issues-loading">Loading...</div>
            <div x-show="!githubIssuesLoading && githubIssuesError" class="issues-error" x-text="githubIssuesError"></div>
            <div x-show="!githubIssuesLoading && !githubIssuesError && githubIssues.length === 0" class="issues-empty">No issues found</div>
            <div class="issue-list" x-show="!githubIssuesLoading && !githubIssuesError && githubIssues.length > 0">
              <template x-for="issue in githubIssues" :key="issue.number">
                <div class="issue-row" @click="fetchIssueDetail(selectedSessionId, issue.number)">
                  <div class="issue-dot" :class="issue.state"></div>
                  <div class="issue-row-content">
                    <div class="issue-row-title" x-text="issue.title"></div>
                    <div class="issue-row-meta" x-text="'#' + issue.number + ' · ' + issueTimeAgo(issue.updated_at)"></div>
                  </div>
                </div>
              </template>
            </div>
            <button x-show="githubIssuesHasMore && !githubIssuesLoading"
                    class="issues-show-more" @click="loadMoreIssues()">Show more</button>
          </div>
        </template>
      </div>

      <!-- Tasks Tab -->
      <div x-show="mobileTab === 'tasks'" style="flex:1;overflow-y:auto;padding:8px;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">
          <span style="font-weight:600;font-size:14px;">Scheduled Tasks</span>
          <button class="btn btn-sm" @click="mobileMenuOpen=false;openTaskModal()" style="font-size:12px;">+ New Task</button>
        </div>
        <template x-if="scheduledTasks.length === 0">
          <div style="color:var(--text-muted);font-size:13px;padding:8px;">No scheduled tasks</div>
        </template>
        <template x-for="task in scheduledTasks" :key="task.id">
          <div class="session-item" @click="mobileMenuOpen=false;openTaskModal(task)"
               :style="!task.enabled && 'opacity:0.5'">
            <span class="status-dot" :class="task.enabled ? 'waiting' : 'idle'"></span>
            <span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
                  x-text="task.name"></span>
            <span style="font-size:10px;color:var(--text-muted);flex-shrink:0;"
                  x-text="formatCron(task.cron_expression)"></span>
          </div>
        </template>
      </div>
    </div>

    <!-- Mobile File Viewer Overlay -->
    <div class="mobile-detail-overlay" x-show="mobileOverlay === 'file'" x-cloak
         :style="mobileOverlay === 'file' ? 'transform:translateX(0)' : 'transform:translateX(100%)'">
      <div class="mobile-detail-header">
        <button @click="mobileOverlay = null; closeFileViewer()" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title" x-text="viewerFile ? viewerFile.split('/').pop() : ''"></span>
        <span class="file-type-badge" x-text="viewerFileType" x-show="viewerFileType"></span>
        <div style="margin-left:auto;display:flex;gap:4px;">
          <button class="btn btn-sm" :class="{ active: viewerMode === 'diff' }" @click="switchToDiffView()">Diff</button>
          <button class="btn btn-sm" :class="{ active: viewerMode === 'full' }" @click="switchToFullView()">Full</button>
        </div>
      </div>
      <div class="mobile-detail-body">
        <div x-show="viewerMode === 'diff'" class="diff-view">
          <div x-show="viewerLoading" class="viewer-loading">Loading diff...</div>
          <div x-show="!viewerLoading && !viewerDiffHtml" class="diff-empty">No changes detected.</div>
          <div x-show="!viewerLoading && viewerDiffHtml" x-html="viewerDiffHtml"></div>
        </div>
        <div x-show="viewerMode === 'full'" class="full-view">
          <div x-show="viewerLoading" class="viewer-loading">Loading...</div>
          <div x-show="viewerBinary" class="viewer-binary">Binary file — cannot display.</div>
          <div x-show="!viewerLoading && !viewerBinary" class="full-file-content" x-html="viewerFullHtml"></div>
          <div x-show="viewerTruncated" class="viewer-truncated">File truncated (exceeds 1MB).</div>
        </div>
      </div>
    </div>

    <!-- Mobile Issue Viewer Overlay -->
    <div class="mobile-detail-overlay" x-show="mobileOverlay === 'issue'" x-cloak
         :style="mobileOverlay === 'issue' ? 'transform:translateX(0)' : 'transform:translateX(100%)'">
      <div class="mobile-detail-header">
        <button @click="mobileOverlay = null; closeIssueViewer()" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title" x-text="selectedIssue ? 'Issue #' + selectedIssue.number : ''"></span>
        <span class="file-type-badge" x-show="selectedIssue"
              x-text="selectedIssue?.state"
              :style="selectedIssue?.state === 'OPEN' ? 'background:#238636;color:#fff' : 'background:#8957e5;color:#fff'"></span>
        <div style="margin-left:auto;display:flex;gap:4px;">
          <a class="btn btn-sm" x-show="selectedIssue" :href="'https://github.com/' + githubRepo + '/issues/' + selectedIssue?.number" target="_blank" rel="noopener">↗ GitHub</a>
          <button class="btn btn-sm" x-show="selectedIssue" @click="generateIssuePrompt(selectedIssue); mobileMenuOpen=false; mobileOverlay=null;">Prompt</button>
        </div>
      </div>
      <div class="mobile-detail-body" style="padding:16px;">
        <div x-show="selectedIssueLoading" class="viewer-loading">Loading issue...</div>
        <div x-show="!selectedIssueLoading && selectedIssue">
          <h3 style="margin:0 0 8px;font-size:16px;" x-text="selectedIssue?.title"></h3>
          <div style="font-size:11px;color:var(--text-muted);margin-bottom:12px">
            opened by <span x-text="selectedIssue?.author"></span> ·
            <span x-text="issueTimeAgo(selectedIssue?.created_at || selectedIssue?.updated_at)"></span>
          </div>
          <div class="issue-labels" x-show="selectedIssue?.labels?.length > 0" style="margin-bottom:12px">
            <template x-for="label in (selectedIssue?.labels || [])" :key="label.name">
              <span class="issue-label" :style="issueLabelStyle(label)" x-text="label.name"></span>
            </template>
          </div>
          <div class="issue-body" style="max-height:none;background:transparent;border:none;padding:0"
               x-html="typeof marked !== 'undefined' ? marked.parse(selectedIssue?.body || '*No description*') : (selectedIssue?.body || '(No description)')"></div>
        </div>
      </div>
    </div>

    <!-- Bottom Tab Bar -->
    <div class="mobile-tab-bar" role="tablist">
      <button class="mobile-tab" :class="{ active: mobileTab === 'sessions' }"
              @click="mobileTab = 'sessions'" role="tab" :aria-selected="mobileTab === 'sessions'">
        <svg width="18" height="18" viewBox="0 0 20 20" fill="currentColor">
          <rect x="3" y="4" width="14" height="2" rx="1"/>
          <rect x="3" y="9" width="14" height="2" rx="1"/>
          <rect x="3" y="14" width="14" height="2" rx="1"/>
        </svg>
        <span>Sessions</span>
      </button>
      <button class="mobile-tab" :class="{ active: mobileTab === 'files' }"
              @click="mobileTab = 'files'" role="tab" :aria-selected="mobileTab === 'files'">
        <svg width="18" height="18" viewBox="0 0 20 20" fill="currentColor">
          <path d="M2 4a2 2 0 012-2h4l2 2h6a2 2 0 012 2v8a2 2 0 01-2 2H4a2 2 0 01-2-2V4z"/>
        </svg>
        <span>Files</span>
      </button>
      <button class="mobile-tab" :class="{ active: mobileTab === 'issues' }"
              @click="mobileTab = 'issues'" role="tab" :aria-selected="mobileTab === 'issues'">
        <svg width="18" height="18" viewBox="0 0 20 20" fill="currentColor">
          <circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" stroke-width="2"/>
          <circle cx="10" cy="10" r="3"/>
        </svg>
        <span>Issues</span>
      </button>
      <button class="mobile-tab" :class="{ active: mobileTab === 'tasks' }"
              @click="mobileTab = 'tasks'" role="tab" :aria-selected="mobileTab === 'tasks'">
        <svg width="18" height="18" viewBox="0 0 20 20" fill="currentColor">
          <circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" stroke-width="2"/>
          <path d="M10 6v4l3 2" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/>
        </svg>
        <span>Tasks</span>
      </button>
    </div>
  </div>
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(mobile): add hamburger menu overlay with tabs and detail views"
```

---

### Task 4: Add Mobile CSS Styles

**Files:**
- Modify: `server/web/static/style.css` (replace existing `@media (max-width: 768px)` block at line 1033, add base styles for mobile overlay)

- [ ] **Step 1: Add base styles for mobile overlay (hidden on desktop)**

Add before the existing `@media (max-width: 768px)` block (before line 1033):

```css
/* ===== Mobile Menu (hidden on desktop) ===== */
.mobile-hamburger { display: none; }
.mobile-menu-overlay { display: none; }
```

- [ ] **Step 2: Replace existing mobile media query**

Replace the existing media query block (lines 1033-1040):

```css
@media (max-width: 768px) {
  .sidebar { display: none; }
  .mobile-session-select { display: block; }
  .dashboard { flex-direction: column; }
  .file-tree-sidebar { display: none; }
  .file-viewer-column { display: none; }
  .resize-handle { display: none; }
}
```

With the full mobile styles:

```css
@media (max-width: 768px) {
  /* Hide desktop elements */
  .sidebar { display: none; }
  .mobile-session-select { display: none; }
  .file-tree-sidebar { display: none; }
  .file-viewer-column { display: none; }
  .resize-handle { display: none; }

  .dashboard { flex-direction: column; }

  /* Hamburger button */
  .mobile-hamburger {
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: var(--text);
    cursor: pointer;
    padding: 4px;
    margin-right: 8px;
    flex-shrink: 0;
  }

  /* Main header mobile layout */
  .main-header {
    display: flex;
    align-items: center;
    padding: 8px 12px;
  }
  .main-header h2 {
    font-size: 0.9rem;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Chat area mobile sizing */
  .chat-bubble.user,
  .chat-bubble.assistant,
  .chat-bubble.tool {
    max-width: 90%;
  }

  /* Input bar sticky */
  .instruction-bar {
    position: sticky;
    bottom: 0;
  }

  /* Mobile Menu Overlay */
  .mobile-menu-overlay {
    display: flex;
    flex-direction: column;
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    height: 100vh; /* fallback */
    height: 100dvh;
    background: var(--bg);
    z-index: 1000;
    transition: transform 250ms ease-out;
  }

  .mobile-menu-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .mobile-menu-close {
    background: transparent;
    border: none;
    color: var(--text);
    cursor: pointer;
    padding: 4px;
  }

  .mobile-menu-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-height: 0;
  }

  /* Bottom Tab Bar */
  .mobile-tab-bar {
    display: flex;
    border-top: 1px solid var(--border);
    background: var(--bg-secondary);
    flex-shrink: 0;
    padding-bottom: env(safe-area-inset-bottom, 0);
  }

  .mobile-tab {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    padding: 8px 4px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 10px;
    min-height: 44px;
    transition: color 0.15s;
  }

  .mobile-tab.active {
    color: var(--accent);
  }

  /* Session items in mobile menu — larger tap targets */
  .mobile-menu-content .session-item {
    min-height: 44px;
    padding: 10px 12px;
  }

  /* Detail Overlays (file viewer, issue detail) */
  .mobile-detail-overlay {
    display: flex;
    flex-direction: column;
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--bg);
    z-index: 1001;
    transition: transform 250ms ease-out;
  }

  .mobile-detail-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    min-height: 44px;
  }

  .mobile-back-btn {
    background: transparent;
    border: none;
    color: var(--text);
    cursor: pointer;
    padding: 4px;
    flex-shrink: 0;
  }

  .mobile-detail-title {
    font-weight: 600;
    font-size: 14px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
  }

  .mobile-detail-body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  /* Full-screen modals on mobile */
  .mobile-menu-overlay ~ div[x-show="showNewSessionModal"] > div,
  .mobile-menu-overlay ~ div[x-show="showResumePicker"] > div,
  .mobile-menu-overlay ~ div[x-show="taskModalOpen"] > div {
    width: 100vw !important;
    height: 100vh !important;
    height: 100dvh !important;
    max-width: none !important;
    max-height: none !important;
    border-radius: 0 !important;
  }

  /* Larger tap targets in modals */
  .browse-item {
    min-height: 44px;
    padding: 10px 12px;
  }
}
```

- [ ] **Step 3: Verify the CSS compiles (no syntax errors) by loading the page**

Run: `cd server && go run . &` — open http://localhost:8080 on a mobile viewport (or Chrome DevTools responsive mode at 375px width).

Expected: Page loads without CSS errors. Hamburger button visible. Chat area fills screen.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat(mobile): add responsive CSS for hamburger menu, tabs, overlays, and modals"
```

---

### Task 5: Manual Testing and Polish

**Files:**
- Modify: `server/web/static/style.css` (polish adjustments)
- Modify: `server/web/static/index.html` (fix any markup issues)
- Modify: `server/web/static/app.js` (fix any state issues)

- [ ] **Step 1: Test hamburger menu flow**

Open the app in Chrome DevTools responsive mode (375x812, iPhone viewport). Verify:
1. Hamburger button is visible in header
2. Tapping hamburger opens full-screen menu with sessions tab
3. Session list is visible with status dots and badges
4. Tapping a session closes the menu and loads the chat
5. X button closes the menu

- [ ] **Step 2: Test bottom tab navigation**

In the mobile menu, verify:
1. All 4 tabs are visible at the bottom
2. Tapping each tab switches the content above
3. Files tab shows file tree when a session is selected
4. Issues tab shows issues list when session has a GitHub repo
5. Tasks tab shows scheduled tasks list

- [ ] **Step 3: Test detail overlays**

1. Open Files tab, tap a file → file viewer overlay slides in from right
2. Back button returns to Files tab in the menu
3. Open Issues tab, tap an issue → issue detail overlay slides in
4. Back button returns to Issues tab
5. "Generate Prompt" button closes menu and injects prompt into chat input

- [ ] **Step 4: Test modals**

1. Tap "+ New Session" → New Session modal opens full-screen
2. Directory browser works, breadcrumbs work, folder selection works
3. Cancel returns to chat view
4. Task modal opens full-screen when creating/editing task
5. Resume picker opens full-screen

- [ ] **Step 5: Test body scroll lock**

1. Open hamburger menu
2. Try scrolling → only menu content scrolls, not the chat behind
3. Close menu → chat scrolls normally again

- [ ] **Step 6: Fix any issues found and commit**

```bash
git add server/web/static/style.css server/web/static/index.html server/web/static/app.js
git commit -m "fix(mobile): polish mobile layout based on testing"
```

---

### Task 6: Push and Create Draft PR

- [ ] **Step 1: Push and create draft PR**

```bash
git push -u origin feat/mobile-web-view
gh pr create --draft --title "feat: mobile web view" --body "$(cat <<'EOF'
## Summary
- Full mobile web view support at <=768px breakpoint
- Hamburger menu with session list + bottom tabs (Files, Issues, Tasks)
- Full-screen overlays for file viewer and issue detail
- Full-screen modals on mobile
- Body scroll lock, iOS Safari viewport fixes, accessibility attributes

Closes #12

## Test plan
- [ ] Chrome DevTools responsive mode at 375px (iPhone)
- [ ] Chrome DevTools responsive mode at 768px (iPad portrait)
- [ ] Hamburger menu opens/closes, sessions selectable
- [ ] All 4 tabs switch content correctly
- [ ] File viewer overlay opens from Files tab
- [ ] Issue detail overlay opens from Issues tab
- [ ] New Session modal is full-screen
- [ ] Task modal is full-screen
- [ ] Resume picker is full-screen
- [ ] Body scroll is locked when menu is open
- [ ] Desktop layout unchanged above 768px
EOF
)"
```
