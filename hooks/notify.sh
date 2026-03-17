#!/usr/bin/env bash
set -euo pipefail

# Claude Controller - Notification Hook (macOS)
# Fire-and-forget: posts notification to server.

INPUT=$(cat)

MESSAGE=$(echo "$INPUT" | jq -r '.message // ""')
CWD=$(echo "$INPUT" | jq -r '.cwd // ""')

# Load config (override with CLAUDE_CONTROLLER_CONFIG env var)
CONFIG_FILE="${CLAUDE_CONTROLLER_CONFIG:-$HOME/.claude-controller.json}"
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
