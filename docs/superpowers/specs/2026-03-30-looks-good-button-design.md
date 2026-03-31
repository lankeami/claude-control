# 👍 Looks Good Button — Design Spec

**Date:** 2026-03-30
**Issue:** #71

## Problem

Users frequently type "👍 Looks Good To Me" to approve Claude's output and keep generation moving. This is repetitive. A one-click button would save time.

## Design

Add a 👍 button to the left of the shell toggle button in the chat input bar. Clicking it immediately sends "👍 Looks Good To Me" as a message — no typing or Enter required.

### Behavior

- **Managed mode:** Calls `sendManagedMessage()` with "👍 Looks Good To Me"
- **Hook mode:** Calls `sendInstruction()` with "👍 Looks Good To Me"
- Uses the existing `handleInput()` flow: sets `inputText`, then calls `handleInput()`
- Disabled while `inputSending` is true (prevents double-sends)
- Does not interfere with any text already in the textarea (overwrites it)

### UI Placement

Inside `.instruction-bar`, before the shell toggle button:

```
[👍] [$] [textarea...] [Send]
```

- Visible in both managed and hook modes (no `x-show` mode filter)
- Styled similarly to `.shell-toggle-btn` but with emoji content instead of `$`

### Files Changed

1. `server/web/static/index.html` — Add button element
2. `server/web/static/style.css` — Add `.lgtm-btn` styles
3. `server/web/static/app.js` — Add `sendLgtm()` method

### Out of Scope

- Customizable message text
- Multiple quick-action buttons
- Keyboard shortcut for LGTM
