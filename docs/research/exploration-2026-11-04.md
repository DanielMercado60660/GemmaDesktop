# Gemma Desktop exploration — 4 November 2026

This exploration inspected the current checkout at `d8dbce0`, its recorded evaluation evidence, and current primary upstream sources. The checkout began clean. No application code, dependencies, model settings, or personal app data were changed. This note supplies evidence for the next planning phase; proposed improvements have not been implemented or qualified.

The strongest direction is to improve dependable task completion and everyday usability. The application already has substantial infrastructure: local inference, governed tools, durable approvals and receipts, workflows, capability profiles, context management, visual retrieval, and evaluation. More features alone would not address the failures recorded by the existing campaign.

The execution path is React → Wails → Go application services → ADK → native Ollama adapter. Go owns permissions, workspace operations, execution, and SQLite persistence. The frontend presents those records. The [architecture document](../architecture.md) remains a useful package map.

**What is already worth preserving**

- Immutable run/capability snapshots, schema validation, revision-aware file edits, and approval decisions outside the model.
- Durable tool intentions and receipts; uncertain external effects are retained instead of automatically replayed.
- Explicit handling of partial, empty, and truncated model responses.
- Conversation-scoped drafts and asynchronous submission ownership, revision conflicts, cancellation-focused confirmation dialogs, and lazily loaded editors.
- Independent evaluation, retained failures, frozen candidate revisions, guarded held-out exposure, and rejection of unqualified candidates.

**Fresh checks**

| Check | Result and scope |
| --- | --- |
| `./scripts/test.sh` | Passed: Go race tests, 33 Python script tests, 65 frontend tests, TypeScript and production frontend build. Some Go packages used cached passing results. |
| `python3 -m unittest discover -s scripts/harness -p 'test_*.py'` | Four adapter tests passed separately. |
| Frontend bundle | Built successfully; the existing large Monaco/editor chunk advisory remains. |
| Working tree after checks | No tracked changes before this note was added. |

The first test attempt could not bind loopback fixture servers under the execution sandbox. The same suite passed after receiving the needed execution permission. These are regression results, not a fresh coding-quality benchmark. Live inference, native UI interaction, optional Docker/Chrome/PTY checks, restricted-executor opt-in tests, notarization, and hardware comparisons were not performed. The main test log is `/tmp/gemma-explore-tests-20261104-full.log`.

**Confirmed issues and product gaps**

| Finding | Evidence in the current checkout | Candidate improvement |
| --- | --- | --- |
| An edited workflow can run its previous saved definition. The badge still says “Saved workflow” whenever an ID exists. Back/navigation can discard workflow edits. | [Workflows.tsx](../../frontend/src/Workflows.tsx), lines 179–185 and 280–310; [App.tsx](../../frontend/src/App.tsx), line 887. | Track dirty state and saved revision; make save-and-run or run-saved explicit; protect navigation. This is the clearest immediate UI correctness fix. |
| Setup and Send can appear ready when Ollama is reachable but the selected model is absent. | [App.tsx](../../frontend/src/App.tsx), lines 872–886 and welcome flow; [modelCapabilities.ts](../../frontend/src/lib/modelCapabilities.ts), line 5. | Check installed completion model, workspace, and executor readiness, with an actionable first-task path. Backend admission remains separate. |
| Unsent prompts and attachments survive navigation but not app restart. | [composer.ts](../../frontend/src/lib/composer.ts), line 35; [App.tsx](../../frontend/src/App.tsx), lines 492 and 732. | Bounded local draft persistence, cleared after successful admission and workspace removal. |
| The standard test entry point omits the four Harness adapter tests; this checkout has no `.github` workflow. | [test.sh](../../scripts/test.sh), line 4; [test_adapter.py](../../scripts/harness/test_adapter.py). Top-level discovery finds 33 tests and zero adapter tests. | Include the adapter suite and establish repeatable macOS CI. Keep service-dependent checks distinct. |
| Frontend tests cover helper functions rather than mounted interactions. Workflow edits and keyboard interactions are not exercised by that layer. | [package.json](../../frontend/package.json), line 11; [frontend tests](../../frontend/tests). | A small rendered interaction suite covering setup, save/run identity, dirty navigation, approvals, and keyboard use. |
| Fresh default agents repeat the base instructions during run construction. | [core.go](../../internal/app/core.go), line 121; [runs.go](../../internal/app/runs.go), line 66. | Remove duplication while preserving customized existing instructions. |

Accessibility deserves targeted checks alongside interaction tests: workspace tabs lack complete keyboard/tab-panel behavior, and approval/completion surfaces lack explicit live announcements. Existing focus styling, reduced-motion handling, and confirmation focus behavior should be preserved. Screen-reader behavior has not been manually assessed.

**The largest reliability opportunity: make verification an explicit operation**

The [historical campaign](../evidence/phase2-qualification.md) recorded 0/36 baseline automated passes and 3/36 candidate passes. Its 72 attempts comprised 54 verification-only failures, ten incomplete attempts, five protected-input failures, and three passes. Separate public-contract probes found semantic defects, including in both Node automated passes. These are results from the frozen campaign binary, not new measurements of this checkout.

The current solver still appends “Use readOnly:true” to its instructions ([harness_solver.go](../../internal/app/harness_solver.go), line 74), while `run_command` defaults to writable execution ([manager.go](../../internal/tools/manager.go), line 595). A dedicated verification action could bind a declared command, enforce the chosen read-only profile, and produce evidence tied to the source revision after the last edit. Ordinary chat could optionally expose separate “agent finished” and “verification passed” outcomes using the existing receipt infrastructure.

Task-specific protected paths should be enforced across file tools and Docker publication when preservation is part of the declared task. Currently the harness checks protected bytes afterward ([harness_grader.go](../../internal/app/harness_grader.go), line 130). This is a constraint of those tasks, not a reason to prohibit normal test editing everywhere.

Verification protocol compliance and code correctness must remain separate measurements. Current new-test detection establishes only that a new test/spec-named file exceeds 40 bytes; its label correctly defers meaningfulness to independent review ([harness_grader.go](../../internal/app/harness_grader.go), lines 140–157). Test discovery checks, regressions that fail on the broken baseline and pass after repair, and independent boundary/property cases would give stronger evidence.

The current full qualification runs all validation/held-out tasks with three paired repeats, and the exposed held-out pool cannot be reused for another unseen-task claim ([evaluation.go](../../internal/harness/evaluation.go), lines 32–47 and 97; [campaign guide](../implementation/HARNESS_CAMPAIGN.md), line 166). A cheaper, explicitly developmental diagnostic suite would let us compare one change at a time before spending on a new frozen qualification pool.

**Performance and context hypotheses to measure**

| Observation | Proposed experiment | Evidence |
| --- | --- | --- |
| Run admission loads all saved messages and runs before filtering for one conversation. | Seed many unrelated chats; measure submission latency and memory, then compare indexed session-scoped queries. | [runs.go](../../internal/app/runs.go), lines 73–117; [store.go](../../internal/store/store.go), line 133. |
| UI polls full state about every 700 ms while active and 2.2 seconds while idle; event caches grow as pages arrive. | Measure bridge bytes, React commits, snapshot latency and memory; evaluate cursor deltas, paginated lists and bounded caches. | [App.tsx](../../frontend/src/App.tsx), lines 520–666; [core.go](../../internal/app/core.go), line 240. |
| Every streamed text/thinking delta inserts synchronously into SQLite with `synchronous=FULL`. | Profile streaming write cost; compare bounded display-delta coalescing while preserving immediate durable tool/approval/checkpoint commits. | [engine.go](../../internal/engine/engine.go), line 437; [runs.go](../../internal/app/runs.go), line 290; [store.go](../../internal/store/store.go), lines 60 and 188. |
| The initial capability profile includes the full built-in catalogue. | Compare small inspect/code-repair/research presets against the existing profile, then evaluate tool discovery for large catalogues. | [capability.go](../../internal/app/capability.go), line 115. |
| Budgets bound total work, but repeated equivalent failures can consume them without progress. | Evaluate state-aware stalled-loop detection with bounded corrective feedback; allow legitimate reruns after changes and preserve uncertainty rules. | [engine.go](../../internal/engine/engine.go), line 33; [adapter.go](../../internal/engine/adapter.go), line 59. |
| Enabled guidance discovery collects nested guidance globally and rejects more than 16 total context sources. Retained exact user context also has a fixed bound. | Evaluate path-scoped guidance and explicit focused-chat handoffs with preserved constraints/receipts. | [context_manifest.go](../../internal/app/context_manifest.go), lines 33–99; [user_context.go](../../internal/engine/user_context.go), line 19. |

Current compaction is already enabled and uses a 60% default threshold, eight retained events, and a dedicated bounded summarizer. The current implementation has evolved beyond older research notes: [compaction.go](../../internal/engine/compaction.go) allows up to 3,072 summary output tokens, declines oversized transcripts rather than silently trimming tool content, and uses a 120-second summarization timeout. Adding compaction again is not an improvement proposal. The remaining question is effective context headroom and continuity quality. The inspector honestly labels its text-token estimate as heuristic and excludes image/schema tokenization ([context_audit.go](../../internal/engine/context_audit.go), line 35).

None of these performance changes has a measured speedup yet.

**Current upstream findings**

| Source checked on 4 November | Relevant change or method | Implication for this project |
| --- | --- | --- |
| [ADK-Go 2.7.0](https://github.com/google/adk-go/releases/tag/v2.7.0), released 2 November; [2.7.1](https://github.com/google/adk-go/releases/tag/v2.7.1), released 4 November | Tool search, restoration of completed join predecessors, cancellation/timeout fixes, a model-call budget, and changed telemetry conventions. | Evaluate a controlled upgrade from the pinned 2.6.0. Re-run recovery, joins, cancellation, budget and adapter contracts before removing app workarounds. |
| [Pinned ADK tool-search example](https://github.com/google/adk-go/blob/v2.7.1/examples/toolsearch/main.go) | Declares a core tool set and exposes additional definitions after search. | A concrete option for reducing large tool catalogues. Discovery must remain inside frozen authorized capabilities; it does not grant new permissions. |
| [Ollama 0.40.14](https://github.com/ollama/ollama/releases/tag/v0.40.14), stable release published 3 November UTC | Faster MLX top-k sampling for large vocabularies. | Benchmark against the recorded 0.40.11 qualification baseline. No installed runtime version was rechecked or changed here, and the speedup in this app is unknown. |
| [Wails 3.1.0](https://github.com/wailsapp/wails/releases/tag/v3.1.0), released 1 November | Event-ordering/locking and desktop fixes; experimental alternative backends and packaging additions. | Review relevant fixes separately from ADK. Keep Go/JS runtime pins aligned and verify the native bridge. Latest listed nightly releases are not the stable upgrade target. |
| [Ollama thinking controls](https://docs.ollama.com/capabilities/thinking) | `/api/show` reports supported thinking values and defaults. | The app exposes Auto/On/Off but does not forward this metadata in its model records. Capability-driven controls could prevent mismatched choices. |
| [Ollama streaming tool calling](https://docs.ollama.com/capabilities/tool-calling) | Preserve accumulated thinking, content and calls through the current tool cycle. | This supports the native adapter direction already implemented; a generic compatible-endpoint rewrite is not justified by this guidance. |
| [Ollama context guidance](https://docs.ollama.com/context-length) | Larger contexts require more memory; observe loaded context and CPU/GPU residency. | Compare measured context/memory/latency profiles rather than globally increasing the default. The FAQ and context page disagree about defaults, reinforcing the need to inspect runtime behavior. |
| [ADK compaction documentation](https://adk.dev/context/compaction/) | Token-triggered compaction with recent-event retention, including Go support. | Consistent with the existing approach. Source/version-specific behavior still needs conformance tests. |

External engineering guidance also supports focused tools, concise results, trace-driven iteration, and small evaluation suites drawn from actual failures. These methods fit the observed problems, but their benefit with local Gemma remains an experiment. Sources: [writing effective tools](https://www.anthropic.com/research/writing-tools-for-agents), [context engineering](https://www.anthropic.com/research/effective-context-engineering-for-ai-agents), and [agent evaluations](https://www.anthropic.com/research/demystifying-evals-for-ai-agents).

**Recommended direction for the planning phase**

1. Correct workflow save/run behavior and missing-model readiness; cover these with rendered interaction tests and close the standard-test discovery gap.
2. Design explicit task verification and declared protected-file enforcement; establish a cheap diagnostic baseline with separate protocol and semantic outcomes.
3. Compare focused capabilities, bounded stall handling, and model/context settings one change at a time. Qualify any claimed quality improvement on a fresh frozen task pool.
4. Evaluate ADK/Ollama/Wails updates independently, retaining the current baseline and recovery contracts.
5. Address measured persistence, polling, streaming, and context bottlenecks; add draft persistence and targeted keyboard/accessibility improvements.

Planning should assign an acceptance test and observable success metric to each selected change. Suggested metrics are first-attempt task correctness, fresh verification after edits, protected-file preservation, invalid/repeated tool calls, completion rate, elapsed time, tokens, and UI response latency. Broad model replacement, training, and new orchestration layers are not prerequisites for investigating these improvements.
