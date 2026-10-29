# Operating a native and upstream Harness² campaign

27 October 2026. This is an operator guide to the implemented API, not a claim that the live model campaign or promotion has passed. The current three-stack pilot is separate evidence. Stop for an infrastructure defect; retain a model’s failed or incomplete baseline as a legitimate experimental outcome.

The application owns the solver, permissions, fixture copies, records and independent grading. Codex authors the proposal/composition packets in this chat. A distinct Codex reviewer inspects the evidence and submits reflection/promotion review. Gemma is only the solver. No provider account, Vertex service or model judge is needed by this adapter. The upstream lane executes the actual pinned core at `1cfe254e8d25b15f37048c3730134686abe60335`, using an application-owned declarative surface and Go solver; this does not reproduce Google’s benchmark results.

## 1. Freeze the campaign conditions

Use an isolated campaign profile/data directory and the **same built application binary** for admission, execution, restart and qualification. The test pilot’s Go test binary is a different binary from a packaged application; do not treat a pilot experiment as a resumable production campaign. Keep the old pilot reports, including failed/interrupted attempts.

Prepare a named agent and capability profile through the existing Agents/Capabilities controls. Keep personal defaults unchanged. The agreed campaign configuration is:

| Condition | Value / source |
|---|---|
| Solver | Installed `gemma4:26b-mlx`; exact digest captured at admission |
| Context request | `65536`, supplied to experiment admission |
| Thinking | `on` |
| Temperature | `1.0` |
| Response token limit | `16384`, including thinking where the runtime supports it |
| Tool protocol | `native` |
| Agent step limit | `80`; nested work and compaction share the run budget |
| Per-trial deadline | `1200` seconds |
| Execution profile | `docker`; no host fallback |
| Qualified toolchain image | `sha256:09c68348396bd61f72b3570279e009741490473e40f8cc70517531696767578c` |
| Authority | Explicit file/read/search/edit/command capability selection; no arbitrary plugin, browser, MCP or grader changes through packets |
| Runtime, binary, grader and input identities | Read the returned `spec`; never infer these from a model’s answer |

The current fixture collection contains 18 tasks: six per language and six per split. Three development tasks are multi-module feature projects: Python SQLite service, Node/TypeScript worker service, and Go HTTP API. The other 15 are narrower contract regression tasks. It is not 18 equally complex service projects.

The first pilot uses `python-ledger`, `node-inventory`, `go-inventory`. The current post-pilot operating plan is a matched **one-task `python-ledger` k=1 campaign per method**, then the preselected native candidate’s paired qualification; these are fresh campaign attempts, not reused pilot rollouts. A full development campaign can use all six development IDs, in an explicitly chosen order. `harness.fixtures.list` returns metadata and withholds held-out prompts; do not inspect embedded hidden case/probe/reference source while authoring proposals. Public requirements, solver-visible files and observed tool results are the allowed proposal evidence.

Admission captures existing active Harness instructions/skill/delegate selections as the baseline. Thus use the same target and binding state for a matched comparison; do not promote one method before admitting the other. Freeze lesson-head statuses for the comparison too: previously accepted compatible lessons can enter later packets, whereas another experiment’s provisional lessons do not. A packet’s own library snapshot is immutable once created, but `start` does not freeze a global lesson-library snapshot for every future packet.

## 2. Transport and read operations

These are the same method/parameter pairs used by the native UI. Prefer the existing app bridge/UI. An already-running isolated loopback development service accepts this envelope at `/api/call`:

```json
{"method":"harness.status","params":{}}
```

For an operator script, save the complete envelope to a JSON file, then POST it with `Content-Type: application/json` to the selected service. Do not open a second writer against the running pilot’s SQLite database. The packaged native application does not enable the development HTTP service.

Useful reads:

| Method | Parameters | Purpose |
|---|---|---|
| `harness.fixtures.list` | `{}` | Fixture metadata/split |
| `harness.experiments.list` | `{}` | Metadata summaries |
| `harness.experiments.get` | `{"id":"EXPERIMENT_ID"}` | Full frozen spec, state, candidate and IDs |
| `harness.packets.list` | `{}` | Find `awaiting_codex` packets; filter by experiment/task/stage |
| `harness.packets.get` | `{"id":"PACKET_ID"}` | Exact request hash, mission, context, evidence and lesson snapshot |
| `harness.trials.list` | `{}` | Find every attempt by experiment, including interrupted attempts |
| `harness.trials.get` | `{"id":"TRIAL_ID"}` | Output identity, observed tool trace, usage, error and bounded progress |
| `harness.revisions.get` | `{"id":"REVISION_ID"}` | Immutable candidate content and selection snapshot |
| `harness.evaluations.get` | `{"id":"EVALUATION_ID"}` | Every paired result, gate reasons and review packet |

Lists omit large bodies. Trial `progress` contains bounded activity type/title, tool count and update time; raw thinking is not stored there. A finished model sentence is not independent correctness evidence.

## 3. Admit without starting inference

```json
{
  "method": "harness.experiments.start",
  "params": {
    "workspaceId": "WORKSPACE_ID",
    "agentId": "CAMPAIGN_AGENT_ID",
    "mode": "native",
    "taskIds": ["python-ledger"],
    "contextSize": 65536,
    "budgetSeconds": 1200,
    "workflowIds": []
  }
}
```

Save the returned experiment ID, `spec.hash`, `baseRevisionId`, fixture digest, model/runtime identities and application identity. `start` performs metadata reads and freezes the specification; it does not run the solver. Agent generation settings come from the saved agent, not additional keys in this request. `authoredBy` is not supplied: baseline capture is attributed to the original agent, not falsely to Codex.

`workflowIds` optionally admits up to eight existing definitions. Automated experiments reject approval nodes. Packets may select only an admitted workflow and already-authorized read-only delegate IDs; neither selection grants new tools. Approved skills are immutable package IDs, not arbitrary filesystem paths.

## 4. Native k=1: four rollouts, three improvement stages

Start/continue with:

```json
{"method":"harness.experiments.advance","params":{"id":"EXPERIMENT_ID"}}
```

One completed development task follows this exact sequence:

| Step | Solver work | Required external response | Native continuation |
|---|---|---|---|
| Baseline | One `base` rollout | Both `proposal_inv` and `proposal_dom` packets | Submit both, then advance |
| Probe | One `inv` rollout, then one `dom` rollout | One `compose` packet | Submit it, then advance |
| Final probe | One `final` rollout | One independent `reflection` packet | Submit review, then advance |
| Next task / finish | Advance moves to next frozen task, or completes | None beyond the next task’s packets | Do not assume this advance is inference-free |

The three improvement stages are INV proposal, DOM proposal and composition. Reflection is an additional review, not a fourth improvement stage. Failed/incomplete solver attempts are retained rather than replaced with convenient successes.

Fetch each packet and inspect every evidence trial and referenced revision. Submit **complete** declarative content, not a patch. Copy the actual packet ID/hash; never invent them:

```json
{
  "method": "harness.packets.submit",
  "params": {
    "id": "PACKET_ID",
    "requestHash": "EXACT_RETURNED_REQUEST_HASH",
    "authoredBy": {"kind":"codex","identity":"ACTUAL_CODEX_PROPOSAL_AUTHOR"},
    "content": {
      "instructions": "COMPLETE_REVIEWED_INSTRUCTIONS",
      "toolGuidance": "COMPLETE_REVIEWED_TOOL_GUIDANCE",
      "lessons": [],
      "skillIds": [],
      "workflowId": "",
      "delegateAgentIds": []
    },
    "notes": "Describe the observed failure, proposed change, and its limits. Cite actual public evidence.",
    "evidence": ["TRIAL_ID_FROM_THIS_PACKET"]
  }
}
```

Use that shape for all three proposal/composition stages. Empty selections intentionally select none; preserve the baseline selections unless the proposal explicitly changes them. Unknown fields, permissions, unapproved IDs and stale hashes are rejected. The response is immutable; an identical retry is idempotent. Do not “retry” with different content under the same packet ID.

Independent reflection uses a different actual reviewer identity from **every proposal/composition author in this experiment**:

```json
{
  "method": "harness.packets.submit",
  "params": {
    "id": "REFLECTION_PACKET_ID",
    "requestHash": "EXACT_RETURNED_REQUEST_HASH",
    "authoredBy": {"kind":"codex","identity":"ACTUAL_INDEPENDENT_REVIEWER"},
    "verdict": "approved",
    "notes": "Review the four actual attempts, regression-test meaning, unresolved defects and evidence limits. Approval here is reflection, not promotion.",
    "evidence": ["EVIDENCE_ID_FROM_THIS_PACKET"],
    "lessons": []
  }
}
```

Use `rejected` when evidence does not support adoption. Identity fields are provenance declarations supplied by the operator, not automatic account authentication; record the real authors/reviewers and do not relabel the proposer as independent.

An optional lesson is `{ "cue":"…", "guidance":"…", "evidence":["ALLOWED_ID"], "scope":"stack", "contradictions":[] }`. Scope can be `stack` or `workspace`. Accepted reflection stores provisional lessons with immutable versions and compatibility metadata. `harness.lessons.setStatus` takes `{ "id":"LESSON_ID", "status":"accepted" }` (also `provisional`/`rejected`); `harness.lessons.history` takes `{ "id":"LESSON_ID" }`. Conflicting accepted lessons require resolution, and unresolved conflicting library entries are not injected together. Keep these statuses fixed during a matched two-method comparison.

Across multiple development tasks, each task’s probes start from the admitted baseline; previous lessons are available to proposals under their scope/compatibility rules. The experiment’s final `candidateId` is the **last task’s composed revision**, not an automatic merge of all task-specific revisions. The final composition/reviewer should explicitly preserve justified earlier development lessons. Qualification then measures that one fixed candidate across all validation/held-out tasks.

## 5. Upstream k=1

Admit the same request with `mode:"upstream"`. Python 3.12/3.13 must already be installed; the application resolves known locations, records interpreter identity, verifies its own adapter identity, and verifies the pinned donor bytes before importing them. No installation or provider fallback occurs.

Call `advance` once. Respond to packets as they appear; the worker continues automatically after each submitted response. It stays an active Harness job while waiting for Codex, so another inference lane cannot start even though the GPU may be idle. Do not repeatedly press Advance while it is active.

The project adapter uses the pinned recursion’s existing `propose_workers=1` and `rollout_workers=1` controls through a scoped dispatch wrapper. Vendored source remains unchanged. This makes INV→DOM proposal/solver dispatch deterministic while preserving `agg` semantics: both candidate proposals derive from the same baseline, and composition follows both probes. Proposal, probe and final order now matches native. Both proposer and selector injection points use external Codex packets; k=1’s final choice is trivial, and reflection still uses the independent reviewer.

The published core treats a chosen final without a delivered result as a failed/partial task. That failure is preserved; it is not converted into a completed experiment. Actual accepted proposal/composition stages and all trials remain counted even when the final fails. Report the failed method/task in the comparison denominator. An upstream failure is not permission to remove that task or relaunch it as if the first attempt never happened.

## 6. Freeze, evaluate once, review, activate

There is no separate candidate-freeze mutation. A completed experiment with nonempty `candidateId` is the frozen candidate. Retrieve the final revision and export the pre-evaluation record. A new proposal requires a new experiment rather than modifying that revision.

```json
{"method":"harness.evaluations.start","params":{"experimentId":"COMPLETED_EXPERIMENT_ID"}}
```

This launches **all six validation plus all six held-out tasks**, baseline and final candidate, three repeats each: **72 solver trials**. There is no public parameter for a cheaper subset or validation-only selection. Do not use this endpoint just to preview the held-out pool.

Order is deterministic: task IDs sorted lexically, then repeat 0/1/2; within each task the arms are baseline→candidate, candidate→baseline, baseline→candidate. Development task order remains the explicit admitted list. Evaluation order is derived from frozen fixture identities and the application’s versioned schedule, rather than being an independently editable order array.

Every scheduled pair remains in the denominator. A solver failure, timeout or incomplete reply is a failed result even if some output files happen to pass. Nonzero independently executed grader checks fail correctness checks; integrity violations and missing duration/token observations prevent qualification. An initial failed solver test is allowed: a later successful exact read-only verification after all mutations can establish freshness. A later mutation or failed exact check requires another successful final verification. Grader/infrastructure errors are recorded as unavailable/failed evidence and prevent promotion, rather than being silently scored as successful or dropped.

The gate requires all 72 unique results, all critical integrity/measurement checks, no held-out task’s pass-count decline across its three repeats, **strictly higher aggregate held-out successes** (18 possible per arm), and median candidate solver duration and tokens each ≤1.2× baseline. The time/token medians use both validation and held-out results. Solver duration excludes separate grading and human review time; tokens are observed prompt+generated totals, not Codex’s proposal/review usage. A perfect held-out baseline cannot meet the strict improvement gate; keeping it is valid.

The post-freeze accounting correction also requires complete provider-terminal accounting for every model attempt, including retries, compaction and delegates. A positive token total alone does not establish completeness: an interrupted final generation may have consumed unreported tokens. Missing legacy accounting remains unknown; it is not upgraded by inference. Partial or unknown observations can be retained and independently reviewed, but cannot establish a cost ratio or authorize promotion. The October 27 qualification binary predates this correction; its observed token totals must not be presented as complete costs. See the [accounting audit](../evidence/phase2-metric-completeness-review.json) and [correction manifest](../evidence/phase2-metric-accounting-followup-manifest.v2.json).

After evaluation completes, fetch `reviewPacketId`. An independent reviewer inspects actual paired results, critical checks, candidate changes and test meaning. Submit `promotion_review` with the reflection shape, `lessons:[]`, and the evaluation ID from that packet’s evidence. Do not send held-out diagnostics into new proposals or retained lessons. Review and promotion recheck trial output hashes and the evaluation-evidence hash.

```json
{"method":"harness.promote","params":{"evaluationId":"EVALUATION_ID"}}
```

This is allowed only after the recorded gate passes and independent review approves. It activates a binding for this workspace+agent, preserves the previous revision, and does not change global agent/model settings. Use the returned promotion ID for rollback:

```json
{"method":"harness.rollback","params":{"id":"PROMOTION_ID"}}
```

Rollback is target-scoped and guarded against replacing a newer unrelated activation.

## 7. Cancellation, restart and exports

| Situation | Operation / behavior |
|---|---|
| Native waiting for packets | Leave it waiting; no solver job is active. Submit later and advance. |
| Cancel optimization | `harness.experiments.cancel {"id":"EXPERIMENT_ID"}`. Cancelled experiments are terminal; a new experiment is a new recorded attempt. |
| Cancel qualification | `harness.evaluations.cancel {"id":"EVALUATION_ID"}`. In-flight evidence remains. |
| Restart an interrupted optimization | Reopen the same profile with the same binary/runtime/model/image, inspect records, then advance. Completed outputs are hash-checked. Uncertain interrupted solver effects are retained, never replayed. |
| Continue interrupted/cancelled qualification | `harness.evaluations.resume {"id":"EVALUATION_ID"}`. Already recorded rows are skipped after output checks; interrupted trials remain failure/insufficient-evidence entries. Only untouched remaining trials run. |
| Environment or fixture changed | Do not rewrite identities to force resume. Preserve the old report and admit a new experiment under the new conditions. |
| Export JSON | `harness.experiments.export {"id":"EXPERIMENT_ID"}` returns the bundle. |
| Save a native artifact | `harness.experiments.exportArtifact {"id":"EXPERIMENT_ID"}` saves the bundle in the app’s artifact store. |

Export after the active trial finishes. Bundles include frozen manifests, public fixture inputs, revisions, packet provenance, observed traces, results, lesson history, and lossless base64 file bytes with hashes/sizes. Hidden grader inputs/expected assertions/probe source and raw thinking are excluded. Known app-root/private metadata is redacted; review candidate artifact contents before sharing. Export time is separate from the stable payload hash. Limits are 8 MiB per captured file, 64 MiB total captured file bytes, and 100 MiB for the saved formatted artifact.

Never edit a trial workspace, an old report, a grader or the SQLite records to make a failed attempt pass. A guided model repair is a separately recorded attempt, not a retroactive fresh success.

## 8. Matched comparison and budget

Run native and upstream development campaigns with identical initial target/binding/lesson state, explicit task list/order, model digest, runtime, generation controls, context, tools, skills, workflow/delegate choices, image, grader and binary. Compare semantic spec fields; experiment IDs/timestamps/mode and upstream-only interpreter/adapter fields naturally differ. Keep the chosen method order, cold/warm runtime state and all failures in the report. The API has no paired random-seed control, so do not describe the resulting model trajectories as identical random draws.

After both development campaigns and independent development review, select **one** final candidate before any held-out exposure, then run its single paired qualification. The global fixture+grader exposure registry deliberately rejects a second candidate evaluation on the same held-out pool. A second data directory does not make already-seen tasks scientifically unseen. A direct native-versus-upstream same-pool held-out comparison would need a separately designed preregistered multi-candidate protocol or genuinely new held-out fixtures; it is not provided by the current API.

| Work | Solver attempts | At a 20-minute per-trial ceiling |
|---|---:|---:|
| Fresh three-stack baseline pilot | 3 | Up to 1 hour |
| k=1 optimization, 1 development task | 4 | Up to 1 hour 20 minutes per method |
| k=1 optimization, 3 development tasks | 12 | Up to 4 hours per method |
| k=1 optimization, all 6 development tasks | 24 | Up to 8 hours per method |
| One final paired qualification | 72 | Up to 24 hours |
| Both 1-task methods + one qualification | 80 | Up to 26 hours 40 minutes |
| Both 3-task methods + one qualification | 96 | Up to 32 hours |
| Both 6-task methods + one qualification | 120 | Up to 40 hours |

These are configured solver ceilings, **not forecasts**. Add Codex review time, setup and independent grading. Each grade can run two bounded commands (three for integration probes), each with a two-minute timeout; successful reference checks were much shorter. A 16K response limit is per generation, not the entire trial. With 80 shared model calls the arithmetic ceiling is 1,310,720 generated tokens per trial, but deadline, context, byte and tool budgets normally stop work much earlier. Do not reserve or promise that amount as expected consumption. Use actual trial usage/duration to estimate the remaining campaign after the first few attempts.

Native review pauses release the inference slot. The upstream waiting worker holds the application’s Harness admission slot until it finishes, is interrupted by application shutdown, or is explicitly cancelled. Neither method consumes Gemma tokens merely while waiting for a packet.

## Source and evidence pointers

Implementation: [application API](../../internal/app/harness.go), [native schedule and packets](../../internal/harness/packets.go), [trial identity/recovery](../../internal/harness/manager.go), [paired evaluation/gates](../../internal/harness/evaluation.go), [upstream adapter](../../scripts/harness/upstream_adapter.py), [pinned core](../../third_party/harness2/UPSTREAM.json), [live pilot driver](../../internal/app/phase2_live_test.go).

Deterministic evidence: [final app/Harness race suites](../evidence/harness-phase2-final-tests.txt), [18 Docker reference qualifications](../evidence/harness-phase2-fixture-tests-final.txt), [actual core order parity](../evidence/harness-phase2-order-parity-tests.txt), [failed-stage accounting](../evidence/harness-phase2-order-and-failure-tests.txt), [adapter integrity/order controls](../evidence/harness-phase2-adapter-order-tests.txt). These checks validate implementation behavior; they are not model quality or promotion results.


### Disposable fixture resources and lightweight status

Harness list/status queries project large trace, packet, evaluation, revision-content, and frozen-agent/skill/workflow payloads inside SQLite before Go decodes the result. Full immutable detail remains available through the existing `*.get` APIs. Ordering and metadata identifiers are unchanged.

Every newly executed Harness solver fixture and temporary grading fixture records a durable cache-cleanup request. After its work has stopped, the executor releases only that fixture’s cache for the frozen Docker image. Completed trial source, output hashes, tool journals, and evidence remain intact; temporary grader source is removed only after guarded cache release. This disposes ephemeral experiment resources and does not evict ordinary project caches. A cleanup failure is retained in `harness.status.cacheCleanup`, prevents infrastructure qualification, and is retried from its exact recorded root after command recovery or before later Harness admission. Startup does not scan for or prune unrecorded historical pilot/project caches.

Automated evidence: [cache/projection checks](../evidence/harness-phase2-cache-and-projection-tests.txt) and [shared solver checkpoint check](../evidence/harness-phase2-cache-solver-tests.txt).
