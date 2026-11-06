# Task reliability plan — 4 November 2026 (Claude Code round)

Goal: the local Gemma agent completes everyday user requests through the
ordinary app path, with every result independently checked. Gemma stays the
solver; Claude builds the host and grades. A repair written by Claude is never
counted as a Gemma success.

## What exploration established

| Finding | Evidence |
| --- | --- |
| Gemma 26B solves more than past scores show. In 49 of 72 campaign attempts the repair passed **every hidden semantic check**; the attempt was failed only because the final test run lacked `readOnly:true`. Main-path logic was right in every completed attempt. | Raw trial records in `.runtime/phase2-campaign-20261027/` re-classified by attempt; summary in this round's exploration notes. |
| The same gap reproduces on today's build. Calculator smoke, 26B, default settings: fixed in 43.5 s, 8 tool calls, independent check passed — but host verification says `unverified` because the model ran the right command read-only instead of calling `verify_task`. | `.runtime/pilot-baseline-defaults-26b-20261104-175446/` |
| Edit friction: 143 of 372 edit calls (38%) were rejected, mostly miscopied or reused 64-hex `expectedHash` values; write rejections preceded 4 of 8 generation-limit deaths. | Campaign trial records. |
| Command friction: ~70 of 486 `run_command` calls misused argv (executable repeated in args, packed flag strings, shell quoting). Generic denials led to guessing loops (9 denials in one Fieldnote attempt). | Campaign + Fieldnote records. |
| Output-limit stops kill the whole run even when the work is already on disk (8 of 72). | `adapter.go` length handling; campaign records. |
| Ordinary runs never tell the model about `verify_task`, the declared command, or how to finish; on the Seatbelt executor the `run_command` description is replaced with Docker-only wording. Default agent duplicates the base instructions. | `internal/tools/manager.go`, `internal/app/core.go`, `runs.go`. |
| Ollama's Gemma 4 tool-call parser can return an empty turn (HTTP 200, no content, no calls) for malformed calls; `think:false` after a tool result can produce empty replies on 26B. The app retries an empty turn once, then fails. | Ollama issues #17882, #18649, #18861, #19273; `adapter.go`. |
| Defaults diverge from Google's guidance (temperature 0.2 vs 1.0); 24-step default budget is tight; ~25 tools exposed for any task. | Model card; `core.go`, `agent_controls.go`, `capability.go`. |

## Approach

One evidence loop, measured on the same tasks before and after each change:

1. **Baseline** the current build on a 10-task everyday suite.
2. **Fix demonstrated host problems** (below), each with unit tests.
3. **Re-run the identical suite** on the fixed build and compare per task.
4. Exercise the production Harness solver/grader on one diagnostics task
   (`event-join`) on both builds.

Defaults change only where the suite shows a benefit. Experimental mechanisms
stay opt-in unless measured.

## Milestone A — everyday task suite and baseline (first)

`scripts/pilot/` holds a headless driver (`gemma_pilot.py`) that starts an
isolated app instance (fresh data directory, never personal data), acts as a
cooperative user approving governed actions, and records prompt, settings,
model identity, transcript, receipts, final files, diff and independent checks.
Each task has a hidden checker that runs candidate code read-only in a macOS
sandbox, plus a negative control (untouched fixture fails) and a positive
control (reference solution passes) proven by `check_controls.py`.

| Task | Kind of work |
| --- | --- |
| calculator-smoke | Tiny repair (exposed README demo; smoke only) |
| wordfreq-repair | Multi-file bug fix + regression tests (red/green checked) |
| todo-feature | Implement a requested CLI feature with tests |
| codebase-qa | Read-only question about a project (answer graded, files unchanged) |
| sales-report | CSV analysis into a markdown report |
| meeting-actions | Extract action items from meeting notes |
| config-update | Precise JSON config edits, nothing else changed |
| organize-notes | Move files into year folders, contents preserved |
| crash-from-log | Debug a crash from a log traceback, fix, add test |
| write-tests | Write unit tests; graded by mutation kills |

Per attempt we record separately: model completed, host verification,
independent correctness, protected-input integrity, time, tool calls and
rejected calls. First-attempt results are kept apart from any follow-up.

## Milestone B — host fixes for demonstrated failures

| # | Change | Expected effect |
| --- | --- | --- |
| B1 | **Host-owned verification at completion.** When a run with a verification contract ends unverified, the host runs the contract read-only itself and records the receipt. If it fails, the failing output is returned to the model for a bounded number of repair rounds. An exact read-only run of the declared command after the last change also counts. | Turns the 49-attempt failure class into an invariant; no flag to remember. |
| B2 | **Edit friction.** Host remembers the last hash it served per path; `expectedHash` becomes optional when the host knows the file's state from this run. Mismatch errors return the current hash and a short excerpt. Exactly-once `oldText` matching gains a whitespace-tolerant fallback. | Fewer rejected edits and runaway turns. |
| B3 | **Actionable command errors.** Detect executable repeated in `args[0]`, a packed argument string, or shell syntax, and return a corrected example. Restore accurate Seatbelt wording for `run_command`. | Fewer guessing loops. |
| B4 | **Output-limit recovery.** On a length stop, retry once with a short continuation nudge instead of failing the run. | Work already on disk is not lost to one long turn. |
| B5 | **Ordinary-run instructions.** Tell the model how to finish, and when a contract exists, about `verify_task` and the declared command. Stop duplicating the base instructions. | Clearer protocol for a small model. |
| B6 | **Defaults (only if measured).** Temperature 1.0, larger step budget, task-scoped tool sets. | Compare on the suite before adopting. |
| B7 | **Check command in the composer.** No UI path sends a verification contract today, so ordinary users never get `verify_task` or a verified result. Add an optional per-workspace "Check command" that becomes the run's contract; the existing verification badge shows the outcome. | Users can say how work is checked; B1 then applies in the real app. |

Each change gets focused Go tests; the standard suite (`./scripts/test.sh`) must
stay green.

## Milestone C — re-measure

Run the identical suite on the fixed build with identical settings, then the
`event-join` Harness diagnostic. Report per task: before/after on each outcome,
every failed attempt kept, and the next evidence-driven change.

## Later (not this round unless time allows)

Harness² refinement on development tasks (needs a recorded author-identity
extension for Claude authorship), 12B/E4B comparisons, managed MLX
qualification, 65K-context proof, Ollama 0.40.14 upgrade measurement.

## Rules kept from the handoff

- Unique output folders; historical evidence is never overwritten.
- Live batches stay under two hours; failures and interruptions are retained.
- Personal app data and defaults are untouched unless measured and reported.
- No commits or pushes unless Daniel asks.

## Status at the end of the round (4 November, evening)

Milestones A–C are done; results are in
[the task pilot record](../evidence/task-pilot-2026-11-04.md).

- B1–B5 and B7 are implemented, adversarially reviewed twice (nine defects
  found and fixed before the final build) and covered by focused tests;
  `./scripts/test.sh` passes.
- B6: only the step budget changed. Fresh installs and new agents now default
  to 40 model steps (stored agents keep their saved value), because three
  measured runs hit the 24-step limit and the passing todo-feature run on the
  release candidate needed 26. Temperature (0.2 vs Google's 1.0) and task-scoped
  tool sets remain unmeasured.
- Added from evidence during the round: hashless `move_file`; `edit_file` may
  name the model's own earlier revision; `plan_update` accepts the next
  revision number; standing instruction to script calculations over many
  values; per-conversation revision memory.

Later in the round (also done): budget status report (one tools-free final
turn when the step budget runs out), a second retry for empty/over-long
generations, host-run checks marked in receipts/Harness traces/UI, fixes for
verification interactions (workflow nodes, MCP tools, commands that never
ran, deadlines during the host check), and calculation guidance that respects
"don't create files" (`python3 -c`).

Next, in order of evidence (detail and run references in
[the trajectory deep dive](../evidence/task-pilot-2026-11-04-trajectory-deep-dive.md)):
1. Measure with at least three repetitions per task and model, and run the
   sealed held-out pool (`scripts/pilot/tasks-heldout/`) once on the final
   build for a fresh generalization number. Do not tune against it.
2. Enforce whole-workspace "don't change files" intent in the host (reject
   writes with a pointer to read-only commands) and keep user constraints
   verbatim in compaction summaries.
3. A degeneracy guard for repeated blocks in thinking and tool arguments,
   retrying with a targeted nudge instead of discarding a full 8K generation.
4. Compaction policy: do not count it against the step budget, trigger on
   measured headroom, keep constraints and edited-file state.
5. Put retry feedback at the end of the prompt so the prefix cache survives.
6. `edit_file` robustness: first differing line on a miss, "already applied"
   detection, short applied-diff in the result.
7. Temperature 1.0 and a coding-scoped tool set remain unmeasured.
8. Harness² refinement on the diagnostics path (needs the Claude
   author-identity extension).


## Follow-up round — 5 November

See [the follow-up work record](TASK_RELIABILITY_ROUND_2026-11-05.md) for
read-only intent enforcement, retry feedback placement, edit diagnostics,
existing-directory no-ops, and paired development measurements. The original
historical results above remain unchanged. Compaction budget separation,
stream repetition guards, batch operations and broader qualification remain
open.
