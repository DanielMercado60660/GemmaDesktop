# Changelog

## 1.3.0 (2026-08-19)
- `prune`: `--keep-days` is now `--max-age`, and the default cutoff went from 30 to 90 days.
- `prune`: new `--min-keep` so a long gap between backups can't empty the directory.
- `apply`: new `--trash` to move archives aside instead of deleting them.
- Monthly retention now keeps 12 months by default (was 6).

## 1.2.0 (2026-03-02)
- `plan --json` for scripts.
- The `BACKUP_ROTATE_DIR` environment variable sets the default backup directory.

## 1.1.0 (2025-11-10)
- `create --exclude PATTERN`, repeatable.
- `-n` as a short form of `--dry-run`.

## 1.0.0 (2025-09-01)
- First version: `create`, `plan`, `apply` and `prune --keep-days` (default 30).
- Default retention: 7 daily, 4 weekly, 6 monthly.
