# Gemma Desktop verification and Harness Lab plan

Research date: 26 October 2026. This is the proposed acceptance plan for implementation; no application tests or inference benchmarks have been run during research.

Completion requires a packaged macOS application performing real work with a local Gemma model, alongside deterministic tests of permissions, persistence, and failure behavior. Keep those evidence categories separate: a mocked transport can prove error handling but cannot prove the installed model can complete a task.

## Integration experiments before broad implementation

| Experiment | Required evidence | Decision if it fails |
| --- | --- | --- |
| Pinned native desktop build | Wails 3.0.0 app opens from Finder with embedded assets, worker-based Monaco, generated bindings, stream transport and graceful close | Fix platform/bundling choice before producing every screen |
| Real Ollama tool round trip | Exact runtime/model digest; streamed text, one call, result continuation, multi-call correlation, thinking handling, cancellation | Correct native adapter or select a verified model/runtime combination |
| ADK workflow conformance | Sequential, conditional, parallel, join, dynamic child and approval flows; reject unsupported conditional joins; resume supported pauses and reconcile abrupt crashes | Constrain graph compiler or pin a narrow upstream fix with regression tests |
| SQLite engine and adapter | Runtime SQLite version contains WAL fix; session events round-trip; migration/backup/restore passes | Change driver selection and its matching dependencies |
| Execution boundary | Files/child processes/network remain confined; missing isolation refuses risky execution | Keep the affected executor unavailable until it passes |
| Artifact boundary | Generated hostile HTML cannot invoke Go services, access secrets, or load privileged assets | Use inert previews and a separate isolated browser |

These experiments are part of the build once kicked off. Research did not install toolchains, start services, download models, or create a runnable application.

## Deterministic automated coverage

Test contracts that cross trust and failure boundaries:

- File traversal, escaping symlinks, special files, unusual Unicode names, hard links, huge files, changed revisions, partial writes, and disk exhaustion.
- Policy precedence, approval scope and expiry, revocation, schema changes, duplicate replies, restart while waiting, and model attempts to grant itself access.
- Process timeout, cancellation with grandchildren and detached/reparented descendants, an exited container CLI with a still-running workload, full output pipes, nonzero exits, signal termination, terminal resize, and restart cleanup.
- Malformed/truncated model streams, tool-call IDs, several calls in one response, invalid JSON, context overflow, runtime disconnect, and absence of final metrics.
- MCP old/new protocol negotiation, pagination, changed tools, tool result errors, slow or malicious peers, OAuth cancellation, bounded frames, and an uncertain write after disconnect.
- Graph resume with completed predecessors, validation rejection of a conditional incoming join edge, recursive/dynamic growth, budget exhaustion, and changed workflow definitions. Restart at a supported approval pause differs from abrupt-crash recovery, which must preserve interrupted or unknown outcomes.
- Event sequence gaps, duplicate delivery, frontend reconnect, slow consumers, and loss of the last transient token batch.
- SQLite migration rollback, concurrent readers, interrupted transactions, failed backups, restore compatibility, and corruption diagnostics.
- Browser isolation against redirects, DNS rebinding, WebRTC, workers/service workers, and direct host/loopback requests, including Ollama; no `--no-sandbox` workaround.
- Terminal title/link/clipboard escape sequences, bounded output retention, and rejection of agent input into a more privileged user terminal.
- Synthetic credentials in model/tool traces, browser headers, terminal input, diagnostic exports, and backups, with explicit retention and tested redaction for known secrets.

Use test fixtures and injected failures here. Do not add tests that merely assert a static label or mirror a trivial implementation.

## Real product journeys

| Journey | Observable success |
| --- | --- |
| First launch | Detects runtime availability, explains setup, selects a real installed/downloaded compatible model, and reaches a usable workspace |
| Repair a small repository | Reproduces a failing test, edits only authorized files, reruns verification, shows diff and test output, and preserves pre-existing user changes |
| Produce a useful artifact | Reads a supplied folder, writes a sourced summary or report, lists its artifact, and opens the exact generated file |
| Approval and cancellation | Pauses before the chosen consequential action; deny prevents it; cancel ends owned execution descendants and this run's inference HTTP request, preserves other clients' Ollama work, and records uncertain remote outcomes |
| Multiple agents | Separate agents inspect or implement bounded subtasks; changes are integrated without two writers corrupting the same workspace |
| Reusable workflow | Saves a validated definition, runs branches and approvals, exposes node details, and resumes an interrupted supported flow |
| MCP service | Connects a real stdio test server and a real Streamable HTTP server; discovers tools and invokes a permitted operation |
| Browser work | Navigates an owned browser to a controlled site, reads content, performs an approved interaction, and captures a bounded artifact |
| Skills | Imports an instruction package, shows its origin/content, uses it in a real run, and prevents its scripts from expanding permissions |
| Offline work | With model and dependencies present, completes the supported local journey while external network access is denied |
| Restart and recovery | Restores conversation and committed progress; does not repeat a write whose outcome is unknown |
| Local distribution | Ad-hoc-signed local arm64 bundle launches outside the development shell and retains state across app updates, without Developer ID credentials |

Use deterministic fixture repositories and local websites for acceptance, plus a small set of realistic user tasks for usability. Each journey records app commit, dependency versions, macOS, runtime/model digest, settings, policy, timings, artifacts, and pass/fail criteria. An assistant's completion sentence alone is not evidence.

## Model and runtime measurements

Measure queue delay, model-load duration, time to first visible text, time to first tool call, prefill and decode rates where the runtime supplies counts, total wall time, peak memory observations, cancellation delay, and verified task success. Separate cold-load, model-resident warm, and prefix-cache-warm runs; record cached prompt counts when available. Record the actual context setting and resident backend rather than the model's advertised maximum. Cancellation tests must preserve the shared Ollama daemon, other clients' generations/downloads, and shared model residency.

Compare the 12B MLX candidate with at least one smaller candidate and one larger candidate only when memory admission permits. Compare MLX and GGUF with task-equivalent prompts/settings and clearly identify differing quantization. Record total task success and latency; a faster token rate can still yield worse task completion. Several agents sharing one local model should first use conservative inference concurrency, then measured increases.

Use repeated runs and report sample counts and variation. Reset workspace, session, and agent memory between candidate trials; balance baseline/candidate order to reduce cache and thermal bias. A seed does not establish determinism across different runtimes, kernels, quantizations, or sampling implementations. Distinguish model errors from harness/tool failures. Preserve failed examples for regression testing.

## Harness Lab design

Harness² proposes candidates, probes their behavior, and composes improved harnesses while keeping model weights fixed. Its release supports OpenCode/Codex executors and benchmark workflows, not a ready-made Go/ADK desktop subsystem. Use its experimental method as inspiration for a modular application feature. [Harness² repository](https://github.com/google-research/harness2/tree/1cfe254e8d25b15f37048c3730134686abe60335)

Store immutable baseline and candidate configurations covering instructions, skill selections, context/memory strategy, permitted tool presentation, delegation/workflow configuration, and model parameters. Security policy is a separate host-owned configuration that candidates cannot change. Scripts and plugins proposed by an optimizer use the same execution boundary as ordinary tools.

Each experiment should record:

- Baseline/candidate configuration hashes and a readable change diff.
- Task fixture revision and separate development/evaluation partitions.
- Model/runtime/quantization/context, sampling parameters, budget, and concurrency.
- Time, tool/model call counts, observed memory, deterministic outcomes, and failure classification.
- Artifacts, execution traces, and which configuration the user chose to promote.

Use deterministic checks where possible: tests pass, expected file exists, schema validates, intended output matches, or a local website reaches the correct state. A model judge can provide an additional assessment, with its model/prompt recorded; it should not be the sole judge of its own edits.

The Harness² experiment guide warns that public benchmark rubrics can be fetched online, that its seed shuffles tasks without controlling model sampling, and that its released execution environment does not enforce network isolation. Keep evaluation answers outside the executor's accessible filesystem and network. Treat these as experimental design constraints, not configuration defaults to copy. [Pinned experiment guide](https://github.com/google-research/harness2/blob/1cfe254e8d25b15f37048c3730134686abe60335/docs/EXPERIMENTS.md)

Promotion should require improved held-out performance under fixed budgets without regressions in security, excessive approvals, or resource usage. Keep held-out examples and scores outside the optimizer's context; repeatedly using them to select prompts turns evaluation into tuning. Keep failed and unscored experiments visible; do not quietly drop them from a comparison. Include Harness Lab in completion, after the core task execution and evidence systems are reliable.

## Visual and release checks

Inspect the real macOS app at narrow and wide window sizes, with keyboard navigation, VoiceOver, reduced motion, increased text size, long code lines, large diffs, and prolonged streaming. Validate dark contrast without relying on color alone. Verify empty, loading, failed, cancelled, waiting, completed, and unknown-outcome states. No decorative green connected/live dots or fabricated activity.

Exercise Finder launch, model/runtime absence, network loss, sleep/wake, window close versus Quit, database upgrade, and resource cleanup. The initial contract should keep a task alive when its window closes only while the app process remains running; ordinary Quit must persist and stop safely. Work continuing after Quit requires an explicitly designed helper/service.

Record the final bundle hash, supported host environment, build commands, tests, live journey evidence, screenshots, known limitations, and any signing/notarization dependency. Use an ad-hoc-signed local arm64 bundle without Developer ID credentials for local delivery. Apple Silicon requires a valid signature, and ad hoc signing satisfies that requirement; it does not establish Developer ID distribution readiness. [Apple code-signing requirements](https://support.apple.com/en-gb/guide/security/secebb113be1/web)
