# backup_rotate

`backup_rotate.py` makes timestamped archives of a directory and keeps the
backup directory from growing forever. It has four subcommands: `create` makes
a new archive, `plan` shows what the retention policy would keep, `apply`
removes what the policy drops, and `prune` removes archives older than a cutoff.

It only needs Python 3.9+ and the standard library (`backup_rotate.py` plus
`rotation.py`, which holds the retention rules).

## How it works

Archives are files named `<prefix>-YYYYMMDD-HHMMSS.tar.gz` or
`<prefix>-YYYYMMDD-HHMMSS.zip` inside one backup directory. The timestamp in the
name is what counts, not the file's modification time. Files with another
prefix, or that don't match the pattern, are never touched.

Retention is grandfather-father-son: the newest archive of each of the last N
days, N ISO weeks and N calendar months is kept; an archive kept for any reason
stays. Setting all three counts to 0 is refused.

## Running it

```sh
python3 backup_rotate.py [-v] COMMAND [options]
python3 backup_rotate.py --help
python3 backup_rotate.py plan --help
```

Global options go **before** the subcommand:

| Option | Default | Description |
| --- | --- | --- |
| `-v`, `--verbose` | off | Log each file considered to stderr. |
| `--version` | | Print the version (1.3.0) and exit. |
| `-h`, `--help` | | Show help. |

Options shared by every subcommand:

| Option | Default | Description |
| --- | --- | --- |
| `-d DIR`, `--backup-dir DIR` | `backups` (or `$BACKUP_ROTATE_DIR` if set) | Directory that holds the archives. |
| `--prefix PREFIX` | `backup` | Archive name prefix; archives with other prefixes are ignored. |

## Subcommands

### create

Archive the directory `SOURCE` into the backup directory as
`<prefix>-YYYYMMDD-HHMMSS.<format>`. The backup directory is created if needed
and is skipped if it lies inside `SOURCE`.

| Option | Default | Description |
| --- | --- | --- |
| `SOURCE` | (required) | Directory to archive. |
| `--format {tar.gz,zip}` | `tar.gz` | Archive format. |
| `--exclude PATTERN` | none | Glob matched against relative paths and each path component; repeat it for several patterns. |

```sh
python3 backup_rotate.py create ~/projects/site --exclude '*.log' --exclude node_modules
```

### plan

Show which archives the retention policy keeps (and why) and which it would
remove. Nothing is changed.

| Option | Default | Description |
| --- | --- | --- |
| `--keep-daily N` | `7` | Keep the newest archive of each of the last N days. |
| `--keep-weekly N` | `4` | Keep the newest archive of each of the last N ISO weeks. |
| `--keep-monthly N` | `12` | Keep the newest archive of each of the last N calendar months. |
| `--json` | off | Print the plan as JSON. |

```sh
python3 backup_rotate.py plan -d /srv/backups --keep-monthly 24
```

### apply

Remove the archives the retention policy drops. Takes the same
`--keep-daily`, `--keep-weekly` and `--keep-monthly` options as `plan` (same
defaults: 7, 4 and 12), plus:

| Option | Default | Description |
| --- | --- | --- |
| `-n`, `--dry-run` | off | Only print what would be removed. |
| `--trash` | off | Move archives into `DIR/.trash/` instead of deleting them. |

```sh
python3 backup_rotate.py -v apply --trash
```

### prune

Remove archives older than a cutoff, regardless of the retention policy.

| Option | Default | Description |
| --- | --- | --- |
| `--max-age DAYS` | `90` | Remove archives older than DAYS days. |
| `--min-keep N` | `3` | Never remove the newest N archives, however old. |
| `-n`, `--dry-run` | off | Only print what would be removed. |

```sh
python3 backup_rotate.py prune --max-age 30 --min-keep 5 --dry-run
```
