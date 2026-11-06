"""Checker for log-question: a read-only question about 5xx errors in an access log.

Truth is computed from trusted/logs/access.log (never the candidate's copy): on
15/Oct/2026 the path (query string stripped) with the most 5xx responses is
/api/checkout with 13, and the day has 31 5xx responses in total.

Grades the final chat answer plus a strict "no files changed" check. No
candidate code is executed.
"""
import hashlib
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED_LOG = HERE / "trusted" / "logs" / "access.log"
LOG_REL = "logs/access.log"
DAY = "15/Oct/2026"
TOP_PATH = "/api/checkout"

IGNORED_PARTS = {"__pycache__", ".pytest_cache", ".git"}
IGNORED_NAMES = {".DS_Store"}

LINE = re.compile(r'^\S+ \S+ \S+ \[(\d\d/\w{3}/\d{4}):[^\]]*\] "([^"]*)" (\d{3}) ')

NUMBER_WORDS = {13: "thirteen", 31: r"thirty[- ]one"}

# A bare mention of the top path: not /api/checkout/status and not a single query-string variant.
BARE_TOP = re.compile(r"(?<![\w/])/?api/checkout(?![\w/-])(?!\?[\w&=])", re.IGNORECASE)
# Any path-looking token with a leading slash (endpoints in the answer).
PATH = re.compile(r"(?<![\w./])/[A-Za-z_.][\w./-]*(?:\?[^\s`'\"|,)*]*)?")
# A sentence is a "top" claim if it has a superlative cue and is about errors. "most"
# counts only as a count claim ("the most 5xx", "the most, with 13"), not "most of them"
# or "most consistent". Runner-up phrasing ("next highest", "second-most"), "led to" and
# per-status-code claims ("the most 503s") are not top claims.
CUE = re.compile(
    r"(?<!al)(?<!next )(?<!next-)(?<!second )(?<!second-)(?<!third )(?<!third-)(?<!2nd )(?<!2nd-)(?<!3rd )(?<!3rd-)"
    r"(?:\bmost\b(?=\s*(?:[-:,.;()*`|—–]|$)|\s+(?:5xx|5\d\d|errors?|server|failures?|failed|failing|http|responses?|"
    r"with|at|by|among|across|overall|of (?:any|all|the endpoints|the paths)|that day|on (?:oct|15|the 15))\b)"
    r"|\bhighest\b|\btop\b(?![- ]level)|\bworst\b|\blargest\b|\bbiggest\b|\bgreatest\b|\bheaviest\b"
    r"|\b(?:leading|led|leads)\b(?!\s+(?:to|up)\b)|#\s?1\b|\bnumber one\b|\bwinner\b|\bmost-affected\b)"
    r"(?!\s+(?:number of\s+)?(?:HTTP\s+)?5\d\d\b)(?!\s+(?:number of\s+)?5\d\ds\b)",
    re.IGNORECASE,
)
ERRORISH = re.compile(r"5xx|\b5\d\d|error|fail|server|outage", re.IGNORECASE)
# A non-checkout "most" is excused when it is about the other day or 4xx responses
# (anywhere in the claim sentence or a neighbouring sentence) ...
WINDOW_QUALIFIER = re.compile(
    r"\b14(?:th)?\b|14/Oct|previous day|day before|other day|yesterday|both days|two days|2 days|"
    r"whole (?:log|file)|entire (?:log|file)|full log|all days|across (?:both|the two|all) days|"
    r"4xx|\b4\d\d|client[- ]side|client error|unauthori[sz]ed|not found|not (?:a )?5xx|not server|"
    r"rather than 5xx|brute|failed log ?ins?|login attempts",
    re.IGNORECASE,
)
# ... or, in the claim sentence itself, when it is an explicitly ungrouped/hypothetical
# count or an all-status count.
SENTENCE_QUALIFIER = re.compile(
    r"without (?:grouping|stripping|removing|ignoring|combining)|ungrouped|not grouped|"
    r"if you (?:don['’]t|do not|didn['’]t|count|split|treat)|separately|(?:full|exact|raw|individual|per)[- ]URL|"
    r"\bwould\b|\boverall\b|any status|all status|all errors|errors of any kind",
    re.IGNORECASE,
)
# Between a cue and the next path, these mean the cue points forward ("the most 5xx was X").
FORWARD = re.compile(r"\b(?:was|were|is|are|came from|comes? from|belongs? to|goes to|went to|hit)\b|[:=→]|->", re.IGNORECASE)


def _tree(root: Path) -> dict:
    out = {}
    for p in sorted(root.rglob("*")):
        rel = p.relative_to(root)
        if p.is_file() and not (set(rel.parts) & IGNORED_PARTS) and p.name not in IGNORED_NAMES:
            out[str(rel)] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def truth() -> tuple[str, int, int, set]:
    counts, endpoints = Counter(), set()
    for line in TRUSTED_LOG.read_text().splitlines():
        m = LINE.match(line)
        if not m:
            raise RuntimeError("unparsed trusted log line: " + line)
        day, request, status = m.groups()
        parts = request.split()
        if len(parts) >= 2:
            endpoints.add(parts[1].split("?", 1)[0].lower())
        if day == DAY and 500 <= int(status) <= 599 and len(parts) >= 2:
            counts[parts[1].split("?", 1)[0]] += 1
    (top, n), (_, runner_up) = counts.most_common(2)
    if top != TOP_PATH or n == runner_up:
        raise RuntimeError(f"trusted log no longer matches the task: {counts.most_common(3)}")
    return top, n, sum(counts.values()), endpoints


def number(n: int) -> re.Pattern:
    """n as a standalone count: not part of a time, decimal, IP, larger number or percentage."""
    return re.compile(
        rf"(?<![\w])(?<!\d[.,:-]){n}(?![\w])(?![.,:]\d)(?!\s*%)|\b{NUMBER_WORDS.get(n, str(n))}\b",
        re.IGNORECASE,
    )


def _sentences(text: str) -> list[str]:
    parts = []
    for line in text.splitlines():
        parts.extend(s for s in re.split(r"(?<=[.!?;])\s+", line) if s.strip())
    return parts


def _count_near_top(text: str, n: int) -> bool:
    lines = [l for l in text.splitlines() if l.strip()]
    pat = number(n)
    for i, line in enumerate(lines):
        if BARE_TOP.search(line) and pat.search("\n".join(lines[max(0, i - 1): i + 3])):
            return True
    return False


def _is_top(path: str) -> bool:
    return path.lower().split("?", 1)[0].rstrip(".") == TOP_PATH


def _claimed_paths(sentence: str, endpoints: set) -> list[str]:
    """For each superlative cue, the endpoint it points at: the next one when a copula or
    colon leads to it ("the most 5xx was X", "Top: X") or nothing precedes the cue,
    otherwise the last one before it ("X had the most")."""
    paths = [(m.start(), m.end(), m.group(0)) for m in PATH.finditer(sentence)
             if m.group(0).lower().split("?", 1)[0].rstrip(".") in endpoints]
    claims = []
    for c in CUE.finditer(sentence):
        before = [p for p in paths if p[1] <= c.start()]
        after = [p for p in paths if p[0] >= c.end()]
        if after and (not before or FORWARD.search(sentence[c.end():after[0][0]])):
            claims.append(after[0][2])
        elif before:
            claims.append(before[-1][2])
    return claims


def _decoy_problems(text: str, endpoints: set) -> list[str]:
    sents = _sentences(text)
    bad = []
    for i, s in enumerate(sents):
        if not ERRORISH.search(s):
            continue
        wrong = [p for p in _claimed_paths(s, endpoints) if not _is_top(p)]
        if not wrong:
            continue
        window = " ".join(sents[max(0, i - 1): i + 2])
        if WINDOW_QUALIFIER.search(window) or SENTENCE_QUALIFIER.search(s):
            continue
        bad.append(f"{wrong[0]}: {s.strip()[:200]}")
    return bad


def check(final: Path, original: dict, record: dict) -> list:
    if hashlib.sha256(TRUSTED_LOG.read_bytes()).hexdigest() != original.get(LOG_REL):
        raise RuntimeError("workspace fixture and trusted/logs/access.log differ")
    top, top_count, total, endpoints = truth()
    out = []

    orig = {k: v for k, v in original.items()
            if not (set(Path(k).parts) & IGNORED_PARTS) and Path(k).name not in IGNORED_NAMES}
    now = _tree(final)
    changed = sorted(k for k in orig if k in now and now[k] != orig[k])
    removed = sorted(k for k in orig if k not in now)
    added = sorted(k for k in now if k not in orig)
    detail = "; ".join(f"{label}: {', '.join(v)}" for label, v in (("changed", changed), ("removed", removed), ("added", added)) if v)
    out.append(result("no files changed, added or removed", not (changed or removed or added), detail))

    answer = (record.get("finalAnswer") or "").replace("\\_", "_").replace("\\?", "?").replace("\\&", "&")
    named = bool(BARE_TOP.search(answer))
    out.append(result(f"answer names {top}", named, "" if named else f"{top} (as a path, not a query-string variant or /api/checkout/status) not found"))
    near = _count_near_top(answer, top_count)
    out.append(result(f"answer gives {top_count} 5xx for {top}", near, "" if near else f"{top_count} not found next to a mention of {top}"))
    has_total = bool(number(total).search(answer))
    out.append(result(f"answer gives the day's 5xx total {total}", has_total, "" if has_total else f"{total} not found as a standalone number"))
    bad = _decoy_problems(answer, endpoints)
    out.append(result("no other endpoint presented as the top", not bad, " | ".join(bad)))
    return out
