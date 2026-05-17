# Usage Rate Limit Widget — Design Spec

**Date:** 2026-05-16
**Status:** Approved

## Overview

Add a real-time Claude rate limit widget to the web UI's session header. The widget shows 5-hour and 7-day token utilization pulled from Anthropic's internal OAuth usage endpoint, giving users an always-visible view of their rate limit headroom without leaving the app.

## Background

Anthropic exposes usage data at `GET https://api.anthropic.com/api/oauth/usage` (beta header: `anthropic-beta: oauth-2025-04-20`). This is the same endpoint used by the macOS menu bar app [ccmeter](https://github.com/s-age/ccmeter). The response includes rolling 5-hour and 7-day utilization ratios plus reset timestamps.

The OAuth access token required for this endpoint is stored in the macOS Keychain by Claude Code under service name `"Claude Code-credentials"`.

## Goals

- Show 5hr and 7-day rate limit utilization in the session header at all times
- Zero-friction token discovery (read from Keychain automatically on macOS)
- Graceful degradation when the token is unavailable or the upstream call fails
- No new Go dependencies, no DB changes

## Architecture

Two components with a clean boundary:

1. **Backend** — `GET /api/usage` handler in `server/api/usage.go`
2. **Frontend** — compact usage bar in `main-header` in `server/web/static/`

---

## Backend

### Endpoint

```
GET /api/usage
Authorization: Bearer <api-key>   (existing auth middleware)
```

### Token Resolution (in priority order)

1. Run `security find-generic-password -a $USER -s "Claude Code-credentials" -w` (macOS Keychain)
2. Parse the JSON output, extract `.claudeAiOauth.accessToken`
3. If step 1 fails or returns empty, fall back to `os.Getenv("CLAUDE_OAUTH_TOKEN")`
4. If both are empty, return `503 Service Unavailable` with `{"error":"no_token"}`

### Upstream Call

```
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <resolved_token>
anthropic-beta: oauth-2025-04-20
```

Timeout: 10 seconds.

### Response

On success, return the upstream JSON body verbatim with `Content-Type: application/json`.

```json
{
  "five_hour":        { "utilization": 0.42, "resets_at": "2026-05-16T18:00:00.000Z" },
  "seven_day":        { "utilization": 0.15, "resets_at": "2026-05-23T00:00:00.000Z" },
  "seven_day_sonnet": { "utilization": 0.08, "resets_at": "2026-05-23T00:00:00.000Z" },
  "extra_usage": {
    "is_enabled": false,
    "monthly_limit": null,
    "used_credits": null,
    "utilization": null,
    "currency": null
  }
}
```

### Error Responses

| Condition | Status | Body |
|---|---|---|
| No token (Keychain miss + no env var) | 503 | `{"error":"no_token"}` |
| Upstream non-200 | 502 | `{"error":"upstream_error","status":<code>}` |
| Upstream network error / timeout | 502 | `{"error":"upstream_error"}` |

### Files Changed

- **Add:** `server/api/usage.go`
- **Edit:** `server/api/router.go` — register `GET /api/usage` on `apiMux`

---

## Frontend

### Widget Placement

Inside the existing `.main-header` div, to the right of the session name, left of `.turns-monitor`. Rendered unconditionally (not gated on session type), but hidden when data is unavailable.

### Visual Design

Two inline segments, each with a label, progress bar, and percentage:

```
5hr [████████░░] 80%    7d [██░░░░░░░░] 18%
```

- Bar width: 60px, height: 4px (matches existing turns bar style)
- Color thresholds:
  - < 70%: `#22c55e` (green)
  - 70–90%: `#f39c12` (amber)
  - > 90%: `#e74c3c` (red)
- Hover tooltip on each bar: `"Resets at <formatted local time>"`
- Both segments hidden independently if their data field is null/missing
- Entire widget hidden (no layout shift) when last fetch returned an error

### Polling

- `setInterval` at 60 seconds, added to the existing Alpine.js `init()` function
- First fetch on page load (no initial delay)
- Errors are swallowed silently — widget disappears, no user-facing message

### State

Two new Alpine.js data properties:
- `usageData` — the parsed JSON response (or `null`)
- `usageError` — boolean, true when last fetch failed

### Files Changed

- **Edit:** `server/web/static/index.html` — add usage bar markup in `.main-header`
- **Edit:** `server/web/static/app.js` — add `fetchUsage()`, polling interval, and `usageData`/`usageError` state

---

## Error Handling Summary

| Condition | Backend | Frontend |
|---|---|---|
| No token on macOS | 503 | Widget hidden |
| No token on Linux/Docker | 503 | Widget hidden |
| Anthropic API down | 502 | Widget hidden |
| Token expired / 401 from Anthropic | 502 | Widget hidden |
| Partial response (some fields null) | 200 | Missing bars omitted individually |
| Network timeout | 502 | Widget hidden |

## What Is NOT Changing

- No new Go module dependencies
- No SQLite schema changes
- No Settings UI changes (token is read automatically from Keychain or env var)
- No touch to `managed/`, `db/`, `tunnel/`, or `mcp/` packages
- No changes to Docker setup or existing auth middleware

## Configuration

For macOS: no setup required. Claude Code's Keychain entry is read automatically.

For Linux/Docker: set `CLAUDE_OAUTH_TOKEN=<token>` in the `.env` file. The token can be extracted from a macOS machine using:

```bash
security find-generic-password -a "$USER" -s "Claude Code-credentials" -w | python3 -c "import sys,json; print(json.load(sys.stdin)['claudeAiOauth']['accessToken'])"
```

## Success Criteria

- `curl -H "Authorization: Bearer <key>" localhost:8080/api/usage` returns a JSON response with `five_hour` key on macOS (no env var needed)
- Widget appears in session header within 1 second of page load
- Widget disappears cleanly (no broken UI) when token is not configured
- Color changes correctly at 70% and 90% thresholds
