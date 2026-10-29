# Fieldnote task tracker: repair and CSV export

This is a real, dependency-free Python package backed by an on-disk SQLite database. Public entry points are `tracker.TaskService` and `tracker.cli.main(argv)`. Preserve their signatures and the immutable original tests.

## Reported defects

1. Status changes appear successful but disappear after reopening the database.
2. Combining `status` and `query` returns tasks that match only one filter. Filters must both match. Query is an ASCII case-insensitive **literal** title substring; `%`, `_`, backslashes, and quotes are data, never SQL syntax.
3. Pagination must be deterministic: newest `created_at` first, then highest `id` first for tied timestamps. Filter before paging. `TaskPage.total` is the total number of filtered matches, independent of `limit` and `offset`.

## Requested feature

Implement `csv_export.serialize(tasks)`, `csv_export.write_tasks(destination, tasks)`, `TaskService.export_csv(destination, *, status=None, query=None)`, and the already-declared CLI `export` command.

- CSV header: `id,title,status,priority,created_at`, in exactly that order.
- Preserve commas, double quotes, embedded newlines, and Unicode titles using standard CSV quoting. Encode files as UTF-8 without a BOM; use LF record terminators.
- Export **all** matching tasks in the same deterministic order as listing. The normal listing limit (maximum 100) must not truncate exports.
- Empty exports still have the header. The service/writer returns the number of task rows written.
- Export filters share the listing semantics and reject invalid status values.
- The CLI prints JSON `{"exported": count, "path": destination}` and returns 0 after writing the file.
- Keep existing validation and parameterized SQL. No external dependencies, networking, subprocesses, migrations, or daemon are needed.

## Scope and verification

You may edit only `tracker/db.py`, `tracker/models.py`, `tracker/repository.py`, `tracker/service.py`, `tracker/csv_export.py`, `tracker/cli.py`, and create `tests/test_regressions.py`. Add meaningful tests for the repaired edge cases and export feature in that new test file. Do not edit, delete, rename, replace, or disable any original test, this README, or `tracker/__init__.py`.

Run the exact Python `unittest discover -s tests -v` command supplied by the user. A separate acceptance suite outside this workspace will verify durable SQLite behavior, filter/pagination edge cases, CLI integration, and CSV correctness. It is intentionally unavailable to the agent.
