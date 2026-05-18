# Managed Session Cost Visibility & Model Routing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce managed session Agent SDK Credit spend via Haiku-default model routing and fix the $0 cost display bug by enriching `result` SSE events with computed cost.

**Architecture:** A new `pricing.go` provides `selectModel()` and `calcCost()`. `StreamNDJSON` gets a variadic `transform` hook that the managed session handler uses to inject `cost` and `model` into `result` events before broadcast. The frontend's existing `sessionCost` accumulation at `data.cost != null` then works automatically. Four small UI changes complete the feature.

**Tech Stack:** Go 1.26, Alpine.js, existing `StreamNDJSON` / `Broadcaster` / `handleSendMessage` patterns.

**Spec:** `docs/superpowers/specs/2026-05-18-managed-session-cost-visibility-design.md`

---

## File Map

| File | Action | What changes |
|------|--------|--------------|
| `server/api/pricing.go` | Create | `ModelHaiku`, `ModelSonnet`, `ModelOpus`, `EscalateAfterChars`, `modelPrices`, `selectModel()`, `calcCost()` |
| `server/api/pricing_test.go` | Create | Unit tests for `selectModel` and `calcCost` |
| `server/managed/stream.go` | Modify | Add variadic `transform ...func(string) string` to `StreamNDJSON` |
| `server/managed/stream_test.go` | Modify | Add test asserting transform replaces broadcast content |
| `server/api/managed_sessions.go` | Modify | Call `selectModel`, broadcast `model_selected`, define `transform` closure, pass to `StreamNDJSON` |
| `server/api/managed_sessions_test.go` | Modify | Tests for model routing and cost enrichment |
| `server/web/static/index.html` | Modify | Add "Auto" option to model picker; add billing callout to new-session modal |
| `server/web/static/app.js` | Modify | Default `selectedModel` to `""`, handle `model_selected` event, fix compact cost guard |

---

## Task 1: Create `pricing.go` with model constants, `selectModel`, and `calcCost`

**Files:**
- Create: `server/api/pricing_test.go`
- Create: `server/api/pricing.go`

- [ ] **Step 1.1: Write the failing tests**

Create `server/api/pricing_test.go`:

```go
package api

import (
	"testing"
)

func TestSelectModel_UserOverride(t *testing.T) {
	got := selectModel("short", false, "claude-opus-4-6")
	if got != "claude-opus-4-6" {
		t.Errorf("got %q, want claude-opus-4-6", got)
	}
}

func TestSelectModel_ImagesEscalate(t *testing.T) {
	got := selectModel("short", true, "")
	if got != ModelSonnet {
		t.Errorf("got %q, want %q", got, ModelSonnet)
	}
}

func TestSelectModel_LongMessageEscalates(t *testing.T) {
	long := make([]byte, EscalateAfterChars+1)
	for i := range long {
		long[i] = 'a'
	}
	got := selectModel(string(long), false, "")
	if got != ModelSonnet {
		t.Errorf("got %q, want %q", got, ModelSonnet)
	}
}

func TestSelectModel_DefaultIsHaiku(t *testing.T) {
	got := selectModel("short message", false, "")
	if got != ModelHaiku {
		t.Errorf("got %q, want %q", got, ModelHaiku)
	}
}

func TestCalcCost_Haiku(t *testing.T) {
	// 1M input tokens at $0.80 + 1M output at $4.00 = $4.80
	got := calcCost(ModelHaiku, 1_000_000, 1_000_000)
	want := 4.80
	if got != want {
		t.Errorf("calcCost Haiku 1M/1M = %.4f, want %.4f", got, want)
	}
}

func TestCalcCost_Sonnet(t *testing.T) {
	// 100k input at $3.00/M + 10k output at $15.00/M
	got := calcCost(ModelSonnet, 100_000, 10_000)
	want := 0.30 + 0.15
	if got != want {
		t.Errorf("calcCost Sonnet 100k/10k = %.4f, want %.4f", got, want)
	}
}

func TestCalcCost_UnknownModel(t *testing.T) {
	got := calcCost("unknown-model", 1_000_000, 1_000_000)
	if got != 0 {
		t.Errorf("unknown model should return 0, got %f", got)
	}
}

func TestCalcCost_ZeroTokens(t *testing.T) {
	got := calcCost(ModelHaiku, 0, 0)
	if got != 0 {
		t.Errorf("zero tokens should return 0, got %f", got)
	}
}
```

- [ ] **Step 1.2: Run tests — expect FAIL (undefined)**

```bash
cd server && go test ./api/ -run "TestSelectModel|TestCalcCost" -v 2>&1 | head -20
```

Expected: compile error — `selectModel`, `ModelHaiku`, etc. undefined.

- [ ] **Step 1.3: Create `pricing.go`**

Create `server/api/pricing.go`:

```go
package api

const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-6"

	// EscalateAfterChars is the message length threshold above which
	// selectModel auto-escalates to Sonnet 4.6.
	EscalateAfterChars = 500
)

// modelPrices maps model IDs to USD cost per 1M tokens.
// Source: Anthropic June 2026 billing documentation.
// Update when Anthropic changes rates.
var modelPrices = map[string]struct{ InputPer1M, OutputPer1M float64 }{
	ModelHaiku:  {0.80, 4.00},
	ModelSonnet: {3.00, 15.00},
	ModelOpus:   {15.00, 75.00},
}

// selectModel picks the model for a managed session turn.
// Priority: explicit user override > image presence > message length > default (Haiku).
func selectModel(message string, hasImages bool, userOverride string) string {
	if userOverride != "" {
		return userOverride
	}
	if hasImages || len(message) > EscalateAfterChars {
		return ModelSonnet
	}
	return ModelHaiku
}

// calcCost returns the estimated USD cost for a single turn.
// Returns 0 for unknown models — never panics.
func calcCost(model string, inputTokens, outputTokens int) float64 {
	p, ok := modelPrices[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1e6)*p.InputPer1M +
		(float64(outputTokens)/1e6)*p.OutputPer1M
}
```

- [ ] **Step 1.4: Run tests — expect PASS**

```bash
cd server && go test ./api/ -run "TestSelectModel|TestCalcCost" -v
```

Expected: all 8 tests PASS.

- [ ] **Step 1.5: Commit**

```bash
cd server && git add api/pricing.go api/pricing_test.go
git commit -m "feat(api): add model routing and cost calculation (pricing.go)"
```

---

## Task 2: Add variadic `transform` hook to `StreamNDJSON`

**Files:**
- Modify: `server/managed/stream.go`
- Modify: `server/managed/stream_test.go`

- [ ] **Step 2.1: Write the failing test**

Open `server/managed/stream_test.go` and add after the last test (before the closing brace of the file):

```go
func TestStreamNDJSON_TransformReplacesLine(t *testing.T) {
	input := `{"type":"result","subtype":"success"}` + "\n"
	b := NewBroadcaster()
	ch := b.Subscribe()

	received := make(chan string, 1)
	go func() {
		for msg := range ch {
			received <- msg
			return
		}
	}()

	transform := func(line string) string {
		return `{"type":"result","subtype":"success","cost":0.001,"model":"claude-haiku-4-5-20251001"}`
	}

	StreamNDJSON(strings.NewReader(input), b, nil, nil, transform)
	b.Close()

	select {
	case msg := <-received:
		if !strings.Contains(msg, `"cost":0.001`) {
			t.Errorf("transform not applied; got: %s", msg)
		}
		if !strings.Contains(msg, `"model"`) {
			t.Errorf("model field missing; got: %s", msg)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for transformed broadcast")
	}
}

func TestStreamNDJSON_NilTransformBroadcastsRaw(t *testing.T) {
	input := `{"type":"result","subtype":"success"}` + "\n"
	b := NewBroadcaster()
	ch := b.Subscribe()

	received := make(chan string, 1)
	go func() {
		for msg := range ch {
			received <- msg
			return
		}
	}()

	StreamNDJSON(strings.NewReader(input), b, nil, nil)
	b.Close()

	select {
	case msg := <-received:
		if strings.Contains(msg, "cost") {
			t.Errorf("raw line should not have cost field; got: %s", msg)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for broadcast message")
	}
}
```

- [ ] **Step 2.2: Run tests — expect FAIL**

```bash
cd server && go test ./managed/ -run "TestStreamNDJSON_Transform" -v
```

Expected: compile error — `StreamNDJSON` called with 5 args but only takes 4.

- [ ] **Step 2.3: Update `StreamNDJSON` signature and broadcast logic**

In `server/managed/stream.go`, change the function signature and the broadcast step.

Old signature:
```go
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string), turnDone chan<- struct{}) {
```

New signature:
```go
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string), turnDone chan<- struct{}, transform ...func(string) string) {
```

Inside the `case res, ok := <-lineCh:` block, replace:
```go
			b.Send(res.line)
```
with:
```go
			line := res.line
			if len(transform) > 0 && transform[0] != nil {
				line = transform[0](res.line)
			}
			b.Send(line)
```

The `onLine` callback still receives the **original** line — the transform only affects what gets broadcast. `onLine(res.line)` stays unchanged.

- [ ] **Step 2.4: Run all managed tests — expect PASS**

```bash
cd server && go test ./managed/ -v
```

Expected: all tests PASS. Existing callers (`StreamNDJSON(r, b, nil, nil)`) work unchanged via variadic.

- [ ] **Step 2.5: Confirm the whole server compiles**

```bash
cd server && go build ./...
```

Expected: no errors.

- [ ] **Step 2.6: Commit**

```bash
git add server/managed/stream.go server/managed/stream_test.go
git commit -m "feat(managed): add variadic transform hook to StreamNDJSON"
```

---

## Task 3: Wire model routing and cost enrichment in `handleSendMessage`

**Files:**
- Modify: `server/api/managed_sessions.go`
- Modify: `server/api/managed_sessions_test.go`

- [ ] **Step 3.1: Read the existing managed_sessions_test.go pattern**

The test server uses `ClaudeBin: "echo"` so `claude -p` becomes `echo -p ...`. The echo process exits immediately with no NDJSON output. That's fine for testing session creation and send-message HTTP responses — it doesn't test SSE content. For model routing tests we only need to assert that the right model is stored in the DB and the right model was passed to `buildPersistentArgs`.

Add these tests to `server/api/managed_sessions_test.go`:

```go
func TestHandleSendMessage_HaikuDefault(t *testing.T) {
	// selectModel with short message and no images should pick Haiku
	model := selectModel("short msg", false, "")
	if model != ModelHaiku {
		t.Errorf("expected Haiku default, got %q", model)
	}
}

func TestHandleSendMessage_SonnetEscalation_LongMessage(t *testing.T) {
	long := make([]byte, EscalateAfterChars+1)
	for i := range long {
		long[i] = 'x'
	}
	model := selectModel(string(long), false, "")
	if model != ModelSonnet {
		t.Errorf("expected Sonnet escalation for long message, got %q", model)
	}
}

func TestHandleSendMessage_SonnetEscalation_Images(t *testing.T) {
	model := selectModel("short", true, "")
	if model != ModelSonnet {
		t.Errorf("expected Sonnet escalation for images, got %q", model)
	}
}

func TestHandleSendMessage_UserOverride(t *testing.T) {
	model := selectModel("short", false, ModelOpus)
	if model != ModelOpus {
		t.Errorf("expected user override Opus, got %q", model)
	}
}

func TestEnrichResultLine_AddsModelAndCost(t *testing.T) {
	line := `{"type":"result","subtype":"success","usage":{"input_tokens":1000,"output_tokens":100}}`
	enriched := enrichResultLine(line, ModelHaiku)

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(enriched), &obj); err != nil {
		t.Fatalf("enriched line is not valid JSON: %v", err)
	}
	if _, ok := obj["cost"]; !ok {
		t.Error("enriched line missing 'cost' field")
	}
	if obj["model"] != ModelHaiku {
		t.Errorf("model=%v, want %q", obj["model"], ModelHaiku)
	}
	// 1000 input at $0.80/M + 100 output at $4.00/M = 0.0008 + 0.0004 = 0.0012
	cost, _ := obj["cost"].(float64)
	if cost == 0 {
		t.Error("cost should be non-zero for known model and non-zero token counts")
	}
}

func TestEnrichResultLine_PassesThroughNonResult(t *testing.T) {
	line := `{"type":"assistant","message":{"content":"hello"}}`
	got := enrichResultLine(line, ModelHaiku)
	if got != line {
		t.Errorf("non-result line should be unchanged; got %q", got)
	}
}

func TestEnrichResultLine_PassesThroughMissingUsage(t *testing.T) {
	line := `{"type":"result","subtype":"error_during_execution"}`
	got := enrichResultLine(line, ModelHaiku)
	if got != line {
		t.Errorf("result without usage should be unchanged; got %q", got)
	}
}
```

- [ ] **Step 3.2: Run tests — expect FAIL**

```bash
cd server && go test ./api/ -run "TestHandleSendMessage_|TestEnrichResultLine" -v 2>&1 | head -20
```

Expected: compile error — `enrichResultLine` undefined.

- [ ] **Step 3.3: Extract `enrichResultLine` as a standalone function**

Add this function to `server/api/managed_sessions.go` (place it near the bottom with other helpers, after `jsonString`):

```go
// enrichResultLine injects "cost" and "model" fields into a result NDJSON event.
// Returns the original line unchanged if it is not a result event, has no usage,
// or cannot be parsed. This function is called by the StreamNDJSON transform hook.
func enrichResultLine(line string, model string) string {
	var typ struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &typ) != nil || typ.Type != "result" {
		return line
	}

	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &raw) != nil {
		return line
	}

	usageBytes, ok := raw["usage"]
	if !ok {
		return line
	}
	var usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	if json.Unmarshal(usageBytes, &usage) != nil || usage.InputTokens == 0 {
		return line
	}

	cost := calcCost(model, usage.InputTokens, usage.OutputTokens)
	raw["cost"], _ = json.Marshal(cost)
	raw["model"], _ = json.Marshal(model)

	enriched, err := json.Marshal(raw)
	if err != nil {
		return line
	}
	return string(enriched)
}
```

- [ ] **Step 3.4: Run tests — expect PASS**

```bash
cd server && go test ./api/ -run "TestHandleSendMessage_|TestEnrichResultLine|TestSelectModel|TestCalcCost" -v
```

Expected: all tests PASS.

- [ ] **Step 3.5: Wire routing and enrichment into `handleSendMessage`**

In `server/api/managed_sessions.go`, inside the `go func()` that handles the message, add these blocks **before** the `continuationCount := 0` line at the top of the goroutine:

```go
		// Model routing — once per user message, before spawning the process.
		selectedModel := selectModel(req.Message, len(req.ImageIDs) > 0, req.Model)
		if sess.Model != selectedModel {
			sess.Model = selectedModel
			_ = s.store.UpdateSessionModel(sessionID, selectedModel)
		}
		reason := "auto"
		if req.Model != "" {
			reason = "user"
		}
		broadcaster.Send(fmt.Sprintf(`{"type":"model_selected","model":%q,"reason":%q}`, selectedModel, reason))

		// transform enriches result events with cost + model before broadcast.
		// Captures sess.Model by reference so it stays in sync if updated.
		transform := func(line string) string {
			return enrichResultLine(line, sess.Model)
		}
```

Then, in the `if !procStarted` block where `StreamNDJSON` is called, change:

```go
				SafeGo("StreamNDJSON:"+sessionID, func() {
					defer close(streamDone)
					managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, turnDone)
				})
```

to:

```go
				SafeGo("StreamNDJSON:"+sessionID, func() {
					defer close(streamDone)
					managed.StreamNDJSON(proc.Stdout, broadcaster, onLine, turnDone, transform)
				})
```

- [ ] **Step 3.6: Build the server — confirm no compile errors**

```bash
cd server && go build ./...
```

Expected: clean build.

- [ ] **Step 3.7: Run all server tests**

```bash
cd server && go test ./... -v 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 3.8: Commit**

```bash
git add server/api/managed_sessions.go server/api/managed_sessions_test.go
git commit -m "feat(api): wire Haiku model routing and result event cost enrichment"
```

---

## Task 4: UI — Auto option, `model_selected` handler, compact guard fix, billing callout

**Files:**
- Modify: `server/web/static/index.html`
- Modify: `server/web/static/app.js`

### 4a: Add "Auto" option to model picker and change default

- [ ] **Step 4.1: Add "Auto" option to the model `<select>` in `index.html`**

Find this block in `server/web/static/index.html` (around line 418):

```html
                <select x-model="selectedModel"
                        x-show="currentSession?.mode === 'managed' && !shellMode"
                        x-cloak
                        @change="localStorage.setItem('claude-controller-model', selectedModel)"
                        aria-label="Model"
                        class="model-select-btn">
                  <option value="claude-opus-4-6">Opus</option>
                  <option value="claude-sonnet-4-6">Sonnet</option>
                  <option value="claude-haiku-4-5-20251001">Haiku</option>
                </select>
```

Replace with:

```html
                <select x-model="selectedModel"
                        x-show="currentSession?.mode === 'managed' && !shellMode"
                        x-cloak
                        @change="localStorage.setItem('claude-controller-model', selectedModel)"
                        aria-label="Model"
                        class="model-select-btn">
                  <option value="">Auto</option>
                  <option value="claude-opus-4-6">Opus</option>
                  <option value="claude-sonnet-4-6">Sonnet</option>
                  <option value="claude-haiku-4-5-20251001">Haiku</option>
                </select>
```

- [ ] **Step 4.2: Change default `selectedModel` in `app.js`**

Find line 39 in `server/web/static/app.js`:

```js
    selectedModel: localStorage.getItem('claude-controller-model') || 'claude-sonnet-4-6',
```

Replace with:

```js
    selectedModel: localStorage.getItem('claude-controller-model') || '',
```

### 4b: Handle `model_selected` SSE event

- [ ] **Step 4.3: Add `model_selected` handler in `app.js`**

Find the existing system init handler (around line 2139):

```js
          // Capture model name from system init event
          if (data.type === 'system' && data.subtype === 'init' && data.model) {
            this.sessionModel = data.model;
          }
```

Add the `model_selected` handler directly above it:

```js
          // Update displayed model when server applies routing
          if (data.type === 'model_selected' && data.model) {
            this.sessionModel = data.model;
          }

          // Capture model name from system init event
          if (data.type === 'system' && data.subtype === 'init' && data.model) {
            this.sessionModel = data.model;
          }
```

### 4c: Fix compact guard so cost accumulates during compact turns

- [ ] **Step 4.4: Move cost accumulation above the `isCompacting` early return**

Find this block in `app.js` (around line 2097):

```js
          if (data.type === 'done' || data.type === 'result') {
            // Skip result events from compact process — they'd pollute cost/pill state
            if (data.type === 'result' && this.isCompacting) {
              return;
            }
            // Track cost from result events
            if (data.type === 'result' && data.cost != null) {
              this.sessionCost = (this.sessionCost || 0) + data.cost;
            }
```

Replace with:

```js
          if (data.type === 'done' || data.type === 'result') {
            // Track cost from result events — always, including compact turns
            if (data.type === 'result' && data.cost != null) {
              this.sessionCost = (this.sessionCost || 0) + data.cost;
            }
            // Skip pill state updates during compact to avoid UI artifacts
            if (data.type === 'result' && this.isCompacting) {
              return;
            }
```

### 4d: Add billing callout to the new-session modal

- [ ] **Step 4.5: Add the callout banner in `index.html`**

Find the path input section in the new-session modal (around line 1096):

```html
      <!-- Path input -->
      <div style="padding:0 24px 12px;">
```

Add the callout **above** the path input `<div>`:

```html
      <!-- Agent SDK billing callout -->
      <div style="padding:8px 24px 0; font-size:11px; color:var(--text-muted); line-height:1.5;">
        Managed sessions use your Agent SDK credit pool — separate from the 5hr/7d interactive usage shown in the header. Haiku 4.5 is the default to minimize cost.
      </div>

      <!-- Path input -->
      <div style="padding:0 24px 12px;">
```

### 4e: Verify visually and commit

- [ ] **Step 4.6: Start the server and verify in the browser**

```bash
cd server && go run . --port 3001
```

Open `http://localhost:3001`. Check:
1. Model picker in input bar shows "Auto / Opus / Sonnet / Haiku" — default is "Auto"
2. Status line shows `Cost $0.0000` initially
3. New Session modal shows the billing callout above the path input
4. In a managed session: sending a message triggers `model_selected` event → status line Model updates

- [ ] **Step 4.7: Commit UI changes**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat(ui): Auto model default, model_selected handler, compact cost guard fix, billing callout"
```

---

## Task 5: Create GitHub issue for Approach A (Interactive Pool / PTY)

- [ ] **Step 5.1: Create the GitHub issue**

```bash
gh issue create \
  --title "feat(managed): replace claude -p with interactive PTY to qualify for Interactive Pool billing" \
  --body "$(cat <<'EOF'
## Background

On June 15, 2026, Anthropic splits billing into two pools:
- **Interactive Pool** (subscription-covered): web, mobile, interactive Claude Code terminal sessions
- **Agent SDK Credit Pool** (metered at full API list prices): `claude -p`, headless mode, CI/CD

Claude-controller's managed mode calls `claude -p` (see `buildPersistentArgs` in `server/api/managed_sessions.go:71`), placing every managed session turn in the Agent SDK Credit Pool.

Approach B (model routing + cost visibility) has been shipped to reduce spend within the Agent SDK pool. This issue tracks **Approach A**: escaping the pool entirely by qualifying managed sessions as Interactive.

## The Human-in-the-Loop Test

From Anthropic's June 2026 billing documentation:
> If a Claude session runs autonomously without a human validating each turn (cron job, CI script, background agent), it draws from the Agent SDK Credit Pool. If a human is actively driving the conversation turn-by-turn in a supported first-party interface, it remains within the Interactive Subscription.

Claude-controller's managed mode IS human-driven turn-by-turn via the web UI. The question is whether removing `-p` and running `claude` in interactive mode qualifies — and whether the billing system actually tracks this distinction via the flag or via OAuth session type.

## What We Know (from Approach B implementation)

- `buildPersistentArgs` (managed_sessions.go) assembles: `claude -p --resume <id> --output-format stream-json --input-format stream-json --verbose [--model] [--allowedTools] [--max-budget-usd]`
- The process is **persistent and warm** — it stays alive between turns, receiving user messages via stdin as stream-json, emitting NDJSON on stdout
- `StreamNDJSON` in `server/managed/stream.go` reads NDJSON → `Broadcaster` → SSE to browser
- The entire managed session pipeline depends on `--output-format stream-json` / `--input-format stream-json` producing clean NDJSON

## Key Unknowns

1. **Does `claude --output-format stream-json --input-format stream-json` (no `-p`) work without a TTY?** The `-p` flag explicitly enables non-interactive/print mode. Without it, claude may require a TTY and reject piped stdin.
2. **Does Anthropic's billing system classify the session as Interactive without `-p`?** The classification may be based on the OAuth session type, not the CLI flag. Removing `-p` may not move the billing bucket.
3. **PTY management complexity**: if `claude` requires a TTY, we need `os.StartProcess` with a PTY on Unix (using `github.com/creack/pty` or similar) and a different approach on Windows. This is non-trivial and breaks the current `cmd.StdinPipe()` / `cmd.StdoutPipe()` architecture.

## Suggested First Step (before any code)

Test manually on the host machine:

```bash
# Test 1: does claude accept stream-json without -p?
echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"say hello"}]}}' | \
  claude --output-format stream-json --input-format stream-json --verbose

# Test 2: does it run without a TTY?
echo '...' | claude --output-format stream-json --input-format stream-json --verbose 2>&1
```

If Test 1 produces NDJSON and Test 2 succeeds without a TTY, removing `-p` from `buildPersistentArgs` is low-risk. If it requires a PTY, evaluate `github.com/creack/pty`.

## Files to Modify if Feasible

- `server/api/managed_sessions.go` — `buildPersistentArgs`: remove `-p` from args
- `server/managed/manager_unix.go` — `Spawn`: add PTY setup if required
- `server/managed/manager_windows.go` — Windows equivalent (or flag as unsupported)
- `server/managed/manager.go` — `Process` struct may need PTY file handle

## Acceptance Criteria

- [ ] Managed session messages do NOT draw from Agent SDK Credit Pool
- [ ] NDJSON streaming pipeline continues working (no change to SSE event shapes)
- [ ] Session continuity (`--resume`) continues working
- [ ] `--allowedTools`, `--max-budget-usd`, `--model` flags still apply
- [ ] Works on macOS (primary target) and does not regress Windows hook mode
EOF
)"
```

- [ ] **Step 5.2: Copy the issue URL and note it**

```bash
gh issue list --limit 1
```

Paste the URL as a comment in `docs/superpowers/specs/2026-05-18-managed-session-cost-visibility-design.md` under the Approach A section so it's linked from the spec.

---

## Self-Review Checklist

Run mentally before starting execution:

- [x] `pricing.go` covers all 4 `selectModel` cases and all `calcCost` model variants
- [x] `enrichResultLine` is extracted as a standalone testable function (not embedded in closure)
- [x] `StreamNDJSON` variadic keeps all 4 existing call sites backward-compatible
- [x] `onLine` still receives raw line (transform only affects broadcast)
- [x] Compact guard fix moves cost accumulation before the early `return`, not removes the guard
- [x] `model_selected` handler updates `sessionModel` (same field as `system.init` handler)
- [x] "Auto" option uses `value=""` matching what `selectModel(msg, false, "")` expects
- [x] Default `selectedModel` changed from `'claude-sonnet-4-6'` to `''`
- [x] GitHub issue body contains enough context for a fresh developer to continue Approach A
