#!/usr/bin/env bash
set -euo pipefail

# Claude Controller - Notification Hook (macOS)
# Fire-and-forget: posts notification to server.

# Skip hooks when running inside a managed session (prevents duplicate sessions)
if [[ "${CLAUDE_CONTROLLER_MANAGED:-}" == "1" ]]; then
    exit 0
fi

INPUT=$(cat)

MESSAGE=$(echo "$INPUT" | jq -r '.message // ""')
CWD=$(echo "$INPUT" | jq -r '.cwd // ""')
# Normalize to git repo root so subdirectory commands don't create duplicate sessions
if [[ -n "$CWD" ]] && command -v git &>/dev/null; then
    GIT_ROOT=$(cd "$CWD" && git rev-parse --show-toplevel 2>/dev/null) || true
    if [[ -n "$GIT_ROOT" ]]; then
        CWD="$GIT_ROOT"
    fi
fi
TRANSCRIPT_PATH=$(echo "$INPUT" | jq -r '.transcript_path // ""')

# Resolve config: explicit override > per-instance config > legacy default-instance config.
# A non-default instance must NOT fall back to the legacy config — that would
# silently route its sessions to another instance's server.
INSTANCE="${CLAUDE_CONTROLLER_INSTANCE:-default}"
if [[ -n "${CLAUDE_CONTROLLER_CONFIG:-}" ]]; then
    CONFIG_FILE="$CLAUDE_CONTROLLER_CONFIG"
elif [[ -f "$HOME/.claude-controller/$INSTANCE/hook-config.json" ]]; then
    CONFIG_FILE="$HOME/.claude-controller/$INSTANCE/hook-config.json"
elif [[ "$INSTANCE" == "default" ]]; then
    CONFIG_FILE="$HOME/.claude-controller.json"
else
    exit 0
fi
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
    -d "{\"computer_name\": \"$COMPUTER_NAME\", \"project_path\": \"$CWD\", \"transcript_path\": \"$TRANSCRIPT_PATH\", \"instance\": \"$INSTANCE\"}" 2>/dev/null) || exit 0

SERVER_SESSION_ID=$(echo "$REGISTER_RESP" | jq -r '.id')
MESSAGE_ESCAPED=$(echo "$MESSAGE" | jq -Rs '.')

# Post notification (fire and forget)
curl -sf --max-time 5 \
    -X POST "$SERVER_URL/api/prompts" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"session_id\": \"$SERVER_SESSION_ID\", \"claude_message\": $MESSAGE_ESCAPED, \"type\": \"notification\"}" > /dev/null 2>&1 || true

exit 0
