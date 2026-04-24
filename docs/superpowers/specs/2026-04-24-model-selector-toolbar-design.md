# Model Selector Toolbar Relocation — Design Spec

**Date:** 2026-04-24
**Status:** Approved

## Overview

Move the model selection dropdown from its own row above the message input into the shortcut buttons toolbar below the input, styled to look like a native toolbar button rather than a form field.

## Problem

The current model `<select>` sits in its own `<div>` above the input textarea, consuming a full row of vertical space and visually disconnected from the other action buttons.

## Solution

Relocate the `<select>` into `.shortcut-buttons`, between the microphone (voice chat) button and the `⌘↵ to send` hint text. Restyle it to blend with the toolbar rather than appear as a standalone form control.

## Layout After Change

```
[ 😁 ] [ $ ] [ 🖼 ] [ 🎤 ] [ Sonnet ▾ ]   ⌘↵ to send
```

## HTML Changes

- **Remove** the standalone wrapper `<div>` (currently at lines 289–297 of `index.html`) that contains the `<select>`.
- **Add** the same `<select>` element into `.shortcut-buttons`, after the mic button and before the `.input-hint` span.
- Visibility condition remains unchanged: `x-show="currentSession?.mode === 'managed' && !shellMode"`.
- `x-model`, `@change` localStorage binding, and `<option>` list remain unchanged.

## Styling

Apply a new CSS class (e.g. `.model-select-btn`) to replace the current inline style:

- `background: transparent`
- `border: 1px solid var(--border)` — matches `.voice-chat-btn` and `.image-upload-btn`
- `color: var(--text-secondary)`
- `font-size: 0.75rem`
- `padding: 4px 6px`
- `border-radius: 6px`
- `cursor: pointer`
- `outline: none`
- On hover: `background: var(--bg-tertiary, #e5e7eb)` — matches `.shortcut-picker-btn:hover` pattern
- Retains native `<select>` caret (no custom arrow needed)

## Scope

- `server/web/static/index.html` — move element, add class
- `server/web/static/style.css` — add `.model-select-btn` rule

No JavaScript changes. No behavioral changes.
