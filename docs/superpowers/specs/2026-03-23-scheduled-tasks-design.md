# Scheduled Tasks Design Spec

**Issue:** #17 — Scheduled Tasks from UI
**Date:** 2026-03-23
**Status:** Draft

## Overview

Add a cron-style scheduled task system that runs shell commands or Claude prompts in the background on a recurring schedule. Tasks are associated with a working directory and optionally a session. A built-in Go scheduler executes tasks, logs output, and reconciles missed tasks on server startup.

## Task Types

1. **Shell command** — runs `bash -c "<command>"` in the specified working directory
2. **Shell script** — runs `bash <script_path>` in the specified working directory
3. **Claude command** — runs `claude -p "<prompt>"` in the specified working directory

The UI distinguishes these via a `task_type` field: `"shell"` (covers commands and scripts) or `"claude"`.

## Data Model

### `scheduled_tasks` table

```sql
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id),
    name TEXT NOT NULL,
    task_type TEXT NOT NULL CHECK(task_type IN ('shell', 'claude')),
    command TEXT NOT NULL,
    working_directory TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_run_at DATETIME,
    next_run_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

- `session_id` is nullable — shell tasks may not belong to a session
- `command` holds either the shell command/script path or the Claude prompt
- `working_directory` is an absolute path where the task executes
- `cron_expression` is standard 5-field cron: `minute hour dom month dow`

### `task_runs` table

```sql
CREATE TABLE IF NOT EXISTS task_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at DATETIME,
    exit_code INTEGER,
    output TEXT,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'success', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_task_runs_task_id ON task_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_started_at ON task_runs(started_at);
```

- `output` stores truncated stdout+stderr (last ~10KB)
- `status` transitions: `running` → `success` (exit 0) or `failed` (non-zero exit)

### Cascade behavior

When a scheduled task is deleted, its runs are cascade-deleted via the FK constraint. When a session is deleted, its associated tasks should be deleted in the `DeleteSession` transaction.

## Go Structs

```go
type ScheduledTask struct {
    ID               string     `json:"id"`
    SessionID        *string    `json:"session_id,omitempty"`
    Name             string     `json:"name"`
    TaskType         string     `json:"task_type"`
    Command          string     `json:"command"`
    WorkingDirectory string     `json:"working_directory"`
    CronExpression   string     `json:"cron_expression"`
    Enabled          bool       `json:"enabled"`
    LastRunAt        *time.Time `json:"last_run_at,omitempty"`
    NextRunAt        *time.Time `json:"next_run_at,omitempty"`
    CreatedAt        time.Time  `json:"created_at"`
    UpdatedAt        time.Time  `json:"updated_at"`
}

type TaskRun struct {
    ID         string     `json:"id"`
    TaskID     string     `json:"task_id"`
    StartedAt  time.Time  `json:"started_at"`
    FinishedAt *time.Time `json:"finished_at,omitempty"`
    ExitCode   *int       `json:"exit_code,omitempty"`
    Output     string     `json:"output"`
    Status     string     `json:"status"`
}
```

## DB Methods

```go
// Tasks CRUD
CreateScheduledTask(task ScheduledTask) (*ScheduledTask, error)
GetScheduledTaskByID(id string) (*ScheduledTask, error)
ListScheduledTasks(sessionID *string) ([]ScheduledTask, error)  // nil = all tasks
UpdateScheduledTask(id string, updates ScheduledTask) error
DeleteScheduledTask(id string) error
GetTasksDueForExecution(now time.Time) ([]ScheduledTask, error)
UpdateTaskNextRun(id string, nextRunAt time.Time) error
UpdateTaskLastRun(id string, lastRunAt time.Time) error

// Runs
CreateTaskRun(taskID string) (*TaskRun, error)
CompleteTaskRun(id string, exitCode int, output string) error
ListTaskRuns(taskID string, limit int) ([]TaskRun, error)
GetTaskRunByID(id string) (*TaskRun, error)
CleanupOldRuns(taskID string, keepCount int) error  // keep last N runs per task
```

## API Endpoints

All endpoints require Bearer token auth (existing middleware).

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/tasks` | Create a scheduled task |
| `GET` | `/api/tasks` | List all tasks (optional `?session_id=` filter) |
| `GET` | `/api/tasks/{taskId}` | Get a single task |
| `PUT` | `/api/tasks/{taskId}` | Update a task |
| `DELETE` | `/api/tasks/{taskId}` | Delete a task and its runs |
| `GET` | `/api/tasks/{taskId}/runs` | List recent runs (last 20) |
| `GET` | `/api/tasks/{taskId}/runs/{runId}` | Get a single run with full output |
| `POST` | `/api/tasks/{taskId}/trigger` | Manually trigger a task immediately |

### Request/Response shapes

**Create task:**
```json
POST /api/tasks
{
    "name": "Daily backup",
    "task_type": "shell",
    "command": "tar -czf backup.tar.gz ./data",
    "working_directory": "/home/user/project",
    "cron_expression": "0 2 * * *",
    "session_id": null
}
```

**Update task:**
```json
PUT /api/tasks/{taskId}
{
    "name": "Daily backup",
    "command": "tar -czf backup.tar.gz ./data ./config",
    "cron_expression": "0 3 * * *",
    "enabled": true
}
```

**Task run response:**
```json
{
    "id": "uuid",
    "task_id": "uuid",
    "started_at": "2026-03-23T02:00:00Z",
    "finished_at": "2026-03-23T02:00:05Z",
    "exit_code": 0,
    "output": "backup.tar.gz created successfully\n",
    "status": "success"
}
```

### Validation

- `cron_expression` validated server-side using `robfig/cron/v3` parser (5-field format)
- `task_type` must be `"shell"` or `"claude"`
- `working_directory` must be an absolute path; server checks it exists via `os.Stat`
- `name` and `command` must be non-empty

## Scheduler

### Architecture

New package `server/scheduler/` with a `Scheduler` struct that runs a background goroutine.

```go
type Scheduler struct {
    store  *db.Store
    done   chan struct{}
    wg     sync.WaitGroup
}
```

### Tick loop

- Ticks every 30 seconds
- Queries `GetTasksDueForExecution(now)` — returns tasks where `next_run_at <= now AND enabled = 1`
- For each due task, spawns a goroutine to execute it
- After spawning, immediately computes and persists the next `next_run_at`

### Task execution

```
1. Create a TaskRun record (status: "running")
2. Build exec.Cmd:
   - shell: bash -c "<command>" with Dir set to working_directory
   - claude: claude -p "<prompt>" with Dir set to working_directory
3. Capture combined stdout+stderr via CombinedOutput()
4. Truncate output to last 10KB if larger
5. Complete the TaskRun (exit code, output, status)
6. Update task's last_run_at
7. Clean up old runs (keep last 50 per task)
```

### Startup reconciliation

On server startup, the scheduler:

1. Queries all enabled tasks
2. Recomputes `next_run_at` from each task's `cron_expression` relative to `time.Now()`
3. For tasks whose `next_run_at` was in the past and within a 5-minute missed window: executes them immediately
4. Persists updated `next_run_at` values

### Stale run cleanup

On startup, any `task_runs` with `status = 'running'` are marked as `failed` (the server crashed mid-execution).

### Integration with main.go

```go
sched := scheduler.New(store)
sched.Start()
defer sched.Stop()
```

`Stop()` closes the done channel and waits for in-flight executions to complete (with a 30s timeout).

## Web UI

### Task list section

A new "Scheduled Tasks" section accessible from the sidebar or as a top-level view. Shows all tasks across sessions/directories.

Each task row displays:
- Name
- Type badge (`shell` / `claude`)
- Cron expression (with human-readable tooltip, e.g., "Every day at 2 AM")
- Working directory (truncated)
- Enabled/disabled toggle
- Last run status indicator (green dot = success, red = failed, gray = never run)
- Edit and delete buttons

### Create/edit modal

Fields:
- **Name** — text input
- **Type** — dropdown: "Shell Command" / "Claude Command"
- **Command** — textarea (label changes to "Prompt" when type is Claude)
- **Working Directory** — text input (absolute path)
- **Cron Expression** — text input with helper text ("5-field cron: min hour dom month dow") and common presets (hourly, daily, weekly)
- **Session** — optional dropdown of existing sessions (for Claude commands)

### Task runs panel

Clicking a task expands a panel showing the last 20 runs:
- Timestamp (relative, e.g., "2 hours ago")
- Duration
- Exit code
- Status badge (success/failed/running)
- Truncated output snippet (first 200 chars)
- Click to expand full output in a scrollable pre-formatted block

### Manual trigger

A "Run Now" button on each task that POST triggers `/api/tasks/{taskId}/trigger` and shows a toast notification.

### Data flow

- `loadScheduledTasks()` called on app init and after CRUD operations
- Alpine.js reactive state: `scheduledTasks[]`, `selectedTask`, `taskRuns[]`, `taskModalOpen`, `taskForm`
- Polling: tasks list refreshes every 30s to pick up run status changes

## Dependencies

- `github.com/robfig/cron/v3` — cron expression parsing and next-run computation
- No other new dependencies

## File Organization

```
server/
├── db/
│   ├── db.go              (add migrations)
│   ├── scheduled_tasks.go (NEW — task + run DB methods)
├── api/
│   ├── router.go          (add task routes)
│   ├── tasks.go           (NEW — task + run handlers)
├── scheduler/
│   ├── scheduler.go       (NEW — scheduler loop + execution)
├── web/static/
│   ├── index.html         (add task UI sections)
│   ├── app.js             (add task state + methods)
└── main.go                (wire up scheduler)
```

## Testing

- **DB layer:** CRUD operations, cascade deletes, due-task queries with time boundaries
- **API layer:** Handler tests following existing patterns (httptest + test store)
- **Scheduler:** Unit test tick logic with mock store; test reconciliation logic
- **Cron validation:** Edge cases — invalid expressions, 6-field rejection, boundary times

## Out of Scope

- Task dependencies / chaining (run B after A completes)
- Task output streaming (runs are fire-and-forget)
- Email/webhook notifications on failure
- Per-task environment variables
- Concurrent execution limits (same task won't run if previous run is still going — can add later)
