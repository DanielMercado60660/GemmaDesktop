"""Checker for organize-notes: dated notes move to notes/<year>/ byte-for-byte.

Grades only the final files. Expected hashes come from `original` (the pristine
fixture tree the driver recorded), never from the candidate's copy.
"""
import hashlib
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result  # noqa: E402

# Ten dated notes (YYYY-MM-DD-title.md directly under notes/) that must move.
DATED = [
    "2024-09-12-kickoff.md",
    "2024-11-20-standup.md",
    "2024-12-18-retro.md",
    "2025-01-08-planning.md",
    "2025-03-04-retro.md",
    "2025-06-17-1on1-priya.md",
    "2025-09-30-launch-checklist.md",
    "2026-01-15-ideas.md",
    "2026-02-03-standup.md",
    "2026-04-22-vendor-call.md",
]
# Files that must stay exactly where they are (2025-summary.md is not a dated note).
STAY = ["notes/README.txt", "notes/todo.txt", "notes/draft-ideas.md", "notes/2025-summary.md"]

SKIP_PARTS = {"__pycache__", ".pytest_cache", ".git"}
SKIP_NAMES = {".DS_Store"}


def _sha(p: Path) -> str:
    return hashlib.sha256(p.read_bytes()).hexdigest()


def _files(root: Path) -> dict[str, str]:
    out = {}
    for p in sorted(root.rglob("*")):
        rel = p.relative_to(root)
        if set(rel.parts) & SKIP_PARTS or p.name in SKIP_NAMES:
            continue
        if p.is_symlink():
            out[rel.as_posix()] = "symlink"
        elif p.is_file():
            out[rel.as_posix()] = _sha(p)
    return out


def check(final: Path, original: dict, record: dict) -> list[dict]:
    orig = {k.replace("\\", "/"): v for k, v in original.items() if Path(k).name not in SKIP_NAMES}
    fixture = {f"notes/{n}" for n in DATED} | set(STAY)
    if set(orig) != fixture:
        raise RuntimeError(f"fixture drift: original tree {sorted(orig)} != expected {sorted(fixture)}")

    moves = {f"notes/{n}": f"notes/{n[:4]}/{n}" for n in DATED}
    expected = {new: orig[old] for old, new in moves.items()} | {p: orig[p] for p in STAY}
    got = _files(final)
    out = []

    missing = [new for new in moves.values() if new not in got]
    changed = [new for new in moves.values() if new in got and got[new] != expected[new]]
    detail = []
    if missing:
        detail.append("missing: " + ", ".join(missing))
    if changed:
        detail.append("content changed or not a regular file: " + ", ".join(changed))
    out.append(result("dated notes in year folders", not missing and not changed, "; ".join(detail)))

    left = [old for old in moves if (final / old).exists() or (final / old).is_symlink()]
    out.append(result("dated notes removed from notes/ root", not left, "still present: " + ", ".join(left) if left else ""))

    bad = [p for p in STAY if got.get(p) != expected[p]]
    out.append(result("undated files untouched in place", not bad, "moved, missing or changed: " + ", ".join(bad) if bad else ""))

    extra = sorted(set(got) - set(expected))
    out.append(result("no extra files", not extra, "unexpected: " + ", ".join(extra) if extra else ""))
    return out
