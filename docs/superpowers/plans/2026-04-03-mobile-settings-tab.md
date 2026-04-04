# Mobile Settings Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a 5th "Settings" tab to the mobile bottom nav bar that opens the existing settings modal.

**Architecture:** Add a button to the `.mobile-tab-bar` in `index.html` that triggers `showSettingsModal = true`. No new state, views, or CSS changes needed — the tab bar already uses flexbox with `flex: 1` per child.

**Tech Stack:** HTML, Alpine.js, CSS (existing)

---

### Task 1: Add Settings Tab Button

**Files:**
- Modify: `server/web/static/index.html:849-857` (inside `.mobile-tab-bar`, after the Tasks tab button)

- [ ] **Step 1: Add the Settings button after the Tasks tab closing `</button>` tag (line 856)**

Insert the following after the Tasks `</button>` (line 856) and before the closing `</div>` (line 857):

```html
      <button class="mobile-tab" @click="showSettingsModal = true" role="button" aria-label="Settings">
        <svg width="18" height="18" viewBox="0 0 20 20" fill="currentColor">
          <path d="M11.4 1.6a1 1 0 00-2.8 0L8.4 3.2a1 1 0 01-.7.5l-1.5.5a1 1 0 01-1-.2L3.8 2.8a1 1 0 00-1.4 1L3.6 5.2a1 1 0 01-.2 1l-.5 1.5a1 1 0 01-.5.7l-1.6.2a1 1 0 000 2.8l1.6.2a1 1 0 01.5.7l.5 1.5a1 1 0 01.2 1L2.4 16.2a1 1 0 001 1.4l1.4-1.2a1 1 0 011-.2l1.5.5a1 1 0 01.7.5l.2 1.6a1 1 0 002.8 0l.2-1.6a1 1 0 01.7-.5l1.5-.5a1 1 0 011 .2l1.4 1.2a1 1 0 001.4-1l-1.2-1.4a1 1 0 01-.2-1l.5-1.5a1 1 0 01.5-.7l1.6-.2a1 1 0 000-2.8l-1.6-.2a1 1 0 01-.5-.7l-.5-1.5a1 1 0 01.2-1l1.2-1.4a1 1 0 00-1-1.4l-1.4 1.2a1 1 0 01-1 .2l-1.5-.5a1 1 0 01-.7-.5l-.2-1.6z"/>
          <circle cx="10" cy="10" r="3" fill="var(--bg-secondary)"/>
        </svg>
        <span>Settings</span>
      </button>
```

Note: This uses a gear/cog SVG. The `fill` on the inner circle uses `var(--bg-secondary)` to punch out the center of the gear against the tab bar background. Unlike other tabs, this button uses `@click="showSettingsModal = true"` instead of setting `mobileTab`, and has no `:class="{ active: ... }"` binding since it opens a modal rather than switching views.

- [ ] **Step 2: Verify the server builds and starts**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds (the HTML is embedded at build time).

- [ ] **Step 3: Manual verification**

Open the app on a mobile viewport (or Chrome DevTools responsive mode, width < 1120px). Confirm:
1. Bottom tab bar shows 5 tabs: Sessions, Files, Issues, Tasks, Settings
2. All 5 tabs are evenly sized
3. Tapping Settings opens the settings modal
4. Settings modal works normally (accordion sections, save, cancel)
5. Other 4 tabs still work as before

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): add settings tab to mobile bottom nav bar"
```
