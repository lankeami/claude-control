# Usage Rate Limit Widget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `GET /api/usage` backend endpoint and a compact session-header widget that shows Claude's 5-hour and 7-day rate limit utilization in real time.

**Architecture:** The Go handler resolves an OAuth token from the macOS Keychain (falling back to `CLAUDE_OAUTH_TOKEN` env var), proxies to `https://api.anthropic.com/api/oauth/usage`, and returns the body verbatim. The Alpine.js frontend polls every 60s and renders two mini progress bars in the `.main-header`.

**Tech Stack:** Go 1.22 (stdlib only), Alpine.js (existing), plain HTML/CSS matching existing widget styles.

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `server/api/usage.go` | Token resolution + upstream proxy handler |
| Create | `server/api/usage_test.go` | Unit tests for the handler |
| Edit | `server/api/router.go` | Register `GET /api/usage` |
| Edit | `server/web/static/app.js` | `usageData`/`usageError` state, `fetchUsage()`, polling |
| Edit | `server/web/static/index.html` | Usage widget markup in `.main-header` |

---

## Task 1: Go handler — token resolution + proxy

**Files:**
- Create: `server/api/usage.go`
- Create: `server/api/usage_test.go`

### Background

The Anthropic OAuth token lives in the macOS Keychain under service name `"Claude Code-credentials"`. The value returned by `security find-generic-password -w` is JSON:

```json
{"claudeAiOauth":{"accessToken":"sk-ant-oaXXX","refreshToken":"...","expiresAt":1234567890000,"scopes":["..."]}}
```

The handler tries the Keychain first, falls back to `CLAUDE_OAUTH_TOKEN` env var, and returns `503` if both are empty.

- [ ] **Step 1: Write failing tests**

Create `server/api/usage_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newUsageTestServer creates a test server with a mock upstream usage URL injected.
func newUsageTestServer(t *testing.T, upstreamURL string) *httptest.Server {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := openTestDB(t, tmpDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	envPath := filepath.Join(tmpDir, ".env")
	s := &Server{store: store, envPath: envPath}
	s.usageUpstreamURL = upstreamURL
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestUsage_NoToken(t *testing.T) {
	// Ensure no env var token is set
	os.Unsetenv("CLAUDE_OAUTH_TOKEN")

	// Use a fake upstream that should never be reached
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when no token")
	}))
	defer upstream.Close()

	ts := newUsageTestServer(t, upstream.URL)
	resp, err := http.Get(ts.URL + "/api/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "no_token" {
		t.Errorf("expected error=no_token, got %q", body["error"])
	}
}

func TestUsage_EnvToken_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-oauth-token" {
			t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Errorf("unexpected anthropic-beta: %s", r.Header.Get("anthropic-beta"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"five_hour":{"utilization":0.42,"resets_at":"2026-05-16T18:00:00.000Z"}}`))
	}))
	defer upstream.Close()

	t.Setenv("CLAUDE_OAUTH_TOKEN", "test-oauth-token")
	ts := newUsageTestServer(t, upstream.URL)

	resp, err := http.Get(ts.URL + "/api/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if _, ok := body["five_hour"]; !ok {
		t.Error("expected five_hour in response")
	}
}

func TestUsage_EnvToken_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	t.Setenv("CLAUDE_OAUTH_TOKEN", "expired-token")
	ts := newUsageTestServer(t, upstream.URL)

	resp, err := http.Get(ts.URL + "/api/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "upstream_error" {
		t.Errorf("expected error=upstream_error, got %v", body["error"])
	}
}
```

Note: the tests use `s.usageUpstreamURL` — a field we'll add to `Server` that defaults to the real Anthropic URL. Also uses `openTestDB` — a small helper we'll add.

- [ ] **Step 2: Add `openTestDB` helper to `sessions_test.go`**

`sessions_test.go` already has `newTestServer`. Add a package-level helper at the top of `usage_test.go` instead (avoids touching other test files):

```go
// openTestDB is a minimal helper used by usage tests to get a *db.Store.
func openTestDB(t *testing.T, dir string) (*db.Store, error) {
	t.Helper()
	return db.Open(filepath.Join(dir, "test.db"))
}
```

Add this block just above `newUsageTestServer` in `usage_test.go`. Add the missing import: `"github.com/jaychinthrajah/claude-controller/server/db"`.

- [ ] **Step 3: Run tests — verify they fail**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go test ./api/ -run TestUsage -v 2>&1 | head -30
```

Expected: compile error — `handleUsage` and `usageUpstreamURL` not defined yet.

- [ ] **Step 4: Create `server/api/usage.go`**

```go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultUsageUpstreamURL = "https://api.anthropic.com/api/oauth/usage"

// usageUpstreamURL is a field on Server used to override the upstream in tests.
// Zero value means use the real Anthropic endpoint.
func (s *Server) getUsageUpstreamURL() string {
	if s.usageUpstreamURL != "" {
		return s.usageUpstreamURL
	}
	return defaultUsageUpstreamURL
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	token := resolveOAuthToken()
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"no_token"}`))
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.getUsageUpstreamURL(), nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream_error"}`)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := client.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream_error","status":%d}`, resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream_error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// resolveOAuthToken tries the macOS Keychain first, then CLAUDE_OAUTH_TOKEN env var.
func resolveOAuthToken() string {
	// Try macOS Keychain
	if token := tokenFromKeychain(); token != "" {
		return token
	}
	// Fall back to env var
	return os.Getenv("CLAUDE_OAUTH_TOKEN")
}

// tokenFromKeychain runs `security find-generic-password` and parses the JSON result.
func tokenFromKeychain() string {
	username := os.Getenv("USER")
	if username == "" {
		return ""
	}
	cmd := exec.Command("/usr/bin/security",
		"find-generic-password",
		"-a", username,
		"-s", "Claude Code-credentials",
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return ""
	}

	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return ""
	}
	return creds.ClaudeAiOauth.AccessToken
}
```

- [ ] **Step 5: Add `usageUpstreamURL` field to `Server` struct in `router.go`**

In `server/api/router.go`, find the `Server` struct definition and add the field:

```go
type Server struct {
	store             *db.Store
	manager           SessionManager
	envPath           string
	permissions       *PermissionManager
	shutdownFunc      func()
	restartInProgress atomic.Bool
	serverID          string
	usageUpstreamURL  string // override for tests; empty means use real Anthropic URL
}
```

- [ ] **Step 6: Run tests — verify they pass**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go test ./api/ -run TestUsage -v
```

Expected output:
```
--- PASS: TestUsage_NoToken (0.00s)
--- PASS: TestUsage_EnvToken_Success (0.00s)
--- PASS: TestUsage_EnvToken_UpstreamError (0.00s)
PASS
```

- [ ] **Step 7: Verify full test suite still passes**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go test ./... 2>&1
```

Expected: all pass, no new failures.

- [ ] **Step 8: Commit**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control
git checkout -b feat/usage-rate-limit-widget
git add server/api/usage.go server/api/usage_test.go
git commit -m "feat(api): add GET /api/usage endpoint with keychain token resolution"
```

---

## Task 2: Register the route

**Files:**
- Modify: `server/api/router.go`

- [ ] **Step 1: Add route registration**

In `server/api/router.go`, find the Settings endpoints block and add the usage route after it:

```go
// Settings endpoints
apiMux.HandleFunc("GET /api/settings/exists", s.handleSettingsExists)
apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
apiMux.HandleFunc("PUT /api/settings", s.handlePutSettings)

// Usage / rate limit endpoint
apiMux.HandleFunc("GET /api/usage", s.handleUsage)
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 3: Smoke test the endpoint manually**

Start the server in one terminal, then in another:

```bash
# In server/: go run .
# Then:
API_KEY=$(grep -o 'key=[a-z0-9-]*' ~/.claude-controller.log 2>/dev/null | head -1 | cut -d= -f2 || echo "check server output for key")
curl -s -H "Authorization: Bearer $API_KEY" http://localhost:8080/api/usage | python3 -m json.tool
```

Expected: JSON with `five_hour`, `seven_day` keys (or `{"error":"no_token"}` on Linux without env var set).

- [ ] **Step 4: Commit**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control
git add server/api/router.go
git commit -m "feat(api): register GET /api/usage route"
```

---

## Task 3: Alpine.js state and polling

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Add `usageData` and `usageError` to the Alpine data object**

In `app.js`, find the block of data properties near the top of the `app` data function (around line 4–60). Add after `connected: true,`:

```js
// Usage / rate limit state
usageData: null,
usageError: false,
```

- [ ] **Step 2: Add `fetchUsage()` method**

Find `turnBarColor()` in `app.js` (around line 756). Add `fetchUsage()` immediately before it:

```js
async fetchUsage() {
  try {
    const resp = await fetch('/api/usage', {
      headers: { 'Authorization': `Bearer ${this.apiKey}` }
    });
    if (!resp.ok) {
      this.usageData = null;
      this.usageError = true;
      return;
    }
    this.usageData = await resp.json();
    this.usageError = false;
  } catch (e) {
    this.usageData = null;
    this.usageError = true;
  }
},

usageBarColor(utilization) {
  if (utilization > 0.9) return '#e74c3c';
  if (utilization > 0.7) return '#f39c12';
  return '#22c55e';
},
```

- [ ] **Step 3: Start polling in `init()`**

In the `init()` function (around line 264), find the end of the `if (this.apiKey)` block. Add usage polling inside that block, after `this.loadShortcuts()`:

```js
if (this.apiKey) {
  await this.tryConnect(this.apiKey);
  await this.loadScheduledTasks();
  await this.checkSettingsFirstRun();
  this.loadShortcuts();
  // Usage rate limit polling
  this.fetchUsage();
  setInterval(() => this.fetchUsage(), 60_000);
}
```

- [ ] **Step 4: Verify no JS errors by checking syntax**

```bash
node --check /Users/jaychinthrajah/workspaces/_personal_/claude-control/server/web/static/app.js
```

Expected: exits 0, no output.

- [ ] **Step 5: Commit**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control
git add server/web/static/app.js
git commit -m "feat(ui): add usage polling state and fetchUsage() to Alpine app"
```

---

## Task 4: Usage widget markup in the session header

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add the usage widget to `.main-header`**

In `index.html`, find the `.main-header` div (around line 142). Locate the line:

```html
<div :class="currentSession?.mode === 'managed' ? 'turns-monitor' : 'turns-monitor hidden'">
```

Insert the usage widget **immediately before** that `turns-monitor` div:

```html
<!-- Usage rate limit widget -->
<div x-show="usageData && !usageError"
     style="display:flex; align-items:center; gap:10px; margin-right:8px;">
  <!-- 5-hour bar -->
  <template x-if="usageData?.five_hour">
    <div style="display:flex; align-items:center; gap:4px;"
         :title="'5hr resets at ' + new Date(usageData.five_hour.resets_at).toLocaleTimeString()">
      <span style="font-size:11px; opacity:0.6; white-space:nowrap;">5hr</span>
      <div style="width:60px; height:4px; background:var(--border); border-radius:2px; overflow:hidden;">
        <div :style="'width:' + Math.round(usageData.five_hour.utilization * 100) + '%; height:100%; border-radius:2px; background:' + usageBarColor(usageData.five_hour.utilization) + '; transition:width 0.3s ease'"></div>
      </div>
      <span x-text="Math.round(usageData.five_hour.utilization * 100) + '%'"
            style="font-size:11px; font-weight:600; min-width:28px;"
            :style="'color:' + usageBarColor(usageData.five_hour.utilization)"></span>
    </div>
  </template>
  <!-- 7-day bar -->
  <template x-if="usageData?.seven_day">
    <div style="display:flex; align-items:center; gap:4px;"
         :title="'7d resets at ' + new Date(usageData.seven_day.resets_at).toLocaleDateString()">
      <span style="font-size:11px; opacity:0.6; white-space:nowrap;">7d</span>
      <div style="width:60px; height:4px; background:var(--border); border-radius:2px; overflow:hidden;">
        <div :style="'width:' + Math.round(usageData.seven_day.utilization * 100) + '%; height:100%; border-radius:2px; background:' + usageBarColor(usageData.seven_day.utilization) + '; transition:width 0.3s ease'"></div>
      </div>
      <span x-text="Math.round(usageData.seven_day.utilization * 100) + '%'"
            style="font-size:11px; font-weight:600; min-width:28px;"
            :style="'color:' + usageBarColor(usageData.seven_day.utilization)"></span>
    </div>
  </template>
</div>
```

- [ ] **Step 2: Verify the server builds and starts cleanly**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go build -o /tmp/cc-test . && echo "BUILD OK"
```

Expected: `BUILD OK`

- [ ] **Step 3: Visual verification**

Start the server (`go run .` in `server/`) and open `http://localhost:8080` in a browser.

Check all four states:

| State | How to test | Expected |
|---|---|---|
| Token available | macOS with Claude Code installed | Two bars appear in header with green/amber/red color |
| Token unavailable | `unset CLAUDE_OAUTH_TOKEN` on Linux | Widget is invisible, no layout shift |
| High utilization | Mock: temporarily hardcode `usageData` in browser console | Bar turns amber at >70%, red at >90% |
| Partial response | Set `usageData = {five_hour: null, seven_day: {utilization:0.5, resets_at:'...'}}` in console | Only 7d bar shows |

- [ ] **Step 4: Run full test suite one final time**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control/server && go test ./... 2>&1
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/jaychinthrajah/workspaces/_personal_/claude-control
git add server/web/static/index.html
git commit -m "feat(ui): add usage rate limit widget to session header"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| Token from Keychain first | Task 1 — `tokenFromKeychain()` |
| Fall back to `CLAUDE_OAUTH_TOKEN` | Task 1 — `resolveOAuthToken()` |
| 503 when no token | Task 1 — `TestUsage_NoToken` |
| 502 on upstream failure | Task 1 — `TestUsage_EnvToken_UpstreamError` |
| Return upstream body verbatim | Task 1 — `io.ReadAll` + direct write |
| Route registration | Task 2 |
| 60s polling | Task 3 — `setInterval(() => this.fetchUsage(), 60_000)` |
| First fetch on page load | Task 3 — `this.fetchUsage()` before interval |
| `usageData` / `usageError` state | Task 3 |
| Widget hidden on error | Task 4 — `x-show="usageData && !usageError"` |
| 5hr bar with label, bar, percent | Task 4 |
| 7d bar with label, bar, percent | Task 4 |
| Color thresholds (70%/90%) | Task 3 — `usageBarColor()` |
| Tooltip with reset time | Task 4 — `:title` binding |
| Individual bar hidden if field null | Task 4 — `x-if="usageData?.five_hour"` |
| No new Go dependencies | Task 1 — stdlib only |
| No DB changes | Confirmed — no db/ touches |

All spec requirements covered. No placeholders. Types consistent across tasks (`usageData`, `usageError`, `usageBarColor`).
