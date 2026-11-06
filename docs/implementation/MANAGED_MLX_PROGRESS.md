# Managed MLX and coding reliability implementation

> Historical snapshot — 4 November 2026. The implementation, test counts, runtime identities and delivery statements below describe that dated checkpoint. The current calculation/delivery work, opt-in campaign outcome and publication verification are recorded in [the November publication follow-up](RELIABILITY_PUBLICATION_2026-11-05.md).

Accepted 4 November 2026. Scope: the user-approved managed MLX, desktop reliability,
host verification, backend conformance, and bounded Harness² implementation plan.
Correctness is the primary model-selection metric. Existing defaults stay active
until independent qualification. Live inference batches stop for review within
two hours; neither checkpointing nor interrupted effects imply success.

## Work ledger

| Milestone | Status | Evidence / remaining work |
| --- | --- | --- |
| Regression foundation and desktop correctness | Implemented; native smoke passed | Revision-safe workflow save/run, private draft CAS/tombstones, shared readiness, keyboard tabs/status announcements; 129 frontend tests passed after the native-preview correction. macOS source CI added. |
| Host-owned verification | Implemented; live coding comparison pending | Immutable optional contracts, governed `verify_task`, source digests, protected publication, independent discovered-test red/green grading; legacy grading remains versioned. |
| Completion backends | Implemented; one 4K oMLX/E4B protocol smoke passed | Ollama completion seam, native oMLX/mlx-serve SSE, reasoning/tool identity checks, explicit capability provenance, shared admission, frozen runtime/model metadata. Later host cleanup hardening has offline evidence only; other pairs and coding quality remain pending. |
| Managed runtime installation | Implemented; first live oMLX/E4B smoke passed | Real patched oMLX/private CPython and native mlx-serve bundles built, immutable model checksums, durable jobs/recovery, owned authenticated servers, explicit activation/rollback. |
| Context and measured performance | Implemented; component measurements recorded | Indexed/scoped queries, bounded message pages, delta and live-state cursors, bounded UI caches, visibility polling, display coalescing, separately frozen experimental mechanisms. Scoped guidance and focused continuation checks passed. Native rendering and model-performance comparisons remain pending. |
| Versioned diagnostics and qualification | Implemented; real campaign pending | Versioned development/qualification packs, separate frozen arms, persisted schedules/checkpoints, ancestry/exposure rules, comparative gate/review, atomic profile activation/rollback. |
| Dependency upgrades | Implemented | ADK 2.7.1, Go/JS Wails 3.1.0, exact Harness² `6868817518876de8fd170c6a8354daca273b6191`; old historical identities retained. External Ollama remains 0.40.11. |
| Live model/backend comparisons | Pending | Conformance first, serving comparison, 36-attempt model screen, targeted ablations. |
| Harness² refinement and fresh qualification | Pending | Multi-task k=1 development, independent review, 72-attempt fresh qualification. |
| Packaged native delivery | Local build and native smoke passed | Real bundled mlx-serve installation completed through the native UI in isolated storage; Models, Settings, Harness tabs, restart persistence and Quit were checked. Corrected runtime/model previews show exact destination and host-calculated disk requirements. This is a local experimental package, not a coding-qualified or notarized release. |

## Compatibility invariants

- Preserve historical evaluation identities and v1 gate calculations.
- Keep video/embeddings on Ollama and one shared inference lease.
- Keep approvals, execution policy, tool receipts and grading host-owned.
- Every experiment freezes code, runtime, model/template/quantization, tools,
  executor, task/grader identities, controls, ordering and cache conditions.
- Missing performance measurements remain unavailable, never fabricated zeros.
- New runtimes and agent mechanisms remain experimental until qualified.
- No public publishing, model training, automatic model routing or k>1 in scope.

## Validation record

The exploration checks describe the pre-change baseline, not this build.

- Integrated `go test -race ./internal/...` passed on 4 November, including the
  final runtime identity, context and grader integration. The subsequent native
  smoke exposed one installation-preview gap; its passing targeted checks and
  final rebuild are recorded separately below.
- Host fixture-oracle validation passed all 18 v2 tasks: 17 reference repairs,
  17 required red/green tests, 17 test-only negative controls, and one read-only
  task. This is fixture/grader evidence, not model success.
- Exact upstream workflow tests and real stdio adapter/parity/checkpoint tests
  passed. No live Harness² optimization claim follows from these fixtures.
- E4B model acquisition and the first bounded managed oMLX probe used isolated
  `.runtime/managed-qualification` storage. Personal defaults are unchanged. The first probe checkpoint passed on 4 November at 23:01 UTC, with the owned server stopped. No further inference ran.

## Measured component performance

Apple M4 Pro, 64 GiB; Go benchmarks on local SQLite with FULL synchronous WAL.
These synthetic measurements exclude model inference and native WebKit rendering.
Five iterations per history case; 1,000 conversations × ten 2-KiB messages.

| Operation | Prior implementation | New implementation |
| --- | ---: | ---: |
| Selected conversation query | 119.06 ms; 73.67 MB allocated | 0.113 ms; 74.1 KB allocated |
| Bounded conversation page | — | 0.120 ms; 78.6 KB allocated |
| 2,048 immediate display deltas plus tool boundary | 2,049 durable writes | 5 durable writes |
| Display burst benchmark (five iterations) | 212.65 ms; 3.00 MB allocated | 0.701 ms; 0.120 MB allocated |

The burst test has no inter-token delays: realistic paced output also flushes at
100 ms. Wall time varies with disk contention; write count, exact text ordering,
authority-event flushes and bounded memory are separately tested. Tool receipts,
approvals and checkpoints retain synchronous durable writes. Reproduce with:

```sh
source scripts/env.sh
go test ./internal/store -run '^$' -bench BenchmarkSelectedSessionHistory -benchtime=5x -benchmem
go test ./internal/app -run '^$' -bench BenchmarkDurableDisplayStream -benchtime=5x -benchmem
```

## Experimental controls

`agent.harnessMechanisms` independently freezes `toolSet` (all/inspect/coding/
research), repeated-failure feedback, stream repetition guard, exploration
reminders, resident-read reuse, duplicate-result projection and deferred tool
discovery. All remain disabled by default. Verification feedback is a separate
boolean. Read reuse revalidates the host read and reduces model-facing bytes;
it does not claim to avoid disk I/O. Results remain complete in the journal,
with receipt references in projections, and reuse ends after context eviction
or any possible mutation. Prose-loop detection deliberately excludes fenced
code, JSON, tables and short repetitive lines.

No new default or broad coding-reliability claim is authorized by this ledger.
The 36-attempt screen, mechanism comparisons, Harness² refinement and 72-attempt
qualification require their actual scheduled runs and explicit batch continuation.

## Final source-check milestone

- Final standard command `./scripts/test.sh`: all Go race packages, 39 Python
  build/runtime suites, seven Harness adapter tests, 65 Node helper tests and
  61 rendered/helper Vitest tests passed. TypeScript and frontend build passed.
- Separate exact-pinned upstream workflow suite: seven tests passed.
- Production Docker grader: all 18 v2 reference tasks and two negative controls
  passed using image
  `sha256:09c68348396bd61f72b3570279e009741490473e40f8cc70517531696767578c`.
  This runs real verification/discovery/red-green grading without any LLM. It
  caught and led to fixes for the grading-directory allowlist and one exception-
  producing regression reference; grading was not weakened.
- V2 runs now freeze `cold_each_attempt`, prepare/release runtimes inside the
  attempt envelope, preserve preparation/release failures as infrastructure,
  and release before grading. Metadata freezes do not launch managed servers.
  Inactive qualified runtime versions can be compared without changing the
  selected working version or rollback pointer.
- Focused continuations preserve exact prior user requests and durable receipt
  references, retain the verification contract, and never resume effects. Their
  composer mode persists with draft revisions. Scoped guidance is resurfaced
  after compaction before affected mutations.

The rendered history benchmark used 10,000 messages, 12 unchanged polls and 40
visible messages. Synthetic JSON response bytes were 38,925,540 before and 3,588
after; three sample median processing times were 12.28–13.22 ms before and
0.076–0.158 ms after. jsdom timings exclude native IPC/layout/paint; current p95
has an outlier, so this is not an FPS or startup claim. Raw data is in
`docs/evidence/history-render-benchmark-2026-11-04.json`.

Live evidence: `docs/evidence/managed-mlx-e4b-2026-11-04.json`. Only patched oMLX
and the pinned E4B artifact at 4,096 context passed protocol-v2 smoke. Coding
quality, 65,536-context support, mlx-serve, 12B and 26B remain unqualified. The
next inference batch requires explicit continuation under the accepted plan.

## Local delivery and review checkpoint

- Final native build: `2026-11-04T23:17:11.744999+00:00`; executable SHA-256
  `d62a838f750a16a86b6a73968a5f769c32d4da95ede7ed689753aaed51b11cfd`.
  Build provenance verified unchanged inputs and both packaged runtime archives.
- After the native-preview fix, all runtime/app Go race tests passed and the
  frontend passed 129 tests (65 Node + 64 Vitest), typecheck and production build.
  Installation previews now use the same host calculation as install preflight,
  with numeric temporary/reserve/free-space requirements and no-network wording
  for bundled archives. Preview failure and insufficient disk block confirmation.
- Native UI evidence is in `docs/evidence/native-mlx-smoke-2026-11-04.json`.
  The runtime-only install used fresh isolated app data on this development Mac.
  Its state survived restart; the corrected runtime and model previews were
  checked on the final build. No model download or inference started during the
  final UI checks. The app was quit after inspection.
- Reviewable local artifacts: `dist/Gemma Desktop.app`,
  `dist/GemmaDesktop-local.zip`, its `.sha256` sidecar, and `dist/build-info.json`.
  Signing is ad hoc; publication and notarization remain separate work.

The implementation is ready for the next explicit inference batch. First repeat
provider qualification on the frozen final host, then establish effective
65,536-context support before admitting any arm to the historical-settings screen.
The serving comparison, 36-attempt model screen, individual mechanism comparisons,
12-rollout Harness² refinement, independent repair review and 72-attempt fresh
qualification have not run. Warm-cache performance and a separately measured
Ollama update remain pending. No existing default was replaced and no broad
coding-reliability improvement is claimed from infrastructure tests.
