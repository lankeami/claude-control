#!/usr/bin/env bash
set -euo pipefail

# Claude Controller Hook Installer
# Sets up the hooks in Claude Code settings and creates the config file.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Claude Controller Hook Installer ==="
echo ""

# Get Claude settings location (CLAUDE_DIR env var or default)
CLAUDE_DIR_RESOLVED="${CLAUDE_DIR:-$HOME/.claude}"
# Expand tilde
CLAUDE_DIR_RESOLVED="${CLAUDE_DIR_RESOLVED/#\~/$HOME}"
DEFAULT_SETTINGS="$CLAUDE_DIR_RESOLVED/settings.json"
read -p "Claude settings file [$DEFAULT_SETTINGS]: " input_settings
SETTINGS_FILE="${input_settings:-$DEFAULT_SETTINGS}"

# Get instance name (each instance runs its own server with its own port/db/key)
read -p "Instance name [default]: " input_instance
INSTANCE="${input_instance:-default}"

# Get config file location (hooks read this at runtime)
if [[ "$INSTANCE" == "default" ]]; then
    DEFAULT_CONFIG="$HOME/.claude-controller.json"
else
    DEFAULT_CONFIG="$HOME/.claude-controller/$INSTANCE/hook-config.json"
fi
read -p "Controller config file [$DEFAULT_CONFIG]: " input_config
CONFIG_FILE="${input_config:-$DEFAULT_CONFIG}"

# Get computer name
COMPUTER_NAME=$(hostname -s 2>/dev/null || hostname)
read -p "Computer name [$COMPUTER_NAME]: " input_name
COMPUTER_NAME="${input_name:-$COMPUTER_NAME}"

# Get server port and URL
read -p "Server port [8080]: " input_port
PORT="${input_port:-8080}"
read -p "Server URL [http://localhost:$PORT]: " input_url
SERVER_URL="${input_url:-http://localhost:$PORT}"

# Get API key
read -p "API key (from QR code or server output): " API_KEY

# Write config
mkdir -p "$(dirname "$CONFIG_FILE")"
cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "$SERVER_URL",
  "computer_name": "$COMPUTER_NAME",
  "api_key": "$API_KEY"
}
EOF
echo "Config written to $CONFIG_FILE"

# Ensure settings file exists
mkdir -p "$(dirname "$SETTINGS_FILE")"
if [[ ! -f "$SETTINGS_FILE" ]]; then
    echo '{}' > "$SETTINGS_FILE"
fi

# Build hook commands — embed config path if non-default
STOP_HOOK="$SCRIPT_DIR/stop.sh"
NOTIFY_HOOK="$SCRIPT_DIR/notify.sh"

# The hooks resolve per-instance config from CLAUDE_CONTROLLER_INSTANCE, so a
# config at the standard per-instance path only needs the instance name.
ENV_PREFIX=""
if [[ "$INSTANCE" != "default" ]]; then
    ENV_PREFIX="CLAUDE_CONTROLLER_INSTANCE=$INSTANCE "
fi
if [[ "$CONFIG_FILE" != "$DEFAULT_CONFIG" ]]; then
    ENV_PREFIX="${ENV_PREFIX}CLAUDE_CONTROLLER_CONFIG=$CONFIG_FILE "
fi
STOP_CMD="${ENV_PREFIX}${STOP_HOOK}"
NOTIFY_CMD="${ENV_PREFIX}${NOTIFY_HOOK}"

# Add hooks to Claude Code settings using jq
jq --arg stop "$STOP_CMD" --arg notify "$NOTIFY_CMD" '
  .hooks.Stop = [{"hooks": [{"type": "command", "command": $stop}]}] |
  .hooks.Notification = [{"hooks": [{"type": "command", "command": $notify}]}]
' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"

echo "Hooks registered in $SETTINGS_FILE"
echo ""
echo "Done! Restart any running Claude Code sessions for hooks to take effect."
