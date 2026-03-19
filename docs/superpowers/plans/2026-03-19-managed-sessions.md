# Managed Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add managed sessions where the Go server spawns `claude -p` child processes, replacing the hook-based instruction queue with direct CLI control.

**Architecture:** New `managed` package handles process lifecycle (spawn, stream, interrupt). DB gets new migrations for managed session fields and messages table. API layer gets new endpoints that delegate to the managed package. Web UI gets new session creation and SSE streaming.

**Tech Stack:** Go 1.21+, SQLite, `exec.Command`, SSE, Alpine.js

**Spec:** `docs/superpowers/specs/2026-03-19-managed-sessions-design.md`

---

## File Structure

### New files
- `server/managed/manager.go` — `Manager` struct: in-memory process map, per-session mutex, spawn/interrupt/cleanup, broadcaster registry
- `server/managed/manager_test.go` — Unit tests for Manager
- `server/managed/stream.go` — NDJSON stdout reader with per-line callback, SSE fan-out broadcaster
- `server/managed/stream_test.go` — Tests for streaming and broadcaster
- `server/db/messages.go` — Messages table CRUD (Create, ListBySession)
- `server/db/messages_test.go` — Tests for messages DB layer
- `server/api/managed_sessions.go` — HTTP handlers for managed session endpoints
- `server/api/managed_sessions_test.go` — Tests for managed session handlers
- `.env.example` — Document CLAUDE_BIN, CLAUDE_ARGS, CLAUDE_ENV

### Modified files
- `server/db/db.go` — Add migrations: new columns on sessions, messages table, partial unique index
- `server/db/sessions.go` — Add `CreateManagedSession`, `GetSessionByID`, `SetInitialized`, update `Session` struct, update `ListSessions`/`getSessionByKey` scans, update `DeleteSession` to cascade messages
- `server/api/router.go` — Register new endpoints, update `Server` struct to hold `*managed.Manager`
- `server/main.go` — Load `.env` config, create `Manager`, pass to router
- `server/web/static/app.js` — New session UI, SSE per-session streaming, stop button
- `server/web/static/index.html` — New session modal, stop button in toolbar
- `.gitignore` — Add `.env`

---

## Task 1: Database migrations — new session fields and messages table

**Files:**
- Modify: `server/db/db.go:38-79` (migrate function)
- Modify: `server/db/sessions.go` (Session struct, scans, DeleteSession)
- Create: `server/db/messages.go`
- Create: `server/db/messages_test.go`

- [ ] **Step 1: Write failing test for CreateManagedSession**

Create `server/db/sessions_test.go` (or add to existing):

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestCreateManagedSession(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Bash","Read"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Mode != "managed" {
		t.Errorf("mode=%s, want managed", sess.Mode)
	}
	if sess.CWD != "/tmp/project" {
		t.Errorf("cwd=%s, want /tmp/project", sess.CWD)
	}
	if sess.Initialized {
		t.Error("new session should not be initialized")
	}

	// Duplicate cwd should fail
	_, err = store.CreateManagedSession("/tmp/project", `["Bash"]`, 50, 5.0)
	if err == nil {
		t.Error("expected error for duplicate cwd, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestCreateManagedSession`
Expected: FAIL — `CreateManagedSession` not defined

- [ ] **Step 3: Write failing test for messages**

```go
// server/db/messages_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestCreateAndListMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Bash","Read","Edit"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	msg1, err := store.CreateMessage(sess.ID, "user", `{"type":"user","content":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if msg1.Role != "user" || msg1.Seq != 1 {
		t.Errorf("msg1: role=%s seq=%d, want user/1", msg1.Role, msg1.Seq)
	}

	msg2, err := store.CreateMessage(sess.ID, "assistant", `{"type":"assistant","content":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if msg2.Seq != 2 {
		t.Errorf("msg2 seq=%d, want 2", msg2.Seq)
	}

	msgs, err := store.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Seq != 1 || msgs[1].Seq != 2 {
		t.Error("messages not ordered by seq")
	}
}

func TestDeleteSessionCascadesMessages(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.CreateManagedSession("/tmp/project", `["Read"]`, 50, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", "hi")

	err = store.DeleteSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	msgs, _ := store.ListMessages(sess.ID)
	if len(msgs) != 0 {
		t.Errorf("got %d messages after delete, want 0", len(msgs))
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd server && go test ./db/ -v -run TestCreateAndListMessages`
Expected: FAIL — `CreateMessage` not defined

- [ ] **Step 5: Add migrations to db.go**

Add these to the `migrations` slice in `server/db/db.go:39`:

```go
// Managed sessions — new columns on sessions
`ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'hook'`,
`ALTER TABLE sessions ADD COLUMN cwd TEXT`,
`ALTER TABLE sessions ADD COLUMN allowed_tools TEXT`,
`ALTER TABLE sessions ADD COLUMN max_turns INTEGER NOT NULL DEFAULT 50`,
`ALTER TABLE sessions ADD COLUMN max_budget_usd REAL NOT NULL DEFAULT 5.0`,
`ALTER TABLE sessions ADD COLUMN initialized INTEGER NOT NULL DEFAULT 0`,

// Partial unique index: one managed session per cwd
`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_cwd ON sessions(cwd) WHERE mode = 'managed'`,

// Messages table
`CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	seq INTEGER NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	exit_code INTEGER,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(session_id, seq)
)`,
`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq)`,
```

- [ ] **Step 6: Update Session struct and all scan functions in sessions.go**

Update the `Session` struct (keep `time.Time` for existing fields):

```go
type Session struct {
	ID             string    `json:"id"`
	ComputerName   string    `json:"computer_name"`
	ProjectPath    string    `json:"project_path"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	Archived       bool      `json:"archived"`
	Mode           string    `json:"mode"`
	CWD            string    `json:"cwd,omitempty"`
	AllowedTools   string    `json:"allowed_tools,omitempty"`
	MaxTurns       int       `json:"max_turns"`
	MaxBudgetUSD   float64   `json:"max_budget_usd"`
	Initialized    bool      `json:"initialized"`
}
```

Create a helper to scan all session columns consistently:

```go
// scanSession scans a full session row. Query must SELECT all columns in this order.
const sessionColumns = `id, computer_name, project_path, COALESCE(transcript_path,''), status, created_at, last_seen_at, archived, mode, COALESCE(cwd,''), COALESCE(allowed_tools,''), max_turns, max_budget_usd, initialized`

func scanSession(scanner interface{ Scan(...interface{}) error }) (Session, error) {
	var sess Session
	var archived, initialized int
	// COALESCE in sessionColumns guarantees non-NULL, so scan directly into strings
	err := scanner.Scan(
		&sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.TranscriptPath,
		&sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived,
		&sess.Mode, &sess.CWD, &sess.AllowedTools, &sess.MaxTurns, &sess.MaxBudgetUSD, &initialized,
	)
	if err != nil {
		return sess, err
	}
	sess.Archived = archived != 0
	sess.Initialized = initialized != 0
	return sess, nil
}
```

Update `getSessionByKey`:

```go
func (s *Store) getSessionByKey(computerName, projectPath string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE computer_name = ? AND project_path = ?`,
		computerName, projectPath)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}
```

Update `ListSessions`:

```go
func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := "SELECT " + sessionColumns + " FROM sessions"
	if !includeArchived {
		query += " WHERE archived = 0"
	}
	query += " ORDER BY last_seen_at DESC"

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}
```

Add `GetSessionByID`:

```go
func (s *Store) GetSessionByID(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
	}
	return &sess, nil
}
```

Add `CreateManagedSession` and `SetInitialized`:

```go
func (s *Store) CreateManagedSession(cwd, allowedTools string, maxTurns int, maxBudgetUSD float64) (*Session, error) {
	id := uuid.New().String()
	// Use "__managed__" as computer_name and cwd as project_path to avoid
	// colliding with the existing UNIQUE(computer_name, project_path) constraint.
	// Each managed session gets unique values because cwd is unique per managed session.
	_, err := s.db.Exec(`INSERT INTO sessions (id, computer_name, project_path, mode, cwd, allowed_tools, max_turns, max_budget_usd, status)
		VALUES (?, '__managed__', ?, 'managed', ?, ?, ?, ?, 'idle')`,
		id, cwd, cwd, allowedTools, maxTurns, maxBudgetUSD)
	if err != nil {
		return nil, fmt.Errorf("create managed session: %w", err)
	}
	return s.GetSessionByID(id)
}

func (s *Store) SetInitialized(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET initialized = 1 WHERE id = ?`, id)
	return err
}
```

Update `DeleteSession` to cascade messages:

```go
func (s *Store) DeleteSession(id string) error {
	s.db.Exec("DELETE FROM messages WHERE session_id = ?", id)
	s.db.Exec("DELETE FROM prompts WHERE session_id = ?", id)
	s.db.Exec("DELETE FROM instructions WHERE session_id = ?", id)
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}
```

- [ ] **Step 7: Implement messages.go**

```go
// server/db/messages.go
package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Note: time import needed for Message.CreatedAt (time.Time)

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Seq       int       `json:"seq"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateMessage(sessionID, role, content string) (*Message, error) {
	id := uuid.New().String()

	// Atomic insert with computed seq — avoids TOCTOU race
	_, err := s.db.Exec(`INSERT INTO messages (id, session_id, seq, role, content)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?), ?, ?)`,
		id, sessionID, sessionID, role, content)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	// Read back to get actual seq and created_at
	var msg Message
	err = s.db.QueryRow(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE id = ?`, id).
		Scan(&msg.ID, &msg.SessionID, &msg.Seq, &msg.Role, &msg.Content, &msg.ExitCode, &msg.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("read back message: %w", err)
	}
	return &msg, nil
}

func (s *Store) CreateMessageWithExitCode(sessionID, role, content string, exitCode int) (*Message, error) {
	id := uuid.New().String()

	_, err := s.db.Exec(`INSERT INTO messages (id, session_id, seq, role, content, exit_code)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?), ?, ?, ?)`,
		id, sessionID, sessionID, role, content, exitCode)
	if err != nil {
		return nil, fmt.Errorf("insert message with exit code: %w", err)
	}

	var msg Message
	err = s.db.QueryRow(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE id = ?`, id).
		Scan(&msg.ID, &msg.SessionID, &msg.Seq, &msg.Role, &msg.Content, &msg.ExitCode, &msg.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("read back message: %w", err)
	}
	return &msg, nil
}

func (s *Store) ListMessages(sessionID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, seq, role, content, exit_code, created_at FROM messages WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content, &m.ExitCode, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
```

- [ ] **Step 8: Run all DB tests**

Run: `cd server && go test ./db/ -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add server/db/db.go server/db/sessions.go server/db/messages.go server/db/messages_test.go server/db/sessions_test.go
git commit -m "feat: add managed sessions DB layer — migrations, messages table, CreateManagedSession"
```

---

## Task 2: Process manager and broadcaster — spawn, interrupt, cleanup, fan-out

**Files:**
- Create: `server/managed/manager.go`
- Create: `server/managed/manager_test.go`
- Create: `server/managed/stream.go`
- Create: `server/managed/stream_test.go`

- [ ] **Step 1: Write failing tests for Manager**

```go
// server/managed/manager_test.go
package managed

import (
	"testing"
	"time"
)

func TestManagerSpawnAndInterrupt(t *testing.T) {
	cfg := Config{
		ClaudeBin:  "sleep",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	m := NewManager(cfg)

	proc, err := m.Spawn("test-session-1", SpawnOpts{
		Args: []string{"60"},
		CWD:  "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil {
		t.Fatal("proc is nil")
	}
	if !m.IsRunning("test-session-1") {
		t.Error("session should be running")
	}

	// Second spawn should fail
	_, err = m.Spawn("test-session-1", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err == nil {
		t.Error("expected error for duplicate spawn")
	}

	// Interrupt
	err = m.Interrupt("test-session-1")
	if err != nil {
		t.Fatalf("interrupt failed: %v", err)
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after interrupt")
	}

	if m.IsRunning("test-session-1") {
		t.Error("session should not be running after interrupt")
	}
}

func TestManagerInterruptNonexistent(t *testing.T) {
	cfg := Config{ClaudeBin: "echo", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	err := m.Interrupt("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestManagerTeardown(t *testing.T) {
	cfg := Config{ClaudeBin: "sleep", ClaudeArgs: []string{}, ClaudeEnv: []string{}}
	m := NewManager(cfg)

	_, err := m.Spawn("sess-1", SpawnOpts{Args: []string{"60"}, CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	err = m.Teardown("sess-1", 2*time.Second)
	if err != nil {
		t.Fatalf("teardown failed: %v", err)
	}
	if m.IsRunning("sess-1") {
		t.Error("session should not be running after teardown")
	}
}
```

- [ ] **Step 2: Write failing tests for Broadcaster and StreamNDJSON**

```go
// server/managed/stream_test.go
package managed

import (
	"strings"
	"testing"
	"time"
)

func TestBroadcasterFanOut(t *testing.T) {
	b := NewBroadcaster()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Send("hello")

	select {
	case msg := <-ch1:
		if msg != "hello" {
			t.Errorf("ch1 got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch1 timed out")
	}

	select {
	case msg := <-ch2:
		if msg != "hello" {
			t.Errorf("ch2 got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch2 timed out")
	}

	b.Unsubscribe(ch1)
	b.Send("world")

	select {
	case msg := <-ch2:
		if msg != "world" {
			t.Errorf("ch2 got %q, want world", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch2 timed out")
	}

	select {
	case <-ch1:
		t.Error("ch1 should not receive after unsubscribe")
	case <-time.After(100 * time.Millisecond):
		// expected
	}

	b.Close()
}

func TestBroadcasterClose(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	b.Close()

	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after broadcaster close")
	}
}

func TestStreamNDJSON(t *testing.T) {
	input := `{"type":"system","subtype":"init"}
{"type":"assistant","content":"hello"}
{"type":"result","cost":0.01}
`
	b := NewBroadcaster()

	var persisted []string
	onLine := func(line string) {
		persisted = append(persisted, line)
	}

	lines := StreamNDJSON(strings.NewReader(input), b, onLine)
	b.Close()

	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
	if len(persisted) != 3 {
		t.Errorf("got %d persisted, want 3", len(persisted))
	}
	// Verify onLine callback was called with correct content
	if !strings.Contains(persisted[0], "system") {
		t.Errorf("first line should contain 'system', got %s", persisted[0])
	}
}

func TestStreamNDJSONCountsAssistantTurns(t *testing.T) {
	input := `{"type":"assistant","content":"turn 1"}
{"type":"tool_use","name":"Read"}
{"type":"tool_result","output":"file contents"}
{"type":"assistant","content":"turn 2"}
{"type":"assistant","content":"turn 3"}
`
	b := NewBroadcaster()

	var turnCount int
	onLine := func(line string) {
		// Count assistant turns (same logic the handler will use)
		if strings.Contains(line, `"type":"assistant"`) {
			turnCount++
		}
	}

	StreamNDJSON(strings.NewReader(input), b, onLine)
	b.Close()

	if turnCount != 3 {
		t.Errorf("turnCount=%d, want 3", turnCount)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd server && go test ./managed/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 4: Implement stream.go**

```go
// server/managed/stream.go
package managed

import (
	"bufio"
	"io"
	"log"
	"sync"
)

// Broadcaster fans out string messages to multiple subscribers.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan string]struct{}
	closed      bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan string]struct{}),
	}
}

func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[ch] = struct{}{}
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
	close(ch)
}

func (b *Broadcaster) Send(msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			log.Printf("warning: dropping message for slow subscriber")
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
}

// StreamNDJSON reads NDJSON lines from r, broadcasts each via b, and calls onLine for each.
// onLine is called synchronously per line (use for persistence/turn counting).
// Returns all lines read. Blocks until r is closed/EOF.
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string)) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)
		b.Send(line)
		if onLine != nil {
			onLine(line)
		}
	}
	return lines
}
```

- [ ] **Step 5: Implement manager.go (includes broadcaster registry)**

```go
// server/managed/manager.go
package managed

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	ClaudeBin  string
	ClaudeArgs []string
	ClaudeEnv  []string
}

type SpawnOpts struct {
	Args []string
	CWD  string
}

type Process struct {
	Cmd      *exec.Cmd
	Stdout   io.ReadCloser
	Stderr   io.ReadCloser
	Done     chan struct{}
	ExitCode int
}

type Manager struct {
	cfg          Config
	mu           sync.Mutex
	procs        map[string]*Process
	broadcasters map[string]*Broadcaster
	mutexes      map[string]*sync.Mutex
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:          cfg,
		procs:        make(map[string]*Process),
		broadcasters: make(map[string]*Broadcaster),
		mutexes:      make(map[string]*sync.Mutex),
	}
}

func (m *Manager) sessionMutex(sessionID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mutexes[sessionID]; !ok {
		m.mutexes[sessionID] = &sync.Mutex{}
	}
	return m.mutexes[sessionID]
}

func (m *Manager) GetBroadcaster(sessionID string) *Broadcaster {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.broadcasters[sessionID]; !ok {
		m.broadcasters[sessionID] = NewBroadcaster()
	}
	return m.broadcasters[sessionID]
}

func (m *Manager) Spawn(sessionID string, opts SpawnOpts) (*Process, error) {
	mu := m.sessionMutex(sessionID)
	mu.Lock()
	defer mu.Unlock()

	if _, running := m.procs[sessionID]; running {
		return nil, fmt.Errorf("session %s already has a running process", sessionID)
	}

	args := append(m.cfg.ClaudeArgs, opts.Args...)
	cmd := exec.Command(m.cfg.ClaudeBin, args...)
	cmd.Dir = opts.CWD
	cmd.Env = append(os.Environ(), m.cfg.ClaudeEnv...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	proc := &Process{
		Cmd:    cmd,
		Stdout: stdout,
		Stderr: stderr,
		Done:   make(chan struct{}),
	}

	m.mu.Lock()
	m.procs[sessionID] = proc
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		if cmd.ProcessState != nil {
			proc.ExitCode = cmd.ProcessState.ExitCode()
		}
		m.mu.Lock()
		delete(m.procs, sessionID)
		m.mu.Unlock()
		close(proc.Done)
	}()

	return proc, nil
}

func (m *Manager) IsRunning(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[sessionID]
	return ok
}

func (m *Manager) Interrupt(sessionID string) error {
	m.mu.Lock()
	proc, ok := m.procs[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running process for session %s", sessionID)
	}
	return proc.Cmd.Process.Signal(syscall.SIGINT)
}

func (m *Manager) Teardown(sessionID string, timeout time.Duration) error {
	if !m.IsRunning(sessionID) {
		return nil
	}
	if err := m.Interrupt(sessionID); err != nil {
		return err
	}

	m.mu.Lock()
	proc := m.procs[sessionID]
	m.mu.Unlock()
	if proc == nil {
		return nil
	}

	select {
	case <-proc.Done:
		return nil
	case <-time.After(timeout):
		proc.Cmd.Process.Kill()
		<-proc.Done
		return nil
	}
}
```

- [ ] **Step 6: Run all managed package tests**

Run: `cd server && go test ./managed/ -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add server/managed/manager.go server/managed/manager_test.go server/managed/stream.go server/managed/stream_test.go
git commit -m "feat: add process manager and SSE broadcaster with NDJSON streaming"
```

---

## Task 3: API handlers — create, message, interrupt, delete, stream, messages

**Files:**
- Create: `server/api/managed_sessions.go`
- Modify: `server/api/router.go`
- Modify: `server/main.go`
- Create: `.env.example`
- Modify: `.gitignore`

- [ ] **Step 1: Create .env.example and update .gitignore**

`.env.example`:
```
# Claude Code binary (default: claude)
CLAUDE_BIN=claude

# Additional CLI flags (space-separated, no spaces in values)
CLAUDE_ARGS=--dangerously-skip-permissions

# Environment variables for child process (comma-separated KEY=VALUE)
CLAUDE_ENV=CLAUDE_CONFIG_DIR=/Users/jay/.claud-bb
```

Add to `.gitignore`:
```
.env
```

- [ ] **Step 2: Add config loading and Manager creation to main.go**

Add import:
```go
"github.com/jaychinthrajah/claude-controller/server/managed"
```

Add these helper functions to `main.go`:

```go
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

In `main()`, after `apiKey := loadOrCreateAPIKey(*dbPath)` (line 56), add:

```go
loadDotEnv(".env")
managedCfg := managed.Config{
	ClaudeBin:  envOrDefault("CLAUDE_BIN", "claude"),
	ClaudeArgs: strings.Fields(os.Getenv("CLAUDE_ARGS")),
	ClaudeEnv:  splitEnv(os.Getenv("CLAUDE_ENV")),
}
mgr := managed.NewManager(managedCfg)
```

Add helper:
```go
func splitEnv(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
```

Change `router := api.NewRouter(store, apiKey)` to:
```go
router := api.NewRouter(store, apiKey, mgr)
```

- [ ] **Step 3: Update Server struct and NewRouter in router.go**

Add import:
```go
"github.com/jaychinthrajah/claude-controller/server/managed"
```

Update struct:
```go
type Server struct {
	store   *db.Store
	manager *managed.Manager
}
```

Update signature:
```go
func NewRouter(store *db.Store, apiKey string, mgr *managed.Manager) http.Handler {
	s := &Server{store: store, manager: mgr}
```

Add new routes inside `NewRouter`, after existing routes:

```go
// Managed session endpoints
apiMux.HandleFunc("POST /api/sessions/create", s.handleCreateManagedSession)
apiMux.HandleFunc("POST /api/sessions/{id}/message", s.handleSendMessage)
apiMux.HandleFunc("POST /api/sessions/{id}/interrupt", s.handleInterrupt)
apiMux.HandleFunc("GET /api/sessions/{id}/messages", s.handleListMessages)
```

Add per-session SSE (handles own auth, like `/api/events`):

```go
root.HandleFunc("GET /api/sessions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
	s.handleSessionStream(w, r, apiKey)
})
```

- [ ] **Step 4: Implement managed_sessions.go**

```go
// server/api/managed_sessions.go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func (s *Server) handleCreateManagedSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CWD          string  `json:"cwd"`
		AllowedTools string  `json:"allowed_tools"`
		MaxTurns     int     `json:"max_turns"`
		MaxBudgetUSD float64 `json:"max_budget_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.CWD == "" {
		http.Error(w, "cwd is required", http.StatusBadRequest)
		return
	}
	if req.AllowedTools == "" {
		req.AllowedTools = `["Bash","Read","Edit","Write","Glob","Grep"]`
	}
	if req.MaxTurns == 0 {
		req.MaxTurns = 50
	}
	if req.MaxBudgetUSD == 0 {
		req.MaxBudgetUSD = 5.0
	}

	sess, err := s.store.CreateManagedSession(req.CWD, req.AllowedTools, req.MaxTurns, req.MaxBudgetUSD)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "session already exists for this directory", http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}

	// Build claude args
	var args []string
	args = append(args, "-p", req.Message)

	if sess.Initialized {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}

	args = append(args, "--output-format", "stream-json")

	if sess.AllowedTools != "" {
		var tools []string
		json.Unmarshal([]byte(sess.AllowedTools), &tools)
		if len(tools) > 0 {
			args = append(args, "--allowedTools", strings.Join(tools, ","))
		}
	}

	if sess.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", sess.MaxBudgetUSD))
	}

	proc, err := s.manager.Spawn(sessionID, managed.SpawnOpts{
		Args: args,
		CWD:  sess.CWD,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	if !sess.Initialized {
		s.store.SetInitialized(sessionID)
	}

	s.store.CreateMessage(sessionID, "user", req.Message)
	s.store.SetSessionStatus(sessionID, "running")

	broadcaster := s.manager.GetBroadcaster(sessionID)

	// Background: stream stdout, persist inline, cleanup
	go func() {
		turnCount := 0

		onLine := func(line string) {
			// Persist each line as it arrives
			role := parseRole(line)
			s.store.CreateMessage(sessionID, role, line)

			// Turn counting
			if role == "assistant" {
				turnCount++
				if turnCount >= sess.MaxTurns {
					log.Printf("session %s hit turn limit (%d), interrupting", sessionID, sess.MaxTurns)
					s.manager.Interrupt(sessionID)
				}
			}
		}

		managed.StreamNDJSON(proc.Stdout, broadcaster, onLine)

		// Drain stderr
		stderrBytes, _ := io.ReadAll(proc.Stderr)

		<-proc.Done

		if proc.ExitCode != 0 && len(stderrBytes) > 0 {
			errMsg := fmt.Sprintf(`{"type":"system","error":true,"stderr":%q,"exit_code":%d}`, string(stderrBytes), proc.ExitCode)
			s.store.CreateMessageWithExitCode(sessionID, "system", errMsg, proc.ExitCode)
			broadcaster.Send(errMsg)
		}

		doneMsg := fmt.Sprintf(`{"type":"done","exit_code":%d}`, proc.ExitCode)
		broadcaster.Send(doneMsg)

		s.store.SetSessionStatus(sessionID, "idle")
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if err := s.manager.Interrupt(sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "interrupted"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	msgs, err := s.store.ListMessages(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []db.Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request, apiKey string) {
	token := r.URL.Query().Get("token")
	if token != apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	broadcaster := s.manager.GetBroadcaster(sessionID)
	ch := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(ch)

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func parseRole(line string) string {
	var obj struct {
		Type string `json:"type"`
	}
	json.Unmarshal([]byte(line), &obj)
	if obj.Type == "" {
		return "system"
	}
	return obj.Type
}
```

- [ ] **Step 5: Verify /api/events SSE works for managed sessions**

The existing `/api/events` endpoint calls `ListSessions()` and `ListPrompts()` every 3 seconds. Since we updated `ListSessions` in Task 1 to include the new `mode`, `cwd`, `status` fields via `scanSession`, managed sessions will automatically appear in the global SSE stream. No code changes needed — the schema update flows through.

Verify: After Task 1's `ListSessions` changes, managed sessions with `status: "idle"` or `status: "running"` will be broadcast to all connected web UI clients via the existing 3-second polling loop in `events.go`.

- [ ] **Step 6: Update handleDeleteSession in sessions.go to teardown managed processes**

In `server/api/sessions.go`, at the top of `handleDeleteSession`, before the store delete call, add:

```go
// Teardown managed process if running
if s.manager != nil {
	s.manager.Teardown(id, 5*time.Second)
}
```

Add `"time"` to imports if not present.

- [ ] **Step 7: Verify the server compiles**

Run: `cd server && go build ./...`
Expected: Compiles without errors

- [ ] **Step 8: Commit**

```bash
git add server/api/managed_sessions.go server/api/router.go server/api/sessions.go server/main.go .env.example .gitignore
git commit -m "feat: add managed session API — create, message, interrupt, stream, delete with turn counting"
```

---

## Task 4: Integration tests for managed session API

**Files:**
- Create: `server/api/managed_sessions_test.go`

- [ ] **Step 1: Write integration tests**

```go
// server/api/managed_sessions_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

func setupTestServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}

	cfg := managed.Config{
		ClaudeBin:  "echo",
		ClaudeArgs: []string{},
		ClaudeEnv:  []string{},
	}
	mgr := managed.NewManager(cfg)
	router := NewRouter(store, "test-api-key", mgr)
	ts := httptest.NewServer(router)
	return ts, store
}

func TestCreateManagedSessionAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"cwd": "/tmp/test-project"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	if sess["mode"] != "managed" {
		t.Errorf("mode=%v, want managed", sess["mode"])
	}
	if sess["cwd"] != "/tmp/test-project" {
		t.Errorf("cwd=%v, want /tmp/test-project", sess["cwd"])
	}
}

func TestCreateDuplicateManagedSession(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	body := `{"cwd": "/tmp/test-project"}`

	req1, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req1.Header.Set("Authorization", "Bearer test-api-key")
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := http.DefaultClient.Do(req1)
	resp1.Body.Close()

	req2, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer test-api-key")
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := http.DefaultClient.Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != 409 {
		t.Errorf("status=%d, want 409 for duplicate cwd", resp2.StatusCode)
	}
}

func TestListMessagesAPI(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/test", `["Read"]`, 50, 5.0)
	store.CreateMessage(sess.ID, "user", "hello")
	store.CreateMessage(sess.ID, "assistant", `{"type":"assistant","content":"hi"}`)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var msgs []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 2 {
		t.Errorf("got %d messages, want 2", len(msgs))
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd server && go test ./api/ -v -run TestCreateManagedSession`
Run: `cd server && go test ./api/ -v -run TestListMessages`
Expected: All PASS

- [ ] **Step 3: Run full test suite**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add server/api/managed_sessions_test.go
git commit -m "test: add integration tests for managed sessions API"
```

---

## Task 5: Web UI — new session creation and managed session chat

**Files:**
- Modify: `server/web/static/index.html`
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add "New Session" button and modal to index.html**

Add to the session list header area in the sidebar:

```html
<button @click="showNewSessionModal = true"
        style="width:100%; padding:8px; margin-bottom:8px; background:var(--accent); color:white; border:none; border-radius:6px; cursor:pointer; font-size:13px;">
  + New Session
</button>
```

Add modal overlay before closing `</body>`:

```html
<div x-show="showNewSessionModal"
     style="position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,0.5); z-index:100; display:flex; align-items:center; justify-content:center;"
     @click.self="showNewSessionModal = false">
  <div style="background:var(--bg); border-radius:12px; padding:24px; width:400px; max-width:90vw;">
    <h3 style="margin-top:0;">New Managed Session</h3>
    <label style="display:block; margin-bottom:12px;">
      <span style="font-size:13px; color:var(--dim);">Project Directory</span>
      <input x-model="newSessionCWD" type="text" placeholder="/path/to/project"
             style="width:100%; padding:8px; margin-top:4px; background:var(--input-bg); border:1px solid var(--border); border-radius:6px; color:var(--text); box-sizing:border-box;">
    </label>
    <div style="display:flex; gap:8px; justify-content:flex-end;">
      <button @click="showNewSessionModal = false"
              style="padding:8px 16px; background:var(--hover); border:none; border-radius:6px; cursor:pointer; color:var(--text);">Cancel</button>
      <button @click="createManagedSession()"
              style="padding:8px 16px; background:var(--accent); color:white; border:none; border-radius:6px; cursor:pointer;">Create</button>
    </div>
  </div>
</div>
```

- [ ] **Step 2: Add Stop button to chat toolbar**

```html
<button x-show="currentSession?.mode === 'managed' && currentSession?.status === 'running'"
        @click="interruptSession()"
        style="padding:6px 12px; background:#e74c3c; color:white; border:none; border-radius:6px; cursor:pointer; font-size:13px;">
  Stop
</button>
```

- [ ] **Step 3: Add mode badge to session list items**

```html
<span x-show="session.mode === 'managed'"
      style="font-size:10px; background:var(--accent); color:white; padding:1px 4px; border-radius:3px; margin-left:4px;">managed</span>
```

- [ ] **Step 4: Add managed session state and methods to app.js**

Add to Alpine `data()`:

```js
showNewSessionModal: false,
newSessionCWD: '',
sessionSSE: null,
```

Add methods:

```js
async createManagedSession() {
  try {
    const res = await fetch('/api/sessions/create', {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ cwd: this.newSessionCWD })
    });
    if (!res.ok) throw new Error(await res.text());
    this.showNewSessionModal = false;
    this.newSessionCWD = '';
    this.showToast('Session created');
  } catch (e) {
    this.showToast('Error: ' + e.message);
  }
},

async sendManagedMessage() {
  if (!this.responseText.trim() || !this.selectedSession) return;
  const msg = this.responseText.trim();
  this.responseText = '';

  try {
    const res = await fetch(`/api/sessions/${this.selectedSession}/message`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: msg })
    });
    if (!res.ok) throw new Error(await res.text());

    this.chatMessages.push({ role: 'user', content: msg });
    this.scrollToBottom();
    this.startSessionSSE(this.selectedSession);
  } catch (e) {
    this.showToast('Error: ' + e.message);
  }
},

startSessionSSE(sessionId) {
  this.stopSessionSSE();
  const url = `/api/sessions/${sessionId}/stream?token=${encodeURIComponent(this.apiKey)}`;
  this.sessionSSE = new EventSource(url);

  this.sessionSSE.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.type === 'done') {
        this.stopSessionSSE();
        return;
      }
      this.chatMessages.push({ role: data.type || 'assistant', content: event.data, raw: true });
      this.scrollToBottom();
    } catch (e) {
      this.chatMessages.push({ role: 'assistant', content: event.data, raw: true });
      this.scrollToBottom();
    }
  };

  this.sessionSSE.onerror = () => {
    this.stopSessionSSE();
  };
},

stopSessionSSE() {
  if (this.sessionSSE) {
    this.sessionSSE.close();
    this.sessionSSE = null;
  }
},

async interruptSession() {
  if (!this.selectedSession) return;
  try {
    await fetch(`/api/sessions/${this.selectedSession}/interrupt`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    this.showToast('Session interrupted');
  } catch (e) {
    this.showToast('Error: ' + e.message);
  }
},

async fetchManagedMessages(sessionId) {
  try {
    const res = await fetch(`/api/sessions/${sessionId}/messages`, {
      headers: { 'Authorization': 'Bearer ' + this.apiKey }
    });
    if (!res.ok) return;
    const msgs = await res.json();
    this.chatMessages = (msgs || []).map(m => ({
      role: m.role,
      content: m.content,
      raw: m.role !== 'user'
    }));
    this.scrollToBottom();
  } catch (e) {
    console.error('Failed to fetch messages:', e);
  }
},
```

- [ ] **Step 5: Update selectSession to handle managed mode**

```js
async selectSession(id) {
  this.selectedSession = id;
  this.stopSessionSSE();

  const sess = this.sessions.find(s => s.id === id);
  if (sess && sess.mode === 'managed') {
    await this.fetchManagedMessages(id);
    if (sess.status === 'running') {
      this.startSessionSSE(id);
    }
  } else {
    await this.fetchTranscript(id);
  }
},
```

- [ ] **Step 6: Update submit handler to route based on session mode**

Update the textarea submit logic:

```js
async handleSubmit() {
  const sess = this.currentSession;
  if (sess && sess.mode === 'managed') {
    await this.sendManagedMessage();
  } else {
    // existing hook-mode logic
    if (this.currentPendingPrompt) {
      await this.respondToPrompt(this.currentPendingPrompt.id, this.responseText);
    } else {
      await this.sendInstruction();
    }
  }
},
```

- [ ] **Step 7: Verify server compiles with embedded static files**

Run: `cd server && go build ./...`
Expected: Compiles

- [ ] **Step 8: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat: web UI for managed sessions — create, stream, interrupt, message history"
```

---

## Task 6: Manual smoke test and final verification

- [ ] **Step 1: Create a .env file**

```bash
cp .env.example .env
# Edit with actual values
```

- [ ] **Step 2: Build and run**

Run: `cd server && go build -o claude-controller . && ./claude-controller`

- [ ] **Step 3: Smoke test the full flow**

1. Open web UI at the printed URL
2. Create a managed session pointing to this repo's directory
3. Send "What files are in the root of this project?"
4. Verify streaming response appears in real-time
5. Send a follow-up — verify context is maintained (`--resume`)
6. Click Stop during a long response — verify interrupt
7. Send `/compact` — verify it works as a regular message
8. Delete the session — verify cleanup

- [ ] **Step 4: Run all tests**

Run: `cd server && go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: address issues found during smoke testing"
```
