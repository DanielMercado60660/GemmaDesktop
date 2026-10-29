# Context and ADK journal audit — 27 October 2026

The captured journal does **not establish that a 32,768-token context caused these failures**. Several failures occur before any compaction and far below the requested context. The fresh 26B run does contain a compaction boundary, but its final failure is an explicit output-generation limit. This is observational evidence, not a controlled larger-context or ADK-bypass experiment.

Source: one read-only SQLite transaction against `.runtime/realistic-qa/gemma.db`, through event sequence 66476. [Full machine-readable evidence](context-adk-audit-journal-20261027.json) contains metadata for all 12 runs and 249 main-model generations. Raw thinking is excluded; only character counts are retained. No inference or production/fixture changes were performed.

## What the evidence can and cannot measure

- **Measured** rows come from persisted ADK event `customMetadata.ollama_metrics`; visible/thinking character counts come from part lengths and stream-event lengths. Tool counts distinguish proposed calls from host callback starts. A callback start can still be denied by approval or fail before an external effect.
- **Inferred terminal** rows subtract all measured completed model generations and successful summary token counts from durable aggregate run usage. They are labeled `residual`; they are not captured HTTP request/response records. This attribution is used only with one unfinished generation, a recorded incomplete result, positive residual output, and no failed-summary ambiguity.
- **Missing** rows remain unknown when cancellation, transport failure, or a budget stop did not persist final metrics. A zero aggregate residual is not evidence that an interrupted generation generated zero tokens.
- Summed prompt tokens count repeated processing across calls; they are **not simultaneous context occupancy**. Requested `contextSize` is configuration, not proof of a runtime-enforced hard limit. No per-generation wire payload, actual tokenizer trace, or full terminal tool buffer was captured.
- There is no basis here for identifying token content from the gap between `eval_count` and visible/thinking character counts. In particular, an output cap can occur while constructing a tool call; the journal does not reveal an unfinished call discarded by the adapter.

## Fresh 26B run: `bd7196397273815a251a1e073b9827f7`

Recorded controls: `gemma4:26b-mlx`, context 32,768, temperature 1, thinking On, output limit 8,192. There are 33 main-model generations plus one summary generation. The first command denial is generation 5 (5,059 prompt tokens); a prescribed command exits nonzero in generation 6 (5,197 prompt tokens). The schema-rejected `read_file` proposal with an extra `expectedHash` is generation 13 (10,614 prompt tokens). These all precede compaction.

Compaction starts at sequence 63616 after generation 21 and applies at 63618. Its separately recorded usage is 5,923 prompt tokens / 531 output tokens, with a 1,906-character summary. The main-model prompt drops from 20,831 tokens at generation 21 to 8,711 at generation 22. The next edit has a stale hash after the summary, but a stale-hash failure also occurred immediately before it; chronology alone cannot attribute that behavior to the summary.

The final generation has **21,217 prompt tokens and 8,192 output tokens by residual accounting**, zero streamed visible characters, only 168 streamed thinking characters, and no host tool callback. Its terminal error explicitly reports the generation length limit. No complete ADK model event exists for that generation, so the number/content of buffered tool proposals is unknown. Even prompt plus the reported output total is 29,409, below the requested 32,768; this weakens a simple context-exhaustion explanation but does not independently qualify the runtime token accounting.

| Gen | Event start | Prompt tokens | Output tokens | Visible chars | Thinking chars | Proposed / started tools | Finish | Compactions already applied | Evidence |
| ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | ---: | --- |
| 1 | 57453 | 2716 | 348 | 0 | 1195 | 1 / 1 | stop | 0 | measured |
| 2 | 57769 | 3330 | 36 | 0 | 66 | 1 / 1 | stop | 0 | measured |
| 3 | 57792 | 4116 | 274 | 0 | 945 | 1 / 1 | stop | 0 | measured |
| 4 | 58032 | 4873 | 26 | 0 | 30 | 1 / 1 | stop | 0 | measured |
| 5 | 58047 | 5059 | 108 | 0 | 81 | 1 / 1 | stop | 0 | measured |
| 6 | 58073 | 5197 | 204 | 0 | 458 | 1 / 1 | stop | 0 | measured |
| 7 | 58189 | 6432 | 206 | 0 | 630 | 1 / 1 | stop | 0 | measured |
| 8 | 58368 | 6909 | 28 | 0 | 20 | 1 / 1 | stop | 0 | measured |
| 9 | 58381 | 7227 | 47 | 0 | 127 | 1 / 1 | stop | 0 | measured |
| 10 | 58413 | 8011 | 870 | 0 | 3049 | 1 / 1 | stop | 0 | measured |
| 11 | 59171 | 9575 | 119 | 0 | 378 | 1 / 1 | stop | 0 | measured |
| 12 | 59259 | 10281 | 65 | 0 | 179 | 1 / 1 | stop | 0 | measured |
| 13 | 59306 | 10614 | 2119 | 0 | 6956 | 1 / 0 | stop | 0 | measured |
| 14 | 61139 | 12765 | 93 | 0 | 297 | 1 / 1 | stop | 0 | measured |
| 15 | 61215 | 13595 | 1971 | 0 | 2476 | 1 / 1 | stop | 0 | measured |
| 16 | 61853 | 15720 | 227 | 0 | 102 | 1 / 1 | stop | 0 | measured |
| 17 | 61888 | 16107 | 116 | 0 | 95 | 1 / 1 | stop | 0 | measured |
| 18 | 61917 | 16673 | 1458 | 0 | 4070 | 1 / 1 | stop | 0 | measured |
| 19 | 62930 | 18290 | 216 | 0 | 726 | 1 / 1 | stop | 0 | measured |
| 20 | 63114 | 19200 | 1471 | 0 | 1057 | 1 / 1 | stop | 0 | measured |
| 21 | 63372 | 20831 | 1266 | 0 | 1002 | 1 / 1 | stop | 0 | measured |
| 22 | 63619 | 8711 | 43 | 0 | 73 | 1 / 1 | stop | 1 | measured |
| 23 | 63647 | 9341 | 1000 | 0 | 34 | 1 / 1 | stop | 1 | measured |
| 24 | 63661 | 10378 | 96 | 0 | 261 | 1 / 1 | stop | 1 | measured |
| 25 | 63740 | 11061 | 1047 | 0 | 79 | 1 / 1 | stop | 1 | measured |
| 26 | 63807 | 12264 | 135 | 0 | 440 | 1 / 1 | stop | 1 | measured |
| 27 | 63922 | 12559 | 2096 | 0 | 3783 | 1 / 1 | stop | 1 | measured |
| 28 | 64937 | 14694 | 110 | 0 | 323 | 1 / 1 | stop | 1 | measured |
| 29 | 65035 | 15068 | 1146 | 0 | 796 | 1 / 1 | stop | 1 | measured |
| 30 | 65293 | 16261 | 986 | 0 | 447 | 1 / 1 | stop | 1 | measured |
| 31 | 65403 | 17408 | 453 | 0 | 1198 | 1 / 1 | stop | 1 | measured |
| 32 | 65759 | 18603 | 2545 | 0 | 2345 | 1 / 1 | stop | 1 | measured |
| 33 | 66419 | 21217 | 8192 | 0 | 168 | ? / 0 | length | 1 | residual |

## 26B continuation: `641b5dceeaa5d69f02b23041bd3b7ac5`

Recorded controls retain context 32,768, temperature 1 and output limit 8,192, with thinking explicitly Off. The failed predecessor snapshot was **not imported**: the immutable request records one previous user message, no previous assistant message, and no snapshot. Its instructions contain bounded host recovery observations. Thus the prior 33-generation ADK transcript and raw malformed tool proposals are not carried into this continuation as normal model/tool history.

The model makes two structurally valid tool calls, then produces an empty result. Its third generation has **7,842 prompt tokens / 3 output tokens by residual accounting**, zero visible/thinking stream characters, and no tool callback. No compaction occurs. The exact wire `done_reason` is not persisted for this terminal response; the adapter records an empty-response failure, not a length failure. This outcome occurs without approaching the configured input context or output budget.

| Gen | Event start | Prompt tokens | Output tokens | Visible chars | Thinking chars | Proposed / started tools | Finish | Compactions already applied | Evidence |
| ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | ---: | --- |
| 1 | 66465 | 6808 | 14 | 0 | 0 | 1 / 1 | stop | 0 | measured |
| 2 | 66470 | 7079 | 20 | 0 | 0 | 1 / 1 | stop | 0 | measured |
| 3 | 66475 | 7842 | 3 | 0 | 0 | ? / 0 | empty_response | 0 | residual |

## 12B comparison history

These attempts differ in prompt history, source state, application revisions, temperature, thinking settings and caller termination. They are not an isolated model-quality or context-size comparison. Null legacy controls in JSON mean the setting was not recorded, not a known false/zero value.

| Run prefix | Thinking / temperature | Main generations | Max known prompt | Applied summaries | First observed tool error | Terminal evidence |
| --- | --- | ---: | ---: | ---: | --- | --- |
| `59da00ba` | unrecorded/default / 0.2 | 42 | 27871 | 0 | G9 edit_file; before first summary | final generation metrics missing |
| `a1c1dc98` | unrecorded/default / 0.2 | 20 | 19670 | 1 | G10 edit_file; before first summary | final generation metrics missing |
| `10055798` | unrecorded/default / 0.2 | 22 | 20074 | 1 | G11 write_file; before first summary | final generation metrics missing |
| `10f7905a` | unrecorded/default / 0.2 | 27 | 20415 | 1 | G20 read_file; after first summary | final generation metrics missing |
| `f3e0cee6` | unrecorded/default / 0.2 | 7 | 8474 | 0 | none recorded | final generation metrics missing |
| `9c44237c` | Off / 0.2 | 20 | 16432 | 0 | G19 run_command; before first summary | 16432 in / 8192 out (residual) |
| `b290cd41` | Off / 0.2 | 14 | 15319 | 0 | G8 edit_file; before first summary | 15319 in / 1146 out (measured) |
| `659846d2` | Off / 1 | 39 | 22292 | 3 | G6 edit_file; before first summary | final generation metrics missing |
| `7d5cd1ef` | Off / 1 | 4 | 12478 | 0 | none recorded | 12478 in / 3263 out (residual) |
| `900ee97f` | On / 1 | 18 | 23161 | 1 | G4 run_command; before first summary | final generation metrics missing |

Two direct counterexamples to a compaction-only account are the 12B Off/0.2 run `9c44237c…` (no summary; final 16,432 input / 8,192 output, length failure) and Off/1 run `7d5cd1ef…` (no summary; final 12,478 input / 3,263 output, empty-response failure). Those terminal counts are residuals, not complete-response captures.

## ADK serialization and tool-shape findings

- Across these stored current-run model/tool events, the audit found no unmatched function-call/response IDs, no call/response name mismatch, no duplicate call IDs, and no non-object root argument payload. This checks structural pairing only; it does not establish semantic quality or prove every request matched the model’s ideal chat template.
- The 12B On/1 run `900ee97f…` proposes `run_command.args` as a JSON-looking **string** rather than an array in generations 4–10, at prompt sizes 9,153–13,161. ADK emits error function responses before the host tool callback. The errors are present in saved model-facing history; they were not successful commands. This predates its first compaction. Repetition after explicit errors is observed, but this audit cannot separate model sampling, template/adapter behavior and error-feedback presentation as causes.
- Fresh 26B generation 13 proposes a forbidden `read_file.expectedHash` property. The paired ADK function response is an error object with only an `error` key; application tool errors also carry `isError`. Both serialize as valid JSON. A difference in error-envelope presentation is real, but there is no evidence that ADK serialized invalid JSON or paired the wrong response with the proposal.
- The earliest 12B baseline `59da00ba…` contains 10 responses repeating the same payload as both escaped JSON `content` and structured `data`. Later runs have zero such duplicates. This historical presentation issue can increase clutter and escaping confusion; it is absent in the two 26B runs and cannot explain their terminal failures by itself.
- The fresh 26B terminal length failure is withheld from the completed ADK transcript. The Off continuation rebuilds from prior user input and bounded recovery observations rather than that failed transcript. There is therefore no direct evidence of malformed prior ADK responses poisoning this continuation. Recovery observations, tool schemas and the requested task still influence the prompt.

## Causality limits and next evidence

The measured record supports **output-budget exhaustion, empty generation, invalid tool arguments and failed edit/test recovery** as distinct observed failure modes. It does not support treating all of them as one context-window failure. The single 26B compaction visibly reduces reported prompt size; whether it preserves every useful fact requires content-aware summary evaluation and a controlled comparison, not merely inspecting its length.

A useful next experiment would freeze the exact model/runtime, prompt, workspace revision, tool schemas, sampling controls and approval policy; compare the same continuation with and without host summarization and with a larger requested context. Capture request metadata/digests and terminal metrics before discarding incomplete responses, including whether tool-call bytes were buffered, without retaining raw thinking in the user-facing audit. That would separate context pressure, compaction content loss, model/tool-template behavior and output budgeting. No such experiment was run for this report.
