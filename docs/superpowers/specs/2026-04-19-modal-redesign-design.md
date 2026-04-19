# Modal Redesign Design Spec

**Date:** 2026-04-19
**Issue:** #119 — New Modal Experience
**Scope:** Design-only changes. No functional changes to modal behavior or data flow.

## Problem

The current modal system has several UX issues:

1. **Too narrow** — All modals use a fixed 500px width, cramping content-heavy modals like New Session and Settings
2. **Cluttered** — New Session modal packs path input, recent dirs, breadcrumbs, directory browser, folder creation, and action buttons into a single column at 70vh max-height
3. **Folder creation dominates** — The "Create new project" section takes significant space despite being rarely used
4. **Settings are hard to read** — Accordion-based layout in a small box makes it hard to see all config at once; saving is unclear about what changed
5. **Inline styles everywhere** — All 5 modals use inline styles, making them inconsistent and hard to maintain
6. **Mobile is a hack** — Mobile overrides force 100vw/100dvh with `!important` rather than responsive design

## Design: Wide Centered Modal

### Unified Modal CSS System

Replace all inline modal styles with shared CSS classes. All modals use a consistent structure:

**Shared traits (all sizes):**
- Centered on screen with dark backdrop (`rgba(0,0,0,0.5)`)
- Click backdrop or press Escape to close
- `border-radius: 16px` (up from 12px)
- Consistent padding: 24px desktop, 16px mobile
- `max-height: 85vh` with internal scroll
- `box-shadow: 0 24px 80px rgba(0,0,0,0.4)`
- Mobile: all sizes become full-screen with sticky header
- X close button in top-right corner (standard pattern)

**CSS classes replacing inline styles:**
- `.modal-backdrop` — fixed overlay with centering flexbox
- `.modal-sm` — 480px width (Permission Prompt, Resume Picker)
- `.modal-md` — 560px width (Task Editor)
- `.modal-lg` — 640px width (New Session, Settings)
- `.modal-header` — title + close button row
- `.modal-body` — scrollable content area
- `.modal-footer` — sticky bottom action bar

### New Session Modal (Large — 640px)

Two-column layout replacing the current single-column stack.

**Structure:**
```
┌─────────────────────────────────────────┐
│ New Session                           ✕ │
├─────────────────────────────────────────┤
│ [/path/to/project                ] [Go] │
├──────────────────┬──────────────────────┤
│ RECENT PROJECTS  │ / > Users > jay >    │
│                  │   workspaces          │
│ ● claude-control │ ● _personal_/    git │
│   ~/_personal_   │ 📁 _work_/           │
│                  │ ● experiments/    git │
│ ● boll-website   │ 📁 archive/          │
│   ~/workspaces   │                      │
│                  │ + New Folder          │
│ ● experiments    │                      │
│   ~/projects     │                      │
├──────────────────┴──────────────────────┤
│                        [Cancel] [Open]  │
└─────────────────────────────────────────┘
```

**Key changes from current:**
- **Two-column split** — recents/favorites (45%) on left, file browser (55%) on right, separated by a vertical border
- **Path input** — full width above the columns
- **Folder creation collapsed** — just a `+ New Folder` text link at the bottom of the file browser column; clicking it expands an inline input + Create button; collapses back after creation or cancel
- **Recents** — each item shows project name (bold), abbreviated path below, and a green dot for git repos
- **File browser** — breadcrumb navigation above the directory listing; git repos show green dot + "git" badge
- **"Select This Folder" → "Open"** — shorter, clearer action button
- **Mobile** — columns stack vertically: recents on top, browser below

### Settings Modal (Large — 640px, Tabbed Sidebar)

Replace accordion layout with a left sidebar tab navigation.

**Structure:**
```
┌─────────────────────────────────────────┐
│ Settings                              ✕ │
├────────────┬────────────────────────────┤
│            │                            │
│ Server  ◄──│ Port                       │
│ Integra-   │ [8080]                     │
│   tions    │ Requires restart           │
│ Shortcuts  │                            │
│ ────────── │ Ngrok Token       [changed]│
│ Actions    │ [••••••••••]               │
│            │ Requires restart           │
│            │                            │
│            │ Claude Binary              │
│            │ [claude]                   │
│            │                            │
│            │ CLI Arguments              │
│            │ [--dangerously-skip-perms] │
│            │                            │
├────────────┴────────────────────────────┤
│ 1 unsaved change       [Cancel] [Save]  │
└─────────────────────────────────────────┘
```

**Tabs:**
1. **Server** (default/active) — Port, Ngrok Token, Claude Binary, CLI Arguments, Environment Variables, Compact Every N Continues, GitHub Token
2. **Integrations** — Jira (URL, Token, Email), Asana (Token), Google Tasks (API Key); each integration in a bordered card; unconfigured ones show "Not configured" summary
3. **Shortcuts** — key/value list with delete buttons, "+ Add Shortcut" at bottom
4. **Actions** — separated from other tabs by a divider; red text; contains Restart Server button

**Key changes from current:**
- **Left sidebar** — 160px wide, background `var(--bg-surface)` or equivalent, active tab has accent-colored text + left border highlight + selected background
- **Content panel** — scrolls independently; sidebar stays fixed
- **Change indicators** — modified fields show a "changed" badge next to the label and a highlighted border on the input
- **Sticky save footer** — spans full modal width; shows "N unsaved changes" count (or hidden when clean); Cancel + Save buttons always visible
- **First-run flow** — still supported; title changes to "Welcome — Configure Claude Controller"; sidebar hidden during first-run; shows only Server fields + "Save & Continue" button
- **Mobile** — sidebar becomes a horizontal scrollable tab bar at the top of the full-screen modal

### Task Create/Edit Modal (Medium — 560px)

Minimal changes — the current form layout works well.

**Changes:**
- Migrate all inline styles to shared CSS classes (`.modal-backdrop`, `.modal-md`, `.modal-header`, etc.)
- `border-radius: 16px`
- Slightly larger input padding (8px → 10px)
- Consistent box-shadow with other modals
- X close button in header

### Resume Picker Modal (Small — 480px)

**Changes:**
- Migrate to CSS classes (`.modal-sm`)
- Add X close button in header
- Slightly more padding on list items (10px 12px → 12px 16px)
- Consistent border-radius and shadow
- Same list layout — it's already clean

### Permission Prompt Modal (Small — 480px)

**Changes:**
- Migrate to CSS classes (`.modal-sm`)
- Consistent border-radius (16px) and shadow
- Same button layout: Deny (red), Allow Always (neutral), Allow (green)
- No structural changes — it's functional as-is

## Mobile Behavior

All modals become full-screen on mobile (existing behavior), but implemented via the shared CSS classes rather than `!important` overrides:

- `.modal-sm`, `.modal-md`, `.modal-lg` all get `width: 100vw; height: 100dvh; max-width: none; border-radius: 0` inside a `@media (max-width: 768px)` block
- Sticky header with back button (existing pattern, now via `.modal-header`)
- Desktop title hidden, mobile header shown (existing pattern)
- **New Session mobile**: columns stack vertically — recents section on top, file browser below
- **Settings mobile**: left sidebar becomes a horizontal scrollable tab bar at top

## CSS Organization

New CSS added to `style.css` (~80-100 lines):

```
/* ===== Modal System ===== */
.modal-backdrop { ... }
.modal-sm { width: 480px; }
.modal-md { width: 560px; }
.modal-lg { width: 640px; }
.modal-header { ... }
.modal-body { ... }
.modal-footer { ... }
.modal-close-btn { ... }

/* New Session specific */
.modal-columns { ... }
.modal-sidebar-list { ... }

/* Settings specific */
.settings-tabs { ... }
.settings-tab { ... }
.settings-tab.active { ... }
.settings-content { ... }
.settings-change-badge { ... }

/* Mobile overrides */
@media (max-width: 768px) {
  .modal-sm, .modal-md, .modal-lg { ... full-screen ... }
  .modal-columns { flex-direction: column; }
  .settings-tabs { ... horizontal tab bar ... }
}
```

Inline styles on the 5 modal HTML blocks in `index.html` will be replaced with these classes.

## What's NOT Changing

- Modal open/close logic (Alpine.js `x-show` bindings)
- Data flow (API calls, form submission, directory browsing)
- Feature set (no features added or removed)
- Shortcut picker popup (not a modal — separate component)
- z-index hierarchy (100 for standard modals, 200 for Permission/Task)
- Escape key behavior
- Alpine.js state properties
