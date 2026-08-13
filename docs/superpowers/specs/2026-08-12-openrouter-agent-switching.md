# Spike: OpenRouter for Switching Between Claude Code and Codex

**Date:** 2026-08-12
**Issue:** [#259](https://github.com/lankeami/claude-control/issues/259)
**Status:** Complete — recommendation at the end

## Question

Can [OpenRouter](https://openrouter.ai) act as a unified model gateway so a managed
session can switch between Claude Code and Codex (OpenAI's CLI agent) seamlessly —
without breaking the machinery Controller depends on (transcript tailing, hook-driven
turn lifecycle, permission bridging, subscription billing)?

## Baseline (the "red" state)

Verified 2026-08-12 on `main`:

- `grep -ri "codex\|openrouter" server/ hooks/` → no matches.
- The managed backend only ever spawns `claude`: `managed.Config.ClaudeBin`
  (`server/managed/manager.go:14`) feeds `exec.Command(cfg.ClaudeBin, ...)` at
  `server/managed/interactive.go:168` (interactive backend), `server/managed/manager.go:108`
  and `server/managed/manager.go:368` (print backend). Transcript tailing
  (`server/managed/transcript.go`) assumes Claude Code's `~/.claude/projects/` JSONL;
  turn lifecycle assumes Claude Code hooks injected via a generated `--settings` file
  (`server/managed/settings.go`).
- `cd server && go test ./...` green before and after this spike (no production code changed).

## Findings: Claude Code ↔ OpenRouter

**It works, and both sides officially document it — but only for Anthropic models, and
it forfeits subscription billing.**

- **Mechanism** (documented): Claude Code's LLM-gateway env vars —
  `ANTHROPIC_BASE_URL="https://openrouter.ai/api"` (Claude Code appends `/v1/messages`),
  `ANTHROPIC_AUTH_TOKEN="sk-or-..."` (OpenRouter key as Bearer), and `ANTHROPIC_API_KEY=""`
  (explicitly empty, or Claude Code can fall back to direct-Anthropic auth). Run `/logout`
  first if a claude.ai login is saved. Sources: [Claude Code LLM gateway docs](https://code.claude.com/docs/en/llm-gateway),
  [OpenRouter's Claude Code cookbook](https://openrouter.ai/docs/cookbook/coding-agents/claude-code-integration).
- **Anthropic models only**: OpenRouter's Anthropic-native endpoint rejects non-Anthropic
  models because Claude Code sends Anthropic-only beta features (context management) —
  confirmed breakage in [claude-agent-sdk-python#789](https://github.com/anthropics/claude-agent-sdk-python/issues/789).
  So OpenRouter does **not** buy "run Claude Code against GPT models"; it buys nothing
  Claude Code can't already do natively.
- **Billing — the killer**: per the official docs, while a gateway credential is active
  "the developer's claude.ai subscription isn't used... traffic is billed per token to
  whoever owns the credential." Subscription (OAuth) billing only works against Anthropic's
  own endpoint. Controller's interactive backend exists *specifically* so usage bills to
  the Claude subscription — routing through OpenRouter reverses that, at pass-through
  per-token rates (Sonnet 4.5: $3/$15 per M in/out; Opus 4.5: $5/$25) plus OpenRouter's
  5.5% (min $0.80) fee on credit purchases.
- **Client-side machinery unaffected**: hooks, transcripts, settings, and slash commands
  are all local; no gateway-related breakage reported. Prompt caching passes through on
  the Anthropic-native endpoint (OpenRouter documents Anthropic cache pricing), but no
  first-party statement guarantees Claude Code caching end-to-end — verify via
  `cache_discount` on real traffic before relying on it. Caching demonstrably breaks on
  OpenRouter's OpenAI-compatible path (vscode#312939, Roo-Code#1185, zed#52576), which
  does not apply here but is the commonly cited failure story.

## Findings: Codex ↔ OpenRouter

**Officially documented on OpenRouter's side, and Codex's lifecycle features are a near
1:1 match for everything Controller needs — in places better than PTY-driving Claude.**

- **Provider config** (documented, [OpenRouter Codex cookbook](https://openrouter.ai/docs/cookbook/coding-agents/codex-cli)):

  ```toml
  model = "anthropic/claude-sonnet-4.5"   # any OpenRouter slug
  model_provider = "openrouter"

  [model_providers.openrouter]
  name = "openrouter"
  base_url = "https://openrouter.ai/api/v1"
  env_key = "OPENROUTER_API_KEY"          # or auth.command to enable catalog fetch
  wire_api = "responses"
  ```

- **`wire_api` timeline matters**: Codex ≥ 0.147 **removed** `wire_api = "chat"` — the CLI
  refuses to start with it (verified locally, see PoC). Third-party providers must speak
  the OpenAI **Responses API**, which OpenRouter now serves in beta
  ([Responses API docs](https://openrouter.ai/docs/api/reference/responses/tool-calling)).
  Older blog posts saying "use `wire_api = \"chat\"` for OpenRouter" are obsolete.
- **Session/transcript format**: rollouts at
  `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl` — date-sharded
  (vs Claude's per-project sharding), each line `{timestamp, type, payload}`. Types
  observed locally: `session_meta` (header, has session id + provider), `turn_context`,
  `response_item` (payload types `message`, `function_call`, `function_call_output`,
  `reasoning`, ...), `event_msg` (payload types `task_started`, `task_complete`,
  `user_message`, `agent_message`, `token_count`). Line-appended and tail-able with the
  same technique as `server/managed/transcript.go`, but the shapes are **not** Claude
  stream-json — an adapter is required before the existing SSE pipeline can consume them.
  `event_msg: token_count` gives a hook for per-turn cost tracking.
- **Hooks**: Codex now ships a first-party hook system nearly 1:1 with Claude Code's —
  `SessionStart`, `SessionEnd`, `Stop`, `PreToolUse`, `PostToolUse`, `PermissionRequest`,
  `UserPromptSubmit`, etc., configured in `hooks.json`/`config.toml`, JSON on stdin,
  deny/rewrite decisions on stdout ([Codex hooks docs](https://developers.openai.com/codex/hooks)).
  Controller's `hook-signal` relay pattern ports directly.
- **Headless control — the better path**: `codex app-server` is a long-lived JSON-RPC 2.0
  process over stdio with `thread/start`, `thread/resume`, `turn/start`,
  `turn/interrupt`, `turn/steer` (inject guidance mid-turn), streamed `item/*`
  notifications, and approvals surfaced to the client
  ([app-server docs](https://developers.openai.com/codex/app-server)). For a Codex
  backend this replaces the entire PTY + transcript-tail + hook-signal contraption.
  `codex exec --json` also exists as a print-mode analog. PTY-driving the TUI works too
  (ESC interrupts a turn; avoid Ctrl+C, which exits — [codex#14708](https://github.com/openai/codex/issues/14708)).
- **Billing**: Codex natively bills to a ChatGPT subscription (OAuth login), exactly
  parallel to Claude Code. Routing Codex through OpenRouter moves it to per-token API
  billing — same trade-off as Claude. OpenRouter is only necessary for Codex when the
  user wants non-OpenAI models inside Codex, or has no ChatGPT plan.

## PoC: Codex CLI → OpenRouter under a PTY

Environment: macOS arm64, codex-cli 0.147.0 (repaired from a broken 0.63.0 npm install
whose vendor binary was missing), isolated `CODEX_HOME`, PTY via `script(1)`.

**Result: infeasible to complete a live turn in this environment — no OpenRouter API key
exists on this machine** (not in env, shell profiles, `~/.config`, or any workspace
`.env`). The failure is precisely credential-shaped, not architectural:

1. With `wire_api = "chat"`: hard startup error — `` `wire_api = "chat"` is no longer
   supported. How to fix: set `wire_api = "responses"` `` (codex ≥ 0.147). Finding, not blocker.
2. With `wire_api = "responses"`: the provider config is **accepted** — a session spawns
   under the PTY (`provider: openrouter`, session id issued, prompt echoed, rollout JSONL
   written) and fails only at the auth step:
   `ERROR: Missing environment variable: OPENROUTER_API_KEY`.
3. Variable isolation (per failure path): Codex against OpenAI directly is *also* blocked
   on this machine — the ChatGPT OAuth refresh token is stale ("refresh token was already
   used. Please log out and sign in again", 401 on `wss://chatgpt.com/backend-api/codex/responses`)
   and `auth.json`'s `OPENAI_API_KEY` is null. So the blocker is credentials, not
   OpenRouter, Codex version, or PTY handling.
4. Endpoint existence probed without credentials: unauthenticated POSTs to
   `https://openrouter.ai/api/v1/{responses,chat/completions,messages}` all return
   **401** (auth required) — not 404 — confirming OpenRouter serves the Responses API
   Codex needs and the Messages API Claude Code needs.

Artifacts (in `docs/superpowers/specs/artifacts/`):

| File | What it shows |
|---|---|
| `2026-08-12-codex-openrouter-attempt-pty.txt` | ANSI-stripped PTY capture: session banner with `provider: openrouter`, prompt, exact blocking error |
| `2026-08-12-codex-openrouter-rollout.jsonl` | The session's rollout JSONL — real Codex transcript format incl. `session_meta`, `event_msg`/`task_complete` carrying the error |
| `2026-08-12-codex-openrouter-config.toml` | The exact provider config Codex accepted |

Re-running with a key is a five-minute exercise: `export OPENROUTER_API_KEY=sk-or-...`
and repeat the step-2 command.

## Gap table

| Concern | Claude Code (today) | Codex, native | Codex via OpenRouter | Gap severity |
|---|---|---|---|---|
| Transcript format | `~/.claude/projects/*.jsonl`, parent-linked, stream-json shapes | `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl`, `{timestamp,type,payload}` envelope | same as native | **Medium** — tail-able, but needs a shape adapter before the SSE pipeline |
| Hooks / turn lifecycle | Stop/Notification/SessionStart via generated settings | First-party hooks, near 1:1 event names, same stdin-JSON pattern | same | **Low** — `hook-signal` ports directly; app-server makes hooks optional |
| Permission bridging | `PermissionRequest` hook → long-poll → Allow/Deny card | `PermissionRequest` hook with `behavior: deny`, or app-server approvals | same | **Low** |
| Question detection | PreToolUse on `AskUserQuestion` | No AskUserQuestion equivalent; approvals + steering instead | same | **Medium** — UI card semantics differ |
| Interrupts | ESC keystroke into PTY | ESC in PTY, or `turn/interrupt` JSON-RPC (clean) | same | **None** |
| Billing | Claude subscription (whole point of interactive backend) | ChatGPT subscription (OAuth) | Per-token OpenRouter credits, +5.5% purchase fee | **High** for OpenRouter path; **none** for native |
| Claude Code via OpenRouter | n/a | n/a | Works (Anthropic models only) but kills subscription billing | **Fatal** for the "unified gateway" idea |
| Privacy | Local-only tool; prompts go to model vendor | Prompts to OpenAI | Prompts additionally transit OpenRouter | **Medium** — posture change, must be opt-in |

## Recommendation: conditional GO

**No-go on the headline idea** — OpenRouter as *the* unified gateway both agents route
through. Putting Claude Code behind OpenRouter buys zero capability (Anthropic models
only), abandons the subscription billing the interactive backend exists to preserve, adds
~5.5% cost, and inserts a third party into a deliberately local-only tool's prompt path.

**Go on the underlying goal** — agent switching — via a different architecture:

1. **Each agent runs native by default.** Claude Code keeps the current interactive
   backend and subscription billing, untouched. Codex authenticates with its own ChatGPT
   OAuth login and bills to that subscription — the exact symmetric trade.
2. **OpenRouter becomes an optional per-agent provider config, not infrastructure.**
   For Codex it's a user-supplied `model_providers.openrouter` block +
   `OPENROUTER_API_KEY` (validated end-to-end here up to the auth step; OpenRouter's
   Responses API endpoint confirmed live). For Claude Code it's the documented gateway
   env vars. Controller never stores or requires an OpenRouter key.
3. **Proposed seam in `server/managed/`:**
   - `agent` column on sessions (`"claude"` default, `"codex"`), alongside the existing
     `mode` field in `server/db/sessions.go:22` — mirroring how `mode` already selects
     hook vs managed.
   - `managed.Config` grows `CodexBin` next to `ClaudeBin` (`server/managed/manager.go:14`).
   - The three `exec.Command(cfg.ClaudeBin, ...)` sites stay Claude-only; a new
     `codex.go` backend implements the same lifecycle contract the interactive backend
     satisfies today (start, send prompt, interrupt, activity state, per-turn cost) —
     backed by **`codex app-server` JSON-RPC** rather than PTY + transcript tailing,
     which eliminates the settings-file/hook-injection machinery for Codex entirely.
   - A transcript adapter translates app-server `item/*` notifications (or rollout
     JSONL entries, as fallback) into the stream-json shapes the existing SSE
     broadcaster and `messages` table already consume — the web UI/iOS app stay
     agent-agnostic.
4. **Privacy note carried into implementation:** any OpenRouter routing must be explicit
   user configuration with a visible indicator, never a default.

## Follow-up execution issues

Created via `/git-issue-create`, one per implementation boundary:

- [#260](https://github.com/lankeami/claude-control/issues/260) — Agent-selection seam: `agent` field on sessions and backend dispatch in `server/managed/`
- [#261](https://github.com/lankeami/claude-control/issues/261) — Codex backend via `codex app-server` with transcript adapter to the existing SSE pipeline
- [#262](https://github.com/lankeami/claude-control/issues/262) — Web UI + iOS agent picker and per-agent status/cost display
