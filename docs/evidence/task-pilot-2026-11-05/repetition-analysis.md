# Offline repetition-guard assessment

Scope: only the completed, deadline-cancelled baseline r1 `log-question` and `wordfreq-repair` attempts from `.runtime/pilot-baseline-20261105-r1-20261105-001232/`. No source changes, mechanism activation, live inference or held-out inputs. The SQLite event journal was opened read-only to recover generation-scoped thinking/text deltas, because the exported transcript omits thinking.

## Existing guard: no trigger on either observed failure

The exact `proseRepetitionGuard` implementation was copied from `internal/engine/mechanisms.go` into the standalone replay program. It was replayed over both channels for all 30 ordinary generations, plus the empty summary-channel records: **zero triggers**. Metrics for completed ordinary generations match the recovered thinking character counts; cancelled generations lack terminal metrics.

The guard requires eight consecutive identical completed lines, each at least 120 bytes, excluding code fences, syntax characters, bullets and short lines. Blank lines do not reset its count; nonempty excluded lines do. The adapter creates independent guards for visible text and thinking per provider attempt, enables them only through frozen `StreamRepetitionGuard`, and excludes compaction summaries. A trigger currently returns a plain `ErrStreamRepetition` provider error: existing empty/length retry handling does **not** recover it automatically. Enabling it alone would terminate a matched attempt.

| Task / generation | Observed thinking | Nonempty lines / distinct lines | Existing eligible lines | Classification |
|---|---:|---:|---:|---|
| log-question step 13 | 7,766 characters | 86 / 81 | 0 | Mostly data inspection before the calculation command; not a sustained exact cycle. |
| log-question step 15, cancelled | 10,893 characters | 78 / 78 | 0 | Manual re-enumeration of log entries after the correct tool answer; long nonrepeating reasoning. |
| wordfreq-repair step 8 | 10,159 characters | 215 / 117 | 0 | Repeated code sketches and reasoning; 93 fenced-code lines. It ultimately produced a useful tool call. |
| wordfreq-repair step 9, length stop | 26,134 characters | 350 / 7 | 0 | Alternating short statements choosing between edit_file and write_file; two dominant lines occurred 173 and 172 times. |
| wordfreq-repair step 10, retry 1, length stop | 27,750 characters | 364 / 35 | 0 | A repeated nine-line punctuation review containing inline-code references; dominant lines occurred 37–38 times. |

The wordfreq failure is therefore a concrete blind spot for the current long-prose guard: short multi-line cycles with inline syntax dominate two generations. The log-question failure is different and would not be caught by an exact repetition detector. Treating all lengthy reasoning as repetition would misclassify that trace.

## Offline candidate detector, not an activated mechanism

A deliberately bounded hypothetical detector was evaluated on the same saved traces: **thinking channel only**, complete nonempty lines, outside fenced code; an identical block of **2–12 lines repeated eight times**, with at least **2,048 bytes** across those repeats; retain at most **96 lines**. It permits inline-code references because those are part of the observed failing loops. It found exactly these two generations and no others in this limited sample:

| Generation | First observed threshold crossing | Observed generation duration | Threshold pattern |
|---|---:|---:|---|
| wordfreq step 9 | 17.608 s, 2,645 bytes seen | 152.207 s | 4 lines × 8; 2,384 repeated bytes |
| wordfreq step 10 | 94.197 s, 6,706 bytes seen | 208.006 s | 9 lines × 8; 5,616 repeated bytes |

The four-line match is two copies of the underlying alternating pair. Those crossing times precede the observed length stops by about 134.6 and 113.8 seconds, respectively. They are **counterfactual interruption opportunities, not measured runtime savings**: interrupted requests and recovery generations would change the trajectory. Event chunks are host-coalesced; timestamps are the recorded event boundary, not original token-arrival times. This prototype has no proven false-positive rate outside these traces.

Wordfreq eventually recovered on the second retry, edited both implementation files, and passed all original tests plus the saved hidden semantic/CLI checks. It still failed the task because the requested regression-test file was missing, no final answer was delivered, and host verification remained unverified when the deadline cancelled it. Early cycle interruption might create time for those remaining requirements, but that outcome has not been demonstrated.

## Concrete next experiment

1. Freeze the proposed thinking-only detector with these exact thresholds and replay it offline against the other three baseline r1 trajectories and a small fixed set of previously successful development traces that contain legitimate code, tables, quotations and repeated test output. Add fixture tests for fenced-code exclusion, fragmented deltas, incomplete final lines, nonrepeating log enumeration and both observed cycle shapes. Reject the detector if it interrupts a legitimate observed action; do not tune against held-out tasks.
2. Only after that specificity check, compare a frozen control with one experimental cycle-guard arm on the same five development tasks, two attempts each. Preserve the 600-second task deadline, 40-call shared budget, model/runtime identity and all tool authority. A guard interruption may consume **at most one of the existing retry opportunities**, discard incomplete tool proposals, and append one brief host nudge; it must not add an unbounded retry or replay an already-completed tool. Keep thinking mode unchanged initially to isolate the detector/recovery effect.
3. Record independent task correctness, required regression-test creation, final-answer delivery, protected inputs, guard triggers, time to next valid action, discarded-generation time, and incomplete token accounting. Stop the experiment on any authority regression, evidence of valid code being interrupted, or repeated guard/retry cycling. Compare correctness before speed.

The nonrepeating log enumeration should be a separate experiment around sufficient-evidence stopping or bounded analysis, not a reason to weaken the repetition detector's specificity.

## Artifacts

Replay programs and deltas remain local under `.runtime/reliability-20261105/`. Aggregate analysis is exported alongside this note.

- `repetition-analysis.json`: generation metrics and aggregate line/cycle statistics, without copied thinking excerpts.
- `existing-guard-replay.json`: exact Go guard replay outcomes.
- `analyze-repetition.py`: read-only journal analysis and hypothetical cycle detector.
- `replay-existing-repetition-guard.go`: copied guard plus standalone JSON replay wrapper.
- `repetition-replay-input.json`: local replay deltas, scoped to these two baseline runs.
