# Directory Search UX — Design Spec

**Date:** 2026-04-28
**Status:** Approved

## Overview

Improve the directory/project search experience in the new-session modal. Three changes: trigger search on button click instead of as-you-type, sort results shallower-first, and clarify that only directories whose own name matches the query are included (already the case in code, now made explicit in design).

## Problem

1. Search fires on every keystroke (with a 300 ms debounce), which is unnecessary for a filesystem walk and can feel janky.
2. Results are returned in walk order (depth-first), so a deeply-nested exact-match can appear before a top-level match.
3. The relevance rule (only a dir whose **name** contains the query is included — not its ancestors or descendants) was implicit; making it explicit helps reviewers and testers.

## Approach

Option A: button-triggered search + backend depth sort. No as-you-type debounce. Simple, focused.

## Design

### 1. UI (`server/web/static/index.html` + `app.js`)

Replace the single search input with a flex row: input + 🔍 button.

```html
<div style="display:flex; gap:6px;">
  <input x-model="dirSearch" type="text" placeholder="Search folders…"
         class="modal-input" style="flex:1;"
         @keydown.enter.prevent="triggerDirSearch()">
  <button class="btn btn-sm" @click="triggerDirSearch()">🔍</button>
</div>
```

- `@input="onDirSearchInput()"` is removed entirely — no as-you-type behavior.
- Enter key on the input and clicking 🔍 both call `triggerDirSearch()`.
- Clearing the input field (empty query) immediately clears results and hides the results panel (handled inside `triggerDirSearch()` with an early return).

`onDirSearchInput()` in `app.js` is renamed `triggerDirSearch()`. The debounce timer (`_dirSearchTimer`, `setTimeout`) is removed — the function fires the API call directly and synchronously sets `dirSearchLoading = true`.

State fields `dirSearch`, `dirSearchResults`, `dirSearchLoading`, and `_dirSearchTimer` remain. `_dirSearchTimer` can be removed since it is no longer used.

### 2. Backend sort (`server/api/browse.go`)

After the walk collects results, sort by two keys:

1. **Depth** (primary) — `strings.Count(path, string(filepath.Separator))` ascending. Shallower paths come first.
2. **Name** (tiebreaker) — case-insensitive alphabetical within the same depth.

```go
sort.Slice(results, func(i, j int) bool {
    sep := string(filepath.Separator)
    di := strings.Count(results[i].Path, sep)
    dj := strings.Count(results[j].Path, sep)
    if di != dj {
        return di < dj
    }
    return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
})
```

The walk and filtering logic are unchanged: only a directory whose **name** (the final path component) contains the query string (case-insensitive) is included. Parent directories that don't match are traversed but not emitted.

**Example** — query `diffintegrator` starting from `~`:
- `~/workspaces/diffintegrator` — depth 3, name matches → included, appears first
- `~/workspaces/diffintegrator/deploy` — depth 4, name `deploy` does not match → excluded
- `~/workspaces/diffintegrator/deploy/diffintegrator` — depth 5, name matches → included, appears second
- Result order: `~/workspaces/diffintegrator`, then `~/workspaces/diffintegrator/deploy/diffintegrator`

### 3. Files changed

| File | Change |
|------|--------|
| `server/web/static/index.html` | Replace search input div with flex row + 🔍 button; remove `@input` handler |
| `server/web/static/app.js` | Rename `onDirSearchInput` → `triggerDirSearch`; remove debounce timer; remove `_dirSearchTimer` state field |
| `server/api/browse.go` | Add `sort.Slice` after walk loop to sort results by depth then name |
| `server/api/browse_test.go` | Update/add tests to assert depth-first ordering |

## Testing

- Search `diffintegrator` from home: verify `~/workspaces/diffintegrator` appears before `~/workspaces/diffintegrator/deploy/diffintegrator`, and `~/workspaces/diffintegrator/deploy` does not appear.
- Verify pressing Enter triggers the search.
- Verify clicking 🔍 triggers the search.
- Verify clearing the input and clicking 🔍 clears results.
- Verify no network request fires while typing (only on button click / Enter).
