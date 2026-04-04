# Shortcut Creator Design Spec

**Date:** 2026-04-03
**Issue:** #100

## Overview

Configurable shortcuts that map short keys (emoji, abbreviations) to full text messages. Users define shortcuts in settings; a picker in the chat input lets them quickly send shortcut values. Typing a shortcut key directly and sending also resolves to the full value.

## Data Model

Shortcuts are stored as a JSON array in a `shortcuts.json` file alongside the server's `.env` file, exposed through the existing `/api/settings` endpoint:

```json
{
  "port": "8080",
  "shortcuts": [
    { "key": "👍", "value": "👍 Looks Good To Me" },
    { "key": "🚀", "value": "Ship it! Merge and deploy." }
  ]
}
```

- **key**: any string, max 20 characters. Displayed on picker buttons and used as the match trigger.
- **value**: the text actually sent to chat. No length limit.
- **Default**: `[{ "key": "👍", "value": "👍 Looks Good To Me" }]` — ships pre-configured, editable/removable by user.

Shortcuts are global (not per-session) and stored server-side.

## Settings Modal — Accordion Refactor

The settings modal is refactored into collapsible accordion sections:

1. **Server Configuration** (open by default) — port, ngrok authtoken, claude bin, claude args, claude env, compact interval, github token
2. **Shortcuts** (collapsed by default) — the shortcut editor

Each section has a clickable header with a chevron indicator (▶ collapsed, ▼ open).

## Settings Modal — Shortcuts Section

The "Shortcuts" accordion section contains:

- A list of shortcut rows, each with:
  - **Key** input: text, max 20 chars, narrow width (~80px)
  - **Value** input: text, wider, the message to send
  - **Delete** button: × icon, removes the row
- An **"+ Add Shortcut"** button at the bottom to append a new empty row

## Chat Input — Shortcut Picker

The hardcoded 👍 LGTM button is removed. In its place:

- A **😁 trigger button** in the `.shortcut-buttons` row
- Clicking 😁 opens a **popup picker** positioned above the button
- The picker displays all configured shortcuts as clickable items
- Clicking a shortcut sends the message immediately and closes the picker
- Clicking outside the picker closes it
- If no shortcuts are configured, the picker shows: "No shortcuts configured — add them in Settings"

## Message Send — Shortcut Resolution

On every message send:

1. Trim the message text
2. Check if the trimmed text exactly matches any shortcut **key**
3. If match found: replace the message with the shortcut's **value** before sending
4. If no match: send the message as-is

## Removed

- The hardcoded `sendLgtm()` method
- The `.lgtm-btn` element and its CSS
