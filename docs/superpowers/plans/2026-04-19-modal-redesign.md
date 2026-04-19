# Modal Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the modal system to be larger, centered, less cluttered, with unified CSS classes replacing inline styles.

**Architecture:** Add shared modal CSS classes (`.modal-backdrop`, `.modal-sm/md/lg`, `.modal-header/body/footer`) to `style.css`. Refactor each modal's HTML in `index.html` to use these classes instead of inline styles. Restructure the New Session modal into a two-column layout and the Settings modal into a tabbed sidebar layout. Add minimal Alpine.js state for new UI elements (`showNewFolderInput`, `settingsActiveTab`, `settingsOriginal` for change tracking).

**Tech Stack:** CSS, HTML, Alpine.js (existing stack — no new dependencies)

---

### File Map

- **Modify:** `server/web/static/style.css` — Add modal CSS classes (~100 lines), update mobile overrides
- **Modify:** `server/web/static/index.html` — Refactor all 5 modal HTML blocks (lines 975-1427)
- **Modify:** `server/web/static/app.js` — Add `settingsActiveTab`, `showNewFolderInput`, `settingsOriginal` state + change tracking logic

---

### Task 1: Add Unified Modal CSS Classes

**Files:**
- Modify: `server/web/static/style.css` (append before the mobile `@media` block around line 1195)

- [ ] **Step 1: Add the modal base classes to style.css**

Insert the following CSS before the existing `@media (max-width: 768px)` block (around line 1195 in style.css). Find the comment `/* ===== Mobile layout ===== */` or the first `@media (max-width: 768px)` and insert before it:

```css
/* ===== Modal System ===== */
.modal-backdrop {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.modal-sm,
.modal-md,
.modal-lg {
  background: var(--bg);
  border-radius: 16px;
  border: 1px solid var(--border);
  box-shadow: 0 24px 80px rgba(0, 0, 0, 0.4);
  display: flex;
  flex-direction: column;
  max-height: 85vh;
  max-width: 90vw;
}

.modal-sm { width: 480px; }
.modal-md { width: 560px; }
.modal-lg { width: 640px; }

.modal-header {
  padding: 20px 24px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.modal-header h3 {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 600;
}

.modal-close-btn {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  line-height: 1;
}

.modal-close-btn:hover {
  background: var(--border);
}

.modal-body {
  padding: 0 24px;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}

.modal-footer {
  padding: 16px 24px;
  border-top: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-shrink: 0;
}

/* Modal form input — shared style for all modal inputs */
.modal-input {
  width: 100%;
  padding: 10px 12px;
  background: var(--input-bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 13px;
  box-sizing: border-box;
}

.modal-input:focus {
  outline: none;
  border-color: var(--accent);
}

.modal-label {
  display: block;
  font-size: 12px;
  font-weight: 500;
  margin-bottom: 4px;
}

.modal-hint {
  font-size: 10px;
  color: var(--text-muted);
  margin-top: 3px;
}

/* ===== New Session Modal ===== */
.modal-columns {
  display: flex;
  min-height: 280px;
  border-top: 1px solid var(--border);
}

.modal-columns-left {
  width: 45%;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  padding: 16px;
  overflow-y: auto;
}

.modal-columns-right {
  flex: 1;
  padding: 16px;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
}

.modal-section-label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
  margin-bottom: 10px;
}

.new-folder-toggle {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--accent);
  cursor: pointer;
}

.new-folder-toggle:hover {
  text-decoration: underline;
}

/* ===== Settings Modal ===== */
.settings-layout {
  display: flex;
  flex: 1;
  min-height: 0;
  border-top: 1px solid var(--border);
}

.settings-tabs {
  width: 160px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  padding: 12px 0;
  background: var(--bg-secondary);
}

.settings-tab {
  padding: 8px 16px;
  font-size: 13px;
  color: var(--text-muted);
  cursor: pointer;
  border-left: 2px solid transparent;
}

.settings-tab:hover {
  color: var(--text);
  background: var(--bg);
}

.settings-tab.active {
  color: var(--accent);
  border-left-color: var(--accent);
  background: var(--bg);
  font-weight: 500;
}

.settings-tab-divider {
  border-top: 1px solid var(--border);
  margin: 8px 16px;
}

.settings-tab.danger {
  color: var(--red);
}

.settings-tab.danger:hover {
  background: var(--bg);
}

.settings-content {
  flex: 1;
  padding: 20px 24px;
  overflow-y: auto;
  min-height: 0;
}

.settings-section-header {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
  margin-bottom: 16px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.settings-field {
  margin-bottom: 16px;
}

.settings-change-badge {
  font-size: 9px;
  background: var(--yellow);
  color: #000;
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 600;
  margin-left: 6px;
}

.modal-footer-status {
  margin-right: auto;
  font-size: 12px;
  color: var(--yellow);
  font-weight: 500;
}
```

- [ ] **Step 2: Update the mobile `@media` block to add modal responsive overrides**

Find the existing mobile `@media (max-width: 768px)` block that contains the current modal overrides (around line 1400). Replace the old modal selectors (`.mobile-menu-overlay ~ div[x-show=...] > div`) with the new class-based ones. Add these rules inside the `@media (max-width: 768px)` block:

```css
  /* Modal full-screen on mobile */
  .modal-sm,
  .modal-md,
  .modal-lg {
    width: 100vw !important;
    height: 100vh !important;
    height: 100dvh !important;
    max-width: none !important;
    max-height: none !important;
    border-radius: 0 !important;
  }

  .modal-header {
    padding: 12px 16px;
  }

  .modal-body {
    padding: 0 16px;
  }

  .modal-footer {
    padding: 12px 16px;
  }

  /* New Session: stack columns vertically on mobile */
  .modal-columns {
    flex-direction: column;
  }

  .modal-columns-left {
    width: 100%;
    border-right: none;
    border-bottom: 1px solid var(--border);
    max-height: 200px;
  }

  .modal-columns-right {
    flex: 1;
  }

  /* Settings: horizontal tab bar on mobile */
  .settings-layout {
    flex-direction: column;
  }

  .settings-tabs {
    width: 100%;
    border-right: none;
    border-bottom: 1px solid var(--border);
    display: flex;
    overflow-x: auto;
    padding: 0;
    flex-shrink: 0;
  }

  .settings-tab {
    border-left: none;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
    padding: 10px 16px;
  }

  .settings-tab.active {
    border-left-color: transparent;
    border-bottom-color: var(--accent);
  }

  .settings-tab-divider {
    display: none;
  }
```

Also remove the old modal mobile overrides that reference `.mobile-menu-overlay ~ div[x-show="showNewSessionModal"] > div` etc. (lines ~1401-1410 in current style.css), since those are replaced by the class-based selectors.

- [ ] **Step 3: Verify CSS compiles and no syntax errors**

Run: Open `http://localhost:9999` in a browser and check DevTools console for CSS errors. Or just visually confirm the page still loads correctly.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat(ui): add unified modal CSS class system (#119)"
```

---

### Task 2: Refactor New Session Modal to Two-Column Layout

**Files:**
- Modify: `server/web/static/index.html:975-1073` (New Session Modal HTML)
- Modify: `server/web/static/app.js` (add `showNewFolderInput` state)

- [ ] **Step 1: Add `showNewFolderInput` state to app.js**

In `server/web/static/app.js`, find the line with `newProjectCreating: false,` (line 51) and add after it:

```javascript
    showNewFolderInput: false,
```

- [ ] **Step 2: Replace the New Session Modal HTML**

In `server/web/static/index.html`, replace the entire New Session Modal block (from `<!-- New Session Modal -->` at line 975 through the closing `</div>` before `<!-- Resume Picker Modal -->` at line 1074) with:

```html
  <!-- New Session Modal -->
  <div x-show="showNewSessionModal" x-cloak
       class="modal-backdrop" style="z-index:100;"
       @click.self="showNewSessionModal = false">
    <div class="modal-lg">
      <!-- Mobile header -->
      <div class="mobile-detail-header mobile-modal-header">
        <button @click="showNewSessionModal = false" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title">New Session</span>
      </div>

      <!-- Desktop header -->
      <div class="modal-header desktop-modal-title">
        <h3>New Session</h3>
        <button class="modal-close-btn" @click="showNewSessionModal = false" aria-label="Close">&times;</button>
      </div>

      <!-- Path input -->
      <div style="padding:0 24px 12px;">
        <div style="display:flex; gap:8px;">
          <input x-model="newSessionCWD" type="text" placeholder="/path/to/project"
                 class="modal-input" style="flex:1;"
                 @keydown="handleBrowseInputKeydown($event)"
                 @input="if (newSessionCWD === browsePath) { browseFilter = ''; } else if (browsePath && newSessionCWD.startsWith(browsePath + '/')) { browseFilter = newSessionCWD.slice(browsePath.length + 1); }">
          <button class="btn btn-sm btn-primary" @click="browseTo(newSessionCWD)" style="white-space:nowrap;">Go</button>
        </div>
        <div x-show="browseConfirmed" style="font-size:11px; color:var(--accent); margin-top:4px;">
          Press Enter again to start session in this folder
        </div>
        <div x-show="!browseConfirmed && browseFilter" style="font-size:11px; color:var(--text-muted); margin-top:4px;">
          Filtering: <span x-text="browseFilter"></span>
        </div>
      </div>

      <!-- Two-column body -->
      <div class="modal-columns">
        <!-- Left: Recent Projects -->
        <div class="modal-columns-left">
          <div class="modal-section-label">Recent Projects</div>
          <template x-if="recentDirs.length === 0">
            <div style="font-size:12px; color:var(--text-muted);">No recent projects</div>
          </template>
          <template x-for="dir in recentDirs" :key="dir.path">
            <div class="browse-item" @click="selectRecentDir(dir.path)" style="cursor:pointer; border-radius:8px; padding:10px 12px; margin-bottom:4px;">
              <span class="browse-icon" style="color:var(--accent);">&#9733;</span>
              <div style="flex:1; min-width:0;">
                <div style="font-weight:600; font-size:13px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="dir.name"></div>
                <div style="font-size:10px; color:var(--text-muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="abbreviatePath(dir.path)"></div>
              </div>
            </div>
          </template>
        </div>

        <!-- Right: File Browser -->
        <div class="modal-columns-right">
          <!-- Breadcrumbs -->
          <div class="browse-breadcrumbs" style="margin-bottom:10px;">
            <template x-for="(crumb, i) in breadcrumbs" :key="crumb.path">
              <span>
                <span x-show="i > 0" style="color:var(--text-muted); margin:0 2px;">&rsaquo;</span>
                <a href="#" @click.prevent="browseTo(crumb.path)" x-text="crumb.label"
                   style="color:var(--accent); text-decoration:none; font-size:13px;"></a>
              </span>
            </template>
          </div>

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
        </div>
      </div>

      <!-- Footer -->
      <div class="modal-footer">
        <button class="btn btn-sm" @click="showNewSessionModal = false">Cancel</button>
        <button class="btn btn-sm btn-primary" @click="createManagedSession()" :disabled="!newSessionCWD.trim()">
          Open
        </button>
      </div>
    </div>
  </div>
```

- [ ] **Step 3: Reset `showNewFolderInput` when modal closes**

In `server/web/static/app.js`, find the `resetBrowseState()` method (called when the modal opens). Add `this.showNewFolderInput = false;` to it. Search for `resetBrowseState` and add the line inside that function body.

- [ ] **Step 4: Verify the New Session modal opens and functions correctly**

Run: Open `http://localhost:9999`, click the "+" button or "New Session" to open the modal. Verify:
- Two-column layout visible on desktop
- Recents on the left, file browser on the right
- Path input works
- "+ New Folder" collapses/expands
- Cancel and Open buttons work
- Click backdrop to close works

- [ ] **Step 5: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat(ui): redesign New Session modal with two-column layout (#119)"
```

---

### Task 3: Refactor Settings Modal to Tabbed Sidebar Layout

**Files:**
- Modify: `server/web/static/index.html:1213-1391` (Settings Modal HTML)
- Modify: `server/web/static/app.js` (add `settingsActiveTab`, `settingsOriginal`, change tracking)

- [ ] **Step 1: Add settings tab state and change tracking to app.js**

In `server/web/static/app.js`, find the settings state block (around line 124-136). Replace:

```javascript
    settingsAccordion: { server: false, integrations: false, shortcuts: false },
```

with:

```javascript
    settingsActiveTab: 'server',
    settingsOriginal: null,
```

- [ ] **Step 2: Add a computed property for unsaved changes count**

In `server/web/static/app.js`, find `openSettingsModal()` (around line 481). After the line that sets `this.settingsForm` from the response data, add:

```javascript
        this.settingsOriginal = JSON.parse(JSON.stringify(this.settingsForm));
```

Then add a new method `settingsChangeCount()` near the other settings methods:

```javascript
    settingsChangeCount() {
      if (!this.settingsOriginal) return 0;
      let count = 0;
      const keys = ['port', 'ngrok_authtoken', 'claude_bin', 'claude_args', 'claude_env', 'compact_every_n_continues', 'github_token', 'jira_url', 'jira_token', 'jira_email', 'asana_token', 'google_tasks_token'];
      for (const key of keys) {
        if (String(this.settingsForm[key] || '') !== String(this.settingsOriginal[key] || '')) count++;
      }
      if (JSON.stringify(this.settingsForm.shortcuts) !== JSON.stringify(this.settingsOriginal.shortcuts)) count++;
      return count;
    },

    isFieldChanged(fieldName) {
      if (!this.settingsOriginal) return false;
      return String(this.settingsForm[fieldName] || '') !== String(this.settingsOriginal[fieldName] || '');
    },
```

- [ ] **Step 3: Reset tab state when modal opens**

In `openSettingsModal()`, add at the top of the method:

```javascript
      this.settingsActiveTab = 'server';
```

- [ ] **Step 4: Replace the Settings Modal HTML**

In `server/web/static/index.html`, replace the entire Settings Modal block (from `<!-- Settings Modal -->` through its closing `</div>` before `<!-- Permission Prompt Modal -->`) with:

```html
  <!-- Settings Modal -->
  <div x-show="showSettingsModal" x-cloak
       class="modal-backdrop" style="z-index:100;"
       @click.self="showSettingsModal = false">
    <div class="modal-lg" style="height:min(85vh, 520px);">
      <!-- Mobile header -->
      <div class="mobile-detail-header mobile-modal-header">
        <button @click="showSettingsModal = false" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title" x-text="settingsFirstRun ? 'Welcome — Configure Claude Controller' : 'Settings'"></span>
      </div>

      <!-- Desktop header -->
      <div class="modal-header desktop-modal-title">
        <h3 x-text="settingsFirstRun ? 'Welcome — Configure Claude Controller' : 'Settings'"></h3>
        <button class="modal-close-btn" @click="showSettingsModal = false" aria-label="Close">&times;</button>
      </div>

      <!-- Tabbed layout -->
      <div class="settings-layout" x-show="!settingsFirstRun">
        <!-- Sidebar tabs -->
        <div class="settings-tabs">
          <div class="settings-tab" :class="{ active: settingsActiveTab === 'server' }" @click="settingsActiveTab = 'server'">Server</div>
          <div class="settings-tab" :class="{ active: settingsActiveTab === 'integrations' }" @click="settingsActiveTab = 'integrations'">Integrations</div>
          <div class="settings-tab" :class="{ active: settingsActiveTab === 'shortcuts' }" @click="settingsActiveTab = 'shortcuts'">Shortcuts</div>
          <div class="settings-tab-divider"></div>
          <div class="settings-tab danger" :class="{ active: settingsActiveTab === 'actions' }" @click="settingsActiveTab = 'actions'">Actions</div>
        </div>

        <!-- Content panel -->
        <div class="settings-content">
          <!-- Server tab -->
          <div x-show="settingsActiveTab === 'server'">
            <div class="settings-field">
              <label class="modal-label">
                Port
                <span x-show="isFieldChanged('port')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.port" type="text" placeholder="8080" class="modal-input" :style="isFieldChanged('port') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Server port (requires restart)</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                Ngrok Auth Token
                <span x-show="isFieldChanged('ngrok_authtoken')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.ngrok_authtoken" type="password" placeholder="ngrok auth token" class="modal-input" :style="isFieldChanged('ngrok_authtoken') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">For remote access via ngrok tunnel (requires restart)</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                Claude Binary
                <span x-show="isFieldChanged('claude_bin')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.claude_bin" type="text" placeholder="claude" class="modal-input" :style="isFieldChanged('claude_bin') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Path to Claude CLI binary</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                CLI Arguments
                <span x-show="isFieldChanged('claude_args')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.claude_args" type="text" placeholder="--dangerously-skip-permissions" class="modal-input" :style="isFieldChanged('claude_args') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Space-separated CLI flags for managed sessions</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                Environment Variables
                <span x-show="isFieldChanged('claude_env')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.claude_env" type="text" placeholder="CLAUDE_CONFIG_DIR=/path" class="modal-input" :style="isFieldChanged('claude_env') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Comma-separated KEY=VALUE pairs for managed session environment</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                Compact Every N Continues
                <span x-show="isFieldChanged('compact_every_n_continues')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.compact_every_n_continues" type="number" min="0" placeholder="0" class="modal-input" :style="isFieldChanged('compact_every_n_continues') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Run /compact every N auto-continues to reduce token usage. 0 = disabled.</div>
            </div>

            <div class="settings-field">
              <label class="modal-label">
                GitHub Token
                <span x-show="isFieldChanged('github_token')" class="settings-change-badge">changed</span>
              </label>
              <input x-model="settingsForm.github_token" type="password" placeholder="ghp_..." class="modal-input" :style="isFieldChanged('github_token') ? 'border-color:var(--yellow)' : ''">
              <div class="modal-hint">Personal access token for GitHub issue tracking (needs repo scope)</div>
            </div>
          </div>

          <!-- Integrations tab -->
          <div x-show="settingsActiveTab === 'integrations'">
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:16px;">
              Connect project management tools to browse issues and tasks alongside GitHub.
            </div>

            <div style="border:1px solid var(--border); border-radius:10px; padding:14px; margin-bottom:12px;">
              <div style="font-size:13px; font-weight:600; margin-bottom:10px; display:flex; align-items:center; gap:6px;">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M15.09 7.39L8.61.91 8 .3l-5.09 5.1a.5.5 0 000 .7L7.65 10.84a.5.5 0 00.7 0l6.74-6.74a.5.5 0 000-.71zM8 10.13L5.17 7.3 8 4.47l2.83 2.83L8 10.13z" fill="#2684FF"/></svg>
                Jira
              </div>
              <div class="settings-field">
                <label class="modal-label">Base URL</label>
                <input x-model="settingsForm.jira_url" type="text" placeholder="https://yourorg.atlassian.net" class="modal-input">
              </div>
              <div class="settings-field">
                <label class="modal-label">API Token</label>
                <input x-model="settingsForm.jira_token" type="password" placeholder="Jira API token" class="modal-input">
              </div>
              <div class="settings-field" style="margin-bottom:0;">
                <label class="modal-label">Email</label>
                <input x-model="settingsForm.jira_email" type="email" placeholder="you@company.com" class="modal-input">
              </div>
            </div>

            <div style="border:1px solid var(--border); border-radius:10px; padding:14px; margin-bottom:12px;">
              <div style="font-size:13px; font-weight:600; margin-bottom:10px; display:flex; align-items:center; gap:6px;">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="4.5" r="2.8" fill="#F06A6A"/><circle cx="3.5" cy="11.5" r="2.8" fill="#F06A6A"/><circle cx="12.5" cy="11.5" r="2.8" fill="#F06A6A"/></svg>
                Asana
              </div>
              <div class="settings-field" style="margin-bottom:0;">
                <label class="modal-label">Personal Access Token</label>
                <input x-model="settingsForm.asana_token" type="password" placeholder="Asana personal access token" class="modal-input">
              </div>
            </div>

            <div style="border:1px solid var(--border); border-radius:10px; padding:14px;">
              <div style="font-size:13px; font-weight:600; margin-bottom:10px; display:flex; align-items:center; gap:6px;">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="7" stroke="#4285F4" stroke-width="1.5" fill="none"/><path d="M5 8l2 2 4-4" stroke="#4285F4" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg>
                Google Tasks
              </div>
              <div class="settings-field" style="margin-bottom:0;">
                <label class="modal-label">API Key</label>
                <input x-model="settingsForm.google_tasks_token" type="password" placeholder="Google Tasks API key" class="modal-input">
              </div>
            </div>
          </div>

          <!-- Shortcuts tab -->
          <div x-show="settingsActiveTab === 'shortcuts'">
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:16px;">
              Define shortcuts that map a key (emoji or text) to a message sent in chat.
            </div>
            <template x-for="(shortcut, idx) in settingsForm.shortcuts" :key="idx">
              <div style="display:flex; gap:8px; align-items:center; margin-bottom:8px;">
                <input x-model="shortcut.key" type="text" maxlength="20" placeholder="Key"
                       class="modal-input" style="width:70px; flex-shrink:0; text-align:center;">
                <input x-model="shortcut.value" type="text" placeholder="Message to send..."
                       class="modal-input" style="flex:1;">
                <button @click="settingsForm.shortcuts.splice(idx, 1)" class="btn btn-sm"
                        style="padding:4px 8px; flex-shrink:0; color:var(--red);" title="Remove shortcut">&times;</button>
              </div>
            </template>
            <button @click="settingsForm.shortcuts.push({key:'', value:''})" class="btn btn-sm"
                    style="font-size:12px; color:var(--accent);">+ Add Shortcut</button>
          </div>

          <!-- Actions tab -->
          <div x-show="settingsActiveTab === 'actions'">
            <div class="settings-field">
              <button class="btn btn-sm" @click="restartServer()" :disabled="serverRestarting"
                      style="background:var(--red); color:white; border:none; padding:10px 20px; border-radius:8px; cursor:pointer; font-size:13px;">
                <span x-show="!serverRestarting">Restart Server</span>
                <span x-show="serverRestarting">Restarting...</span>
              </button>
              <div class="modal-hint">Gracefully restarts the server, preserving all session state</div>
            </div>
          </div>
        </div>
      </div>

      <!-- First-run flow (no sidebar, just server fields) -->
      <div x-show="settingsFirstRun" class="modal-body" style="padding-top:16px;">
        <div class="settings-field">
          <label class="modal-label">Port</label>
          <input x-model="settingsForm.port" type="text" placeholder="8080" class="modal-input">
          <div class="modal-hint">Server port (requires restart)</div>
        </div>
        <div class="settings-field">
          <label class="modal-label">Ngrok Auth Token</label>
          <input x-model="settingsForm.ngrok_authtoken" type="password" placeholder="ngrok auth token" class="modal-input">
          <div class="modal-hint">For remote access via ngrok tunnel (requires restart)</div>
        </div>
        <div class="settings-field">
          <label class="modal-label">Claude Binary</label>
          <input x-model="settingsForm.claude_bin" type="text" placeholder="claude" class="modal-input">
          <div class="modal-hint">Path to Claude CLI binary</div>
        </div>
        <div class="settings-field">
          <label class="modal-label">CLI Arguments</label>
          <input x-model="settingsForm.claude_args" type="text" placeholder="--dangerously-skip-permissions" class="modal-input">
          <div class="modal-hint">Space-separated CLI flags for managed sessions</div>
        </div>
      </div>

      <!-- Error -->
      <p class="error-msg" x-show="settingsError" x-text="settingsError" style="margin:0 24px 8px;"></p>

      <!-- Footer -->
      <div class="modal-footer">
        <template x-if="!settingsFirstRun && settingsChangeCount() > 0">
          <span class="modal-footer-status" x-text="settingsChangeCount() + ' unsaved change' + (settingsChangeCount() > 1 ? 's' : '')"></span>
        </template>
        <button class="btn btn-sm" @click="showSettingsModal = false; settingsFirstRun = false;" x-text="settingsFirstRun ? 'Skip' : 'Cancel'"></button>
        <button class="btn btn-sm btn-primary" @click="saveSettings()" :disabled="settingsSaving">
          <span x-show="!settingsSaving" x-text="settingsFirstRun ? 'Save & Continue' : 'Save'"></span>
          <span x-show="settingsSaving">Saving...</span>
        </button>
      </div>
    </div>
  </div>
```

- [ ] **Step 5: Clean up references to `settingsAccordion` in app.js**

Search for `settingsAccordion` in app.js and remove all references:
- Remove the `settingsAccordion: { server: false, integrations: false, shortcuts: false },` line (already replaced in Step 1)
- Search for any other references and remove them (there should be none beyond the state declaration)

- [ ] **Step 6: Verify the Settings modal opens and all tabs work**

Run: Open `http://localhost:9999`, click the settings gear icon. Verify:
- Sidebar shows Server, Integrations, Shortcuts, divider, Actions
- Clicking each tab shows the correct content
- Server tab is selected by default
- Change badges appear when editing a field
- Footer shows "N unsaved changes" count
- Save/Cancel buttons work
- First-run flow shows simplified view (no sidebar)

- [ ] **Step 7: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat(ui): redesign Settings modal with tabbed sidebar layout (#119)"
```

---

### Task 4: Refactor Task, Resume, and Permission Modals

**Files:**
- Modify: `server/web/static/index.html` (Task modal ~1121-1211, Resume modal ~1075-1119, Permission modal ~1393-1427)

- [ ] **Step 1: Replace the Resume Picker Modal HTML**

Replace the `<!-- Resume Picker Modal -->` block with:

```html
  <!-- Resume Picker Modal -->
  <div x-show="showResumePicker" x-cloak
       class="modal-backdrop" style="z-index:100;"
       @click.self="showResumePicker = false">
    <div class="modal-sm">
      <!-- Mobile header -->
      <div class="mobile-detail-header mobile-modal-header">
        <button @click="showResumePicker = false" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title">Resume a Session</span>
      </div>

      <!-- Desktop header -->
      <div class="modal-header desktop-modal-title">
        <h3>Resume a Session</h3>
        <button class="modal-close-btn" @click="showResumePicker = false" aria-label="Close">&times;</button>
      </div>

      <div class="modal-body">
        <template x-if="resumeLoading">
          <div style="padding:2rem; text-align:center; color:var(--text-muted); font-size:13px;">Loading sessions...</div>
        </template>

        <template x-if="!resumeLoading && resumableSessions.length === 0">
          <div style="padding:2rem; text-align:center; color:var(--text-muted); font-size:13px;">No sessions to resume</div>
        </template>

        <template x-for="rs in resumableSessions" :key="rs.session_id">
          <div @click="resumeSession(rs.session_id, rs.summary)"
               style="padding:12px 16px; border-bottom:1px solid var(--border); cursor:pointer; transition:background 0.15s;"
               onmouseenter="this.style.background='var(--hover)'"
               onmouseleave="this.style.background='transparent'">
            <div style="font-weight:600; font-size:14px; margin-bottom:2px;" x-text="rs.summary || 'Untitled session'"></div>
            <div style="font-size:12px; color:var(--text-muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" x-text="rs.first_prompt"></div>
            <div style="display:flex; gap:8px; margin-top:4px; font-size:11px; color:var(--text-muted);">
              <span x-show="rs.git_branch" x-text="rs.git_branch" style="background:var(--input-bg); padding:1px 5px; border-radius:3px;"></span>
              <span x-text="rs.message_count + ' messages'"></span>
              <span x-text="timeAgo(rs.modified)"></span>
            </div>
          </div>
        </template>
      </div>

      <div class="modal-footer">
        <button class="btn btn-sm" @click="showResumePicker = false">Cancel</button>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: Replace the Task Create/Edit Modal HTML**

Replace the `<!-- Task Create/Edit Modal -->` block with:

```html
  <!-- Task Create/Edit Modal -->
  <div x-show="taskModalOpen" x-cloak
       class="modal-backdrop" style="z-index:200;"
       @click.self="taskModalOpen = false" @keydown.escape.window="taskModalOpen && (taskModalOpen = false)">
    <div @click.stop class="modal-md">
      <!-- Mobile header -->
      <div class="mobile-detail-header mobile-modal-header">
        <button @click="taskModalOpen = false" class="mobile-back-btn" aria-label="Back">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
            <path d="M12.7 5.3a1 1 0 010 1.4L9.4 10l3.3 3.3a1 1 0 01-1.4 1.4l-4-4a1 1 0 010-1.4l4-4a1 1 0 011.4 0z"/>
          </svg>
        </button>
        <span class="mobile-detail-title" x-text="editingTask ? editingTask.name : 'New Scheduled Task'"></span>
      </div>

      <!-- Desktop header -->
      <div class="modal-header desktop-modal-title">
        <h3 x-text="editingTask ? editingTask.name : 'New Scheduled Task'"></h3>
        <div style="display:flex; align-items:center; gap:6px;">
          <template x-if="editingTask">
            <div style="display:flex; gap:6px;">
              <button type="button" class="btn btn-sm" @click="triggerTask(editingTask.id)" style="font-size:0.75rem;">&#9654; Run Now</button>
              <button type="button" class="btn btn-sm" @click="toggleTaskEnabled(editingTask); editingTask.enabled = !editingTask.enabled" style="font-size:0.75rem;"
                      x-text="editingTask?.enabled ? 'Disable' : 'Enable'"></button>
              <button type="button" class="btn btn-sm" @click="taskModalOpen = false; deleteTask(editingTask.id)" style="font-size:0.75rem; color:#ef4444;">Delete</button>
            </div>
          </template>
          <button class="modal-close-btn" @click="taskModalOpen = false" aria-label="Close">&times;</button>
        </div>
      </div>

      <div class="modal-body">
        <form @submit.prevent="saveTask()">
          <div class="settings-field">
            <label class="modal-label">Name</label>
            <input type="text" x-model="taskForm.name" required class="modal-input" placeholder="e.g., Daily Backup">
          </div>
          <div class="settings-field">
            <label class="modal-label">Type</label>
            <select x-model="taskForm.task_type" class="modal-input">
              <option value="shell">Shell Command</option>
              <option value="claude">Claude Command</option>
            </select>
          </div>
          <div class="settings-field">
            <label class="modal-label" x-text="taskForm.task_type === 'claude' ? 'Prompt' : 'Command'"></label>
            <textarea x-model="taskForm.command" required rows="3"
                      class="modal-input" style="font-family:monospace; font-size:0.85rem; resize:vertical;"
                      :placeholder="taskForm.task_type === 'claude' ? 'e.g., Summarize the latest changes' : 'e.g., tar -czf backup.tar.gz ./data'"></textarea>
          </div>
          <div class="settings-field">
            <label class="modal-label">Working Directory</label>
            <input type="text" x-model="taskForm.working_directory" required
                   class="modal-input" style="font-family:monospace; font-size:0.85rem;"
                   placeholder="/absolute/path/to/project">
          </div>
          <div class="settings-field">
            <label class="modal-label">Cron Expression</label>
            <input type="text" x-model="taskForm.cron_expression" required
                   class="modal-input" style="font-family:monospace; font-size:0.85rem;"
                   placeholder="0 9 * * *">
            <div class="modal-hint">
              min hour day month weekday |
              <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 * * * *'">hourly</span>
              <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 9 * * *'">daily 9am</span>
              <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '0 9 * * 1-5'">weekdays</span>
              <span style="cursor:pointer; text-decoration:underline;" @click="taskForm.cron_expression = '*/5 * * * *'">every 5min</span>
            </div>
          </div>
          <div x-show="taskFormErrors" style="color:#ef4444; font-size:0.8rem; margin-bottom:8px;" x-text="taskFormErrors"></div>

          <!-- Recent Runs (only when editing) -->
          <div x-show="editingTask && taskRuns.length > 0" style="margin-top:8px; border-top:1px solid var(--border); padding-top:16px;">
            <h4 style="margin:0 0 10px 0; font-size:0.85rem; color:var(--text-muted);">Recent Runs</h4>
            <template x-for="run in taskRuns.slice(0, 5)" :key="run.id">
              <div style="border:1px solid var(--border); border-radius:8px; padding:8px 10px; margin-bottom:6px; font-size:0.8rem;">
                <div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:4px;">
                  <div style="display:flex; align-items:center; gap:6px;">
                    <span style="width:7px; height:7px; border-radius:50%;"
                          :style="'background:' + (run.status === 'success' ? '#22c55e' : run.status === 'failed' ? '#ef4444' : '#eab308')"></span>
                    <span style="font-weight:500;" x-text="run.status"></span>
                    <span style="font-size:0.75rem; color:var(--text-muted);" x-text="formatRelativeTime(run.started_at)"></span>
                  </div>
                  <span style="font-size:0.7rem; color:var(--text-muted);" x-show="run.exit_code != null" x-text="'exit ' + run.exit_code"></span>
                </div>
                <div x-show="run.output" style="background:var(--input-bg); border-radius:4px; padding:6px; font-family:monospace; font-size:0.7rem; white-space:pre-wrap; word-break:break-all; max-height:120px; overflow-y:auto; color:var(--text-muted);"
                     x-text="(run.output || '').substring(0, 300) + ((run.output || '').length > 300 ? '\n...' : '')"></div>
              </div>
            </template>
          </div>

          <div class="modal-footer" style="margin:16px -24px -24px; padding:16px 24px;">
            <button type="button" class="btn btn-sm" @click="taskModalOpen = false">Cancel</button>
            <button type="submit" class="btn btn-sm btn-primary" :disabled="taskLoading" x-text="editingTask ? 'Update' : 'Create'"></button>
          </div>
        </form>
      </div>
    </div>
  </div>
```

- [ ] **Step 3: Replace the Permission Prompt Modal HTML**

Replace the `<!-- Permission Prompt Modal -->` block with:

```html
  <!-- Permission Prompt Modal -->
  <div x-show="pendingPermission" x-cloak
       class="modal-backdrop" style="z-index:200;">
    <div @click.stop class="modal-sm">
      <div class="modal-header">
        <h3 style="display:flex; align-items:center; gap:8px;">
          <span style="color:var(--yellow);">&#9888;</span> Permission Required
        </h3>
      </div>
      <div class="modal-body" style="padding-bottom:16px;">
        <template x-if="pendingPermission">
          <div>
            <div style="margin-bottom:12px;">
              <span style="font-weight:600;" x-text="pendingPermission.tool_name || 'Tool'"></span>
              <span style="color:var(--text-muted); margin-left:8px;" x-text="pendingPermission.description || ''"></span>
            </div>
            <template x-if="pendingPermission.input">
              <pre style="background:var(--input-bg); border:1px solid var(--border); border-radius:8px; padding:12px; font-size:12px; overflow-x:auto; max-height:200px; overflow-y:auto; margin-bottom:16px; white-space:pre-wrap; word-break:break-all;"
                   x-text="typeof pendingPermission.input === 'string' ? pendingPermission.input : JSON.stringify(pendingPermission.input, null, 2)"></pre>
            </template>
          </div>
        </template>
      </div>
      <div class="modal-footer">
        <button class="btn" @click="respondToPermission('deny')"
                style="background:var(--red); color:white; border:none; padding:8px 16px; border-radius:8px; cursor:pointer;">
          Deny
        </button>
        <button class="btn" @click="respondToPermission('allow_always')"
                style="background:var(--input-bg); color:var(--text); border:1px solid var(--border); padding:8px 16px; border-radius:8px; cursor:pointer;">
          Allow Always
        </button>
        <button class="btn" @click="respondToPermission('allow')"
                style="background:var(--green); color:white; border:none; padding:8px 16px; border-radius:8px; cursor:pointer;">
          Allow
        </button>
      </div>
    </div>
  </div>
```

- [ ] **Step 4: Verify all three modals function correctly**

Run: Open `http://localhost:9999`. Test:
- Resume picker: click "Resume" in sidebar menu — list renders, close works
- Task modal: go to Tasks tab, click "New Task" or edit existing — form renders, save works
- Permission modal: if a permission prompt appears, buttons work

- [ ] **Step 5: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): refactor Task, Resume, Permission modals to unified CSS (#119)"
```

---

### Task 5: Clean Up Old Modal CSS and Remove Dead Code

**Files:**
- Modify: `server/web/static/style.css` (remove old modal-specific selectors)

- [ ] **Step 1: Remove old mobile modal overrides**

In `server/web/static/style.css`, inside the `@media (max-width: 768px)` block, find and remove the old modal selectors that targeted inline-styled modals:

```css
  /* Remove these rules — replaced by .modal-sm/.modal-md/.modal-lg selectors */
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
```

Also find and remove any `.modal-inner` rules in the mobile block (like `padding-top: 0 !important`) since we no longer use `.modal-inner`.

- [ ] **Step 2: Remove the old `.settings-accordion-header` and `.settings-accordion-body` CSS**

Search for `settings-accordion` in style.css and remove all related rules (the accordion header, chevron, and body styles). These are no longer used since we replaced accordions with tabs.

- [ ] **Step 3: Verify no console errors and all modals still work**

Run: Open `http://localhost:9999`, open each modal in succession. Check DevTools console for any CSS or JS errors. Confirm mobile view works using browser DevTools responsive mode.

- [ ] **Step 4: Commit**

```bash
git add server/web/static/style.css
git commit -m "chore(ui): remove old modal CSS overrides and accordion styles (#119)"
```

---

### Task 6: Final Verification and Test

**Files:**
- No file changes — verification only

- [ ] **Step 1: Run Go tests to ensure nothing is broken**

Run: `cd server && go test ./... -v`
Expected: All tests pass. (The modal changes are HTML/CSS/JS only, so Go tests should be unaffected.)

- [ ] **Step 2: Verify all modals on desktop**

Open `http://localhost:9999` in a desktop browser. Test each modal:
1. **New Session**: Two-column layout, recents left, browser right, "+ New Folder" collapses, "Open" button works
2. **Settings**: Tabbed sidebar, all 4 tabs work, change badges appear, unsaved changes count in footer
3. **Task Editor**: Form fields, cron shortcuts, recent runs (if editing)
4. **Resume Picker**: Session list, click to resume
5. **Permission Prompt**: (trigger by using a managed session that requests permission)

- [ ] **Step 3: Verify all modals on mobile (responsive mode)**

Open DevTools, switch to responsive mode (iPhone SE or similar). For each modal:
- Full-screen rendering
- Sticky header with back button
- New Session: columns stacked vertically
- Settings: horizontal tab bar at top
- Scrolling works within modal body

- [ ] **Step 4: Commit any final fixes if needed**

If any issues were found and fixed:
```bash
git add -A
git commit -m "fix(ui): polish modal redesign edge cases (#119)"
```

Now let me do the self-review of this plan against the spec.

**Self-Review:**

1. **Spec coverage:**
   - Unified CSS system: Task 1
   - New Session two-column: Task 2
   - Settings tabbed sidebar: Task 3
   - Task/Resume/Permission cleanup: Task 4
   - Mobile behavior: Tasks 1 (CSS) + 6 (verification)
   - Remove old CSS: Task 5
   - First-run flow: Task 3 (included in Settings HTML)
   - All covered.

2. **Placeholder scan:** No TBDs, TODOs, or vague instructions. All steps have code or commands.

3. **Type consistency:** `settingsActiveTab`, `showNewFolderInput`, `settingsOriginal`, `settingsChangeCount()`, `isFieldChanged()` — used consistently across Tasks 2-3. CSS class names match between Task 1 definitions and Tasks 2-4 usage.

Plan is clean. Saving now.
