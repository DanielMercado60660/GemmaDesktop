# Capability and Harness² phase

28 October 2026 — implemented, qualified and delivered as local release 0.2.1. The candidate failed promotion gates; the baseline is retained. This document preserves the accepted contract and links its final evidence.

## Preserved baseline

Before implementation, 377 tracked and untracked source/evidence files were archived at `.runtime/phase2-baseline/20261027-172240/source.tar.gz` (SHA-256 `a417b366a70e2d0d1830917d4eb831f152f301cdfd8fcd2134e444f58259bee5`). The previous release was copied alongside it (SHA-256 `3448b0249979b09dffee015ce39c597af3abba5b8612c190a963526c230e4080`). Base Git commit: `93ff161798d6048815a9e6429189bf9184e00a56`. Existing uncommitted work is part of this baseline and must be preserved.

## Accepted implementation

1. **Capabilities:** versioned profiles selecting tools, immutable complete skill bundles, MCP tool selectors, execution profiles and read-only delegates. Enforce at host execution, freeze on admission, retain budgets/deadline through recovery. Add context provenance and bounded project guidance, durable plans, delegated child records. OAuth uses native browser/PKCE, bounded endpoints and Keychain; uncertain mutations are never replayed automatically.
2. **Coding:** Docker-first Node/TypeScript, Python and Go. File tools edit the original immediately. Each command receives a filtered private snapshot, runs offline with owned process lifecycle, and publishes permitted source changes before returning. Full input freshness checks, conflict handling, publication journal, private caches, protected paths and real read-only mounts are required. Start at one guest, four CPUs, 8 GiB and bounded processes/output/time. Separate approved dependency provisioning. Keep stdio MCP restricted.
3. **Experiments:** immutable specs, revisions, trials, lessons, evaluations and promotion records. Freeze inputs, model/runtime/configuration, capabilities/skills/context/workflow, executor/dependencies/grader and task order. Eighteen fixtures: six per stack, split into six development, six validation and six held-out tasks. Trusted deterministic checks stay outside solver inputs and execute candidate code only in isolation. Separate Codex review; never ask Gemma to grade itself.
4. **Harness²:** native Propose–Probe–Compose and pinned upstream Python core `1cfe254e8d25b15f37048c3730134686abe60335`, sharing the Go solver through a narrow application-owned stdio protocol. Begin k=1: four solver rollouts, three improvement stages. Codex packets are authored/imported through this chat, durable/cancellable/hash-validated/idempotent. No automatic provider connection. Editable harness surface is instructions, guidance, approved skill selections, scoped lessons and approved workflow/delegation configuration; never model weights, security, graders or arbitrary executable plugins.

## Qualification and promotion

Infrastructure fixtures precede a three-task live pilot (one per stack), followed by the larger campaign. Use installed 26B/64K/native tools with frozen generation controls and serialized inference. Preserve personal settings. Report fresh one-shot outcomes separately from guided repair.

Final candidates freeze before held-out evaluation. Paired baseline/candidate trials repeat three times per task. Retain all failures/incomplete runs. Activation is authorized only when critical integrity checks pass, no held-out task pass count declines, aggregate successes increase, median elapsed time and tokens are each at most 1.2 times baseline, and independent review finds no blocking issue. Activate only the named target profile, record previous revision and evidence, and support rollback. Keeping baseline is a valid experimental result.

Acceptance includes actual multi-process builds/tests, temporary HTTP tests, cancellation/daemon-loss/restart, credential/network isolation, source conflicts/recovery, immutable profiles and skill resources, context provenance, delegation budgets, OAuth callbacks/refresh/signout, packet identity/leakage/stage accounting/native-upstream parity, and existing app/video regression. External-provider OAuth compatibility is claimed only after exercising that provider. No Google benchmark replication claim.

## Ownership and integration

Three implementation lanes: capability/engine/MCP; executor/workspace/tools; experiment/app harness/Python/fixtures. Root owns application facade, frontend integration, documentation, packaging and final qualification. Shared interfaces are integrated rather than implementing duplicate runtime/permission authorities. Live inference is serialized by the integration owner.

Deferred: native Xcode executor, Apple-container backend, persistent previews, remote A2A, executable optimization plugins, expanded audio/video understanding and public notarization. Normal coding never requires a whole-task Apply Changes step. Retain current Wails/Go/ADK architecture and review dependency upgrades separately.

## Final evidence ledger

- Starting source and release preservation: complete; hashes above. Existing uncommitted work is retained.
- Milestones A–D: implemented and integrated. Frozen 0.2.0 ran the campaign; separately built 0.2.1 includes status recovery, complete/partial/unknown token accounting and bounded saved command provenance corrections.
- Current backend race suite: 265 top-level tests / 529 including subtests passed; 29 explicit opt-in skips. Frontend: 65 passed; scripts: 33; upstream adapter: 4. Earlier separate real-host executor suite: 68 top-level / 101 including subtests, no skips.
- Current real-Docker follow-up passed connection loss/recovery and literal offline npm build, worker thread, temporary HTTP, Python spawn workers, SQLite WAL and subprocess reopening. All 18 read-only reference fixtures passed with the pinned image logged. These are infrastructure checks, not model task successes.
- Fresh three-stack model pilot, independent review, live skill/delegation/linked-plan demonstration, video retrieval, native citation seeking and playback are complete. Current 0.2.1 accounting smoke reconciles two generations and 1,000/37 prompt/output tokens. Current video retrieval retained 6/6 interval hits; one flower/star answer remains visually ambiguous.
- Matched native and actual pinned-upstream optimization each completed four solver rollouts and three improvement stages. Native lessons remain provisional; upstream reflection retained no lesson.
- All 72 paired qualification attempts are retained: baseline 0/36 automated passes, candidate 3/36; held-out counts 0/18 versus 1/18. Ten solver attempts were incomplete; five changed protected original tests. Independent review covered all attempts and reproduced public-contract defects in both Node automated passes.
- Promotion was independently rejected and recorded. Historical complete costs are unknown; the correction hides unsupported ratios and blocks activation. No candidate was activated. Final immutable exports, populated native Optimize/Lessons/Revisions views, ad-hoc signature and local archive are complete.

See the [qualification report](../evidence/phase2-qualification.md),
[current check summary](../evidence/phase2-correction-qualification-summary.json),
[delivery coverage closure](../evidence/phase2-delivery-coverage-audit.md),
[capability operations](CAPABILITY_OPERATIONS.md) and [campaign guide](HARNESS_CAMPAIGN.md).

## Completed campaign and recovery record

The campaign is preserved in `.runtime/phase2-campaign-20261027`; its owned
loopback server is stopped. Native experiment `3c1f216827db7eef221ce7565c9936d6`
was preselected before exposure. Upstream experiment
`e3f1b402a830bf8d04efbf45f55ee3ae` is the completed matched compatibility run.
Both composed candidate
`e3ebe7dc737b5cbd8a0fc850ae288a3aa64be67b76e4225a7f78203224e7510b` from baseline
`3e5a9718673c383d4f588dc1bd6da095776c6ca812180c11734f52ccb8615a00`.

Evaluation `68381c81596bc2c357d5f0ade2e2063e` ran from 01:05:30 to 07:43:26 UTC
on 28 October. All 72 scheduled results exist; none was replaced. Independent
review packet `3626b735b6eb018dbbb8863015a5b864` was imported with a rejected
verdict. There are no promotion or active-binding records. The final native
export contains 76 trials including four development rollouts, five packets,
two provisional lessons and 448 byte-verified output files. Its payload hash is
`b87e272616fabbd88e0d0c9713f23a6e399cf6753084e066bbcbd7b8a04b0be3`.

Frozen campaign binary SHA-256:
`42bd293e1ac85940b3f9948618a49b6e32b584478852d8786371f1367466e31d`.
Its original archive and build evidence remain under `qualification-build/`.
Delivered 0.2.1 binary SHA-256:
`ebd1ba6e8bf39f81e655986cb0da5a5a71bb78c58e5189b2a1069fc4a47a4521`.
Delivered archive SHA-256:
`4276b3277511a04636cc6fde5c4cd54216a756c918ff45243b48bed2452b0359`.

Do not relabel the exposed held-out pool unseen, reconstruct missing historical
costs, rerun failed trials to erase outcomes or attribute current correction
checks to the frozen binary. A later campaign needs fresh reserved tasks and
complete accounting. The actual app activation/rollback chain was exercised
only with synthetic solver/grader fixtures, including persistence, target
isolation, idempotence and stale rollback rejection; no real candidate promotion
or rollback occurred.

The connection-loss check drops its own Unix proxy and existing connections
while an actual owned guest continues, then verifies blocked admission,
reconciliation, descendant cleanup and unpublished-edit discard. It did not
stop Docker Desktop or simulate a VM crash. Storage bounds remain monitored
logical bytes with possible overshoot. Public OAuth-provider interoperability
and the explicitly deferred capabilities above remain unqualified or deferred.

All owned test servers, native QA instances and disposable guests were stopped.
The user's Ollama/Docker services, installed models and personal app settings
were preserved. Native correction UI inspection used a separate copy of the
campaign database; its displayed 12B chat default did not alter the frozen 26B
experiments and no 12B qualification was performed in this phase.
