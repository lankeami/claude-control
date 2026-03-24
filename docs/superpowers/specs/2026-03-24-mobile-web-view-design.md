# Mobile Web View Design

## Problem

The web UI is desktop-first with a three-column layout that collapses poorly on mobile. At <=768px, both sidebars are hidden and replaced with a basic dropdown selector, leaving most functionality (file browser, issues, scheduled tasks, file viewer) completely inaccessible. GitHub issue #12 requests full feature parity on mobile.

## Approach

CSS-only responsive design with minimal Alpine.js state additions. No new files — all changes live in the existing `index.html`, `app.js`, and `style.css`. The 768px breakpoint already exists; we expand it to cover all mobile functionality.

## Design

### Layout Architecture

Chat is the home view. On screens <=768px, the three-column desktop layout collapses to a single column showing only the chat area (messages + input bar). The left sidebar, right sidebar, file viewer, and resize handles are all hidden via CSS.

A hamburger button in the top-left of the main header replaces the collapsed sidebar toggle. Tapping it opens a full-screen overlay containing the session list and a bottom tab bar for navigating to secondary features.

### Hamburger Menu

**Trigger:** Hamburger icon (three horizontal lines) in the main header, left side. Visible only at <=768px. Replaces the existing mobile session dropdown (`.mobile-session-select`).

**Overlay:** Full-screen, `position: fixed`, `z-index: 1000`, slides in from the left (`transform: translateX`). Background matches `--bg` theme variable.

**Close behavior:** Tapping a session closes the menu and switches to that session's chat. An X button in the top-right also closes it. No swipe-to-close.

### Bottom Tab Bar

Fixed at the bottom of the hamburger menu overlay. Four tabs:

| Tab | Icon | Content |
|-----|------|---------|
| Sessions | List icon | Session list with status dots, mode badges, pending counts, delete buttons. "+ New Session" and "Resume" buttons at top. |
| Files | Folder icon | File tree + git status. Same content as right sidebar. |
| Issues | Circle-dot icon | GitHub issues list, search, state filter, detail view. |
| Tasks | Clock icon | Scheduled tasks list, create/edit buttons. |

Icons are inline SVG or Unicode to avoid adding an icon library.

### Full-Screen Overlays

When a detail view is needed from within the hamburger menu, it opens as a full-screen overlay that stacks on top of the menu.

**File Viewer overlay:** Slides in from the right when tapping a file from the Files tab.
- Header: Back arrow (returns to menu), filename, diff/full toggle, file type badge.
- Body: Same rendered content as desktop (syntax highlighting, markdown, JSON tree, etc.).

**Issue Detail overlay:** Same pattern when tapping an issue from the Issues tab.
- Header: Back arrow, issue title, state dot.
- Body: Issue body (markdown rendered), labels, "Generate Prompt" button, link to GitHub.

Closing the hamburger menu also closes all stacked overlays.

### Mobile Chat Experience

Chat area fills the full viewport below the header. Changes from desktop:
- Chat bubbles: `max-width: 90%` (up from 75-85%).
- Activity pills remain at top, same behavior.
- Input bar sticks to the bottom with same unified input (managed message / hook instruction / shell mode toggle).

Main header on mobile:
- Left: Hamburger icon.
- Center: Current session name (truncated with ellipsis).
- Right: Stop button (managed mode) / connection status dot.

Hook mode prompts display inline in the chat area, full-width.

### Modals on Mobile

All existing modals (New Session, Resume Picker, Task Editor) go full-screen at <=768px:
- `width: 100vw; height: 100vh; max-width: none; max-height: none; border-radius: 0;`
- Larger tap targets (min 44px height per Apple HIG).
- Scrollable content area.

No structural HTML changes — CSS overrides within the media query only.

### Alpine.js State Changes

Three new state variables in the existing `app` data object:

| Variable | Type | Purpose |
|----------|------|---------|
| `mobileMenuOpen` | boolean | Controls hamburger menu overlay visibility |
| `mobileTab` | string (`'sessions'` \| `'files'` \| `'issues'` \| `'tasks'`) | Active tab within the hamburger menu |
| `mobileOverlay` | string \| null (`null` \| `'file'` \| `'issue'`) | Which detail overlay is showing |

Existing methods get small additions:
- `selectSession()` sets `mobileMenuOpen = false`.
- `openFileViewer()` sets `mobileOverlay = 'file'` when on mobile.
- `fetchIssueDetail()` sets `mobileOverlay = 'issue'` when on mobile.

A helper method `isMobile()` checks `window.innerWidth <= 768` for conditional logic inside imperative methods only (not used in templates for reactive binding).

### Resize and Scroll Behavior

**Resize above 768px:** The mobile menu overlay is hidden via CSS outside the media query (`.mobile-menu-overlay { display: none }` in the base styles). This means even if `mobileMenuOpen` is `true`, the overlay is invisible on desktop. No resize listener needed.

**Body scroll lock:** When the hamburger menu is open, `overflow: hidden` is applied to `<body>` to prevent the chat area from scrolling behind the overlay. Applied via Alpine (`x-effect` on `mobileMenuOpen`).

**Session switch cleanup:** `selectSession()` also resets `mobileOverlay = null` to prevent stale detail views when switching sessions.

### iOS Safari / Viewport

**Viewport height:** Use `100dvh` (dynamic viewport height) instead of `100vh` for the overlay and modal heights. This accounts for iOS Safari's collapsible address bar. Fallback to `100vh` for older browsers.

**Input bar:** Use `position: sticky` (not `fixed`) for the input bar within the chat flex column. This avoids iOS Safari issues where `position: fixed` elements jump when the virtual keyboard opens.

### Accessibility

The hamburger button has `aria-label="Open menu"` and `aria-expanded` bound to `mobileMenuOpen`. The tab bar uses `role="tablist"` with `role="tab"` on each tab and `aria-selected` for the active tab. Focus is trapped within the overlay when open.

### Animations

Menu overlay slides in with `transform: translateX(-100%)` to `translateX(0)`, 250ms ease-out. Detail overlays slide from the right with the same timing. CSS transitions on the transform property.

### Empty States

When no session is selected, the Files, Issues, and Tasks tabs show a centered message: "Select a session to view [files/issues/tasks]."

### Out of Scope

- Browser back button / `history.pushState` integration — not implemented in v1. Physical back button does not close overlays.
- Swipe gestures for menu open/close.
- Tablet-specific (1024px) breakpoint.

### What Does NOT Change

- Desktop layout — no changes to the three-column layout above 768px.
- API layer — no server-side changes.
- Data flow — same Alpine.js stores, same SSE/polling.
- File structure — no new HTML, JS, or CSS files.
- Feature set — all existing features preserved, just re-laid-out for mobile.

## Files Modified

| File | Changes |
|------|---------|
| `server/web/static/style.css` | Expand `@media (max-width: 768px)` block with hamburger menu, bottom tabs, overlays, full-screen modals, mobile chat sizing. |
| `server/web/static/app.js` | Add `mobileMenuOpen`, `mobileTab`, `mobileOverlay` state. Add `isMobile` getter. Update `selectSession`, `openFileViewer`, `fetchIssueDetail` to manage mobile state. |
| `server/web/static/index.html` | Add hamburger button in header. Add mobile menu overlay template with tabs and content sections. Add file/issue detail overlay templates. Remove old `.mobile-session-select` dropdown. |
