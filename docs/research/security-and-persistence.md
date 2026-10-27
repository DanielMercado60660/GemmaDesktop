# Gemma Desktop execution security and persistence

Research date: 26 October 2026. These are proposed implementation requirements, supported by the sources linked below. Runtime enforcement still needs the acceptance tests in [verification-plan.md](verification-plan.md).

The Go host must own authorization, durable execution records, and access to secrets. ADK, the model, repository instructions, imported skills, browser pages, and MCP servers may request operations; none can expand their own authority. Apply the same checks to frontend commands and agent tool calls. A terminal, artifact opener, or workflow editor must not become an alternate route around those checks.

## Execution boundaries

Use a small set of explicit capabilities: reading selected workspace files, preparing changes, applying an approved change set, executing processes in a qualified environment, and contacting specified services. An approval binds the exact operation, normalized arguments, workspace, execution environment, destination, policy version, and relevant file revisions. If those change while the approval waits, prepare a fresh request. Expired approvals and approvals from a previous policy version must not silently authorize a different action.

Record allow, deny, and approval-required decisions with the rule that caused them. A user can grant a narrow capability for a run or workspace; the model cannot write a configuration file that grants itself the same capability. Imported project instructions and skills remain task context. Keep application policy outside writable agent workspaces.

Gemini CLI supplies useful examples of a host policy engine, approval modes, and policy precedence. Its inspected documentation also says workspace policy files are currently disabled and contains inconsistent numerical examples. Borrow the separation of policy from model reasoning, not its configuration wholesale. [Policy documentation at inspected commit](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/docs/reference/policy-engine.md)

## Files and concurrent edits

Use an opened `os.Root` for built-in workspace file operations, with additional checks for allowed file types, protected application/repository metadata, size, and requested access. Go documents traversal and escaping-symlink protection, but `Root` is not a process sandbox and does not prevent access to devices or crossing filesystem boundaries. Its metadata-changing methods have platform caveats. Avoid exposing broad ownership, permission, or link manipulation tools until separately qualified. [Go Root reference](https://pkg.go.dev/os@go1.26.2#Root), [traversal-resistant API rationale](https://go.dev/blog/osroot)

Canonicalize and record the root selected by the user, then work relative to that handle. A path prefix test or `EvalSymlinks` followed by a normal open is insufficient against races. Reject device files, sockets, and unexpected special files. Hard links deserve explicit policy: an in-root name can refer to the same inode as a file elsewhere. Prefer private working copies for untrusted execution and atomic replacement of regular output files over modifying a shared inode in place.

Proposed edit transaction:

1. Read the allowed file and record its content hash, encoding, newline style, and identity.
2. Construct a bounded patch with exact, unambiguous matching and show its resulting diff.
3. Save the before image before entering the final commit section.
4. Acquire the application's per-workspace write lease; recheck the expected revision and authorization.
5. Write a temporary sibling, flush as required, replace atomically, and record the resulting hash and artifact.
6. If external edits conflict, preserve both versions and ask for resolution rather than overwriting them.

An application lock coordinates our agents, not an unrelated editor. Atomic rename is not compare-and-swap. The Qwen Code edit implementation explicitly acknowledges this remaining race despite multiple freshness checks. Stronger isolation comes from operating on a private snapshot and merging a reviewed change set back into the user's checkout. [Qwen edit implementation](https://github.com/QwenLM/qwen-code/blob/34769f9d41a1ea253a000285f3c32e60fac6fb26/packages/core/src/tools/edit.ts)

For the initial product, allow one writer per workspace and use separate copies/checkouts for concurrent mutating tasks. Git worktrees share repository refs and configuration; they separate working files but do not isolate privileges. A Linux sandbox should not receive a writable mount of the user's common Git directory merely to make a linked worktree function. [Git worktree documentation](https://git-scm.com/docs/git-worktree)

Use controlled Git invocations with structured arguments, a sanitized environment, disabled pagers and prompts, and bounded output. Explicitly disable external diff drivers and text conversion for read-only diff inspection. Review hooks, filters, credential helpers, submodules, and configuration before mutation commands. A command named `git` is not intrinsically read-only. [Git diff options](https://git-scm.com/docs/git-diff#Documentation/git-diff.txt---no-ext-diff)

## Shell and macOS isolation

Command allowlists, timeouts, and approvals are useful controls but do not confine an executed test suite. A repository can put arbitrary code behind `npm test`, a Makefile, or a compiler plugin. Use a process executor with explicit executable and arguments, a minimal environment, a private temporary directory, process-group cancellation, and limits on CPU, memory where supported, time, output, and descendants. Do not inherit shell startup files or the entire GUI application's environment.

The preferred first isolation experiment is Apple's `container` CLI, accessed from Go as an external process. Version 1.6.1 was the current release inspected; it includes fixes to signal forwarding. It runs Linux containers on Apple Silicon, offers mount/resource controls, and requires the container service. It is not installed on the inspected PATH. Use the CLI rather than building a Swift management layer. [Release](https://github.com/apple/container/releases/tag/1.6.1), [versioned command reference](https://github.com/apple/container/blob/1.6.1/docs/command-reference.md)

Default container networking uses NAT. An isolated named network separates container networks; it does not by itself prove internet egress is blocked. The release's `network create --internal` selects host-only mode, which still needs host-service exposure testing. The initial implementation must demonstrate deny-by-default egress, including direct IPs, IPv6, DNS, redirects, and proxy bypass. A proxy environment variable alone is not enforcement. Deny sandboxed tools and local MCP subprocesses direct access to host Ollama, including gateway aliases: its local API can forward to cloud models and bypass the application's provider policy. Only the authorized provider broker should reach it. [Networking](https://github.com/apple/container/blob/1.6.1/docs/networking.md), [network creation source](https://github.com/apple/container/blob/1.6.1/Sources/ContainerCommands/Network/NetworkCreate.swift), [Ollama cloud routing](https://docs.ollama.com/api/authentication)

Mount only a private workspace snapshot and designated output directory. Do not mount the user's home, runtime socket, SSH agent, browser profile, application database, or credential store. Pin execution images by digest and treat dependency-install steps as code execution. File boundaries, process limits, and network restrictions are independently tested properties. Cancellation must stop the owned execution unit, including detached or reparented descendants; process-group signaling alone does not prove this. Persist ownership IDs so an interrupted container CLI does not leave an untracked running workload.

An interactive terminal is a separately granted user capability. Agents use structured process tools and cannot inject input into a more privileged user terminal to bypass approval. Do not claim exact-command authorization by parsing arbitrary terminal keystrokes. Terminal titles, links, clipboard escape sequences, and output are untrusted; bound retention and transport queues.

Native Xcode/macOS commands require a different executor. The local `sandbox-exec` manual marks it deprecated; it should not be the sole long-term isolation strategy. Gemini CLI and OpenWorker demonstrate Seatbelt profiles, but their existence does not prove our profile safe or stable on the target OS. Keep native execution disabled unless its declared boundary passes dedicated tests. A narrowly qualified native helper is a later implementation option. [Gemini sandbox guide](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/docs/cli/sandbox.md), [OpenWorker macOS sandbox design](https://github.com/andrewyng/openworker/blob/main/docs/macos-sandbox.md)

## MCP integration

The official Go SDK v1.8.0 supports the 2026-07-28 protocol as well as earlier revisions. Its current lifecycle includes discovery and per-request metadata, with fallback to the legacy initialization exchange. Roots, sampling, and logging are deprecated in the new protocol, although compatibility remains. Do not build filesystem security on MCP roots. [SDK release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0), [protocol support](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md), [client feature support](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/client.md)

Proposed service contract:

- A configured server has a stable identity, transport, exact executable/endpoint, credential reference, permitted capabilities, and configuration hash.
- Local stdio servers run through the process execution boundary. A host process launched with the SDK's `CommandTransport` does not acquire a sandbox automatically.
- Remote servers use Streamable HTTP and the SDK's negotiated protocol behavior. Legacy SSE is compatibility support, not the default new configuration.
- Tool discovery stores names and schema hashes. Tool descriptions and annotations are untrusted. Changed schemas invalidate relevant standing approvals.
- Every invocation passes through the application tool broker, with argument/result size limits, deadlines, progress, audit records, and clear distinction between transport errors and `IsError` tool results.
- Pagination, tool-list changes, disconnection, reconnect, elicitation, and cancellation require explicit UI states. Unattended elicitation pauses; it never invents a user's answer.

The SDK includes client OAuth support and discovery protections, but a custom HTTP client can bypass portions of its SSRF defenses. Preserve these checks when adding proxies or transport wrappers. Bind credentials to the intended issuer/resource and use an explicit loopback OAuth callback with state/PKCE validation. Never pass a token obtained for one service to another. Keep credentials in a platform secret store, with references rather than values in SQLite. [SDK authorization and security sections](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md#authorization), [MCP security guidance](https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices)

Cancellation ends our wait but cannot prove that a remote server did not complete a side effect. Preserve an `outcome_unknown` state and reconcile using service evidence or an idempotency key before retrying a write.

## Browser and artifact previews

Use an owned Chromium process with its own disposable profile, controlled through CDP. The proposed Go client is chromedp v0.21.0, whose newer generic action API differs from many older examples. It supports pipe-based control; avoid exposing an unauthenticated debugging listener. Keep site isolation enabled and handle cross-origin iframe targets explicitly. [Release](https://github.com/chromedp/chromedp/releases/tag/v0.21.0), [versioned API guide](https://github.com/chromedp/chromedp/blob/v0.21.0/docs/API.md)

Do not attach to the user's ordinary browser profile. Chrome's remote-debugging changes explicitly require a non-default profile for normal Chrome and recommend Chrome for Testing for automation. [Chrome documentation](https://developer.chrome.com/blog/remote-debugging-port)

Give each run an owned tab/context and serialize operations within that tab. Separate browser lifetime from individual operation timeouts. Approvals cover uploads, form submissions, account changes, downloads outside the artifact store, and other consequential actions. Navigation checks also cover redirects, subresources, WebSockets, local files, and private network targets; a URL check on the initial navigation alone cannot claim network isolation. A disposable profile separates state, not network or filesystem privileges. Qualify worker/service-worker traffic, WebRTC, DNS rebinding, and loopback service access, retain Chromium's sandbox, and prohibit `--no-sandbox` in the qualified configuration.

The Wails release's iframe binding warning is a release gate: generated HTML, SVG with active content, arbitrary remote pages, and notebook output must not execute in the privileged application webview. Render inert, sanitized representations there. Interactive previews belong in a separate browser/profile or a genuinely unprivileged viewer with a proven boundary. See [desktop-platform.md](desktop-platform.md).

## Durable state and recovery

Use one selected SQL driver registration, one application-owned SQLite policy and migrations, with an ADK `session.Service` adapter where needed. Both `glebarez/go-sqlite` and the top-level `modernc.org/sqlite` package register `sqlite`; importing both can panic. ADK's database service can accept a compatible app-owned GORM connection, or a custom conformant session service. Keep the database on local application storage, not inside a user's repository or a network/synchronized workspace. Large logs, images, and output files belong in a bounded artifact store; rows contain metadata, hashes, and references.

The ADK dependency investigation found modernc SQLite v1.46.1's darwin/arm64 engine documented as SQLite 3.51.2. SQLite's WAL reset bug is fixed in 3.51.3 and later, or designated backports. An app dependency may upgrade the selected driver, so verify the actual runtime using `SELECT sqlite_version()` and record its build. Do not assume a recent Go module number proves the embedded engine safe. See [ADK dependency analysis](adk-runtime.md), [modernc v1.46.1](https://pkg.go.dev/modernc.org/sqlite@v1.46.1), [SQLite WAL fix](https://www.sqlite.org/wal.html#walresetbug).

Use short transactions, an owned writer queue, a small reader pool, busy timeouts, and foreign-key settings applied to every connection. Approvals and tool intents must be durable before execution; publish committed state changes after commit. Token deltas may use a separate bounded transient stream and periodic flushes to application draft records. ADK's released runner/session service skip partial events, so do not depend on its `AppendEvent` for those drafts. A final non-partial event finalizes the draft without duplicating text and is committed before completion. WAL allows concurrent readers but still serializes writers. [SQLite WAL behavior](https://www.sqlite.org/wal.html), [ADK persistence behavior](adk-runtime.md)

Proposed records:

| Record group | Purpose |
| --- | --- |
| Workspaces and workspace snapshots | User-authorized roots, repository state, write lease, original revisions |
| Agent and workflow versions | Immutable instructions, model settings, skill hashes, compiled graph identity |
| Sessions, messages, runs, node attempts | Conversation plus actual execution state and parent-child relationships |
| Execution events | Versioned payload, monotonic sequence, source event ID, run/node/tool IDs, causality |
| Tool intents and results | Requested operation, policy decision, dispatch attempt, external operation ID, outcome |
| Approvals and grants | Exact scope, request digest, expiry, revocation, provenance |
| Artifacts and change sets | Before/after hashes, media type, sizes, verification references, retention |
| Model/runtime configurations | Exact tags/digests, observed capabilities and tested version combination |

A run moves through queued, running, waiting for approval, cancelling, and terminal states. After a crash, an interrupted side-effecting call becomes uncertain until reconciled. Replay rebuilds the interface; it does not redispatch tools. Freeze the workflow definition and tool/configuration hashes per run, and reject incompatible resume. Preserve durable terminal events even when a client is disconnected.

Back up with SQLite's online backup API or a qualified `VACUUM INTO` workflow. Copying only an active `.db` file ignores its WAL state. Test restoration and migration failure on real backup fixtures. [SQLite backup documentation](https://www.sqlite.org/backup.html)

## Security acceptance conditions

Before broad autonomy, demonstrate blocked path traversal/symlink races, preserved concurrent edits, confined child processes, denied secret access and egress, stale approval rejection, hostile MCP schema handling, unprivileged artifact previews, and restart without repeating external writes. Use synthetic credentials to test logs, model/tool traces, browser headers, terminal input, exports, and backup retention. Keep known credentials out of ordinary diagnostics; arbitrary secret detection cannot be guaranteed. The interface should state the actual execution environment and report uncertainty honestly. No prompt-injection filter or model reviewer substitutes for those controls.
