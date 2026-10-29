# Workbench guide

A local agent workbench for macOS and Apple Silicon. Go owns inference, orchestration, permissions, persistence, and tool execution. A Wails 3 window hosts the React interface. Gemma runs through your local Ollama installation; the app rejects remote inference endpoints and cloud model aliases.

This is an independent project, unaffiliated with Google. The original brief and concept image are preserved in [docs/reference](reference/product-specification-v1.md) and [design/reference](../design/reference/gemma-desktop-concept.png).

## Run

1. Start Ollama locally (`ollama serve`, or the Ollama application).
2. Install a local model, for example `ollama pull gemma4:12b-mlx`. The Models screen also supports downloads, cancellation, load, unload, and deletion.
3. Open `dist/Gemma Desktop.app` after building.
4. Choose a project directory, create a conversation, and send a task. Consequential tool calls appear as approvals with their exact arguments. Denying a request returns an error to the agent.

The app defaults to `http://127.0.0.1:11434`, a 32K context, 24 model steps, and approval-required execution. A 64K working configuration has also been exercised on the 64 GiB target Mac with a measured 55,457-token input; select context size in Settings and distinguish capacity from coding qualification. Model weights are managed by Ollama outside the app bundle. Download size is not a promise of working memory usage: context, the runner, and other active applications affect memory requirements.

Closing the window hides it and lets active work continue in the app process. Clicking the Dock icon reopens it. Quitting cancels active work. There is no independent background daemon.

## Build and test

Requires an Apple Silicon Mac, Xcode command-line development tools, Node 22.12+ (or another supported Vite runtime), and a Go bootstrap installation. Scripts select Go 1.26.10 and keep its caches inside this project. The current target machine and exact verification evidence are recorded in [docs/implementation/STATUS.md](implementation/STATUS.md).

```sh
./scripts/icon.sh             # optional: regenerate the original app icon
./scripts/build.sh            # React build, native arm64 app, ad-hoc signature
./scripts/test.sh             # Go race tests, harness/provenance tests, frontend build
./scripts/package-local.sh    # verified local app archive and SHA-256 checksum
./scripts/dev.sh              # embedded UI + headless app at 127.0.0.1:8787
```

Development mode is explicit, loopback-only, rejects foreign origins/hosts, and is absent from an ordinary app launch. Core logic is independent of Wails in `internal/app`, `internal/engine`, and the other internal packages. Tests may need permission to create local listeners, launch Chrome, or apply a macOS sandbox when run inside another sandbox.

With the development server and a real local Gemma model running (use Homebrew Python for the restricted-executor coding check; Python.org framework libraries are outside its allowed paths):

```sh
/opt/homebrew/bin/python3 scripts/e2e.py
python3 scripts/realistic-e2e.py --qualify-fixture
python3 scripts/realistic-e2e.py --run
python3 scripts/workflow-e2e.py
GEMMA_LIVE_MODEL=gemma4:12b-mlx ./scripts/live-model-test.sh
```

The coding test creates its own small failing project under `.runtime`, accepts only a narrowly defined edit and Python test command, and independently reruns the tests. It writes actual evidence to `docs/evidence/live-coding-task.json`. It does not modify a user's project.

The realistic qualification creates a multi-file SQLite task tracker with persistence, filtering, and pagination defects plus a CSV service/CLI feature request. It protects the original tests, permits only scoped source edits and a read-only Python test command, records Git changes, runs nine independent acceptance checks outside the model's workspace, and checks saved-conversation continuity. Every attempt, including failures, is retained in `docs/evidence/realistic-*.json`.

For an interrupted or failed task, `--continue-report` plus `--followup-prompt-file` starts a new turn in the recorded conversation. It verifies the prior report and file hashes, preserves the exact approval scope, and writes a separate linked report. A recovery success does not change the original attempt's result. Qualification requires a fresh successful test command after the final edit, no later failed test attempt, additional discovered regression tests, and all nine independent cases. Cached command receipts cannot verify later edits, and changes to the harness during a run invalidate qualification. Generated sources and tests also need independent review for meaningful behavior and assertions.

The explicit harness variant `--tool-protocol structured_json` selects an experimental response-schema transport; `native` remains the default. Validated action objects still pass through ADK and the existing governed executor. This mode buffers JSON until completion, rejects unsupported schemas and unsafe numeric values, and does not execute truncated or invalid proposals. It is an API/harness experiment without a UI selector; see the [context/runtime investigation](research/context-adk-investigation-2026-10-27.md) for live qualification and limitations.

## Workbench

- **Chats:** streamed answers, conversation-owned drafts and attachments, run cancellation, approvals, paged tool activity, bounded local context summaries, and saved results. Observed-action receipts distinguish actual command/file outcomes from model prose; follow-ups restore completed tool history and summaries from saved failed-run snapshots without replaying effects, with bounded recovery evidence when a usable snapshot is absent.
- **Files and changes:** scoped file browsing and Monaco editing with revision checks; separate staged, unstaged and untracked Git status alongside source diffs and recorded artifacts.
- **Agents:** named instructions, local model choice, step budgets, explicit Auto/On/Off thinking, response-token limits (default 8,192, including thinking), sampling temperature (0–2, legacy default 0.2), and read-only or approval modes. Auto preserves the runtime's thinking default. An empty ordinary generation receives at most one retry when the original budgets allow it, with completed tool history preserved. Reaching a generation limit or receiving a second empty terminal answer marks the response incomplete; truncated tool calls never execute. Partial answers remain in visible history but are excluded from completed model context. The global read-only mode remains authoritative; capability-profile controls select the permitted tools and skill versions.
- **Workflows:** React Flow editing backed by validated ADK graphs. Agent, condition, approval, parallel, join, output, and dynamic fan-out nodes are compiled in Go. The frontend does not schedule work.
- **Models:** real Ollama inventory, model downloads and residency controls, host memory, and runtime diagnostics.
- **Capabilities:** versioned profiles choose tools, immutable skill versions, MCP tools, execution settings and read-only specialists. Each run freezes its effective profile, deadline and budgets. The Context inspector records admission and runtime sources; the Plan inspector distinguishes progress claims from linked execution evidence.
- **Tools:** local stdio and remote HTTP MCP configuration, workspace-bound discovery and explicit invocation approval. HTTP connections support native-browser OAuth with PKCE, constrained endpoints and Keychain-backed refresh. Browser tools use an isolated Chrome profile and approved-origin networking.
- **Skills:** imports complete immutable bundles with references, assets and scripts, hashes and provenance. Approved resource materialization preserves binary bytes and requires a revision check. Scripts run only through a separately permitted command executor. Import never expands an existing profile.
- **Artifacts:** retained before/after file content, generated files, and browser screenshots; native open/reveal actions.
- **Harness Lab:** retains the bounded text comparator and adds native and pinned-upstream Harness² coding experiments, hash-validated Codex packets, immutable revisions, scoped lessons, isolated grading, paired evaluations and gated promotion/rollback. Gemma solves; Codex proposes and independently reviews. See the [campaign operator guide](implementation/HARNESS_CAMPAIGN.md).
- **Terminal:** an explicitly approved interactive user shell, separate from agent execution. Agents have no terminal-input API.

## Boundaries and recovery

See [docs/architecture.md](architecture.md) for the ownership model and [docs/security.md](security.md) for precise operational limits.

Workspace tools reject traversal, symlinks, protected credential paths and unsafe file types. Edits require a previously read hash; concurrent external changes cause conflicts. The restricted command profile retains macOS Seatbelt and denies child processes. The Docker profile supports Node/TypeScript, Python and Go subprocesses in an offline Linux guest using a pinned installed image. It receives a filtered private project copy, never the original-project mount, home, app database, credentials or Docker socket. There is no unrestricted fallback. The user terminal intentionally has user-account privileges and asks for approval separately.

Docker commands publish permitted source changes automatically before returning, including valid changes from ordinary nonzero exits. Cancellation, timeout and uncertain execution discard unpublished changes. Journals reconcile interrupted publication without overwriting unrelated edits. Dependency acquisition is a separate approved setup operation for supported integrity-locked public dependencies; installation is offline. Private caches are scoped to the project, image, architecture and dependency versions. See the [executor contract and setup](../internal/execution/README.md) for supported locks, limits and recovery. The 2 GiB retained-storage monitor is a soft bound with possible interval overshoot, not a hard disk quota. Stdio MCP remains on its restricted executor.

Commands can select a guarded relative working directory and request `readOnly:true`, which permits test databases in private temporary storage while protecting project files. Diagnostic output retains both its beginning and end. A successful process exit is evidence of that process outcome, not an independent correctness judgment.

The configured context size is requested from Ollama, and the UI separately shows runtime-reported capacity. These values are not a verified hard token limit on every runner: the earlier Ollama 0.32.3 MLX path processed an oversized-history qualification despite `num_ctx:1024` and explicit no-truncation flags. Ollama 0.40.11 still treats MLX context as soft; the later 64K request processed 55,457 measured input tokens with no observed increase in swap use. See [the original observation](evidence/context-overflow-observation.json) and [the context/runtime investigation](research/context-adk-investigation-2026-10-27.md). Task time/output budgets and ADK compaction remain host controls; model/backend changes require their own qualification.

Ordinary conversations retain original user text hidden by compaction as chronological quoted user history, reconstructed from the raw session rather than the generated summary. Later requests take precedence. The current request stays a native user message; tool results, assistant text, and generated summaries never enter that archive. A 64 KiB archive bound fails explicitly rather than dropping instructions. This preserves text, not prior image pixels, and does not grant permissions or guarantee model compliance.

SQLite stores the application journal, configurations, approvals, tool intents/outcomes, artifacts, and full ADK checkpoints. After an unexpected exit, unfinished work is marked interrupted; uncertain tool outcomes are not replayed. A supported native workflow approval checkpoint can be explicitly resumed with its frozen graph and agent definitions. Arbitrary process crashes cannot be safely resumed as if external actions had never occurred.

No raw generated HTML, remote web pages, or active SVG is embedded in the privileged application webview. Markdown disables raw HTML. Monaco workers and frontend assets are bundled locally.

## Visual video questions

The workspace **Video** tab imports an immutable private copy of a local MP4, indexes sampled frames, and answers visual questions with clickable source timestamps. The first slice supports H.264, constant frame rate up to 60 fps, up to 90 seconds, 256 MiB and 1920×1080. Optional AAC audio is preserved for playback but is not analyzed. Rotated, variable-rate, HDR, nonzero-start and multiple-video-stream inputs are rejected explicitly.

This path requires installed FFmpeg/ffprobe and a multimodal Ollama runtime; the qualified target is FFmpeg 8.1 and Ollama 0.40.11. The explicit retrieval-model download is `embeddinggemma-2:440m-mxfp8-text-vision` (about 778 MB). It produces retrieval vectors, not answers; select an installed vision completion model for answering. Models and FFmpeg are outside the app bundle.

Extraction and indexing are bounded, cancellable jobs. Each indexed frame retains its original presentation timestamp and hash. Queries retrieve up to three distinct sampled frames, and answers use those images without tools or coding history. Host-validated citations seek the immutable clip; valid citations establish the referenced source, not the truth of every model claim. This is frame-based retrieval, not continuous motion or audio understanding.

Indexes publish atomically only when complete. Restart marks interrupted work without silently replaying it; users can explicitly index again. Normal cancellation cleans unpublished index files. An abrupt process crash can leave unpublished private files for later cleanup; they are not searchable. Media URLs are short-lived, same-origin capabilities and contain no source paths. See [the design and qualification record](research/video-rag-design.md) and [current implementation status](implementation/STATUS.md).

## Distribution

`scripts/build.sh` creates an ad-hoc-signed local app without Developer ID credentials. This is suitable for validation on this Mac. `scripts/package-local.sh` archives that app with a checksum. It is not a notarized public release. `scripts/distribute.sh` uses an explicitly supplied Developer ID identity and a preconfigured notarytool keychain profile; secrets are not written into the repository. Only run it when those distribution credentials and release intent are available.

Dependencies remain under their upstream licenses. See [NOTICE.md](../NOTICE.md). The research reports are design evidence; implemented behavior and verification evidence take precedence over proposed research assumptions.
