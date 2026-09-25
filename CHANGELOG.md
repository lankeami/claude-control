# Changelog

## 2026-09-25

- [8fc06c7](https://github.com/lankeami/claude-control/commit/8fc06c739ce7f89c00c1e1bc40e9a49a25937330) fix: auto-complete pipeline runs, add project context, improve discoverability
  Pipeline runs previously stayed in running forever because nothing marked the parent run complete when all items finished. This was due to autoship using Claude Code JS Workflow tool (client-side) rather than the server-side RunParallel method that had completion logic.

## 2026-09-21

- [a9eb875](https://github.com/lankeami/claude-control/commit/a9eb8757ed8f0837b4e873bfb80689f4d81feb94) fix: shell mode input starting with / bypasses executeShell
  The slash command check in handleInput() ran before the shellMode check, so any shell command starting with / (like absolute paths) was intercepted as a slash command and sent as a prompt instead of being executed as a shell command. Moved the slash command check inside the else branch so shellMode takes priority.
- [96f53af](https://github.com/lankeami/claude-control/commit/96f53aff6a22721e0bcd190110a02533f777ffe5) fix: add delivery confirmation and send mutex to SendPrompt
  SendPrompt was fire-and-forget — writing to the PTY without confirming the CLI consumed the input. Unconsumed bytes accumulated in the kernel buffer and concatenated with the next prompt. Now SendPrompt serializes via a per-process mutex, detects stale unconsumed input (clearing with ESC + Ctrl-U before the new prompt), and waits for PTY output change as delivery confirmation.

## 2026-09-20

- [26b230c](https://github.com/lankeami/claude-control/commit/26b230cb5157f87d38f32d702d955110bf72f3c3) feat: add pipeline runs UI and parallel workflow execution (#283)
  Bridge autoship parallel orchestration to the web UI via a new pipeline_runs system. The Workflows sidebar was always empty during autoship runs because autoship uses Claude Code's Workflow tool (JS orchestration) which never writes to the workflows SQLite table.

## 2026-09-16

- [7767338](https://github.com/lankeami/claude-control/commit/7767338b18bc6baad6eed7e66be53a7274fe071a) fix: dismiss welcome splash screen and clean up prompt-failed turn end
  Two fixes for interactive managed sessions:

## 2026-09-10

- [42ccd11](https://github.com/lankeami/claude-control/commit/42ccd11c070d80abf0b7284b5cf330ebb08dffad) fix: parse thread ID from nested response.thread.id
  thread/start returns {thread: {id: ...}, model: ..., ...} not a flat {id: ...}. ThreadID was empty, causing turn/start to fail with "invalid thread id: invalid length". Also remove debug logging.
- [d0952ae](https://github.com/lankeami/claude-control/commit/d0952aec8897a2498867e6107a16b0b693e65048) fix: align Codex app-server integration with current protocol (#279)
  - Wire CODEX_BIN env var in main.go (was defined on Config but never set) - Send initialize handshake before any other JSON-RPC method - Store thread ID from thread/start response, pass it to turn/start - Fix turn/start params: threadId + input array instead of bare message - Wait for turn/completed notification instead of treating RPC response as completion - Fix item/agentMessage/delta: read delta field, not text - Fix item/started: parse nested item object with type-specific fields (commandExecution, mcpToolCall, dynamicToolCall, fileChange) - Add item/completed and turn/completed notification adapters - Replace token_count listener with thread/tokenUsage/updated - Handle item/commandExecution/requestApproval and item/fileChange/requestApproval with accept/decline responses - Update all tests for new protocol shapes

## 2026-09-09

- [283dee8](https://github.com/lankeami/claude-control/commit/283dee8d22ec6250eb629cd2d4352f70477bf03d) feat: add clear button to session search filter (#277)
  Wrap both desktop and mobile session filter inputs in a positioned container with an X button that clears sessionFilter on click. Button only appears when filter text is present.

## 2026-09-01

- [5f656d2](https://github.com/lankeami/claude-control/commit/5f656d2250818016f99c0862d689a1870400666a) fix: hide model label from mobile header (already in bottom bar)
  On mobile web, the model name (e.g. "claude-opus-4-6") was displayed in the top header bar, overlapping the project/session name. The model is already accessible via the selector dropdown in the bottom input toolbar, so hide the header label on mobile viewports.

## 2026-08-21

- [7bd6563](https://github.com/lankeami/claude-control/commit/7bd65632d686ef00b22f79c12ecf9e894f9cf4bc) feat: add agent picker and per-session agent visibility to web UI and iOS app (#262)
  Expose agent switching to end users: the new-session modal gains a Claude/Codex radio selector, session list cards show a purple badge for non-claude agents, and the session detail header surfaces the agent and model/provider string.
- [2186675](https://github.com/lankeami/claude-control/commit/218667594ff63c76338ef1be8eeb6b39a9dc275a) feat: add agent-selection seam to managed sessions (#260)
  Add an `agent` field on managed sessions ("claude" default, "codex") threaded from DB through the create-session API into server/managed/, where process spawn dispatches by agent. Claude sessions keep the existing interactive/print backends unchanged; codex routes to a stub that returns a not-implemented error for the follow-up issue to fill in.

## 2026-08-15

- [8736ab4](https://github.com/lankeami/claude-control/commit/8736ab4df8d9bbfc91d0def436b12bc76072869b) fix: resolve /reload-skills stall by adding local command fallback timeout
  Slash commands processed locally by the CLI (e.g. /reload-skills) never trigger a Stop hook or produce transcript activity, so waitForTurnEnd blocked indefinitely. Adds a 5s fallback: if a slash command produces no transcript echo or assistant activity within the timeout, the turn ends cleanly without waiting for a Stop hook.

## 2026-08-12

- [5343d29](https://github.com/lankeami/claude-control/commit/5343d290f0a876bb3d528d29156aaba0d2e125b1) Spike: evaluate OpenRouter for Claude Code / Codex agent switching (#259)
  Research doc with conditional-GO recommendation (agents native, OpenRouter optional per-agent config), PoC artifacts from Codex CLI 0.147.0 under PTY, and links to execution issues #260-#262.

## 2026-08-11

- [90113dd](https://github.com/lankeami/claude-control/commit/90113dd877fd8a97b3251584d5757f30d478ff84) fix: make managed AskUserQuestion option buttons clickable
  The option buttons bind :disabled="msg.answered". The question message was pushed without an `answered` key at all, and Alpine renders the buttons disabled when the bound property is absent — so the card showed but no answer could be selected (the "unable to select the answers" report). Initialize answered:false on the message so the buttons are enabled until the question is actually answered.
- [887c065](https://github.com/lankeami/claude-control/commit/887c0658b643199b580f9e4310245440c034b6c8) fix: dedup managed AskUserQuestion cards by tool_use_id
  Two front-end paths create a question card from the same tool call: the typed pending_question SSE event (real-time, from the PreToolUse hook) and the raw assistant tool_use block. In the interactive backend the CLI buffers the AskUserQuestion transcript entry until the question resolves, so the second path fires at resolution and pushes a stale, unanswerable duplicate carrying the same tool_use_id — the "2 of the same questions" bug.

## 2026-08-10

- [fc36b74](https://github.com/lankeami/claude-control/commit/fc36b74482cd288460a05638312b8da9b82b22dd) feat: add /release skill for minor-bump-by-default GitHub releases
  Adds .claude/skills/release/SKILL.md: finds the previous v* tag, bumps the minor version unless an explicit version is given, runs pre-flight checks (on main, clean tree, up to date with origin), tags main, and publishes a categorized release via gh.
- [674c2de](https://github.com/lankeami/claude-control/commit/674c2de4b701b4fe391e66d011595ed96ddda0a6) feat: support multiple sessions per repo via worktrees with confirm prompt
  When creating a session for a repo that already has one, the web UI now asks: Yes opens a new session in a fresh git worktree with a distinct name; No selects the existing session. Server accepts worktree+name on POST /api/sessions/create and runs git worktree add; plain duplicate creates still 409.
- [c8294be](https://github.com/lankeami/claude-control/commit/c8294becb76309c918c77d153c0c27068e7cff82) fix: cron scheduler fires at stale-timezone wall-clock times (#250)
  Two compounding defects made scheduled tasks fire hours late after the machine's timezone changed while the server was running:
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
