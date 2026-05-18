# Managed Session Cost Visibility & Model Routing — Design Spec

**Date:** 2026-05-18
**Status:** Approved
**Context:** Anthropic's June 15, 2026 billing transition splits usage into the Interactive Pool (subscription-covered: web, mobile, interactive Claude Code) and the Agent SDK Credit Pool (metered at full API list prices: `claude -p`, headless mode, CI). Claude-controller's managed mode spawns `claude -p` for every message, placing all managed session usage in the Agent SDK Credit Pool. This spec covers cost reduction and visibility for managed sessions (Approach B). Approach A — replacing `claude -p` with an interactive PTY session to qualify for the Interactive Pool — is tracked in a separate GitHub issue.

---

## Goals

1. Reduce per-turn managed session cost via model routing (Haiku 4.5 default, Sonnet 4.6 escalation).
2. Fix the `sessionCost` widget always showing $0 by computing cost server-side and enriching result events.
3. Surface the current model and running session cost in the UI so users understand what they're spending.
4. Make the Interactive Pool / Agent SDK Credit Pool separation explicit so users don't conflate the 5hr/7d utilization bars with managed session spend.

---

## Out of Scope

- Replacing `claude -p` with interactive PTY mode (tracked separately as Approach A).
- Persisting token counts or cost to the database (no schema changes).
- Querying Anthropic's SDK credit balance via API (no endpoint exists yet).
- Prompt caching headers (separate future optimization).

---

## Architecture

Three concerns, each bounded to specific files:

1. **Model routing** — server-side only, `server/api/managed_sessions.go` + new `server/api/pricing.go`.
2. **Cost enrichment** — server-side, `onLine` callback in `handleSendMessage`, enriches `result` events before broadcast.
3. **UI** — four touch points in `server/web/static/app.js` and `server/web/static/index.html`.

No DB schema changes. No new API endpoints.

---

## Section 1: Model Routing

### Model constants

New file `server/api/pricing.go`:

```go
package api

const (
    ModelHaiku  = "claude-haiku-4-5-20251001"
    ModelSonnet = "claude-sonnet-4-6"
    ModelOpus   = "claude-opus-4-6"

    // EscalateAfterChars is the message length threshold for auto-escalation to Sonnet.
    EscalateAfterChars = 500
)

// modelPrices maps model IDs to USD cost per 1M tokens.
// Source: Anthropic June 2026 billing documentation.
// Update these when Anthropic changes rates.
var modelPrices = map[string]struct{ InputPer1M, OutputPer1M float64 }{
    ModelHaiku:  {0.80, 4.00},
    ModelSonnet: {3.00, 15.00},
    ModelOpus:   {15.00, 75.00},
}

// selectModel picks the model for a turn.
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

// calcCost returns the estimated USD cost for a turn.
// Returns 0 for unknown models rather than erroring.
func calcCost(model string, inputTokens, outputTokens int) float64 {
    p, ok := modelPrices[model]
    if !ok {
        return 0
    }
    return (float64(inputTokens)/1e6)*p.InputPer1M +
        (float64(outputTokens)/1e6)*p.OutputPer1M
}
```

### Application in `handleSendMessage`

At the top of `handleSendMessage`, before `EnsureProcess`:

```go
selectedModel := selectModel(req.Message, len(req.ImageIDs) > 0, req.Model)
if sess.Model != selectedModel {
    sess.Model = selectedModel
    _ = s.store.UpdateSessionModel(sessionID, selectedModel)
}

// Broadcast model selection event so frontend can show escalation notice.
reason := "user"
if req.Model == "" {
    reason = "auto"
}
modelEvent := fmt.Sprintf(`{"type":"model_selected","model":%q,"reason":%q}`, selectedModel, reason)
broadcaster.Send(modelEvent)
```

### Process lifetime behaviour

`buildPersistentArgs` uses `sess.Model` to set `--model`. The model is fixed for the lifetime of a warm process run. It re-evaluates on next process spawn (after process death + restart, or after `/clear`). If the user changes model via the picker mid-session, it applies on the next process spawn.

`UpdateSessionModel` already exists in `db/sessions.go` — no schema change needed.

---

## Section 2: Per-Turn Cost Tracking (fixing $0 bug)

### Root cause

`app.js:2103` accumulates cost via:
```js
if (data.type === 'result' && data.cost != null)
```

Claude's CLI `result` events include `usage.input_tokens` / `usage.output_tokens` but no `cost` field. The server passes raw NDJSON through unchanged. So `data.cost` is always `null` and `sessionCost` never increments.

### Fix: enrich the `result` event before broadcasting

In the `onLine` callback in `handleSendMessage`, when `role == "result"`, parse the usage, compute cost, and re-marshal the enriched JSON before broadcasting — instead of letting `StreamNDJSON` broadcast the raw line.

Implementation approach:
- `StreamNDJSON` broadcasts raw lines.
- `onLine` is called synchronously for each line.
- For `result` lines: parse usage, compute cost, construct an enriched version with `cost` and `model` fields, then call `broadcaster.Send(enrichedLine)`.
- The raw line from `StreamNDJSON` must NOT be broadcast for `result` events. Add a variadic `transform ...func(string) string` parameter to `StreamNDJSON`. If provided and non-nil, its return value replaces the raw line before broadcast. Variadic keeps all existing call sites (`RunCompact`, tests) backward-compatible — they pass no transform and get unchanged behaviour.

Signature change in `server/managed/stream.go`:
```go
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string), turnDone chan<- struct{}, transform ...func(string) string)
```
Inside the broadcast step: if `len(transform) > 0 && transform[0] != nil`, broadcast `transform[0](line)` instead of the raw line.

Enriched result event shape (adds `cost` and `model` to existing fields):
```json
{
  "type": "result",
  "subtype": "success",
  "usage": {"input_tokens": 1240, "output_tokens": 87},
  "cost": 0.000449,
  "model": "claude-haiku-4-5-20251001"
}
```

No frontend changes needed for cost accumulation — `data.cost` check already works.

### Cost for compact turns

Compact turns (`/compact` sent via stdin on the warm process) also emit `result` events. These should be costed normally — they consume real tokens. The existing `isCompacting` guard in the frontend (`app.js:2099`) wraps an early `return` that skips both cost accumulation and pill state updates. Do NOT remove the guard entirely — pill state should still be skipped during compact to avoid UI artifacts. Instead, move the cost accumulation (`sessionCost += data.cost`) to execute before the `isCompacting` early return, so compact token spend is counted but pill handling is still suppressed.

---

## Section 3: UI Components

### 3a. Model badge on assistant turn bubbles

Each assistant message bubble gets a small pill below the text, populated from the `model` and `cost` fields on the corresponding `result` event. The pill is matched to the most recent assistant message at the time the `result` event arrives.

Format: `haiku-4-5 · 1,240 in · 87 out · $0.0004`

If model was auto-escalated (reason = "auto"), prefix with: `↑ sonnet-4-6 (auto) · ...`

### 3b. Running cost widget in session header

Placed next to the existing 5hr/7d usage rate limit widget. Accumulates `cost` from result events in Alpine state (`sessionCost`). Resets to $0.00 on page load (ephemeral — shows "cost this session view").

Format: `Session cost: $0.0023`

Important: the widget label or a tooltip must clarify this is Agent SDK Credit spend, separate from the 5hr/7d Interactive Pool shown beside it.

### 3c. Model picker dropdown in session header

Dropdown with four options:
- `Auto (Haiku / Sonnet)` — heuristic routing, default
- `Haiku 4.5` — always use Haiku
- `Sonnet 4.6` — always use Sonnet
- `Opus 4.6` — always use Opus

Selection is stored in Alpine session state and sent as `model` in the next message request body. The server's `selectModel()` treats it as `userOverride`, bypassing the heuristic. No new API endpoint — the existing `model` field in `POST /api/sessions/:id/message` handles it.

### 3d. Billing callout in new-session modal

Static single-line info banner above the CWD input field:

> Agent SDK credits are used for managed sessions — separate from the 5hr/7d interactive usage shown in the header. Haiku 4.5 is the default to minimize cost.

No interaction required.

---

## Section 4: Data Flow

```
User sends message (+ optional model override via dropdown)
    |
    v
handleSendMessage
    |- selectModel(message, hasImages, req.Model) -> selectedModel
    |- UpdateSessionModel(selectedModel) if changed
    |- broadcast: {"type":"model_selected","model":"...","reason":"auto"|"user"}
    |
    |- EnsureProcess -> buildPersistentArgs (uses sess.Model = selectedModel)
    |- SendTurn via stdin
    |
    v
StreamNDJSON (with transformLine hook)
    |- role == "assistant" -> persist text, tool names (unchanged)
    |- role == "result"
    |     |- parse subtype, errors (existing)
    |     |- parse usage.input_tokens + usage.output_tokens
    |     |- calcCost(sess.Model, ...) -> inject "cost" + "model" into JSON
    |     |- broadcast enriched result (transformLine returns enriched JSON)
    |
    v
SSE -> browser
    |- model_selected  -> show escalation notice inline in chat
    |- result (enriched) -> data.cost increments sessionCost
                         -> model badge attached to last assistant bubble
    |- done            -> finalize pills, stop SSE (unchanged)
```

---

## Section 5: Error Handling

| Scenario | Behaviour |
|---|---|
| `usage` missing or zero in `result` event | Skip cost enrichment, broadcast original line. No error. |
| Unknown model in `calcCost` | Returns 0.0. Cost badge shows $0.0000. No crash. |
| `UpdateSessionModel` DB error | Log and continue. `sess.Model` updated in-memory, process gets correct model. |
| User sends unknown model string as override | Passed through `selectModel` as `userOverride`, set on `sess.Model`, passed to `--model`. Claude CLI rejects invalid models with non-zero exit — same error path as today. |
| Page refresh mid-session | `sessionCost` resets to null. Widget shows "$0.00 (this view)". Acceptable for v1. |

---

## Section 6: Testing

| Test | Location | What it covers |
|---|---|---|
| `TestSelectModel` | `pricing_test.go` | All 4 cases: override, images, long message, default |
| `TestCalcCost` | `pricing_test.go` | Known token counts vs expected USD for each model |
| `TestCalcCost_UnknownModel` | `pricing_test.go` | Returns 0, no panic |
| `TestHandleSendMessage_ModelSelected` | `managed_sessions_test.go` | `model_selected` event broadcast on send |
| `TestHandleSendMessage_CostEnriched` | `managed_sessions_test.go` | `result` event contains `cost` and `model` fields |
| `TestHandleSendMessage_HaikuDefault` | `managed_sessions_test.go` | Short message with no images gets Haiku |
| `TestHandleSendMessage_SonnetEscalation` | `managed_sessions_test.go` | Long message gets Sonnet |

No new integration tests for the UI cost widget (ephemeral Alpine state, verified manually via Playwright).

---

## Approach A — GitHub Issue (to be created after spec approval)

A GitHub issue will be filed covering the Interactive Pool escape via interactive PTY:

**Title:** `feat(managed): replace claude -p with interactive PTY to qualify for Interactive Pool billing`

**Body will include:**
- Summary of the June 15 billing transition and why `-p` hits Agent SDK Credits
- The "Human-in-the-Loop" test from Anthropic's documentation
- What we know from implementing Approach B: the persistent warm process model, the `--output-format stream-json` / `--input-format stream-json` flags, the NDJSON → SSE pipeline
- Unknowns: whether claude supports `--output-format stream-json` without `-p`; whether Anthropic classifies it as interactive even without `-p`; PTY management complexity on Unix vs Windows
- Suggested first step: test `claude --output-format stream-json --input-format stream-json` (no `-p`) and observe whether it (a) accepts stdin without a TTY and (b) produces the same NDJSON output
- Files to modify if feasible: `server/api/managed_sessions.go` (`buildPersistentArgs`), `server/managed/manager_unix.go` (PTY spawn), `server/managed/manager_windows.go`
