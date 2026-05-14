# Pixel Bar Horizontal Layout — Design Spec

**Issue:** #147 — Agent View Pixel Bar is too long
**Date:** 2026-05-14

## Problem

Each agent tool-call pixel in the Agent View Pixel Bar renders as a vertical stack (one per line), making the bar grow tall enough to block the chat area. This becomes a usability blocker on sessions with many tool calls.

Additionally, `clearAgentInvocations()` is never called when the user runs `/clear` or when a `/compact` completes, so pixels accumulate indefinitely within a session.

## Root Cause

The pixel bar container uses `x-show` to toggle visibility. Alpine.js `x-show` works by toggling `display: none !important`. When restoring visibility, some Alpine v2 builds restore the element's display to `block` rather than honouring the existing inline `display: flex` style, causing child pixels to stack vertically instead of flowing horizontally.

## Solution

### 1. Fix the horizontal layout (index.html)

Replace `x-show` on the pixel bar wrapper with `x-if` inside a `<template>` tag. `x-if` renders and removes the element from the DOM entirely — there is no `display: none` conflict, and `display: flex` on the inner div is always respected when the element is present.

**Before:**
```html
<div x-show="currentSession?.mode === 'managed' && agentInvocations.length > 0"
     style="display:flex; align-items:center; gap:3px; padding:4px 16px 6px; flex-wrap:wrap; border-bottom:1px solid var(--border);">
  ...pixels...
</div>
```

**After:**
```html
<template x-if="currentSession?.mode === 'managed' && agentInvocations.length > 0">
  <div style="display:flex; align-items:center; gap:3px; padding:4px 16px 6px; flex-wrap:wrap; border-bottom:1px solid var(--border);">
    ...pixels...
  </div>
</template>
```

The pixel dots and the "N tool calls" button remain unchanged inside the container.

### 2. Clear on `/clear` (app.js)

In the `/clear` slash-command handler, after `this.chatMessages = []`, add:

```js
this.clearAgentInvocations();
```

### 3. Clear on `compact_complete` (app.js)

In the SSE event handler for `data.type === 'compact_complete'`, after `this.isCompacting = false` and the system message push, add:

```js
this.clearAgentInvocations();
```

This gives the user visual confirmation that the context was compacted — the pixel history from the pre-compact turns is cleared, and a fresh bar starts for the new context window.

## Out of Scope

- No pixel cap / truncation (future issue if needed)
- No CSS file changes — layout is controlled inline
- No Go server changes
- No new Alpine components or stores

## Files Changed

| File | Change |
|------|--------|
| `server/web/static/index.html` | Wrap pixel bar in `<template x-if>` instead of `x-show` |
| `server/web/static/app.js` | Add `clearAgentInvocations()` in `/clear` handler and `compact_complete` SSE handler |

## Success Criteria

- Pixels render left-to-right, wrapping to the next row when the bar reaches container width
- Running `/clear` resets the pixel bar to empty
- A completed `/compact` resets the pixel bar to empty
- A session with hundreds of tool calls shows a compact multi-row pixel block, not a tall column
- `go build` passes with no errors
