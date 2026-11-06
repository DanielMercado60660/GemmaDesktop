# Task reliability follow-up — 5 November 2026

Continue the [November 4 plan](TASK_RELIABILITY_PLAN_2026-11-04.md) in a bounded
round. Gemma remains the task solver; host code and independent graders must
not repair its candidate files. Earlier uncommitted changes remain in place.

## Scope

1. Enforce narrowly recognized whole-workspace no-change requests through the
   existing frozen read-only authority. Permit explicitly approved commands
   with the workspace forced read-only, so calculations can use inline code.
   Narrow protected-file instructions must still allow legitimate repairs.
2. Append ordinary empty/length retry feedback after the conversation without
   rewriting the system instruction or persisted history. Preserve resource
   bounds and complete tool-call/result pairs.
3. Improve edit diagnostics and successful result wording. Recognize a repeated
   successful edit only with same-conversation evidence at its exact current
   revision; include a bounded diff preview. Treat an existing real directory
   as a no-op, including an ancestor of a task-protected file. Proven no-op
   receipts must preserve verification state and never trigger repair of
   unrelated pre-existing failures.
4. Correct the development pilot's stale 24-step default to the app's current
   40-step default.

Existing code already restores compacted original user messages verbatim with
a strict size bound and chronological ordering. This round tests that mechanism
alongside retries instead of creating another user-message archive.

Compaction budget separation is deferred: `Budget.MaxModelCalls` is the shared
bound for ordinary and summary calls, with the existing single tools-free
terminal-report allowance. It applies across agents, delegates, workflows and
verification and needs a deliberate aggregate-budget policy before summary
calls can have separate limits. Stream repetition guards, batch file operations,
temperature changes, and Harness² tuning remain separate backlog items.

## Validation protocol

- Freeze the pre-change working source and hashes under
  `.runtime/reliability-20261105/baseline-source/`; compile it independently.
- Run source-level regression tests, including adversarial scope, revision,
  read-only command, continuation, and retry-history tests.
- Use isolated application data and disposable development fixtures on the
  installed `gemma4:12b-mlx` model. Explicitly use 40 steps, 32K context,
  temperature 0.2, 8,192 output tokens and a 600-second task deadline on both
  builds. Do not run concurrent inference batches.
- First repetition: `log-question`, `explain-failure`, `todo-feature`,
  `crash-from-log`, `wordfreq-repair`. Second and third repetitions focus on
  `log-question`, `explain-failure`, `wordfreq-repair`.
- Preserve failed and interrupted attempts, record binary/model identities,
  and report independent correctness separately from run completion and host
  verification. These are exposed development tasks, not qualification.
- Leave the sealed held-out task pool untouched during development.

## Status

Implementation, focused tests, native packaging, source provenance checks and
the bounded live pilot are complete; see the
[validation and evidence record](../evidence/task-pilot-2026-11-05.md).
The full Go suite retains four baseline-reproduced FFmpeg-version failures.
The baseline scored 2/5, exploratory v2 5/5, and final v3 4/5 independently
correct. All three final-v3 coding tasks also passed host verification; the
log-analysis task timed out without a final answer. These are separate single
development repetitions, not qualification or a causal improvement estimate.
Further repetitions are deferred; outcomes from different candidate revisions
are not pooled. All protected inputs were preserved and the final driver
exited cleanly.

Evaluation hygiene: no held-out task was run or edited. During diagnosis of a
separate FFprobe test failure, an overly broad text search accidentally displayed
fixture fragments from `slow-dedupe`, `inventory-reorder`, and `regex-extract`.
These fragments were not used to design or tune the implementation. Treat those
three cases as exposed to the developer; replace them before any strictly blind
qualification. Prior-session author/reviewer summaries also describe portions
of this pool, so a strictly blind developer evaluation should use freshly
authored tasks after the final build is frozen. Future searches exclude the
held-out directory explicitly.

## Remaining work

1. Improve delivery of a final answer after a correct computed result. The
   final-v3 log attempt repeated the baseline's deadline failure: long
   nonrepeating enumeration, then correct Python output, but no final answer.
   Test a narrowly bounded thinking-cycle guard separately. Offline replays
   identify short repeated blocks in other failed and eventually successful
   development traces; they do not prove recovery or runtime savings. A cycle
   guard would not address the nonrepeating log enumeration, and `read_file`
   schema mistakes also need separate treatment.
2. Define a shared budget policy before separating compaction calls from task
   steps. Exact original user-message restoration already exists; avoid
   duplicating that mechanism.
3. Run broader controlled repetitions and newly authored blind tasks after
   freezing the next candidate. Temperature, scoped tools, alternate models,
   managed MLX qualification and Harness² refinement remain unmeasured here.
4. Resolve the media subsystem's FFmpeg 8.1 requirement versus the installed
   9.0.2 toolchain in a separate compatibility change. Preserve the four
   reproduced baseline failures until supported behavior is established.

The subsequent [explore → plan → build cycle](TASK_RELIABILITY_ROUND2_2026-11-05.md)
adds bounded schema guidance, an opt-in thinking-cycle experiment and context
measurements. The results above remain the historical record for this round.
