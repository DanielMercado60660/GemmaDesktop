# Candidate 3: bounded tool-call failure diagnosis

2026-10-27. Read-only investigation; no inference, fixture edits, acceptance changes, or adapter changes.

Evidence: `docs/evidence/realistic-20261027-115147-73074c.json`; isolated `.runtime/realistic-qa/gemma.db`, run `10055798b09930098faf08b0be0af21f`, session `30a39afc3c8483c9611d78ec673b0174`. SQLite was opened in read-only mode.

## Finding

The run failed after 752.932 seconds with `execution budget exceeded: maximum tool calls in one response`. Available evidence strongly supports repeated model generation, rather than a demonstrated cumulative-stream aggregation bug. Preserve the 64-call bound and exact command validation; do not deduplicate calls by argument similarity or repair the malformed command automatically.

The durable journal records 20 executed tool attempts, four failed tools and four source changes. The proposed test command contained one malformed string in `args`, rather than the prescribed six separate strings, and approval denied it. There is no observed successful test command. In the final generation, 13,807 characters of streamed thinking contain 69 occurrences of the test command's argument tokens and repetitive self-correction. This is direct evidence of a generation loop, independent of how tool proposals are counted. No pending proposals from that failed response reached tool execution.

The final failed response's raw tool proposals are not stored in ADK: the adapter buffers them until a complete response, while text/thinking deltas are emitted as they arrive. Consequently the journal cannot establish each of the 65 incoming proposal identities/indices. This limits certainty about the wire stream. The task-owned Ollama PTY could not be read from this subagent's tool namespace; standard home-directory logs were stale. No running process was interrupted.

## Parser and adapter contract

Pinned Ollama `v0.32.3`, commit `fce745fe5e4f0c7741ca46fc5484c9471c997d00`, consumes the buffered text through a tool-call closing tag before emitting the parsed call. `Add` assembles a new result slice for that invocation and increments `Function.Index`. The chat handler assigns an ID and places these new calls in that response chunk. This is a delta contract; appending chunks is appropriate. The source does not support treating each chunk as a cumulative replacement list. [Gemma4 parser](https://github.com/ollama/ollama/blob/v0.32.3/model/parsers/gemma4.go#L115), [buffer consumption](https://github.com/ollama/ollama/blob/v0.32.3/model/parsers/gemma4.go#L308), [chat handler](https://github.com/ollama/ollama/blob/v0.32.3/server/routes.go#L2813)

The app's `internal/engine/adapter.go` increments the per-response count once for every incoming `ToolCalls` entry. At 64 it fails before constructing a completed ADK response. Nothing in this investigation establishes that its count is duplicating entries. Do not change parser semantics or enlarge the budget merely to make this candidate proceed.

## Compaction fidelity

The original user event is timestamped `2026-10-27T11:52:26.491196-05:00`. Its prompt is byte-for-byte equal to the request's persisted prompt, including executable, six argument strings, read-only requirement, scope restrictions and handoff marker.

The sole compaction action covers `11:52:42.273749` through `11:58:05.082185` in the same time zone. It therefore excludes the original user event. The 1,993-character summary contains project defects, completed edits and pending work; it does not restate or alter the command. The failed generation itself retained knowledge of the correct command tokens. There is no evidence that compaction transformed the argument array into the malformed string.

Summary wording calls the repository fixed based on edits, without test evidence. The separate durable execution receipt correctly distinguishes observed changes from validation. UI completion claims should continue to depend on observed checks, not summary wording.

## Next run and optional observability

The source-only refinement already removes synthetic file closing markers; the failed candidate remains unchanged. A fresh run can test that general presentation correction with the same task, protected files, acceptance suite and approval policy.

For a future diagnostic improvement, emit bounded failed-response proposal telemetry: total count, a capped list of tool names, distinct ID/index counts, duplicate-identity count and failure reason. Exclude raw arguments/source, do not execute incomplete proposals, and preserve the existing limit. This would distinguish stream-protocol repetition from genuine generation without retaining enormous argument payloads. It was not implemented in this investigation.
