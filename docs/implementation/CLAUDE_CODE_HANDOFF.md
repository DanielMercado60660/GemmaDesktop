# Gemma Desktop handoff for Claude Code

Prepared for Daniel Mercado on 4 November 2026. Workspace:
`/Users/danielmercado/GemmaDesktop`.

The objective is for the **local Gemma agent to complete real tasks a user asks
for**, producing independently checked results through this desktop application.
Daniel wants another **Explore → Plan → Build** round. Successful infrastructure
tests, a working installer, model tool-call compatibility, or an agent saying
“done” do not satisfy that objective on their own.

The November 4 implementation substantially expanded the infrastructure but
stopped after its first live protocol checkpoint. **No actual coding task was
run against the new managed-MLX configuration.** The next round should reach an
actual task pilot early, using the existing Ollama path if qualifying another
server would delay it. Claude is the developer/reviewer; Gemma must remain the
task solver. A repair authored by Claude is not a Gemma success.

## What has actually worked

| Record | Observed model outcome | Meaning |
| --- | --- | --- |
| [Calculator release demo](../evidence/release-prep-20261029/demo-homebrew-passed.json) | Gemma 12B read files, encountered the failing tests, edited `calculator.py`, ran the command again, and passed three tests. | Real task completion on a very small problem; historical build. |
| [Fieldnote final coding turn](../evidence/realistic-followup-20261027-152802-723946c9.json) | Gemma 26B completed the multi-file SQLite/CSV task on coding turn seven. Fourteen project tests and nine independent acceptance cases passed, with fresh verification and protected inputs preserved. | Guided multi-turn recovery, not fresh one-shot reliability. Earlier failures remain part of its history. |
| [Additional Fieldnote checks](../evidence/public-search-final-result.json) and [source review](../evidence/native-final-source-review.json) | Six further search-contract cases passed, with candidate files unchanged. | Useful additional evidence; the original acceptance suite had missed defects. |
| [Historical paired campaign](../evidence/phase2-paired-evaluation-final-summary.json) | 72 actual 26B attempts: baseline 0/36 and candidate 3/36 automated passes; held-out 0/18 and 1/18. | [Independent review rejected promotion](../evidence/phase2-independent-promotion-review.json). Both Node automated passes had substantive defects under supplemental probes. Complete historical cost accounting is unknown. |
| [November 4 E4B probe](../evidence/managed-mlx-e4b-2026-11-04.json) | Patched oMLX/E4B passed a 4,096-context protocol smoke with synthetic tool continuation and cancellation. | Compatibility only. No coding result, 65,536-context proof, speed ranking, model screen or final qualification. Later host lifecycle hardening has offline evidence only. |

The historical campaign recorded 62 terminal “completed” statuses; that is not
62 correct solutions. Its three-stack live pilot also failed overall. Fifty-four
campaign attempts failed only the saved automated verification check, but that
does not imply their repairs were correct: independent review found additional
defects. The new host-owned `verify_task` was designed to address this recurring
verification problem. **Its effect on real model success still needs testing.**

## Read these first

1. [Current implementation ledger](MANAGED_MLX_PROGRESS.md): implemented changes,
   tests, measured component performance, and the outstanding live campaign.
2. [User reference index](../research/USER_REFERENCE_INDEX.md): original and later
   references, including projects absent from the most recent eight-link list.
3. [November 4 exploration](../research/exploration-2026-11-04.md) and
   [MLX and donor exploration](../research/mlx-donors-harness-round2-2026-11-04.md):
   source pins and hypotheses. Their pre-build statements are historical.
4. [Historical qualification](../evidence/phase2-qualification.md) and the actual
   run records above: learn from failures without relabeling them.
5. [Architecture](../architecture.md), [capability operations](CAPABILITY_OPERATIONS.md),
   [execution setup](../../internal/execution/README.md), and
   [runtime packaging](../../packaging/runtimes/README.md).

`README.md`, `STATUS.md`, and portions of `HARNESS_CAMPAIGN.md` describe the October
release. They are not the current managed-MLX status. In particular, the current
Harness² source pin is `6868817518876de8fd170c6a8354daca273b6191`, not the historical
`1cfe254e…`. Inspect owning source when the documents disagree.

## Current working state

- The November 4 implementation is **uncommitted** on top of `d8dbce0`. Preserve
  tracked changes and untracked files. A fresh GitHub clone does not contain this
  work. No reset, clean, or replacement checkout should discard it.
- The review package is `dist/GemmaDesktop-local.zip`; the app is
  `dist/Gemma Desktop.app`. The native smoke exercised executable SHA-256
  `d62a838f750a16a86b6a73968a5f769c32d4da95ede7ed689753aaed51b11cfd`.
  `dist/build-info.json` is authoritative; recheck drift before experiments.
- Go race tests, Python and adapter suites, 129 frontend tests, TypeScript and
  production builds passed. The production Docker grader passed 18 reference
  tasks and two negative controls. These are infrastructure results, not model
  task successes. Native runtime installation and corrected setup previews also
  passed [smoke checks](../evidence/native-mlx-smoke-2026-11-04.json).
- ADK is 2.7.1, Go/JS Wails is 3.1.0. The recorded external Ollama is 0.40.11;
  read the actual daemon/model identities before another run.
- Existing defaults were preserved. Experimental mechanisms are off by default.
  No new model/backend/profile has earned promotion. Signing is local ad hoc;
  public publication and notarization are separate.

### Available local runtime data

The historical installed Ollama models are `gemma4:12b-mlx` and
`gemma4:26b-mlx`. These already use MLX through Ollama; native MLX servers are
alternative serving configurations. Check complete artifacts and live availability.

The new native E4B pair is in this isolated private profile:

```text
.runtime/managed-qualification-20261104/
  probe-checkpoint.json
  managed-inference/state.json
  managed-inference/runtimes/omlx-4213c328-gemma1/
  managed-inference/models/gemma4-e4b-4bit/gemma4-e4b-4bit/
```

It contains the installed runtime, weights and receipts. Reuse it through the
manager rather than downloading weights again. Do not run two app/manager
instances against the same profile, delete its checkpoint, hand-edit qualification
receipts, or use its 4K receipt as authorization for 64K. A new authorized batch
must have its own durable record while retaining the prior checkpoint.

The separate `.runtime/native-mlx-smoke-20261104` profile contains installed
mlx-serve only, with no model. Bundles are in `dist/runtime-resources`. Packaged
mlx-serve is stable v26.11.1 at `ac00f921…`; research inspected later `5b6fae44…`.
It has not passed live model qualification. The 12B/26B managed model catalogues
exist, but this round did not acquire those weights.

## Explore

Keep the first investigation focused on the complete user-task path:

1. Inspect the dirty tree, build identity, available models, executor image and
   active settings without altering personal defaults.
2. Trace an ordinary request through `run.start`, ADK, completion, governed file
   tools/commands, `verify_task`, final receipts and independent grading.
3. Read a few historical failed trajectories. Classify the actual cause: bad
   repair, ineffective tests, missed verification, tool-schema friction, repeated
   edits, context loss, truncation, authority violation, or runtime failure.
4. Review the donor source relevant to that failure. Compare current source to
   the recorded pins before borrowing changes; avoid another broad feature pass.
5. Inspect only public development task material for solver/proposer work. Keep
   held-out assertions and expected repairs away from prompts and lessons.

Important implementation locations:

| Concern | Files |
| --- | --- |
| Ordinary tasks and receipts | `internal/app/runs.go`, `verification_contract.go`, `receipt.go`, `internal/tools/verification.go` |
| Agent loop and experiments | `internal/engine/engine.go`, `adapter.go`, `verification.go`, `mechanisms.go` |
| Runtime/model identity | `internal/app/completion.go`, `frozen_completion.go`, `internal/inference/`, `internal/runtimes/` |
| Real solver and grader | `internal/app/harness.go`, `harness_solver.go`, `harness_grader.go`, `harness_regression.go` |
| Versioned schedules and gates | `internal/app/harness_v2.go`, `harness_runtime.go`, `harness_promotion_v2.go`, `internal/harness/v2*.go` |
| Task packs | `internal/harness/packs/`, `internal/harness/packs.go` |
| Upstream Harness² | `scripts/harness/upstream_adapter.py`, `third_party/harness2/UPSTREAM.json` |
| Task-facing UI | `frontend/src/App.tsx`, `components/RunReceipt.tsx`, `components/RunVerification.tsx`, `components/ReliabilityWorkbench.tsx` |

## Plan

Present a short plan whose first milestone is **observed task execution**, with
the model, prompt, expected artifacts, independent checks, budget and stopping
conditions specified before the run. Do not require all three providers and all
models to be ready before testing one usable path.

Recommended first pilot:

1. Reproduce the small calculator repair on the current build as an end-to-end
   smoke, retaining the broken-source check and final test receipt. It is already
   exposed development material and cannot count as fresh held-out evidence.
2. Run one nontrivial development task through the production solver/grader with
   one attempt. Use an existing available Ollama model if appropriate. A concrete
   entry point is a `coding-diagnostics-v2` task such as `event-join`; verify its
   public contract and current API schema first.
3. Exercise a representative project request through the ordinary app path as
   well. A harness-only success does not establish that the composer, approvals,
   tools, verification and final artifact work together for a user.

Record first-attempt success separately from success after feedback. Preserve
every failed or interrupted attempt. For each task, distinguish model completion,
host verification, independent correctness, protected-input integrity and useful
delivery. If it fails, fix the demonstrated host/harness issue or compare one
prompt/mechanism change, then run a fresh comparable attempt.

Use immutable verification contracts and meaningful discovered regression tests
where appropriate. Never make a grade pass by weakening the expected result,
editing protected tests, accepting stale verification, or having Claude finish
the model's task. Read-only tasks should complete with a supported answer and
unchanged files; they should not be pushed into editing.

## Build and run

The production APIs already expose `harness.packs.list`, `harness.arms.create`,
`harness.diagnostics.create/advance/get/list/export/cancel` and versioned paired
qualification. Follow their actual schema in source and tests. A one-task pilot
can use one frozen arm, `repeats: 1`, and advance with `maxAttempts: 1`; these call
the real solver and grader. They are different from the reference-oracle tests.
Harness arm admission requires an explicit Docker capability profile and installed
digest-pinned executor image, including when the model runs through Ollama. The
restricted default executor is not sufficient for that diagnostic path.
Use ordinary `run.start` with a `verificationContract` for the product journey;
that verifies the declared command but does not automatically invoke independent
Harness grading. Independently check its frozen resulting files separately.

`scripts/e2e.py` and `scripts/realistic-e2e.py` contain useful historical live
drivers. **Do not run them blindly:** the calculator driver overwrites
`docs/evidence/live-coding-task.json`. Adapt output destinations to unique records,
validate current contracts and budgets, and preserve historical evidence first.
Use the supported Homebrew Python or governed Docker executor; the old Python.org
framework-path failure is an execution restriction, not proof of a model failure.

Run task-relevant checks after each change, then the standard suite and a packaged
native journey before delivery. `source scripts/env.sh` selects project Go tools;
`./scripts/test.sh` is the standard non-inference suite. Runtime packaging requires
the verified private build resources described in its README. Do not replace
them with empty placeholders to make a build pass.

### Claude authorship compatibility

Some proposal/review paths deliberately require `Author.Kind == "codex"`, including
custom content admission in `internal/app/harness_v2.go` and historical manager/
packet checks. Existing/base-arm diagnostics and ordinary task runs can be used
without claiming new Codex authorship.

Before Claude-authored Harness² proposals or reviews, plan a small versioned
author-identity extension that records the actual author/provider and preserves
independent review, hashes, local authority and historical v1 identities. Do not
submit Claude work under a fabricated Codex identity or disable provenance checks.

## Retained experiment rules

The latest request is for this handoff, **not permission to resume the stopped
inference batch**. Daniel's next kickoff/continuation governs subsequent work.
The accepted plan requires live batches of at most two hours and explicit
continuation after each checkpoint. Do not interpret elapsed time as approval.

Each attempt has at most 30 minutes including preparation, a 20-minute solver
limit, grading and cleanup. Start it only if its full allowance fits. Persist
schedule/order/cursor first, end healthy batches as `checkpointed`, release
runtime ownership, and never automatically replay interrupted effects. Reuse
completed attempts only after identity and output-hash validation.

The full comparison ladder remains available after the task pilot:

| Stage | Planned design |
| --- | --- |
| Provider admission | Offline fixtures plus real cancellation, context, tool-cycle and accounting probes on exact artifacts |
| Serving comparison | Hold 26B fixed where artifacts match; cold/warm tool loops and two coding tasks with two repeats; label quantization/weight differences |
| Model screen | Six development tasks × E4B/12B/26B × two repeats = 36 attempts in 12 balanced batches |
| Mechanism comparison | Baseline versus one mechanism, six tasks × two repeats = 24 attempts; keep read-only performance separate |
| Harness² refinement | Three development tasks, upstream k=1, four solver rollouts per task = 12 rollouts; compose and independently review lessons |
| Final qualification | Six validation plus six fresh held-out tasks × two frozen configurations × three repeats = 72 attempts |

The controlled screen freezes 65,536 context, 16,384 output tokens, thinking on,
temperature 1, 80 steps and 1,200 solver seconds. Prove effective support before
admission. A deliberately smaller-context smoke is a separate experiment, not a
substitute for this screen. Give E4B its own focused-tool/verification-feedback
comparisons if another model wins the initial screen.

Freeze both arms, shared host/grader, order and cache policy across checkpoints.
Preserve semantic task ancestry; exposed historical tasks do not become fresh
when renamed. Keep detailed held-out feedback withheld until qualification ends.

Promotion requires a complete trustworthy evaluation with complete terminal usage
accounting, strictly higher aggregate held-out success, no per-task held-out
regression, zero candidate integrity violations, no host-authority regression, and
independent repair/test review. The current v2 implementation additionally vetoes
per-task validation regressions; inspect `internal/harness/v2_gate.go` when
reconciling the accepted policy with implementation.
Time and tokens remain visible; the old 1.2× limit is not the new cross-model
veto. Activate the complete profile atomically with rollback only after it passes.

ADK and Go retain execution authority. Video/embeddings remain on Ollama. Do not
silently change personal defaults or enable unmeasured mechanisms. Training,
automatic routing, cross-provider workflows, Harness² k>1 and public notarized
distribution remain outside this round.

## What Daniel should receive next

The next substantive checkpoint should include actual user prompts, model/runtime
and settings, resulting artifacts/diffs, observed verification commands, independent
outcomes, all failed attempts, assistance supplied and the next evidence-driven
change. If the model still fails, say precisely why. A larger passing infrastructure
test count by itself is not the intended deliverable.
