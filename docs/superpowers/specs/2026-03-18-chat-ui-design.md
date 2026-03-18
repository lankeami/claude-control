# Chat UI + Transcript Integration Design Spec

Replace the prompt-card UI with a messaging-style chat interface that shows the full Claude Code conversation history by reading the session transcript file directly from disk.

## Motivation

The current dashboard shows only prompts (the moments Claude is waiting for input), with no context about the conversation. Users see "Claude is waiting for input" but don't know what Claude said or did. Reading the transcript file gives the full picture — every assistant message and user response — displayed as a familiar chat interface.

## Scope

**In scope:**
- Chat bubble UI (Claude left/gray, user right/blue, chronological bottom-up)
- Transcript endpoint that reads JSONL from disk
- Larger textarea for responses, small send button
- Stop hook sends transcript_path when registering session
- Fix transcript parsing to handle tool-use-only messages

**Out of scope:**
- Rendering tool use details (tool calls shown as summary text or skipped)
- Markdown rendering in chat bubbles (plain text for v1)
- Infinite scroll / pagination (load full transcript for now)

## Backend Changes

### Database

Add `transcript_path` column to sessions table:

```sql
ALTER TABLE sessions ADD COLUMN transcript_path TEXT;
```

Migration is additive — existing sessions get NULL, which is fine.

### Store Changes

- `UpsertSession(computerName, projectPath, transcriptPath)` — accepts optional transcript path, stores it on upsert
- `GetTranscriptPath(sessionID)` — returns the stored path (may be empty)

### New Endpoint: `GET /api/sessions/{id}/transcript`

Requires auth (Bearer token). Reads the JSONL file at the stored `transcript_path`, parses it, and returns a JSON array of messages.

**Response format:**
```json
[
  {
    "role": "assistant",
    "content": "I'll restructure the router to split static files from the API.",
    "timestamp": "2026-03-18T15:02:44Z"
  },
  {
    "role": "user",
    "content": "looks good",
    "timestamp": "2026-03-18T15:03:12Z"
  }
]
```

**Parsing rules:**
- Read all lines from the JSONL file
- For each entry with `.type == "assistant"`: extract text content from `.message.content[]` where content block `.type == "text"`. If no text blocks exist (e.g., all tool_use), skip the entry.
- For each entry with `.type == "user"`: extract text content similarly. Skip tool_result entries (these are internal).
- Entries with `.type == "tool_result"` or other non-message types: skip entirely.
- Return in chronological order (file order, oldest first).
- If transcript_path is empty or file doesn't exist: return empty array `[]`.
- Safety cap: return at most the last 500 messages to prevent pathological cases with very long sessions.

**File:** `server/api/transcript.go`

### Hook Changes

**`hooks/stop.sh`:** Add `transcript_path` to the session register request body:

```bash
TRANSCRIPT_PATH=$(echo "$INPUT" | jq -r '.transcript_path // ""')

# In the register call:
-d "{\"computer_name\": \"$COMPUTER_NAME\", \"project_path\": \"$CWD\", \"transcript_path\": \"$TRANSCRIPT_PATH\"}"
```

**`hooks/notify.sh`:** Same change — pass `transcript_path` when registering.

**PowerShell hooks (`stop.ps1`, `notify.ps1`):** Out of scope for this change. Windows support can be updated in a follow-up.

**`hooks/stop.sh` transcript parsing fix:** The current `tail -20 | jq` approach fails when the last assistant message is all tool_use blocks. Fix: search backward through more lines and filter for entries that actually contain text content.

### Router Changes

Add route to the authenticated API mux in `router.go`:

```go
apiMux.HandleFunc("GET /api/sessions/{id}/transcript", s.handleGetTranscript)
```

## Frontend Changes

### Chat Layout (replaces prompt cards)

The `.prompt-list` area becomes a `.chat-area` with message bubbles:

**Claude messages (assistant):**
- Left-aligned
- Gray background (`var(--bg-secondary)`)
- Rounded corners (more rounded on the right)
- Max-width ~75% of chat area

**User messages:**
- Right-aligned
- Blue background (`var(--accent)`, white text)
- Rounded corners (more rounded on the left)
- Max-width ~75% of chat area

**Ordering:** Chronological, oldest at top, newest at bottom. Chat area auto-scrolls to bottom on load and when new messages arrive.

**Pending state:** If the session has a pending prompt, the last Claude bubble gets a subtle pulsing indicator or "waiting for response..." label.

### Response Textarea

Replaces the small single-line input:

- Multi-line `<textarea>`, ~80px min-height, resizable vertically
- Small circular send button (arrow icon or ">" character) positioned at the bottom-right of the textarea
- Enter sends (Shift+Enter for newline)
- Only visible when the selected session has a pending prompt
- Sticky at the bottom of the chat area

### Instruction Input

Remains as a smaller secondary input below the textarea. Only visible when a session is selected. Labeled "Send instruction (delivered on next stop)".

### Data Flow

1. User selects session → JS fetches `GET /api/sessions/{id}/transcript`
2. Transcript rendered as chat bubbles
3. SSE continues pushing session list + prompt status updates
4. On SSE update: if selected session has new data, re-fetch transcript
5. Responding to a prompt: POST to existing `/api/prompts/{id}/respond`, then re-fetch transcript

### Mobile Layout

Same stacked layout as before. Session dropdown at top, chat area fills the screen, textarea sticky at bottom.

## File Changes

```
server/
  db/
    sessions.go       # MODIFIED: UpsertSession accepts transcript_path, add GetTranscriptPath
    db.go             # MODIFIED: add migration for transcript_path column
  api/
    router.go         # MODIFIED: add transcript route
    transcript.go     # NEW: transcript endpoint handler
    transcript_test.go # NEW: tests
    sessions.go       # MODIFIED: registerRequest includes transcript_path
  web/
    static/
      index.html      # MODIFIED: chat bubble layout
      style.css       # MODIFIED: chat bubble styles
      app.js          # MODIFIED: transcript fetching, chat rendering
hooks/
  stop.sh             # MODIFIED: send transcript_path, fix parsing
  notify.sh           # MODIFIED: send transcript_path
```
