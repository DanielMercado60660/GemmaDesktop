"""Checker for codebase-qa: a read-only question about how the listening port is chosen.

Truth (service/settings_loader.py): DEFAULT_PORT = 8085, overridden by the
NOTES_PORT environment variable. Decoys: README "Deployment notes (2023)" says
8000; legacy/old_server.py has PORT = 5000.

Grades the final chat answer plus a strict "no files changed" check. No
candidate code is executed.
"""
import hashlib
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result  # noqa: E402

IGNORED_PARTS = {"__pycache__", ".pytest_cache", ".git"}
IGNORED_NAMES = {".DS_Store"}

PORT = re.compile(r"(?<![\d.,])8,?085(?!\d|[.,]\d)")
ENV_VAR = re.compile(r"(?<![A-Za-z0-9_])NOTES_PORT(?![A-Za-z0-9_])")
FILE = re.compile(r"settings_loader", re.IGNORECASE)
DECOY = re.compile(r"(?<![\d.,])(?:8,?000|5,?000)(?!\d|[.,]\d)")

# A decoy number in a sentence with one of these words is being presented as a port.
CUE = re.compile(r"\bports?\b|default|listen|\bbind|\bbound\b|\bserv(?:es|ing|ed)\b|\bruns?\b|running|effective|\bactual", re.IGNORECASE)
# Words that mark a decoy mention as non-authoritative (old, documented elsewhere, unused).
QUALIFIER = re.compile(
    r"legacy|old_server|\bold(?:er)?\b|outdated|out[- ]of[- ]date|\bstale\b|obsolete|deprecated|unused|"
    r"not used|(?:is|are)n['’]t used|never used|no longer|superseded|replaced|prototype|histor(?:y|ical)|"
    r"previous|former|earlier|\bv1\b|2023|readme|documentation|\bdocs?\b|deployment|nginx|"
    r"misleading|incorrect|\bwrong\b|ignore|decoy|inaccurate|irrelevant|leftover|dead code|"
    r"nothing imports|contradict|conflict|discrepan|mismatch|unrelated|red herring|elsewhere|"
    # Specific negations only: a bare "not" would excuse "if NOTES_PORT is not set it listens on 8000".
    r"\bneither\b|"
    r"(?:\bnot|n['’]t)\s+(?:actually\s+|really\s+)?(?:used|use[sd]?\b|read\b|load(?:ed)?\b|the\b|imported|relevant|current|accurate|correct|true|"
    r"apply|applicable|matter|affect|control|change|referenced|wired|what|where|in use|live|authoritative|reliable|"
    r"up[- ]to[- ]date)",
    re.IGNORECASE,
)
# Contrast words count only when the correct port appears in the same window.
CONTRAST = re.compile(r"\bbut\b|however|whereas|\bwhile\b|although|though|instead|rather than|\bvs\.?|versus", re.IGNORECASE)
# A decoy number used as an override example ("NOTES_PORT=8000", "set NOTES_PORT
# to 5000") is not presented as the default; those values are excused in the
# window. "If NOTES_PORT is not set it listens on 8000" has no assignment and
# still fails.
ASSIGN = re.compile(
    r"(?<![A-Za-z0-9_])NOTES_PORT[`'\"]?\s*(?:(?:is|was)\s+)?(?:set\s+)?(?:=|:=|\bto\b|\bas\b)\s*[`'\"]?(8,?000|5,?000)(?!\d|[.,]\d)")


def _tree(root: Path) -> dict:
    out = {}
    for p in sorted(root.rglob("*")):
        rel = p.relative_to(root)
        if p.is_file() and not (set(rel.parts) & IGNORED_PARTS) and p.name not in IGNORED_NAMES:
            out[str(rel)] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def _sentences(text: str) -> list[str]:
    parts = []
    for line in text.splitlines():
        parts.extend(s for s in re.split(r"(?<=[.!?])\s+", line) if s.strip())
    return parts


def _decoy_problems(text: str) -> list[str]:
    sents = _sentences(text)
    bad = []
    for i, s in enumerate(sents):
        window = " ".join(sents[max(0, i - 1): i + 2])
        t = s
        for value in {m.group(1).replace(",", "") for m in ASSIGN.finditer(window)}:
            t = re.sub(rf"(?<![\d.,]){value[0]},?{value[1:]}(?!\d|[.,]\d)", "", t)
        if not DECOY.search(t) or not CUE.search(t):
            continue
        if QUALIFIER.search(window):
            continue
        if CONTRAST.search(window) and PORT.search(window):
            continue
        bad.append(s.strip()[:200])
    return bad


def check(final: Path, original: dict, record: dict) -> list:
    out = []

    orig = {k: v for k, v in original.items()
            if not (set(Path(k).parts) & IGNORED_PARTS) and Path(k).name not in IGNORED_NAMES}
    now = _tree(final)
    changed = sorted(k for k in orig if k in now and now[k] != orig[k])
    removed = sorted(k for k in orig if k not in now)
    added = sorted(k for k in now if k not in orig)
    detail = "; ".join(f"{label}: {', '.join(v)}" for label, v in (("changed", changed), ("removed", removed), ("added", added)) if v)
    out.append(result("no files changed, added or removed", not (changed or removed or added), detail))

    answer = (record.get("finalAnswer") or "").replace("\\_", "_")
    out.append(result("answer gives default port 8085", bool(PORT.search(answer)), "" if PORT.search(answer) else "8085 not found in answer"))
    out.append(result("answer names env var NOTES_PORT", bool(ENV_VAR.search(answer)), "" if ENV_VAR.search(answer) else "exact identifier NOTES_PORT not found"))
    out.append(result("answer names service/settings_loader.py", bool(FILE.search(answer)), "" if FILE.search(answer) else "settings_loader not mentioned"))
    bad = _decoy_problems(answer)
    out.append(result("decoy ports (8000/5000) not presented as the port", not bad, " | ".join(bad)))
    return out
