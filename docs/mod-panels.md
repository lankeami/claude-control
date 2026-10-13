# Mod Panels — Controller Web UI

The Controller web UI can display **mod panels** — lightweight data widgets that show mod state from Claude Code sessions. Panels appear in two locations:

- **Mod bar** — a compact row above the chat area showing key-value data (token counts, cost, status text)
- **Mod pane** — a collapsible side panel alongside the chat view for larger displays (agent trees, file lists, structured data)

## How It Works

```
Claude Code mod → $.state / hook-signal → POST /api/sessions/{id}/mod-state → SSE → Web UI
```

1. A Claude Code mod (or any script) POSTs panel data to the Controller's API
2. The server broadcasts a `mod_state` SSE event to the session's stream
3. The web UI routes the event to the correct panel slot (bar or pane)
4. Alpine.js renders the panel data as HTML

## API Reference

### `POST /api/sessions/{id}/mod-state`

Inject a mod panel into a managed session's web UI.

**Headers:**
```
Authorization: Bearer <api-key>
Content-Type: application/json
```

**Body:**
```json
{
  "panel_id": "unique-panel-id",
  "panel_title": "Display Title",
  "slot": "bar",
  "data": { "key": "value" }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `panel_id` | string | yes | Unique identifier for this panel. Repeat calls with the same ID update the panel. |
| `panel_title` | string | no | Display name shown in the panel header. Defaults to `panel_id`. |
| `slot` | string | no | `"bar"` (compact header row) or `"pane"` (side panel). Defaults to `"bar"`. |
| `data` | object/string/null | yes | Panel content. Objects render as key-value rows. `null` removes the panel. |

## Quick Start: Create a Panel from Scratch

### 1. Bar panel (compact, in the header)

```bash
# Token counter in the mod bar
curl -X POST http://localhost:8080/api/sessions/SESSION_ID/mod-state \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "panel_id": "tokens",
    "panel_title": "Tokens",
    "slot": "bar",
    "data": { "input": 1200, "output": 340, "cost": "$0.02" }
  }'
```

The mod bar appears above the chat area showing: **TOKENS** input: 1200 · output: 340 · cost: $0.02

### 2. Pane panel (side panel, alongside chat)

```bash
# Agent tree in the side pane
curl -X POST http://localhost:8080/api/sessions/SESSION_ID/mod-state \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "panel_id": "agent-tree",
    "panel_title": "Agent Tree",
    "slot": "pane",
    "data": {
      "root": "coordinator",
      "children": 3,
      "status": "running",
      "depth": 2
    }
  }'
```

When a pane-slot panel exists, a toggle button (▷) appears in the mod bar. Click it to open the side pane.

### 3. Update a panel

POST with the same `panel_id` to update. The web UI reflects changes immediately.

```bash
# Update token count
curl -X POST http://localhost:8080/api/sessions/SESSION_ID/mod-state \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "panel_id": "tokens",
    "panel_title": "Tokens",
    "slot": "bar",
    "data": { "input": 2400, "output": 680, "cost": "$0.04" }
  }'
```

### 4. Remove a panel

Send `"data": null` to remove a panel.

```bash
curl -X POST http://localhost:8080/api/sessions/SESSION_ID/mod-state \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "panel_id": "tokens", "data": null }'
```

## Importing an Existing Claude Code Mod

Claude Code mods use `$.state` atoms for reactive data and `ui.render` hooks for terminal/desktop UI. Since mod component trees are rendered in-process (not serialized), the Controller uses a **data bridge** — you forward the mod's state data to the Controller API, and the web UI renders its own HTML.

### Option A: Companion hook-signal (recommended for managed sessions)

For mods running in managed interactive sessions, add a `state.set` handler in the mod that forwards state via `hook-signal`:

```typescript
// In your mod's register function
on('turn.complete', async ($, e, next) => {
  const result = await next(e);

  // Forward your mod's state to the Controller
  const state = await read($, myAtom);
  try {
    await $.process.run({
      argv: ['curl', '-s', '-X', 'POST',
        `http://localhost:${process.env.PORT || 8080}/api/sessions/${process.env.SESSION_ID}/mod-state`,
        '-H', 'Authorization: Bearer ' + process.env.API_KEY,
        '-H', 'Content-Type: application/json',
        '-d', JSON.stringify({
          panel_id: 'my-mod',
          panel_title: 'My Mod',
          slot: 'bar',
          data: state
        })
      ]
    });
  } catch { /* non-critical */ }

  return result;
});
```

### Option B: External script polling mod state

If you can't modify the mod, write a script that reads the mod's output (from the transcript, a file, or stdout) and posts to the Controller:

```bash
#!/bin/bash
# poll-mod-state.sh — Reads a mod's state file and forwards to Controller
SESSION_ID="$1"
API_KEY="$2"
STATE_FILE="$3"

while true; do
  if [ -f "$STATE_FILE" ]; then
    DATA=$(cat "$STATE_FILE")
    curl -s -X POST "http://localhost:8080/api/sessions/$SESSION_ID/mod-state" \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -d "{\"panel_id\":\"external-mod\",\"panel_title\":\"External Mod\",\"slot\":\"bar\",\"data\":$DATA}"
  fi
  sleep 2
done
```

### Option C: Direct integration in a Claude Code hook

If the mod runs in a managed session with the Controller's generated settings file, the `hook-signal` subcommand is already available. Add a post-turn hook that forwards state:

```json
// In the session's settings file (generated by the Controller)
{
  "hooks": {
    "Stop": [{
      "command": "claude-controller hook-signal --event mod_state --session $SESSION_ID --data '{\"panel_id\":\"my-mod\",\"data\":{...}}'"
    }]
  }
}
```

> **Note:** Option C is a future enhancement — the `hook-signal` subcommand doesn't yet support a `mod_state` event type. Use Option A or B for now.

## Limitations

- **Data only** — the Controller renders its own HTML from the data you send; it does not render the mod's terminal/desktop component tree
- **No interactivity** — mod buttons, inputs, and select elements are not forwarded; the web UI displays read-only data
- **Object data renders as key-value rows** — nested objects are JSON-stringified; for better display, flatten your data before sending
- **Mobile** — the mod pane is hidden on narrow viewports (< 768px); bar panels remain visible and scroll horizontally

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Mod bar doesn't appear | No `bar`-slot panels sent | POST a panel with `"slot": "bar"` |
| Pane toggle not visible | No `pane`-slot panels exist | POST a panel with `"slot": "pane"` |
| Panel shows stale data | Panel not being updated | POST again with same `panel_id` and new `data` |
| 404 on mod-state endpoint | Session not found or not managed | Check `SESSION_ID` is a managed session |
| 400 on mod-state endpoint | Missing `panel_id` | Include `"panel_id"` in the request body |
