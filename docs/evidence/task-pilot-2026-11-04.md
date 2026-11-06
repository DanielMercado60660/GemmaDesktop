# Everyday task pilot — 4 November 2026

Observed Gemma task attempts through the ordinary app path, before and after
this round's host fixes ([plan](../implementation/TASK_RELIABILITY_PLAN_2026-11-04.md)).
Gemma was the only solver. Claude wrote the host changes, the task suite and
the checkers; no candidate file was edited and no grade was relaxed.

## Setup

- Machine: Apple M4 Pro, 64 GiB, macOS 27.0.1. Ollama 0.40.11.
- Models: `gemma4:26b-mlx` (digest `f0fc7e0ae494…`, nvfp4) and `gemma4:12b-mlx`
  (`ded7a2735003…`).
- Settings: a fresh install's defaults — 32K context, 24 model steps,
  temperature 0.2, thinking auto, 8,192 output tokens, the full tool catalogue,
  approval mode, macOS sandbox executor. Identical for every batch.
- Driver: [`scripts/pilot/`](../../scripts/pilot/README.md). Each batch uses its
  own isolated data directory under `.runtime/pilot-<label>-<time>/`; personal
  app data was never touched. The driver approves governed actions like a
  cooperative user and refuses edits to protected paths.
- Grading: hidden per-task checkers run candidate code read-only under
  `sandbox-exec` with no network. Every checker passed its controls (untouched
  fixture fails, reference passes) and an adversarial review against
  alternative-correct and plausible-wrong solutions.
- Binaries (production tag, built from this tree):
  baseline `46cb9517…` (tree before this round's changes),
  fixed `1e843ff7…` (first fix bundle), final `f994ab53…` (after review fixes).

The ten tasks are new development material, not held-out evidence; they are
now exposed. One attempt per task per batch.

| Task | Kind of work | Declared check |
| --- | --- | --- |
| calculator-smoke | Tiny repair (README demo, already exposed) | `python3 -m unittest -v` |
| wordfreq-repair | Two bugs in two files + regression tests | unittest discover |
| codebase-qa | Read-only question about a small service | — |
| sales-report | CSV → markdown revenue table | — |
| meeting-actions | Meeting notes → action-item checklist | — |
| config-update | Four precise JSON edits, nothing else | — |
| organize-notes | Move 10 dated notes into year folders | — |
| crash-from-log | Fix a crash from a log traceback + test | unittest discover |
| todo-feature | Implement a README-specified CLI feature + tests | unittest discover |
| write-tests | Write tests; graded by mutation kills | `python3 -m unittest -v test_text_utils` |

## Results (Gemma 26B)

| Task | suite-baseline-26b | suite-baseline-26b-r2 | suite-fixed-26b | suite-final-26b |
|---|---|---|---|---|
| calculator-smoke | PASS, unverified · 29s · 7 calls (2 failed) | PASS, unverified · 54s · 12 calls (4 failed) | PASS, verified · 37s · 7 calls (1 failed) | PASS, verified · 25s · 7 calls (1 failed) |
| wordfreq-repair | PASS, unverified · 136s · 18 calls (5 failed) | FAIL, unverified, run failed · 198s · 10 calls (3 failed) | PASS, verified · 319s · 19 calls (3 failed) | PASS, verified · 124s · 16 calls (2 failed) |
| codebase-qa | PASS · 29s · 6 calls (0 failed) | PASS · 30s · 5 calls (0 failed) | PASS · 28s · 5 calls (0 failed) | PASS · 19s · 5 calls (0 failed) |
| sales-report | FAIL, run failed · 147s · 2 calls (0 failed) | PASS · 247s · 14 calls (5 failed) | PASS · 292s · 8 calls (0 failed) | PASS · 259s · 12 calls (0 failed) |
| meeting-actions | FAIL, run failed · 193s · 6 calls (3 failed) | PASS · 54s · 3 calls (0 failed) | PASS · 51s · 3 calls (0 failed) | PASS · 54s · 3 calls (0 failed) |
| config-update | PASS · 72s · 4 calls (0 failed) | PASS · 58s · 4 calls (0 failed) | PASS · 41s · 4 calls (0 failed) | PASS · 39s · 4 calls (0 failed) |
| organize-notes | FAIL, run failed · 80s · 24 calls (1 failed) | FAIL, run failed · 152s · 4 calls (0 failed) | PASS · 88s · 17 calls (1 failed) | PASS · 73s · 21 calls (1 failed) |
| crash-from-log | PASS, unverified · 118s · 15 calls (5 failed) | PASS, unverified · 110s · 13 calls (4 failed) | PASS, verified · 128s · 11 calls (2 failed) | PASS, verified · 92s · 9 calls (1 failed) |
| todo-feature | FAIL, unverified, run failed · 311s · 23 calls (10 failed) | PASS, unverified · 360s · 15 calls (4 failed) | PASS, verified · 273s · 19 calls (4 failed) | FAIL, unverified, run failed · 348s · 20 calls (4 failed) |
| write-tests | PASS, unverified · 60s · 9 calls (3 failed) | FAIL, unverified, run failed · 179s · 4 calls (1 failed) | PASS, verified · 59s · 6 calls (0 failed) | PASS, verified · 59s · 9 calls (1 failed) |
| **Total** | 6/10 correct; 0/5 host-verified | 7/10 correct; 0/5 host-verified | 10/10 correct; 5/5 host-verified | 9/10 correct; 4/5 host-verified |

Columns: two runs of the old build, the first fix bundle, and the final build
(after review fixes), all with identical settings. Each cell: independent
grade, host verification status when the task declared a check, run status
if not completed, wall time, tool calls (failed calls).

| Summary | Old build (2 runs) | Fixed builds (2 runs) |
| --- | --- | --- |
| Independently correct | 13/20 (65%) | 19/20 (95%) |
| Declared checks host-verified | 0/10 | 9/10 |
| Runs killed (output limit or step budget) | 7 | 1 (step budget) |
| Failed tool calls | 50 | 21 |

The final-build miss (todo-feature) was not friction: Gemma made steady
progress with four failed calls and ran out of the 24-step budget mid-repair.
That run, plus two earlier budget exhaustions, is why fresh installs now
default to 40 steps (measured separately below).

### Independent review of the final-build outputs

A separate reviewer per task read each diff and final answer against the
user's request, probing suspected defects with small experiments. Of the nine
completed tasks: six sound, three with minor issues, none with a substantive
defect, and every final answer was honest about what was run.

| Task | Verdict | Notable |
| --- | --- | --- |
| calculator-smoke | sound | Stale module docstring (cosmetic). |
| wordfreq-repair | minor issues | Fix matches the reference exactly, but the new regression tests duplicate existing ones and would not catch two plausible partial fixes. |
| codebase-qa | minor issues | Correct; did not read the README/legacy file, so it could not warn about the stale 8000/5000 ports. |
| sales-report | sound | Wrote a script with month-prefix filtering and Decimal arithmetic; totals recomputed exactly. |
| meeting-actions, config-update, organize-notes | sound | — |
| crash-from-log | sound | Names the exception slightly imprecisely; regression test is narrow. |
| write-tests | minor issues | Custom-separator and digit cases thinly covered (still killed ≥4/6 mutants). |
| todo-feature | run hit budget | Left a failing test and legacy items without a shown priority, and the user received **no final answer**. |

The last row is a product gap: when a run exhausts its budget, the user gets
an error instead of a status report. That is the top next change.

## Why the baseline failed

Every one of the seven baseline failures was the run being killed, not a wrong
answer delivered:

| Batch | Task | How the run ended | What led there |
| --- | --- | --- | --- |
| baseline r1 | sales-report | Output limit (8,192 tokens) | Totalled ~50 CSV rows inside its reasoning instead of running a script. |
| baseline r1 | meeting-actions | Output limit | Could not create `ACTIONS.md`: omitted hash rejected, guessed hash got a misleading "changed since read" error, then looped in reasoning about `expectedHash`. |
| baseline r1 | organize-notes | 24-step budget | Read every file just to copy its hash for `move_file`; 8 of 10 moved. |
| baseline r1 | todo-feature | 24-step budget | 7 of 10 failed calls were revision conflicts on its own edits (reused its first read's hash). |
| baseline r2 | wordfreq-repair | Output limit | New-file hash rejection, then a cut-off generation. |
| baseline r2 | organize-notes | Output limit | One long generation after creating the folders. |
| baseline r2 | write-tests | Output limit | New-file hash rejection ("expectedHash is required"), then a cut-off generation. |

Baseline failures are intermittent: sales-report, meeting-actions and
todo-feature passed in the repeat. The host fixes remove failure modes that
strike some of the time, rather than unlocking tasks the model cannot do.

Separately, **no baseline run was ever host-verified**: in every task with a
declared check, Gemma either ran the right command itself or nothing, but
never `verify_task`, so even correct work stayed "unverified". The same
happened on the production Harness path (below).

## What changed in the host

| Change | Where |
| --- | --- |
| Host runs the declared check itself when the model finishes after changing files, through the same approval and executor as a model call; on a real failure it returns the output tail (inside the untrusted-data boundary) for at most two repair rounds. Checks that could not start or hit a host limit end without "fix it" feedback. Turns that changed nothing skip the check. | `internal/engine/engine.go`, `verification.go`; `internal/tools/verification.go` |
| `expectedHash` optional: the host fills the revision the model last saw in this conversation, repairs miscopied hashes, creates files when omitted, lets `move_file` use the current revision, and applies an `edit_file` that names the model's own earlier revision on top of its change. Stale or cross-conversation revisions still conflict; whole-file writes never clobber unseen content. | `internal/tools/manager.go` |
| Whitespace/CRLF-tolerant single-match fallback for `edit_file`, validated and applied on the same span. | `internal/workspace/workspace.go` |
| Actionable `run_command` errors (executable with arguments, repeated executable, shell operators, shells, packed flags, `cd`); accurate macOS-sandbox description (was Docker wording); auto `readOnly` for protected tasks. | `internal/tools/manager.go` |
| One retry after an output-length stop, with a nudge to keep reasoning short and use a script for calculations. | `internal/engine/adapter.go` |
| "Task check" instructions naming the declared command and `verify_task`. | `internal/app/verification_contract.go`, `runs.go` |
| Composer "Check command" field (per workspace) so users can declare a check. | `frontend/src/lib/checkCommand.ts`, `App.tsx` |
| `verify_task` receipt survives large output (previously lost its pass/fail fields above 128 KiB). | `internal/tools/verification.go` |

Every change has focused tests that fail without it; the standard suite
(`./scripts/test.sh`) passes. Two adversarial review passes found and fixed
nine defects before the final build (e.g. workflow runs re-running their whole
graph; revision memory shared across workflow nodes; question-only turns
triggering repair loops).

## Production Harness path (`event-join`, Docker solver + grader)

One attempt per build through the real Harness² diagnostics API
(`harness.arms.create` → `harness.diagnostics.create/advance`), Gemma 26B,
65,536 context, thinking on, temperature 1, 80 steps, 1,200 s, pinned
toolchain image `sha256:09c68348…`.

| Build | Grade | Time | Tool calls | What happened |
| --- | --- | --- | --- | --- |
| Old | **fail** (13/14 checks) | 271 s | 31 | All six hidden contract cases, red/green regression tests and integrity passed. Failed only "prescribed verification after final edit": the solver prompt said to use `verify_task`; Gemma never did, and its final `node --test` lacked `readOnly:true`. Hash rejections pushed it to write files through shell heredocs. |
| Final | **pass** (14/14) | 183 s | 19 | Created and rewrote its regression test with plain `write_file` calls (no hash). Its last command was a read-only `node --test` after the last edit. |

The final-build pass came from Gemma's own read-only run, not the host check
(this arm left `verificationFeedback` off). 

A third attempt on the release-candidate build enabled host verification on
the arm (`verificationFeedback: true`):

| Build | Grade | Time | Tool calls | What happened |
| --- | --- | --- | --- | --- |
| Release candidate, host verification on | **pass** (14/14) | 209 s | 14 | Gemma's last command was a writable `node --test`. When it finished, the host ran the frozen check itself (`verify_task`, call ID from the host verifier rather than the model) and the grader accepted that receipt as fresh verification after the final edit. |

This is the host-owned verification path working end to end on the
production solver and Docker grader. The Harness trial trace does not yet keep
the `hostInitiated` marker (it was identified by its host-issued call ID);
recording it is a small follow-up.

## Gemma 12B (the fresh-install default model)

Same suite and settings with `gemma4:12b-mlx`, which a fresh install selects
by default. The release-candidate column also uses the new 40-step default.

| Task | suite-baseline-12b | suite-final-12b | suite-final2-12b-steps40 |
|---|---|---|---|
| calculator-smoke | FAIL, unverified, run failed · 38s · 3 calls (0 failed) | PASS, verified · 35s · 6 calls (0 failed) | PASS, verified · 35s · 6 calls (0 failed) |
| wordfreq-repair | PASS, unverified · 183s · 13 calls (3 failed) | PASS, verified · 177s · 16 calls (3 failed) | PASS, verified · 255s · 20 calls (4 failed) |
| codebase-qa | PASS · 32s · 4 calls (0 failed) | PASS · 40s · 5 calls (0 failed) | PASS · 42s · 5 calls (0 failed) |
| sales-report | FAIL · 118s · 5 calls (1 failed) | FAIL · 138s · 4 calls (0 failed) | PASS · 59s · 5 calls (0 failed) |
| meeting-actions | PASS · 57s · 6 calls (1 failed) | PASS · 55s · 3 calls (0 failed) | PASS · 46s · 4 calls (0 failed) |
| config-update | PASS · 64s · 10 calls (0 failed) | PASS · 52s · 7 calls (0 failed) | PASS · 58s · 7 calls (0 failed) |
| organize-notes | PASS, run failed · 127s · 24 calls (0 failed) | PASS · 77s · 15 calls (0 failed) | PASS · 101s · 15 calls (0 failed) |
| crash-from-log | PASS, unverified · 155s · 16 calls (5 failed) | PASS, verified · 95s · 11 calls (1 failed) | PASS, verified · 118s · 11 calls (1 failed) |
| todo-feature | FAIL, unverified, run failed · 225s · 9 calls (0 failed) | FAIL, unverified, run failed · 335s · 3 calls (0 failed) | PASS, verified · 440s · 16 calls (2 failed) |
| write-tests | PASS, unverified · 91s · 8 calls (3 failed) | PASS, verified · 82s · 8 calls (1 failed) | PASS, verified · 87s · 9 calls (2 failed) |
| **Total** | 7/10 correct; 0/5 host-verified | 8/10 correct; 4/5 host-verified | 10/10 correct; 5/5 host-verified |

- Host verification transfers fully (0/5 → 4/5 → 5/5).
- On the final build 12B's two misses were model-side: it totalled the CSV in
  its head and got two regions wrong, and on todo-feature it produced two
  consecutive over-long generations that the single retry could not rescue.
- The release candidate adds the standing instruction to script calculations
  over many values and the 40-step budget. 12B then wrote a script for
  sales-report (correct in 59 s) and completed todo-feature in 16 tool calls —
  its first pass on that task in any build.

### Independent review of the 12B release-candidate outputs

Same method as the 26B review. All ten outputs: three sound, seven with minor
issues, none with a substantive defect.

- Recurring minor issues: regression tests that duplicate existing tests (so
  they would not catch plausible partial fixes); a duplicated module docstring
  from an over-broad edit; read-only answers that are correct but do not warn
  about stale conflicting sources.
- One honesty slip: in write-tests, 12B's final answer showed a cleaned-up
  test file although its cleanup edit had failed, leaving a stray reasoning
  comment on disk. Its test-result claims matched the receipt. This is a
  model-side limitation; the host already instructs it never to claim a change
  without a successful result.

## Release candidate (40-step default, calculation guidance, plan-revision tolerance)

Gemma 26B, final build vs release candidate, otherwise identical settings:

| Task | suite-final-26b | suite-final2-26b-steps40 |
|---|---|---|
| calculator-smoke | PASS, verified · 25s · 7 calls (1 failed) | PASS, verified · 30s · 7 calls (1 failed) |
| wordfreq-repair | PASS, verified · 124s · 16 calls (2 failed) | PASS, verified · 124s · 16 calls (1 failed) |
| codebase-qa | PASS · 19s · 5 calls (0 failed) | PASS · 22s · 5 calls (0 failed) |
| sales-report | PASS · 259s · 12 calls (0 failed) | PASS · 87s · 8 calls (1 failed) |
| meeting-actions | PASS · 54s · 3 calls (0 failed) | PASS · 54s · 3 calls (0 failed) |
| config-update | PASS · 39s · 4 calls (0 failed) | PASS · 50s · 4 calls (0 failed) |
| organize-notes | PASS · 73s · 21 calls (1 failed) | PASS · 70s · 21 calls (1 failed) |
| crash-from-log | PASS, verified · 92s · 9 calls (1 failed) | PASS, verified · 104s · 14 calls (2 failed) |
| todo-feature | FAIL, unverified, run failed · 348s · 20 calls (4 failed) | PASS, verified · 339s · 25 calls (4 failed) |
| write-tests | PASS, verified · 59s · 9 calls (1 failed) | PASS, verified · 44s · 6 calls (0 failed) |
| **Total** | 9/10 correct; 4/5 host-verified | 10/10 correct; 5/5 host-verified |

- todo-feature passed using 26 model calls; the final-build attempt had
  stopped at exactly 24.
- With the calculation guidance, 26B wrote and ran a script for sales-report
  straight away: 4,040 output tokens versus 11,000–13,000 on earlier builds
  (87 s versus ~250–290 s).

## Fresh tasks (never used for tuning)

Eight additional tasks were written after the host changes were made and were
not used to tune them: README writing, a rename refactor, JSON→CSV, a log
question, a SQLite question, a customer email reply, contact de-duplication,
and a read-only test-failure diagnosis. Each ran once on the old build (old
defaults) and once on the new build (release candidate 2, new defaults).

| Task | Old 26B | New 26B | Old 12B | New 12B |
|---|---|---|---|---|
| readme-from-code | PASS | PASS | PASS | PASS |
| rename-refactor | PASS, unverified | PASS, verified | FAIL · empty turns at budget | PASS, verified |
| json-to-csv | FAIL · output limit | PASS | PASS | PASS |
| log-question | FAIL · output limit | FAIL · created a file | FAIL · created a file | FAIL · output limit |
| sqlite-question | PASS | PASS | FAIL · tool-call burst | FAIL · three empty turns |
| email-reply | PASS* | PASS* | PASS | PASS |
| dedupe-contacts | FAIL · output limit | PASS | FAIL · step budget | PASS |
| explain-failure | FAIL · output limit | PASS | PASS | PASS |
| **Total** | **4/8** | **7/8** | **4/8** | **6/8** |

\* Grading correction, disclosed: the email-reply checker originally required
the order number, which the prompt never asks for. Both 26B replies were
otherwise correct and failed only that check. It is now advisory
(`critical=False`) and every fresh-task batch was regraded from its frozen
files with `scripts/pilot/regrade.py`; original records are unchanged. Under
the original grading the totals are 3/8, 6/8, 4/8, 6/8.

- The new build removed the old build's dominant failure on unseen work:
  four output-limit deaths (26B) and a budget exhaustion (12B) became passes.
- The log question remains hard. The new 26B answered it correctly but created
  a helper script although the user said not to create files; the standing
  calculation guidance ("write and run a short script") likely nudged it there.
  The guidance now says to use `python3 -c` when files must not be created
  (final build; see the focused batch below).
- 12B's remaining misses are model/runtime-side: three consecutive empty
  generations (Ollama's Gemma 4 tool-call parsing) and an over-long
  generation that two retries could not rescue.
- Independent review of the new 26B outputs: three sound, four minor issues,
  one substantive (the created file, which its answer also did not mention).
  The rename left two test identifiers containing the old name while its
  answer said every reference was updated.

### Final build, focused check (Gemma 26B)

The final source (all fixes, including the refined "use `python3 -c` when
files must not be created" guidance) ran the five non-trivial fresh tasks plus
the heaviest original tasks. All **8/8 were correct**: log-question (answered with
`python3 -c`, **no files created**, 82 s), email-reply, json-to-csv,
dedupe-contacts, explain-failure, sales-report (76 s), and todo-feature and
wordfreq-repair (both host-verified).

A sealed pool of twelve further tasks (`scripts/pilot/tasks-heldout/`) was
authored and cross-reviewed but deliberately not run.

A [trajectory deep dive](task-pilot-2026-11-04-trajectory-deep-dive.md) over
125 completed runs found new builds correct in 74/78 runs (95%) versus 29/47
(62%) for the old build, and 47/48 correct after the 40-step default; it
ranks the next host changes.

## End-to-end product journey (native UI, fresh install defaults)

A fresh data directory on the release-candidate build (default agent: 12B,
40 steps), driven through the real web UI in the built-in browser:

1. Typed the README demo request into the composer, opened **Check**, entered
   `python3 -m unittest -v` (a piped variant was rejected inline with
   "Use a single command — shells aren't available.").
2. Sent it, then reviewed and approved each action in the approval dialog:
   the failing test run, the `calculator.py` edit, the passing re-run, and
   `verify_task` ("Run the frozen task verification command with source
   mounted read-only").
3. The run finished **Completed · Task verification passed** with an accurate
   answer. An independent rerun confirmed 3/3 tests pass and the test file is
   unchanged. 12B's edit duplicated the module docstring (harmless, cosmetic).

## Limits of this evidence

- One attempt per task per batch (two for the 26B baseline); small samples.
- The tasks are small (≤ 40 KB fixtures) and Python-only on the macOS sandbox.
- Development material, now exposed; not a held-out qualification.
- Timing is indicative only: other work (tests, reviews) shared the machine.
- Defaults were not changed; temperature, step budget and tool scoping remain
  candidates to measure next.
