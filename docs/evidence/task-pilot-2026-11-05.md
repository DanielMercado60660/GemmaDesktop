# Task reliability follow-up — 5 November 2026

This is a paired development pilot, not qualification. The implementation scope
and open backlog are recorded in the
[work record](../implementation/TASK_RELIABILITY_ROUND_2026-11-05.md).

## Builds and controls

The baseline freezes the working tree before this round, including the earlier
uncommitted work. The candidate adds narrowly recognized workspace read-only
intent, trailing retry feedback, clearer edit diagnostics/results, conservative
edit replay, and existing-directory no-ops. Original compacted-user-message
preservation already existed and was tested rather than replaced.

| Identity | Value |
| --- | --- |
| Baseline binary SHA-256 | `a5f89134c422a9083849c0ad4d1bfc8e7d26e91ad24c85bd1deaa5cbe769a21b` |
| Candidate binary SHA-256 | `f39d16b235b5345cb938bf33fb4a450dcb7c3cf67e45da2f2a73288acf2d812c` |
| Candidate source fingerprint | `01e7040d062ac1828c1c5b597b4c1c11da83987584f0f8c22b4488d5c22883f8` |
| Model | `gemma4:12b-mlx` |
| Model digest | `ded7a27350032202d9e9b2a6071e8aa89959ab156771b5228f30863c741c4970` |
| Runtime | Ollama `0.40.11` |
| Settings | 32K context, 40 steps, temperature 0.2, thinking Auto, 8,192 output tokens |
| Driver deadline | 600 seconds per task, with bounded cancellation wait |

Each arm starts an isolated app instance and uses disposable project fixtures.
Gemma alone changes candidate files. Checkers, protected-input checks, and raw
workspace audits run independently. All failures and timeouts remain in the
denominator. Inference batches run serially; some baseline work overlapped
compilation/testing, so elapsed times are observational, not a speed benchmark.
Each evaluated candidate is frozen separately. An exploratory v2 batch began
before source review found a no-op verification bookkeeping bug: existing
directories and already-applied edits could incorrectly trigger fresh checks
or repairs. Final v3 narrowly recognizes successful built-in no-op receipts,
preserving prior change/verification state. V2 outcomes are retained separately
and are never pooled into the final-v3 result. Its binary hash was
`b9c8c415c6a59a7202b40b6dd64fc67dd5dabb5776ebe5a2d7adfb6a2586fc69`.
An earlier unevaluated build also remains in the local records.

## Source and native validation

- Python: 39 repository tests and 7 Harness adapter tests passed.
- Frontend: 76 unit tests and 73 UI tests passed; production build passed.
- Full Go race run: all packages except `internal/media` passed. Four media
  tests fail because installed FFmpeg/ffprobe is **9.0.2**, while the subsystem
  deliberately requires **8.1**. Those same four failures reproduce on the
  frozen baseline; the owning files are byte-identical. No version guard or
  global installation was changed.
- The final app/engine/tools/workspace integration suites passed with race
  detection after the no-op correction. Actual built-in-tool tests prove that
  no-op-only tasks do not trigger verification/repair, and edit replay retains
  a passing verification receipt.
- A real macOS sandbox test passed: an approved Python calculation could read
  source and use private temporary storage, but could neither overwrite source
  nor create a helper file, even with model arguments requesting `readOnly:false`.
- `scripts/build.sh` rebuilt `dist/Gemma Desktop.app`, verified its ad-hoc
  signature, and recorded `inputsVerifiedUnchanged: true`. The previous app
  bundle remains at `.runtime/reliability-20261105/previous-app.app`.

Tests cover narrow/quoted/conditional no-write wording, explicit continuation
and changed user intent, configured read-only agents, frozen workflow executor
selection, exact request-prefix and tool-pair preservation on retries, stale
edit revisions, conversation isolation, and protected-directory races.

## Live results

The baseline finished **2/5 independently correct**, exploratory v2 **5/5**,
and the separately frozen final v3 **4/5**. Each coding task passed both
independent checks and host verification on final v3. All timeouts and the
empty-response failure remain in the denominator. The initial plan allowed
three repetitions on the focused subset; after the baseline took about 35
minutes, additional repetitions were deferred for this round. There is one
attempt per task per revision, with no pooled candidate score.

| Task | Baseline outcome | Host verification | Seconds |
| --- | --- | --- | ---: |
| log-question | Incorrect; cancelled at deadline without final answer | Not applicable | 602.0 |
| explain-failure | Correct | Not applicable | 193.2 |
| todo-feature | Correct | Passed | 533.2 |
| crash-from-log | Incorrect; empty-response retries exhausted | Unverified | 157.1 |
| wordfreq-repair | Incorrect; cancelled before required regression tests | Unverified | 601.8 |

[Baseline results](task-pilot-2026-11-05/baseline-20261105-r1.json) and
[raw audit](task-pilot-2026-11-05/baseline-audit.json) retain the individual outcomes.
All protected inputs were preserved. The explain-failure task created five
`.pytest_cache` files; its grade checks preserved source, not zero filesystem
writes. The strict log-question task created no files but never delivered the
correct counts, even though a script had computed them.

| Task | Exploratory v2 outcome | Host verification | Seconds |
| --- | --- | --- | ---: |
| log-question | Correct | Not applicable | 378.5 |
| explain-failure | Correct | Not applicable | 140.0 |
| todo-feature | Correct | Passed | 523.1 |
| crash-from-log | Correct | Passed | 94.6 |
| wordfreq-repair | Correct after length-retry recovery | Passed | 464.6 |

[Exploratory v2 results](task-pilot-2026-11-05/candidate-20261105-r1.json) and
[raw audit](task-pilot-2026-11-05/exploratory-v2-audit.json) are separate from
final-v3 evidence. All protected inputs were preserved. The strict log task
left the raw workspace unchanged; explain-failure again added five pytest
cache files without changing existing files. Each coding task changed its
implementation and added a regression-test file. A second reviewer checked
all ten baseline/v2 outcomes, build identities and raw filesystem deltas.

| Task | Final v3 outcome | Host verification | Seconds |
| --- | --- | --- | ---: |
| log-question | Incorrect; cancelled at deadline without final answer | Not applicable | 601.3 |
| explain-failure | Correct within the existing diagnostic rubric | Not applicable | 225.3 |
| todo-feature | Correct | Passed | 555.9 |
| crash-from-log | Correct | Passed | 408.7 |
| wordfreq-repair | Correct | Passed | 562.7 |

[Final v3 results](task-pilot-2026-11-05/candidate-final-20261105-r1.json) and
[raw audit](task-pilot-2026-11-05/final-v3-audit.json) retain all five outcomes.
All protected inputs were preserved. The final batch exited cleanly and no
further repetitions were launched. The tested frozen binary matches the
rebuilt native app byte-for-byte.
An independent reviewer checked all fifteen exported outcomes against their
original records, including build identities, settings, grades, host receipts
and raw file/directory deltas.

The final-v3 log attempt preserved file contents and directory names. It
exhausted one generation on long, nonrepeating manual enumeration, later
computed the correct counts with Python, then underwent compaction and reached
the deadline before answering. A cycle detector alone would not address this
failure. Its cancelled generation has incomplete usage accounting.
The [saved diagnostic](task-pilot-2026-11-05/final-log-question-diagnostic.json)
retains the generation and retry evidence.

Final-v3 explain-failure identified the correct function and mechanism but
cited line 33 instead of 35, within the unchanged checker's two-line tolerance.
It preserved existing files while adding the same five pytest cache files and
three cache directories. All three coding tasks changed their intended
implementation files, added regression tests that fail on the original code,
and passed independent semantic checks plus host verification. Final v3 had
six pre-dispatch schema rejections: five in crash-from-log and one in
wordfreq-repair, all for command arguments encoded as strings instead of
arrays. The model recovered, but command-format friction remains.

## Evidence limitations and next steps

- One attempt per task and revision is a small development sample. Outcomes
  vary between revisions, and there are no per-feature ablations; the scores
  do not establish a general reliability improvement or isolate a cause.
- A completed run or a host verification badge is not the independent grade.
- The pilot's `toolCalls.errors` omits pre-dispatch schema rejections. The raw
  event audit reports those separately.
- The driver's normal file comparison excludes caches such as `.pytest_cache`.
  Raw workspace audits include caches and directory-name changes, but do not
  compare permission or timestamp metadata. Preserved source does not imply
  zero writes.
- Cancelled generations may have no terminal usage receipt. Recorded token
  totals cannot be treated as complete cost accounting for those runs.
- Prefix-preserving retry construction has regression coverage. Runtime cache
  reuse and latency improvement have not been established.
- No held-out tasks were run. A broad toolchain search accidentally displayed
  fixture fragments from three held-out cases; see the work record before
  treating that pool as strictly blind developer evaluation material.

Early baseline evidence strengthens two remaining backlog items: preventing
long final generations after a correct computed result, and improving recovery
from `read_file` range-schema mistakes. Neither is silently tuned into the
frozen candidate during this comparison.


An [offline replay of the existing repetition guard](task-pilot-2026-11-05/repetition-analysis.md)
found no triggers in the two cancelled baseline attempts. The code-repair trace
contains short multi-line cycles; the log trace contains nonrepeating manual
counting. A thinking-only cycle prototype identified two counterfactual stopping
points in the saved code-repair trace. This is an experiment proposal, not a
production mechanism or a measured speed improvement.

A [fixed-threshold specificity replay](task-pilot-2026-11-05/repetition-success-specificity.md)
then examined seven successful baseline/v2 traces. It flagged the three
repetitive generations that reached the output cap without accepted tools or
visible answers, and none of 101 normally terminated generations. The affected
tasks later recovered. This supports a bounded experiment; earlier interruption
could change that recovery, and this small exposed sample establishes neither
a qualified false-positive rate nor runtime savings. No detector was enabled
in production or either live candidate.
