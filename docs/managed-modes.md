# Managed Session Backends: Interactive vs Print

Managed sessions (the chat interface in the web UI / iOS app) can run on one of
two backends. Both produce the same user experience — same chat UI, same SSE
streaming, same session lifecycle — but they drive Claude Code very differently
and **bill differently**.

| | `interactive` (default) | `print` (legacy) |
|---|---|---|
| Process model | One long-lived interactive `claude` per session, under a PTY | Warm `claude -p --input-format stream-json` process per session |
| Billing | Claude Code **subscription** | Anthropic **API** (metered) |
| Output source | Tails the native transcript JSONL in `~/.claude/projects/` | Parses NDJSON from the process's stdout |
| Turn boundaries | Stop hook fires → server ends the turn | `result` event on stdout |
| Tool restrictions | `permissions.allow` in a generated per-session settings file | `--allowedTools` CLI flag |
| Turn limits | Server counts assistant transcript entries, sends ESC | `--max-turns` CLI flag |
| Budget caps | Server sums per-turn cost from transcript usage | `--max-budget-usd` CLI flag |
| Interrupt | ESC keystroke to the PTY (process survives) | SIGINT to the process |
| Permission prompts | Notification hook surfaces the prompt text (no remote approve/deny) | MCP bridge (`--permission-prompt-tool`) with approve/deny in the UI |
| Images | Saved to disk, referenced by path in the prompt | Inline base64 via stream-json |
| Platforms | macOS / Linux | All (forced on Windows — no ConPTY support yet) |

> **Why two backends?** Pricing changes bill `claude -p` (headless) usage
> against the Anthropic API instead of the Claude Code subscription. The
> interactive backend keeps managed sessions on subscription billing. The
> print backend is kept for one release as a rollback path (issue #174).

## Choosing a mode

Precedence: `--managed-mode` CLI flag > `MANAGED_MODE` environment variable
(including `.env`) > default (`interactive`).

```bash
# Default — interactive backend
make local

# One-off run on the legacy claude -p backend
make local MANAGED_MODE=print

# Persistent choice — add to .env (the make command line still wins)
echo "MANAGED_MODE=print" >> .env

# Running the binary directly
./server/claude-controller --port 9999 --managed-mode print
MANAGED_MODE=print go run .   # from server/
```

On startup the mode is fixed for the server's lifetime; restart to switch.
Existing sessions work in either mode — the session data (messages, turn
counts, budget spend) is shared, only the process driving Claude changes.

## How the interactive backend works

1. **Spawn** — the first message to a session starts `claude --session-id <id>`
   (or `--resume <claude-session-id>` for resumed sessions) under a
   pseudo-terminal (`creack/pty`), with `--settings` pointing at a generated
   per-session settings file in `~/.claude-controller/sessions/<id>/`.
2. **Hooks** — that settings file injects three hooks that run the
   `claude-controller hook-signal` subcommand, which POSTs back to the server
   (`POST /api/sessions/{id}/hook-event`):
   - **SessionStart** reports the real CLI session ID and transcript path
     (interactive `--resume` forks to a *new* session ID, so this can't be
     computed up front).
   - **Stop** signals turn completion — the server synthesizes the
     `result`/`done` SSE events and flips `activity_state` to `waiting`.
   - **Notification** relays permission prompts and other notices to the UI.
3. **Streaming** — the server tails the transcript JSONL and forwards raw
   lines to the existing SSE pipeline. Transcript entries have the same shape
   as stream-json events, so frontends are unchanged. Entries older than the
   process spawn are filtered (no history replay on resume).
4. **Prompts** — messages are typed into the PTY using bracketed paste
   (multi-line safe) followed by Enter. `/compact` and other slash commands
   are typed the same way.
5. **Enforcement** — turn limits interrupt with ESC and feed the existing
   auto-continue flow; budget caps compare `SUM(cost)` from transcript usage
   against the session's `max_budget_usd` and pause the session when exceeded.

## How the print backend works

Each session keeps a warm `claude -p --output-format stream-json
--input-format stream-json` process. Messages are written to its stdin as
stream-json user turns; NDJSON events stream from stdout to the SSE pipeline.
Restrictions travel as CLI flags (`--allowedTools`, `--max-turns`,
`--max-budget-usd`), and permission prompts route through an MCP bridge to the
web UI.

## Known gaps in interactive mode

- **Permission prompts**: tools outside the session's allowlist trigger an
  in-terminal prompt that can't be answered remotely. The Notification hook
  surfaces the prompt text in the chat; either add the tool to the session's
  allowed tools or interrupt the turn. (Print mode supports remote
  approve/deny via the MCP bridge.)
- **Images** are referenced by file path in the prompt rather than attached
  inline; Claude reads them with the Read tool.
- **Model changes** mid-session apply on the next process spawn, not the
  current one (the long-lived process pins its `--model` at startup).
- **Windows** always uses print mode until ConPTY support lands.

## Troubleshooting

- **Session stuck in "working"**: the Stop hook may have failed to reach the
  server. Check that `~/.claude-controller/api.key` exists and the server port
  matches what the hook command in
  `~/.claude-controller/sessions/<id>/settings.json` was generated with. A
  server restart resets stale states.
- **No streamed output**: the transcript tailer starts when the SessionStart
  hook reports the transcript path; if hooks never fire, the server falls back
  to the computed path after 30 seconds. Look for `tailing transcript` lines
  in the server log.
- **Need the old behavior back**: `make local MANAGED_MODE=print`.
