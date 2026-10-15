#!/usr/bin/env bash
# Guidance test for issue #324 — loop-update ordering rule.
#
# Confirmed rendering rule (see docs/findings/wakeup-render.md):
#   Response text only renders reliably in the terminal when it is the FINAL
#   block of a turn. Text written before a trailing tool call (e.g. a turn
#   that ends in ScheduleWakeup) is NOT rendered while a background task is
#   running. Every status/loop turn must therefore END with the status text —
#   call ScheduleWakeup first, then write the update after its result returns.
#
# This test fails (red) when:
#   1. No loop-update ordering rule exists in the repo's guidance, or
#   2. Any guidance in docs/ or skills/ contradicts the confirmed rule by
#      telling the model to write updates BEFORE a trailing ScheduleWakeup
#      call (the pre-call text is exactly what gets dropped).
set -uo pipefail
cd "$(dirname "$0")/../.."

fail=0

# --- 1. The ordering rule must exist in repo guidance -----------------------
rule_hits=$(grep -rl "loop-update ordering rule" docs/ .claude/skills/ skills/ 2>/dev/null || true)
if [[ -z "$rule_hits" ]]; then
  echo "FAIL: no loop-update ordering rule found in docs/ or skills/ guidance"
  fail=1
else
  echo "PASS: loop-update ordering rule present in: $rule_hits"
fi

# --- 2. No guidance may contradict the confirmed rule -----------------------
# A contradiction is guidance instructing text-BEFORE-a-trailing-ScheduleWakeup
# ordering (e.g. "write the update before calling ScheduleWakeup") without the
# end-the-turn-in-text correction.
# Markdown blockquote lines (path:line:> ...) are citations of external text,
# not this repo's guidance, and are excluded.
contradictions=$(grep -rn "ScheduleWakeup" docs/ .claude/skills/ skills/ 2>/dev/null \
  | grep -vE '^[^:]+:[0-9]+:\s*>' \
  | grep -iE "before (a |the )?(trailing )?(call|calling|ScheduleWakeup)|immediately BEFORE" \
  | grep -viE "do not|never|dropped|drops|not render|anti-pattern|contradic" || true)
if [[ -n "$contradictions" ]]; then
  echo "FAIL: guidance contradicting the confirmed rendering rule:"
  echo "$contradictions"
  fail=1
else
  echo "PASS: no guidance contradicts the confirmed rendering rule"
fi

exit $fail
