# Loop-update ordering rule (status loops, wakeups, backgrounded work)

This is the repo's **loop-update ordering rule**, confirmed by the evidence
in `docs/findings/wakeup-render.md` (issue #324) and guarded by
`repro/wakeup-render/check-guidance.sh`.

## The rule

**Every status/loop turn must END with the status text.** When a turn
schedules its next tick with `ScheduleWakeup` (or ends in any trailing tool
call), response text written earlier in the turn is NOT rendered in the
terminal while a background task is running — the session looks stalled even
though updates were "written" every turn.

Correct ordering for a per-tick status turn:

1. Do the tick's checks (Bash, reads, etc.).
2. Call `ScheduleWakeup` to arm the next tick.
3. **After** the ScheduleWakeup result returns, write the status update as
   ordinary response text, as the final block of the turn. No tool call may
   follow the text.

Anti-pattern (this drops the update): never write the update immediately
before a trailing ScheduleWakeup call — never let the turn end in the tool call.
Some harness tool-result strings have instructed this pre-call ordering; the
observed rendering behavior contradicts it (see findings doc) — do not follow
such a tool-result instruction; this rule wins. Also never leave the update
only in thinking blocks — thinking never renders as a response.

## Scope

Applies to any backgrounded work with a status loop in this repo's workflows:
`/loop` prompts, autoship-style background Workflows with check-ins, and any
skill that tells the model to self-schedule wakeups. If you author or edit a
loop prompt template, the template must say "end the turn with the status
text (after the ScheduleWakeup call)", never "write the update before the
call".

## Regression guard

- A status turn with no background task that ends in trailing text must
  always render (control case — stays green across any change here).
- `repro/wakeup-render/check-guidance.sh` fails if this rule disappears or if
  any guidance in `docs/` or `skills/` reintroduces the pre-call ordering.
