# Shortcut Drag-and-Drop Reorder Design

## Overview

Add drag-and-drop reordering for emoji shortcuts in the settings modal. Users can grab a drag handle on each shortcut row and drag it to a new position. Works on both desktop (HTML5 Drag and Drop API) and mobile/touch devices (touch events).

## Current State

Shortcuts are stored as a JSON array in `shortcuts.json`. The array order determines display order in both the settings modal and the shortcut picker popup. Each shortcut has `key` (emoji/text) and `value` (message) fields. There is no explicit ordering field — position is implicit from array index.

The settings modal shortcuts tab renders each shortcut as a row with a key input, value input, and delete button. There is no reordering UI.

## Design

### UI Layout

Each shortcut row gains a drag handle on the left side:

```
[drag-handle] [key input] [value input] [x delete]
```

The drag handle displays a six-dot grip icon (Unicode ⠿ or CSS-rendered dots) and serves as the exclusive drag initiation target. This prevents accidental drags when interacting with the text inputs.

Handle sizing: 24x24px visible area with 44px minimum touch target on mobile.

### Desktop: HTML5 Drag and Drop

Each shortcut row container gets `draggable="true"`. The `@dragstart` handler checks `event.target.closest('.shortcut-drag-handle')` — if the drag didn't originate from the handle, it calls `event.preventDefault()` to cancel. This prevents accidental drags when clicking inputs or the delete button.

**Events on each row:**

- `@dragstart` — Stores the dragged index in Alpine state (`shortcutDragIdx`), sets `dataTransfer.effectAllowed = 'move'`, adds a `.dragging` class (opacity 0.4) to the row.
- `@dragover.prevent` — Determines the target index from the row being hovered. Sets `shortcutDragOverIdx` to show an insertion indicator.
- `@dragend` — Clears `shortcutDragIdx` and `shortcutDragOverIdx`, removes visual classes.
- `@drop` — Calls `shortcutReorder(fromIdx, toIdx)` to splice the shortcuts array.

### Mobile: Touch Events

HTML5 Drag and Drop does not work on mobile browsers. Touch equivalents are added to the drag handle:

- `@touchstart` — Records starting index and Y position, adds `.dragging` class.
- `@touchmove.prevent` — Uses `document.elementFromPoint()` on touch coordinates to determine which row the finger is over. Updates `shortcutDragOverIdx` for the insertion indicator. The `.prevent` modifier stops page scrolling during the drag.
- `@touchend` — Calls `shortcutReorder(fromIdx, toIdx)`, clears state.

### Reorder Logic

A single `shortcutReorder(fromIdx, toIdx)` method handles the array manipulation for both desktop and touch:

```javascript
shortcutReorder(fromIdx, toIdx) {
    if (fromIdx === toIdx) return;
    const [item] = this.settingsForm.shortcuts.splice(fromIdx, 1);
    this.settingsForm.shortcuts.splice(toIdx, 0, item);
}
```

This mutates `settingsForm.shortcuts` in place, which Alpine.js reactivity picks up. The existing save flow (`PUT /api/settings`) already persists the full shortcuts array in order, so no backend changes are needed.

### Alpine.js State Additions

Two new properties on the app data object:

- `shortcutDragIdx: null` — Index of the row currently being dragged.
- `shortcutDragOverIdx: null` — Index of the current drop target (for the insertion indicator).

### Visual Feedback

- **Drag handle idle:** `color: var(--text-muted)`, `cursor: grab`.
- **Drag handle hover:** `color: var(--text-primary)`.
- **Dragging row:** `opacity: 0.4` via `.shortcut-row.dragging` class.
- **Drop target indicator:** `2px solid var(--accent)` border on top or bottom edge of the target row, depending on whether the dragged item would land above or below. Applied via `.shortcut-row.drag-over-above` (border-top) and `.shortcut-row.drag-over-below` (border-bottom) classes.

No animations or transitions — just immediate visual state changes for simplicity.

### CSS Classes

New classes added to `style.css`:

- `.shortcut-drag-handle` — Grip icon styling, cursor, user-select, touch target padding.
- `.shortcut-row` — Wrapper class for each shortcut (replaces the current inline `div` style).
- `.shortcut-row.dragging` — Reduced opacity during drag.
- `.shortcut-row.drag-over-above` — Top border accent line.
- `.shortcut-row.drag-over-below` — Bottom border accent line.

## Files Changed

All edits to existing files — no new files created.

| File | Changes |
|------|---------|
| `server/web/static/index.html` | Add drag handle element to each shortcut row. Add drag/touch event bindings. Wrap each row in a `.shortcut-row` container. |
| `server/web/static/app.js` | Add `shortcutDragIdx`, `shortcutDragOverIdx` state. Add methods: `shortcutDragStart`, `shortcutDragOver`, `shortcutDrop`, `shortcutDragEnd`, `shortcutTouchStart`, `shortcutTouchMove`, `shortcutTouchEnd`, `shortcutReorder`. |
| `server/web/static/style.css` | Add `.shortcut-drag-handle`, `.shortcut-row`, `.shortcut-row.dragging`, `.shortcut-row.drag-over-above`, `.shortcut-row.drag-over-below` classes. |

## No Backend Changes

The existing `PUT /api/settings` endpoint persists the shortcuts array in the order received. The `Shortcut` struct does not need an ordering field — array position is the order. No new API endpoints, no database changes, no Go code changes.

## Change Detection

The existing `settingsChangeCount()` method already compares `JSON.stringify(settingsForm.shortcuts)` against the original, so reordering shortcuts will correctly flag unsaved changes.
