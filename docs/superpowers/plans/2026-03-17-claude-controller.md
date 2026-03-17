# Claude Controller Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a system to remotely control Claude Code sessions from an iPhone via a local Go server, Claude Code hooks, and a SwiftUI app.

**Architecture:** Go server on localhost exposes a REST API (SQLite storage), tunneled via ngrok. Claude Code hooks (bash/PowerShell) POST events to the server and long-poll for responses. iOS app connects via ngrok URL (QR code pairing) to view prompts and send responses.

**Tech Stack:** Go 1.22+, SQLite (mattn/go-sqlite3), ngrok-go, Swift/SwiftUI (iOS 17+), bash, PowerShell

**Spec:** `docs/superpowers/specs/2026-03-17-claude-controller-design.md`

---

## File Structure

```
claude-controller/
├── server/
│   ├── main.go                  # CLI flags, startup orchestration, QR display
│   ├── main_test.go             # Integration test: startup + health check
│   ├── go.mod
│   ├── go.sum
│   ├── db/
│   │   ├── db.go                # Open DB, run migrations, WAL mode
│   │   ├── db_test.go           # Migration + WAL mode tests
│   │   ├── sessions.go          # Session CRUD (upsert, list, heartbeat, archive)
│   │   ├── sessions_test.go
│   │   ├── prompts.go           # Prompt CRUD (create, respond, list, get response)
│   │   ├── prompts_test.go
│   │   ├── instructions.go      # Instruction CRUD (queue, fetch, mark delivered)
│   │   └── instructions_test.go
│   ├── api/
│   │   ├── router.go            # Route registration, server creation
│   │   ├── middleware.go         # Auth bearer token, rate limiting, IP lockout
│   │   ├── middleware_test.go
│   │   ├── sessions.go          # POST /register, POST /heartbeat, GET /sessions
│   │   ├── sessions_test.go
│   │   ├── prompts.go           # POST /prompts, GET /response (long-poll), POST /respond, GET list
│   │   ├── prompts_test.go
│   │   ├── instructions.go      # POST /instruct, GET /instructions
│   │   ├── instructions_test.go
│   │   ├── pairing.go           # GET /pairing, GET /status
│   │   └── pairing_test.go
│   └── tunnel/
│       ├── tunnel.go            # ngrok tunnel start, URL retrieval
│       └── tunnel_test.go
├── hooks/
│   ├── stop.sh                  # macOS Stop hook
│   ├── stop.ps1                 # Windows Stop hook
│   ├── notify.sh                # macOS Notification hook
│   ├── notify.ps1               # Windows Notification hook
│   └── test/
│       └── hook_test.sh         # Integration test with mock server
├── ios/
│   └── ClaudeController/
│       ├── ClaudeController.xcodeproj
│       ├── ClaudeControllerApp.swift        # App entry point, scene routing
│       ├── Models/
│       │   ├── Session.swift                # Session Codable model
│       │   ├── Prompt.swift                 # Prompt Codable model
│       │   └── ServerConfig.swift           # Pairing config (URL + key)
│       ├── Services/
│       │   ├── APIClient.swift              # HTTP client, auth header, all endpoints
│       │   ├── KeychainService.swift        # Store/retrieve server configs
│       │   └── PollingService.swift         # Adaptive polling timer
│       └── Views/
│           ├── PairingView.swift            # QR scanner + manual URL entry
│           ├── MainView.swift               # Session selector + prompt queue
│           ├── PromptCardView.swift         # Single prompt card (pending vs answered)
│           ├── InstructionSheet.swift       # New instruction modal
│           └── SettingsView.swift           # Paired servers, archive management
└── docs/
```

---

## Task 1: Go Module + SQLite Database Layer

**Files:**
- Create: `server/go.mod`
- Create: `server/db/db.go`
- Create: `server/db/db_test.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd server && go mod init github.com/jaychinthrajah/claude-controller/server
```

- [ ] **Step 2: Add SQLite dependency**

```bash
cd server && go get github.com/mattn/go-sqlite3
```

- [ ] **Step 3: Write failing test for DB initialization**

Create `server/db/db_test.go`:

```go
package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesTablesAndWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Verify WAL mode
	var journalMode string
	err = store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", journalMode)
	}

	// Verify tables exist
	tables := []string{"sessions", "prompts", "instructions"}
	for _, table := range tables {
		var name string
		err := store.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}

	// Verify unique constraint on sessions
	_, err = store.db.Exec(
		"INSERT INTO sessions (id, computer_name, project_path, status, created_at, last_seen_at, archived) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), 0)",
		"id1", "mac1", "/project", "active",
	)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	_, err = store.db.Exec(
		"INSERT INTO sessions (id, computer_name, project_path, status, created_at, last_seen_at, archived) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), 0)",
		"id2", "mac1", "/project", "active",
	)
	if err == nil {
		t.Error("expected unique constraint violation, got nil")
	}

	// Verify file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

```bash
cd server && go test ./db/ -v -run TestOpenCreatesTablesAndWAL
```

Expected: FAIL — `Open` not defined.

- [ ] **Step 5: Implement db.Open**

Create `server/db/db.go`:

```go
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			computer_name TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
			archived INTEGER NOT NULL DEFAULT 0,
			UNIQUE(computer_name, project_path)
		)`,
		`CREATE TABLE IF NOT EXISTS prompts (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			claude_message TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'prompt',
			response TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			answered_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS instructions (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			message TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			delivered_at DATETIME
		)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 6: Run test to verify it passes**

```bash
cd server && go test ./db/ -v -run TestOpenCreatesTablesAndWAL
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add server/go.mod server/go.sum server/db/db.go server/db/db_test.go
git commit -m "feat: add SQLite database layer with WAL mode and migrations"
```

---

## Task 2: Session CRUD Operations

**Files:**
- Create: `server/db/sessions.go`
- Create: `server/db/sessions_test.go`

- [ ] **Step 1: Write failing tests for session operations**

Create `server/db/sessions_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestUpsertSession(t *testing.T) {
	store := newTestStore(t)

	// First upsert creates
	s1, err := store.UpsertSession("mac1", "/project/a")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if s1.ComputerName != "mac1" || s1.ProjectPath != "/project/a" {
		t.Errorf("unexpected session: %+v", s1)
	}

	// Second upsert returns same ID, updates last_seen_at
	s2, err := store.UpsertSession("mac1", "/project/a")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if s2.ID != s1.ID {
		t.Errorf("expected same ID %s, got %s", s1.ID, s2.ID)
	}

	// Different project creates new session
	s3, err := store.UpsertSession("mac1", "/project/b")
	if err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	if s3.ID == s1.ID {
		t.Error("expected different ID for different project")
	}
}

func TestListSessions(t *testing.T) {
	store := newTestStore(t)

	store.UpsertSession("mac1", "/project/a")
	store.UpsertSession("mac1", "/project/b")

	sessions, err := store.ListSessions(false)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestArchiveSession(t *testing.T) {
	store := newTestStore(t)

	s, _ := store.UpsertSession("mac1", "/project/a")
	err := store.SetArchived(s.ID, true)
	if err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	// Archived sessions excluded by default
	sessions, _ := store.ListSessions(false)
	if len(sessions) != 0 {
		t.Errorf("expected 0 non-archived sessions, got %d", len(sessions))
	}

	// Included when requested
	sessions, _ = store.ListSessions(true)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session with archived, got %d", len(sessions))
	}
}

func TestHeartbeat(t *testing.T) {
	store := newTestStore(t)

	s, _ := store.UpsertSession("mac1", "/project/a")
	err := store.Heartbeat(s.ID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Heartbeat for nonexistent session
	err = store.Heartbeat("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./db/ -v -run "TestUpsertSession|TestListSessions|TestArchiveSession|TestHeartbeat"
```

Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement session operations**

Create `server/db/sessions.go`:

```go
package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID           string    `json:"id"`
	ComputerName string    `json:"computer_name"`
	ProjectPath  string    `json:"project_path"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Archived     bool      `json:"archived"`
}

func (s *Store) UpsertSession(computerName, projectPath string) (*Session, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, computer_name, project_path, status, created_at, last_seen_at, archived)
		VALUES (?, ?, ?, 'active', datetime('now'), datetime('now'), 0)
		ON CONFLICT(computer_name, project_path) DO UPDATE SET
			last_seen_at = datetime('now'),
			status = 'active'
	`, id, computerName, projectPath)
	if err != nil {
		return nil, fmt.Errorf("upsert session: %w", err)
	}

	return s.getSessionByKey(computerName, projectPath)
}

func (s *Store) getSessionByKey(computerName, projectPath string) (*Session, error) {
	var sess Session
	var archived int
	err := s.db.QueryRow(`
		SELECT id, computer_name, project_path, status, created_at, last_seen_at, archived
		FROM sessions WHERE computer_name = ? AND project_path = ?
	`, computerName, projectPath).Scan(
		&sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.Status,
		&sess.CreatedAt, &sess.LastSeenAt, &archived,
	)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.Archived = archived != 0
	return &sess, nil
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := "SELECT id, computer_name, project_path, status, created_at, last_seen_at, archived FROM sessions"
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
		var sess Session
		var archived int
		if err := rows.Scan(&sess.ID, &sess.ComputerName, &sess.ProjectPath, &sess.Status, &sess.CreatedAt, &sess.LastSeenAt, &archived); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.Archived = archived != 0
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Heartbeat(id string) error {
	res, err := s.db.Exec("UPDATE sessions SET last_seen_at = datetime('now') WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func (s *Store) SetArchived(id string, archived bool) error {
	val := 0
	if archived {
		val = 1
	}
	_, err := s.db.Exec("UPDATE sessions SET archived = ? WHERE id = ?", val, id)
	return err
}

func (s *Store) SetSessionStatus(id, status string) error {
	_, err := s.db.Exec("UPDATE sessions SET status = ? WHERE id = ?", status, id)
	return err
}
```

- [ ] **Step 4: Add uuid dependency**

```bash
cd server && go get github.com/google/uuid
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd server && go test ./db/ -v -run "TestUpsertSession|TestListSessions|TestArchiveSession|TestHeartbeat"
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/db/sessions.go server/db/sessions_test.go server/go.mod server/go.sum
git commit -m "feat: add session CRUD with upsert, list, heartbeat, archive"
```

---

## Task 3: Prompt CRUD Operations

**Files:**
- Create: `server/db/prompts.go`
- Create: `server/db/prompts_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/db/prompts_test.go`:

```go
package db

import (
	"testing"
)

func TestCreateAndGetPrompt(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	p, err := store.CreatePrompt(sess.ID, "Which DB?", "prompt")
	if err != nil {
		t.Fatalf("CreatePrompt: %v", err)
	}
	if p.ClaudeMessage != "Which DB?" || p.Status != "pending" || p.Type != "prompt" {
		t.Errorf("unexpected prompt: %+v", p)
	}
}

func TestRespondToPrompt(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	p, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	err := store.RespondToPrompt(p.ID, "SQLite")
	if err != nil {
		t.Fatalf("RespondToPrompt: %v", err)
	}

	response, err := store.GetPromptResponse(p.ID)
	if err != nil {
		t.Fatalf("GetPromptResponse: %v", err)
	}
	if response == nil || *response != "SQLite" {
		t.Errorf("expected 'SQLite', got %v", response)
	}
}

func TestGetPromptResponsePending(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	p, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	response, err := store.GetPromptResponse(p.ID)
	if err != nil {
		t.Fatalf("GetPromptResponse: %v", err)
	}
	if response != nil {
		t.Errorf("expected nil for pending prompt, got %v", response)
	}
}

func TestListPrompts(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	store.CreatePrompt(sess.ID, "Q1", "prompt")
	store.CreatePrompt(sess.ID, "Q2", "prompt")
	store.CreatePrompt(sess.ID, "Done", "notification")

	// All prompts for session
	prompts, err := store.ListPrompts(sess.ID, "")
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 3 {
		t.Errorf("expected 3, got %d", len(prompts))
	}

	// Only pending prompts (not notifications)
	prompts, _ = store.ListPrompts("", "pending")
	// All 3 have status "pending" since that's the default.
	// Filter by type for prompt-only: tested via the API layer.
	if len(prompts) != 3 {
		t.Errorf("expected 3 pending, got %d", len(prompts))
	}

	// Respond to one and verify count changes
	store.RespondToPrompt(prompts[0].ID, "answer")
	prompts, _ = store.ListPrompts("", "pending")
	if len(prompts) != 2 {
		t.Errorf("expected 2 pending after responding, got %d", len(prompts))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./db/ -v -run "TestCreateAndGetPrompt|TestRespondToPrompt|TestGetPromptResponsePending|TestListPrompts"
```

Expected: FAIL

- [ ] **Step 3: Implement prompt operations**

Create `server/db/prompts.go`:

```go
package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Prompt struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	ClaudeMessage string     `json:"claude_message"`
	Type          string     `json:"type"`
	Response      *string    `json:"response"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	AnsweredAt    *time.Time `json:"answered_at"`
}

func (s *Store) CreatePrompt(sessionID, claudeMessage, promptType string) (*Prompt, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO prompts (id, session_id, claude_message, type, status, created_at)
		VALUES (?, ?, ?, ?, 'pending', datetime('now'))
	`, id, sessionID, claudeMessage, promptType)
	if err != nil {
		return nil, fmt.Errorf("create prompt: %w", err)
	}

	return s.GetPromptByID(id)
}

func (s *Store) GetPromptByID(id string) (*Prompt, error) {
	var p Prompt
	err := s.db.QueryRow(`
		SELECT id, session_id, claude_message, type, response, status, created_at, answered_at
		FROM prompts WHERE id = ?
	`, id).Scan(&p.ID, &p.SessionID, &p.ClaudeMessage, &p.Type, &p.Response, &p.Status, &p.CreatedAt, &p.AnsweredAt)
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	return &p, nil
}

func (s *Store) RespondToPrompt(id, response string) error {
	res, err := s.db.Exec(`
		UPDATE prompts SET response = ?, status = 'answered', answered_at = datetime('now')
		WHERE id = ? AND status = 'pending'
	`, response, id)
	if err != nil {
		return fmt.Errorf("respond to prompt: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("prompt %s not found or already answered", id)
	}
	return nil
}

func (s *Store) GetPromptResponse(id string) (*string, error) {
	var response *string
	err := s.db.QueryRow("SELECT response FROM prompts WHERE id = ?", id).Scan(&response)
	if err != nil {
		return nil, fmt.Errorf("get response: %w", err)
	}
	return response, nil
}

func (s *Store) ListPrompts(sessionID, status string) ([]Prompt, error) {
	query := "SELECT id, session_id, claude_message, type, response, status, created_at, answered_at FROM prompts WHERE 1=1"
	var args []interface{}

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	defer rows.Close()

	var prompts []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.SessionID, &p.ClaudeMessage, &p.Type, &p.Response, &p.Status, &p.CreatedAt, &p.AnsweredAt); err != nil {
			return nil, fmt.Errorf("scan prompt: %w", err)
		}
		prompts = append(prompts, p)
	}
	return prompts, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && go test ./db/ -v -run "TestCreateAndGetPrompt|TestRespondToPrompt|TestGetPromptResponsePending|TestListPrompts"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/prompts.go server/db/prompts_test.go
git commit -m "feat: add prompt CRUD with create, respond, list, get response"
```

---

## Task 4: Instruction CRUD Operations

**Files:**
- Create: `server/db/instructions.go`
- Create: `server/db/instructions_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/db/instructions_test.go`:

```go
package db

import (
	"testing"
)

func TestQueueAndFetchInstruction(t *testing.T) {
	store := newTestStore(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	instr, err := store.QueueInstruction(sess.ID, "Run the tests")
	if err != nil {
		t.Fatalf("QueueInstruction: %v", err)
	}
	if instr.Message != "Run the tests" || instr.Status != "queued" {
		t.Errorf("unexpected instruction: %+v", instr)
	}

	// Fetch queued instruction
	fetched, err := store.FetchNextInstruction(sess.ID)
	if err != nil {
		t.Fatalf("FetchNextInstruction: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected instruction, got nil")
	}
	if fetched.Message != "Run the tests" || fetched.Status != "delivered" {
		t.Errorf("unexpected fetched: %+v", fetched)
	}

	// No more instructions
	fetched2, err := store.FetchNextInstruction(sess.ID)
	if err != nil {
		t.Fatalf("FetchNextInstruction: %v", err)
	}
	if fetched2 != nil {
		t.Errorf("expected nil, got %+v", fetched2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./db/ -v -run TestQueueAndFetchInstruction
```

Expected: FAIL

- [ ] **Step 3: Implement instruction operations**

Create `server/db/instructions.go`:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Instruction struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	Message     string     `json:"message"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
}

func (s *Store) QueueInstruction(sessionID, message string) (*Instruction, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO instructions (id, session_id, message, status, created_at)
		VALUES (?, ?, ?, 'queued', datetime('now'))
	`, id, sessionID, message)
	if err != nil {
		return nil, fmt.Errorf("queue instruction: %w", err)
	}

	var instr Instruction
	err = s.db.QueryRow(`
		SELECT id, session_id, message, status, created_at, delivered_at
		FROM instructions WHERE id = ?
	`, id).Scan(&instr.ID, &instr.SessionID, &instr.Message, &instr.Status, &instr.CreatedAt, &instr.DeliveredAt)
	if err != nil {
		return nil, fmt.Errorf("get instruction: %w", err)
	}
	return &instr, nil
}

func (s *Store) FetchNextInstruction(sessionID string) (*Instruction, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var instr Instruction
	err = tx.QueryRow(`
		SELECT id, session_id, message, status, created_at, delivered_at
		FROM instructions WHERE session_id = ? AND status = 'queued'
		ORDER BY created_at ASC LIMIT 1
	`, sessionID).Scan(&instr.ID, &instr.SessionID, &instr.Message, &instr.Status, &instr.CreatedAt, &instr.DeliveredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch instruction: %w", err)
	}

	// Mark as delivered within same transaction
	_, err = tx.Exec(`
		UPDATE instructions SET status = 'delivered', delivered_at = datetime('now') WHERE id = ?
	`, instr.ID)
	if err != nil {
		return nil, fmt.Errorf("mark delivered: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	instr.Status = "delivered"
	return &instr, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd server && go test ./db/ -v -run TestQueueAndFetchInstruction
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/instructions.go server/db/instructions_test.go
git commit -m "feat: add instruction queue with FIFO fetch and delivered marking"
```

---

## Task 5: API Middleware (Auth + Rate Limiting)

**Files:**
- Create: `server/api/middleware.go`
- Create: `server/api/middleware_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/api/middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareValidToken(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	rl := NewRateLimiter(60, 10)
	handler := AuthMiddleware("test-key", rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, 1) // 5 req/min, 1 lockout attempt
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, 100) // 2 req/min
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i < 2 && rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
		if i == 2 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request %d: expected 429, got %d", i, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./api/ -v -run "TestAuthMiddleware|TestRateLimiter"
```

Expected: FAIL

- [ ] **Step 3: Implement middleware**

Create `server/api/middleware.go`:

```go
package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func AuthMiddleware(apiKey string, rl *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		// Check if IP is locked out from too many failed auth attempts
		if rl.IsLockedOut(ip) {
			http.Error(w, `{"error":"too many failed attempts, try again later"}`, http.StatusTooManyRequests)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			rl.RecordFailedAuth(ip)
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != apiKey {
			rl.RecordFailedAuth(ip)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type RateLimiter struct {
	mu             sync.Mutex
	requests       map[string][]time.Time
	failedAuths    map[string][]time.Time
	maxPerMinute   int
	maxFailedAuths int
	lockoutMinutes int
}

func NewRateLimiter(maxPerMinute, maxFailedAuths int) *RateLimiter {
	return &RateLimiter{
		requests:       make(map[string][]time.Time),
		failedAuths:    make(map[string][]time.Time),
		maxPerMinute:   maxPerMinute,
		maxFailedAuths: maxFailedAuths,
		lockoutMinutes: 15,
	}
}

func (rl *RateLimiter) RecordFailedAuth(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.failedAuths[ip] = append(rl.failedAuths[ip], time.Now())
}

func (rl *RateLimiter) IsLockedOut(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(rl.lockoutMinutes) * time.Minute)
	var recent []time.Time
	for _, t := range rl.failedAuths[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.failedAuths[ip] = recent
	return len(recent) >= rl.maxFailedAuths
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		rl.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-1 * time.Minute)

		// Clean old entries
		var recent []time.Time
		for _, t := range rl.requests[ip] {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}

		if len(recent) >= rl.maxPerMinute {
			rl.mu.Unlock()
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		rl.requests[ip] = append(recent, now)
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && go test ./api/ -v -run "TestAuthMiddleware|TestRateLimiter"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/middleware.go server/api/middleware_test.go
git commit -m "feat: add auth middleware and rate limiter"
```

---

## Task 6: Session API Handlers

**Files:**
- Create: `server/api/router.go`
- Create: `server/api/sessions.go`
- Create: `server/api/sessions_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/api/sessions_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

func newTestServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	router := NewRouter(store, "test-key")
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store
}

func authReq(method, url string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, url, &buf)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestRegisterSession(t *testing.T) {
	ts, _ := newTestServer(t)

	body := map[string]string{"computer_name": "mac1", "project_path": "/proj"}
	req := authReq("POST", ts.URL+"/api/sessions/register", body)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)
	if session["computer_name"] != "mac1" {
		t.Errorf("unexpected: %v", session)
	}
}

func TestListSessions(t *testing.T) {
	ts, store := newTestServer(t)
	store.UpsertSession("mac1", "/proj/a")
	store.UpsertSession("mac1", "/proj/b")

	req := authReq("GET", ts.URL+"/api/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var sessions []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./api/ -v -run "TestRegisterSession|TestListSessions"
```

Expected: FAIL — `NewRouter` not defined.

- [ ] **Step 3: Implement router and session handlers**

Create `server/api/router.go`:

```go
package api

import (
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type Server struct {
	store *db.Store
}

func NewRouter(store *db.Store, apiKey string) http.Handler {
	s := &Server{store: store}
	mux := http.NewServeMux()

	// Session endpoints
	mux.HandleFunc("POST /api/sessions/register", s.handleRegisterSession)
	mux.HandleFunc("POST /api/sessions/{id}/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("PUT /api/sessions/{id}/archive", s.handleSetArchived)

	// Prompt endpoints (added in Task 7)

	// Pairing/status endpoints
	mux.HandleFunc("GET /api/pairing", s.handlePairing)
	mux.HandleFunc("GET /api/status", s.handleStatus)

	rl := NewRateLimiter(60, 10)
	return rl.Middleware(AuthMiddleware(apiKey, rl, mux))
}
```

Create `server/api/sessions.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

type registerRequest struct {
	ComputerName string `json:"computer_name"`
	ProjectPath  string `json:"project_path"`
}

func (s *Server) handleRegisterSession(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	session, err := s.store.UpsertSession(req.ComputerName, req.ProjectPath)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Heartbeat(id); err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	sessions, err := s.store.ListSessions(includeArchived)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []db.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

type archiveRequest struct {
	Archived bool `json:"archived"`
}

func (s *Server) handleSetArchived(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req archiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if err := s.store.SetArchived(id, req.Archived); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

```

Create `server/api/pairing.go`:

```go
package api

import "net/http"

func (s *Server) handlePairing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"paired":true}`))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
```

Note: import the `db` package with alias if needed. The `db` import path is `github.com/jaychinthrajah/claude-controller/server/db`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && go test ./api/ -v -run "TestRegisterSession|TestListSessions"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/router.go server/api/sessions.go server/api/sessions_test.go server/api/pairing.go
git commit -m "feat: add session API handlers with register, heartbeat, list, archive, pairing"
```

---

## Task 7: Prompt API Handlers (Including Long-Poll)

**Files:**
- Create: `server/api/prompts.go`
- Create: `server/api/prompts_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/api/prompts_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCreatePrompt(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	body := map[string]string{"session_id": sess.ID, "claude_message": "Which DB?", "type": "prompt"}
	req := authReq("POST", ts.URL+"/api/prompts", body)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var prompt map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&prompt)
	if prompt["claude_message"] != "Which DB?" {
		t.Errorf("unexpected: %v", prompt)
	}
}

func TestRespondAndGetResponse(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	prompt, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	// Respond
	body := map[string]string{"response": "SQLite"}
	req := authReq("POST", ts.URL+"/api/prompts/"+prompt.ID+"/respond", body)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("respond: expected 200, got %d", resp.StatusCode)
	}

	// Get response (should return immediately since already answered)
	req = authReq("GET", ts.URL+"/api/prompts/"+prompt.ID+"/response", nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get response: expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "SQLite" {
		t.Errorf("expected 'SQLite', got %v", result["response"])
	}
}

func TestLongPollTimeout(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	prompt, _ := store.CreatePrompt(sess.ID, "Which DB?", "prompt")

	// Long-poll with short timeout should return pending
	start := time.Now()
	req := authReq("GET", ts.URL+"/api/prompts/"+prompt.ID+"/response?timeout=1", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected ~1s wait, got %v", elapsed)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "pending" {
		t.Errorf("expected 'pending', got %v", result["status"])
	}
}

func TestListPendingPrompts(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")
	store.CreatePrompt(sess.ID, "Q1", "prompt")
	store.CreatePrompt(sess.ID, "Q2", "prompt")

	req := authReq("GET", ts.URL+"/api/prompts?status=pending", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var prompts []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&prompts)
	if len(prompts) != 2 {
		t.Errorf("expected 2, got %d", len(prompts))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./api/ -v -run "TestCreatePrompt|TestRespondAndGetResponse|TestLongPollTimeout|TestListPendingPrompts"
```

Expected: FAIL

- [ ] **Step 3: Implement prompt handlers**

Create `server/api/prompts.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type createPromptRequest struct {
	SessionID     string `json:"session_id"`
	ClaudeMessage string `json:"claude_message"`
	Type          string `json:"type"`
}

func (s *Server) handleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	var req createPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Update session status to waiting
	s.store.SetSessionStatus(req.SessionID, "waiting")

	prompt, err := s.store.CreatePrompt(req.SessionID, req.ClaudeMessage, req.Type)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prompt)
}

func (s *Server) handleGetPromptResponse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Parse timeout (default 30s, for testing allow override via query param)
	timeoutSec := 30
	if t := r.URL.Query().Get("timeout"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 && v <= 30 {
			timeoutSec = v
		}
	}

	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	for time.Now().Before(deadline) {
		response, err := s.store.GetPromptResponse(id)
		if err != nil {
			http.Error(w, `{"error":"prompt not found"}`, http.StatusNotFound)
			return
		}
		if response != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "answered",
				"response": *response,
			})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timeout — return pending
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
}

type respondRequest struct {
	Response string `json:"response"`
}

func (s *Server) handleRespondToPrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req respondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.RespondToPrompt(id, req.Response); err != nil {
		http.Error(w, `{"error":"prompt not found or already answered"}`, http.StatusNotFound)
		return
	}

	// Get prompt to find session ID and reset session status to active
	prompt, err := s.store.GetPromptByID(id)
	if err == nil && prompt != nil {
		s.store.SetSessionStatus(prompt.SessionID, "active")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	status := r.URL.Query().Get("status")

	prompts, err := s.store.ListPrompts(sessionID, status)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if prompts == nil {
		prompts = []db.Prompt{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prompts)
}
```

- [ ] **Step 4: Register prompt routes in router.go**

Add to `NewRouter` in `server/api/router.go`, after the session endpoints comment:

```go
	// Prompt endpoints
	mux.HandleFunc("POST /api/prompts", s.handleCreatePrompt)
	mux.HandleFunc("GET /api/prompts/{id}/response", s.handleGetPromptResponse)
	mux.HandleFunc("POST /api/prompts/{id}/respond", s.handleRespondToPrompt)
	mux.HandleFunc("GET /api/prompts", s.handleListPrompts)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd server && go test ./api/ -v -run "TestCreatePrompt|TestRespondAndGetResponse|TestLongPollTimeout|TestListPendingPrompts"
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/prompts.go server/api/prompts_test.go server/api/router.go
git commit -m "feat: add prompt API handlers with long-polling response endpoint"
```

---

## Task 8: Instruction API Handlers

**Files:**
- Create: `server/api/instructions.go`
- Create: `server/api/instructions_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/api/instructions_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestQueueAndFetchInstruction(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	// Queue instruction from iOS
	body := map[string]string{"message": "Run tests"}
	req := authReq("POST", ts.URL+"/api/sessions/"+sess.ID+"/instruct", body)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("instruct: expected 200, got %d", resp.StatusCode)
	}

	// Fetch from hook
	req = authReq("GET", ts.URL+"/api/sessions/"+sess.ID+"/instructions", nil)
	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var instr map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&instr)
	if instr["message"] != "Run tests" {
		t.Errorf("expected 'Run tests', got %v", instr)
	}
}

func TestFetchInstructionEmpty(t *testing.T) {
	ts, store := newTestServer(t)
	sess, _ := store.UpsertSession("mac1", "/proj")

	req := authReq("GET", ts.URL+"/api/sessions/"+sess.ID+"/instructions", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./api/ -v -run "TestQueueAndFetchInstruction$|TestFetchInstructionEmpty"
```

Expected: FAIL

- [ ] **Step 3: Implement instruction handlers**

Create `server/api/instructions.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

type instructRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleInstruct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req instructRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	instr, err := s.store.QueueInstruction(id, req.Message)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instr)
}

func (s *Server) handleFetchInstructions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	instr, err := s.store.FetchNextInstruction(id)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if instr == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instr)
}
```

- [ ] **Step 4: Register instruction routes in router.go**

Add to `NewRouter` in `server/api/router.go`:

```go
	// Instruction endpoints
	mux.HandleFunc("POST /api/sessions/{id}/instruct", s.handleInstruct)
	mux.HandleFunc("GET /api/sessions/{id}/instructions", s.handleFetchInstructions)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd server && go test ./api/ -v -run "TestQueueAndFetchInstruction$|TestFetchInstructionEmpty"
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/instructions.go server/api/instructions_test.go server/api/router.go
git commit -m "feat: add instruction queue API with instruct and fetch endpoints"
```

---

## Task 9: ngrok Tunnel + QR Code + main.go

**Files:**
- Create: `server/tunnel/tunnel.go`
- Create: `server/main.go`

- [ ] **Step 1: Add dependencies**

```bash
cd server && go get golang.ngrok.com/ngrok && go get github.com/skip2/go-qrcode
```

- [ ] **Step 2: Implement tunnel wrapper**

Create `server/tunnel/tunnel.go`:

```go
package tunnel

import (
	"context"
	"fmt"
	"net"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

type Tunnel struct {
	listener net.Listener
	url      string
}

func Start(ctx context.Context) (*Tunnel, error) {
	listener, err := ngrok.Listen(ctx, config.HTTPEndpoint())
	if err != nil {
		return nil, fmt.Errorf("ngrok listen: %w", err)
	}
	return &Tunnel{listener: listener, url: listener.Addr().String()}, nil
}

func (t *Tunnel) URL() string {
	return t.url
}

func (t *Tunnel) Listener() net.Listener {
	return t.listener
}

func (t *Tunnel) Close() error {
	return t.listener.Close()
}
```

- [ ] **Step 3: Implement main.go**

Create `server/main.go`:

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/jaychinthrajah/claude-controller/server/api"
	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/tunnel"
)

func main() {
	port := flag.Int("port", 0, "port to listen on (default: 8080, auto-detect if occupied)")
	dbPath := flag.String("db", "", "path to SQLite database (default: ~/.claude-controller/data.db)")
	flag.Parse()

	if *port == 0 {
		if p := os.Getenv("PORT"); p != "" {
			v, err := strconv.Atoi(p)
			if err == nil {
				*port = v
			}
		}
		if *port == 0 {
			*port = findAvailablePort(8080)
		}
	}

	if *dbPath == "" {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".claude-controller")
		os.MkdirAll(dir, 0755)
		*dbPath = filepath.Join(dir, "data.db")
	}

	store, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()

	apiKey := loadOrCreateAPIKey(*dbPath)

	router := api.NewRouter(store, apiKey)

	// Start local server
	addr := fmt.Sprintf("localhost:%d", *port)
	localListener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	fmt.Printf("Local server listening on %s\n", addr)

	// Start ngrok tunnel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun, err := tunnel.Start(ctx)
	if err != nil {
		log.Printf("Warning: ngrok tunnel failed: %v", err)
		log.Printf("Server is running locally only at http://%s", addr)
		log.Printf("To expose via ngrok, set NGROK_AUTHTOKEN environment variable")
	} else {
		defer tun.Close()
		// ngrok-go's Addr().String() may or may not include scheme
		ngrokURL := tun.URL()
		if !strings.HasPrefix(ngrokURL, "https://") {
			ngrokURL = "https://" + ngrokURL
		}
		fmt.Printf("ngrok tunnel: %s\n", ngrokURL)
		displayQRCode(ngrokURL, apiKey)

		// Serve on ngrok listener too
		go http.Serve(tun.Listener(), router)
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		localListener.Close()
	}()

	http.Serve(localListener, router)
}

func findAvailablePort(preferred int) int {
	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", preferred))
	if err == nil {
		l.Close()
		return preferred
	}
	// Find random available port
	l, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		return preferred
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func loadOrCreateAPIKey(dbPath string) string {
	keyFile := filepath.Join(filepath.Dir(dbPath), "api.key")
	data, err := os.ReadFile(keyFile)
	if err == nil && len(data) > 0 {
		return string(data)
	}

	key := generateAPIKey()
	os.WriteFile(keyFile, []byte(key), 0600)
	return key
}

func generateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "sk-" + hex.EncodeToString(b)
}

func displayQRCode(ngrokURL, apiKey string) {
	payload := map[string]interface{}{
		"url":     ngrokURL,
		"key":     apiKey,
		"version": 1,
	}
	jsonData, _ := json.Marshal(payload)

	qr, err := qrcode.New(string(jsonData), qrcode.Medium)
	if err != nil {
		log.Printf("Failed to generate QR code: %v", err)
		fmt.Printf("\nPairing payload: %s\n", jsonData)
		return
	}

	fmt.Println("\n--- Scan this QR code with the Claude Controller iOS app ---")
	fmt.Println(qr.ToSmallString(false))
	fmt.Printf("Pairing payload: %s\n\n", jsonData)
}
```

- [ ] **Step 4: Verify it compiles**

```bash
cd server && go build -o /dev/null .
```

Expected: Compiles without errors.

- [ ] **Step 5: Commit**

```bash
git add server/tunnel/tunnel.go server/main.go server/go.mod server/go.sum
git commit -m "feat: add ngrok tunnel, QR code display, and main entry point"
```

---

## Task 10: macOS Hook Scripts (bash)

**Files:**
- Create: `hooks/stop.sh`
- Create: `hooks/notify.sh`

- [ ] **Step 1: Create macOS Stop hook**

Create `hooks/stop.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Claude Controller - Stop Hook (macOS)
# Reads Claude's stop event, posts to local server, long-polls for response.

# Read JSON from stdin
INPUT=$(cat)

# Parse fields
HOOK_EVENT=$(echo "$INPUT" | jq -r '.hook_event_name // ""')
STOP_HOOK_ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
CWD=$(echo "$INPUT" | jq -r '.cwd // ""')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
TRANSCRIPT_PATH=$(echo "$INPUT" | jq -r '.transcript_path // ""')

# Load config
CONFIG_FILE="$HOME/.claude-controller.json"
if [[ ! -f "$CONFIG_FILE" ]]; then
    exit 0  # No config, exit silently
fi

SERVER_URL=$(jq -r '.server_url // "http://localhost:8080"' "$CONFIG_FILE")
COMPUTER_NAME=$(jq -r '.computer_name // ""' "$CONFIG_FILE")
if [[ -z "$COMPUTER_NAME" ]]; then
    COMPUTER_NAME=$(hostname -s 2>/dev/null || hostname)
fi

# Check if server is reachable
if ! curl -sf --max-time 2 "$SERVER_URL/api/status" -H "Authorization: Bearer $(jq -r '.api_key // ""' "$CONFIG_FILE")" > /dev/null 2>&1; then
    exit 0  # Server not running, exit silently
fi

API_KEY=$(jq -r '.api_key // ""' "$CONFIG_FILE")
AUTH_HEADER="Authorization: Bearer $API_KEY"

# Register session (upsert)
REGISTER_RESP=$(curl -sf --max-time 5 \
    -X POST "$SERVER_URL/api/sessions/register" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"computer_name\": \"$COMPUTER_NAME\", \"project_path\": \"$CWD\"}" 2>/dev/null) || exit 0

SERVER_SESSION_ID=$(echo "$REGISTER_RESP" | jq -r '.id')

if [[ "$STOP_HOOK_ACTIVE" == "true" ]]; then
    # Claude is already continuing from a previous stop hook.
    # Check for queued instructions only.
    INSTR_RESP=$(curl -sf --max-time 5 \
        -X GET "$SERVER_URL/api/sessions/$SERVER_SESSION_ID/instructions" \
        -H "$AUTH_HEADER" 2>/dev/null)

    if [[ $? -eq 0 && -n "$INSTR_RESP" ]]; then
        INSTR_MSG=$(echo "$INSTR_RESP" | jq -r '.message // ""')
        if [[ -n "$INSTR_MSG" ]]; then
            echo "{\"decision\": \"block\", \"reason\": \"User instruction: $INSTR_MSG\"}"
            exit 0
        fi
    fi
    # No instruction queued, let Claude stop normally
    exit 0
fi

# Normal stop: extract Claude's last message from transcript
CLAUDE_MSG=""
if [[ -n "$TRANSCRIPT_PATH" && -f "$TRANSCRIPT_PATH" ]]; then
    # Get last assistant message from JSONL transcript
    CLAUDE_MSG=$(tail -20 "$TRANSCRIPT_PATH" | jq -rs '
        [.[] | select(.type == "assistant")] | last | .message.content |
        if type == "array" then [.[] | select(.type == "text") | .text] | join("\n")
        elif type == "string" then .
        else ""
        end
    ' 2>/dev/null || echo "")
fi

if [[ -z "$CLAUDE_MSG" ]]; then
    CLAUDE_MSG="Claude is waiting for input"
fi

# Escape JSON special characters in the message
CLAUDE_MSG_ESCAPED=$(echo "$CLAUDE_MSG" | jq -Rs '.')

# Post prompt to server
PROMPT_RESP=$(curl -sf --max-time 5 \
    -X POST "$SERVER_URL/api/prompts" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"session_id\": \"$SERVER_SESSION_ID\", \"claude_message\": $CLAUDE_MSG_ESCAPED, \"type\": \"prompt\"}" 2>/dev/null) || exit 0

PROMPT_ID=$(echo "$PROMPT_RESP" | jq -r '.id')

# Long-poll for response (indefinitely)
while true; do
    POLL_RESP=$(curl -sf --max-time 35 \
        -X GET "$SERVER_URL/api/prompts/$PROMPT_ID/response" \
        -H "$AUTH_HEADER" 2>/dev/null) || continue

    POLL_STATUS=$(echo "$POLL_RESP" | jq -r '.status // "pending"')

    if [[ "$POLL_STATUS" == "answered" ]]; then
        RESPONSE=$(echo "$POLL_RESP" | jq -r '.response // ""')
        RESPONSE_ESCAPED=$(echo "$RESPONSE" | jq -Rs '.' | sed 's/^"//;s/"$//')
        echo "{\"decision\": \"block\", \"reason\": \"User responded: $RESPONSE_ESCAPED\"}"
        exit 0
    fi

    # Still pending, retry
    sleep 1
done
```

- [ ] **Step 2: Create macOS Notification hook**

Create `hooks/notify.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Claude Controller - Notification Hook (macOS)
# Fire-and-forget: posts notification to server.

INPUT=$(cat)

MESSAGE=$(echo "$INPUT" | jq -r '.message // ""')
CWD=$(echo "$INPUT" | jq -r '.cwd // ""')

CONFIG_FILE="$HOME/.claude-controller.json"
if [[ ! -f "$CONFIG_FILE" ]]; then
    exit 0
fi

SERVER_URL=$(jq -r '.server_url // "http://localhost:8080"' "$CONFIG_FILE")
COMPUTER_NAME=$(jq -r '.computer_name // ""' "$CONFIG_FILE")
API_KEY=$(jq -r '.api_key // ""' "$CONFIG_FILE")

if [[ -z "$COMPUTER_NAME" ]]; then
    COMPUTER_NAME=$(hostname -s 2>/dev/null || hostname)
fi

AUTH_HEADER="Authorization: Bearer $API_KEY"

# Check server reachable
if ! curl -sf --max-time 2 "$SERVER_URL/api/status" -H "$AUTH_HEADER" > /dev/null 2>&1; then
    exit 0
fi

# Register session
REGISTER_RESP=$(curl -sf --max-time 5 \
    -X POST "$SERVER_URL/api/sessions/register" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"computer_name\": \"$COMPUTER_NAME\", \"project_path\": \"$CWD\"}" 2>/dev/null) || exit 0

SERVER_SESSION_ID=$(echo "$REGISTER_RESP" | jq -r '.id')
MESSAGE_ESCAPED=$(echo "$MESSAGE" | jq -Rs '.')

# Post notification (fire and forget)
curl -sf --max-time 5 \
    -X POST "$SERVER_URL/api/prompts" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"session_id\": \"$SERVER_SESSION_ID\", \"claude_message\": $MESSAGE_ESCAPED, \"type\": \"notification\"}" > /dev/null 2>&1 || true

exit 0
```

- [ ] **Step 3: Make scripts executable**

```bash
chmod +x hooks/stop.sh hooks/notify.sh
```

- [ ] **Step 4: Commit**

```bash
git add hooks/stop.sh hooks/notify.sh
git commit -m "feat: add macOS Claude Code hook scripts (stop + notification)"
```

---

## Task 11: Windows Hook Scripts (PowerShell)

**Files:**
- Create: `hooks/stop.ps1`
- Create: `hooks/notify.ps1`

- [ ] **Step 1: Create Windows Stop hook**

Create `hooks/stop.ps1`:

```powershell
# Claude Controller - Stop Hook (Windows)
# Reads Claude's stop event, posts to local server, long-polls for response.

$ErrorActionPreference = "SilentlyContinue"

$input_data = $input | Out-String | ConvertFrom-Json
if (-not $input_data) { exit 0 }

$hook_event = $input_data.hook_event_name
$stop_hook_active = $input_data.stop_hook_active
$cwd = $input_data.cwd
$transcript_path = $input_data.transcript_path

# Load config
$config_path = Join-Path $env:USERPROFILE ".claude-controller.json"
if (-not (Test-Path $config_path)) { exit 0 }

$config = Get-Content $config_path | ConvertFrom-Json
$server_url = if ($config.server_url) { $config.server_url } else { "http://localhost:8080" }
$computer_name = if ($config.computer_name) { $config.computer_name } else { $env:COMPUTERNAME }
$api_key = $config.api_key

$headers = @{
    "Authorization" = "Bearer $api_key"
    "Content-Type" = "application/json"
}

# Check server reachable
try {
    Invoke-RestMethod -Uri "$server_url/api/status" -Headers $headers -TimeoutSec 2 | Out-Null
} catch {
    exit 0
}

# Register session
$register_body = @{ computer_name = $computer_name; project_path = $cwd } | ConvertTo-Json
try {
    $session = Invoke-RestMethod -Method Post -Uri "$server_url/api/sessions/register" -Headers $headers -Body $register_body -TimeoutSec 5
} catch {
    exit 0
}

$session_id = $session.id

if ($stop_hook_active -eq $true) {
    # Check for queued instructions only
    try {
        $instr = Invoke-RestMethod -Uri "$server_url/api/sessions/$session_id/instructions" -Headers $headers -TimeoutSec 5
        if ($instr -and $instr.message) {
            $result = @{ decision = "block"; reason = "User instruction: $($instr.message)" } | ConvertTo-Json -Compress
            Write-Output $result
            exit 0
        }
    } catch { }
    exit 0
}

# Extract Claude's last message from transcript
$claude_msg = "Claude is waiting for input"
if ($transcript_path -and (Test-Path $transcript_path)) {
    try {
        $lines = Get-Content $transcript_path -Tail 20
        foreach ($line in ($lines | Sort-Object -Descending)) {
            $entry = $line | ConvertFrom-Json
            if ($entry.type -eq "assistant" -and $entry.message.content) {
                $content = $entry.message.content
                if ($content -is [array]) {
                    $claude_msg = ($content | Where-Object { $_.type -eq "text" } | ForEach-Object { $_.text }) -join "`n"
                } elseif ($content -is [string]) {
                    $claude_msg = $content
                }
                break
            }
        }
    } catch { }
}

# Post prompt
$prompt_body = @{ session_id = $session_id; claude_message = $claude_msg; type = "prompt" } | ConvertTo-Json
try {
    $prompt = Invoke-RestMethod -Method Post -Uri "$server_url/api/prompts" -Headers $headers -Body $prompt_body -TimeoutSec 5
} catch {
    exit 0
}

$prompt_id = $prompt.id

# Long-poll for response
while ($true) {
    try {
        $poll = Invoke-RestMethod -Uri "$server_url/api/prompts/$prompt_id/response" -Headers $headers -TimeoutSec 35
        if ($poll.status -eq "answered") {
            $response = $poll.response
            $result = @{ decision = "block"; reason = "User responded: $response" } | ConvertTo-Json -Compress
            Write-Output $result
            exit 0
        }
    } catch { }
    Start-Sleep -Seconds 1
}
```

- [ ] **Step 2: Create Windows Notification hook**

Create `hooks/notify.ps1`:

```powershell
# Claude Controller - Notification Hook (Windows)
# Fire-and-forget: posts notification to server.

$ErrorActionPreference = "SilentlyContinue"

$input_data = $input | Out-String | ConvertFrom-Json
if (-not $input_data) { exit 0 }

$message = $input_data.message
$cwd = $input_data.cwd

$config_path = Join-Path $env:USERPROFILE ".claude-controller.json"
if (-not (Test-Path $config_path)) { exit 0 }

$config = Get-Content $config_path | ConvertFrom-Json
$server_url = if ($config.server_url) { $config.server_url } else { "http://localhost:8080" }
$computer_name = if ($config.computer_name) { $config.computer_name } else { $env:COMPUTERNAME }
$api_key = $config.api_key

$headers = @{
    "Authorization" = "Bearer $api_key"
    "Content-Type" = "application/json"
}

try {
    Invoke-RestMethod -Uri "$server_url/api/status" -Headers $headers -TimeoutSec 2 | Out-Null
} catch {
    exit 0
}

$register_body = @{ computer_name = $computer_name; project_path = $cwd } | ConvertTo-Json
try {
    $session = Invoke-RestMethod -Method Post -Uri "$server_url/api/sessions/register" -Headers $headers -Body $register_body -TimeoutSec 5
} catch {
    exit 0
}

$prompt_body = @{ session_id = $session.id; claude_message = $message; type = "notification" } | ConvertTo-Json
try {
    Invoke-RestMethod -Method Post -Uri "$server_url/api/prompts" -Headers $headers -Body $prompt_body -TimeoutSec 5 | Out-Null
} catch { }

exit 0
```

- [ ] **Step 3: Commit**

```bash
git add hooks/stop.ps1 hooks/notify.ps1
git commit -m "feat: add Windows Claude Code hook scripts (stop + notification)"
```

---

## Task 12: iOS App — Xcode Project + Models

**Files:**
- Create: `ios/ClaudeController/ClaudeControllerApp.swift`
- Create: `ios/ClaudeController/Models/Session.swift`
- Create: `ios/ClaudeController/Models/Prompt.swift`
- Create: `ios/ClaudeController/Models/ServerConfig.swift`

- [ ] **Step 1: Create Xcode project via command line**

```bash
mkdir -p ios/ClaudeController
```

Note: The Xcode project (`.xcodeproj`) must be created in Xcode or via `xcodegen`. For this plan, create an Xcode project manually:
1. Open Xcode → File → New → Project → App
2. Product Name: ClaudeController, Interface: SwiftUI, Language: Swift
3. Save to `ios/ClaudeController/`
4. Set deployment target to iOS 17.0

- [ ] **Step 2: Create data models**

Create `ios/ClaudeController/Models/Session.swift`:

```swift
import Foundation

struct Session: Codable, Identifiable, Hashable {
    let id: String
    let computerName: String
    let projectPath: String
    var status: String
    let createdAt: Date
    var lastSeenAt: Date
    var archived: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case computerName = "computer_name"
        case projectPath = "project_path"
        case status
        case createdAt = "created_at"
        case lastSeenAt = "last_seen_at"
        case archived
    }

    var displayName: String {
        let project = URL(fileURLWithPath: projectPath).lastPathComponent
        return "\(computerName) / \(project)"
    }

    var isStale: Bool {
        lastSeenAt.timeIntervalSinceNow < -300 // 5 minutes
    }
}
```

Create `ios/ClaudeController/Models/Prompt.swift`:

```swift
import Foundation

struct Prompt: Codable, Identifiable {
    let id: String
    let sessionId: String
    let claudeMessage: String
    let type: String // "prompt" or "notification"
    var response: String?
    var status: String // "pending" or "answered"
    let createdAt: Date
    var answeredAt: Date?

    enum CodingKeys: String, CodingKey {
        case id
        case sessionId = "session_id"
        case claudeMessage = "claude_message"
        case type
        case response
        case status
        case createdAt = "created_at"
        case answeredAt = "answered_at"
    }

    var isPending: Bool { status == "pending" && type == "prompt" }
    var isNotification: Bool { type == "notification" }
}
```

Create `ios/ClaudeController/Models/ServerConfig.swift`:

```swift
import Foundation

struct ServerConfig: Codable, Identifiable, Hashable {
    let url: String
    let key: String
    let version: Int
    var label: String? // User-assigned label, e.g. computer name

    var id: String { url }
}
```

- [ ] **Step 3: Commit**

```bash
git add ios/
git commit -m "feat: add iOS app models (Session, Prompt, ServerConfig)"
```

---

## Task 13: iOS App — API Client + Keychain Service

**Files:**
- Create: `ios/ClaudeController/Services/APIClient.swift`
- Create: `ios/ClaudeController/Services/KeychainService.swift`

- [ ] **Step 1: Create API client**

Create `ios/ClaudeController/Services/APIClient.swift`:

```swift
import Foundation

class APIClient: ObservableObject {
    private var config: ServerConfig
    private let session: URLSession
    private let decoder: JSONDecoder

    init(config: ServerConfig) {
        self.config = config
        self.session = URLSession.shared
        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let dateStr = try container.decode(String.self)
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: dateStr) { return date }
            // Try SQLite datetime format
            let sqlFormatter = DateFormatter()
            sqlFormatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
            sqlFormatter.timeZone = TimeZone(identifier: "UTC")
            if let date = sqlFormatter.date(from: dateStr) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Cannot decode date: \(dateStr)")
        }
    }

    func updateConfig(_ config: ServerConfig) {
        self.config = config
    }

    private func request(_ method: String, _ path: String, body: Data? = nil) async throws -> Data {
        guard let url = URL(string: config.url + path) else {
            throw APIError.invalidURL
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.setValue("Bearer \(config.key)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = body
        req.timeoutInterval = 10

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200...299).contains(http.statusCode) else {
            throw APIError.httpError(http.statusCode)
        }
        return data
    }

    // MARK: - Pairing

    func validatePairing() async throws -> Bool {
        let _ = try await request("GET", "/api/pairing")
        return true
    }

    func checkStatus() async throws -> Bool {
        let _ = try await request("GET", "/api/status")
        return true
    }

    // MARK: - Sessions

    func listSessions(includeArchived: Bool = false) async throws -> [Session] {
        let path = includeArchived ? "/api/sessions?include_archived=true" : "/api/sessions"
        let data = try await request("GET", path)
        return try decoder.decode([Session].self, from: data)
    }

    func setArchived(sessionId: String, archived: Bool) async throws {
        let body = try JSONEncoder().encode(["archived": archived])
        let _ = try await request("PUT", "/api/sessions/\(sessionId)/archive", body: body)
    }

    // MARK: - Prompts

    func listPrompts(sessionId: String? = nil, status: String? = nil) async throws -> [Prompt] {
        var path = "/api/prompts?"
        if let sid = sessionId { path += "session_id=\(sid)&" }
        if let s = status { path += "status=\(s)&" }
        let data = try await request("GET", path)
        return try decoder.decode([Prompt].self, from: data)
    }

    func respondToPrompt(promptId: String, response: String) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["response": response])
        let _ = try await request("POST", "/api/prompts/\(promptId)/respond", body: body)
    }

    // MARK: - Instructions

    func sendInstruction(sessionId: String, message: String) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["message": message])
        let _ = try await request("POST", "/api/sessions/\(sessionId)/instruct", body: body)
    }
}

enum APIError: LocalizedError {
    case invalidURL
    case invalidResponse
    case httpError(Int)

    var errorDescription: String? {
        switch self {
        case .invalidURL: return "Invalid server URL"
        case .invalidResponse: return "Invalid response from server"
        case .httpError(let code): return "Server returned error \(code)"
        }
    }
}
```

- [ ] **Step 2: Create Keychain service**

Create `ios/ClaudeController/Services/KeychainService.swift`:

```swift
import Foundation
import Security

class KeychainService {
    private static let serviceKey = "com.claude-controller.servers"

    static func saveConfigs(_ configs: [ServerConfig]) {
        guard let data = try? JSONEncoder().encode(configs) else { return }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
        ]

        SecItemDelete(query as CFDictionary)

        let addQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
            kSecValueData as String: data,
        ]

        SecItemAdd(addQuery as CFDictionary, nil)
    }

    static func loadConfigs() -> [ServerConfig] {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
            kSecReturnData as String: true,
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess, let data = result as? Data else {
            return []
        }

        return (try? JSONDecoder().decode([ServerConfig].self, from: data)) ?? []
    }

    static func addConfig(_ config: ServerConfig) {
        var configs = loadConfigs()
        configs.removeAll { $0.url == config.url }
        configs.append(config)
        saveConfigs(configs)
    }

    static func removeConfig(url: String) {
        var configs = loadConfigs()
        configs.removeAll { $0.url == url }
        saveConfigs(configs)
    }
}
```

- [ ] **Step 3: Commit**

```bash
git add ios/
git commit -m "feat: add iOS API client and Keychain service"
```

---

## Task 14: iOS App — Polling Service

**Files:**
- Create: `ios/ClaudeController/Services/PollingService.swift`

- [ ] **Step 1: Create adaptive polling service**

Create `ios/ClaudeController/Services/PollingService.swift`:

```swift
import Foundation
import Combine

@MainActor
class PollingService: ObservableObject {
    @Published var sessions: [Session] = []
    @Published var pendingPrompts: [Prompt] = []
    @Published var allPrompts: [Prompt] = []
    @Published var isConnected: Bool = false
    @Published var selectedSessionId: String?

    private var apiClient: APIClient?
    private var timer: Timer?
    private var activeInterval: TimeInterval = 3
    private var idleInterval: TimeInterval = 15

    func configure(client: APIClient) {
        self.apiClient = client
        startPolling()
    }

    func startPolling() {
        stopPolling()
        poll()
        scheduleNext()
    }

    func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func scheduleNext() {
        let hasActiveSession = sessions.contains { $0.status == "waiting" || $0.status == "active" }
        let interval = hasActiveSession ? activeInterval : idleInterval

        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: false) { [weak self] _ in
            Task { @MainActor in
                self?.poll()
                self?.scheduleNext()
            }
        }
    }

    private func poll() {
        guard let client = apiClient else { return }

        Task {
            do {
                self.sessions = try await client.listSessions()
                self.pendingPrompts = try await client.listPrompts(status: "pending")

                if let sid = selectedSessionId {
                    self.allPrompts = try await client.listPrompts(sessionId: sid)
                }

                self.isConnected = true
            } catch {
                self.isConnected = false
            }
        }
    }

    var pendingCount: Int { pendingPrompts.count }
}
```

- [ ] **Step 2: Commit**

```bash
git add ios/
git commit -m "feat: add adaptive polling service with active/idle intervals"
```

---

## Task 15: iOS App — Pairing View (QR Scanner)

**Files:**
- Create: `ios/ClaudeController/Views/PairingView.swift`

- [ ] **Step 1: Create pairing view with QR scanner**

Create `ios/ClaudeController/Views/PairingView.swift`:

```swift
import SwiftUI
import AVFoundation

struct PairingView: View {
    @Environment(\.dismiss) var dismiss
    @State private var scannedCode: String?
    @State private var manualURL: String = ""
    @State private var manualKey: String = ""
    @State private var showManualEntry = false
    @State private var isPairing = false
    @State private var errorMessage: String?
    var onPaired: (ServerConfig) -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                if showManualEntry {
                    manualEntryForm
                } else {
                    QRScannerView { code in
                        scannedCode = code
                        handleScannedCode(code)
                    }
                    .frame(maxHeight: 400)
                    .cornerRadius(12)

                    Text("Scan the QR code shown in your terminal")
                        .font(.subheadline)
                        .foregroundColor(.secondary)
                }

                if let error = errorMessage {
                    Text(error)
                        .foregroundColor(.red)
                        .font(.caption)
                }

                Button("Enter manually instead") {
                    showManualEntry.toggle()
                }
                .font(.subheadline)
            }
            .padding()
            .navigationTitle("Pair Server")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }

    private var manualEntryForm: some View {
        VStack(spacing: 16) {
            TextField("Server URL (e.g. https://abc123.ngrok.io)", text: $manualURL)
                .textFieldStyle(.roundedBorder)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            TextField("API Key (e.g. sk-...)", text: $manualKey)
                .textFieldStyle(.roundedBorder)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            Button(action: {
                let config = ServerConfig(url: manualURL, key: manualKey, version: 1)
                pairWith(config)
            }) {
                if isPairing {
                    ProgressView()
                } else {
                    Text("Connect")
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(manualURL.isEmpty || manualKey.isEmpty || isPairing)
        }
    }

    private func handleScannedCode(_ code: String) {
        guard let data = code.data(using: .utf8),
              let config = try? JSONDecoder().decode(ServerConfig.self, from: data) else {
            errorMessage = "Invalid QR code"
            return
        }
        pairWith(config)
    }

    private func pairWith(_ config: ServerConfig) {
        isPairing = true
        errorMessage = nil

        Task {
            let client = APIClient(config: config)
            do {
                let _ = try await client.validatePairing()
                KeychainService.addConfig(config)
                onPaired(config)
                dismiss()
            } catch {
                errorMessage = "Failed to connect: \(error.localizedDescription)"
            }
            isPairing = false
        }
    }
}

// QR Scanner using AVCaptureSession
struct QRScannerView: UIViewControllerRepresentable {
    var onCodeScanned: (String) -> Void

    func makeUIViewController(context: Context) -> QRScannerViewController {
        let vc = QRScannerViewController()
        vc.onCodeScanned = onCodeScanned
        return vc
    }

    func updateUIViewController(_ uiViewController: QRScannerViewController, context: Context) {}
}

class QRScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCodeScanned: ((String) -> Void)?
    private var captureSession: AVCaptureSession?

    override func viewDidLoad() {
        super.viewDidLoad()

        let session = AVCaptureSession()
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device) else { return }

        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.frame = view.bounds
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)

        captureSession = session
        session.startRunning()
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput metadataObjects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let object = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let code = object.stringValue else { return }
        captureSession?.stopRunning()
        onCodeScanned?(code)
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add ios/
git commit -m "feat: add pairing view with QR scanner and manual entry"
```

---

## Task 16: iOS App — Main View + Prompt Cards

**Files:**
- Create: `ios/ClaudeController/Views/MainView.swift`
- Create: `ios/ClaudeController/Views/PromptCardView.swift`
- Create: `ios/ClaudeController/Views/InstructionSheet.swift`

- [ ] **Step 1: Create prompt card view**

Create `ios/ClaudeController/Views/PromptCardView.swift`:

```swift
import SwiftUI

struct PromptCardView: View {
    let prompt: Prompt
    @State private var responseText: String = ""
    var onRespond: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Circle()
                    .fill(prompt.isPending ? Color.green : Color.gray.opacity(0.4))
                    .frame(width: 10, height: 10)

                if prompt.isPending {
                    Text("Claude is waiting...")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundColor(.green)
                } else if prompt.isNotification {
                    Text(prompt.createdAt, style: .relative)
                        .font(.caption)
                        .foregroundColor(.secondary)
                } else {
                    Text(prompt.createdAt, style: .relative)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }

            Text(prompt.claudeMessage)
                .font(.body)
                .lineLimit(nil)

            if prompt.isPending {
                HStack {
                    TextField("Type your response...", text: $responseText)
                        .textFieldStyle(.roundedBorder)

                    Button("Send") {
                        guard !responseText.isEmpty else { return }
                        onRespond(responseText)
                        responseText = ""
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(responseText.isEmpty)
                }
            } else if let response = prompt.response {
                Text("Replied: \(response)")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .italic()
            }
        }
        .padding()
        .background(prompt.isPending ? Color.green.opacity(0.05) : Color.clear)
        .cornerRadius(12)
    }
}
```

- [ ] **Step 2: Create instruction sheet**

Create `ios/ClaudeController/Views/InstructionSheet.swift`:

```swift
import SwiftUI

struct InstructionSheet: View {
    @Environment(\.dismiss) var dismiss
    @State private var message: String = ""
    @State private var isSending = false
    @State private var showConfirmation = false
    var onSend: (String) async -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text("This instruction will be delivered when Claude finishes its current turn.")
                    .font(.caption)
                    .foregroundColor(.secondary)

                TextEditor(text: $message)
                    .frame(minHeight: 120)
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.gray.opacity(0.3))
                    )

                if showConfirmation {
                    Label("Instruction queued", systemImage: "checkmark.circle.fill")
                        .foregroundColor(.green)
                }
            }
            .padding()
            .navigationTitle("New Instruction")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Send") {
                        isSending = true
                        Task {
                            await onSend(message)
                            isSending = false
                            showConfirmation = true
                            try? await Task.sleep(nanoseconds: 1_500_000_000)
                            dismiss()
                        }
                    }
                    .disabled(message.isEmpty || isSending)
                }
            }
        }
    }
}
```

- [ ] **Step 3: Create main view**

Create `ios/ClaudeController/Views/MainView.swift`:

```swift
import SwiftUI

struct MainView: View {
    @StateObject private var polling = PollingService()
    @State private var selectedConfig: ServerConfig?
    @State private var showPairing = false
    @State private var showInstruction = false
    @State private var showSettings = false

    private var configs: [ServerConfig] { KeychainService.loadConfigs() }
    private var apiClient: APIClient? {
        guard let config = selectedConfig else { return nil }
        return APIClient(config: config)
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if polling.sessions.isEmpty && !polling.isConnected {
                    ContentUnavailableView(
                        "No Server Connected",
                        systemImage: "antenna.radiowaves.left.and.right.slash",
                        description: Text("Pair with your computer to get started")
                    )
                } else {
                    // Session selector
                    if !polling.sessions.isEmpty {
                        Picker("Session", selection: $polling.selectedSessionId) {
                            Text("All Sessions").tag(String?.none)
                            ForEach(polling.sessions) { session in
                                HStack {
                                    Circle()
                                        .fill(session.status == "waiting" ? Color.green : Color.gray)
                                        .frame(width: 8, height: 8)
                                    Text(session.displayName)
                                }
                                .tag(Optional(session.id))
                            }
                        }
                        .padding(.horizontal)
                    }

                    Divider()

                    // Prompt list
                    List {
                        let prompts = polling.selectedSessionId != nil
                            ? polling.allPrompts
                            : polling.pendingPrompts

                        if prompts.isEmpty {
                            Text("No prompts")
                                .foregroundColor(.secondary)
                        }

                        ForEach(prompts) { prompt in
                            PromptCardView(prompt: prompt) { response in
                                Task {
                                    try? await apiClient?.respondToPrompt(
                                        promptId: prompt.id,
                                        response: response
                                    )
                                }
                            }
                            .listRowInsets(EdgeInsets())
                            .listRowSeparator(.hidden)
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("Claude Controller")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button(action: { showInstruction = true }) {
                            Label("New Instruction", systemImage: "plus.message")
                        }
                        .disabled(polling.selectedSessionId == nil)

                        Button(action: { showPairing = true }) {
                            Label("Pair Server", systemImage: "qrcode.viewfinder")
                        }

                        Button(action: { showSettings = true }) {
                            Label("Settings", systemImage: "gear")
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                }
            }
            .sheet(isPresented: $showPairing) {
                PairingView { config in
                    selectedConfig = config
                    if let client = apiClient {
                        polling.configure(client: client)
                    }
                }
            }
            .sheet(isPresented: $showInstruction) {
                InstructionSheet { message in
                    guard let sid = polling.selectedSessionId else { return }
                    try? await apiClient?.sendInstruction(sessionId: sid, message: message)
                }
            }
            .sheet(isPresented: $showSettings) {
                SettingsView(
                    polling: polling,
                    onSelectConfig: { config in
                        selectedConfig = config
                        if let client = apiClient {
                            polling.configure(client: client)
                        }
                    }
                )
            }
            .onAppear {
                if let first = configs.first {
                    selectedConfig = first
                    if let client = apiClient {
                        polling.configure(client: client)
                    }
                } else {
                    showPairing = true
                }
            }
            .badge(polling.pendingCount)
        }
    }
}
```

- [ ] **Step 4: Commit**

```bash
git add ios/
git commit -m "feat: add main view with session selector, prompt cards, instruction sheet"
```

---

## Task 17: iOS App — Settings View + App Entry Point

**Files:**
- Create: `ios/ClaudeController/Views/SettingsView.swift`
- Create: `ios/ClaudeController/ClaudeControllerApp.swift`

- [ ] **Step 1: Create settings view**

Create `ios/ClaudeController/Views/SettingsView.swift`:

```swift
import SwiftUI

struct SettingsView: View {
    @Environment(\.dismiss) var dismiss
    @ObservedObject var polling: PollingService
    @State private var configs: [ServerConfig] = KeychainService.loadConfigs()
    @State private var showPairing = false
    var onSelectConfig: (ServerConfig) -> Void

    var body: some View {
        NavigationStack {
            List {
                Section("Paired Servers") {
                    ForEach(configs) { config in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(config.label ?? config.url)
                                    .font(.body)
                                Text(config.url)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }

                            Spacer()

                            Circle()
                                .fill(polling.isConnected ? Color.green : Color.red)
                                .frame(width: 10, height: 10)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            onSelectConfig(config)
                            dismiss()
                        }
                    }
                    .onDelete { indexSet in
                        for i in indexSet {
                            KeychainService.removeConfig(url: configs[i].url)
                        }
                        configs = KeychainService.loadConfigs()
                    }

                    Button(action: { showPairing = true }) {
                        Label("Add Server", systemImage: "plus")
                    }
                }

                Section("Archived Sessions") {
                    ForEach(polling.sessions.filter { $0.archived }) { session in
                        HStack {
                            Text(session.displayName)
                                .foregroundColor(.secondary)
                            Spacer()
                            Button("Unarchive") {
                                Task {
                                    // TODO: Call unarchive API
                                }
                            }
                            .font(.caption)
                        }
                    }
                }
            }
            .navigationTitle("Settings")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .sheet(isPresented: $showPairing) {
                PairingView { config in
                    configs = KeychainService.loadConfigs()
                    onSelectConfig(config)
                }
            }
        }
    }
}
```

- [ ] **Step 2: Create app entry point**

Create `ios/ClaudeController/ClaudeControllerApp.swift`:

```swift
import SwiftUI

@main
struct ClaudeControllerApp: App {
    var body: some Scene {
        WindowGroup {
            MainView()
        }
    }
}
```

- [ ] **Step 3: Commit**

```bash
git add ios/
git commit -m "feat: add settings view and app entry point"
```

---

## Task 18: Hook Installation Guide + Config Setup

**Files:**
- Create: `hooks/install.sh`

- [ ] **Step 1: Create installation helper script**

Create `hooks/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Claude Controller Hook Installer
# Sets up the hooks in Claude Code settings and creates the config file.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="$HOME/.claude-controller.json"
SETTINGS_FILE="$HOME/.claude/settings.json"

echo "=== Claude Controller Hook Installer ==="
echo ""

# Get computer name
COMPUTER_NAME=$(hostname -s 2>/dev/null || hostname)
read -p "Computer name [$COMPUTER_NAME]: " input_name
COMPUTER_NAME="${input_name:-$COMPUTER_NAME}"

# Get server URL
read -p "Server URL [http://localhost:8080]: " input_url
SERVER_URL="${input_url:-http://localhost:8080}"

# Get API key
read -p "API key (from QR code or server output): " API_KEY

# Write config
cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "$SERVER_URL",
  "computer_name": "$COMPUTER_NAME",
  "api_key": "$API_KEY"
}
EOF
echo "Config written to $CONFIG_FILE"

# Ensure settings file exists
mkdir -p "$(dirname "$SETTINGS_FILE")"
if [[ ! -f "$SETTINGS_FILE" ]]; then
    echo '{}' > "$SETTINGS_FILE"
fi

# Add hooks to Claude Code settings using jq
STOP_HOOK="$SCRIPT_DIR/stop.sh"
NOTIFY_HOOK="$SCRIPT_DIR/notify.sh"

jq --arg stop "$STOP_HOOK" --arg notify "$NOTIFY_HOOK" '
  .hooks.Stop = [{"hooks": [{"type": "command", "command": $stop}]}] |
  .hooks.Notification = [{"hooks": [{"type": "command", "command": $notify}]}]
' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"

echo "Hooks registered in $SETTINGS_FILE"
echo ""
echo "Done! Restart any running Claude Code sessions for hooks to take effect."
```

- [ ] **Step 2: Make executable**

```bash
chmod +x hooks/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add hooks/install.sh
git commit -m "feat: add hook installation helper script"
```

---

## Task 19: Run All Server Tests

- [ ] **Step 1: Run full test suite**

```bash
cd server && go test ./... -v
```

Expected: All tests pass.

- [ ] **Step 2: Fix any failures and re-run until green**

- [ ] **Step 3: Commit any fixes**

```bash
git add server/
git commit -m "fix: resolve test failures from integration"
```

---

## Task 20: CLAUDE.md + Final Commit

**Files:**
- Create: `CLAUDE.md`

- [ ] **Step 1: Create CLAUDE.md**

Create `CLAUDE.md`:

```markdown
# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Controller — a system for remotely controlling Claude Code sessions from an iPhone. Three components: Go server, Claude Code hooks, and a SwiftUI iOS app.

## Build & Test Commands

### Go Server
```bash
cd server && go build -o claude-controller .   # Build
cd server && go test ./... -v                  # Run all tests
cd server && go test ./db/ -v                  # Test DB layer only
cd server && go test ./api/ -v                 # Test API handlers only
cd server && go test ./api/ -v -run TestName   # Run single test
cd server && go run .                          # Run server (starts on :8080)
cd server && go run . --port 9090              # Custom port
```

### iOS App
Open `ios/ClaudeController/` in Xcode. Build target: iOS 17.0+.

### Hooks
```bash
./hooks/install.sh                             # Install hooks into Claude Code
```

## Architecture

- `server/` — Go REST API server. `db/` package handles SQLite, `api/` package handles HTTP routes, `tunnel/` handles ngrok.
- `hooks/` — Bash (macOS) and PowerShell (Windows) scripts that fire on Claude Code Stop and Notification events. Stop hook blocks and long-polls for user response.
- `ios/` — SwiftUI app. `Services/` has API client and polling, `Views/` has all screens, `Models/` has Codable types.

## Key Design Decisions

- Stop hook returns `{"decision": "block", "reason": "..."}` to feed responses back to Claude
- `stop_hook_active` field prevents infinite hook loops
- Instructions queue and deliver on next Stop event (cannot interrupt mid-turn)
- Long-poll: server holds connection 30s, hook retries indefinitely
- SQLite with WAL mode for concurrent hook writes
- No cloud services — Go server runs locally, ngrok tunnels to phone

## Spec
`docs/superpowers/specs/2026-03-17-claude-controller-design.md`
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add CLAUDE.md with build commands and architecture overview"
```
