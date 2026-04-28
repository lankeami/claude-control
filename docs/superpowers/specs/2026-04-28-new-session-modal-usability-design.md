# New Session Modal Usability — Design Spec

**Date:** 2026-04-28
**Issue:** #126
**Status:** Approved

## Overview

Three targeted usability fixes for the New Session modal:

1. **New folder input width** — move the "New Folder" section out of the constrained right column so its input matches the full width of the path input at the top of the modal.
2. **Navigate to new session on creation** — after creating a session via the "Open" button or "New Folder" flow, immediately navigate the user to the new session.
3. **Recursive directory search** — add a dedicated, debounced search input that finds directories recursively from the current browse path.

---

## Fix 1 — New Folder Input Width

### Problem

The "New Folder" expand/collapse section lives inside `modal-columns-right`, which is ~55% of the modal width. The path (CWD) input at the top of the modal spans the full width. The new folder name input therefore appears significantly narrower than the path input, making it feel cramped.

### Solution

Move the "New Folder" section (the `showNewFolderInput` toggle and expanded form) out of `modal-columns-right` into a new full-width strip between the `modal-columns` div and `modal-footer`. The strip gets the same horizontal padding as the path input row (`padding: 0 24px 12px`) and a top border matching the columns separator. The input inside uses `flex:1; min-width:0` to fill the available width. The Create and × buttons remain to the right of the input, unchanged.

No new CSS classes are needed — the new strip uses inline padding and the existing `modal-input` class on the input.

---

## Fix 2 — Navigate to New Session on Creation

### Problem

- `createManagedSession()` creates the session via API but never navigates to it. The user sees the modal close and the sidebar stay on whatever session was previously selected.
- `createNewProject()` calls `this.loadSessions()` (undefined function) then sets `this.selectedSessionId` directly (bypassing `selectSession`, so no messages are fetched).

### Solution

Both `createManagedSession()` and `createNewProject()` should follow the same pattern used by `selectRecentDir()`:

1. Parse the response JSON to obtain the session object.
2. Call `await this.pollState()` to refresh `this.sessions` (one fetch to `/api/sessions`; `pollState` is already defined).
3. Call `this.selectSession(sess.id)` — the session is now in the list, so full navigation runs (messages fetched, SSE started if applicable).

This ensures zero blank-panel edge cases and fixes the `loadSessions` undefined reference.

---

## Fix 3 — Recursive Directory Search

### Backend

**New endpoint:** `GET /api/browse/search?path=<base>&q=<query>`

- Registered in `server/api/router.go` alongside the existing `GET /api/browse` route.
- Handler `handleBrowseSearch` in `server/api/browse.go` (alongside the existing browse handler).
- Uses `filepath.Walk` starting from `path` (defaults to home directory if empty).
- Skips hidden directories (entries whose names start with `.`).
- Depth cap: 5 levels below the base path to bound worst-case walk time.
- Match criterion: directory name contains `q` (case-insensitive, `strings.Contains`).
- Returns up to 50 results in `[]BrowseEntry` format (same struct as `/api/browse` — `name`, `path`, `is_git_repo`).
- Requires the same `Authorization: Bearer` header as all other API routes.
- Empty `q` returns a 400 Bad Request.

**Response shape** (same as existing browse entries):
```json
{
  "entries": [
    { "name": "claude-control", "path": "/Users/jay/workspaces/_personal_/claude-control", "is_git_repo": true }
  ]
}
```

### Frontend

**State additions** (Alpine.js component):
- `dirSearch: ''` — current search query string.
- `dirSearchResults: []` — results from the last search API call.
- `dirSearchLoading: false` — true while the debounced search request is in flight.

**Search input:** Added above the directory list inside `modal-columns-right`, directly below the breadcrumbs. Placeholder: "Search folders…". Bound to `dirSearch` via `x-model`. An `@input` handler triggers the debounced search function.

**Debounce:** 300 ms. Implemented with a `clearTimeout` / `setTimeout` pattern stored in a closure variable (no extra library). When the query becomes empty, results are cleared and the normal browse view is restored.

**Results display:** While `dirSearch` is non-empty, the normal `filteredBrowseEntries` directory list is hidden and replaced by the search results list. Each result row shows:
- Directory name (bold, 13px) — top line.
- Full path abbreviated with `abbreviatePath()` — bottom line, muted.
- Git indicator (`git` badge in green) when `is_git_repo` is true.

Clicking a result calls `browseTo(entry.path)` (navigates into that directory) and clears the search query.

When `dirSearchLoading` is true, a "Searching…" placeholder replaces the list. When results are empty and not loading, shows "No matching directories".

**Interaction with `browseFilter`:** The existing `browseFilter` (set by typing in the CWD path input) continues to work unchanged when `dirSearch` is empty. When `dirSearch` is non-empty, the search results list fully replaces the `filteredBrowseEntries` list — no interaction between the two states.

**Modal reset:** `openNewSessionModal()` resets `dirSearch`, `dirSearchResults`, and `dirSearchLoading` to their defaults.

---

## Files Changed

| File | Change |
|------|--------|
| `server/api/browse.go` | Add `handleBrowseSearch` handler |
| `server/api/router.go` | Register `GET /api/browse/search` |
| `server/web/static/app.js` | Add search state + debounce logic; fix `createManagedSession` and `createNewProject` navigation |
| `server/web/static/index.html` | Move new-folder section; add search input + results list |

No new CSS classes required. No database changes. No new API authentication changes.

---

## Out of Scope

- Global (cross-all-directories) search — search is always relative to the current `browsePath`.
- Search result ranking — results are returned in `filepath.Walk` order (depth-first).
- Caching search results — each keystroke debounce fires a fresh request.
