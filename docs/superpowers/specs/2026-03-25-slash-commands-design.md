# Slash Commands Design

**Date:** 2026-03-25
**Issue:** #51 — Handle Slash Commands

## Overview

Add slash command support to the managed session web UI. When a user types `/` in the chat input, an autocomplete dropdown appears showing available commands. Commands come from three sources: built-in web UI commands, Claude CLI commands, and custom user-defined commands discovered from `.claude/commands/` directories.

## Command Sources

### Built-in Commands

Hardcoded in the web UI JavaScript. These are handled client-side or via dedicated server endpoints.

| Command | Description | Execution |
|---------|-------------|-----------|
| `/resume` | Continue a previous CLI session | Opens resume picker (existing behavior) |
| `/clear` | Clear chat display | Client-side: clears `chatMessages` array |
| `/compact` | Compact conversation context | Sends as prompt: "Please compact the conversation context" |
| `/cost` | Show session cost info | Client-side: displays cost from session metadata |
| `/help` | Show available commands | Client-side: renders command list as system message in chat |

### Claude CLI Commands (informational)

Some CLI commands don't translate to managed mode. These are excluded: `/config`, `/doctor`, `/init`, `/login`, `/logout`, `/vim`, `/terminal-setup`, `/model`, `/status`, `/review`. They require interactive CLI access that managed mode doesn't provide.

### Custom User Commands

Discovered from the filesystem by the server:
- `{session.CWD}/.claude/commands/` — project-level commands
- `~/.claude/commands/` — user-level commands

Each command is a `.md` file with YAML frontmatter:

```yaml
---
name: gsd:help
description: Show available GSD commands
argument-hint: [topic]
allowed-tools:
  - Read
  - Bash
---
```

The file body (after frontmatter) is a prompt template. When executed, the body is sent as the message via `sendManagedMessage()`. If the command accepts an argument, it's appended to the prompt body.

## Server API

### `GET /api/sessions/{id}/commands`

Returns all available slash commands for a session.

**Response:**
```json
[
  {
    "name": "/compact",
    "description": "Compact conversation context",
    "source": "builtin",
    "hasArg": false,
    "argHint": ""
  },
  {
    "name": "/gsd:help",
    "description": "Show available GSD commands",
    "source": "custom",
    "hasArg": true,
    "argHint": "[topic]"
  }
]
```

**Behavior:**
1. Return hardcoded built-in commands
2. Scan `{session.CWD}/.claude/commands/` recursively for `.md` files
3. Scan `~/.claude/commands/` recursively for `.md` files
4. Parse YAML frontmatter from each file for metadata
5. Merge all commands, deduplicating by name (project commands override user commands)
6. Return sorted by name

### `GET /api/sessions/{id}/commands/{name}/content`

Returns the body content of a custom command (frontmatter stripped).

**Response:**
```json
{
  "content": "The prompt template body...",
  "allowedTools": ["Read", "Bash"]
}
```

This endpoint is needed because the command list endpoint only returns metadata. The full prompt body is fetched on-demand when a custom command is executed.

## Web UI Autocomplete

### Trigger
- Dropdown appears when user types `/` as the first character in the textarea
- Only shown for managed sessions (not hook mode)

### Filtering
- As user continues typing after `/`, the list filters by prefix match on command name
- E.g., `/co` shows `/compact`, `/cost`

### Navigation
- Arrow Up/Down to navigate items
- Enter or click to select
- Escape to dismiss
- Dropdown dismisses on blur or when input no longer starts with `/`

### Selection Behavior
- Built-in commands: populate textarea with command name, ready to submit (or auto-execute for no-arg commands)
- Custom commands with no argument: populate textarea and auto-submit
- Custom commands with argument: populate textarea with command name + space, cursor at end for user to type argument

### Caching
- Commands are fetched once when a managed session is selected
- Cache is invalidated when switching sessions
- A refresh can be triggered manually if needed

## Command Execution Flow

```
User types "/" → autocomplete appears
User selects command → textarea populated
User presses Send (or auto-submit) → handleInput()
  ├─ Built-in command? → execute client-side handler
  └─ Custom command?
       ├─ Fetch content from /api/sessions/{id}/commands/{name}/content
       ├─ Append user argument if provided
       └─ Send as regular message via sendManagedMessage()
```

### Built-in Command Handlers

- **`/resume`**: Existing logic — opens resume picker modal
- **`/clear`**: `this.chatMessages = []` — clears UI only
- **`/compact`**: Sends `"Please compact the conversation context and summarize what we've been working on"` as a managed message
- **`/cost`**: Pushes a system message to chat with `this.currentSession.total_cost` info
- **`/help`**: Pushes a system message listing all available commands with descriptions

## UI Styling

The autocomplete dropdown:
- Positioned above the input bar (opens upward)
- Matches existing UI styling (dark theme, rounded corners)
- Each item shows command name (bold) and description (muted)
- Highlighted item has a subtle background color
- Max height with scroll for long lists
- Source badge (e.g., "custom") shown subtly next to custom commands

## Out of Scope

- Command argument validation (arguments are free-text)
- Command-specific allowed tools override (custom command `allowed-tools` frontmatter is informational only in v1)
- Slash commands in hook mode sessions
- Editing or creating custom commands from the web UI
