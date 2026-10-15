# Repro: main-thread text dropped when a turn ends in ScheduleWakeup (issue #324)

A scripted manual verification of whether main-thread response text renders in
the terminal while a background task is running, as a function of where the
text sits relative to a trailing `ScheduleWakeup` call.

Run this in an **interactive** Claude Code session (the drop is a terminal
rendering behavior — it cannot be observed from `claude -p` or from a
subagent, only by a human watching the terminal). Record each variant's
result in `README.md` and attach a terminal capture (e.g. `script -q
captures/<variant>.txt claude` or a screen recording) under `captures/`.

## Setup

1. `cd` into any repo and start `claude` interactively.
2. Note harness version: `claude --version` (record it in README.md — the
   affected session ran v2.1.286).

## Variant A — background task + text BEFORE trailing ScheduleWakeup (expected: DROPPED)

1. Start a long-running background task:
   > Run `sleep 600` in the background with the Bash tool (run_in_background: true).
2. Ask for a self-scheduling status loop with text-first ordering:
   > Every turn from now on: print "TICK n" (incrementing n) as ordinary
   > response text, and THEN call ScheduleWakeup for 60 seconds later, as the
   > last tool call of the turn. Do this for 3 ticks.
3. **Assert:** "TICK n" is visible in the terminal before the next wakeup
   fires, for every n. Watch the terminal between wakeups — do not scroll
   back after the loop ends.

## Variant B — control: NO background task, text before trailing ScheduleWakeup

1. Fresh session (or kill the background task first).
2. Repeat step A2 exactly.
3. **Assert:** same as A3. This isolates background-task execution as a
   cause: if A drops and B renders, the drop requires a running background
   task; if both drop, trailing-tool-call turns alone suppress rendering.

## Variant C — corrected ordering: background task + turn ENDS in text

1. Start the background task as in A1.
2. Ask for the corrected ordering:
   > Every turn from now on: first call ScheduleWakeup for 60 seconds later,
   > and after its result returns, print "TICK n" as ordinary response text
   > so the turn ENDS with the text. No tool call may follow the text. Do
   > this for 3 ticks.
3. **Assert:** every "TICK n" is visible in the terminal while the background
   task is still running.

## Control regression guard

A plain status turn (no background task) that ends in trailing text must
always render — rerun Variant B's assertion after any guidance change.

## Recording results

| Variant | Background task | Text position | Rendered live? |
|---------|-----------------|---------------|----------------|
| A       | running         | before trailing ScheduleWakeup | record |
| B       | none            | before trailing ScheduleWakeup | record |
| C       | running         | final block of turn            | record |

Commit captures under `captures/` and summarize in `README.md`.
