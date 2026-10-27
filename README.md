# Gemma Desktop

A local agent workbench for macOS and Apple Silicon. Go owns inference, orchestration, permissions, persistence, and tool execution. A Wails 3 window hosts the React interface. Gemma runs through your local Ollama installation; the app rejects remote inference endpoints and cloud model aliases.

This is an independent project, unaffiliated with Google. The original brief and concept image are preserved in [docs/reference](docs/reference/product-specification-v1.md) and [design/reference](design/reference/gemma-desktop-concept.png).

## Run

1. Start Ollama locally (`ollama serve`, or the Ollama application).
2. Install a local model, for example `ollama pull gemma4:12b-mlx`. The Models screen also supports downloads, cancellation, load, unload, and deletion.
3. Open `dist/Gemma Desktop.app` after building.
4. Choose a project directory, create a conversation, and send a task. Consequential tool calls appear as approvals with their exact arguments. Denying a request returns an error to the agent.

The app defaults to `http://127.0.0.1:11434`, a 32K context, 24 model steps, and approval-required execution. Model weights are managed by Ollama outside the app bundle. Download size is not a promise of working memory usage: context, the runner, and other active applications affect memory requirements.

Closing the window hides it and lets active work continue in the app process. Clicking the Dock icon reopens it. Quitting cancels active work. There is no independent background daemon.

## Build and test

Requires an Apple Silicon Mac, Xcode command-line development tools, Node 22.12+ (or another supported Vite runtime), and a Go bootstrap installation. Scripts select Go 1.26.10 and keep its caches inside this project. The current target machine and exact verification evidence are recorded in [docs/implementation/STATUS.md](docs/implementation/STATUS.md).

```sh
./scripts/icon.sh             # optional: regenerate the original app icon
./scripts/build.sh            # React build, native arm64 app, ad-hoc signature
./scripts/test.sh             # Go race tests, frontend tests and production build
./scripts/package-local.sh    # verified local app archive and SHA-256 checksum
./scripts/dev.sh              # embedded UI + headless app at 127.0.0.1:8787
```

Development mode is explicit, loopback-only, rejects foreign origins/hosts, and is absent from an ordinary app launch. Core logic is independent of Wails in `internal/app`, `internal/engine`, and the other internal packages. Tests may need permission to create local listeners, launch Chrome, or apply a macOS sandbox when run inside another sandbox.

With the development server and a real local Gemma model running:

```sh
python3 scripts/e2e.py
python3 scripts/workflow-e2e.py
GEMMA_LIVE_MODEL=gemma4:12b-mlx ./scripts/live-model-test.sh
```

The coding test creates its own small failing project under `.runtime`, accepts only a narrowly defined edit and Python test command, and independently reruns the tests. It writes actual evidence to `docs/evidence/live-coding-task.json`. It does not modify a user's project.

## Workbench

- **Chats:** streamed answers, preserved conversation context, file attachments, run cancellation, approvals, chronological tool activity, and saved results.
- **Files and changes:** scoped file browsing and Monaco editing with revision checks; Monaco comparison of Git changes and recorded file artifacts.
- **Agents:** named instructions, local model choice, step budgets, and permission modes. The global read-only mode remains authoritative.
- **Workflows:** React Flow editing backed by validated ADK graphs. Agent, condition, approval, parallel, join, output, and dynamic fan-out nodes are compiled in Go. The frontend does not schedule work.
- **Models:** real Ollama inventory, model downloads and residency controls, host memory, and runtime diagnostics.
- **Tools:** local stdio and remote HTTP MCP configuration, workspace-bound discovery and explicit invocation approval. Browser tools use an isolated Chrome profile and approved-origin networking.
- **Skills:** copies a supplied SKILL.md into application storage and exposes it through ADK's skill toolset. Import does not authorize scripts or expand permissions.
- **Artifacts:** retained before/after file content, generated files, and browser screenshots; native open/reveal actions.
- **Harness Lab:** repeatable baseline/candidate comparisons on a bounded frozen text snapshot. It records configuration, input digest, output, timing, usage, and tool-call counts. It grants read-only tools and does not present execution success as an objective quality score.
- **Terminal:** an explicitly approved interactive user shell, separate from agent execution. Agents have no terminal-input API.

## Boundaries and recovery

See [docs/architecture.md](docs/architecture.md) for the ownership model and [docs/security.md](docs/security.md) for precise operational limits.

Workspace tools reject traversal, symlinks, protected credential paths and unsafe file types. Edits require a previously read hash; concurrent external changes cause conflicts. Agent commands run in a macOS Seatbelt profile with no network, no credential directories, a private temporary home, bounded output, and **no child process creation**. Commands requiring subprocess trees are restricted until a broader execution environment is qualified. There is no unrestricted fallback. The user terminal intentionally has user-account privileges and asks for approval separately.

SQLite stores the application journal, configurations, approvals, tool intents/outcomes, artifacts, and full ADK checkpoints. After an unexpected exit, unfinished work is marked interrupted; uncertain tool outcomes are not replayed. A supported native workflow approval checkpoint can be explicitly resumed with its frozen graph and agent definitions. Arbitrary process crashes cannot be safely resumed as if external actions had never occurred.

No raw generated HTML, remote web pages, or active SVG is embedded in the privileged application webview. Markdown disables raw HTML. Monaco workers and frontend assets are bundled locally.

## Distribution

`scripts/build.sh` creates an ad-hoc-signed local app without Developer ID credentials. This is suitable for validation on this Mac. `scripts/package-local.sh` archives that app with a checksum. It is not a notarized public release. `scripts/distribute.sh` uses an explicitly supplied Developer ID identity and a preconfigured notarytool keychain profile; secrets are not written into the repository. Only run it when those distribution credentials and release intent are available.

Dependencies remain under their upstream licenses. See [NOTICE.md](NOTICE.md). The research reports are design evidence; implemented behavior and verification evidence take precedence over proposed research assumptions.
