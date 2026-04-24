# Shortcut Drag-and-Drop Reorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add drag-and-drop reordering for emoji shortcuts in the settings modal, working on both desktop and mobile/touch.

**Architecture:** Pure frontend change across 3 files. Add CSS classes for drag handle and visual feedback, Alpine.js state and methods for drag/touch event handling, and HTML markup for the drag handle and event bindings. No backend changes — array order in `settingsForm.shortcuts` is already persisted as-is.

**Tech Stack:** Alpine.js, HTML5 Drag and Drop API, Touch Events API, vanilla CSS.

---

### File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `server/web/static/style.css` | Modify (add classes near line 2203) | `.shortcut-drag-handle`, `.shortcut-row`, `.shortcut-row.dragging`, `.shortcut-row.drag-over-above`, `.shortcut-row.drag-over-below` |
| `server/web/static/app.js` | Modify (add state at ~line 138, methods after existing shortcut functions) | `shortcutDragIdx`, `shortcutDragOverIdx` state; `shortcutDragStart`, `shortcutDragOver`, `shortcutDrop`, `shortcutDragEnd`, `shortcutTouchStart`, `shortcutTouchMove`, `shortcutTouchEnd`, `shortcutReorder` methods |
| `server/web/static/index.html` | Modify (lines 1442-1451) | Wrap shortcut rows in `.shortcut-row`, add drag handle, add event bindings |

---

### Task 1: Add CSS classes for drag handle and visual feedback

**Files:**
- Modify: `server/web/static/style.css:2203` (insert after the existing shortcut comment block intro)

- [ ] **Step 1: Add the shortcut reorder CSS classes**

Insert the following block just before the `/* Shortcut picker trigger button */` comment at line 2204 in `style.css`:

```css
/* Shortcut drag-and-drop reorder */
.shortcut-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.shortcut-drag-handle {
  cursor: grab;
  color: var(--text-muted);
  font-size: 16px;
  line-height: 1;
  user-select: none;
  -webkit-user-select: none;
  padding: 4px;
  flex-shrink: 0;
  touch-action: none;
}

.shortcut-drag-handle:hover {
  color: var(--text-primary);
}

.shortcut-row.dragging {
  opacity: 0.4;
}

.shortcut-row.drag-over-above {
  border-top: 2px solid var(--accent);
}

.shortcut-row.drag-over-below {
  border-bottom: 2px solid var(--accent);
}

@media (max-width: 600px) {
  .shortcut-drag-handle {
    padding: 10px;
    font-size: 18px;
  }
}
```

- [ ] **Step 2: Verify the CSS is valid**

Open the server and confirm the page loads without CSS errors. Run:

```bash
cd server && go run . &
```

Visit `http://localhost:8080`, open Settings > Shortcuts tab. The existing shortcuts should still render (no visual change yet since HTML doesn't use the classes).

- [ ] **Step 3: Commit**

```bash
git add server/web/static/style.css
git commit -m "style: add CSS classes for shortcut drag-and-drop reorder"
```

---

### Task 2: Add Alpine.js state and drag/touch/reorder methods

**Files:**
- Modify: `server/web/static/app.js:138` (add state properties)
- Modify: `server/web/static/app.js` (add methods after existing shortcut-related functions)

- [ ] **Step 1: Add drag state properties**

In `app.js`, find line 138 where `showShortcutPicker: false,` is defined. Add the two new state properties right after it:

```javascript
showShortcutPicker: false,
shortcutDragIdx: null,
shortcutDragOverIdx: null,
```

- [ ] **Step 2: Add the `shortcutReorder` method**

Find the `sendShortcut(value)` method in `app.js` (around line 1225). Add the following method right after it:

```javascript
shortcutReorder(fromIdx, toIdx) {
    if (fromIdx === toIdx || fromIdx === null || toIdx === null) return;
    const [item] = this.settingsForm.shortcuts.splice(fromIdx, 1);
    this.settingsForm.shortcuts.splice(toIdx, 0, item);
},
```

- [ ] **Step 3: Add the desktop drag event methods**

Add the following methods right after `shortcutReorder`:

```javascript
shortcutDragStart(event, idx) {
    if (!event.target.closest('.shortcut-drag-handle')) {
        event.preventDefault();
        return;
    }
    this.shortcutDragIdx = idx;
    event.dataTransfer.effectAllowed = 'move';
    event.target.closest('.shortcut-row').classList.add('dragging');
},

shortcutDragOver(event, idx) {
    if (this.shortcutDragIdx === null) return;
    event.preventDefault();
    // Clear previous indicators
    document.querySelectorAll('.shortcut-row').forEach(el => {
        el.classList.remove('drag-over-above', 'drag-over-below');
    });
    const row = event.target.closest('.shortcut-row');
    if (!row) return;
    const rect = row.getBoundingClientRect();
    const midY = rect.top + rect.height / 2;
    if (event.clientY < midY) {
        row.classList.add('drag-over-above');
        this.shortcutDragOverIdx = idx;
    } else {
        row.classList.add('drag-over-below');
        this.shortcutDragOverIdx = idx + 1;
    }
},

shortcutDrop(event) {
    event.preventDefault();
    this.shortcutReorder(this.shortcutDragIdx, this.shortcutDragOverIdx > this.shortcutDragIdx ? this.shortcutDragOverIdx - 1 : this.shortcutDragOverIdx);
    this.shortcutDragEnd();
},

shortcutDragEnd() {
    this.shortcutDragIdx = null;
    this.shortcutDragOverIdx = null;
    document.querySelectorAll('.shortcut-row').forEach(el => {
        el.classList.remove('dragging', 'drag-over-above', 'drag-over-below');
    });
},
```

- [ ] **Step 4: Add the touch event methods**

Add the following methods right after `shortcutDragEnd`:

```javascript
shortcutTouchStart(event, idx) {
    this.shortcutDragIdx = idx;
    event.target.closest('.shortcut-row').classList.add('dragging');
},

shortcutTouchMove(event, idx) {
    if (this.shortcutDragIdx === null) return;
    event.preventDefault();
    const touch = event.touches[0];
    // Clear previous indicators
    document.querySelectorAll('.shortcut-row').forEach(el => {
        el.classList.remove('drag-over-above', 'drag-over-below');
    });
    const el = document.elementFromPoint(touch.clientX, touch.clientY);
    if (!el) return;
    const row = el.closest('.shortcut-row');
    if (!row) return;
    const rowIdx = parseInt(row.dataset.idx, 10);
    if (isNaN(rowIdx)) return;
    const rect = row.getBoundingClientRect();
    const midY = rect.top + rect.height / 2;
    if (touch.clientY < midY) {
        row.classList.add('drag-over-above');
        this.shortcutDragOverIdx = rowIdx;
    } else {
        row.classList.add('drag-over-below');
        this.shortcutDragOverIdx = rowIdx + 1;
    }
},

shortcutTouchEnd() {
    if (this.shortcutDragIdx !== null && this.shortcutDragOverIdx !== null) {
        this.shortcutReorder(this.shortcutDragIdx, this.shortcutDragOverIdx > this.shortcutDragIdx ? this.shortcutDragOverIdx - 1 : this.shortcutDragOverIdx);
    }
    this.shortcutDragEnd();
},
```

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: add Alpine.js state and methods for shortcut drag reorder"
```

---

### Task 3: Update HTML to add drag handle and event bindings

**Files:**
- Modify: `server/web/static/index.html:1442-1451`

- [ ] **Step 1: Replace the shortcut row template**

In `index.html`, find the shortcuts `x-for` template block (lines 1442-1451). Replace it with:

```html
<template x-for="(shortcut, idx) in settingsForm.shortcuts" :key="idx">
  <div class="shortcut-row"
       :data-idx="idx"
       draggable="true"
       @dragstart="shortcutDragStart($event, idx)"
       @dragover="shortcutDragOver($event, idx)"
       @drop="shortcutDrop($event)"
       @dragend="shortcutDragEnd()">
    <span class="shortcut-drag-handle"
          @touchstart="shortcutTouchStart($event, idx)"
          @touchmove="shortcutTouchMove($event, idx)"
          @touchend="shortcutTouchEnd()">&#x2807;</span>
    <input x-model="shortcut.key" type="text" maxlength="20" placeholder="Key"
           class="modal-input" style="width:70px; flex-shrink:0; text-align:center;">
    <input x-model="shortcut.value" type="text" placeholder="Message to send..."
           class="modal-input" style="flex:1;">
    <button @click="settingsForm.shortcuts.splice(idx, 1)" class="btn btn-sm"
            style="padding:4px 8px; flex-shrink:0; color:var(--red);" title="Remove shortcut">&times;</button>
  </div>
</template>
```

Key changes from the original:
- Added `class="shortcut-row"` with `draggable="true"` and `:data-idx="idx"` on the row div
- Added drag event bindings (`@dragstart`, `@dragover`, `@drop`, `@dragend`) on the row
- Added `<span class="shortcut-drag-handle">` with touch events (`@touchstart`, `@touchmove`, `@touchend`) before the inputs
- The `&#x2807;` entity renders as a vertical three-dot grip icon (⠇). This is a compact, universally recognized drag affordance.

- [ ] **Step 2: Verify end-to-end on desktop**

Run the server and open Settings > Shortcuts:

```bash
cd server && go run .
```

1. Open `http://localhost:8080`, create a session, open Settings > Shortcuts tab
2. Verify drag handles appear to the left of each shortcut row
3. Drag a shortcut by its handle to a new position — confirm the row moves
4. Confirm the insertion indicator (accent border) appears during drag
5. Click Save — reopen settings and verify the new order persisted

- [ ] **Step 3: Verify change detection works**

1. Open Settings > Shortcuts
2. Drag a shortcut to a new position (don't save)
3. Confirm the save button shows unsaved changes (change count increments)
4. Click Save — confirm toast appears and changes persist

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat: add drag-and-drop reorder UI for shortcuts in settings modal"
```

---

### Task 4: Manual mobile/touch testing

**Files:** None (testing only)

- [ ] **Step 1: Test on mobile viewport**

Using browser DevTools, switch to a mobile viewport (e.g., iPhone 14 Pro, 393x852):

1. Open Settings > Shortcuts
2. Touch and hold the drag handle on a shortcut
3. Drag up/down — verify the insertion indicator appears on target rows
4. Release — verify the shortcut moves to the new position
5. Verify the text inputs are still tappable and editable (not intercepted by touch events)

- [ ] **Step 2: Test edge cases**

1. Drag a shortcut to the same position — verify nothing changes
2. Drag the first shortcut to the last position — verify correct placement
3. Drag the last shortcut to the first position — verify correct placement
4. Add a new shortcut, then drag it — verify it works on freshly added rows
5. Delete a shortcut after reordering — verify indices are correct

- [ ] **Step 3: Final commit (if any fixes needed)**

If any fixes were applied during testing:

```bash
git add -A
git commit -m "fix: address shortcut drag-and-drop edge cases"
```
