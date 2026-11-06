# Offline successful-trace specificity check

The existing proposed thinking-cycle detector was replayed without changes against every completed, independently correct record in the current baseline-r1 and candidate-v2-r1 batches. Its two analysis functions were extracted verbatim from `analyze-repetition.py`; its top-level script was not run. SQLite journals were opened read-only inside snapshot transactions. No inference, held-out inputs, threshold tuning or production activation occurred.

Detector source SHA256: `4ac50455f839733d233fd52df0ed300904c332e234d51aa7d303ed8f9d5c828b`.

Fixed thresholds: thinking channel only; ordinary generations; completed nonempty lines outside fenced code; identical blocks of 2–12 lines repeated eight times; at least 2,048 repeated bytes; retain at most 96 lines.

| Batch | Task | Ordinary generations | With thinking | Fenced-code lines | Triggered generations |
|---|---|---:|---:|---:|---:|
| baseline | explain-failure | 10 | 10 | 224 | 0 |
| baseline | todo-feature | 19 | 19 | 143 | 0 |
| candidate-v2 | crash-from-log | 11 | 11 | 30 | 0 |
| candidate-v2 | explain-failure | 8 | 8 | 130 | 0 |
| candidate-v2 | log-question | 6 | 6 | 40 | 1 |
| candidate-v2 | todo-feature | 30 | 30 | 98 | 1 |
| candidate-v2 | wordfreq-repair | 20 | 20 | 205 | 1 |

Total: **7 successful task traces**, **104 ordinary generations** (104 with thinking), 11,129 thinking events, 162,298 thinking bytes, and 3,112 completed nonempty thinking lines. 2 summary generations were excluded. 19 ordinary generations contained 870 fenced-code lines. **3 generations triggered in 3 traces.**

## Exact trigger events

| Batch / task | Step / attempt | Event sequence | Event timestamp | Pattern | Bytes seen | Terminal outcome / tool calls |
|---|---|---:|---|---|---:|---|
| candidate-v2 / log-question | 5 / 1 | 1427 | 2026-11-05T06:51:36.476847Z | 3 lines × 8; 2376 bytes | 8145 | length / 0 |
| candidate-v2 / todo-feature | 23 / 1 | 4352 | 2026-11-05T07:01:46.515301Z | 5 lines × 8; 2520 bytes | 3179 | length / 0 |
| candidate-v2 / wordfreq-repair | 13 / 1 | 7327 | 2026-11-05T07:08:26.703752Z | 6 lines × 8; 2560 bytes | 7607 | length / 0 |

## Generation-level review

All three triggers correspond to the sample's three output-length stops, each with **zero accepted tool calls and zero visible answer characters**. None of the other **101 normally terminated generations** triggers. These are therefore observed repetition candidates inside eventually successful tasks, not evidence of three false aborts of productive generations.

- **log-question, step 5:** The dominant thinking line appears 79 times; its underlying original log row occurs once. The attempt reached the output cap with no accepted tool call or visible answer. The threshold crossed 93.999 seconds into the generation, 55.928 seconds before its recorded termination.
- **todo-feature, step 23:** A single statement choosing the same test command repeats 398 times; no tool call or visible answer was accepted before the output cap. The threshold crossed 18.349 seconds into the generation, 120.551 seconds before its recorded termination.
- **wordfreq-repair, step 13:** Each of three punctuation-analysis statements occurs 120 times. Earlier fenced code is excluded; the trigger is outside it. No tool call or visible answer was accepted before the output cap. The threshold crossed 51.868 seconds into the generation, 104.140 seconds before its recorded termination.

Each affected task recovered on a later retry and ultimately passed. Earlier interruption remains counterfactual: these timing gaps are **not measured savings**, and replacing the original retry trajectory could still change correctness. The log trigger repeats one specific row; this differs from the earlier baseline log-question trace's long but nonrepeating enumeration, which the same detector did not flag.

## Limits and decision

- Small exposed development sample on one machine, model and runtime; no qualified false-positive rate or generalization estimate.
- Successful task completion is not proof every intermediate generation was useful; any trigger requires generation-level review.
- The detector observes thinking only. Tool arguments, visible prose, incoming tool/test output and summary generations are outside its proposed scope.
- Coalesced durable event chunks recover complete-line content but not exact provider token timing; trigger times are recorded event boundaries.
- No controlled interruptions/retries were run. Offline crossings establish neither runtime savings nor task recovery outcomes.
- No held-out tasks or checkers were inspected, and no threshold tuning or production activation was performed.

Further controlled work is justified, without default activation: all three triggers align with observed repetitive generations that later hit the output cap without accepted tool calls or visible answers, while none of 101 normally terminated generations trigger. This small exposed sample establishes no qualified false-positive rate and does not demonstrate that an earlier interruption would preserve recovery or save runtime.

The JSON companion retains each selected record hash, model/binary identity, event-stream hash, generation metrics, counts and any exact trigger event sequence. The helper is `check-repetition-success-specificity.py`; all artifacts remain under `.runtime/reliability-20261105/`.
