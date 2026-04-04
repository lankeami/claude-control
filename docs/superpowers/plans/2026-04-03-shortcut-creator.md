# Shortcut Creator Implementation Plan

**Goal:** Add configurable shortcuts that map short keys to full text messages, with a picker UI in chat input and management in settings.

**Architecture:** Shortcuts stored as JSON in a `shortcuts.json` file alongside the `.env`. Exposed through the existing `/api/settings` GET/PUT endpoints with a new `shortcuts` field. Frontend gets a shortcut picker popup, accordion settings sections, and shortcut resolution on send.

**Tech Stack:** Go (server), Alpine.js (frontend), HTML/CSS (UI)

---

## Tasks

### Task 1: Backend — Shortcuts storage and API
- Add `Shortcut` struct and `Shortcuts []Shortcut` to `settingsPayload`
- Add `shortcutsPath`, `readShortcuts`, `writeShortcuts` helpers
- Update `handleGetSettings` to include shortcuts
- Update `handlePutSettings` to write shortcuts
- Tests for PUT with shortcuts, GET with shortcuts, GET with no shortcuts file

### Task 2: Frontend — Load shortcuts and settings form state
- Add `shortcuts`, `showShortcutPicker`, `settingsAccordion` state
- Add `shortcuts` to `settingsForm`
- Update `openSettingsModal` and `saveSettings` to handle shortcuts
- Add `loadShortcuts()` method, call on init and authenticate

### Task 3: Frontend — Settings modal accordion refactor
- Wrap existing settings fields in "Server Configuration" accordion (open by default)
- Add "Shortcuts" accordion section (collapsed by default) with add/edit/delete rows
- Add accordion CSS

### Task 4: Frontend — Shortcut picker popup
- Replace LGTM button with 😁 trigger button
- Add popup picker with shortcut list
- Replace `.lgtm-btn` CSS with `.shortcut-picker-*` CSS

### Task 5: Frontend — Shortcut resolution on send
- Replace `sendLgtm()` with `sendShortcut(value)`
- Add shortcut resolution in `handleInput()` — exact match of trimmed message against shortcut keys

### Task 6: Verification and cleanup
- Run all Go tests
- Build server
- Remove orphaned LGTM references
- Update README and CLAUDE.md
