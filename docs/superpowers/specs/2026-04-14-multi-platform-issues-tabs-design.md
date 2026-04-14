# Multi-Platform Project Management Tabs — Design Spec

**Date:** 2026-04-14
**Issue:** #90 — Add more project management tools

## Overview

Add support for multiple project management platforms (Jira, Asana, Google Tasks) alongside the existing GitHub integration in the issues panel. The panel gets a favicon-based tab bar that conditionally shows only configured platforms.

## Requirements

1. Tab bar at the top of the issues section with platform favicon icons
2. Only show tabs for platforms that have credentials configured in Settings
3. GitHub tab retains full existing functionality (issues, PRs, search, state filters, detail view)
4. New platform tabs show a "connected" placeholder — no real API calls yet
5. Settings panel gets configuration fields for each new platform
6. Works on both desktop (right sidebar) and mobile (issues tab)

## Settings Fields

| Platform | Fields | Notes |
|----------|--------|-------|
| GitHub | (existing) token | Already implemented |
| Jira | `jira_url`, `jira_token`, `jira_email` | Base URL needed for self-hosted instances |
| Asana | `asana_token` | Personal access token |
| Google Tasks | `google_tasks_token` | API key or OAuth token |

Settings are stored in the existing server-side settings system (SQLite `settings` table, key-value pairs).

## Tab Bar UI

- Horizontal row of favicon icons (16×16) at the top of the issues section
- Each icon is a clickable tab; active tab gets an underline/accent indicator
- Icons: GitHub Octocat SVG, Jira blue diamond SVG, Asana coral dots SVG, Google Tasks checkmark SVG
- All favicons are inline SVGs (no external fetches, works offline)
- Tab bar only renders if at least one platform is configured
- If only GitHub is configured, tab bar still shows (single tab) for consistency

### Favicon Sources (inline SVGs)
- **GitHub**: Octocat mark (monochrome, adapts to theme)
- **Jira**: Blue diamond/gradient mark
- **Asana**: Three coral/orange dots
- **Google Tasks**: Blue checkmark with circle

## Frontend State

```javascript
// New state fields
issuesProvider: 'github',  // 'github' | 'jira' | 'asana' | 'google_tasks'

// Computed from settings
configuredProviders: [],   // Populated from settings on load
```

`configuredProviders` is derived by checking which platform tokens are non-empty in settings.

## Tab Content Per Platform

### GitHub (existing)
- Issues list with open/closed toggle, search, pagination
- Pull requests list with same controls
- Detail view with markdown body rendering

### Jira (placeholder)
- Shows: "Connected to Jira at {jira_url}" with Jira icon
- Message: "Issue browsing coming in a future update"

### Asana (placeholder)
- Shows: "Connected to Asana" with Asana icon
- Message: "Task browsing coming in a future update"

### Google Tasks (placeholder)
- Shows: "Connected to Google Tasks" with Google Tasks icon
- Message: "Task browsing coming in a future update"

## API Changes

### New Settings Endpoints (use existing settings infrastructure)
No new endpoints needed — the existing `GET/PUT /api/settings` handles arbitrary key-value pairs.

### New Keys
- `jira_url`, `jira_token`, `jira_email`
- `asana_token`
- `google_tasks_token`

## Desktop Layout

```
┌─────────────────────────────────┐
│ [GH] [Jira] [Asana] [GT]       │  ← Tab bar (favicons only)
│ ─────────────────────────────── │
│ ▾ Issues (3 open)               │  ← Current issues UI (GitHub tab)
│   #42 Fix login bug             │
│   #38 Add dark mode             │
│ ▾ Pull Requests (1 open)        │
│   #41 Refactor auth             │
└─────────────────────────────────┘
```

## Mobile Layout

Same tab bar appears at the top of the mobile Issues tab content area, before the issues list.

## Scope Boundaries

**In scope:**
- Tab bar UI with inline SVG favicons
- Settings fields for Jira, Asana, Google Tasks
- Conditional tab visibility
- Placeholder content for new platforms
- Both desktop and mobile layouts

**Out of scope:**
- Actual API integrations for Jira/Asana/Google Tasks
- OAuth flows
- Issue detail views for new platforms
- Cross-platform issue search
