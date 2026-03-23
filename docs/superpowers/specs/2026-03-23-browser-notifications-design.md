# Browser Notifications for Session State Changes

**Date:** 2026-03-23
**Issue:** #38 — Browser notification for user notifications

## Problem

When working across multiple projects, there's no way to know when a background session finishes or needs input without manually checking each session. Browser notifications solve this by alerting the user when Claude completes work on a session they're not currently viewing.

## Approach

**Global events polling with state-change detection.** Monitor the existing `/api/events` SSE stream (which returns all sessions every 3s). Track previous `activity_state` per session. When a managed session transitions from `working` → `waiting` and is NOT the currently selected session, fire a browser notification with the session name and last assistant message.

## Design

### Notification Trigger

In the `startSSE()` method's `'update'` event listener in `app.js`, compare each session's current `activity_state` against its previous value stored in a `prevActivityStates` map. The same logic must also be wired into `pollState()` (the fallback when SSE degrades to polling after 3 failures).

**Trigger condition:** `previous === 'working'` AND `current === 'waiting'` AND `session.id !== selectedSessionId`

This fires only for background sessions transitioning to "waiting for input," avoiding noise when the user is already watching the session.

**Initial load:** On first update, `prevActivityStates` is empty so `prev` is `undefined`, not `'working'`. No false positives on page load.

### Notification Content

- **Title:** `sessionName(session)` — e.g., "my-project" for managed, "MacBook / my-project" for hook
- **Body:** Last assistant message text, truncated to 120 characters. Fetched via `GET /api/sessions/{id}/messages` using the `Authorization: Bearer` header (same auth pattern as `fetchManagedMessages`). The `content` field of assistant messages is a JSON string that must be parsed to extract the text. If no assistant message is found, fall back to "Claude is ready for your input"

### Permission Flow

Modern browsers require a user gesture to trigger `Notification.requestPermission()` — calling it on page load will silently fail.

Request permission on the user's first interaction with the app (e.g., when they first send a message or click a session). Check `Notification.permission`:
- `"granted"` — notifications work immediately
- `"default"` — call `Notification.requestPermission()` on next user interaction
- `"denied"` — no notifications, no error, graceful degradation

### State Tracking

Add `prevActivityStates: {}` to the Alpine.js app data object. Extract the state-change detection into a reusable method `checkActivityStateNotifications(sessions)` called from both `startSSE()` and `pollState()`:

```javascript
checkActivityStateNotifications(sessions) {
  for (const session of sessions) {
    const prev = this.prevActivityStates[session.id];
    const curr = session.activity_state;

    if (prev === 'working' && curr === 'waiting' && session.id !== this.selectedSessionId) {
      this.sendBrowserNotification(session);
    }

    this.prevActivityStates[session.id] = curr;
  }
}
```

### Notification Method

New method `sendBrowserNotification(session)`:

1. Check `Notification.permission === 'granted'`
2. Fetch `GET /api/sessions/{session.id}/messages` with `Authorization: Bearer` header
3. Find last message with `role === 'assistant'`, JSON-parse its `content` field, extract text
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
- `prevActivityStates` tracking in both SSE and polling code paths
- `checkActivityStateNotifications()` helper method
- `sendBrowserNotification()` method with proper auth and content parsing
- Notification permission request on first user interaction
- Click-to-focus behavior
- Notification auto-dismiss after 10s

### Out of Scope
- Notification preferences UI or toggle
- Sound/audio alerts
- Hook-mode session notifications (they don't have `activity_state` transitions)
- Notification grouping/batching
- Desktop notification icon customization
- Stale `prevActivityStates` cleanup (harmless, bounded by session count)

## Files to Modify

1. `server/web/static/app.js` — Add `prevActivityStates` data, `checkActivityStateNotifications()` in both `startSSE()` update listener and `pollState()`, `sendBrowserNotification()` method, permission request on first user interaction
