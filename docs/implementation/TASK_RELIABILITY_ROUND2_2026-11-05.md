# Reliability round 2 — explore, plan, build

## Exploration

The previous [round](TASK_RELIABILITY_ROUND_2026-11-05.md) ended with a frozen
4/5 development result. Three separate failure mechanisms remain:

- Long nonrepeating enumeration consumed the log task's time. Correct Python
  output did not become a final answer before the driver's 600-second cutoff.
  A cycle detector cannot address that failure. The app's own deadline is
  20 minutes; the external driver limit is not part of its frozen request.
- Exact short thinking cycles appeared in both failed and eventually
  successful tasks. The frozen offline prototype flagged the three length-stop
  generations in seven successful traces, and none of 101 normally terminated
  generations. This supports an experiment, not default activation.
- Final-v3 coding runs had six command-argument schema rejections. The model
  received raw validator errors; rejection did not include a concrete repair
  instruction. These proposals never executed.

A smaller global output cap is not a safe default fix: successful saved edit
calls required 4,130 and 4,809 output tokens. Successful commands also do not
prove that the entire user request is finished. Do not force completion or
remove useful tool authority on either basis.

The final log compaction summarized just two old events, cost about 26 seconds,
and retained the large recent log result. Baseline compaction, however, followed
a prompt too large to reserve the full output allowance. Skipping all
compaction or merely raising a static threshold is not justified. Add useful
input-size and elapsed-cost observations first; measured headroom needs to
account for newly generated content and subsequent tool results.

Ollama 0.40.11's [Gemma 4 renderer](https://github.com/ollama/ollama/blob/v0.40.11/model/renderers/gemma4.go)
retains thinking only after the last user message. Consequently, the existing
trailing retry message preserves the request's wire prefix but can change the
rendered prompt by suppressing earlier thinking. No KV-cache preservation or
speed improvement is established by the request-prefix tests.

## Frozen plan

1. Enrich confirmed pre-dispatch schema errors with bounded, actionable
   guidance. Preserve the original error, proposed arguments and call/result
   pairing. Use actual types and declared constraints; never coerce a string
   into executable arguments. Runs without activity listeners also benefit.
2. Add a separate false-by-default `thinkingCycleGuard` experiment. Preserve
   the older prose guard's semantics. Match completed nonempty thinking lines
   outside fences: 2–12 lines repeated eight times, at least 2,048 repeated
   bytes, at most 96 retained lines. Keep partial-line storage bounded.
3. A guard interruption discards its entire uncommitted generation and uses an
   existing retry opportunity, model call and output allowance. Retry only
   after clean backend cancellation; cleanup failures, user cancellation and
   exhausted limits remain failures. Do not inspect visible prose, tool
   arguments, summaries or tools-free budget reports for thinking cycles.
4. Improve compaction/context telemetry without changing its policy or budget.
   Label wire estimates honestly and preserve original history and authority.
   Export schema-rejection counts separately from dispatched tool failures in
   pilot records; preserve existing grade and outcome semantics.

No task-specific answers, checker changes, solver-file repairs, default token
changes, automatic completion, commits or pushes. Preserve all earlier dirty
work. Exclude `scripts/pilot/tasks-heldout/**` from searches and reads.

## Validation and measurement

- Pre-round source and binary are retained under
  `.runtime/reliability-round2-20261105/`. Binary SHA-256:
  `f39d16b235b5345cb938bf33fb4a450dcb7c3cf67e45da2f2a73288acf2d812c`.
- Exercise invalid-argument recovery without execution, hostile strings,
  active-schema bounds, no-listener behavior, genuine executor errors and
  model/tool budget exhaustion.
- Exercise guard fragmentation, exact boundaries, fences, long lines,
  nonrepeating enumeration, excluded channels, partial-call discard, existing
  completed calls, clean cancellation, native cleanup failure and mixed retry
  exhaustion. Replay the unchanged saved development corpus.
- Run affected Go race suites, frontend checks if controls change, Python
  pilot tests if reporting changes, and a native build with provenance checks.
  Preserve the four known media/toolchain failures as separate baseline issues.
- Freeze one final candidate. Run `log-question`, `crash-from-log`, and
  `wordfreq-repair` once with the thinking-cycle guard off, then once with it
  on. Both arms use the same binary and schema feedback, Gemma 12B/Ollama,
  32K context, 40 steps, temperature 0.2, thinking Auto, 8,192 output tokens,
  and a 600-second driver deadline. Serial inference only; no extra attempts.
- Keep all outcomes, corrections, interruptions, terminal-receipt gaps,
  schema rejections, host verification and raw file/directory deltas. Compare
  correctness before timing. This is a bounded development experiment, not
  general reliability qualification. The previous v3 results are historical
  context, not a fresh paired baseline for this round.

## Status

Exploration, implementation, focused validation, native packaging and the
six-attempt live comparison are complete; see the
[evidence record](../evidence/task-pilot-round2-2026-11-05.md). The guard remains
off by default. No compaction policy or output-limit change was made.

Control scored 2/3; guard-enabled scored 3/3, with both coding tasks independently
correct and host-verified in both arms. No cycle interruption occurred, so
those outcomes establish no guard benefit or speed improvement. One real
range-error recovery exercised the new schema feedback. All protected inputs
survived; independent review found no export/audit discrepancies. No extra
attempts ran and both pilot processes exited.

Evaluation hygiene: a test-filename inventory used an incorrect exclusion glob
and listed held-out paths. No held-out fixture content was displayed or inspected, and
no held-out task was run. Subsequent searches use `!**/tasks-heldout/**` or
explicit file paths. As already recorded in round 1, strict blind qualification
requires newly authored tasks after the final build is frozen.

## Remaining work

- Nonrepeating enumeration, calculation correctness and timely final answers
  remain unresolved. The control log attempt incorrectly computed no 5xx errors
  and timed out; its failure was not merely delayed delivery of a correct result.
- Deadline-aware finishing first requires the app to receive and freeze a
  narrowed caller deadline. A successful command is not proof of completed
  work, and a final budget report must remain an incomplete outcome.
- A compaction headroom experiment must include newly generated responses and
  tool results, tool/schema changes and uncertainty. New telemetry is ready;
  default policy is unchanged and no live compaction occurred in this pilot.
- The cycle guard needs live trigger-and-recovery evidence and broader
  specificity measurements before any default activation. Replay and mocked
  backend tests establish bounded behavior, not general efficacy.
- Broader repetitions, fresh blind tasks, alternate models, Harness² refinement
  and the media/toolchain compatibility work remain separate backlog items.
