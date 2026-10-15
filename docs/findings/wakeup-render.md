# Findings: main-thread responses not rendering while background work runs

**Issue:** #324 · **Session:** `ba408b58-cadb-4523-a2a6-ac018096fe1f`
(project `-Users-jaychinthrajah-workspaces--personal--shared-mental-load`) ·
**Harness:** Claude Code v2.1.286 · **Date:** 2026-10-15 (03:37–04:50 UTC ≈
23:37–00:50 ET)

## Summary

While an `/autoship` Workflow ran as a background task, the operator's status
requests were answered by turns that ended in a trailing `ScheduleWakeup`
call. The response text in those turns — written before the call, exactly as
the ScheduleWakeup tool-result guidance instructs — never rendered in the
terminal. Turns in the same session that **ended in plain text** rendered
normally, including while the same background task was still running.

**Confirmed rendering rule:** response text renders reliably only when it is
the final block of the turn. Text followed by a tool call in a turn that ends
in `ScheduleWakeup` is dropped from the live terminal while a background task
is running. Therefore: in any status/wakeup loop, call `ScheduleWakeup`
first, then write the per-tick status update **after** the call's result
returns, so the turn ends in text.

## Timeline and cited turns

Background task `wg3g8xv94` (autoship Workflow, run `wf_13e052e2-80f`) was
launched at **03:39:37Z** (transcript line 154/156) and completed at
**04:49:37Z** (line 752 task-notification). Every turn below falls inside
that window — the background task was running for all of them.

Line numbers refer to the session JSONL; the committed excerpt is
`repro/wakeup-render/captures/session-ba408b58-turn-log.txt`.

### Turn 1 — DROPPED (lines 178–198, 03:58:14–03:58:39Z)

- **Trigger:** operator asked "Consider our timeline of events… Recap…"
  (line 174, 03:58:03Z).
- **Shape:** thinking → `text(62)` ("Checking the live status of the autoship
  run before recapping:") → Bash → Bash → thinking → **trailing
  ScheduleWakeup** (line 198).
- **Background task:** running (19 min into `wg3g8xv94`).
- **Text position:** before tool calls, not final block.
- **Outcome:** nothing visible; operator repeated the same request verbatim
  at 03:59:59Z (line 203).

### Turn 2 — DROPPED (lines 205–220, 04:00:02–04:00:21Z)

- **Shape:** thinking → Bash → thinking → **trailing ScheduleWakeup**
  (line 220). **No text block at all** — the "update" existed only in
  thinking blocks, which never render as response text.
- **Background task:** running.
- **Outcome:** silent; operator escalated to "Status????" at 04:01:09Z
  (line 225).

### Turn 3 — DROPPED (lines 227–258, 04:01:13–04:01:37Z)

- **Shape:** Bash → thinking → Bash → Bash → thinking → **trailing
  ScheduleWakeup** (line 258). No text block.
- **Background task:** running.
- **Outcome:** silent; operator escalated to "Why have you not posted any
  response? I keep asking for a status and you've ghosted me" at 04:02:11Z
  (line 263).

### Turn 4 — RENDERED (lines 266–286, 04:02:23–04:02:48Z)

- **Shape:** thinking → Bash → SendFeedback → ScheduleWakeup (line 281,
  mid-turn) → thinking → **`text(1745)` as the final block** (line 286).
- **Background task:** still running (47 more minutes to go).
- **Outcome:** rendered — the operator read it and acted on its suggestion,
  invoking `/loop` at 04:06:00Z (line 300). This is the decisive
  counter-case: same session, same running background task, same
  ScheduleWakeup call in the turn — the only difference is the turn **ends**
  in text.

### Turn 5 — DROPPED (lines 440–477, 04:22:05–04:22:57Z)

- **Shape:** Bash → thinking → `text(143)` (mid-turn) → Bash → Bash →
  thinking → **trailing ScheduleWakeup** (line 477).
- **Background task:** running.
- **Outcome:** not seen live. The operator confirmed after the fact: "I want
  to investigate why you weren't propagating responses to me until work was
  done" (line 792, 10:57:42Z).

### Supporting case — RENDERED (line 165, 03:39:46Z)

Turn ending in `text(1478)` nine seconds after the background task launched —
rendered (the operator's 03:58 message references its content).

## Separating the two candidate causes

1. **Trailing-tool-call turns (text not final block): CONFIRMED.** All four
   dropped turns ended in `ScheduleWakeup`; their text (when present) sat
   before tool calls. Both rendered turns ended in text. Turn 4 ran under
   identical background-task conditions as the dropped turns and rendered —
   the discriminating variable is block order, not the background task.
2. **Background-task suppression alone: NOT SUPPORTED** as a standalone
   cause — turns 4 and the 03:39:46 turn rendered while the task ran. What
   the recorded session cannot exclude is an *interaction* (pre-call text
   might render fine when no background task is running): the session has no
   trailing-ScheduleWakeup turn outside the background window. That control
   is Variant B in `repro/wakeup-render/run.md` and remains an open check.

## Evidence limitation

The JSONL transcript records what the model emitted, not what the terminal
painted. "Rendered / not rendered" above is inferred from the operator's
real-time behavior (verbatim re-asks, escalations, and acting on turn 4's
suggestion) plus their explicit post-hoc confirmations (lines 263, 792, and
at 11:18 "I asked for status checkins after the skill invoked"). A live A/B
per `run.md` is the authoritative check for the remaining control case.

## Guidance conflict (harness angle)

The `ScheduleWakeup` tool result in the affected session instructed the
opposite of the observed-safe ordering:

> "…there is no post-arm slot for a status update, so write the per-tick
> update as ordinary response text immediately BEFORE calling ScheduleWakeup."

Observed behavior contradicts this: the pre-call update is what gets dropped,
and turn 4 proves a post-arm text slot **does** exist (text emitted after the
ScheduleWakeup result, same turn, rendered). This was escalated upstream via
`SendFeedback` from within the affected session itself (turn 4, line 278,
toolu id in transcript) — the draft documents the rendering drop as a harness
bug. Mitigation stands regardless of the upstream fix: **end every loop turn
with the status text** (see `docs/guidance/loop-status-updates.md`).

## The rule as encoded

See `docs/guidance/loop-status-updates.md` (the repo's loop-update ordering
rule) and the automated check `repro/wakeup-render/check-guidance.sh`.
