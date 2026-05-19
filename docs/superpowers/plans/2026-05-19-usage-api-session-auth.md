# OAuth Usage API Session Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the 5hr/7d usage graphs visible in the browser client by fixing the `/api/usage` endpoint authentication.

**Architecture:** Rewrite `/api/usage` to use session-based auth (already in the codebase for other endpoints) instead of trying to read OAuth token from the server's Keychain. Browser clients send sessionId; server validates it, uses its own OAuth token to call Anthropic, and caches the result. This avoids exposing OAuth tokens to the browser and eliminates repeated API calls.

**Tech Stack:** Go (existing), SQLite (existing), Anthropic OAuth API (existing)

---

## File Structure

| File | Responsibility |
|------|---|
| `server/api/usage.go` | Rewritten handler with session auth + caching |
| `server/api/usage_test.go` | Tests for session auth, caching, fallback behavior |
| `server/db/sessions.go` | New method: `GetSession(id)` (or use existing) |

---

## Implementation Plan

### Task 1: Add usage cache to Server struct

**Files:**
- Modify: `server/api/usage.go` (add fields)
- Modify: `server/api/server.go` (add fields)

- [ ] **Step 1: Define usage cache type**

In `server/api/usage.go`, add above the handler:

```go
// UsageCache holds cached usage data with timestamp for TTL checking.
type UsageCache struct {
    Data      []byte    // Raw JSON from Anthropic
    Timestamp time.Time
}

const UsageCacheTTL = 60 * time.Second // Cache for 60 seconds
```

- [ ] **Step 2: Add cache field to Server struct**

In `server/api/server.go`, find the `Server` struct and add:

```go
type Server struct {
    // ... existing fields ...
    usageCache    *UsageCache
    usageCacheMu  sync.RWMutex
}
```

- [ ] **Step 3: Commit**

```bash
git add server/api/usage.go server/api/server.go
git commit -m "feat(usage): add cache type and Server field for usage API caching"
```

---

### Task 2: Rewrite `/api/usage` handler with session auth

**Files:**
- Modify: `server/api/usage.go` (replace `handleUsage` function)

- [ ] **Step 1: Write test for session auth failure**

In `server/api/usage_test.go`, add:

```go
func TestHandleUsage_MissingSession(t *testing.T) {
    // Request without sessionId should return 401
    mux := http.NewServeMux()
    server := &Server{store: &db.Store{}}
    mux.HandleFunc("GET /api/usage", server.handleUsage)
    ts := httptest.NewServer(mux)
    defer ts.Close()

    resp, _ := http.Get(ts.URL + "/api/usage")
    if resp.StatusCode != http.StatusUnauthorized {
        t.Errorf("expected 401, got %d", resp.StatusCode)
    }
}
```

- [ ] **Step 2: Write test for valid session**

In `server/api/usage_test.go`, add (mock Anthropic response):

```go
func TestHandleUsage_ValidSession(t *testing.T) {
    // Create a mock server to respond like Anthropic
    mockAnthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"five_hour":{"utilization":0.42,"resets_at":"2026-05-19T18:00:00.000Z"}}`))
    }))
    defer mockAnthropicServer.Close()

    // Create real Server with test DB and mock upstream
    store := setupTestDB(t)
    sess := &db.Session{ID: "test-session", Mode: "managed"}
    store.CreateSession(sess)

    server := &Server{
        store:                store,
        usageUpstreamURL:     mockAnthropicServer.URL,
        skipKeychain:         true, // Skip real keychain lookup
        usageCache:           &UsageCache{},
        usageCacheMu:         sync.RWMutex{},
    }
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/usage", server.handleUsage)
    ts := httptest.NewServer(mux)
    defer ts.Close()

    // Request WITH sessionId query param
    resp, _ := http.Get(ts.URL + "/api/usage?sessionId=test-session")
    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected 200, got %d", resp.StatusCode)
    }

    var body map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&body)
    if _, ok := body["five_hour"]; !ok {
        t.Error("expected five_hour in response")
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd server && go test ./api -v -run TestHandleUsage_MissingSession
cd server && go test ./api -v -run TestHandleUsage_ValidSession
```

Expected: Both tests FAIL (handler doesn't exist yet)

- [ ] **Step 4: Implement new handler**

Replace the entire `handleUsage` function in `server/api/usage.go`:

```go
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
    // Extract sessionId from query params or cookies
    sessionID := r.URL.Query().Get("sessionId")
    if sessionID == "" {
        // Try cookie fallback (if browser sets one)
        if cookie, err := r.Cookie("sessionId"); err == nil {
            sessionID = cookie.Value
        }
    }

    // Verify session exists (user is authorized)
    if sessionID == "" {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusUnauthorized)
        w.Write([]byte(`{"error":"missing_session"}`))
        return
    }

    sess, err := s.store.GetSession(sessionID)
    if err != nil || sess == nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusUnauthorized)
        w.Write([]byte(`{"error":"invalid_session"}`))
        return
    }

    // Check cache first (server-side caching, not client-side)
    s.usageCacheMu.RLock()
    if s.usageCache != nil && time.Since(s.usageCache.Timestamp) < UsageCacheTTL {
        cachedData := s.usageCache.Data
        s.usageCacheMu.RUnlock()
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "public, max-age=60")
        w.Write(cachedData)
        return
    }
    s.usageCacheMu.RUnlock()

    // Fetch from Anthropic (server-to-server, using Keychain token)
    token := s.resolveOAuthToken()
    if token == "" {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte(`{"error":"no_oauth_token"}`))
        return
    }

    client := &http.Client{Timeout: 10 * time.Second}
    req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, s.getUsageUpstreamURL(), nil)
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("anthropic-beta", "oauth-2025-04-20")

    resp, err := client.Do(req)
    if err != nil || resp.StatusCode != http.StatusOK {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadGateway)
        w.Write([]byte(`{"error":"upstream_error"}`))
        return
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)

    // Cache the result
    s.usageCacheMu.Lock()
    s.usageCache = &UsageCache{
        Data:      body,
        Timestamp: time.Now(),
    }
    s.usageCacheMu.Unlock()

    // Return to client
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Cache-Control", "public, max-age=60")
    w.Write(body)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd server && go test ./api -v -run TestHandleUsage
```

Expected: Both PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/usage.go server/api/usage_test.go
git commit -m "feat(api): rewrite /api/usage handler with session auth and caching"
```

---

### Task 3: Update browser client to send sessionId

**Files:**
- Modify: `server/web/static/app.js` (update `fetchUsage` method)

- [ ] **Step 1: Check current fetchUsage implementation**

Search `server/web/static/app.js` for the `fetchUsage()` method (around line 767).

- [ ] **Step 2: Update fetch call to include sessionId**

Replace the `fetch('/api/usage'` line with:

```javascript
const sessionId = this.selectedSessionId; // Current session from component state
const queryString = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : '';
const resp = await fetch(`/api/usage${queryString}`, {
    method: 'GET',
    credentials: 'include', // Send any cookies (fallback)
});
```

Full updated method:

```javascript
async fetchUsage() {
    try {
        const sessionId = this.selectedSessionId;
        const queryString = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : '';
        const resp = await fetch(`/api/usage${queryString}`, {
            method: 'GET',
            credentials: 'include',
        });
        if (!resp.ok) {
            this.usageData = null;
            this.usageError = true;
            return;
        }
        this.usageData = await resp.json();
        this.usageError = false;
    } catch (err) {
        this.usageData = null;
        this.usageError = true;
    }
}
```

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "fix(ui): pass sessionId to /api/usage endpoint"
```

---

### Task 4: Verify GetSession method exists in DB

**Files:**
- Check: `server/db/sessions.go`

- [ ] **Step 1: Check if GetSession exists**

```bash
grep -n "func.*GetSession" server/db/sessions.go
```

If it returns a match, done. If not, proceed to Step 2.

- [ ] **Step 2: Add GetSession method (if missing)**

In `server/db/sessions.go`, add:

```go
// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*Session, error) {
    var sess Session
    err := s.db.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id).Scan(
        &sess.ID, &sess.Mode, &sess.ComputerName, &sess.ProjectPath, &sess.CWD, &sess.Model,
        &sess.MaxTurns, &sess.TurnCount, &sess.CreatedAt, &sess.ActivityState, &sess.Cost,
    )
    if err == sql.ErrNoRows {
        return nil, nil // Not found
    }
    if err != nil {
        return nil, err
    }
    return &sess, nil
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd server && go build -o claude-controller .
```

Expected: Builds without errors.

- [ ] **Step 4: Commit (if added)**

```bash
git add server/db/sessions.go
git commit -m "feat(db): add GetSession method for usage auth"
```

---

### Task 5: End-to-end test

**Files:**
- Manual testing (browser)

- [ ] **Step 1: Start the server locally**

```bash
cd server && go run . --port 9999
```

- [ ] **Step 2: Open browser and start a managed session**

Navigate to `http://localhost:9999`, create a new managed session.

- [ ] **Step 3: Check browser console for fetch errors**

Open DevTools (F12) → Console. Look for any fetch errors on `/api/usage`.

- [ ] **Step 4: Verify usage bars appear**

After 3-5 seconds, the 5hr and 7d bars should appear in the top-right header with utilization %.

If still missing, check:
- DevTools Network tab: Is `/api/usage` call succeeding (200)?
- DevTools Console: Any JS errors?
- Server logs: Any `handleUsage` errors?

- [ ] **Step 5: Commit final working state**

```bash
git add -A
git commit -m "fix(usage): complete OAuth usage API with session auth and caching"
```
