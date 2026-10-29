# Third-party components

Gemma Desktop is independent software and is not endorsed by Google, Ollama, Wails, or the other upstream projects. Gemma weights are obtained separately through the user's local Ollama runtime and retain the model publisher's applicable terms.

Most components are upstream package dependencies. Preserve their notices when distributing compiled components. Exact selected versions and integrity hashes are in `go.mod`, `go.sum`, and `frontend/package-lock.json`. The Harness² core is also vendored as described below.

Principal components include Google ADK-Go and GenAI SDK (Apache-2.0), Wails (MIT), Ollama's public API (runtime distributed separately), the official MCP Go SDK (MIT), chromedp (MIT), modernc SQLite and its dependencies (see their package-specific licenses), React (MIT), Vite (MIT), Tailwind CSS (MIT), Radix UI (MIT), shadcn-style components, Monaco Editor (MIT), React Flow (MIT), xterm.js (MIT), Lucide icons (ISC), and react-markdown/remark (MIT). Gemini CLI and the other reference applications informed architectural research.

Google Research's Harness² Python core is retained unmodified at revision `1cfe254e8d25b15f37048c3730134686abe60335` in `third_party/harness2`, together with its [upstream license](third_party/harness2/LICENSE) and [per-file provenance manifest](third_party/harness2/UPSTREAM.json). The packaged app includes those resources. Project-owned adapters live in `scripts/harness` and invoke the pinned core through the Go-owned solver boundary. Local compatibility tests do not reproduce or imply Google's published benchmark results.

Original Gemma Desktop application code and documentation are licensed under the [MIT License](LICENSE), copyright 2026 Daniel Mercado. Vendored third-party code retains its upstream license. This file is an attribution inventory, not a replacement for upstream license texts or a grant of rights to model weights.
