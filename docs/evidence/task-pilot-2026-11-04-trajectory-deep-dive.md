> Scope note (added by the coordinator): this analysis covers the 125 runs completed when it started. The 12B new-build batch on the eight fresh tasks (`pilot-new-rc2-12b-*`, 6/8 after regrading) and the final focused batch (`pilot-final-rc3-26b-focus-*`) finished later and are reported in task-pilot-2026-11-04.md, not here. Statements below that 12B was never run on the fresh tasks with a new build predate that batch.

# Task pilot 2026-11-04: trajectory deep dive (125 runs)

**Scope.** This covers 125 analyzed runs of gemma4:26b-mlx and gemma4:12b-mlx through the ordinary app path. "Baseline" means the old build (47 runs). "New" means the fixed, final, final2, rc1 and rc2 builds (78 runs). Run paths below are relative to `.runtime/` and use the short batch labels in the key.

**Headline**
- **Correctness.** New builds got 74 of 78 runs right (94.9%). Baseline got 29 of 47 (61.7%). On the 10 suite tasks, which both families ran, the score was 67/70 new against 21/31 baseline.
- **Latest builds.** Since final2 (the 40-step default), 47 of 48 runs were correct. The one miss is `rc2-26/log-question`, a constraint violation.
- **How baseline runs failed.** In 16 of 18 baseline failures, the host stopped the run. These were 8192-token length caps with no retry, model-call budget exhaustion, empty-generation stops and the tool-burst cap. Most trace back to the expectedHash sentinel and to the missing length and empty retries. The 4 new-build failures split into 2 host stops and 2 wrong answers.
- **What still costs time on new builds:**
  - generations cut off at the token cap and discarded: about 1,060 s across 6 runs
  - context compaction: 10 runs
  - hand arithmetic re-checking script output that was already correct: 4 of 4 26b sales-report runs
  - serial one-file operations
  - 32 failed tool calls that could have been avoided
- **Sample size.** Every (task, model, build) cell has n=1. Differences between fixed, final, final2, rc1 and rc2 come down to 1–2 runs, which is noise. The 12b model was never run on the 8 newer tasks (dedupe-contacts through sqlite-question) with any new build.

**Batch key**

| Label | Batch dir | Label | Batch dir |
|---|---|---|---|
| base26-def | pilot-baseline-defaults-26b-20261104-175446 | fixed26 | pilot-suite-fixed-26b-20261104-184203 |
| base12-new | pilot-new-baseline-12b-20261104-223749 | final26 | pilot-suite-final-26b-20261104-192837 |
| base26-new | pilot-new-baseline-26b-20261104-215132 | final12 | pilot-suite-final-12b-20261104-195004 |
| base12 | pilot-suite-baseline-12b-20261104-200813 | final2-26 | pilot-suite-final2-26b-steps40-20261104-202629 |
| base26-r1 | pilot-suite-baseline-26b-20261104-182144 | final2-12 | pilot-suite-final2-12b-steps40-20261104-204554 |
| base26-r2 | pilot-suite-baseline-26b-r2-20261104-190425 | rc1-26 / rc1-12 | pilot-suite-rc1-26b-r2-20261104-210938 / pilot-suite-rc1-12b-r2-20261104-212856 |
| | | rc2-26 | pilot-new-rc2-26b-20261104-221235 |

---

## 1. Outcomes

### By model and build family

| Model | Family | Runs | Correct | Incorrect | Cause categories of the incorrect runs |
|---|---|---|---|---|---|
| 26b | baseline | 29 | 18 (62%) | 11 | hash-friction 6 · output-limit 2 · command-friction 1 · step-budget 1 · arithmetic-in-reasoning 1 |
| 12b | baseline | 18 | 11 (61%)¹ | 7 | empty-generation 2 · hash-friction 1 · constraint-violation 1 · step-budget 1 · tool-burst 1 · arithmetic-in-reasoning 1 |
| 26b | new | 48 | 46 (96%)² | 2 | constraint-violation 1 · step-budget 1 |
| 12b | new | 30 | 28 (93%) | 2 | arithmetic-in-reasoning 1 · output-limit 1 |
| **All** | | **125** | **103** | **22** | |

¹ Includes `base12/organize-notes`. It was graded correct, but the run itself failed: the budget ran out right after the last move and there was no final answer. It is tagged step-budget.
² Includes `rc2-26/email-reply`, tagged checker-strictness. `base26-new/email-reply` is also counted correct after the regrade made the order-number check advisory.

### By build and task

| Build | Step cap | 26b | 12b | Task set |
|---|---|---|---|---|
| baseline | 24 | 18/29 | 11/18 | suite (26b r1, r2 and defaults calculator-smoke) + new-task set |
| fixed | 24 | 10/10 | — | suite |
| final | 24 | 9/10 | 8/10 | suite |
| final2 | 40 | 10/10 | 10/10 | suite |
| rc1 | not recorded | 10/10 | 10/10 | suite |
| rc2 | 40 | 7/8 | — | new-task set |

| Suite task | Base → New | | New-task set | Base → New (26b rc2 only) |
|---|---|---|---|---|
| calculator-smoke | 3/4 → 7/7 | | dedupe-contacts | 0/2 → 1/1 |
| codebase-qa | 3/3 → 7/7 | | email-reply | 2/2 → 1/1 |
| config-update | 3/3 → 7/7 | | explain-failure | 1/2 → 1/1 |
| crash-from-log | 3/3 → 7/7 | | json-to-csv | 1/2 → 1/1 |
| meeting-actions | 2/3 → 7/7 | | **log-question** | **0/2 → 0/1** |
| organize-notes | 1/3 → 7/7 | | readme-from-code | 2/2 → 1/1 |
| sales-report | 1/3 → 6/7 | | rename-refactor | 1/2 → 1/1 |
| todo-feature | 1/3 → 5/7 | | sqlite-question | 1/2 → 1/1 |
| wordfreq-repair | 2/3 → 7/7 | | | |
| write-tests | 2/3 → 7/7 | | | |

### How the failures ended

| Ending | Baseline (18) | New (4) |
|---|---|---|
| Hit the 8192-token output cap, with no retry or with retries used up | 9 | 1 |
| Model-call budget exhausted | 4 | 1 |
| Empty generation, then stop | 2 | 0 |
| Per-response tool-call cap | 1 | 0 |
| Completed with a wrong result | 2 | 2 |

Failed tool calls fell from 114 in 47 baseline runs (2.4 per run) to 66 in 78 new-build runs (0.85 per run). Both counts include deliberate red test runs, which are covered in §3.

---

## 2. What the improved builds fixed, and what remains

### Categories that disappeared or shrank (failing runs, baseline → new)

| Category | Base → New | What changed, with evidence | Representative runs |
|---|---|---|---|
| hash-friction | 7 → 0 | expectedHash is now optional or filled in by the host, and an empty value on a missing path is treated as "missing". 26 of 47 baseline runs had some expectedHash friction: rejections, made-up hashes, or reads made only to fetch a hash. In 5 of the 7 failures the model looped in its thinking over which value to send until it hit the 8192 cap. New builds had 0 expectedHash rejections, and the same tasks pass (meeting-actions 7/7, write-tests 7/7, wordfreq-repair 7/7, dedupe-contacts and json-to-csv 1/1). | `base26-r1/meeting-actions`, `base12-new/dedupe-contacts`, `base26-r1/todo-feature` (63-character hash sent 6 times) |
| Length-cap stops | 9 → 1 (tagged output-limit: 2 → 1) | New builds retry after a length cutoff, with feedback. 6 new runs hit the cap and 5 recovered. All 9 baseline runs that hit the cap failed. | `base26-new/explain-failure` (correct diagnosis lost to the cap), `base26-r2/organize-notes`; recovered: `rc2-26/sqlite-question` |
| empty-generation | 2 → 0 | The retry now includes corrective feedback. The baseline retry resent identical history and got the same empty result. All 3 empty generations on new builds recovered. | `base12/calculator-smoke`, `base12/todo-feature`; recovered: `final2-12/crash-from-log` |
| step-budget | 1 (+1 correct run that failed) → 1 → 0 | move_file no longer needs expectedHash, and the default is now 40 steps. The one new-build case ran at the 24 cap. No step-budget failures in the 48 runs since final2. | `base12-new/rename-refactor`, `base26-r1/organize-notes`, `final26/todo-feature` |
| arithmetic-in-reasoning | 2 → 1 → 0 | No failures after final. The 2 later 12b sales-report runs used scripts. | `base12/sales-report`, `final12/sales-report` |
| tool-burst | 1 → 0, **not retested** | sqlite-question was never re-run on 12b with a new build. | `base12-new/sqlite-question` |

Friction that is gone even though it was rarely fatal:
- **readOnly rejection after approval.** There were 21 rejections in 13 baseline runs, for example 4 in `base12/crash-from-log`. New builds have 0, because readOnly is now set automatically.
- **"Unverified" receipts.** All 18 baseline runs with a verification contract ended unverified. In new builds 2 of 36 did, and both were failed runs. The host now runs verify_task when the model doesn't.
- **plan_update revision conflicts.** There were 6 failed calls in fixed and final, and 0 in the 48 runs after the revision tolerance landed in taskplan.go.

### What remains: the 4 failures on new builds

| Run | Category | What happened |
|---|---|---|
| `final12/sales-report` | arithmetic-in-reasoning | It multiplied 36 rows in its thinking. It got 52×17.25 wrong and dropped a West row. It then claimed "The report is correct." |
| `final12/todo-feature` | output-limit | Both attempts produced about 27k characters of looping thinking with no tool call. The single retry used the same thinking settings, and the build allowed only 1 retry. |
| `final26/todo-feature` | step-budget | It used 24 of 24 calls. 2 went to compaction summaries, and 3–4 were lost after it misread a repaired-hash note and hit schema errors. The last call was a compaction, so the step-1 preamble was recorded as the final answer. |
| `rc2-26/log-question` | constraint-violation | It created analyze_logs.py even though the user said "don't change or create any files". The numbers were correct. The compaction summary also dropped the constraint. |

**Near misses** (correct, but only just):
- 3 runs used at least 24 model calls and would have failed under the old 24-step cap: `final2-26/todo-feature` (26), `rc1-12/todo-feature` (24) and `rc2-26/rename-refactor` (24).
- 5 runs survived a length-capped generation only through retry #1.

---

## 3. Remaining friction on new builds (78 runs)

### Failed tool calls: 66 in total, 32 avoidable

| Kind | Calls | Runs | Waste? | Examples |
|---|---|---|---|---|
| Deliberate red or reproduction test run (exit ≠ 0) | 23 | 22 | No. It is counted as a failure by mistake. | `final2-26/crash-from-log`, `rc1-26/wordfreq-repair` |
| Normal iteration: the model's own test bugs, probes | 11 | 6 | No | todo-feature in 5 runs (argparse SystemExit), `final2-12/write-tests` |
| run_command argv or shell assumptions | 5 | 5 | Yes | `rc2-26/sqlite-question` (literal quotes), `rc2-26/json-to-csv` (`python3 -c` nested inside -c), `fixed26/crash-from-log` (over-escaped `\"`), `final2-26/organize-notes` (`bash -c`), `final2-26/sales-report` (args `['rm generate_report.py']`) |
| Wrong test invocation or working directory | 5 | 4 | Yes | `final12/wordfreq-repair`, `final2-12/wordfreq-repair` (2), `rc1-12/wordfreq-repair`, `final2-26/crash-from-log` (test run as a script) |
| Other command errors | 2 | 2 | Yes | `rc1-12/sales-report` (ran the script before writing it), `rc1-26/sales-report` (grep alternation without -E, exit 1) |
| edit_file oldText not found | 7 | 6 | Yes | `fixed26/todo-feature` (2), `final12/write-tests` and `final2-12/write-tests` (whole file from memory), `final26/todo-feature` and `final2-26/todo-feature` (edit already applied) |
| plan_update revision conflict | 6 | 5 | Yes, fixed since | `fixed26/wordfreq-repair` (2), `final26/organize-notes` |
| Schema validation | 3 | 2 | Yes | `final26/todo-feature` (missing path; `expectedHash: null`), `rc1-26/todo-feature` (missing executable) |
| Sandbox denial (fork, or a write under forced readOnly) | 2 | 1 | Yes | `rc1-26/todo-feature`. It also weakened its tests to get around the denial. |
| make_directory on an existing directory refused as "protected" | 2 | 2 | Yes | `final12/wordfreq-repair`, `final2-12/wordfreq-repair` |

### Waste that produced no failed call

| Kind | Count | Cost |
|---|---|---|
| Generations cut off at the token cap and discarded | 6 runs: `rc2-26/sqlite-question` (192 s), `rc2-26/rename-refactor` (120 s, runaway tool arguments), `fixed26/wordfreq-repair` (138 s), `final12/todo-feature` (312 s, fatal), `final2-12/todo-feature` (159 s), `rc1-12/wordfreq-repair` (140 s, one line repeated 250 times) | about 1,060 s |
| Retry re-reads the whole prompt from scratch (feedback is appended to SystemInstruction, which invalidates the prefix cache) | 4 runs: `rc1-12/calculator-smoke` 16 s, `final2-12/crash-from-log` 28.5 s, `final2-12/todo-feature` 22 s, `final2-12/wordfreq-repair` 64 s | about 131 s |
| Context compaction | 12 events in 10 runs, all at about 19.4–20k of the 32k context. 1 was fatal (`final26/todo-feature`) and 1 dropped a user constraint (`rc2-26/log-question`). 3 ran just before the final answer (`final26`, `fixed26` and `rc1-26` sales-report). Others: `rc1-12/todo-feature` (2 compactions, about 119 s, stale summary), `final2-26/todo-feature` (forgot a file it wrote). | 16–75 s each |
| Hand arithmetic re-checking correct script output | 4 of 4 26b new-build sales-report runs: `rc1-26` (about 9 calls, 200 s), `final26` (6 calls, about 170 s), `fixed26` (3 calls, about 140 s), `final2-26` (45 s). Also `rc2-26/log-question` (miscounted 13 as 10). | about 9 minutes |
| One-file-at-a-time operations | organize-notes in 7 of 7 runs: 13 serial mkdir/move calls and 13 approvals, 16–22 model calls in total. config-update on 12b, 3 of 3 runs: 4 separate edits with 4 approvals. config-update on 26b, 4 of 4 runs: the whole file regenerated (1.5k–3.3k output tokens, 34–54 s). | steps and tokens |
| A successful result read as a failure | 4 runs: `rc2-26/json-to-csv` (delete revisionNote), `rc2-26/rename-refactor` (`created:false`), `final26/todo-feature` and `final2-26/todo-feature` (repaired-hash note led to re-edits and re-reads) | 1–4 calls each |

### Quality defects the checker did not catch

- **12b calculator-smoke.** In 3 of 3 runs, edit_file duplicated the module docstring next to the edit. The tool reported success, and the final answers didn't mention the change.
- **12b write-tests.** In 3 of 3 runs, the first draft contained leaked reasoning comments such as `# Wait,`. They stayed in the final file in 2 runs. In 2 runs, the code shown in the answer differs from the file on disk.
- **Helper scripts left behind.** 4 new runs: `fixed26`, `final2-26` and `rc1-12` sales-report, and `rc2-26/log-question`, where it was fatal.
- **Self-introduced undefined name.** `D` was used without being defined in `final2-26/crash-from-log` and `rc1-26/crash-from-log`. Both runs caught it by re-reading.

---

## 4. Model-limited vs host-addressable

| Failure | Classification | Rationale |
|---|---|---|
| `final26/todo-feature` | **Host-addressable, mostly fixed** | Under 40 steps, `final2-26/todo-feature` needed 26 calls and passed. The rest of the problem (compaction counted against the budget, the misleading revisionNote, null-vs-absent schema errors) is still host-side. |
| `final12/sales-report` | **Host-addressable through guidance; the arithmetic errors are a model limit** | The 2 later 12b runs (final2, rc1) used scripts and passed. That is consistent with the calculation guidance helping, but n=2. |
| `final12/todo-feature` | **Model behavior (12b degenerate repetition), mitigable by the host** | Both attempts looped. `final2-12` recovered after one retry. A third attempt with the same settings would probably loop again, so the fix is to change the retry conditions (thinking off or reduced, repetition abort), not to add retries. |
| `rc2-26/log-question` | **Model instruction-following, gateable by the host** | 0 of 3 across all builds and models. Both runs that wrote a file failed the check, and the third (`base26-new`) planned to write one. The host can enforce read-only intent. |

**Remaining friction**
- **Host-addressable:** argv misuse, test-invocation hints, edit_file diagnostics, tool-result wording, compaction policy, retry cache placement, batch operations, the read-only gate, and how red runs are classified.
- **Model-limited (the host can only mitigate):**
  - repetition loops, in both models
  - empty generations: 7 of the 8 runs with one were 12b
  - distrust of tool output: 26b re-checked script results by hand in all 4 new-build sales runs
  - silent duplication next to an edit, and leaked reasoning in written files: 12b, 3 of 3 each

---

## 5. Behavior patterns and outcomes

| Pattern | Counts | Association |
|---|---|---|
| Computing aggregates with a script | sales-report: script **7/7** correct, mental math **0/3** | Strong, n=10 |
| Re-deriving script output by hand (26b) | 4 of 4 new-build 26b sales-report runs; 0–9 extra calls, 45–200 s | Never changed the answer; pure cost |
| Hitting the 8192 cap (doneReason=length) | Baseline 9 of 9 failed. New builds: 6 runs, 5 recovered, 1 failed. | Fatal without a retry, but costs 2–5 minutes even with one. The loops center on one unresolved micro-question: which hash value, sqlite quoting, `args.sort is None`, stripping parentheses. |
| Calling verify_task on its own | 26b: **16/21** new-build contract runs. 12b: **0/15**, so the host ran it after the answer. Baseline: 0/18. | 12b answers can't cite the verification result (e.g. `final12/crash-from-log`). |
| Reproducing the failure first (red run) | 22 new runs; 21 correct | Good practice, but not predictive here: the base rate is 74/78 |
| Claiming verification without evidence | "Report is correct": 2 runs, both wrong (`base12` and `final12` sales-report). "Valid JSON" without parsing: 7 runs, all true by luck. | Unbacked claims are unreliable |
| Creating helper files under a no-write instruction | 2 of 2 file-writing log-question runs failed | Deterministic failure |
| High step counts | 6 new runs used 22 or more calls (todo-feature, rename-refactor, organize-notes) | Budget-sensitive tasks |
| Reading a successful result as a failure | 4 runs; 2 cascaded into 3–4 wasted calls | Driven by the wording of tool results |

---

## 6. Ranked backlog

**Before measuring anything:**
- Run 12b on the 8 newer tasks (dedupe-contacts through sqlite-question) with the current build; tool-burst and log-question have no 12b new-build data.
- Use at least 3 repetitions per (task, model). At n=1, one flaky run moves a build's score by 10 points.
- Have the harness record discarded-generation seconds, avoidable vs. deliberate failed calls, compaction count and seconds, approvals, and leftover files.
- Add scale variants (a 30-file organize-notes, a larger log) so budget and batching effects become visible.

**1. Enforce read-only intent**
- **Change:**
  - When the prompt forbids workspace changes ("don't change or create any files"), reject write_file and edit_file with a pointer to `run_command readOnly:true` plus `python3 -c`.
  - Default run_command to readOnly.
  - Add an end-of-run gate that lists any files created.
  - Copy user constraints word for word into compaction summaries.
- **Evidence:** log-question failed 0/3 overall, and it is the only failure since final2 (`rc2-26/log-question`, `base12-new/log-question`). The rc2 compaction dropped the constraint. `base12-new/explain-failure` wrote `.pytest_cache` into the workspace.
- **Expected impact:** likely fixes log-question (n=3). It also prevents cache side effects.
- **Risk:** false positives on narrow prohibitions ("don't modify tests/test_todo.py" while code edits are needed). Match only whole-workspace prohibitions.
- **Measure:** log-question and explain-failure, 3 reps per model. Check for regressions on todo-feature and crash-from-log, which contain narrow protected-file wording.

**2. Stream-level degeneracy guard, and retries that change conditions**
- **Change:**
  - Detect cycles of repeated blocks or n-grams in the thinking stream and in tool arguments, including code fences and short lines. The current proseRepetitionGuard misses these.
  - Cap tool-argument length relative to the target file's size.
  - On a trigger, abort early and retry with thinking reduced or off, plus a targeted nudge.
- **Evidence:** 6 new runs, about 1,060 s discarded, 1 fatal (`final12/todo-feature`). Others: `rc1-12/wordfreq-repair` (one line 250 times), `rc2-26/sqlite-question`, `rc2-26/rename-refactor` (runaway tool arguments). All 9 baseline runs that hit the cap failed.
- **Expected impact:** saves 2–4 minutes per occurrence and turns double-loop failures into recoveries.
- **Risk:** false aborts on content that is legitimately repetitive (CSV, test tables). Require N repeats with no progress, and log every trigger for audit.
- **Measure:** doneReason=length count, discarded-generation seconds, guard triggers and false-positive rate. todo-feature and wordfreq-repair on 12b, 5 reps.

**3. Compaction policy**
- **Change:**
  - Don't count compaction against maxSteps.
  - Trigger on headroom: skip it if `contextSize − measuredPrompt ≥ maxOutputTokens + margin`, or if the final answer is next.
  - Make it incremental from the previous summary.
  - Keep the constraints, plus the paths and hashes of files created or edited.
  - Trim failing-test output that a newer run has superseded.
  - Calibrate the token estimate from measured promptTokens: `rc2-26/log-question` measured 36.1k against an estimate of 13.2k, over the 32k context.
- **Evidence:** 12 compactions in 10 of 78 runs; 1 fatal (`final26/todo-feature`); 1 lost constraint; 3 wasted just before the final answer; `rc1-12/todo-feature` spent about 119 s on 2 compactions with a stale summary.
- **Expected impact:** fewer budget deaths on long tasks, plus 15–75 s per avoided compaction.
- **Risk:** context overflow if compaction is delayed too long. Rely on measured tokens, not chars/4.
- **Measure:** compactions per run, summary-call seconds, re-reads after compaction, overflow errors, model calls on todo-feature and rename-refactor.

**4. Put retry feedback at the end of the prompt, and don't charge retries to the step budget**
- **Change:** Inject length and empty-response feedback as a trailing message instead of editing SystemInstruction, so the cached prompt prefix survives.
- **Evidence:** 4 runs paid a full re-prefill, 16–64 s each and about 131 s in total: `final2-12/wordfreq-repair` 64 s against about 1 s cached; `final2-12/crash-from-log`. Retries also used steps in `rc2-26/rename-refactor` and `final2-12/crash-from-log`.
- **Expected impact:** retry prefill drops from 16–64 s to about 1–3 s.
- **Risk:** the feedback might carry less weight at the end of the prompt. Check that the retry recovery rate stays the same.
- **Measure:** prompt_eval seconds on retry attempts compared with the previous step; recovery rate.

**5. Treat executed results as authoritative**
- **Change:** Add to the base instructions, not only to the length feedback: "Compute multi-row arithmetic with a script. Executed output beats mental math. If you doubt it, print per-row values from a script."
- **Evidence:** sales-report was 7/7 with a script and 0/3 without. `final12/sales-report` failed. All 4 26b new-build sales-report runs re-derived the numbers by hand, costing about 9 minutes in total (`rc1-26/sales-report` alone made 9 extra calls).
- **Expected impact:** removes an entire failure class and most of the sales-report time.
- **Risk:** low; it may add a script call for trivial sums.
- **Measure:** sales-report and log-question wall time; calls made after the first correct script output; 12b correctness over 3 reps.

**6. edit_file robustness**
- **Change:**
  - When oldText isn't found, report the first line that differs and the current hash.
  - When newText is already present, say "already applied".
  - Return a short diff of the applied change (20 lines or fewer).
  - Warn when newText duplicates lines next to oldText.
  - Accept an atomic list of replacements under one hash and one approval.
- **Evidence:** 7 not-found failures in 6 runs (2 were re-applying an edit that had already landed). Silent docstring duplication in 3 of 3 12b calculator-smoke runs. Whole-file rewrites in 4 of 4 26b config-update runs, done "to avoid edit_file mismatch" (`rc1-26/config-update`: 3,290 tokens, 54 s). 4 approvals per 12b config-update run.
- **Expected impact:** fewer wasted steps and tokens, and silent defects become visible.
- **Risk:** larger tool results use context; partial multi-edit failures need atomic semantics.
- **Measure:** not-found count, duplicated blocks in the calculator-smoke diff, config-update output tokens and approvals.

**7. run_command misuse and invocation hints (hint only, never block)**
- **Change:** Flag these cases:
  - args[0] starting with the executable name plus a space
  - an argument wrapped in literal quotes
  - a `-c` payload that starts with the executable name
  - `\"` sequences inside `-c`
  - a script path that doesn't exist yet (reject before approval)
  - ModuleNotFoundError for a workspace package when run from tests/ or as a script (point to the contract command from the root)
  - grep exit 1 (report as "no matches", not a failure)

  Echo the exact argv in error results, and put the contract command in the system prompt.
- **Evidence:** 12 avoidable failed calls in 10 runs (`rc2-26/sqlite-question`, `final2-26/sales-report`, 3 of 3 12b wordfreq-repair runs started with the wrong test invocation).
- **Expected impact:** about 1–2 steps saved per occurrence.
- **Risk:** low.
- **Measure:** failed run_command calls per run, excluding test exits ≠ 0.

**8. Make successful results read as successful (cheap)**
- **Change:**
  - Start a repaired-hash note with "Edit applied successfully".
  - Report edit_file results as `applied: true, replacements: 1` rather than `created:false`.
  - Make make_directory on an existing directory a no-op.
  - When a parent of a protected path is refused, name the protected file.
- **Evidence:** success misread as failure in 4 runs, which contributed 3–4 calls to the fatal `final26/todo-feature`. Misleading mkdir refusals in 2 runs.
- **Expected impact:** small per run, at almost no cost.
- **Risk:** about none.
- **Measure:** re-reads or re-edits within 2 steps of a successful mutation.

**9. Batch file operations**
- **Change:** Let move_file accept a list of moves with createParents, under one approval that shows a preview of every move.
- **Evidence:** organize-notes used 16–22 model calls with 13 approvals in 7 of 7 runs; `final26/organize-notes` used 22 of 24 steps; budget or hash problems caused 2 of the 3 baseline organize-notes failures.
- **Expected impact:** cuts about 13 calls to about 2; this matters at scale.
- **Risk:** a bigger blast radius per approval. Keep a checkpoint per batch.
- **Measure:** model calls and approvals on organize-notes, and on a new 30-file variant.

**10. Verification and receipt hygiene**
- **Change:**
  - Classify test exits ≠ 0 as "tests failed (expected)" rather than tool errors.
  - Run the contract verification before the answer is finalized and give the result to the model.
  - Count an identical readOnly contract command as verification.
  - Note created files that the answer doesn't mention.
  - Lint written code for `# Wait,` and `# Actually,`.
  - Parse .json files after writing them.
- **Evidence:**
  - 23 of 66 new-build failed calls are deliberate red runs.
  - 12b never calls verify_task itself (0/15).
  - 4 runs ran the same suite twice (run_command, then verify_task).
  - Helper files were left in 4 runs, leaked reasoning stayed in 2 final files, and 7 "valid JSON" claims were never parsed.
- **Expected impact:** accurate metrics and answers that state real verification.
- **Risk:** low. Verifying before the answer adds one generation.
- **Measure:** failed calls excluding red runs; share of answers that cite the verification result; leftover-file count.

**Also considered, lower priority:**
- Trim disconnected or irrelevant tools: about 12 KB of declarations and a 9–12 s cold prefill on step 1 (`final12/codebase-qa`).
- Map sandbox EPERM errors to concrete alternatives such as tempfile or calling `main()` in-process (`rc1-26/todo-feature`, n=1).
- Fix the delete_file expectedHash description, which still says "use missing only when creating a new file".
