# Activity Status Pills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real-time activity status pills to the managed session chat UI — showing thinking, tool execution, staleness warnings, and connection loss.

**Architecture:** Client-side state machine in Alpine.js derives activity from NDJSON events already flowing via SSE. Server adds a lightweight heartbeat ticker in the Broadcaster. Pills render as compact inline elements in the chat flow, visually distinct from message bubbles.

**Tech Stack:** Go (server heartbeat), Alpine.js (state machine + rendering), CSS (pill styles)

**Spec:** `docs/superpowers/specs/2026-03-21-activity-status-pills-design.md`

---

### Task 1: Add heartbeat ticker to StreamNDJSON

**Files:**
- Modify: `server/managed/stream.go:62-78`
- Test: `server/managed/stream_test.go` (create if not exists)

The current `StreamNDJSON` function reads lines from an `io.Reader` and broadcasts them. We need to add a 15-second ticker that sends heartbeat JSON through the same Broadcaster.

- [ ] **Step 1: Write the failing test**

Create `server/managed/stream_test.go`:

```go
package managed

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStreamNDJSON_SendsHeartbeats(t *testing.T) {
	// Use short interval for testing
	orig := HeartbeatInterval
	HeartbeatInterval = 500 * time.Millisecond
	defer func() { HeartbeatInterval = orig }()

	pr, pw := io.Pipe()
	b := NewBroadcaster()
	defer b.Close()

	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	done := make(chan struct{})
	go func() {
		StreamNDJSON(pr, b, nil)
		close(done)
	}()

	time.Sleep(1 * time.Second)
	pw.Close()
	<-done

	// Check that at least one heartbeat was received and onLine was NOT called for it
	foundHeartbeat := false
	for {
		select {
		case msg := <-ch:
			var obj map[string]interface{}
			if json.Unmarshal([]byte(msg), &obj) == nil {
				if obj["type"] == "heartbeat" {
					foundHeartbeat = true
					if _, ok := obj["ts"]; !ok {
						t.Error("heartbeat missing 'ts' field")
					}
				}
			}
		default:
			goto done_reading
		}
	}
done_reading:
	if !foundHeartbeat {
		t.Error("expected at least one heartbeat message")
	}
}

func TestStreamNDJSON_OnLineNotCalledForHeartbeats(t *testing.T) {
	orig := HeartbeatInterval
	HeartbeatInterval = 200 * time.Millisecond
	defer func() { HeartbeatInterval = orig }()

	pr, pw := io.Pipe()
	b := NewBroadcaster()
	defer b.Close()

	var onLineCalls []string
	onLine := func(line string) {
		onLineCalls = append(onLineCalls, line)
	}

	done := make(chan struct{})
	go func() {
		StreamNDJSON(pr, b, onLine)
		close(done)
	}()

	// Write a real line, wait for heartbeats, then close
	pw.Write([]byte(`{"type":"assistant"}` + "\n"))
	time.Sleep(500 * time.Millisecond)
	pw.Close()
	<-done

	// onLine should have been called exactly once (for the assistant line, not heartbeats)
	if len(onLineCalls) != 1 {
		t.Errorf("expected 1 onLine call, got %d", len(onLineCalls))
	}
}

func TestStreamNDJSON_BroadcastsLines(t *testing.T) {
	input := `{"type":"assistant","message":"hello"}` + "\n"
	r := strings.NewReader(input)
	b := NewBroadcaster()
	defer b.Close()

	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	StreamNDJSON(r, b, nil)

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "assistant") {
			t.Errorf("expected assistant message, got: %s", msg)
		}
	default:
		t.Error("expected a broadcast message")
	}
}
```

**Note:** The existing `StreamNDJSON` returns `[]string`. The new implementation changes the signature to return nothing. You must also update the existing tests in `stream_test.go` that capture the return value — change `lines := StreamNDJSON(...)` to just `StreamNDJSON(...)`. The caller in `managed_sessions.go` does not use the return value, so no change needed there.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./managed/ -v -run TestStreamNDJSON`
Expected: FAIL — heartbeat test fails because StreamNDJSON doesn't send heartbeats yet.

- [ ] **Step 3: Refactor StreamNDJSON to support heartbeats**

Modify `server/managed/stream.go`. The current `StreamNDJSON` is a simple loop reading lines. We need to restructure it to use a goroutine for reading + a ticker for heartbeats.

Replace the `StreamNDJSON` function (lines 62-78) with:

```go
// HeartbeatInterval is the interval between heartbeat messages.
// Exported for testing.
var HeartbeatInterval = 15 * time.Second

// StreamNDJSON reads newline-delimited JSON from r, broadcasts each line via b,
// and sends periodic heartbeat messages. Calls onLine for each non-heartbeat line
// if provided.
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string)) {
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			lines <- line
		}
	}()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return
			}
			b.Send(line)
			if onLine != nil {
				onLine(line)
			}
		case <-ticker.C:
			hb := fmt.Sprintf(`{"type":"heartbeat","ts":%d}`, time.Now().UnixMilli())
			b.Send(hb)
		}
	}
}
```

Add imports at top of file: `"fmt"` and `"time"` (alongside existing `"bufio"`, `"io"`, `"log"`).

- [ ] **Step 4: Update existing tests for new signature**

If the existing `stream_test.go` has tests that capture the return value of `StreamNDJSON` (e.g., `lines := StreamNDJSON(...)`), update them to not capture the return value since the function no longer returns `[]string`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./managed/ -v -run TestStreamNDJSON`
Expected: PASS — both tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/managed/stream.go server/managed/stream_test.go
git commit -m "feat: add heartbeat ticker to StreamNDJSON broadcaster"
```

---

### Task 2: Filter heartbeats from message persistence

**Files:**
- Modify: `server/api/managed_sessions.go:138-157` (onLine callback)

The `onLine` callback in `handleSendMessage` persists every line to the database. Heartbeat messages should NOT be persisted.

- [ ] **Step 1: Add heartbeat filter to onLine callback**

In `server/api/managed_sessions.go`, find the `onLine` callback inside `handleSendMessage`. It currently persists every line. Add a check at the top:

```go
onLine := func(line string) {
    // Don't persist heartbeat messages
    if parseRole(line) == "heartbeat" {
        return
    }
    // ... rest of existing onLine logic
}
```

This uses the existing `parseRole` function which does proper JSON unmarshalling, consistent with the codebase pattern. No new imports needed.

- [ ] **Step 2: Run existing tests**

Run: `cd server && go test ./... -v`
Expected: All existing tests pass. Heartbeats don't break anything.

- [ ] **Step 3: Commit**

```bash
git add server/api/managed_sessions.go
git commit -m "fix: filter heartbeat messages from database persistence"
```

---

### Task 3: Add pill CSS styles

**Files:**
- Modify: `server/web/static/style.css`

- [ ] **Step 1: Add activity pill styles**

Append the following to the end of `server/web/static/style.css` (before any closing comments):

```css
/* Activity Status Pills */
.activity-pills {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    margin: 4px 0;
}

.activity-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 12px;
    font-family: var(--font-mono, monospace);
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.activity-pill.active {
    background: #1a2a1a;
    border: 1px solid #3a4a3a;
    color: #7cb87c;
}

.activity-pill.completed {
    background: #2a2a3a;
    border: 1px solid #3a3a4a;
    color: #6a7a8a;
}

.activity-pill.stale {
    background: #2a1a0a;
    border: 1px solid #5a4a2a;
    color: #c8a040;
}

.activity-pill.disconnected {
    background: #2a0a0a;
    border: 1px solid #5a2a2a;
    color: #c05050;
}

.pill-icon {
    flex-shrink: 0;
}

.pill-icon.pulse {
    animation: pill-pulse 1.5s ease-in-out infinite;
}

@keyframes pill-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
}

.pill-duration {
    margin-left: 4px;
    opacity: 0.6;
    font-size: 10px;
}
```

- [ ] **Step 2: Verify styles load**

Run the server: `cd server && go run .`
Open the web UI. Open browser dev tools and confirm the `.activity-pill` class exists in the stylesheet (Elements → Styles panel, search for "activity-pill").

- [ ] **Step 3: Commit**

```bash
git add server/web/static/style.css
git commit -m "feat: add CSS styles for activity status pills"
```

---

### Task 4: Add pill container to HTML

**Files:**
- Modify: `server/web/static/index.html:106-129`

- [ ] **Step 1: Add activity pills template inside chat area**

In `server/web/static/index.html`, find the chat area `x-for` loop (around line 123). After the existing `</template>` that closes the `x-for="(msg, idx) in chatMessages"` loop, add the pills container:

```html
            <!-- Activity Status Pills -->
            <template x-if="activityPills.length > 0">
                <div class="activity-pills">
                    <template x-for="(pill, pidx) in activityPills" :key="pidx">
                        <span class="activity-pill" :class="pill.state">
                            <span class="pill-icon" :class="{ pulse: pill.state === 'active' }"
                                  x-text="pill.state === 'active' ? '●' : pill.state === 'completed' ? '✓' : pill.state === 'stale' ? '⚠' : '✕'"></span>
                            <span x-text="pill.label"></span>
                            <span class="pill-duration" x-show="pill.duration" x-text="pill.duration"></span>
                        </span>
                    </template>
                </div>
            </template>
```

- [ ] **Step 2: Verify HTML renders**

Run server and open web UI. The pills container should be hidden (no pills yet). Verify no rendering errors in browser console.

- [ ] **Step 3: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat: add activity pills container to chat area HTML"
```

---

### Task 5: Implement the activity state machine in app.js

**Files:**
- Modify: `server/web/static/app.js`

This is the main task — adding the state machine, pill management, staleness timer, and heartbeat tracking to the Alpine.js app.

- [ ] **Step 1: Add state properties to Alpine data model**

In `server/web/static/app.js`, find the Alpine.js `data()` return object (around line 1). Add these properties:

```javascript
// Activity Status Pills
activityPills: [],
stalenessTimer: null,
heartbeatTimer: null,
lastEventTime: null,
currentPillStart: null,
```

- [ ] **Step 2: Add the pill helper methods**

Add these methods to the Alpine.js component (find a suitable location near the existing `sessionStatus` or `bubbleClass` methods):

```javascript
// --- Activity Status Pills ---

addActivityPill(label, state) {
    // Complete the current active pill
    const now = Date.now();
    const activePill = this.activityPills.find(p => p.state === 'active');
    if (activePill) {
        const elapsed = Math.round((now - (this.currentPillStart || now)) / 1000);
        activePill.state = 'completed';
        activePill.duration = elapsed + 's';
    }

    // Add the new pill (originalLabel stored for staleness revert)
    this.activityPills.push({ label, originalLabel: label, state, duration: null });
    this.currentPillStart = now;

    // Enforce stacking limit: max 10 completed + 1 active
    const completed = this.activityPills.filter(p => p.state === 'completed');
    if (completed.length > 10) {
        const idx = this.activityPills.indexOf(completed[0]);
        this.activityPills.splice(idx, 1);
    }

    this.scrollToBottom();
},

clearActivityPills() {
    this.activityPills = [];
    this.currentPillStart = null;
    this.clearStalenessTimer();
    // NOTE: Do NOT clear the heartbeat timer here — it's managed by SSE lifecycle
},

extractToolContext(block) {
    const name = block.name || 'Tool';
    const input = block.input || {};
    let context = '';

    if (input.file_path) {
        // Show just the filename, not full path
        const parts = input.file_path.split('/');
        context = parts[parts.length - 1];
    } else if (input.command) {
        context = input.command.substring(0, 30);
    } else if (input.pattern) {
        context = input.pattern.substring(0, 30);
    }

    const full = context ? `${name} ${context}` : name;
    return full.length > 40 ? full.substring(0, 37) + '...' : full;
},

// --- Staleness & Heartbeat Timers ---

resetStalenessTimer() {
    this.clearStalenessTimer();
    this.lastEventTime = Date.now();

    // If there was a stale pill, revert it back to active
    const stalePill = this.activityPills.find(p => p.state === 'stale');
    if (stalePill) {
        stalePill.state = 'active';
        stalePill.label = stalePill.originalLabel;
    }

    this.stalenessTimer = setTimeout(() => {
        const activePill = this.activityPills.find(p => p.state === 'active');
        if (activePill) {
            const elapsed = Math.round((Date.now() - this.lastEventTime) / 1000);
            activePill.originalLabel = activePill.label; // preserve for revert
            activePill.state = 'stale';
            activePill.label = `${activePill.label} — ${elapsed}s, may be stalled`;
        }
    }, 60000);
},

clearStalenessTimer() {
    if (this.stalenessTimer) {
        clearTimeout(this.stalenessTimer);
        this.stalenessTimer = null;
    }
},

resetHeartbeatTimer() {
    this.clearHeartbeatTimer();
    this.heartbeatTimer = setTimeout(() => {
        // No heartbeat for 30s — connection likely lost
        this.addActivityPill('Connection lost — server may be down', 'disconnected');
        this.clearStalenessTimer();
    }, 30000);
},

clearHeartbeatTimer() {
    if (this.heartbeatTimer) {
        clearTimeout(this.heartbeatTimer);
        this.heartbeatTimer = null;
    }
},
```

- [ ] **Step 3: Modify startSessionSSE to process activity events**

In the `startSessionSSE` method (around line 558), replace the entire `onmessage` handler with a merged version that includes activity pill logic. The complete handler should be:

```javascript
this.sessionSSE.onmessage = (event) => {
    try {
        const data = JSON.parse(event.data);

        // --- Activity pill processing ---

        // Heartbeat: reset heartbeat timer only, don't render anything
        if (data.type === 'heartbeat') {
            this.resetHeartbeatTimer();
            return;
        }

        // Any non-heartbeat event resets both timers
        this.resetStalenessTimer();
        this.resetHeartbeatTimer();

        // Done/result: clear pills and stop SSE
        if (data.type === 'done' || data.type === 'result') {
            this.clearActivityPills();
        }
        if (data.type === 'done') {
            this.stopSessionSSE();
            // Refresh session to get updated status
            this.fetchSessionMessages(sessionId);
            return;
        }

        // Tool use (nested inside assistant event): add tool pill
        if (data.type === 'assistant' && data.message && data.message.content) {
            for (const block of data.message.content) {
                if (block.type === 'tool_use') {
                    const label = this.extractToolContext(block);
                    this.addActivityPill(label, 'active');
                }
            }
        }

        // Tool result: complete previous pill, show thinking
        if (data.type === 'tool_result') {
            this.addActivityPill('Thinking...', 'active');
        }

        // --- Existing message rendering (keep as-is) ---

        // Only render assistant text and errors as chat messages
        if (data.type === 'assistant' && data.message && data.message.content) {
            for (const block of data.message.content) {
                if (block.type === 'text' && block.text) {
                    this.chatMessages.push({
                        role: 'assistant',
                        msg_type: 'text',
                        content: block.text,
                        created_at: new Date().toISOString()
                    });
                }
            }
        }

        // ... keep any other existing rendering logic (errors, etc.)

        this.scrollToBottom();
    } catch (e) {
        // Ignore parse errors for non-JSON lines
    }
};
```

**Note:** Adapt the existing rendering logic into this merged handler. The key ordering is: heartbeat check → timer resets → done/result → pill updates → message rendering. The `done` handler calls `return` to prevent further processing. The `result` handler clears pills but does NOT return (it may contain final message content).

- [ ] **Step 4: Add initial "Thinking..." pill when message is sent, heartbeat timer on SSE connect**

**On message send:** Find the `sendMessage` / `sendManagedMessage` method. After the API call that triggers the process, add:

```javascript
// Show initial "Thinking..." pill
this.clearActivityPills();
this.addActivityPill('Thinking...', 'active');
this.resetStalenessTimer();
```

Also find the `sendInstruction` method if separate, and add the same.

**On SSE connect:** In `startSessionSSE`, right after creating the EventSource, start the heartbeat timer:

```javascript
this.resetHeartbeatTimer();
```

This ensures connection monitoring starts when SSE opens, not just when a message is sent. The heartbeat timer runs for the entire SSE connection lifetime.

- [ ] **Step 5: Clear pills and heartbeat timer on SSE disconnect and session change**

In `stopSessionSSE()` (around line 638), add cleanup for both pills and heartbeat timer:

```javascript
this.clearActivityPills();
this.clearHeartbeatTimer();
```

In `selectSession()` (around line 340), add `this.clearActivityPills();` after the existing `this.stopSessionSSE();` call (around line 343). This ensures pills are cleared when switching between sessions.

- [ ] **Step 6: Manual test — end to end**

Run: `cd server && go run .`
1. Open web UI, create a managed session
2. Send a message and watch the chat area
3. Verify: "Thinking..." pill appears immediately
4. Verify: tool pills appear as Claude runs tools (e.g., "Read handler.go")
5. Verify: completed pills show checkmarks and durations
6. Verify: pills clear when response finishes
7. Verify: max 10 completed pills (send a message that triggers many tools)

- [ ] **Step 7: Manual test — staleness and heartbeat**

1. Send a long-running message
2. Wait 60s+ without events → verify active pill turns amber with "may be stalled"
3. Stop the server while a session is running → verify red "Connection lost" pill appears within ~30s

- [ ] **Step 8: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: implement activity status pills state machine and rendering"
```

---

### Task 6: Final integration test

**Files:**
- All modified files

- [ ] **Step 1: Run Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests pass including new heartbeat tests.

- [ ] **Step 2: Full manual walkthrough**

1. Start server: `cd server && go run .`
2. Open web UI
3. Create managed session
4. Send message: "List all Go files in the server directory"
5. Observe:
   - "Thinking..." pill (green, pulsing)
   - Tool pills appear (e.g., "Bash ls server/")
   - Completed pills show checkmarks and durations
   - Pills clear when Claude responds
6. Send another message to verify cycle repeats
7. Switch sessions — pills clear

- [ ] **Step 3: Commit any final fixes**

```bash
git add -A
git commit -m "feat: activity status pills — final integration fixes"
```
