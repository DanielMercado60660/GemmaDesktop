# Security and operational limits

The host controls permissions. Prompts, repository instructions, skills, MCP descriptions, and web content cannot grant authority. Read-only mode removes mutating tools and is checked again by the executor. Each consequential operation requires a host approval tied to its exact detached arguments. Decisions are persisted before delivery and cannot be answered twice.

File access uses a selected root and pinned directory descriptors. Absolute/parent traversal, symlink components, credential directories, `.git` writes, and agent-policy files are protected. Reads and edits accept regular bounded UTF-8 files. Edits use the full original hash and preserve recorded before/after contents. There is no portable atomic compare-and-swap against arbitrary external editors; changes detected before replacement fail with a conflict. Root confinement does not by itself isolate processes.

The model executor therefore uses a separate macOS sandbox. It denies network and child process creation, clears inherited credentials/environment, confines writes to the workspace/private temporary directory, and bounds output/time. Failure to apply the sandbox is an error. Git inspection uses fixed arguments and disables hooks, external diff/textconv, fsmonitor, and interactive prompts. Broader build chains requiring subprocesses need a future qualified container executor. The application does not start Docker or install Apple's container runtime implicitly.

The interactive terminal is an explicit user-account shell. Its approval explains that it can access user files and networking. It is not exposed as an agent tool. Shell startup files and inherited secret variables are not loaded.

Ollama uses a loopback-only origin with proxy inheritance and redirects disabled. Model metadata is inspected for remote targets before inference. The app does not change an unrelated shared Ollama configuration or terminate a shared daemon. The validation daemon was started with cloud disabled.

MCP connections are bound to canonical workspace roots and frozen configuration. Schemas are validated and hashed. Tool calls are independently approved; protocol roots are not treated as isolation. Stdio servers run under the restricted executor, so servers requiring child processes or unrestricted files may fail. HTTP endpoints are pinned and redirect constrained. Do not place credentials in command arguments or URLs. Authentication flows beyond the exposed configuration are not claimed as verified.

Browser tasks use a fresh Chrome profile without the Wails bridge. An origin proxy permits only the approved origin, blocks unapproved redirects/resources, and validates destinations. Browser clicks and text entry are separately approved, and credential fields are unavailable. The ordinary application never renders arbitrary active HTML or web content in its privileged origin.

Database recovery expires outstanding approvals, marks unfinished runs/experiments interrupted, and labels started tool intents with no outcome as uncertain. It does not replay commands, writes, MCP actions, or browser clicks. Only explicit workflow approval checkpoints have supported resumption. Quitting the application stops its owned work; there is no guarantee of execution after process exit.

The experimental evaluator cannot change these policies. Both configurations receive the same frozen bounded text inputs and read-only tools. Timing and model output are evidence; correctness still requires a task-specific rubric or independent verification.
