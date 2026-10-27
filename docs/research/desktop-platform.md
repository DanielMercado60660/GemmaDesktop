# Gemma Desktop desktop platform research

Research date: October 26, 2026. Scope: Wails, the React interface, native macOS behavior, presentation security, and packaging. This is a proposed implementation blueprint; no application has been built or launched.

Wails remains a suitable host for this application, with one important update to the initial discussion: **Wails v3.0.0 became a stable desktop release on October 26, 2026.** Its GitHub release is not marked prerelease and identifies commit `7b11001b92b712210b27d8cb53430e83f67ad8e4`. Pin that release and its matching JavaScript runtime for the first integration build. Its remaining iframe bridge limitation materially affects artifact previews and browser features. [Release](https://github.com/wailsapp/wails/releases/tag/v3.0.0)

## Dependency and host compatibility

| Component | Verified state | Proposed treatment |
| --- | --- | --- |
| Wails Go module and CLI | `v3.0.0`; stable desktop release; source declares Go 1.26.0 and toolchain 1.26.10 | Pin CLI, module, and `@wailsio/runtime` to 3.0.0 together. Avoid floating `@latest`. |
| Go | Host reports 1.26.2; Wails selects 1.26.10 | Use a project-pinned patched toolchain, coordinated with ADK's minimum. Do not silently change the system installation. |
| Node and npm | Host reports Node 22.22.3 and npm 10.9.8 | Node satisfies Vite's documented 22.12+ branch. Lock dependencies with `package-lock.json`. |
| Xcode | Host reports Xcode 27.1, build 27A9275; active developer directory is `/Applications/Xcode.app/Contents/Developer` | Native compilation tooling is present. Still run the pinned Wails doctor and packaged-app smoke test at kickoff. |
| Wails CLI | Not on the inspected PATH | Install the pinned CLI as an explicit build setup step. |
| React | Current official docs identify 19.3; Wails 3.0.0 React template still declares 18.2 | Prefer 19.3 after verifying peer dependencies; update React, React DOM, and types together instead of inheriting template versions accidentally. |
| Vite | Wails template uses `^8.2.4`; release notes identify security fixes in that version | Resolve an exact patched 8.2.x version and record it. Node is build tooling, not a shipped runtime. |
| Tailwind and shadcn/ui | Tailwind 4 uses modern browser features, including a Safari 16.4 floor; shadcn has an official Vite integration | Use CSS variables as design tokens. Review generated components and dependency changes; retain source ownership. |
| Monaco | Latest GitHub release inspected: 0.57.0 | Candidate pin; bundle ESM workers locally and test inside the production WKWebView. |
| React Flow | Latest React package release inspected: `@xyflow/react` 12.12.0 | Candidate pin; use it for editing and displaying graphs, never scheduling execution. |
| xterm.js | Latest release inspected: 6.0.0 | Candidate pin; add only required first-party addons and a separately governed Go PTY service. |

These are candidate versions, not a tested combined lockfile. Wails' generated macOS deployment target is 12.0, while the full product depends on additional runtime, WebKit, and isolation requirements. Establish the product's supported macOS floor from that complete set. The current host is arm64 macOS 27.0.1; native validation here will not establish older-system support. [Wails module](https://github.com/wailsapp/wails/blob/v3.0.0/v3/go.mod), [React template](https://github.com/wailsapp/wails/blob/v3.0.0/v3/internal/templates/react/frontend/package.json.tmpl), [installation](https://v3.wails.io/getting-started/installation/), [React versions](https://react.dev/versions), [Vite requirements](https://vite.dev/guide/), [Tailwind compatibility](https://tailwindcss.com/docs/compatibility), [shadcn Vite](https://ui.shadcn.com/docs/installation/vite), [Monaco release](https://github.com/microsoft/monaco-editor/releases/tag/v0.57.0), [React Flow release](https://github.com/xyflow/xyflow/releases/tag/%40xyflow/react%4012.12.0), [xterm release](https://github.com/xtermjs/xterm.js/releases/tag/6.0.0).

## Backend boundary and desktop transport

Expose narrow desktop façade services, rather than registering internal runtime structs directly. Wails binds exported methods; its services are shared across windows. A frontend argument is a request, not proof of authorization. Backend services must resolve workspace IDs, validate state revisions, and invoke the same policy layer used by agents. Keep raw filesystem roots, process handles, secrets, and database access outside bound APIs. [Service implementation](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/services.go), [binding implementation](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/bindings.go).

Recommended façade groups are Workspace, Conversation, Run, Approval, Artifact, Workflow, Model, ToolConfiguration, and Settings. Commands such as starting a run should return its durable ID quickly. Long-running work belongs to the backend run manager; UI subscriptions can attach and detach independently. A cancellation request identifies a run and is persisted before backend cancellation proceeds. Wails bound calls support cancellation, but cancelling a JavaScript promise is not sufficient evidence that spawned processes and model work have stopped. [Call implementation](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/messageprocessor_call.go).

Persist domain events first and publish notifications second. Each event needs a monotonically increasing cursor within its defined stream, run ID, event type, schema version, timestamp, and typed payload. The desktop reconnects by loading a snapshot plus events after a cursor, deduplicating overlap. This prevents window closure, slow rendering, or a lost notification from corrupting displayed state. Token fragments may be coalesced for transport; approvals, tool boundaries, errors, and completion events must retain their semantics.

Wails 3.0.0 includes `HandleStream` and `StreamConn`, with `Send`, `TrySend`, bounded queues, connection cancellation, and no required TCP listener. Evaluate this for PTY bytes and high-volume updates. Keep ordinary commands on generated bindings and domain notifications on typed events. Stream buffers are transport mechanisms, not durable history. [Stream source](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/stream.go), [event manager](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/event_manager.go).

## Native window behavior and lifecycle

Use Wails' native macOS window, application menu, file dialogs, traffic lights, and window controls. Build the custom header inside the content region; limit drag regions so they do not swallow composer, tab, or editor input. A restrained native translucent sidebar can be evaluated, but visual polish must survive an opaque fallback. Retain public macOS APIs and avoid the `private_mac_apis` build tag unless a specific requirement justifies it. [Window options](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/webview_window_options.go), [macOS packaging](https://v3.wails.io/guides/build/macos/).

Closing a window and quitting the application need different semantics. Wails exposes `ShouldQuit`, cancellable `WindowClosing` hooks, and `ApplicationShouldTerminateAfterLastWindowClosed`; macOS ordinarily keeps the app running after its last window closes. Proposed behavior: closing the window leaves active work in the running app and reopening restores it; Cmd+Q explains active work and then cancels/checkpoints before exit. Continuing after process exit requires a separately designed helper or service. Nothing in an embedded Wails runtime provides that durability automatically. [Lifecycle](https://v3.wails.io/concepts/lifecycle/), [application options](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/application_options.go).

Service startup occurs in registration order and shutdown in reverse order. Register persistence before dependent runtime services. Startup checks for missing Ollama or a model should produce a usable recovery screen instead of blocking the entire interface. Avoid waiting for model loading in UI initialization. Flush durable state continuously; force quit and power loss can bypass shutdown hooks. Sleep/wake must cause endpoint reconciliation and explicit interrupted states where completion is uncertain. [Service lifecycle source](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/services.go).

## Content and WebView security

The Wails 3.0.0 release explicitly says iframe bound-method calls can reach `/wails/runtime` on **all platforms**. Its desktop HTTP transport currently permits requests delivered through its asset handler. Therefore treat any iframe in the privileged app as potentially able to call bound Go methods. The native macOS message callback supplies frame origin information, but that does not remove the separate bound-call transport limitation. [Release security section](https://github.com/wailsapp/wails/releases/tag/v3.0.0), [desktop transport guard](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/transport_http_notserver.go), [macOS bridge](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/webview_window_darwin.m).

Consequences for this product:

- Render model text and Markdown through a constrained renderer with raw HTML disabled. Sanitize any supported rich content and validate link schemes independently.
- Do not mount generated HTML, arbitrary websites, or untrusted SVG documents as active content in the privileged Wails origin. Use inert previews or a separate browser process without the application bridge.
- Keep browser automation in its own controlled browser profile/process. Wails' `Browser.OpenURL` opens the system browser; it is not a browser automation API.
- Bundle scripts, fonts, editor workers, and terminal assets locally. Establish a restrictive production CSP against the actual bridge and worker-loading behavior; prohibit remote executable content and frame embedding. Do not weaken the policy broadly to make a development-server example work.
- Deny unnecessary camera, microphone, geolocation, notification, and clipboard-reading capabilities in window options. Enable individual capabilities only when implemented and requested.
- Keep Wails' experimental app-control MCP build tag out of production. That development server is distinct from the product's user-configured MCP client.

[Browser manager](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/browser_manager.go), [window permissions](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/webview_window_options.go), [MCP build exclusion](https://github.com/wailsapp/wails/blob/v3.0.0/v3/pkg/application/mcp_disabled.go).

## Editor, graph, and terminal integration

Monaco models need stable, unique URIs scoped to workspace and revision. The same relative path in two projects must not collide. Diff originals should refer to captured content, while modified sides refer to an explicit working revision; handle file changes after the diff opens. Dispose models/listeners when ownership ends, preserve view state separately, and cap large/binary-file previews. Monaco is an editor component, not a VS Code extension host. [Monaco concepts](https://github.com/microsoft/monaco-editor/blob/main/README.md).

Use Monaco's ESM integration and Vite's worker support. Verify worker creation, syntax services, fonts, and diff calculation under the packaged Wails origin with networking disabled. A successful localhost development preview does not establish this. Lazy-load Monaco and instantiate it only for visible editor panes. Test resizing, hidden-to-visible transitions, CJK composition, clipboard operations, and tab navigation in WKWebView. [ESM integration](https://github.com/microsoft/monaco-editor/blob/main/docs/integrate-esm.md).

React Flow should maintain two distinct concepts: editable workflow definition and execution projection. Stable backend node IDs link actual events to nodes; layout coordinates do not determine execution. Memoize custom node components and handlers, subscribe narrowly, and update graph statuses only when those statuses change. Token streaming must not redraw the entire graph. Preserve keyboard and screen-reader controls, and offer a textual execution list for complex runs. [Performance](https://reactflow.dev/learn/advanced-use/performance), [accessibility](https://reactflow.dev/learn/advanced-use/accessibility).

xterm only renders a terminal. The backend owns PTY creation, process groups, environment filtering, resizing, cancellation, and permissions. Distinguish an agent's structured command execution from a user-operated interactive terminal. All JavaScript in the terminal page can potentially manipulate terminal I/O, so terminal content, titles, links, and clipboard escape sequences remain untrusted. Never turn terminal output into HTML or use terminal text as authorization. [xterm security](https://xtermjs.org/docs/guides/security/).

Implement flow control with bounded byte queues and write-completion acknowledgments. xterm's `write` is asynchronous; unlimited forwarding can exceed buffers. Use high/low watermarks, cap scrollback, report truncated retained output, and verify that cancellation still responds under a high-volume producer. Fit/resize after layout changes and test renderer fallback in WKWebView rather than requiring WebGL. [Flow control](https://xtermjs.org/docs/guides/flowcontrol/).

## Interface ownership

The visual direction is charcoal, electric blue, spacious typography, subtle orbital texture, and clear task language. No permanent green connected/live dots. Show status where it changes a decision: loading a model, awaiting approval, running tests, failing, or completing with results.

| Proposed area | Responsibility |
| --- | --- |
| `frontend/src/app` | Shell, navigation, command palette, route state, panel resizing, error boundaries. |
| `frontend/src/design` | Tokens, typography, accessible primitives, light/dark themes, restrained motion. |
| `frontend/src/features/workspaces` and `conversations` | Workspace selection, session list, transcript, composer, attachments. |
| `frontend/src/features/runs` and `approvals` | Activity timeline, cancellation, approval details, completion summaries. |
| `frontend/src/features/editor` and `terminal` | Monaco lifecycle, diff views, terminal sessions and transport consumption. |
| `frontend/src/features/workflows` | Definition editor and execution graph, with separate stores. |
| `frontend/src/features/models`, `tools`, `artifacts`, `settings` | Focused management screens and diagnostics. |
| `frontend/src/bridge` | Generated-binding adapter, event subscription/replay, DTO conversion. Generated files stay separate. |
| `internal/desktop` | Wails composition, narrow façade services, menus, dialogs, native window behavior. |
| `build/darwin` | Bundle metadata, icons, entitlements, packaging/signing tasks. |

The main workspace has a compact sidebar, task header, central conversation/results surface, bottom composer, and collapsible inspector with Activity, Graph, and Runtime views. Files, diffs, terminal, and artifacts open as purposeful work panes. Approval details must expose exact affected resources and consequences. Preserve keyboard focus when streaming; announce meaningful status changes rather than each token.

## Packaging and acceptance gates

Produce an arm64 `.app` early using the pinned generated Taskfiles, then a branded DMG. Keep downloaded models and mutable state outside the bundle; changing signed bundle contents invalidates the signature. Build a local ad-hoc-signed app without distribution credentials. Developer ID signing, hardened runtime, notarization, and stapling are a separate distribution gate. Review required entitlements rather than copying permissive signing snippets. Do not ship Wails' default DMG branding. [Packaging](https://v3.wails.io/guides/build/macos/).

Required desktop acceptance checks:

1. Launch the packaged app without a dev server; all UI assets and Monaco workers load offline.
2. Prove command binding, streamed output, cancellation, and event catch-up after closing/reopening the window.
3. Exercise Cmd+Q, force quit, app restart, and sleep/wake during model, tool, and approval states.
4. Attempt hostile Markdown, links, HTML artifacts, terminal titles, and iframe payloads; no unauthorized backend action occurs.
5. Open and resize diffs and terminal panes repeatedly; verify resource disposal and output backpressure.
6. Navigate the full task and approval flow with keyboard and VoiceOver; test reduced motion and readable contrast.
7. Verify real native window controls, menus, dialogs, fullscreen, display scaling, and multiple-monitor restoration.
8. Inspect the final bundle and DMG, confirm no development MCP/debug server, and run the real end-to-end workflow from that bundle.

Browser tests can verify React behavior and rendering quickly, but production WKWebView and native lifecycle tests remain required. Outstanding decisions are the supported macOS floor, packaged worker/CSP behavior, final frontend lockfile, and whether unattended work must survive explicit application quit.
