# Model Selector Toolbar Relocation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the model `<select>` from its own row above the message input into the shortcut buttons toolbar, styled as a compact button-like control.

**Architecture:** Pure HTML + CSS change — remove the standalone wrapper div, relocate the `<select>` into `.shortcut-buttons` between the mic button and the hint text, and add a `.model-select-btn` CSS class that makes it blend with the toolbar.

**Tech Stack:** HTML (Alpine.js x-model bindings), CSS custom properties

---

## File Map

| File | Change |
|------|--------|
| `server/web/static/index.html` | Remove standalone model `<div>` (lines 289–297); add `<select>` into `.shortcut-buttons` after mic button |
| `server/web/static/style.css` | Add `.model-select-btn` rule after `.voice-chat-btn` block |

---

### Task 1: Add `.model-select-btn` CSS rule

**Files:**
- Modify: `server/web/static/style.css` (after line ~2433, end of `.voice-chat-btn` block)

- [ ] **Step 1: Add the CSS rule**

In `server/web/static/style.css`, after the `.voice-chat-btn.active.listening` / `@keyframes voice-pulse` block (around line 2433), add:

```css
/* Model selector styled as toolbar button */
.model-select-btn {
  background: transparent;
  border: none;
  color: var(--text-secondary, #6b7280);
  font-size: 0.75rem;
  padding: 4px 6px;
  border-radius: 6px;
  cursor: pointer;
  outline: none;
  flex-shrink: 0;
}
.model-select-btn:hover {
  background: var(--bg-tertiary, #e5e7eb);
}
```

- [ ] **Step 2: Verify CSS file saves cleanly** — open `server/web/static/style.css` and confirm the new rule appears without syntax errors (no mismatched braces).

---

### Task 2: Relocate the model `<select>` in index.html

**Files:**
- Modify: `server/web/static/index.html` lines 289–297 (remove), line 368 (insert after)

- [ ] **Step 1: Remove the standalone model wrapper div**

In `server/web/static/index.html`, remove these lines (289–297):

```html
              <div x-show="currentSession?.mode === 'managed' && !shellMode" x-cloak style="display:flex; align-items:center; padding:0 0 6px 0;">
                <select x-model="selectedModel" @change="localStorage.setItem('claude-controller-model', selectedModel)"
                        aria-label="Model"
                        style="background:var(--bg-secondary); color:var(--text-primary); border:1px solid var(--border); border-radius:6px; padding:3px 8px; font-size:0.75rem; cursor:pointer; outline:none;">
                  <option value="claude-opus-4-6">Opus</option>
                  <option value="claude-sonnet-4-6">Sonnet</option>
                  <option value="claude-haiku-4-5-20251001">Haiku</option>
                </select>
              </div>
```

- [ ] **Step 2: Insert the `<select>` into `.shortcut-buttons` after the mic button**

After the closing `</button>` of the `.voice-chat-btn` block (currently line 368, just before `<span class="input-hint">`), insert:

```html
                <select x-model="selectedModel"
                        x-show="currentSession?.mode === 'managed' && !shellMode"
                        x-cloak
                        @change="localStorage.setItem('claude-controller-model', selectedModel)"
                        aria-label="Model"
                        class="model-select-btn">
                  <option value="claude-opus-4-6">Opus</option>
                  <option value="claude-sonnet-4-6">Sonnet</option>
                  <option value="claude-haiku-4-5-20251001">Haiku</option>
                </select>
```

The resulting `.shortcut-buttons` block should read:

```
[ shortcut picker ] [ $ shell ] [ image upload ] [ mic ] [ model select ] [ ⌘↵ hint ]
```

- [ ] **Step 3: Commit**

```bash
git checkout -b feat/model-selector-toolbar
git add server/web/static/index.html server/web/static/style.css
git commit -m "feat: move model selector into shortcut toolbar as compact button"
```

---

### Task 3: Visual verification

**Files:** None (read-only verification)

- [ ] **Step 1: Start the server**

```bash
cd server && go run .
```

Expected output: server starts on `:8080` with no errors.

- [ ] **Step 2: Open the UI and verify layout**

Open `http://localhost:8080` in a browser. Start or select a managed session. Confirm:
1. No model dropdown appears above the message input (old row is gone).
2. In the shortcut toolbar: `😁 | $ | 🖼 | 🎤 | [Sonnet ▾] | ⌘↵ to send`
3. The model selector blends visually — no visible border, transparent background.
4. Hovering the selector shows a subtle background highlight.
5. Changing the model still persists to `localStorage` (reload and confirm the selection is remembered).
6. In hook-mode sessions or shell mode, the selector is hidden (matches old behavior).
