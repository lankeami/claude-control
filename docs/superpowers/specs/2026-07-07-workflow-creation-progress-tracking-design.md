# Workflow Creation & Progress Tracking Design

## Overview

Add the ability to define multi-step workflows (sequences of prompts) for managed sessions and monitor their execution progress in real time. Workflows are directed acyclic graphs (DAGs) of steps with success/failure branching, executed against existing managed sessions.

## Data Model

### `workflows` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | UUID |
| `name` | TEXT NOT NULL | Display name |
| `description` | TEXT | Optional description |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | |

### `workflow_steps` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | UUID |
| `workflow_id` | TEXT FK | References workflows(id) CASCADE |
| `name` | TEXT NOT NULL | Step display name |
| `prompt` | TEXT NOT NULL | The prompt to send to Claude |
| `step_order` | INTEGER NOT NULL | Position in the visual layout |
| `on_success` | TEXT FK nullable | Next step ID on clean exit |
| `on_failure` | TEXT FK nullable | Next step ID on error exit |
| `max_retries` | INTEGER DEFAULT 0 | Retry count before taking failure path |
| `timeout_seconds` | INTEGER DEFAULT 0 | 0 = no timeout |

The DAG is encoded via `on_success`/`on_failure` self-referencing FKs. A step with `on_success = NULL` means "workflow complete after this step." The entry point is the step with the lowest `step_order`.

Default wiring: when a user adds a new step in the builder, the previous step's `on_success` is auto-wired to the new step. This makes the common linear case seamless — users only touch the dropdowns when they want branching.

### `workflow_runs` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | UUID |
| `workflow_id` | TEXT FK | References workflows(id) |
| `session_id` | TEXT FK | References sessions(id) |
| `status` | TEXT | `pending`, `running`, `paused`, `completed`, `failed`, `cancelled` |
| `current_step_id` | TEXT FK nullable | References workflow_steps(id) |
| `started_at` | DATETIME | |
| `finished_at` | DATETIME nullable | |
| `error` | TEXT nullable | Failure reason |

### `workflow_run_steps` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | UUID |
| `run_id` | TEXT FK | References workflow_runs(id) CASCADE |
| `step_id` | TEXT FK | References workflow_steps(id) |
| `status` | TEXT | `pending`, `running`, `completed`, `failed`, `skipped` |
| `attempt` | INTEGER DEFAULT 1 | Current retry attempt |
| `started_at` | DATETIME nullable | |
| `finished_at` | DATETIME nullable | |
| `error` | TEXT nullable | |

## API Endpoints

### Workflow CRUD

- `POST /api/workflows` — Create workflow with steps. Body: `{name, description, steps: [{name, prompt, step_order, on_success_index, on_failure_index, max_retries, timeout_seconds}]}`. Steps reference each other by index in the array; server assigns IDs and resolves pointers.
- `GET /api/workflows` — List all workflows.
- `GET /api/workflows/:id` — Get workflow with its steps.
- `PUT /api/workflows/:id` — Update workflow definition. Full replace of steps array.
- `DELETE /api/workflows/:id` — Delete workflow. CASCADE deletes steps. Runs are preserved as orphan references.

### Workflow Execution

- `POST /api/workflows/:id/run` — Start a run. Body: `{session_id}`. Creates `workflow_run` + `workflow_run_steps` records, starts the engine goroutine. Returns the run ID. Returns 409 if the session already has an active run.
- `POST /api/workflow-runs/:id/pause` — Pause after current step completes.
- `POST /api/workflow-runs/:id/resume` — Resume a paused run.
- `POST /api/workflow-runs/:id/cancel` — Cancel immediately. Interrupts current step if running.

### Workflow Run Tracking

- `GET /api/workflow-runs` — List runs. Filterable by `?workflow_id=` and `?session_id=`.
- `GET /api/workflow-runs/:id` — Get run with all step statuses.
- `GET /api/workflow-runs/:id/stream` — SSE endpoint for real-time progress.

### SSE Event Types

```json
{"type":"step_started","step_id":"...","step_name":"Run tests","attempt":1}
{"type":"step_completed","step_id":"...","status":"completed"}
{"type":"step_failed","step_id":"...","error":"process exited with error","attempt":1,"max_retries":2}
{"type":"run_completed","status":"completed"}
{"type":"run_failed","error":"..."}
{"type":"run_paused"}
{"type":"run_cancelled"}
```

## Workflow Execution Engine

Server-side goroutine in `server/managed/workflow_engine.go`.

### Lifecycle

1. `StartRun(runID)` — spawned as a goroutine when `POST /api/workflows/:id/run` is called.
2. Loads the workflow definition + creates `workflow_run_steps` records for all steps (status=`pending`).
3. Identifies entry step (lowest `step_order`).
4. **Step loop:**
   - Sets current step to `running`, updates `workflow_runs.current_step_id`.
   - Sends the step's prompt to the session via the same internal path as `handleSendMessage` (reuses the managed session handler logic at the Go layer, not via HTTP).
   - Waits for `activity_state` to leave `working` (polls DB or listens to broadcaster).
   - Evaluates outcome: clean exit → follow `on_success` pointer, error exit → check retries remaining → retry or follow `on_failure` pointer.
   - If next step pointer is NULL → run is complete.
5. Broadcasts progress events to a per-run broadcaster (same pattern as session SSE).

### Step Completion Detection

Success vs failure is determined by activity state only:
- **Success:** Process exits cleanly — `activity_state` transitions to `waiting` or `idle`.
- **Failure:** Process errors, hits max turns, or step timeout expires.

### Pause / Resume / Cancel

- **Pause:** Sets a flag checked between steps. Current step finishes, then engine stops advancing. Run status → `paused`.
- **Resume:** Clears flag, re-enters step loop from `current_step_id`.
- **Cancel:** If a step is currently running, interrupts the session (same as existing interrupt endpoint). Run status → `cancelled`.

### Crash Recovery

On server startup, query for `workflow_runs` with status=`running`. For each:
- If session's `activity_state` is `idle`/`waiting`: the step completed while server was down. Evaluate outcome and resume from next step.
- If `activity_state` is `working`: wait for it to finish.

This mirrors the existing stale `activity_state` reset logic.

### Concurrency Guard

A session can only have one active workflow run at a time. The engine checks this before starting and rejects with 409 if another run is in progress.

### Session Interaction

The workflow engine reuses existing managed session infrastructure. Workflow steps are regular messages in the session's chat history — the user sees each prompt and response in real time via the existing session SSE stream. The workflow run SSE stream provides step-level progress metadata on top of this.

## Web UI: Workflow Builder

### Location

"Workflows" section in the sidebar, alongside sessions and scheduled tasks. Uses existing Alpine.js patterns.

### Workflow List View

- List of saved workflows with name, step count, last-run status.
- "New Workflow" button opens the builder modal.
- Click a workflow to edit or run it.

### Builder Modal

- **Header:** Name field, description field (optional).
- **Steps list:** Vertical list of step cards, each showing:
  - Step name (editable inline)
  - Prompt text (textarea, expands on focus)
  - Max retries (number input, default 0)
  - Timeout seconds (number input, default 0 = none)
  - On Success → dropdown of other steps or "End workflow"
  - On Failure → dropdown of other steps or "End workflow"
- **Add Step** button appends a new step card. Auto-wires previous step's `on_success` to the new step.
- **Drag to reorder** steps (sets `step_order` for visual ordering; DAG pointers control execution flow).
- **Delete step** button with confirmation (warns if other steps reference it).
- **Save** button — validates DAG (no unreachable steps, entry point exists), then POST/PUT to API.

## Web UI: Workflow Progress

### Sidebar Integration

When a session has an active workflow run, its sidebar entry shows a progress indicator: "Step 2/5 - Running tests" with a colored dot (yellow=running, green=completed, red=failed, gray=paused).

### Chat Area — Progress Panel

Collapsible panel between message history and input field:
- Workflow name and overall status (running/paused/completed/failed/cancelled).
- **Step timeline/stepper:** vertical list of steps with:
  - Status icon: checkmark (completed), spinner (running), X (failed), circle (pending)
  - Step name, attempt count if retried, duration
  - Current step highlighted and pulsing
  - Completed steps dimmed with green checkmark
  - Failed steps show red X with error text
  - DAG branching shown as fork icon with tooltips: "On success → Step X" / "On failure → Step Y"
- **Control buttons:** Pause/Resume toggle, Cancel (with confirmation).
- **Run button** in chat header: "Run Workflow" dropdown to pick a saved workflow and start it against the current session.

### Real-Time Updates

Progress panel subscribes to the run's SSE stream (`/api/workflow-runs/:id/stream`). Step transitions update the stepper in place without reload.

### Run History

Accessible from workflow list view. Click a workflow to see past runs with timestamps, status, session name, duration. Click a run for step-by-step detail.

## Error Handling

- **Step timeout:** Engine cancels the step after `timeout_seconds`. Counts as failure.
- **Server restart mid-run:** Crash recovery logic resumes in-progress runs on startup.
- **Session deleted during run:** Engine detects missing session, marks run as failed.
- **Orphaned runs:** Server startup sweep marks stale `running` runs with dead sessions as `failed`.

## Security

- No new permissions or elevated access. Workflows execute through existing managed session infrastructure with the same tool restrictions.
- Workflow APIs use the same API key auth middleware as all other endpoints.
- Step prompts are treated as literal strings — no template variable interpolation.

## Testing Strategy

- **Unit tests:** DB CRUD operations for all four tables, DAG validation logic.
- **Integration tests:** Engine step execution with mock session, pause/resume/cancel flows, crash recovery.
- **API tests:** All CRUD and execution endpoints with auth verification.
