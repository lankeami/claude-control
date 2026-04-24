# Model Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-turn model selection to the chat UI and per-task model selection for scheduled tasks.

**Architecture:** Model is ephemeral per-turn for chat (stored in localStorage, sent with each message POST). For scheduled tasks, model is persisted in the `scheduled_tasks` DB table. The `--model` CLI flag is appended to process args when a model is specified.

**Tech Stack:** Go (server), SQLite (DB), Alpine.js (frontend)

---

### Task 1: Add model field to scheduled tasks DB layer

**Files:**
- Modify: `server/db/db.go:101-114` (migrations)
- Modify: `server/db/scheduled_tasks.go:19-32` (struct), `44` (taskColumns), `46-59` (scanTask), `78` (CreateScheduledTask), `130` (UpdateScheduledTask)
- Modify: `server/db/scheduled_tasks_test.go`

- [ ] **Step 1: Write failing test for model field in scheduled tasks**

Add to `server/db/scheduled_tasks_test.go`:

```go
func TestScheduledTaskModelField(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "Model task", "claude", "summarize", "/tmp", "0 9 * * *", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q, want %q", task.Model, "claude-opus-4-6")
	}

	// Verify empty model works
	task2, err := store.CreateScheduledTask("", "No model", "shell", "echo hi", "/tmp", "0 * * * *", "")
	if err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if task2.Model != "" {
		t.Errorf("model: got %q, want empty", task2.Model)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestScheduledTaskModelField`
Expected: FAIL — `CreateScheduledTask` has wrong number of arguments

- [ ] **Step 3: Add migration, update struct, columns, scan, and create/update functions**

In `server/db/db.go`, add migration after line 132 (the `compact_every_n_continues` migration):

```go
`ALTER TABLE scheduled_tasks ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
```

In `server/db/scheduled_tasks.go`:

Add `Model` field to `ScheduledTask` struct (after `UpdatedAt`):

```go
Model            string     `json:"model"`
```

Update `taskColumns` constant to:

```go
const taskColumns = `id, session_id, name, task_type, command, working_directory, cron_expression, enabled, last_run_at, next_run_at, created_at, updated_at, COALESCE(model,'')`
```

Update `scanTask` to scan Model — add `&t.Model` at the end of the Scan call:

```go
err := scanner.Scan(
    &t.ID, &t.SessionID, &t.Name, &t.TaskType, &t.Command,
    &t.WorkingDirectory, &t.CronExpression, &enabled,
    &t.LastRunAt, &t.NextRunAt, &t.CreatedAt, &t.UpdatedAt,
    &t.Model,
)
```

Update `CreateScheduledTask` signature to accept `model string`:

```go
func (s *Store) CreateScheduledTask(sessionID, name, taskType, command, workingDir, cronExpr, model string) (*ScheduledTask, error) {
```

Update the INSERT SQL in `CreateScheduledTask`:

```go
_, err := s.db.Exec(`
    INSERT INTO scheduled_tasks (id, session_id, name, task_type, command, working_directory, cron_expression, model, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
`, id, sessPtr, name, taskType, command, workingDir, cronExpr, model)
```

Update `UpdateScheduledTask` signature to accept `model string`:

```go
func (s *Store) UpdateScheduledTask(id, name, taskType, command, workingDir, cronExpr string, enabled bool, model string) error {
```

Update the UPDATE SQL:

```go
_, err := s.db.Exec(`
    UPDATE scheduled_tasks
    SET name = ?, task_type = ?, command = ?, working_directory = ?, cron_expression = ?, enabled = ?, model = ?, updated_at = datetime('now')
    WHERE id = ?
`, name, taskType, command, workingDir, cronExpr, enabledInt, model, id)
```

- [ ] **Step 4: Fix all callers of CreateScheduledTask and UpdateScheduledTask**

Update existing test calls in `server/db/scheduled_tasks_test.go` to pass `""` as the new `model` parameter:

```go
// Every existing call like:
store.CreateScheduledTask(sess.ID, "Task A", "shell", "echo a", "/tmp", "0 * * * *")
// becomes:
store.CreateScheduledTask(sess.ID, "Task A", "shell", "echo a", "/tmp", "0 * * * *", "")
```

Update `server/api/tasks.go` — `handleCreateTask` (line 73):

```go
task, err := s.store.CreateScheduledTask(req.SessionID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression, req.Model)
```

Update `server/api/tasks.go` — `handleUpdateTask` (line 148):

```go
if err := s.store.UpdateScheduledTask(taskID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression, req.Enabled, req.Model); err != nil {
```

Add `Model` field to both request structs in `server/api/tasks.go`:

```go
type createTaskRequest struct {
	SessionID        string `json:"session_id"`
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
	Model            string `json:"model"`
}

type updateTaskRequest struct {
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
	Enabled          bool   `json:"enabled"`
	Model            string `json:"model"`
}
```

Update `server/api/tasks.go` — `toggleTaskEnabled` body construction in the test if needed, and update the scheduler caller in `server/scheduler/scheduler_test.go` if it calls `CreateScheduledTask`.

- [ ] **Step 5: Run all tests to verify**

Run: `cd server && go test ./db/ -v -run TestScheduledTask`
Run: `cd server && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add server/db/db.go server/db/scheduled_tasks.go server/db/scheduled_tasks_test.go server/api/tasks.go
git commit -m "feat: add model field to scheduled tasks DB layer (#122)"
```

---

### Task 2: Pass model flag in scheduler execution

**Files:**
- Modify: `server/scheduler/scheduler.go:127-165` (executeTask)
- Modify: `server/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing test for model in task execution**

Add to `server/scheduler/scheduler_test.go`:

```go
func TestExecuteTaskWithModel(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateScheduledTask("", "Model test", "claude", "hello", "/tmp", "0 * * * *", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sched := New(store)
	sched.executeTask(*task)

	runs, err := store.ListTaskRuns(task.ID, 1)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one run")
	}
	// The test verifies the task runs without panicking with the model field.
	// Since "claude" binary won't exist in CI, we just check a run was created.
}
```

- [ ] **Step 2: Run test to confirm baseline behavior**

Run: `cd server && go test ./scheduler/ -v -run TestExecuteTaskWithModel`
Expected: PASS or FAIL depending on whether `claude` binary exists — but no panic.

- [ ] **Step 3: Add model flag to executeTask**

In `server/scheduler/scheduler.go`, update the `executeTask` function. Change the `case "claude"` block (line 140):

```go
case "claude":
    args := []string{"-p"}
    if task.Model != "" {
        args = append(args, "--model", task.Model)
    }
    args = append(args, task.Command)
    cmd = exec.CommandContext(ctx, "claude", args...)
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./scheduler/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/scheduler/scheduler.go server/scheduler/scheduler_test.go
git commit -m "feat: pass --model flag in scheduled task execution (#122)"
```

---

### Task 3: Add per-turn model to message API

**Files:**
- Modify: `server/api/managed_sessions.go:138-152` (handleSendMessage request struct), `69-121` (buildPersistentArgs)

- [ ] **Step 1: Write failing test for model in message request**

Add to `server/api/managed_sessions_test.go`:

```go
func TestBuildPersistentArgs_WithModel(t *testing.T) {
	sess := &db.Session{
		ID:       "test-id",
		MaxTurns: 10,
	}
	cfg := managed.Config{}
	args := buildPersistentArgs(sess, cfg)

	// Without model, no --model flag
	for _, a := range args {
		if a == "--model" {
			t.Error("unexpected --model flag without model set")
		}
	}
}
```

- [ ] **Step 2: Run test to verify baseline passes**

Run: `cd server && go test ./api/ -v -run TestBuildPersistentArgs_WithModel`
Expected: PASS (baseline — no model yet)

- [ ] **Step 3: Add model parameter to handleSendMessage and thread through to process spawn**

In `server/api/managed_sessions.go`, update the request struct in `handleSendMessage` (line 141):

```go
var req struct {
    Message string `json:"message"`
    ImageID string `json:"image_id"`
    Model   string `json:"model"`
}
```

Then, after the request is parsed (around line 205, before the goroutine), store the model so it can be used in `buildPersistentArgs`. The cleanest approach: pass the model through by temporarily setting it on the session object before calling `buildPersistentArgs`. Add a `Model` field to the `Session` struct (it won't be persisted — just used as a carrier).

In `server/db/sessions.go`, add `Model` field to the `Session` struct (after `CompactEveryNContinues`):

```go
Model                  string  `json:"model,omitempty"`
```

Note: This field is NOT in the DB — it's only used as a transient carrier for the per-turn model. Don't add it to `sessionColumns` or `scanSession`.

In `server/api/managed_sessions.go`, inside the `handleSendMessage` goroutine, before the `for` loop (around line 220), set the model on the session:

```go
sess.Model = req.Model
```

In `buildPersistentArgs`, add after the `max-budget-usd` block (after line 99):

```go
if sess.Model != "" {
    args = append(args, "--model", sess.Model)
}
```

- [ ] **Step 4: Update test to verify model flag is appended**

Update the test in `server/api/managed_sessions_test.go`:

```go
func TestBuildPersistentArgs_WithModel(t *testing.T) {
	sess := &db.Session{
		ID:       "test-id",
		MaxTurns: 10,
	}
	cfg := managed.Config{}

	// Without model — no --model flag
	args := buildPersistentArgs(sess, cfg)
	for _, a := range args {
		if a == "--model" {
			t.Error("unexpected --model flag without model set")
		}
	}

	// With model — --model flag present
	sess.Model = "claude-opus-4-6"
	args = buildPersistentArgs(sess, cfg)
	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "claude-opus-4-6" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --model claude-opus-4-6 in args, got: %v", args)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd server && go test ./api/ -v -run TestBuildPersistentArgs`
Expected: PASS

Run: `cd server && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/managed_sessions.go server/api/managed_sessions_test.go server/db/sessions.go
git commit -m "feat: add per-turn model flag to message API (#122)"
```

---

### Task 4: Add model selector to chat UI

**Files:**
- Modify: `server/web/static/app.js:113` (taskForm state), `1519-1562` (sendManagedMessage)
- Modify: `server/web/static/index.html:289-311` (chat input area), `1210-1242` (task form)

- [ ] **Step 1: Add selectedModel state to Alpine.js app**

In `server/web/static/app.js`, find the app state initialization (around line 36) and add:

```javascript
selectedModel: localStorage.getItem('claude-controller-model') || 'claude-sonnet-4-6',
```

- [ ] **Step 2: Add model to taskForm state**

In `server/web/static/app.js`, update the `taskForm` object (line 113):

```javascript
taskForm: { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '', model: '' },
```

- [ ] **Step 3: Add model dropdown to chat input area in HTML**

In `server/web/static/index.html`, add a model selector row just before the `<div class="input-row">` (before line 289). Insert:

```html
              <div x-show="currentSession?.mode === 'managed' && !shellMode" x-cloak style="display:flex; align-items:center; padding:0 0 6px 0;">
                <select x-model="selectedModel" @change="localStorage.setItem('claude-controller-model', selectedModel)"
                        style="background:var(--bg-secondary); color:var(--text-primary); border:1px solid var(--border); border-radius:6px; padding:3px 8px; font-size:0.75rem; cursor:pointer; outline:none;">
                  <option value="claude-opus-4-6">Opus</option>
                  <option value="claude-sonnet-4-6">Sonnet</option>
                  <option value="claude-haiku-4-5-20251001">Haiku</option>
                </select>
              </div>
```

- [ ] **Step 4: Send model with managed message**

In `server/web/static/app.js`, in the `sendManagedMessage` function (around line 1532), update the body construction:

```javascript
const body = { message: msg, model: this.selectedModel };
if (imageId) body.image_id = imageId;
```

- [ ] **Step 5: Add model dropdown to task creation form**

In `server/web/static/index.html`, add a model field to the task form after the cron expression field (after line 1241, before the error div):

```html
          <div class="settings-field" x-show="taskForm.task_type === 'claude'">
            <label class="modal-label">Model</label>
            <select x-model="taskForm.model" class="modal-input">
              <option value="">Default</option>
              <option value="claude-opus-4-6">Opus</option>
              <option value="claude-sonnet-4-6">Sonnet</option>
              <option value="claude-haiku-4-5-20251001">Haiku</option>
            </select>
          </div>
```

- [ ] **Step 6: Populate model when editing existing tasks**

In `server/web/static/app.js`, update the `openTaskModal` function (around line 3194). When populating `taskForm` from an existing task (line 3197-3201), add `model`:

```javascript
this.taskForm = {
    name: task.name, task_type: task.task_type, command: task.command,
    working_directory: task.working_directory, cron_expression: task.cron_expression,
    session_id: task.session_id || '', model: task.model || ''
};
```

And when creating a new task (line 3206), include `model`:

```javascript
this.taskForm = { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '', model: '' };
```

Also update `toggleTaskEnabled` (around line 3256) to include model in the PUT body:

```javascript
body: JSON.stringify({
    name: task.name, task_type: task.task_type, command: task.command,
    working_directory: task.working_directory, cron_expression: task.cron_expression,
    enabled: !task.enabled, model: task.model || ''
})
```

- [ ] **Step 7: Manual verification**

Run: `cd server && go run .`
Verify:
1. Model dropdown appears above chat input for managed sessions
2. Changing model persists across page reload (localStorage)
3. Model dropdown in task form only shows for "claude" task type
4. Sending a message includes the model in the request body (check network tab)

- [ ] **Step 8: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: add model selector dropdown to chat UI and task form (#122)"
```

---

### Task 5: Update CLAUDE.md and final verification

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add model selection spec/plan references to CLAUDE.md**

Add to the Spec & Plan section of `CLAUDE.md`:

```markdown
- Model selection spec: `docs/superpowers/specs/2026-04-22-model-selection-design.md`
- Model selection plan: `docs/superpowers/plans/2026-04-22-model-selection.md`
```

- [ ] **Step 2: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 3: Build to verify no compile errors**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add model selection spec/plan references to CLAUDE.md (#122)"
```
