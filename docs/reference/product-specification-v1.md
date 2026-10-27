# Gemma Desktop
## Full Product Specification & Autonomous Codex Build Challenge

**Version:** 1.0 — Full implementation brief  
**Target:** macOS / Apple Silicon  
**Primary language:** Go  
**Frontend:** Wails 3 + React + TypeScript  
**Inference:** Ollama / Gemma 4 / MLX  
**Agent framework:** Google ADK-Go v2  
**Development objective:** Complete the entire application autonomously

---

## 1. Mission

Build **Gemma Desktop**, a polished, local-first AI agent desktop application inspired by Google's Gemma ecosystem and agent engineering technologies.

Gemma Desktop should combine capabilities found in coding agents, AI coworker applications, and general-purpose agent workbenches.

It must provide a capable agent runtime, a modern desktop interface, sophisticated tool execution, local model management, multi-agent orchestration, and observable workflows.

The project must be implemented as a real, maintainable, working application.

### Critical instruction: Complete the entire project

**You are being evaluated on your ability to independently complete a major software engineering project from a high-level specification.**

You are not being asked to build a prototype, create a scaffold, or demonstrate only the first development milestone.

You are responsible for executing the **entire project through completion**.

- Research the dependencies and APIs.
- Design the architecture.
- Implement every required subsystem.
- Integrate the components.
- Write and execute tests.
- Diagnose and repair failures.
- Build and polish the desktop interface.
- Complete the workflow and tool integrations.
- Package the macOS application.
- Produce comprehensive documentation.
- Verify that the finished application works.

Continue automatically from one milestone to the next. Do not stop after M0, M1, M2, or any other intermediate stage to request authorization to continue.

Make reasonable technical decisions independently. If an initial design proves unsuitable, investigate alternatives, document the reasoning, and implement a working solution.

Only request user intervention when genuinely necessary for an external dependency, such as unavailable hardware, credentials, legally required consent, or a consequential action requiring authorization. Never bypass permission boundaries to avoid a blocker.

Do not represent unfinished functionality as complete. If an external blocker truly prevents completion, document exactly what remains and preserve all completed, verified work.

**The challenge is the final product, not the intermediate code output.**

---

## 2. Core architecture

Gemma Desktop is primarily a Go application.

### Backend

Use:

- Go as the primary implementation language.
- Google ADK-Go v2 for agent execution and orchestration.
- Ollama for local inference management.
- SQLite for persistence.
- The official MCP Go SDK.
- Standard Go libraries where practical.
- Proven third-party packages for specialized functionality.

The application backend should handle agent lifecycle, model communication, workspace access, tool execution, authorization, session history, workflow management, and execution observability.

### Desktop

Use:

- Wails 3.
- React.
- TypeScript.
- Vite.
- Tailwind CSS.
- shadcn/ui.
- Monaco Editor.
- React Flow.
- xterm.js where a terminal component is needed.

Prefer a modular Go backend embedded in the Wails application initially. Add a separate background service if required for reliable unattended tasks, crash isolation, or independent clients.

Avoid Electron and avoid building a large Swift interoperability layer.

The core Go agent runtime must remain usable independently of the desktop interface.

---

## 3. Model inference

Ollama is the default inference layer.

Use Gemma 4, preferring the MLX runtime on supported Apple Silicon hardware.

Primary model candidate:

`gemma4:12b-mlx`

Additional supported models:

- `gemma4:e2b-mlx`
- `gemma4:e4b-mlx`
- `gemma4:26b-mlx`
- `gemma4:31b-mlx`
- Compatible Ollama GGUF models.

Implement model discovery, installation, selection, loading, unloading, streaming, cancellation and error handling.

Detect the host's hardware capabilities and available memory. Recommend appropriate models rather than assuming every Mac can run the largest configurations.

Build an inference abstraction around Ollama's API so additional model providers may be integrated later.

Use available ADK model interfaces where practical, creating a dedicated adapter when necessary for reliable streaming, tool calls, model configuration and metadata.

Do not rebuild MLX, llama.cpp or Ollama.

Benchmark supported model backends before making claims about their relative performance.

---

## 4. Agent runtime

Google ADK-Go v2 is the primary orchestration framework.

Study Gemini CLI for agent-engineering patterns, including:

- Agent lifecycle.
- Tool contracts.
- System instructions.
- Context management.
- File-editing strategies.
- Permission handling.
- Error recovery.
- Hooks and extensibility.
- Execution checkpoints.

Do not wholesale-port Gemini CLI or build a competing orchestration framework.

Implement:

- Configurable agents.
- Agent instructions and capabilities.
- Workspaces and context.
- Tool execution.
- Task planning.
- Multi-turn execution loops.
- Session persistence.
- Context management.
- Cancellation and execution budgets.
- Delegation.
- Artifact generation.
- Verified completion summaries.

Use ADK's existing mechanisms wherever they provide the required functionality.

### Graph orchestration

Support:

- Sequential execution.
- Conditional routing.
- Parallel execution.
- Fan-out/fan-in.
- Dynamic graphs.
- Multiple cooperating agents.
- Human approval nodes.
- Execution persistence.
- Recovery and resumption.

Combine open-ended agent loops with structured graph workflows.

A graph may orchestrate multiple autonomous agents, each with its own tools, model configuration and execution limits.

---

## 5. Tooling and execution

Create a centrally governed tool execution layer.

Initial capabilities must include:

**Filesystem:** Reading, searching, listing, creating, editing, moving and managing workspace files.

**Coding:** Applying patches, examining diffs, searching source code, running tests and inspecting build output.

**Git:** Repository status, diffs, history, branches and checkpoint-related operations.

**Shell:** Controlled process execution with cancellation, timeouts and output limits.

**MCP:** Discovering, configuring and calling external MCP servers through the official Go SDK.

**Browser:** Browser automation using a suitable maintained interface such as Chrome DevTools Protocol, with explicit permissions.

**Artifacts:** Recording and presenting files generated or modified by the agent.

All tools must use a consistent execution contract with argument validation, scoped permissions, clear errors, structured results and audit history.

### Security requirements

Authorization is enforced by the host application, not by instructions to the model.

Implement:

- Explicit workspace boundaries.
- Filesystem path validation and symlink escape protection.
- Read-only and approval-required execution modes.
- Consequential operation approvals.
- Shell execution restrictions.
- Controlled network permissions.
- Protection against prompt-injection attempts.
- Credential protection.
- Tool execution logging.
- Cancellation and resource limits.

Research appropriate macOS execution isolation, including Apple's Containerization framework.

If a genuinely secure sandbox is unavailable, restrict risky functionality instead of silently running unrestricted tools.

---

## 6. Persistence, recovery and observability

Use SQLite with versioned schema migrations.

Persist:

- Workspaces.
- Agent configurations.
- Sessions.
- Runs.
- Tool calls.
- Permissions and approvals.
- Artifacts.
- Workflow definitions.
- Execution events.
- Checkpoints.
- Model configurations.

Create a typed execution event system supporting live frontend updates and historical replay.

The UI should be capable of showing:

- What the agent is doing.
- Which tools are being used.
- What files were modified.
- Which agent or workflow node is active.
- Whether an operation succeeded or failed.
- Why an approval is needed.
- Model and runtime performance.
- Available artifacts and final results.

Persist agent state and distinguish it from external side effects. Do not blindly replay non-idempotent operations after crashes.

Provide structured logs and useful diagnostics without recording secrets unnecessarily.

---

## 7. Desktop UI and design reference

**Visual inspiration:**  
https://deepmind.google/models/gemma/

**Provided visual reference:**  
Attach the generated Gemma Desktop concept image and save it within the project as:

`design/reference/gemma-desktop-concept.png`

Use that image as the principal visual direction for the desktop application.

It is a conceptual mockup, not an exact pixel specification. Reproduce its overall design language, layout quality, hierarchy and feeling rather than blindly copying every label or example value.

### Visual identity

Take inspiration from the official Gemma aesthetic:

- Deep charcoal and near-black surfaces.
- Luminous electric-blue accents.
- Subtle dotted geometric patterns.
- Orbital and constellation-like decorative elements.
- Refined typography.
- Softly illuminated active states.
- Controlled gradients.
- Minimal, polished component borders.
- Spacious, consistent layouts.

The desktop should feel sophisticated, cohesive and distinctly associated with the Gemma model ecosystem.

Create an original application identity. Do not imply official Google endorsement or unnecessarily copy protected branding assets.

### Primary interface

The application should provide:

**Navigation:** Workspaces, Chats, Agents, Workflows, Models, Tools, Artifacts and Settings.

**Main workspace:** Conversational agent interface, tool execution history, progress indicators, file views, code diffs and task artifacts.

**Execution monitoring:** Live workflow visualization, active operations, completion states and failures.

**Model information:** Selected model, runtime connection, inference performance and memory utilization.

**Interaction:** Prompt composer, attachments, model selection, approval controls, cancellation and keyboard shortcuts.

### Usability requirements

- Native-feeling macOS window.
- Responsive and resizable panels.
- Professional dark theme.
- Optional light theme.
- Smooth streaming updates.
- Syntax-highlighted source code.
- Monaco-powered diff viewer.
- Clear tool-call and approval displays.
- Excellent empty, loading and error states.
- Accessible interaction patterns.
- Appropriate keyboard navigation.
- No distracting or excessive animations.

Build a visual experience consistent with the provided concept image while making practical improvements where needed.

**The UI must be functional.** Decorative mockups, simulated model activity and nonfunctional interface controls do not satisfy this requirement.

---

## 8. Workflow visualization

Use React Flow or an equivalent mature React library.

Provide an interactive graphical representation of ADK workflows.

Show:

- Agent nodes.
- Tool execution nodes.
- Conditional branches.
- Parallel operations.
- Node status.
- Active execution.
- Completed operations.
- Failed operations.
- Approval checkpoints.
- Execution relationships.

Allow the user to inspect node details and related execution events.

Support defining and executing reusable workflows, with validation before execution.

Do not create a second workflow scheduler inside the React frontend. The Go/ADK backend remains authoritative.

---

## 9. Additional capabilities

Implement the following as part of the completed product:

### MCP support

- Local and remote server configuration.
- Connection status.
- Tool discovery.
- Scoped authorization.
- Tool invocation.
- Error handling.

### Skills

Integrate with ADK's existing skill abstractions where appropriate.

Support reusable agent instructions, workflow templates and tool configurations.

### Model management

Provide a usable interface for installed models, model downloads, hardware suitability, selection and runtime configuration.

### Artifacts

Provide a first-class artifact experience for completed work.

Users should be able to inspect, open and locate generated deliverables.

### Application lifecycle

Support startup checks, graceful shutdown, durable state, recoverable failures and practical diagnostic reporting.

### Packaging

Create a runnable macOS application bundle.

Provide build and distribution scripts. Support code signing and notarization when the necessary credentials are available. Missing distribution credentials must not prevent building and validating an unsigned local application.

---

## 10. Harness Lab — experimental capability

Study:

https://github.com/google-research/harness2

Use its ideas to develop an optional experimental harness optimization and evaluation interface.

Explore configurable harness components including:

- System instructions.
- Tool guidance.
- Memory strategies.
- Skills.
- Agent delegation.
- Workflow configurations.
- Scripts and plugins.
- Model execution parameters.

Allow users to compare candidate configurations against a baseline using repeatable evaluation tasks.

Capture performance evidence and configuration changes.

Separate experimental harness configurations from application security enforcement. Harness optimization must never disable permission boundaries.

Keep the experimental system modular so limitations do not compromise the core desktop experience.

---

## 11. Development execution plan

Use the following milestones to manage implementation internally.

**These milestones are not stopping points. Continue automatically through the entire plan.**

### M0 — Foundation
Create the repository, Go module, Wails application, React frontend, backend architecture, development tooling and automated tests.

### M1 — Model and agent integration
Implement Ollama connectivity, Gemma 4 inference, ADK-Go integration, streaming, tool calling and basic agent execution.

### M2 — Coding and file workflows
Implement workspace tools, file editing, Git, shell execution, permissions, persistence and code verification.

### M3 — Complete desktop experience
Implement the polished frontend following the supplied image, including workspace navigation, conversations, live progress, diffs, artifacts and model controls.

### M4 — Advanced orchestration
Implement multi-agent workflows, dynamic ADK graphs, live graph visualization, approvals, resource limits and recovery.

### M5 — Full integration and product completion
Finish MCP, skills, browser functionality, runtime diagnostics, application lifecycle, packaging, documentation and comprehensive testing.

### M6 — Final quality assurance
Run end-to-end workflows, address defects, verify the application on the target Mac, inspect the visual interface, and resolve integration and usability problems.

Repeat debugging and verification until all achievable completion criteria are satisfied.

Do not substitute mock services or hardcoded demonstrations for working production integrations. Mocks are permitted in automated tests.

---

## 12. Definition of done

The project is complete only when:

- The macOS desktop application builds and launches.
- Ollama connectivity works with a real local Gemma model.
- ADK-Go successfully orchestrates live agent execution.
- The application performs meaningful filesystem and coding tasks.
- Tool permissions are enforced.
- Users can inspect and approve consequential operations.
- Sessions and execution history persist.
- The interface substantially implements the provided visual design.
- Model management is functional.
- MCP integration is functional.
- Multi-agent workflows and graph visualization operate.
- Required browser and skills capabilities have working implementations.
- The application reliably reports and recovers from supported failures.
- Automated tests pass.
- A real end-to-end task is successfully completed.
- An installable local macOS build is produced.
- Project documentation explains setup, architecture, operation and testing.

Additionally, the final report must include:

1. Implemented features.
2. Architecture summary.
3. Build commands and test results.
4. Evidence from actual end-to-end execution.
5. Location of the finished application.
6. Known limitations and any externally blocked requirements.
7. Relevant screenshots of the implemented interface.

No placeholder feature may be described as implemented.

---

## 13. Key upstream references

- Google ADK-Go: https://github.com/google/adk-go
- ADK documentation: https://adk.dev/llms-full.txt
- Gemini CLI: https://github.com/google-gemini/gemini-cli
- Ollama: https://github.com/ollama/ollama
- Ollama API: https://docs.ollama.com/api
- Gemma 4 models: https://ollama.com/library/gemma4
- Wails 3: https://wails.io
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- Qwen Code: https://github.com/QwenLM/qwen-code
- OpenWork: https://github.com/different-ai/openwork
- OpenWorker: https://github.com/andrewyng/openworker
- Harness²: https://github.com/google-research/harness2
- A2A Go: https://github.com/a2aproject/a2a-go

Review relevant implementation details and current documentation rather than assuming compatibility based on project descriptions alone.

Preserve applicable license and attribution requirements.

---

## 14. Final directive to Codex

**Build Gemma Desktop completely.**

You are being given substantial autonomy to design, research, implement, test, debug and improve this software.

Your ability to independently deliver a complete, working product is the central purpose of this experiment.

Plan internally and use milestones to maintain engineering discipline, but do not stop at intermediate achievements or return the project simply because the initial vertical slice works.

Make decisions, investigate failures, correct mistakes, refactor as needed, and keep progressing toward the finished application.

Treat the visual reference as a real product design goal.

Treat end-to-end functionality, reliability, security, usability and maintainability as equally important engineering requirements.

If a subsystem fails, diagnose and repair it. If an upstream library is unsuitable, document the evidence and use a justified alternative while preserving the architectural goals.

You should seek user intervention only for genuinely blocked external dependencies or actions requiring authorization.

**Deliver the finished Gemma Desktop application, its source code, tests, documentation, and executable macOS build—not just a demonstration of how it could be built.**