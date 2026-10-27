# Third-party components

Gemma Desktop is independent software and is not endorsed by Google, Ollama, Wails, or the other upstream projects. Gemma weights are obtained separately through the user's local Ollama runtime and retain the model publisher's applicable terms.

The application depends on upstream packages rather than copying their source into its own modules. Preserve their notices when distributing their compiled components. Exact selected versions and integrity hashes are in `go.mod`, `go.sum`, and `frontend/package-lock.json`.

Principal components include Google ADK-Go and GenAI SDK (Apache-2.0), Wails (MIT), Ollama's public API (runtime distributed separately), the official MCP Go SDK (MIT), chromedp (MIT), modernc SQLite and its dependencies (see their package-specific licenses), React (MIT), Vite (MIT), Tailwind CSS (MIT), Radix UI (MIT), shadcn-style components, Monaco Editor (MIT), React Flow (MIT), xterm.js (MIT), Lucide icons (ISC), and react-markdown/remark (MIT). Harness² and Gemini CLI informed architectural research; their implementations were not wholesale ported.

Repository licensing for original application source should be chosen by the project owner before public distribution. This file is an attribution inventory, not a replacement for upstream license texts or a grant of rights to model weights.
