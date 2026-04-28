# Status Line Design

## Overview

Add a configurable status line to the Claude Controller web UI, similar to the Claude Code CLI's status line. It displays session metadata (model, cost, context %, turns, activity state, working directory) in a persistent footer bar, with per-item visibility toggleable from the Settings modal.

## Requirements

- **Bottom bar**: Persistent footer pinned to the bottom of the `.main` area, below the chat input
- **Configurable**: Each item can be toggled on/off via a new "Status Line" tab in the Settings modal
- **Defaults**: Model and Cost enabled by default; all others off
- **Persistence**: Configuration stored in `localStorage` under key `statusLineConfig`
- **Managed sessions only**: Status line only visible when a managed session is selected (same as turns-monitor)

## Available Items

| Item | Key | Default | Data Source |
|------|-----|---------|-------------|
| Model | `model` | on | NDJSON `system/init` event — `{"type":"system","subtype":"init","model":"..."}` |
| Cost | `cost` | on | Existing `sessionCost` accumulator from `result` events |
| Context % | `context` | off | NDJSON `system` events if available; hidden if data not present |
| Turns | `turns` | off | Existing `turn_count / max_turns` from session object |
| Activity | `activity` | off | Existing `activity_state` from session object |
| Working Dir | `cwd` | off | Existing `cwd` from session object |

## Architecture

### Data Flow — Model Name

The Claude CLI NDJSON stream emits a `{"type":"system","subtype":"init"}` line at process start. This includes a `model` field. The frontend SSE handler already processes these NDJSON lines — we capture and store the model name in Alpine state as `sessionModel`. Reset to `null` on session switch.

### Data Flow — Context %

Claude CLI's NDJSON stream may include context window usage in `system` events. If the data is present, we parse and display it. If not, the item simply doesn't render (no error, no placeholder). No custom hook bridge for v1.

### Settings UI

New "Status Line" tab added to the Settings modal, positioned between "Shortcuts" and the divider. Contains labeled checkboxes for each of the 6 items. Changes apply immediately (no restart needed). The tab follows the existing settings pattern with `.settings-field` classes.

Default `statusLineConfig` value:
```json
{
  "model": true,
  "cost": true,
  "context": false,
  "turns": false,
  "activity": false,
  "cwd": false
}
```

Loaded from `localStorage` on app init. Written back on any toggle change.

### Status Line HTML

A `<div class="status-line">` element placed after the chat input area inside `.main`, containing a `<template x-for>` or individual `<span>` elements for each enabled item. Items are separated by `|` divider spans.

Structure:
```
┌─────────────────────────────────────────┐
│  Opus 4.6  |  $0.42                     │
└─────────────────────────────────────────┘
```

### CSS

- Pinned to bottom of `.main` via standard flow (not absolute/fixed positioning)
- Height: single line, ~32px with padding
- Font: `0.8rem`, color `var(--text-muted)` — matches `turns-monitor` styling
- Background: `var(--bg)` with top border `1px solid var(--border)`
- Items: `display: flex; align-items: center; gap: 8px;`
- Dividers: `color: var(--border)` pipe characters
- Responsive: on small screens, items wrap naturally; abbreviated labels if needed
- Hidden when no managed session is selected or all items are toggled off

### Alpine State

New state properties in the main `Alpine.data` object:

- `sessionModel: null` — string, set from NDJSON `system/init`
- `statusLineConfig: { model: true, cost: true, context: false, turns: false, activity: false, cwd: false }` — loaded from localStorage on init

New methods:

- `initStatusLineConfig()` — reads from localStorage, merges with defaults
- `toggleStatusLineItem(key)` — flips the boolean and persists to localStorage
- `statusLineVisible()` — returns true if managed session selected AND at least one item is enabled

### NDJSON Parsing Change

In the existing SSE message handler (app.js ~line 1860-1970), add a check for `system/init` events:

```javascript
if (data.type === 'system' && data.subtype === 'init' && data.model) {
  this.sessionModel = data.model;
}
```

## Scope

### In Scope
- Status line footer bar with 6 configurable items
- Settings modal tab with checkboxes
- localStorage persistence
- Model name extraction from NDJSON stream

### Out of Scope
- Hook mode sessions (no NDJSON stream available)
- Custom hook bridge for context window data
- Drag-and-drop reordering of items
- Server-side persistence of status line config
- Context % if Claude CLI doesn't emit it in NDJSON

## Files to Modify

1. **`server/web/static/index.html`** — Add status line HTML after chat input, add Settings tab
2. **`server/web/static/app.js`** — Add state, methods, NDJSON parsing for model
3. **`server/web/static/style.css`** — Add `.status-line` styles
