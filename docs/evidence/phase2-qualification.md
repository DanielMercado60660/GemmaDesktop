# Capability and Harness² qualification

28 October 2026. **The phase is delivered as local release 0.2.1. The 72-trial campaign is complete; promotion was rejected and the baseline is retained.**
This report separates implemented software, deterministic/host integration checks,
fresh model outcomes and optimizer evidence. It supersedes old statements that
capability profiles, complete skill bundles, Docker or coding Harness² are absent.

## Preserved starting point

The original working tree and release were preserved before edits, including
uncommitted files. See [the accepted phase record](../implementation/PHASE2.md)
for archive hashes and ownership. The previous successful realistic coding result
was a guided multi-turn result; this phase measures fresh task attempts.

## Infrastructure evidence

| Area | Evidence and boundary |
|---|---|
| Profiles, skills, context, plans, delegation | Host-authority and recovery race tests; [independent review](phase2-capability-independent-review-summary.json). Imports preserve complete immutable resource bytes. |
| OAuth | Controlled SDK-discovery/fallback, callback/state/issuer, cancellation, refresh, restart and uncertain-action tests; [real temporary Keychain and browser fixtures](phase2-native-integrations.jsonl). No external OAuth provider has been qualified. |
| Docker execution | [Final real-host suite](phase2-final-execution.jsonl): 68 top-level tests / 101 including subtests passed, with Seatbelt, Docker, public locked dependencies and PTY checks. |
| Toolchain setup | [Explicit setup helper execution](phase2-toolchain-setup.txt) verified pinned public inputs and built an inspected Linux/arm64 image. It did not change the selected campaign image. |
| Disposable resources | [Cache-release review](phase2-cache-release-summary.json), durable solver/grader cleanup, recovery and ordinary-cache/source/journal preservation. Ordinary project caches are retained. |
| Fixtures | [All 18 reference projects](harness-phase2-final-fixtures-and-cleanup-tests.txt) passed isolated checks, disposable-cache assertions and output-hash preservation. Three projects are larger multi-file features; fifteen are narrower contract regressions. |
| Live recovery | [Actual packaged-app restart](phase2-harness-live-restart.json) preserved the four native trials, frozen revisions, packet hashes and waiting reflection state. No inference or tool action replayed; disposable cleanup reported zero pending/failed entries. |
| Activation and rollback wiring | [Real application binding tests](phase2-harness-rollback-integration/manifest.json): two race tests passed through native packets, paired gates, independent review, app dispatch and SQLite reopening. They prove restoration, target isolation, idempotence and stale rollback rejection using synthetic solver/grader work; no production candidate was promoted. |
| Native/upstream method | [Actual pinned-core order parity](harness-phase2-order-parity-tests.txt), [failed-stage accounting](harness-phase2-order-and-failure-tests.txt), [adapter integrity](harness-phase2-adapter-order-tests.txt). Controlled tests establish four solver calls and three improvement stages for k=1. |
| Harness instruction transport | [Actual solver wire fixture](phase2-harness-instruction-wire-review.json) confirms revision instructions, tool guidance, lessons and final-verification guidance in both outgoing fake-Ollama system messages around a real file callback. This does not establish historical live payload contents or model compliance. |
| Native command argument transport | [Read-only wire fixture](phase2-harness-readonly-wire-review.json) captures the actual Harness schema and proves an explicit `readOnly:true` survives native Ollama/ADK transport, host authorization, the typed executor request, receipt and next tool message. The property is optional and defaults false. The recording executor does not establish real Docker enforcement or historical model compliance. |
| Read-only verification feasibility | The [18-reference test](../../internal/app/harness_integration_test.go) executes every exact prescribed public command against read-only source before separate grading; [all 18 passed](harness-phase2-final-fixtures-and-cleanup-tests.txt). TypeScript compilation and SQLite test databases use temporary directories. The frozen solver and grader use the same default private paths; `dist`/`build` are not silently writable. This direct-executor proof is distinct from live model compliance, and its log alone does not attest an image digest. |
| Lightweight evidence access | [SQL metadata projection tests](harness-phase2-metadata-projection-tests.txt) confirm list payloads are removed before Go decoding; full details remain available on demand. |
| Interface and package | Frozen local 0.2.0 app, signature verified; [build identities](phase2-release-build-info.json) and [archive checksum](phase2-release-package.sha256). [Live skill/delegate/plan demonstration](phase2-native-capability-live.json) passed, with one rejected receipt reference corrected by Gemma. Native [plan](phase2-native-plan-final.png), [context](phase2-native-context-final.png), and [video answer](phase2-native-video-answer-final.png) inspected. |
| Frozen-package regression suites | [Backend race suite](phase2-release-final-backend.jsonl): 242 top-level / 470 including subtests passed, 27 opt-in skips. [Frontend](phase2-release-frontend-tests.txt): 61 tests passed. [Scripts](phase2-release-script-tests.txt): 33 tests passed; [upstream adapter](phase2-release-adapter-tests.txt): 4 tests passed. |
| Correction-source regression suites | [Combined current-source evidence](phase2-correction-qualification-summary.json): 265 top-level / 529 including subtests passed with race detection, 29 explicit opt-in skips; 65 frontend, 33 script and 4 adapter tests passed. These results are separate from the frozen campaign binary. |
| Actual Docker connection loss | [Real-container acceptance](phase2-correction-docker-acceptance.jsonl) passed: the test-owned Unix proxy loses its listener and active connections, the guest survives, a restarted executor blocks new commands, recovery stops the owned guest/descendants, discards private edits and resumes. Docker Desktop itself was not stopped; no VM-crash claim. |
| Literal builds and multiprocessing | The same [host acceptance log](phase2-correction-docker-acceptance.jsonl) proves a literal offline npm build, installed TypeScript compilation, a worker thread, guest-loopback HTTP, two spawn-based Python workers, SQLite WAL writes and a fresh subprocess reopen. Original source stayed read-only and unchanged; owned guests were removed. |
| Current read-only reference proof | [All 18 reference fixtures](phase2-correction-reference-fixtures.jsonl) passed again in 71.583 seconds, with the exact immutable image and effective private paths logged. This is deterministic infrastructure proof, not 18 Gemma successes. |
| Video regression | Frozen 0.2.0 [pipeline](phase2-video-regression.json): 6/6 retrieval intervals and seven completed answers; native citation seeking to 21 seconds and playback to 30 seconds were observed ([final frame](phase2-native-video-playback-final.png)). The separate [0.2.1 run](phase2-correction-video-regression.json) retained 6/6 interval hits and seven completed answers in 40.382 seconds. [Independent review](phase2-correction-operational-independent-review.json) confirmed five clear positive answers, an ambiguous yellow flower/star silhouette and appropriate scoped dog abstention; seven completed answers is not seven unambiguous semantic passes. |
| Correction-package live accounting | [Two-generation native-tool smoke](phase2-correction-live-accounting.json) reconciles 1,000 prompt and 37 output tokens, two measured terminal responses and zero unknown contributions. One actual authorized file read returned the correct unchanged marker. The [independent persisted-record review](phase2-correction-operational-independent-review.json) found no contradiction. This bounded smoke is not a new coding campaign or a 64K capacity measurement. |
| Populated native Harness views | Release 0.2.1 shows [all 72 trials and unknown historical costs](phase2-native-correction-campaign-final.png), [two provisional lessons](phase2-native-correction-lessons.png) and [four immutable revisions](phase2-native-correction-revisions.png). [Independent visual review](phase2-native-final-workbench-review.json) found no blocking defect. Inspection used a separate copy of the campaign database; no activation, lesson decision or inference was performed. |

Host integration tests do not certify every possible sandbox escape or all remote
provider behavior. Storage reports are bounded non-atomic logical-byte scans. The
2 GiB/100,000-entry monitor permits interval overshoot and is not a hard filesystem
quota; image and Docker VM allocation are excluded. Publication is journaled
per-file recovery, not an atomic transaction against unrelated editors.

## Fresh coding pilot

Primary conditions: installed `gemma4:26b-mlx`, requested 65,536 context, native tools,
thinking on, temperature 1.0, 16,384 output tokens, 80 model steps and 1,200 seconds
per trial. Exact model/runtime, capability, fixture, grader, binary and image
identities are in the [frozen pilot report](phase2-live-pilot-report.json).
Personal settings were not changed.

| Task | Solver time | Tool calls | Initial independent grade |
|---|---:|---:|---|
| Python transactional ledger service | 220.527 s | 16 | Failed final read-only verification receipt; all other recorded checks passed |
| TypeScript inventory worker service | 236.812 s | 18 | Failed final read-only verification receipt; all other recorded checks passed |
| Go inventory HTTP service | 286.556 s | 22 | Failed final read-only verification receipt; all other recorded checks passed |

The exact prescribed commands did execute successfully after edits, but all three
omitted `readOnly:true`. Their zero exit codes do not satisfy the protected final
verification contract. The grades remain failed.

[Independent source review](phase2-live-pilot-independent-review.json) then found
three public-contract issues missed by those checks. [Supplemental Docker probes](phase2-live-pilot-public-observations.json)
confirmed Python's wrong exception for a non-object event, TypeScript's special
object-key failures, and Go's acceptance of unmatched trailing JSON delimiters.
The original candidate files and grades were not rewritten. Added regression
tests were meaningful but incomplete; Go had no added HTTP regression.

An earlier pilot attempt exposed a missing app-owned before-image directory for
Harness file edits. That infrastructure defect was repaired and regression-tested.
Its failed Python and interrupted Node attempts remain in
`.runtime/phase2-live-20261027a`, with restart evidence retaining the interruption.
The fresh three-stack run above is `.runtime/phase2-live-20261027b`; it is not a
continuation or replacement of the earlier results.

## Optimizer qualification

Both matched k=1 cycles are complete on development task `python-ledger`: four
fresh solver rollouts and three improvement stages per method. The actual pinned
upstream Python core ran through the Go-owned adapter. The [eight recorded rollouts](phase2-matched-development-rollouts.json)
retain the native baseline's generation-limit failure and seven completed solver
attempts. Every completed attempt ran its prescribed public command successfully,
but omitted explicit read-only verification. Independent public probes found
remaining validation gaps; completion is not a successful independent task grade.

The [matched admission and proposal record](phase2-matched-admission-and-proposals.json)
preserves identical baseline, INV, DOM and composed guidance across methods, with
method-specific notes based on actual trajectories. Both selected revision
`e3ebe7dc737b5cbd8a0fc850ae288a3aa64be67b76e4225a7f78203224e7510b`.
The native and upstream evidence exports have stable payload identities
([native](phase2-native-export-integrity.json), [upstream](phase2-upstream-export-integrity.json)).
The upstream independent reflection marked both changed components confounded
and retained no lesson. Native lessons remain provisional. These single-draw
development comparisons establish integration compatibility, not a causal method
advantage or a reliable improvement.

The native candidate was preselected before held-out exposure. Evaluation
`68381c81596bc2c357d5f0ade2e2063e` ran from 01:05:30 to 07:43:26 UTC on 28 October,
with the candidate frozen before all 72 validation/held-out trials: twelve tasks,
two arms and three repeats. Every scheduled attempt is retained; 62 solver runs
completed and ten failed, including generation-limit and deadline outcomes. No
failed attempt was replaced. The [sanitized result](phase2-paired-evaluation-final-summary.json)
records complete denominators and task-level counts.

| Partition | Baseline automated passes | Candidate automated passes |
|---|---:|---:|
| Validation | 0 / 18 | 2 / 18 |
| Held-out | 0 / 18 | 1 / 18 |
| All paired attempts | 0 / 36 | 3 / 36 |

The [independent review](phase2-independent-promotion-review.json) separates the
automated outcomes into 54 verification-only failures, ten incomplete attempts,
five protected-input failures and three passes. These buckets sum to 72; they do
not imply that an automated verification-only failure is free of untested bugs.
Overlapping check counts include nine additional semantic/execution/discovery
failures. The independent rejection was imported into the application's durable
review record; no lesson or activation was created from qualification evidence.

Five attempts changed protected original tests: one baseline and four candidate
attempts. Independent public-source review found that these changes retained or
strengthened the original assertions; they still violate the frozen preservation
contract. They are not evidence of a host sandbox escape. Reviews cover every
[Go](phase2-go-paired-source-review.summary.json),
[Node](phase2-node-paired-source-review.json), and
[Python](phase2-python-paired-source-review.summary.json) attempt, with immutable
batch records and rechecked file hashes. Most completed repairs have useful tests,
but missing or stale read-only verification, discovery gaps and ineffective
assertions remain distinct failure modes.

The three automated passes have actual fresh read-only verification. However,
[seven supplemental isolated probes](phase2-supplemental-public-probes/observations.v3.json)
confirmed public-contract counterexamples in the targeted Go/Node sources,
including both Node automated passes. Ordinary controls passed; originals and
prepared source hashes were unchanged; all owned probe containers were removed.
These observations supplement independent review and do not rewrite frozen grades.
The Python passing attempt had no static public-contract blocker identified;
that bounded review is not an exhaustive proof.

The saved gate is ineligible. Its historical observed medians and ratios are
retained, but the old binary did not prove complete terminal-token accounting.
They cannot establish the required cost gate. The correction explicitly treats
legacy accounting as unknown. No candidate activation or production rollback
occurred. Hidden assertions and expected answers remain excluded from proposals
and lesson packets. The [operator guide](../implementation/HARNESS_CAMPAIGN.md)
records the sequence and one-use held-out reservation; this exposed pool must not
be relabeled unseen for another optimization campaign.

The [final export](phase2-native-final-export-integrity.json), checked by a
[separate reviewer](phase2-native-final-export-independent-review.json), includes 76 native
trials (four development rollouts plus 72 paired attempts), five packets and two
provisional lessons. All 448 decoded files match their byte hashes. Repeated
exports have the same payload identity, the immutable artifact is read-only, and
the upstream export identity remains unchanged. Trusted grader diagnostics and
assertion payloads are excluded; candidate artifact bytes are preserved exactly.

Promotion requires every critical integrity check, no per-held-out-task decline,
strict aggregate held-out improvement, median elapsed time and tokens within
20 percent of baseline, and independent review. Keeping the baseline is a valid
outcome. Controlled parity is not reproduction of Google's published benchmarks.

## Delivery and remaining boundaries

Milestones A–D, the three-stack pilot, both optimization paths, all 72 paired
attempts, independent rejection, final exports, native demonstrations and local
correction package are complete. The app and archive are available at
[`dist/Gemma Desktop.app`](../../dist/Gemma%20Desktop.app) and
[`dist/GemmaDesktop-local.zip`](../../dist/GemmaDesktop-local.zip).
The local signature is verified; public notarization was not requested.

| Identity | Frozen campaign | Delivered correction |
|---|---|---|
| Version / build | 0.2.0 / 2 | 0.2.1 / 3 |
| Binary SHA-256 | `42bd293e1ac85940b3f9948618a49b6e32b584478852d8786371f1367466e31d` | `ebd1ba6e8bf39f81e655986cb0da5a5a71bb78c58e5189b2a1069fc4a47a4521` |
| Archive SHA-256 | `68bf6554c1fd25aa576f3b20845e126a7056b3cd81813cfcd69f5c73295045e0` | `4276b3277511a04636cc6fde5c4cd54216a756c918ff45243b48bed2452b0359` |
| Build record | [Frozen inputs](phase2-release-build-info.json) | [Correction inputs](phase2-correction-build-info.json) |

The old package is preserved in the campaign's `qualification-build/` directory.
Current source checks and smoke results do not retroactively qualify its costs or
change any trial. A future improvement campaign needs fresh unseen tasks and
complete accounting; this phase establishes the machinery and records why the
tested candidate was not accepted. Current operational documentation is in
[Capability operations](../implementation/CAPABILITY_OPERATIONS.md),
[Harness campaign](../implementation/HARNESS_CAMPAIGN.md), and the
[reference audit](../research/phase2-reference-audit-2026-10-27.md).

Deferred by agreement: native Xcode execution, Apple-container backend, persistent
previews, remote A2A, executable optimization plugins, expanded audio/video
understanding and public notarized distribution. External OAuth providers,
dynamic client registration and provider-side token revocation are not qualified.
The native card currently labels evaluation execution Completed and shows the
failed gate reason; an explicit independent-review verdict badge remains a
nonblocking clarity improvement.

## Post-freeze corrections

The live campaign exposed stale parent experiment labels during native and
upstream probes. Actual trial progress, backend job ownership and replay guards
remained correct. A [combined source-only status correction](harness-combined-status-followup-manifest.json)
now distinguishes active work from unanswered packets and prevents late worker
status updates from overwriting cancellation. Seven targeted tests passed across
three race-suite repetitions; the native regression failed before its fix. The
campaign finished on its original immutable binary. This correction is included
in the separately built 0.2.1 package above.

An [independent accounting audit](phase2-metric-completeness-review.json) found
that interrupted generations can leave positive but incomplete token totals in
the frozen binary. Those totals remain useful observations, but cannot support
complete-cost promotion claims. The [version 2 accounting correction](phase2-metric-accounting-followup-manifest.v2.json)
tracks model attempts and terminal receipts, includes retries and nested work,
and rejects promotion when costs are partial or unknown. It still permits an
honest negative independent review of unchanged evidence. Legacy records are not
retroactively declared complete. The interface hides unsupported cost ratios
and activation controls; the accounting journal remains available in context
inspection without flooding the chat or graph.

A separate [observation correction](phase2-harness-observation-metadata/manifest.json)
preserves bounded command provenance and verification fields when a large tool
response is clipped for Harness storage. The frozen binary discarded that saved
metadata after 32 KiB; the model received the original result separately. The
correction does not alter old observations or their grades. Synthetic receipt
and wire tests exercise the distinction between a successful read-only final
verification and a writable or failed command.

The accounting, observation and cost-interface corrections passed an
[independent source review](phase2-accounting-independent-source-review.json):
21 targeted backend tests (51 including subtests) and nine frontend tests passed,
with no blocking defect found. The initial sandbox denial of local fixture
listeners and the successful authorized rerun are both retained. These changes
are present in release 0.2.1, with [combined qualification](phase2-correction-qualification-summary.json),
separate live accounting/video checks and native inspection. They are absent from
the preserved 0.2.0 campaign package. All owned test servers, native verification
instances and disposable containers were stopped; the user's Ollama and Docker
services and personal settings were preserved.
