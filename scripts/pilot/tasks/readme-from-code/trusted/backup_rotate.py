#!/usr/bin/env python3
"""Make timestamped archives of a directory and thin out old ones.

Retention rules live in rotation.py.
"""
from __future__ import annotations

import argparse
import fnmatch
import json
import os
import shutil
import sys
import tarfile
import zipfile
from datetime import datetime
from pathlib import Path

import rotation

__version__ = "1.3.0"

DEFAULT_BACKUP_DIR = os.environ.get("BACKUP_ROTATE_DIR", "backups")
DEFAULT_PREFIX = "backup"
DEFAULT_MAX_AGE_DAYS = 90
DEFAULT_MIN_KEEP = 3
TRASH_DIR = ".trash"


def log(args, message: str) -> None:
    if args.verbose:
        print(message, file=sys.stderr)


def _excluded(rel: str, patterns: list[str]) -> bool:
    parts = rel.split("/")
    return any(fnmatch.fnmatch(rel, p) or any(fnmatch.fnmatch(part, p) for part in parts) for p in patterns)


def cmd_create(args) -> int:
    source = Path(args.source)
    if not source.is_dir():
        print(f"error: {source} is not a directory", file=sys.stderr)
        return 2
    dest = Path(args.backup_dir)
    dest.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime(rotation.STAMP_FORMAT)
    target = dest / f"{args.prefix}-{stamp}.{args.format}"
    if target.exists():
        print(f"error: {target} already exists", file=sys.stderr)
        return 1
    dest_resolved = dest.resolve()
    files = []
    for path in sorted(source.rglob("*")):
        if not path.is_file() or dest_resolved in path.resolve().parents:
            continue
        rel = path.relative_to(source).as_posix()
        if _excluded(rel, args.exclude):
            log(args, f"skip {rel}")
            continue
        log(args, f"add  {rel}")
        files.append((path, rel))
    if args.format == "zip":
        with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as zf:
            for path, rel in files:
                zf.write(path, rel)
    else:
        with tarfile.open(target, "w:gz") as tf:
            for path, rel in files:
                tf.add(path, arcname=rel)
    print(f"created {target} ({len(files)} files)")
    return 0


def _policy(args) -> rotation.Policy:
    return rotation.Policy(daily=args.keep_daily, weekly=args.keep_weekly, monthly=args.keep_monthly)


def _scan(args) -> list[rotation.Backup]:
    backups = rotation.scan(Path(args.backup_dir), args.prefix)
    if not backups:
        print(f"no '{args.prefix}' archives in {args.backup_dir}", file=sys.stderr)
    for b in backups:
        log(args, f"found {b.path.name}")
    return backups


def _remove(args, backup: rotation.Backup) -> None:
    if args.dry_run:
        print(f"would remove {backup.path.name}")
    elif getattr(args, "trash", False):
        trash = Path(args.backup_dir) / TRASH_DIR
        trash.mkdir(exist_ok=True)
        shutil.move(str(backup.path), str(trash / backup.path.name))
        print(f"trashed {backup.path.name}")
    else:
        backup.path.unlink()
        print(f"removed {backup.path.name}")


def cmd_plan(args) -> int:
    keep, drop = rotation.plan(_scan(args), _policy(args))
    if args.json:
        doc = {"keep": [{"file": b.path.name, "reasons": r} for b, r in keep], "drop": [b.path.name for b in drop]}
        print(json.dumps(doc, indent=2))
        return 0
    for b, reasons in keep:
        print(f"keep  {b.path.name}  ({', '.join(reasons)})")
    for b in drop:
        print(f"drop  {b.path.name}")
    print(f"{len(keep)} kept, {len(drop)} to remove")
    return 0


def cmd_apply(args) -> int:
    _, drop = rotation.plan(_scan(args), _policy(args))
    for b in drop:
        _remove(args, b)
    print(f"{len(drop)} archive(s) {'to remove' if args.dry_run else 'removed'}")
    return 0


def cmd_prune(args) -> int:
    if args.max_age < 1:
        print("error: --max-age must be at least 1", file=sys.stderr)
        return 2
    old = rotation.older_than(_scan(args), days=args.max_age, min_keep=args.min_keep)
    for b in old:
        _remove(args, b)
    print(f"{len(old)} archive(s) older than {args.max_age} days {'to remove' if args.dry_run else 'removed'}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="backup_rotate.py",
        description="Create timestamped archives of a directory and rotate old ones.",
    )
    parser.add_argument("-v", "--verbose", action="store_true", help="log each file considered to stderr")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    sub = parser.add_subparsers(dest="command", metavar="COMMAND", required=True)

    location = argparse.ArgumentParser(add_help=False)
    location.add_argument("-d", "--backup-dir", default=DEFAULT_BACKUP_DIR, metavar="DIR",
                          help="directory that holds the archives (env: BACKUP_ROTATE_DIR)")
    location.add_argument("--prefix", default=DEFAULT_PREFIX,
                          help="archive name prefix; archives with other prefixes are never touched")

    retention = argparse.ArgumentParser(add_help=False)
    retention.add_argument("--keep-daily", type=int, default=rotation.DEFAULT_POLICY.daily, metavar="N",
                           help="keep the newest archive of each of the last N days")
    retention.add_argument("--keep-weekly", type=int, default=rotation.DEFAULT_POLICY.weekly, metavar="N",
                           help="keep the newest archive of each of the last N ISO weeks")
    retention.add_argument("--keep-monthly", type=int, default=rotation.DEFAULT_POLICY.monthly, metavar="N",
                           help="keep the newest archive of each of the last N calendar months")

    p = sub.add_parser("create", parents=[location], help="archive a directory into the backup directory")
    p.add_argument("source", help="directory to archive")
    p.add_argument("--format", choices=("tar.gz", "zip"), default="tar.gz", help="archive format")
    p.add_argument("--exclude", action="append", default=[], metavar="PATTERN",
                   help="glob matched against relative paths and each path component; repeatable")
    p.set_defaults(func=cmd_create)

    p = sub.add_parser("plan", parents=[location, retention], help="show which archives the retention policy keeps")
    p.add_argument("--json", action="store_true", help="print the plan as JSON")
    p.set_defaults(func=cmd_plan)

    p = sub.add_parser("apply", parents=[location, retention], help="remove archives the retention policy drops")
    p.add_argument("-n", "--dry-run", action="store_true", help="only print what would be removed")
    p.add_argument("--trash", action="store_true", help=f"move archives into DIR/{TRASH_DIR}/ instead of deleting them")
    p.set_defaults(func=cmd_apply)

    p = sub.add_parser("prune", parents=[location], help="remove archives older than a cutoff")
    p.add_argument("--max-age", type=int, default=DEFAULT_MAX_AGE_DAYS, metavar="DAYS",
                   help="remove archives older than DAYS days (default: %(default)s)")
    p.add_argument("--min-keep", type=int, default=DEFAULT_MIN_KEEP, metavar="N",
                   help="never remove the newest N archives, however old")
    p.add_argument("-n", "--dry-run", action="store_true", help="only print what would be removed")
    p.set_defaults(func=cmd_prune)
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if hasattr(args, "keep_daily") and _policy(args).keeps_nothing():
        print("error: retention policy keeps nothing; refusing", file=sys.stderr)
        return 2
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
