# Mobile Settings Tab — Design Spec

## Problem

Settings are inaccessible on mobile. The gear icon lives in the desktop sidebar header, which is hidden on small screens. The only mobile path is through the hamburger menu, which is buried and easy to miss.

## Solution

Add a 5th "Settings" tab to the mobile bottom tab bar. Tapping it opens the existing settings modal (`showSettingsModal = true`).

## What Changes

### HTML (`index.html`)

Add a new `<button>` inside `.mobile-tab-bar`, after the Tasks tab:

- Gear/cog SVG icon at 18x18 (matching other tab icons)
- Label text: "Settings"
- On click: `showSettingsModal = true` (does NOT set `mobileTab`)
- `aria-label="Settings"`

Unlike other tabs, this button does not switch `mobileTab` — it triggers the modal directly.

### CSS (`style.css`)

No changes required. The tab bar uses `display: flex` with `flex: 1` per child. Adding a 5th tab automatically redistributes from 25% to 20% width each.

### JavaScript (`app.js`)

No changes required. `showSettingsModal` already exists and controls the settings modal.

### Behavior

- Tap opens the same settings modal used by the desktop gear icon
- Modal contains Server Configuration and Shortcuts accordion sections
- Modal closes via X button, click-outside, or Cancel/Skip button
- Settings tab never shows an "active" highlight (it's a modal trigger, not a view)
- First-run settings flow is unaffected

### Accessibility

- `role="button"` with `aria-label="Settings"`
- Follows the same interactive pattern as other tab bar buttons

## Out of Scope

- Dedicated settings view/page (unnecessary — modal is sufficient)
- Changes to desktop settings access
- New settings options or categories
