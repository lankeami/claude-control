#!/bin/bash
# mod-token-panel.sh — Tails a session's SSE stream for result events
# and pushes running token/cost totals to the mod bar.
#
# Usage: ./scripts/mod-token-panel.sh <session-id> [base-url] [api-key]
#
# base-url defaults to http://localhost:9999
# api-key  defaults to reading ~/.claude-controller/api.key

set -euo pipefail

SESSION_ID="${1:?usage: mod-token-panel.sh <session-id> [base-url] [api-key]}"
BASE_URL="${2:-http://localhost:9999}"
API_KEY="${3:-$(cat ~/.claude-controller/api.key 2>/dev/null)}"

if [ -z "$API_KEY" ]; then
  echo "error: no API key — pass as arg or ensure ~/.claude-controller/api.key exists" >&2
  exit 1
fi

TOTAL_IN=0
TOTAL_OUT=0
TOTAL_COST=0

post_panel() {
  local cost_fmt
  cost_fmt=$(printf '$%.2f' "$TOTAL_COST")
  curl -s -X POST "${BASE_URL}/api/sessions/${SESSION_ID}/mod-state" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H "Content-Type: application/json" \
    -d "{\"panel_id\":\"tokens\",\"panel_title\":\"Tokens\",\"slot\":\"bar\",\"data\":{\"input\":${TOTAL_IN},\"output\":${TOTAL_OUT},\"cost\":\"${cost_fmt}\"}}" \
    >/dev/null
}

echo "Watching session ${SESSION_ID} for token usage..."
echo "Posting to ${BASE_URL}/api/sessions/${SESSION_ID}/mod-state"

# Post initial zero state
post_panel

# Tail the SSE stream and accumulate usage from result events
curl -s -N "${BASE_URL}/api/sessions/${SESSION_ID}/stream?token=${API_KEY}" | while IFS= read -r line; do
  # SSE lines are prefixed with "data: "
  data="${line#data: }"
  [ "$data" = "$line" ] && continue  # skip non-data lines

  type=$(echo "$data" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('type',''))" 2>/dev/null || true)

  if [ "$type" = "result" ]; then
    read -r in_tok out_tok cost_val <<< "$(echo "$data" | python3 -c "
import sys, json
d = json.load(sys.stdin)
u = d.get('usage', {})
print(int(u.get('input_tokens', 0)), int(u.get('output_tokens', 0)), float(d.get('cost', 0)))
" 2>/dev/null || echo "0 0 0")"

    [[ $in_tok =~ ^[0-9]+$ ]] && [[ $out_tok =~ ^[0-9]+$ ]] && [[ $cost_val =~ ^[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?$ ]] || continue

    TOTAL_IN=$((TOTAL_IN + in_tok))
    TOTAL_OUT=$((TOTAL_OUT + out_tok))
    TOTAL_COST=$(python3 -c "import sys; print(round(float(sys.argv[1]) + float(sys.argv[2]), 6))" "$TOTAL_COST" "$cost_val")

    echo "Turn complete: +${in_tok}in/+${out_tok}out (+\$${cost_val}) → total: ${TOTAL_IN}in/${TOTAL_OUT}out/\$${TOTAL_COST}"
    post_panel
  fi
done
