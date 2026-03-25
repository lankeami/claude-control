# Slash Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add slash command autocomplete to the managed session web UI, supporting built-in commands and custom commands discovered from `.claude/commands/` directories.

**Architecture:** New Go handler (`commands.go`) scans filesystem for custom commands and serves them via API. Frontend adds an autocomplete dropdown component to the chat input that filters commands as the user types, dispatching to appropriate handlers on selection.

**Tech Stack:** Go (server API), Alpine.js (frontend), CSS (dropdown styling)

**Spec:** `docs/superpowers/specs/2026-03-25-slash-commands-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `server/api/commands.go` | Create | Command discovery, frontmatter parsing, two API handlers |
| `server/api/commands_test.go` | Create | Tests for command discovery and API endpoints |
| `server/api/router.go` | Modify | Register two new routes |
| `server/web/static/app.js` | Modify | Add slash command state, autocomplete logic, command execution dispatch |
| `server/web/static/index.html` | Modify | Add autocomplete dropdown HTML |
| `server/web/static/style.css` | Modify | Add autocomplete dropdown styles |

---

### Task 1: Server — Command Discovery & API

**Files:**
- Create: `server/api/commands.go`
- Create: `server/api/commands_test.go`
- Modify: `server/api/router.go:51-60`

- [ ] **Step 1: Create `commands.go` with types, frontmatter parsing, discovery, and both handlers**

Create `server/api/commands.go` with:
- `slashCommand` struct (Name, Description, Source, HasArg, ArgHint)
- `builtinCommands` var with /clear, /compact, /cost, /help, /resume
- `parseFrontmatter(content string) (commandMeta, body string)` — simple line-based YAML parsing
- `discoverCommands(dir, source string) []slashCommand` — walks dir for `.md` files
- `handleListCommands` — GET handler returning built-in + project + user commands, deduped
- `handleCommandContent` — GET handler reading command body by `?name=` query param

Key details:
- Use `claudeConfigDir()` from `resume.go` for user-level commands path
- Source is `"project"` for CWD commands, `"user"` for ~/.claude/commands/
- Project commands take priority over user commands (dedup by name)
- Command name derived from frontmatter `name:` field, falling back to relative filepath with `/` → `:`
- `handleCommandContent` converts `:` back to filepath separator for lookup
- Return 200 with empty-ish results if CWD doesn't exist (graceful degradation)

- [ ] **Step 2: Register routes in `router.go`**

Add after line 60 (after `handleShellExecute`):
```go
apiMux.HandleFunc("GET /api/sessions/{id}/commands", s.handleListCommands)
apiMux.HandleFunc("GET /api/sessions/{id}/commands/content", s.handleCommandContent)
```

- [ ] **Step 3: Create `commands_test.go` with tests**

Tests to write:
- `TestParseFrontmatter` — valid frontmatter extracts name, description, argument-hint, body
- `TestParseFrontmatterNoFrontmatter` — returns empty meta, full content as body
- `TestDiscoverCommands` — temp dir with `.md` files, including nested subdirectory
- `TestHandleListCommands` — creates managed session with temp CWD, verifies builtins + custom returned
- `TestHandleCommandContent` — verifies body returned with frontmatter stripped
- `TestHandleCommandContentNotFound` — returns 404

Use `setupTestStore(t)` helper creating temp SQLite DB.

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./api/ -v -run "TestParseFrontmatter|TestDiscoverCommands|TestHandleListCommands|TestHandleCommandContent"`
Expected: All PASS

- [ ] **Step 5: Run full test suite for regressions**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/commands.go server/api/commands_test.go server/api/router.go
git commit -m "feat: add slash commands discovery API endpoints

Add GET /api/sessions/{id}/commands for listing available commands
and GET /api/sessions/{id}/commands/content for fetching command body.
Discovers custom commands from .claude/commands/ directories."
```

---

### Task 2: Frontend — Autocomplete Dropdown

**Files:**
- Modify: `server/web/static/app.js:28-30` (state), `~118` (more state), `~769` (methods)
- Modify: `server/web/static/index.html:251-270` (input bar)
- Modify: `server/web/static/style.css` (append styles)

- [ ] **Step 1: Add Alpine.js state variables**

In `app.js`, after `shellMode: false,` (line 118), add:
```javascript
// Slash commands
slashCommands: [],
slashCommandsLoaded: false,
showSlashMenu: false,
slashFilter: '',
slashSelectedIndex: 0,
```

- [ ] **Step 2: Add computed property and methods**

Add these methods to the Alpine component:

`get filteredSlashCommands()` — filters `slashCommands` by prefix match on `slashFilter`

`loadSlashCommands()` — lazy fetch from `/api/sessions/{id}/commands`, sets `slashCommandsLoaded`

`onInputChange()` — called on every input; shows/hides menu based on whether input starts with `/`; sets `slashFilter` to text after `/` up to first space; calls `loadSlashCommands()` if needed

`handleSlashKeydown(e)` — ArrowUp/Down navigate, Enter/Tab select, Escape dismiss; falls through to Cmd+Enter send if menu is closed

`selectSlashCommand(cmd)` — closes menu, sets `inputText` to `cmd.name` (+ space if `hasArg`)

- [ ] **Step 3: Invalidate cache on session switch**

In `selectSession` method, add near the top:
```javascript
this.slashCommands = [];
this.slashCommandsLoaded = false;
this.showSlashMenu = false;
```

- [ ] **Step 4: Update instruction-bar HTML in `index.html`**

Replace the instruction-bar div (lines 252-270) to:
- Add `style="position:relative"` to the wrapper div
- Insert autocomplete dropdown div before textarea (positioned `bottom:100%`)
- Dropdown uses `x-for` over `filteredSlashCommands`, shows name, argHint, description, source badge
- Update textarea: add `x-ref="chatInput"`, replace `@keydown` with `handleSlashKeydown($event)`, add `onInputChange()` to `@input`
- Update placeholder to mention `/ for commands`

- [ ] **Step 5: Add CSS for dropdown**

Append to `style.css`:
- `.slash-menu` — absolute positioned above input, max-height 280px, scroll, dark bg, border, shadow, z-index 100
- `.slash-menu-item` — flex row, gap 8px, padding 8px 12px, cursor pointer
- `.slash-menu-item.selected` — highlight background
- `.slash-cmd-name` — bold, nowrap
- `.slash-cmd-hint` — muted, italic, nowrap
- `.slash-cmd-desc` — muted, flex 1, ellipsis overflow
- `.slash-cmd-source` — small badge for non-builtin commands

- [ ] **Step 6: Build to verify**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles

- [ ] **Step 7: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html server/web/static/style.css
git commit -m "feat: add slash command autocomplete dropdown UI

Shows filtered command list when typing / in managed session input.
Supports keyboard navigation (arrows, enter, escape, tab).
Lazy-loads commands on first / keystroke per session."
```

---

### Task 3: Frontend — Command Execution Dispatch

**Files:**
- Modify: `server/web/static/app.js:769-791` (refactor `handleInput`)

- [ ] **Step 1: Refactor `handleInput()` to route slash commands**

Replace the existing `/resume` interception (lines 773-780) with general slash command routing:
- If managed session and input starts with `/`, parse cmdName + cmdArg, show in chat, call `executeSlashCommand()`
- Otherwise, existing logic (sendManagedMessage / executeShell / sendInstruction)

- [ ] **Step 2: Add `executeSlashCommand(cmdName, cmdArg)` dispatcher**

Switch on cmdName:
- `/resume` → `openResumePicker()`
- `/clear` → `chatMessages = []`
- `/compact` → set inputText to compaction prompt, call `sendManagedMessage()`
- `/cost` → push system message with `currentSession.total_cost`
- `/help` → `loadSlashCommands()`, push system message listing all commands
- default → `executeCustomCommand(cmdName, cmdArg)`

- [ ] **Step 3: Add `executeCustomCommand(cmdName, cmdArg)`**

- Fetch from `/api/sessions/{id}/commands/content?name={cmdName}`
- If `$ARGUMENTS` in prompt, replace all occurrences with cmdArg; otherwise append
- Set `inputText` to final prompt, call `sendManagedMessage()`
- On 404/error, push system message "Unknown command"

- [ ] **Step 4: Build and run full test suite**

Run: `cd server && go build -o claude-controller . && go test ./... -v`
Expected: Build succeeds, all tests pass

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: slash command execution with built-in and custom command support

Refactors handleInput() to dispatch slash commands through unified system.
Built-in: /resume, /clear, /compact, /cost, /help.
Custom commands: fetches prompt template, supports \$ARGUMENTS substitution.
Replaces hardcoded /resume interception."
```

---

### Task 4: Final Verification & PR

- [ ] **Step 1: Run full Go test suite**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 2: Build final binary**

Run: `cd server && go build -o claude-controller .`
Expected: Compiles without errors

- [ ] **Step 3: Push and open draft PR**

```bash
git push -u origin feat/slash-commands
gh pr create --draft --title "feat: slash command autocomplete for managed sessions" --body "$(cat <<'EOF'
## Summary

Closes #51

- Adds slash command autocomplete dropdown when typing `/` in managed session chat input
- Discovers custom commands from `.claude/commands/` directories (project-level and user-level)
- Built-in commands: `/clear`, `/compact`, `/cost`, `/help`, `/resume`
- Custom commands: fetches prompt template from server, supports `$ARGUMENTS` substitution
- Keyboard navigation: arrow keys, enter/tab to select, escape to dismiss

## New API endpoints

- `GET /api/sessions/{id}/commands` — list all available slash commands
- `GET /api/sessions/{id}/commands/content?name=/cmd` — get custom command prompt body

## Test plan

- [ ] Type `/` in managed session input — autocomplete dropdown appears with commands
- [ ] Type `/co` — dropdown filters to show `/compact`, `/cost`
- [ ] Arrow keys navigate, Enter selects, Escape dismisses
- [ ] `/help` shows command list in chat
- [ ] `/clear` clears chat display
- [ ] `/cost` shows session cost
- [ ] `/resume` opens resume picker
- [ ] `/compact` sends compaction prompt to Claude
- [ ] Custom commands (from `.claude/commands/`) appear in list and execute correctly
- [ ] `$ARGUMENTS` substitution works in custom command templates
- [ ] Switching sessions reloads commands
EOF
)"
```
