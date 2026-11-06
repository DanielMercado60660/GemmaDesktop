# Everyday task pilot

Real Gemma attempts through the ordinary app path, graded by hidden checkers.
Gemma is always the solver; nothing here edits a candidate's files or relaxes
a grade.

| File | Purpose |
| --- | --- |
| `gemma_pilot.py` | Starts an isolated app instance (fresh data directory under `.runtime/pilot-<label>-<time>/`), configures one agent, runs each task in a disposable copy of its fixture, approves governed actions as a cooperative user (edits to protected paths are refused), and records prompt, settings, model identity, transcript, receipts, final files, diff and grade. |
| `harness_pilot.py` | One production Harness diagnostic (real solver + Docker grader) on a development-pack task, default `event-join`. Pins the toolchain image by digest and creates a Docker capability profile in the isolated data directory. |
| `check_controls.py` | Proves every checker: the untouched fixture must fail, the reference solution must pass. Run it after editing any task. |
| `compare.py` | Side-by-side table of batches (correctness, host verification, time, tool calls). |
| `checklib.py` | Checker helpers. Candidate code only runs read-only under `sandbox-exec` with no network. |
| `tasks/<id>/` | `task.json` (prompt, optional verification contract, protected paths), `workspace/` (all the model sees), `reference/`, `trusted/`, `check.py`. |

## Run

```sh
source scripts/env.sh
go build -tags production -trimpath -o /tmp/gemma-dev .
python3 scripts/pilot/check_controls.py
python3 scripts/pilot/gemma_pilot.py --binary /tmp/gemma-dev --model gemma4:26b-mlx --label mytry \
  --tasks calculator-smoke wordfreq-repair codebase-qa sales-report meeting-actions \
          config-update organize-notes crash-from-log todo-feature write-tests
python3 scripts/pilot/compare.py .runtime/pilot-<labelA>-<time> .runtime/pilot-<labelB>-<time>
```

Defaults match a fresh install (32K context, 40 steps, temperature 0.2,
thinking auto, 8,192 output tokens, all tools). Override with `--agent '{...}'`
and `--settings '{...}'` (same fields as `agent.save` / `settings.save`).
Use one batch at a time: Ollama serves one model request at a time, and
concurrent batches would distort timing.

## Reading a result

`record.json` keeps four outcomes apart: whether the model completed, the
host's verification status, the independent grade, and protected-input
integrity. Only `grade.passed` means the task was done correctly. Generation
details (thinking, token counts, retries) are in the batch's own
`data/gemma.db` (`events` table, read it with `sqlite3 -readonly`).

`toolCalls.errors` counts dispatched tool failures. `validationErrors` and
`validationFailures` separately report rejected arguments that never reached
execution. Older records lack these fields; an absent field is not a zero.

These tasks are development material. Once used to tune the host they are
exposed; do not present them as held-out evidence.

## External-Codex Harness² campaign

`harness_campaign.py` supervises twenty actual upstream k=1 rounds on the
predeclared five-task development cycle. Four Gemma probes per round produce
80 optimization attempts, followed by a frozen initial-versus-selected
diagnostic with ten additional attempts. Model/runtime/image/binary/operator
identities remain fixed; the current campaign profile selects local Gemma26B.
The program invokes no Codex CLI or cloud proposer.

```sh
python3 scripts/pilot/harness_campaign.py --binary /absolute/frozen/gemma-desktop \
  --out .runtime/harness-campaign-26b
# After an interrupted operator, use the exact same binary and script:
python3 scripts/pilot/harness_campaign.py --binary /absolute/frozen/gemma-desktop \
  --out .runtime/harness-campaign-26b --resume
```

The resident isolated app writes packet requests under `requests/`. The actual
Codex proposer writes `responses/<request-id>.json` containing `response`, the
application's complete packet response (ID/hash, actual author identity, full
declarative Content and evidence-supported notes). An independent reviewer
handles reflection files using the reviewer identity and adds a top-level
`retainRevisionId`, choosing the prior base or reviewed composed candidate.
Failed upstream rounds that lack an official reflection receive a separately
labeled terminal-review request; it cannot invent an upstream packet or retain
the failed candidate. Actor names are explicit constants in the frozen operator.

`harness.experiments.start` accepts optional `baseRevisionId` for compatible,
reviewed development carry-forward. It preserves the entire immutable Content
and provenance without changing any product binding. The unchanged initial
baseline remains an allowed fallback. `harness.trials.gradeDevelopment` accepts
`experimentId` and `trialId`, grades only existing immutable development outputs,
and returns counts and opaque infrastructure categories. Its `gradingFinished`
field describes the grader attempt; solver completion is a separate fact.
Interrupted grading reservations are retained instead of silently replayed.

The operator records solver delivery and semantic correctness separately and
requires both for candidate retention. It preserves all failed/rejected rounds,
uses the application's three-attempt/two-hour checkpoints, and never changes
graders or solver fixtures. Final diagnostic checks/details stay under
`private-audit/` for independent review; public operator summaries contain only
per-case counts/statuses. Development outcomes do not authorize promotion or
prove broad reliability.
