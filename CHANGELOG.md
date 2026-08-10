# Changelog

## 2026-08-10

- [fdca53b](https://github.com/lankeami/claude-control/commit/fdca53b2203790bdead9379f350d8589047d074f) fix: unwedge managed sessions after an unknown slash command (#248)
  Two changes: - SendPrompt writes ESC before the bracketed paste, closing any leftover TUI overlay (e.g. the slash-command suggestion dropdown an unknown command leaves open) that would otherwise swallow the paste. - The transcript watcher now recognizes the CLI's "Unknown command: /foo" system entry, surfaces a specific error naming the command, and finishes the turn cleanly instead of hanging until the generic "Prompt submission not confirmed" warning.

## 2026-08-06

- [e86a12f](https://github.com/lankeami/claude-control/commit/e86a12fe3490c358ab29727e7e06e8b8a1b6a27e) fix: stay silent on AskUserQuestion in PermissionRequest hook (#245)
  The generated per-session settings wire the PermissionRequest hook with no matcher, so it fired for AskUserQuestion too, relaying it to the web UI as an Allow/Deny card and blocking the TUI question dialog. The question card only appeared after interrupting the session.

## 2026-08-05

- fix: autoship runner crashes under `set -euo pipefail` (#243)
  Two shell bugs: (1) `write_autoship_save_point` glob on empty `.claude/save-points/` exits 2 under pipefail — added `|| true` guard matching `purge_autoship_save_point`. (2) `record_checkpoint` trips `set -u` on missing 4th arg and emits trailing-comma JSON on empty arg — default with `${4:-}` and conditionally omit the metadata field.
- [9a3d286](https://github.com/lankeami/claude-control/commit/9a3d286987eb6bde6140bbb65c9e73855afe12bb) fix: mobile menu header shows session title instead of app name (#241)
  The mobile side-menu overlay header was hardcoded to "Claude Controller". It now binds the selected session's display name via the existing sessionName() helper, falling back to the app name when no session is selected.
- [b0ab189](https://github.com/lankeami/claude-control/commit/b0ab189ee1e08af55a3d3388ca4c7d1d9499607f) fix: theme-aware session/task row background for dark mode (#239)
  .session-item hardcoded rgba(255,255,255,0.7) (the light-theme watermark scrim from b01d074), which rendered light-gray cards under light text in dark mode. Move the value to an --item-bg variable with a dark override so mobile session and task lists are readable in both themes.

## 2026-08-01

- [0eb693f](https://github.com/lankeami/claude-control/commit/0eb693f5ef7401683f01d4b392f79adf848fca27) fix: mobile skills tab parity with desktop (#236) (#237)
- [2c8896d](https://github.com/lankeami/claude-control/commit/2c8896d8d7ccd03f1f6a9494698f919de80a727b) feat: add author and last-updated time to PR list items (#234) (#235)

## 2026-07-28

- [77ed033](https://github.com/lankeami/claude-control/commit/77ed033e1e4166321f33b81bbf9ae4130b1a0fc6) feat: add shell-mode support for shortcuts (#233)
- [b3cf9b4](https://github.com/lankeami/claude-control/commit/b3cf9b44f97755b37bc2b3bfc47f4e23e5fe0add) feat: show open PR link before branch name in git status bar (#231)
