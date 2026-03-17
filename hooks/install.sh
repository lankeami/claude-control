#!/usr/bin/env bash
set -euo pipefail

# Claude Controller Hook Installer
# Sets up the hooks in Claude Code settings and creates the config file.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="$HOME/.claude-controller.json"
SETTINGS_FILE="$HOME/.claude/settings.json"

echo "=== Claude Controller Hook Installer ==="
echo ""

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

# Add hooks to Claude Code settings using jq
STOP_HOOK="$SCRIPT_DIR/stop.sh"
NOTIFY_HOOK="$SCRIPT_DIR/notify.sh"

jq --arg stop "$STOP_HOOK" --arg notify "$NOTIFY_HOOK" '
  .hooks.Stop = [{"hooks": [{"type": "command", "command": $stop}]}] |
  .hooks.Notification = [{"hooks": [{"type": "command", "command": $notify}]}]
' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"

echo "Hooks registered in $SETTINGS_FILE"
echo ""
echo "Done! Restart any running Claude Code sessions for hooks to take effect."
