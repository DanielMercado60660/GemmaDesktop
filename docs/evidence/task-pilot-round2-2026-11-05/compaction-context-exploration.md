# Context and answer-delivery exploration — 5 November 2026

Explore-only review. No source edits, inference, or held-out task access. The
three saved log-question attempts are development evidence, one attempt per
build, not a controlled causal experiment.

## What the saved attempts establish

| Frozen build | Outcome | Relevant trajectory |
| --- | --- | --- |
| Baseline | Cancelled at 602.0 s | Nine read-file schema failures, then a large read. A command produced the correct counts at 06:17:22 UTC. Compaction took 20.32 s; the next generation did not finish before the 600 s driver deadline. |
| Exploratory v2 | Correct at 378.5 s | A command produced the correct counts at 06:49:41 UTC; a second command reconfirmed them. The next reasoning generation reached 8,192 tokens. A trailing-user-feedback retry returned the answer. No compaction occurred. |
| Final v3 | Cancelled at 601.3 s | After reading the log, 12,174 characters of nonrepeating, line-by-line enumeration hit the output cap. Retry produced code; one command failed, then two successful commands reported the correct counts. Compaction took 26.43 s; the next generation was cancelled before an answer. |

The long v3 enumeration is not evidence of a repeated-block loop. The first
correct command output appeared roughly 100 seconds before the deadline, but
the model chose another command, then the host compacted. Skipping maintenance
would have returned 26 seconds; it does not prove the model would have answered.
The deadline was the pilot driver's 600 seconds, not an engine step-limit stop.

## Why that compaction ran and why it did little

`internal/engine/compaction.go:22` uses 60% of context by default: 19,660 tokens
for this 32,768 context. It retains eight recent events. It does not derive the
trigger from the configured 8,192-token output reserve or any measured saving.

ADK v2.7.1 `internal/compactioninternal/tail_retention.go` checks the latest
observed prompt token count plus a character estimate of later events, then
selects a self-contained prefix before that retained tail. It protects the live
user question and tool pairs. The current model-response event's generated
tokens are not added to that observed prompt reading by `promptTokenCount`;
the estimate starts at the next event. This matters for any proposed headroom
skip: observed prompt tokens alone do not describe the next prompt.

Final v3's last pre-compaction measured prompt was 21,067 tokens. Prompt plus
the full configured output reserve totals 29,259, leaving 3,509 tokens before
accounting for the just-produced 605-token response, the new tool result,
message framing, and uncertainty. There plausibly was enough headroom for one
ordinary continuation, but the present 60% trigger cannot express that.

Only the initial `list_files` call/result pair was selected. Its thinking text
was 613 characters; the entire selected content JSON was approximately one
kilobyte. The resulting summary was 610 characters. The much larger `read_file`
result remained in the eight-event tail. This was demonstrably low-yield
maintenance, not a useful compression of the dominant evidence. Exact rendered
token saving was not measured: the continuation never returned terminal usage.

Baseline differs materially: its last measured prompt was 26,391 tokens, so
adding the full 8,192 output allowance already exceeds 32,768. A universal
compaction disable or unconditional answer attempt is not supported.

The text-character audit is not safe admission arithmetic: final v3 step 3
estimated 6,520 text tokens while the provider measured 17,867 prompt tokens.
Tools and log tokenization account for part of this difference. Use the audit
as an observation, not a tokenizer or reliable free-context estimate.

## Thinking actually reaches the model, with a retry caveat

Local `/api/show` identifies `gemma4` rendering; its displayed Go template is
only `{{ .Prompt }}`. The pinned [Ollama 0.40.11 renderer](https://github.com/ollama/ollama/blob/v0.40.11/model/renderers/gemma4.go)
includes assistant thinking after the last user message and excludes earlier
thinking. Thus the ordinary active tool cycle retains reasoning. A synthetic
trailing user retry changes that boundary: v2 added 528 wire characters yet
measured prompt tokens fell from 21,120 to 19,098. Wire-prefix preservation
therefore does not prove renderer/KV-prefix preservation. After retry, the
synthetic message is not persisted, so later ordinary calls restore the prior
boundary. Do not globally strip active-cycle thinking on an assumption that
the provider already discards it.

## Bounded recommendation and tests

Do not change default compaction policy in the schema-feedback/optional-cycle-
guard round solely from these three traces. The evidence supports a separate
small context-admission experiment: skip low-yield maintenance only when a
conservative projection establishes room for an ordinary continuation plus
its full output allowance and margin. It must include the latest generated
response and intervening tool payloads, reject unknown image/schema changes,
preserve original user instructions/tool pairs, and retain normal compaction
when that projection cannot establish room. No task identifiers, expected
answers, graders, or host-authored solution should enter this decision.

Before enabling it, prove with synthetic requests: a 21K prompt plus short
response/result can continue; a 26K prompt cannot reserve 8K; a huge new tool
result cannot slip past stale observed usage; large tool arguments, images,
tool discovery, and multi-agent scopes invalidate unsafe reuse. A two-event
directory-list prefix should be recognized as low yield while a large old
read can still be compacted. Keep branch, original-user, tool-pair, cancellation,
and compaction-failure regressions intact. Test the actual Gemma renderer's
last-user boundary for retry feedback, not just the wire JSON prefix.

For evaluation, use frozen source and existing development fixtures only:
log-question for answer delivery and wordfreq-repair/todo-feature for context
and productive long-generation controls. Compare the experimental flag and
control in the same binary. Record correctness, completion, unchanged files,
compaction window size/saving, maintenance seconds, retries, and runtime
prompt tokens separately. Stop at a bounded initial pilot; make no performance
or generalization claim from one attempt.

Machine-readable event summary: `log-question-context-summary.json` beside
this memo. Installed model readback: `ollama-12b-template.json`. Full saved
run paths and timestamps are in the event summary.
