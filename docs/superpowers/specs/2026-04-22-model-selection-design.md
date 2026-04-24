# Model Selection Design

**Issue:** #122
**Date:** 2026-04-22

## Overview

Add model selection to two places: (1) a persistent dropdown in the managed session chat UI for switching models between turns, and (2) a dropdown in the scheduled task creation form.

## Goals

- Let users switch between Opus, Sonnet, and Haiku between turns in managed sessions
- Let users choose a model per scheduled task
- Keep the implementation minimal — no DB changes for sessions, per-turn model only

## Non-Goals

- Per-session persistent model (model is not stored in the sessions table)
- Free-text model ID input
- Model selection for hook-mode sessions
- Model usage tracking or cost display

## Design

### Model Options

Three curated models, no free-text input:

| Display Name | CLI `--model` Value         |
|--------------|-----------------------------|
| Opus         | `claude-opus-4-6`           |
| Sonnet       | `claude-sonnet-4-6`         |
| Haiku        | `claude-haiku-4-5-20251001` |

Default: Sonnet.

### Chat UI — Per-Turn Model Selector

**Location:** Persistent compact dropdown above the chat input area, left-aligned. Always visible, similar to ChatGPT's model picker.

**State management:**
- Alpine.js state variable `selectedModel` initialized from `localStorage` key `claude-controller-model` (default: `claude-sonnet-4-6`)
- On model change, new value is written to `localStorage`
- On each message send, the current `selectedModel` value is included in the POST request body

**Display:** Short labels ("Opus", "Sonnet", "Haiku") in the dropdown. Compact styling that doesn't compete with the chat input.

### API Changes

**`POST /api/managed-sessions/:id/message`** — Add optional `model` string field to the JSON request body.

**`buildPersistentArgs` function** — When the `model` field is present and non-empty in the message request, append `--model <value>` to the CLI args passed to `claude -p`. This is per-invocation — the session object is not modified.

**No changes to:**
- Session creation endpoint (model is not a session property)
- Sessions table schema
- Session GET/list responses

### Scheduled Tasks — Per-Task Model

**DB migration:** Add `model TEXT NOT NULL DEFAULT ''` column to `scheduled_tasks` table. Empty string means use the CLI's default model.

**`ScheduledTask` struct:** Add `Model string` field with JSON tag `"model"`.

**Task creation API:** Accept optional `model` field in the create/update request body.

**Task execution (`executeTask`):** When `task.Model` is non-empty, append `--model <value>` to the command args before spawning the process.

**UI:** Add a model dropdown to the scheduled task creation/edit form. Same three options (Opus, Sonnet, Haiku) plus a "Default" option that maps to empty string.

### Data Flow

#### Chat (per-turn):
```
User selects model in dropdown
  → Alpine.js state + localStorage
    → POST /api/managed-sessions/:id/message { model: "claude-opus-4-6", message: "..." }
      → buildPersistentArgs appends --model flag
        → claude -p --model claude-opus-4-6 ...
```

#### Scheduled tasks (per-task):
```
User selects model in task form
  → POST /api/triggers { ..., model: "claude-haiku-4-5-20251001" }
    → Stored in scheduled_tasks.model column
      → executeTask reads task.Model, appends --model flag
        → claude -p --model claude-haiku-4-5-20251001 ...
```

### Edge Cases

- **Empty/missing model in message request:** Don't append `--model` flag; CLI uses its own default.
- **Invalid model ID:** Let the CLI handle validation and error reporting — no server-side validation needed.
- **localStorage unavailable:** Fall back to Sonnet default in Alpine.js init.
- **Existing scheduled tasks after migration:** Get empty string model (DEFAULT ''), which means CLI default — no behavior change.
