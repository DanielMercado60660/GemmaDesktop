# Realistic local-model coding qualification

The model receives only `workspace/`: seven source modules, an immutable README, and five original tests. It must repair three defects in a SQLite-backed tracker and implement CSV export across the repository/service/serializer/CLI boundary. A new `tests/test_regressions.py` is required. The package uses only the Python standard library.

The independent `acceptance_test.py` stays outside the selected workspace. Its nine cases cover persisted updates, filter intersections/totals, literal query characters, stable pagination, validation, Unicode/quoted/newline CSV, exports larger than 100 rows, empty exports, and a real CLI-to-service-to-file round trip. `reference.py` is a harness-only known repair used to establish that the acceptance oracle is achievable. It is never supplied to Gemma.

## Preparation without inference

From the repository root:

```sh
python3 scripts/realistic-e2e.py --qualify-fixture
python3 scripts/realistic-e2e.py --prepare-only
```

Qualification runs the original and independently held suites against the seeded source, then against the harness-only reference repair. Both seeded runs must fail; both repaired runs must pass. All four test processes use a macOS sandbox: candidate source and acceptance code are read-only, only private temporary storage is writable, and network access and child-process creation are denied. No unrestricted fallback exists.

Preparation prints the fresh workspace, manifest, prompt file, and exact approved Python argv. It creates a local Git baseline commit for tracked-file and new-file review. Original tests, README, package `__init__.py`, and `.gitignore` have frozen hashes and read-only file modes; Git metadata is frozen separately. Git commands are harness-owned with hooks, external configuration, signing, and automatic index refresh disabled. A driver integrity check runs repeatedly and after verification; an indirect test alteration through generated code also fails qualification.

## Run the actual local model

Start the application development transport on loopback using its normal launch instructions. Then run:

```sh
python3 scripts/realistic-e2e.py --run --url http://127.0.0.1:8787
```

To use a previously prepared, unchanged fixture, add `--manifest /absolute/path/to/manifest.json`. The default model is `gemma4:12b-mlx`, the run budget is 80 model calls, and the harness deadline is 20 minutes. The driver refuses to begin while another app run or evaluation is active. It never downloads a model or substitutes a mock.

The driver creates a visible workspace, agent, and conversation through the existing application API. Open that conversation in the desktop UI to watch the same execution. It approves only `write_file`/`edit_file` targeting the six named source files or the one new test file, and only the exact printed Python `unittest discover -s tests -v` argv. New fixtures additionally require `readOnly:true` on `run_command`, making the workspace read-only during tests while permitting private temporary database/export writes. It denies deletes, moves, alternate commands, network/browser/MCP actions, and protected-file edits. Existing application policy and revision checks remain authoritative. Historical manifests lacking `readOnlyTests` retain their original baseline policy; they are never silently upgraded. `--no-read-only-tests` exists only for explicit comparison with that older baseline.

For a manual native-UI run, add the printed workspace path, create a conversation, and paste the exact contents of `PROMPT.txt`. Apply the same narrow approval policy. The automated driver is preferred when a complete audit record and independent grading are required; running a manual chat alone does not produce harness qualification evidence.

## Evidence and verdicts

Each model attempt has its own `docs/evidence/realistic-TIMESTAMP.json`; `realistic-latest.json` points to the latest result by containing the same report. Previous failed attempts remain preserved. Reports include the prompt/configuration, local runtime/model metadata, baseline tests, exact approval decisions, paginated conversation/tool/error evidence, artifacts, source diffs/hashes, Git HEAD/status/tracked and staged diffs/untracked files, final public and independent tests, and timings. Incremental progress is saved beside the manifest. Historical fixtures without a Git baseline are explicitly marked unavailable rather than silently changed.

After the coding task passes, a separate read-only conversation turn must recall a randomly generated marker and the work from persisted conversation without calling tools. A successful model completion message alone is not a passing grade. The full verdict additionally requires both test suites to pass, all protected inputs to remain unchanged, a new regression-test file, and a successful action-free continuity check.
