# Browser Notifications for Session State Changes

**Date:** 2026-03-23
**Issue:** #38 — Browser notification for user notifications

## Problem

When working across multiple projects, there's no way to know when a background session finishes or needs input without manually checking each session. Browser notifications solve this by alerting the user when Claude completes work on a session they're not currently viewing.

## Approach

**Global events polling with state-change detection.** Monitor the existing `/api/events` SSE stream (which returns all sessions every 3s). Track previous `activity_state` per session. When a managed session transitions from `working` → `waiting` and is NOT the currently selected session, fire a browser notification with the session name and last assistant message.

## Design

### Notification Trigger

In the global events handler (`handleGlobalEvents()` in `app.js`), compare each session's current `activity_state` against its previous value stored in a `prevActivityStates` map.

**Trigger condition:** `previous === 'working'` AND `current === 'waiting'` AND `session.id !== selectedSessionId`

This fires only for background sessions transitioning to "waiting for input," avoiding noise when the user is already watching the session.

### Notification Content

- **Title:** `sessionName(session)` — e.g., "my-project" for managed, "MacBook / my-project" for hook
- **Body:** Last assistant message text, truncated to 120 characters. Fetched via `GET /api/sessions/{id}/messages`. If no assistant message is found, fall back to "Claude is ready for your input"

### Permission Flow

On page load, check `Notification.permission`:
- `"granted"` — notifications work immediately
- `"default"` — request permission on first page load via `Notification.requestPermission()`
- `"denied"` — no notifications, no error, graceful degradation

No custom UI for permission management. The browser's native permission prompt is sufficient.

### State Tracking

Add `prevActivityStates: {}` to the Alpine.js app data object. On each global events update:

```javascript
// In handleGlobalEvents, after updating sessions:
for (const session of sessions) {
  const prev = this.prevActivityStates[session.id];
  const curr = session.activity_state;

  if (prev === 'working' && curr === 'waiting' && session.id !== this.selectedSessionId) {
    this.sendBrowserNotification(session);
  }

  this.prevActivityStates[session.id] = curr;
}
```

### Notification Method

New method `sendBrowserNotification(session)`:

1. Check `Notification.permission === 'granted'`
2. Fetch `GET /api/sessions/{session.id}/messages?token=...`
3. Find last message with `role === 'assistant'`, extract text content
4. Truncate body to 120 chars
5. Create `new Notification(title, { body, tag: session.id })` — `tag` deduplicates if multiple fire for the same session
6. On click: `window.focus()`, call `selectSession(session.id)`
7. Auto-close after 10 seconds via `setTimeout(() => notification.close(), 10000)`

### Click Behavior

Clicking a notification focuses the browser window and switches to that session:

```javascript
notification.onclick = () => {
  window.focus();
  this.selectSession(session.id);
  notification.close();
};
```

## Scope

### In Scope
- `prevActivityStates` tracking in global events handler
- `sendBrowserNotification()` method
- Notification permission request on page load
- Click-to-focus behavior
- Notification auto-dismiss after 10s

### Out of Scope
- Notification preferences UI or toggle
- Sound/audio alerts
- Hook-mode session notifications (they don't have `activity_state` transitions)
- Notification grouping/batching
- Desktop notification icon customization

## Files to Modify

1. `server/web/static/app.js` — Add `prevActivityStates` data, state-change detection in global events handler, `sendBrowserNotification()` method, permission request on `init()`
