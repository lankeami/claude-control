# wakeup-render repro (issue #324)

Did main-thread response text render in the terminal while a background task
ran, when the turn ended in a trailing `ScheduleWakeup` call?

## Contents

- `run.md` — the scripted A/B/C repro procedure (interactive; requires a human
  watching the terminal).
- `check-guidance.sh` — automated test: a loop-update ordering rule must exist
  in this repo's guidance, and no guidance may contradict the confirmed
  rendering rule. Run: `./repro/wakeup-render/check-guidance.sh`.
- `captures/session-ba408b58-turn-log.txt` — terminal-session capture
  reconstructed from the recorded transcript of the affected session
  (`ba408b58-cadb-4523-a2a6-ac018096fe1f`, harness v2.1.286), scrubbed of
  tokens/emails. It shows per-turn block ordering, trailing tool calls, and
  background-task state for every turn in the affected window.

## Recorded repro run (session ba408b58, 2026-10-15, harness v2.1.286)

This run was executed live by the operator in the affected session; the
transcript capture above is the committed record. The session itself contains
a natural A/B: background task `wg3g8xv94` (autoship Workflow) ran from
03:39:37Z to 04:49:37Z, during which some turns ended in `ScheduleWakeup` and
some ended in plain text.

| Case | Turn (UTC) | Background task | Text position | Rendered live? |
|------|-----------|-----------------|---------------|----------------|
| A1 | 03:58:15–03:58:39 (lines 178–198) | running | `text(62)` first, then 2 Bash, trailing ScheduleWakeup | **NO** — operator re-asked at 03:59:59 |
| A2 | 04:00:02–04:00:21 (lines 205–220) | running | no text at all, trailing ScheduleWakeup | **NO** — operator escalated "Status????" at 04:01:09 |
| A3 | 04:01:13–04:01:37 (lines 227–258) | running | no text at all, trailing ScheduleWakeup | **NO** — operator escalated "you've ghosted me" at 04:02:11 |
| A4 | 04:22:05–04:22:57 (lines 440–477) | running | `text(143)` mid-turn, trailing ScheduleWakeup | **NO** — operator later: "weren't propagating responses to me until work was done" |
| C1 | 03:39:46 (line 165) | running (launched 9s earlier) | final block of turn | **YES** — operator's 03:58 message references it |
| C2 | 04:02:48 (lines 266–286) | running | ScheduleWakeup mid-turn, **turn ends in text(1745)** | **YES** — operator acted on it (invoked `/loop` at 04:06 as the text suggested) |

**Which variant fails:** text-before-trailing-ScheduleWakeup turns (A1–A4)
drop; turns that end in text (C1–C2) render, even while the same background
task is still running.

**Variant B (control: trailing ScheduleWakeup, no background task):** not
present in the recorded session — the background task was running for the
entire ScheduleWakeup window. Pending a fresh interactive run per `run.md`;
the findings doc notes this evidence limitation. The control that IS covered:
turns ending in trailing text render both with (C1, C2) and without
(pre-03:39 turns, post-04:50 turns) a background task — the regression guard
is green.

**Post-fix verification (run.md Variant C):** the corrected ordering —
ScheduleWakeup first, status text as the final block — matches cases C1/C2,
which rendered on every occurrence while the background task was still
running. A fresh scripted 3-tick Variant C run should be captured here the
next time an operator runs a backgrounded loop (checklist in `run.md`).

Root cause analysis: `docs/findings/wakeup-render.md`.
