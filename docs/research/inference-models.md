# Gemma Desktop inference and model management research

Research date: October 26, 2026. Target: Apple Silicon macOS. The recommended starting point is a native Ollama HTTP adapter, one explicitly selected local Gemma model, and application-controlled inference admission. Model availability, runtime compatibility, and task quality must be qualified separately.

## Compatibility baseline

The current official Ollama release is **v0.40.11**, published October 24, 2026. Its tag resolves to commit `675150d6ad2747df1e218ef59de9194bc4a7e538`; source observations below refer to that release unless specified. This is a candidate qualification target, not proof that an upgrade is necessary for every requested feature. [Release](https://github.com/ollama/ollama/releases/tag/v0.40.11), [commit](https://github.com/ollama/ollama/commit/675150d6ad2747df1e218ef59de9194bc4a7e538).

Read-only host checks found `/usr/local/bin/ollama` reporting client version **0.32.3**. The CLI could not connect to a running instance, and `127.0.0.1:11434/api/version` refused the connection. No runtime was started and no models were downloaded. The parent host inventory identifies an M4 Pro Mac mini with 64 GB unified memory. These observations do not establish which models exist in another configured model directory or which server version will run at kickoff.

| Candidate | Catalog download size | Catalog maximum context | Initial use |
| --- | --- | --- | --- |
| `gemma4:e2b-mlx` | 7.5 GB | 128K | Low-memory and latency comparison |
| `gemma4:e4b-mlx` | 9.5 GB | 128K | Smaller-model quality comparison |
| `gemma4:12b-mlx` | 7.7 GB | 256K | First balanced candidate |
| `gemma4:26b-mlx` | 18 GB | 256K | Larger agent-task candidate |
| `gemma4:31b-mlx` | 19 GB | 256K | Larger dense-model candidate |

These are catalog values, not measured resident memory or speed. The non-monotonic sizes demonstrate why parameter count alone is an inadequate memory estimate. The 12B page currently identifies digest prefix `ded7a2735003`. Store the full installed digest after pulling; tags may change. [Catalog](https://ollama.com/library/gemma4), [12B MLX artifact](https://ollama.com/library/gemma4:12b-mlx).

Google documents native system messages, function calling, and thinking for Gemma 4. The model card advertises image input across the family and audio input for E2B/E4B/12B. An advertised model modality does not prove transport support in the chosen Ollama version. Initial app qualification should cover text, tools, and images; do not expose audio controls from a family-level label alone. [Gemma 4 model card](https://ai.google.dev/gemma/docs/core/model_card_4).

## Ownership and provider contract

Suggested boundaries, to be aligned with the final package blueprint:

- `internal/inference/ollama`: HTTP transport, native request/response conversion, NDJSON parsing, capabilities, terminal metrics, cancellation, and runtime-specific error normalization.
- `internal/models`: installed inventory, curated catalog, download jobs, license metadata, hardware suitability, selection, and runtime compatibility records.
- `internal/inference/admission`: fair queues, concurrency limits, interactive-task priority, model residency intent, and backpressure.
- `internal/runtime/adk`: ADK model interface adaptation and conversion between agent events and provider messages. Authorization stays outside the model adapter.
- Frontend model screens: call backend services and render typed state; they do not call Ollama directly or reconstruct provider request bodies.

The conceptual provider contract needs `Probe`, `ListInstalled`, `Inspect`, `ListRunning`, `Pull`, `Delete`, `Warm`, `Unload`, and `StreamChat`. Streaming should produce separate text deltas, thinking deltas, complete tool-call proposals, usage, completion, and errors. Every call carries a cancellation context. Keep wire-specific fields available in provider metadata without making them the app-wide domain model.

Prefer a small native HTTP adapter using Go's standard library unless the official repository's Go client dependency graph is acceptable after inspection. The client is maintained in the Ollama repository and is useful as a behavioral reference, but importing it also brings environment/auth helpers and repository coupling. Its current `go.mod` requires Go 1.26.10. Do not import the engine or build MLX into Gemma Desktop. [Client](https://github.com/ollama/ollama/blob/v0.40.11/api/client.go), [module](https://github.com/ollama/ollama/blob/v0.40.11/go.mod).

## Model lifecycle

| User operation | Ollama boundary | App requirement |
| --- | --- | --- |
| Check runtime | `GET /api/version`, bounded health request | Distinguish executable, running server, responsive endpoint, and compatible version |
| Installed models | `GET /api/tags` | Save name, digest, size, capabilities, and remote metadata |
| Inspect model | `POST /api/show` | Read capabilities, thinking controls, license, format, model metadata, and optional version requirement |
| Install | `POST /api/pull`, streaming | Track progress and completion; allow cancellation and retry |
| Warm | Empty `messages` request to `/api/chat` | Treat terminal `done_reason: load` as load acknowledgement |
| Unload | Empty `messages`, `keep_alive: 0` | Treat acknowledgement separately from observed `/api/ps` disappearance |
| Delete | `DELETE /api/delete` | Confirm selected model; do not delete blobs directly |
| Actual residency | `GET /api/ps` | Read effective context, digest, expiry, memory, and optional runner |

These endpoints and structures are implemented in the release's [client](https://github.com/ollama/ollama/blob/v0.40.11/api/client.go), [types](https://github.com/ollama/ollama/blob/v0.40.11/api/types.go), and [handlers](https://github.com/ollama/ollama/blob/v0.40.11/server/routes.go).

Use explicit model names, not `latest`, in saved agent configurations. Record the resolved digest and runtime version for every evaluation. Current source supports runner selection and manifest lists: one tag can identify multiple runner variants. `/api/show` can expose `manifests` and `requires`; `/api/ps` can report `runner`. Prefer the explicit MLX tags for the first qualification and record the actual runner when available. An absent field on older versions means unknown, not false.

Pull progress is not guaranteed to be one monotonically increasing file counter. Legacy transfers report per-blob progress; the safetensors transfer path can report aggregate progress. Key counters by digest, represent verifying/writing states without a percentage, and require terminal success plus inventory reconciliation. Ollama owns partial downloads and integrity checking. Its legacy downloader reference-counts shared downloads: cancelling this app's request may leave another client's download running. Never purge shared partial files to implement Cancel. [Transfer and verification](https://github.com/ollama/ollama/blob/v0.40.11/server/images.go), [shared download lifecycle](https://github.com/ollama/ollama/blob/v0.40.11/server/download.go).

The runtime is shared infrastructure. Default to attaching to the user's Ollama installation; do not terminate or reconfigure it when Gemma Desktop exits. An app-owned runtime, if later provided, needs a separate ownership marker, port, model storage decision, and process lifecycle.

## Streaming, tools, and context

Use `/api/chat` directly for the first adapter. The compatibility APIs introduce another translation layer; the native API exposes the model controls and performance fields needed here. HTTP streams use NDJSON. Parse records incrementally, impose a deliberate record-size bound, handle split network reads and Unicode, and treat an EOF before a terminal response as incomplete. A stream may return an `error` object after HTTP 200; status alone is insufficient. [Streaming](https://docs.ollama.com/api/streaming), [errors](https://docs.ollama.com/api/errors).

The current message structure separates `content`, `thinking`, `images`, and `tool_calls`. Tool calls include an index and optional ID; responses support `tool_name` and `tool_call_id`. Preserve those associations, including two calls to the same tool. For runtimes without IDs, generate stable application IDs and translate consistently. Validate and authorize only complete tool proposals; never dispatch an incomplete argument fragment. Keep the assistant's tool-call message before the corresponding results in the next request. [Wire types](https://github.com/ollama/ollama/blob/v0.40.11/api/types.go), [tool calling](https://docs.ollama.com/capabilities/tool-calling).

**Thinking history has a model-specific trap.** Google says historical thinking should be omitted before the next user turn, with an exception for tool-call turns. Ollama's Gemma renderer includes `Thinking` only for assistant messages after the latest user message, preserving tool continuation. Keep thinking separate from answer text and preserve it through a tool cycle; do not apply a blanket “strip every thought immediately” rule. Let Ollama render control tokens. Its library README is less precise than the current Google card on the tool-turn exception. [Model guidance](https://ai.google.dev/gemma/docs/core/model_card_4), [renderer](https://github.com/ollama/ollama/blob/v0.40.11/model/renderers/gemma4.go).

Thinking controls are also versioned capabilities. Current `/api/show` advertises supported booleans or named levels and a default. Missing metadata requires a compatibility profile or probe; it does not prove the model cannot think. Do not offer arbitrary low/medium/high values when the selected model exposes booleans. [Thinking API](https://docs.ollama.com/capabilities/thinking).

For images, the REST API expects base64 bytes. The backend should authorize and read attachments, validate decoded dimensions and size, and store artifact references rather than duplicate base64 in every event. A document/PDF attachment needs extraction or page rendering before it becomes an image input. [Vision transport](https://docs.ollama.com/capabilities/vision).

Do not rely on provider truncation as context management. Ollama's prompt builder can remove old messages while preserving system messages and the latest message, and its source calls multimodal token accounting heuristic. The app should compact before overflow, preserve tool-call/result pairs, reserve output capacity, and record what was summarized or omitted. [Prompt construction](https://github.com/ollama/ollama/blob/v0.40.11/server/prompt.go).

## Memory and concurrency

Explicitly set the working context size. The FAQ still describes a 4096-token default, while the dedicated context guide and current source describe hardware tiers of 4K, 32K, or 256K. Treat this as documentation drift and query the effective context in `/api/ps`. The catalog's maximum window is not the application's memory budget. [FAQ](https://docs.ollama.com/faq), [context guide](https://docs.ollama.com/context-length), [runtime configuration](https://github.com/ollama/ollama/blob/v0.40.11/envconfig/config.go).

For this 64 GB host, begin qualification with 12B MLX at a proposed 32K context and one active inference request. Compare 64K only after measuring task quality, prefill latency, and memory pressure. Then compare 26B and 31B under the same tasks and budgets. This is an experiment design, not a claim that 32K is optimal or that every larger configuration fits.

Suitability estimation should account for installed model bytes, context/cache demand, concurrent sequences, image processing, the desktop application, and other host workloads. Show “estimated” until a load is measured. Quantized cache support and flash-attention settings vary by backend; do not present llama.cpp tuning flags as proven MLX controls. On Apple Silicon, unified memory is shared: summing a model's total and GPU memory would double-count the same allocation. The MLX client returns the same resident figure for total and VRAM. [MLX reporting](https://github.com/ollama/ollama/blob/v0.40.11/mlxrunner/client.go).

Bound app queues independently of Ollama. Current source defaults `OLLAMA_NUM_PARALLEL` to 1 and server queue capacity to 512. Agent fan-out can therefore become a long inference queue; it is not free parallel intelligence. Start with one inference lease, allow tools to work concurrently when safe, prioritize interactive cancellation, and measure before enabling greater inference parallelism. Do not change shared daemon environment variables automatically. [Configuration](https://github.com/ollama/ollama/blob/v0.40.11/envconfig/config.go), [scheduler](https://github.com/ollama/ollama/blob/v0.40.11/server/sched.go).

## Locality, credentials, and observability

A loopback API URL does not guarantee local inference. Ollama can forward requests to cloud models through its local server. Current inventory and inspection structures expose `remote_host` and `remote_model`; check both. In local-only mode, reject remote models and remote endpoints, avoid automatic cloud fallback, and consider the version-gated `:local` source selector. If the app owns a runtime, `OLLAMA_NO_CLOUD=1` provides an additional boundary; do not silently modify an independently managed daemon. [Authentication and cloud routing](https://docs.ollama.com/api/authentication), [source selector](https://github.com/ollama/ollama/blob/v0.40.11/internal/modelref/modelref.go), [configuration](https://github.com/ollama/ollama/blob/v0.40.11/envconfig/config.go).

Use a backend-owned HTTP client with an explicit endpoint, no ambient proxy for loopback calls, constrained redirects, and bounded connection/header/idle timeouts. Local Ollama normally has no authentication. Remote endpoints require an explicit trust decision, appropriate TLS/authentication, and secret storage outside the frontend and logs. Do not broaden `OLLAMA_ORIGINS` to `*` or bind Ollama to all interfaces to solve a WebView problem.

Record request queue duration, first response event, first answer text, cold-load duration, completion time, prompt/output counts, and final model metrics. `eval_count / (eval_duration / 1e9)` gives decode throughput when the denominator is positive; it is not whole-task throughput. Current usage fields arrive in the terminal chunk, and cached prompt count can be absent. Mark unavailable values as unavailable. `/api/ps` memory is a runtime observation, not total system memory utilization. Keep raw prompts, tool results, thinking, and attachments out of diagnostics by default; upstream request-body logging is separately configurable. [Usage metrics](https://docs.ollama.com/api/usage), [running models](https://docs.ollama.com/api/ps).

Gemma 4's current official model card labels the release Apache 2.0; Ollama's repository is MIT. Preserve applicable notices for the precise components distributed and retain each installed model's license metadata. Do not infer all compatible model licenses from Gemma's license. [Gemma card](https://ai.google.dev/gemma/docs/core/model_card_4), [Ollama license](https://github.com/ollama/ollama/blob/v0.40.11/LICENSE).

## Qualification gates before promoting the adapter

1. Probe the actual server version; record model digest, format, runner, capabilities, context, and thinking controls. Establish which features work on the installed 0.32.3 runtime versus the selected supported release.
2. Complete a streamed text exchange and a real read-tool/write-tool/test-tool loop. Include two calls with the same tool name, Unicode arguments, malformed arguments, and thinking-enabled continuation.
3. Cancel while queued, loading, generating, and downloading. Verify the request settles and a later request succeeds without terminating another client's work. Measure cancellation latency.
4. Exercise absent model, unsupported option, load failure, disk-full pull, truncated stream, malformed NDJSON, and server loss. Do not convert partial output into success or automatically repeat an executed tool.
5. Compare MLX and a compatible GGUF configuration using the same model/task where practical, declared quantizations, fixed context, cold/warm runs, repeated samples, and quality criteria. Report configuration differences instead of attributing all speed differences to the backend.
6. Validate image inputs separately. Verify context compaction preserves the plan, approvals, and complete tool cycles.
7. Run a downloaded local model with external networking disabled. Test rejection of a cloud alias, cloud-backed custom model, and changed endpoint.
8. Restart the desktop during an active run. Recover the application's state without claiming that an interrupted Ollama generation can resume from its exact last token.

The remaining empirical questions are the minimum supported runtime for the chosen artifact, the best model/context tradeoff on this Mac, backend-specific cache behavior, and sustained task success. The sources establish the integration surfaces; live qualification must establish those operating limits.
