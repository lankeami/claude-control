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
# Normalize to git repo root so subdirectory commands don't create duplicate sessions
if [[ -n "$CWD" ]] && command -v git &>/dev/null; then
    GIT_ROOT=$(cd "$CWD" && git rev-parse --show-toplevel 2>/dev/null) || true
    if [[ -n "$GIT_ROOT" ]]; then
        CWD="$GIT_ROOT"
    fi
fi
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
TRANSCRIPT_PATH=$(echo "$INPUT" | jq -r '.transcript_path // ""')

# Load config (override with CLAUDE_CONTROLLER_CONFIG env var)
CONFIG_FILE="${CLAUDE_CONTROLLER_CONFIG:-$HOME/.claude-controller.json}"
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
    -d "{\"computer_name\": \"$COMPUTER_NAME\", \"project_path\": \"$CWD\", \"transcript_path\": \"$TRANSCRIPT_PATH\"}" 2>/dev/null) || exit 0

SERVER_SESSION_ID=$(echo "$REGISTER_RESP" | jq -r '.id')

if [[ "$STOP_HOOK_ACTIVE" == "true" ]]; then
    # Claude is already continuing from a previous stop hook.
    # Check for queued instructions first.
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

# Normal stop: extract Claude's last text message from transcript
CLAUDE_MSG=""
if [[ -n "$TRANSCRIPT_PATH" && -f "$TRANSCRIPT_PATH" ]]; then
    CLAUDE_MSG=$(tail -100 "$TRANSCRIPT_PATH" | jq -rs '
        [.[] | select(.type == "assistant") |
         .message.content |
         if type == "array" then [.[] | select(.type == "text") | .text] | join("\n")
         elif type == "string" then .
         else ""
         end |
         select(. != "")
        ] | last // ""
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
