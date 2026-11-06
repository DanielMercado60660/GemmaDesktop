# Reliability round 2 — 5 November 2026

Implementation follows the frozen [explore → plan → build record](../implementation/TASK_RELIABILITY_ROUND2_2026-11-05.md).
This is a development experiment, not general reliability qualification.

## Built behavior

- Confirmed schema failures now give bounded guidance for command arguments
  supplied as strings and declared file-range bounds. The original error and
  proposal remain intact; nothing is coerced or executed before validation.
- A separate `thinkingCycleGuard` is available in experimental harness controls
  and remains off by default. It interrupts sustained exact thinking cycles
  outside fenced code and consumes an existing retry allowance. It does not
  inspect visible answers, tool arguments, summaries or budget reports.
- Uncommitted tool proposals are discarded on interruption. Clean callback
  cancellation permits recovery; joined cleanup errors, cancellation and
  exhausted limits do not. Prior completed actions are not replayed.
- Compaction telemetry now measures selected input components, attempt cost,
  result status and summary size. Generation context distinguishes wire text
  from wire thinking. Neither is labeled as token savings or cache evidence.
- Pilot records/export now separate pre-dispatch validation rejections from
  dispatched failures. Historical missing fields remain unknown, not zero.

No default output cap, deadline, compaction policy, grader or task fixture was
changed. The long nonrepeating log enumeration remains outside the guard's
scope. A successful command does not cause the host to fabricate completion.

## Validation

- Five affected Go packages passed race suites: app, engine, tools, workspace
  and inference. The initial restricted run could not bind a loopback test
  server; the scoped rerun passed. Schema-recovery tests use controlled schemas
  and a mock host; active range constraints are also tested directly.
- Python: 41 repository tests and 7 Harness adapter tests passed.
- Frontend: 76 unit and 73 UI tests passed; production build passed.
- Native app build, ad-hoc signature and source-input provenance checks passed.
- The full Go suite was not repeated. The prior four media failures remain
  unaddressed: installed FFprobe is still 9.0.2 while the subsystem requires 8.1.

The [frozen replay](task-pilot-round2-2026-11-05/thinking-cycle-replay.md)
reproduced all five expected trigger boundaries across 134 saved generations,
with no extra triggers. Production fence parsing was corrected to avoid
embedded/opposite/short marker false closures, with unchanged thresholds and
replay outcomes. Synthetic/native mock tests cover cleanup, partial calls,
history, channels, mixed retries, output exhaustion and receipt accounting.
This does not qualify a false-positive rate or demonstrate live recovery.

## Frozen identities and controls

| Identity | Value |
| --- | --- |
| Candidate binary SHA-256 | `de1c69829f8aec47588405a2e710e75e831ba170190d5c8258deb8e1c0742c2d` |
| Source fingerprint | `5538de830d4cc17a040d3e44226c79f91a4ad5c5ade1a34099bda36b7fcba8b2` |
| Frontend fingerprint | `e1ec3560fe119d437d14774b675a7ce3f79d4ed0319c9031058cd03480e783db` |
| Verified model | `gemma4:12b-mlx` on Ollama 0.40.11 |
| Model digest | `ded7a27350032202d9e9b2a6071e8aa89959ab156771b5228f30863c741c4970` |
| Both arms | Requested 32K context, 40 steps, temperature 0.2, thinking Auto, 8,192 output tokens |
| Driver deadline | 600 seconds per task plus bounded cancellation wait |
| Control | `thinkingCycleGuard=false` |
| Experiment | `thinkingCycleGuard=true` |

Both arms use the same frozen binary, including schema feedback. The older
stream-prose guard stays off. Each arm runs log-question, crash-from-log and
wordfreq-repair exactly once, serially, in fresh isolated fixtures. Gemma alone
edits solver files. All failures and cancellations remain in the denominator.
Previous v3 results are historical context, not a fresh paired baseline here.

## Live results

The control batch finished **2/3 independently correct** and the experimental
batch **3/3**. Both coding tasks passed host verification in both arms. Exactly
six attempts ran; both pilot processes exited and no extra attempts launched.

| Task | Control outcome | Host verification | Seconds |
| --- | --- | --- | ---: |
| log-question | Incorrect; deadline cancellation without final answer | Not applicable | 601.7 |
| crash-from-log | Correct | Passed | 170.7 |
| wordfreq-repair | Correct | Passed | 195.9 |

[Control results](task-pilot-round2-2026-11-05/round2-control-20261105-r1.json)
and the [raw audit](task-pilot-round2-2026-11-05/control-audit.json) retain every
outcome. All protected inputs were preserved. The log task had zero file or
directory deltas; both coding tasks passed independent checks and added tests
that fail on the original code. All three reported schema-rejection counts
match the raw journal (zero). No control task compacted context.

The log task reached the output cap twice. Its first length stop exposed
12,133 thinking characters; the second exposed only 902 and no accepted tool
call or visible answer. Token totals do not reveal where the remaining output
went, so its cause is unobserved rather than an inferred thinking/argument loop.
The later generation still did not deliver an answer before the deadline.
Its calculation incorrectly reported that no 5xx errors existed; this attempt
must not inherit the previous round's correct-intermediate-calculation claim.
Five of its six provider attempts supplied terminal receipts, so usage totals
are incomplete. An independent reviewer checked all control records, exports,
checkpoints, receipts and file/directory deltas.

| Task | Guard-enabled outcome | Host verification | Seconds | Cycle interruptions |
| --- | --- | --- | ---: | ---: |
| log-question | Correct | Not applicable | 57.5 | 0 |
| crash-from-log | Correct | Passed | 128.4 | 0 |
| wordfreq-repair | Correct | Passed | 208.5 | 0 |

The successful log task recovered from one rejected `read_file` range proposal
using the new schema feedback. It left the raw file and directory inventories
unchanged. **The cycle detector never triggered in this pilot.** The scores
and timing differences cannot establish a guard benefit or speed improvement.
The crash task had one ordinary empty-response retry. All 38 experimental
provider attempts supplied terminal receipts; all three experimental tasks
preserved protected inputs and had no unexpected file, directory or cache
changes. The guard remains off by default.

[Experimental results](task-pilot-round2-2026-11-05/round2-cycle-20261105-r1.json)
and the [complete comparison audit](task-pilot-round2-2026-11-05/comparison-audit.json)
retain both arms. An independent reviewer checked all six outcomes against
records, checkpoints, journals, host receipts and raw workspace deltas. New
schema-rejection metrics match raw events throughout. The observed range-error
recovery exercises real feedback delivery, but does not isolate its causal
benefit. Neither arm compacted context, so the new compaction telemetry was
validated in tests rather than exercised by this live pilot.

Post-evaluation provenance verification confirms the source still matches
the frozen build inputs. The native app and evaluated binary remain identical.

## Interpretation limits

One attempt per task per arm cannot establish general reliability or speed.
Guard triggers and recovery may alter later trajectories. Missing terminal
usage receipts after interruption remain incomplete accounting. Mock native
tests are not live managed-MLX qualification. No held-out tasks were run; the
work record retains the filename-inventory exclusion mistake and the previous
round's requirement for newly authored blind tasks.

The startup model snapshot reported a 16K loaded context while run admission
and wire requests specified 32K. The requested setting is verified; it is not
a measured loaded-capacity or context-qualification claim.

The [context exploration](task-pilot-round2-2026-11-05/compaction-context-exploration.md)
explains why low-yield compaction, manual enumeration and rendered retry
history require separate experiments. A shorter global response cap would
truncate known productive edits. Measured headroom must include new responses
and tool results before any compaction bypass can be justified.
