# Status Line Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a configurable status line footer bar to the web UI showing session metadata (model, cost, context %, turns, activity, working directory) for managed sessions.

**Architecture:** Frontend-only feature. Alpine.js state holds status line config (persisted to localStorage) and session model name (parsed from NDJSON stream). New "Status Line" tab in existing Settings modal with toggle checkboxes. Status line HTML sits between the instruction-bar and the end of the chat-column.

**Tech Stack:** Alpine.js, vanilla CSS, localStorage

---

### Task 1: Add Alpine state and localStorage persistence for status line config

**Files:**
- Modify: `server/web/static/app.js:171` (add state properties after `sessionCost`)
- Modify: `server/web/static/app.js:242-254` (load config in `init()`)
- Modify: `server/web/static/app.js:857` (reset `sessionModel` on session switch)

- [ ] **Step 1: Add state properties**

In `server/web/static/app.js`, after line 171 (`sessionCost: null,`), add:

```javascript
    sessionModel: null,
    statusLineConfig: {
      model: true,
      cost: true,
      context: false,
      turns: false,
      activity: false,
      cwd: false,
    },
```

- [ ] **Step 2: Add config loading in init()**

In `server/web/static/app.js`, inside the `init()` method, after line 244 (`this.voiceChatSupported = ...;`), add:

```javascript
      // Load status line config from localStorage
      try {
        const saved = localStorage.getItem('statusLineConfig');
        if (saved) {
          const parsed = JSON.parse(saved);
          this.statusLineConfig = { ...this.statusLineConfig, ...parsed };
        }
      } catch (e) { /* ignore corrupt localStorage */ }
```

- [ ] **Step 3: Add helper methods**

In `server/web/static/app.js`, add these methods (after the existing `turnBarColor()` method around line 728):

```javascript
    toggleStatusLineItem(key) {
      this.statusLineConfig[key] = !this.statusLineConfig[key];
      localStorage.setItem('statusLineConfig', JSON.stringify(this.statusLineConfig));
    },

    statusLineVisible() {
      const sess = this.sessions.find(s => s.id === this.selectedSessionId);
      if (!sess || sess.mode !== 'managed') return false;
      return Object.values(this.statusLineConfig).some(v => v);
    },
```

- [ ] **Step 4: Reset sessionModel on session switch**

In `server/web/static/app.js`, inside `selectSession()`, after line 857 (`this.sessionCost = null;`), add:

```javascript
      this.sessionModel = null;
```

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(ui): add Alpine state and localStorage for status line config (#64)"
```

---

### Task 2: Parse model name from NDJSON system/init event

**Files:**
- Modify: `server/web/static/app.js:1938` (SSE message handler, after the system error block)

- [ ] **Step 1: Add model extraction from system/init events**

In `server/web/static/app.js`, in the SSE `onmessage` handler, find the block at line 1938:

```javascript
          } else if (data.type === 'system' && data.error) {
```

Insert **before** that `else if` (after the closing `}` of the assistant text block at line 1937):

```javascript
          // Capture model name from system init event
          if (data.type === 'system' && data.subtype === 'init' && data.model) {
            this.sessionModel = data.model;
          }
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(ui): parse model name from NDJSON system/init event (#64)"
```

---

### Task 3: Add status line HTML to index.html

**Files:**
- Modify: `server/web/static/index.html:362` (after instruction-bar closing `</div>`, before chat-column closing `</div>`)

- [ ] **Step 1: Add status line HTML**

In `server/web/static/index.html`, after line 362 (the `</div>` that closes the `.instruction-bar`), add:

```html
            <!-- Status line -->
            <div class="status-line" x-show="statusLineVisible()" x-cloak>
              <template x-if="statusLineConfig.model">
                <span class="status-line-item" x-show="sessionModel">
                  <span class="status-line-label">Model</span>
                  <span x-text="sessionModel"></span>
                </span>
              </template>
              <template x-if="statusLineConfig.cost">
                <span class="status-line-item">
                  <span class="status-line-label">Cost</span>
                  <span x-text="sessionCost != null ? '$' + sessionCost.toFixed(4) : '$0.00'"></span>
                </span>
              </template>
              <template x-if="statusLineConfig.context">
                <span class="status-line-item" style="display:none;">
                  <span class="status-line-label">Context</span>
                  <span>—</span>
                </span>
              </template>
              <template x-if="statusLineConfig.turns">
                <span class="status-line-item" x-show="currentSession?.max_turns > 0">
                  <span class="status-line-label">Turns</span>
                  <span x-text="(currentSession?.turn_count || 0) + '/' + (currentSession?.max_turns || 0)"></span>
                </span>
              </template>
              <template x-if="statusLineConfig.activity">
                <span class="status-line-item">
                  <span class="status-line-label">Activity</span>
                  <span class="status-line-dot"
                        :style="'background:' + (currentSession?.activity_state === 'working' ? '#f39c12' : currentSession?.activity_state === 'waiting' ? '#22c55e' : '#6b7280')"></span>
                  <span x-text="currentSession?.activity_state || 'idle'"></span>
                </span>
              </template>
              <template x-if="statusLineConfig.cwd">
                <span class="status-line-item" x-show="currentSession?.cwd">
                  <span class="status-line-label">CWD</span>
                  <span x-text="currentSession?.cwd" style="max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; display:inline-block; vertical-align:bottom;"></span>
                </span>
              </template>
            </div>
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): add status line HTML to chat column (#64)"
```

---

### Task 4: Add status line CSS

**Files:**
- Modify: `server/web/static/style.css:535` (after `.instruction-bar` styles)

- [ ] **Step 1: Add status line CSS**

In `server/web/static/style.css`, after line 535 (the closing `}` of `.instruction-bar`), add:

```css
/* Status line */
.status-line {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 1rem;
  border-top: 1px solid var(--border);
  font-size: 0.75rem;
  color: var(--text-muted);
  background: var(--bg);
  flex-shrink: 0;
  min-height: 24px;
}
.status-line-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.status-line-item + .status-line-item::before {
  content: '|';
  color: var(--border);
  margin-right: 4px;
}
.status-line-label {
  opacity: 0.6;
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.status-line-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  display: inline-block;
}
```

- [ ] **Step 2: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat(ui): add status line CSS styles (#64)"
```

---

### Task 5: Add Status Line settings tab

**Files:**
- Modify: `server/web/static/index.html:1261` (settings tabs list)
- Modify: `server/web/static/index.html:1400` (before Actions tab content)

- [ ] **Step 1: Add the tab button**

In `server/web/static/index.html`, after line 1261 (the Shortcuts tab div):

```html
          <div class="settings-tab" :class="{ active: settingsActiveTab === 'shortcuts' }" @click="settingsActiveTab = 'shortcuts'">Shortcuts</div>
```

Add:

```html
          <div class="settings-tab" :class="{ active: settingsActiveTab === 'statusline' }" @click="settingsActiveTab = 'statusline'">Status Line</div>
```

- [ ] **Step 2: Add the tab content**

In `server/web/static/index.html`, after the Shortcuts tab content closing `</div>` (at line 1399, after the `</div>` that closes the `x-show="settingsActiveTab === 'shortcuts'"` block), add:

```html
          <!-- Status Line tab -->
          <div x-show="settingsActiveTab === 'statusline'">
            <div style="font-size:12px; color:var(--text-muted); margin-bottom:16px;">
              Choose which items appear in the bottom status bar for managed sessions.
            </div>
            <template x-for="item in [
              { key: 'model', label: 'Model', desc: 'Claude model name (e.g., Opus 4.6)' },
              { key: 'cost', label: 'Cost', desc: 'Accumulated session cost' },
              { key: 'context', label: 'Context %', desc: 'Context window usage (when available)' },
              { key: 'turns', label: 'Turns', desc: 'Turn count / max turns' },
              { key: 'activity', label: 'Activity', desc: 'Session activity state (working/waiting/idle)' },
              { key: 'cwd', label: 'Working Dir', desc: 'Session working directory' },
            ]" :key="item.key">
              <div style="display:flex; align-items:center; gap:10px; padding:8px 0; border-bottom:1px solid var(--border);">
                <label style="display:flex; align-items:center; gap:8px; cursor:pointer; flex:1;">
                  <input type="checkbox" :checked="statusLineConfig[item.key]"
                         @change="toggleStatusLineItem(item.key)"
                         style="width:16px; height:16px; accent-color:var(--accent); cursor:pointer;">
                  <div>
                    <div style="font-size:13px; font-weight:500;" x-text="item.label"></div>
                    <div style="font-size:11px; color:var(--text-muted);" x-text="item.desc"></div>
                  </div>
                </label>
              </div>
            </template>
          </div>
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): add Status Line tab to Settings modal (#64)"
```

---

### Task 6: Manual verification and final commit

- [ ] **Step 1: Build and run the server**

```bash
cd server && go build -o claude-controller . && echo "Build OK"
```

- [ ] **Step 2: Verify the status line renders**

Start the server and open the web UI. Create or select a managed session. Verify:
- Status line appears at the bottom of the chat area
- Model and Cost items are visible by default
- Other items are hidden

- [ ] **Step 3: Verify Settings tab works**

Open Settings → Status Line tab. Toggle items on/off. Verify:
- Changes immediately reflect in the status line
- Refreshing the page preserves the config

- [ ] **Step 4: Verify model name parsing**

Send a message in a managed session. After the response, verify the Model item shows the model name (parsed from the NDJSON init event).

- [ ] **Step 5: Update CLAUDE.md with spec/plan references**

In `CLAUDE.md`, add to the Spec & Plan section:

```markdown
- Status line spec: `docs/superpowers/specs/2026-04-19-status-line-design.md`
- Status line plan: `docs/superpowers/plans/2026-04-19-status-line.md`
```

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add status line spec/plan references to CLAUDE.md (#64)"
```
