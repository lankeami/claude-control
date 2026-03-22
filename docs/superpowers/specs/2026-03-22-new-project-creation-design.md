# New Project Creation — Design Spec

**Date:** 2026-03-22
**Issue:** #3 — "Be able to create a New Project"

## Problem

Starting a new managed session currently requires an existing directory. Users cannot create a brand-new project from the web UI — they must first create a directory manually, then browse to it. This adds friction for new projects.

## Solution

Add a "New Project" input within the existing browse modal. Users navigate to a parent directory, type a project name, and click "Create". The server creates the directory, initializes a git repo, writes a default `.gitignore`, and creates a managed session pointing at it. The user lands in the session chat in idle state, ready to send their first message.

## API

### `POST /api/sessions/create-project` (authenticated)

Uses the existing `/api/sessions/` namespace since the end result is a managed session.

**Request body:**
```json
{
  "parent_path": "/Users/jay/workspaces",
  "name": "my-new-project"
}
```

**Success response (201 Created):** Returns the newly created managed session (same shape as `POST /api/sessions/create` response).

```json
{
  "id": "uuid",
  "mode": "managed",
  "cwd": "/Users/jay/workspaces/my-new-project",
  "allowed_tools": "[\"Bash\",\"Read\",\"Edit\",\"Write\",\"Glob\",\"Grep\"]",
  "max_turns": 50,
  "max_budget_usd": 5.0,
  "status": "idle",
  "initialized": false,
  "created_at": "...",
  "last_seen_at": "..."
}
```

**Error responses:**
- `400` — Invalid project name or missing fields
- `400` — Parent path does not exist or is not a directory
- `409` — Directory already exists at target path
- `409` — Managed session already exists for that path (from existing unique constraint)
- `500` — Failed to create directory, init git, or write .gitignore

### Server-side logic (sequential)

1. **Validate name** — single regex whitelist: `^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,253}[a-zA-Z0-9])?$`
   - Min 1 char, max 255 chars
   - Must start and end with alphanumeric
   - Only allows letters, digits, `.`, `-`, `_` internally
   - Rejects shell metacharacters, path traversal, null bytes, spaces, and injection vectors
2. **Validate parent_path** — resolve to absolute path, resolve symlinks via `filepath.EvalSymlinks`, verify it exists and is a directory
3. **Check target** — `parent_path/name` must not already exist
4. **Create directory** — `os.Mkdir(fullPath, 0755)` (not `MkdirAll` — fails atomically if exists, won't create intermediate dirs)
5. **Git init** — `exec.Command("git", "init")` with `Dir = fullPath`
6. **Write .gitignore** — create default `.gitignore` in the new directory
7. **Create managed session** — call existing `CreateManagedSession` with `cwd = fullPath` and defaults for tools/turns/budget
8. **Return session** — 201 Created with session JSON

**Cleanup on failure:** If steps 5-7 fail after directory creation, clean up via `os.RemoveAll`. If cleanup itself fails, log the error but still return the primary error to the client.

## Name Validation — Security

The regex whitelist approach is the primary defense. Only explicitly safe characters pass through. This prevents:

- **Command injection:** No `;`, `|`, `&`, `$`, backticks, `()`, etc.
- **Path traversal:** No `..` (consecutive dots blocked by start/end alphanumeric requirement, and `.` only allowed internally between other chars)
- **Null byte injection:** Regex doesn't match `\x00`
- **Prompt injection:** Names are only used as filesystem paths, never interpolated into shell strings or prompts
- **Executable names:** While technically a name like `rm` would pass the regex, it's harmless — it's just a directory name, never executed
- **Symlink attacks:** Parent path resolved via `filepath.EvalSymlinks` before use

The name is always joined to the parent path using `filepath.Join`, never string concatenation with `/`.

## Default `.gitignore`

Minimal and language-agnostic — covers OS files and common environment patterns. Claude Code can generate a language-specific `.gitignore` as the user's first task.

```
# OS
.DS_Store
Thumbs.db

# Environment
.env
.env.*

# IDE
.idea/
.vscode/

# Dependencies (common)
node_modules/
vendor/
__pycache__/
*.pyc
.venv/

# Build output
dist/
build/

# Logs
*.log
```

## Frontend Changes

### Browse Modal Enhancement

Add a "New Project" row below the directory list in the existing browse modal:

- A text input with placeholder "New project name..."
- A "Create" button next to it (disabled when input is empty or name is invalid)
- Loading/disabled state on the Create button during the request to prevent double-clicks
- On click/enter: `POST /api/sessions/create-project` with `parent_path = browsePath`, `name = input`
- On success: close modal, set active session to the new session, navigate to chat view
- On error: show inline error message below the input with actionable text:
  - 400 invalid name → "Invalid project name. Use letters, numbers, hyphens, dots, or underscores."
  - 409 directory exists → "Directory already exists. You can select it from the list above."
  - 409 session exists → "A session already exists for this directory."
  - 500 → "Failed to create project. Please try again."
- Input validation mirrors server regex — disable Create button and show hint for invalid names

### No changes to existing flows

The browse, select, and two-step Enter flows remain unchanged. The new project input is purely additive.

## Testing

- **API tests** (`server/api/`):
  - Valid project creation → 201, session returned, directory exists with `.git` and `.gitignore`
  - Invalid name (special chars, empty, too long) → 400
  - Parent path doesn't exist → 400
  - Directory already exists → 409
  - Cleanup on partial failure
- **Name validation unit tests** — cover edge cases: min/max length, single char, allowed specials, rejected chars, path traversal attempts
