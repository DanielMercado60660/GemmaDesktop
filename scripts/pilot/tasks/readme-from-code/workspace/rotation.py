"""Retention rules for timestamped backup archives.

An archive is any file named <prefix>-YYYYMMDD-HHMMSS.tar.gz or
<prefix>-YYYYMMDD-HHMMSS.zip. The timestamp in the name (local time) is what
counts, not the file's modification time.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path

STAMP_FORMAT = "%Y%m%d-%H%M%S"
_NAME = re.compile(r"^(?P<prefix>.+)-(?P<stamp>\d{8}-\d{6})\.(?:tar\.gz|zip)$")


@dataclass(frozen=True)
class Policy:
    """Grandfather-father-son retention: how many periods of each kind to keep.

    For each kind, the newest archive of each of the most recent N periods is
    kept (days, ISO weeks, calendar months). An archive kept for any reason is
    kept. 0 turns a kind off.
    """

    daily: int = 7
    weekly: int = 4
    monthly: int = 12

    def keeps_nothing(self) -> bool:
        return self.daily <= 0 and self.weekly <= 0 and self.monthly <= 0


DEFAULT_POLICY = Policy()


@dataclass(frozen=True)
class Backup:
    path: Path
    prefix: str
    taken: datetime


def parse_name(path: Path) -> Backup | None:
    m = _NAME.match(path.name)
    if not m:
        return None
    try:
        taken = datetime.strptime(m["stamp"], STAMP_FORMAT)
    except ValueError:
        return None
    return Backup(path, m["prefix"], taken)


def scan(directory: Path, prefix: str) -> list[Backup]:
    """Archives in `directory` (not recursive) with this prefix, newest first."""
    if not directory.is_dir():
        return []
    found = []
    for p in directory.iterdir():
        b = parse_name(p) if p.is_file() else None
        if b is not None and b.prefix == prefix:
            found.append(b)
    return sorted(found, key=lambda b: b.taken, reverse=True)


def _newest_per_period(backups: list[Backup], count: int, key) -> list[Backup]:
    chosen, seen = [], set()
    for b in backups:  # newest first
        k = key(b.taken)
        if k in seen:
            continue
        if len(seen) >= count:
            break
        seen.add(k)
        chosen.append(b)
    return chosen


def plan(backups: list[Backup], policy: Policy = DEFAULT_POLICY):
    """Split archives into (keep, drop). `keep` pairs each archive with its reasons."""
    reasons: dict[Path, list[str]] = {}
    rules = [
        ("daily", policy.daily, lambda t: t.date()),
        ("weekly", policy.weekly, lambda t: t.isocalendar()[:2]),
        ("monthly", policy.monthly, lambda t: (t.year, t.month)),
    ]
    for label, count, key in rules:
        for b in _newest_per_period(backups, count, key):
            reasons.setdefault(b.path, []).append(label)
    keep = [(b, reasons[b.path]) for b in backups if b.path in reasons]
    drop = [b for b in backups if b.path not in reasons]
    return keep, drop


def older_than(backups: list[Backup], days: int, min_keep: int, now: datetime | None = None) -> list[Backup]:
    """Archives taken more than `days` days ago, never touching the newest `min_keep`."""
    cutoff = (now or datetime.now()) - timedelta(days=days)
    return [b for b in backups[max(min_keep, 0):] if b.taken < cutoff]
