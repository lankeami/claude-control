# Pixel Bar Horizontal Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the Agent View pixel bar so pixels render horizontally with word-wrap, and clear the bar when `/clear` or `/compact` completes.

**Architecture:** Two files change. `index.html` gets the pixel bar container swapped from `x-show` to `x-if` (eliminates Alpine's `display:none` conflict that causes vertical stacking). `app.js` gets `clearAgentInvocations()` wired into the `/clear` slash-command handler and the `compact_complete` SSE event handler.

**Tech Stack:** Alpine.js v2 (browser, no build step), Go (server embeds static files)

---

## File Map

| File | Change |
|------|--------|
| `server/web/static/index.html` | Lines 194–208 — wrap pixel bar in `<template x-if>` instead of `x-show` on outer div |
| `server/web/static/app.js` | Line 1091 — add `clearAgentInvocations()` after `chatMessages = []` in `/clear` handler |
| `server/web/static/app.js` | Line 2030 — add `clearAgentInvocations()` after `scrollToBottom` in `compact_complete` handler |

---

### Task 1: Create feature branch

**Files:**
- No file changes — branch setup only

- [ ] **Step 1: Create and switch to feature branch**

```bash
git checkout -b feat/pixel-bar-horizontal
```

Expected output: `Switched to a new branch 'feat/pixel-bar-horizontal'`

---

### Task 2: Fix pixel bar layout — replace `x-show` with `x-if`

**Files:**
- Modify: `server/web/static/index.html:194-208`

The root cause of vertical stacking: Alpine v2's `x-show` restores `display: block` when un-hiding an element, overriding the inline `display: flex`. Replacing with `x-if` removes/re-creates the element entirely — no display-mode conflict.

- [ ] **Step 1: Replace the pixel bar wrapper in index.html**

Find lines 194–208 (the `<!-- Agent View Pixel Bar -->` block). Replace:

```html
        <!-- Agent View Pixel Bar (managed sessions only) -->
        <div x-show="currentSession?.mode === 'managed' && agentInvocations.length > 0"
             style="display:flex; align-items:center; gap:3px; padding:4px 16px 6px; flex-wrap:wrap; border-bottom:1px solid var(--border);">
          <template x-for="(inv, i) in agentInvocations" :key="i">
            <div @click="agentViewOpen = true"
                 :title="inv.label + ' · ' + inv.status + (inv.duration ? ' · ' + inv.duration : '')"
                 class="agent-pixel"
                 :class="{ 'agent-pixel-running': inv.status === 'running' }"
                 :style="'background:' + (inv.status === 'running' ? '#f39c12' : '#22c55e')">
            </div>
          </template>
          <button @click="agentViewOpen = true"
                  style="margin-left:6px; font-size:11px; color:var(--text-muted); background:none; border:none; cursor:pointer; padding:0; text-decoration:underline; flex-shrink:0;"
                  x-text="agentInvocations.length + (agentInvocations.length === 1 ? ' tool call' : ' tool calls')">
          </button>
        </div>
```

With:

```html
        <!-- Agent View Pixel Bar (managed sessions only) -->
        <template x-if="currentSession?.mode === 'managed' && agentInvocations.length > 0">
          <div style="display:flex; align-items:center; gap:3px; padding:4px 16px 6px; flex-wrap:wrap; border-bottom:1px solid var(--border);">
            <template x-for="(inv, i) in agentInvocations" :key="i">
              <div @click="agentViewOpen = true"
                   :title="inv.label + ' · ' + inv.status + (inv.duration ? ' · ' + inv.duration : '')"
                   class="agent-pixel"
                   :class="{ 'agent-pixel-running': inv.status === 'running' }"
                   :style="'background:' + (inv.status === 'running' ? '#f39c12' : '#22c55e')">
              </div>
            </template>
            <button @click="agentViewOpen = true"
                    style="margin-left:6px; font-size:11px; color:var(--text-muted); background:none; border:none; cursor:pointer; padding:0; text-decoration:underline; flex-shrink:0;"
                    x-text="agentInvocations.length + (agentInvocations.length === 1 ? ' tool call' : ' tool calls')">
            </button>
          </div>
        </template>
```

- [ ] **Step 2: Verify build passes**

```bash
cd server && go build -o claude-controller .
```

Expected: exits 0, no output. (The Go build embeds the static files — if index.html has a syntax error that prevents embedding, this will catch it.)

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "fix(ui): use x-if on pixel bar to restore horizontal flex layout"
```

---

### Task 3: Clear pixel bar on `/clear`

**Files:**
- Modify: `server/web/static/app.js:1091`

- [ ] **Step 1: Add `clearAgentInvocations()` to the `/clear` success branch**

Find the `/clear` case handler around line 1081. The `else` branch currently reads:

```js
            } else {
              this.chatMessages = [];
            }
```

Change it to:

```js
            } else {
              this.chatMessages = [];
              this.clearAgentInvocations();
            }
```

- [ ] **Step 2: Verify build passes**

```bash
cd server && go build -o claude-controller .
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "fix(ui): clear agent pixel bar when /clear is run"
```

---

### Task 4: Clear pixel bar on `compact_complete`

**Files:**
- Modify: `server/web/static/app.js:2030`

- [ ] **Step 1: Add `clearAgentInvocations()` to the `compact_complete` SSE handler**

Find the `compact_complete` block around line 2022. It currently reads:

```js
          if (data.type === 'compact_complete') {
            this.isCompacting = false;
            this.chatMessages.push({
              id: 'compact-done-' + Date.now(),
              role: 'system',
              content: 'Compact complete.',
              isAutoContinue: true
            });
            this.$nextTick(() => this.scrollToBottom(true));
            return;
          }
```

Change it to:

```js
          if (data.type === 'compact_complete') {
            this.isCompacting = false;
            this.clearAgentInvocations();
            this.chatMessages.push({
              id: 'compact-done-' + Date.now(),
              role: 'system',
              content: 'Compact complete.',
              isAutoContinue: true
            });
            this.$nextTick(() => this.scrollToBottom(true));
            return;
          }
```

(`clearAgentInvocations()` placed before the push so the bar resets at the same moment the "Compact complete." message appears.)

- [ ] **Step 2: Verify build passes**

```bash
cd server && go build -o claude-controller .
```

Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "fix(ui): clear agent pixel bar when compact completes"
```

---

### Task 5: Visual verification

**Files:**
- No changes — verification only

- [ ] **Step 1: Start the server**

```bash
cd server && go run . --port 3001
```

Expected: server starts on port 3001 without errors. (Use 3001 to avoid conflicts with any existing process on 3000.)

- [ ] **Step 2: Take a screenshot of a managed session with tool calls**

Open `http://localhost:3001` in a browser. Start or select a managed session that has run tool calls (so `agentInvocations.length > 0`). Confirm:
- Pixel dots appear left-to-right in a horizontal row
- When there are many dots, they wrap to a second row rather than growing into a tall column

- [ ] **Step 3: Verify `/clear` resets the bar**

In the chat input, type `/clear` and send. Confirm the pixel bar disappears (because `agentInvocations` is now `[]`).

- [ ] **Step 4: Stop the server**

`Ctrl+C` the running process.

---

### Task 6: Open draft PR

**Files:**
- No changes — PR creation only

- [ ] **Step 1: Push branch and open draft PR**

```bash
git push -u origin feat/pixel-bar-horizontal
gh pr create --draft --title "fix(ui): horizontal pixel bar with clear on /clear and compact" --body "$(cat <<'EOF'
## Summary

Fixes #147 — Agent View pixel bar was stacking pixels vertically, making it grow tall enough to block the chat area.

- Replace `x-show` with `x-if` on pixel bar container: eliminates Alpine v2 conflict where `x-show` restores `display:block`, overriding the inline `display:flex`
- Clear pixel bar when `/clear` succeeds
- Clear pixel bar when `compact_complete` SSE event fires

## Test plan

- [ ] Start a managed session, run several tool-heavy prompts, confirm pixels render left-to-right with wrap
- [ ] Run `/clear` — confirm pixel bar resets to empty
- [ ] Trigger a `/compact` — confirm pixel bar clears when "Compact complete." system message appears
- [ ] `go build` passes cleanly
EOF
)"
```

Return the PR URL to the user.
