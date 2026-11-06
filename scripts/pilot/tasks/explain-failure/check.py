"""Checker for explain-failure: read-only diagnosis of one failing test.

Truth: `python3 -m unittest discover -s tests` fails exactly one test,
tests/test_picklist.py PickSheetTests.test_pick_sheet_single_zone. Root cause:
warehouse/picklist.py `def group_by_zone(lines, groups={})` has a mutable
default dict that is created once and shared by every call that omits
`groups`. pick_sheet() relies on that default, so lines from the earlier
pick_sheet tests (zones R, A, C, D) are still in the dict when the single-zone
test runs. The test passes on its own; build_wave() passes its own dict and is
unaffected.

Graded on the final chat answer plus a check that no existing file was changed
(added scratch files are tolerated and reported). No candidate code is run.
"""
import hashlib
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result  # noqa: E402

HERE = Path(__file__).resolve().parent
SOURCE = "warehouse/picklist.py"
FAILING_TEST = "test_pick_sheet_single_zone"
FUNCTION = "group_by_zone"
LINE_TOLERANCE = 2
MAX_RANGE_SPAN = 8

IGNORED_PARTS = {"__pycache__", ".pytest_cache", ".git"}
IGNORED_NAMES = {".DS_Store"}

TEST_NAME = re.compile(rf"(?<![A-Za-z0-9_]){FAILING_TEST}(?![A-Za-z0-9_])")
# The source module, not tests/test_picklist.py (the "_" before it is excluded).
FILE = re.compile(r"(?<![A-Za-z0-9_])picklist(?![A-Za-z0-9_])", re.IGNORECASE)
FUNC = re.compile(rf"(?<![A-Za-z0-9_]){FUNCTION}(?![A-Za-z0-9_])")
LITERAL = re.compile(r"groups\s*=\s*\{\s*\}")
LINE_REFS = [
    re.compile(r"\blines?\s*(?:no\.?|number|#)?\s*:?\s*(\d+)(?:\s*(?:-|–|—|to|through|and)\s*(\d+))?", re.IGNORECASE),
    re.compile(r"picklist\.py\s*[:#]\s*L?(\d+)(?:\s*-\s*L?(\d+))?", re.IGNORECASE),
    re.compile(r"(?<![A-Za-z0-9_])L(\d+)(?:\s*-\s*L?(\d+))?(?![A-Za-z0-9_])"),
]

# Mechanism, family 1: a default argument/parameter (the `{}` default of `groups`),
# or the definition-time evaluation that makes such a default shared.
DEFAULT_ARG = re.compile(
    r"mutable\s+default"
    r"|default\s+(?:[\w'’]+\s+){0,3}?(?:argument|arg|parameter|param|kwarg|dict(?:ionary)?|mapping|\{\s*\}|groups)"
    r"|(?:argument|arg|parameter|param|kwarg|groups)(?:['’]s)?\s+(?:\w+\s+){0,2}?default"
    r"|defaults?\s+to\s+(?:an?\s+)?(?:empty\s+)?(?:dict(?:ionary)?|mapping|\{\s*\})"
    r"|groups\s*=\s*\{\s*\}"
    r"|\{\s*\}\s+(?:as\s+(?:the|its|a)\s+)?default"
    r"|(?:at|during)\s+(?:function\s+)?definition(?:\s+time)?|(?:definition|def)[- ]time"
    r"|when\s+(?:the\s+)?(?:function|def|module)\s+(?:is|was|gets)\s+(?:first\s+)?(?:defined|created|compiled|loaded|imported)",
    re.IGNORECASE,
)
# Mechanism, family 2: that one object is shared / persists across calls.
SHARED = re.compile(
    r"mutable|\bshar(?:e|ed|es|ing)\b|same\s+(?:\w+\s+)?(?:dict(?:ionary)?|object|instance|mapping|\{\s*\})"
    r"|(?:single|one)\s+(?:shared\s+)?(?:dict(?:ionary)?|object|instance|mapping)\b"
    r"|re-?us(?:e|es|ed|ing)\b|persist|survives?|retain|lingers?|stick(?:s|y)\s+around|\bleak"
    r"|(?:evaluated|created|defined|built|initiali[sz]ed|instantiated|made|allocated)\s+(?:only\s+)?once"
    r"|only\s+once|just\s+once|once\s+(?:at|when|per)|(?:definition|def|import|compile)[- ]time"
    r"|carr(?:y|ies|ied)\s+over|accumulat|left\s*over|leftover|stale"
    r"|(?:state|lines|data|entries|zones)\s+(?:from|between|across)\s+(?:\w+\s+){0,2}?(?:calls?|tests?|invocations?|runs?)"
    r"|between\s+(?:\w+\s+)?(?:calls|tests|invocations|runs)|across\s+(?:\w+\s+)?(?:calls|tests|invocations|runs)"
    r"|(?:previous|earlier|prior|other|preceding)\s+(?:\w+\s+)?(?:calls?|tests?|invocations?|orders?)",
    re.IGNORECASE,
)
# The mechanism must be stated near the real culprit (within two sentences), so a
# story blaming the Order.lines default_factory decoy cannot borrow the wording.
ANCHOR = re.compile(rf"(?<![A-Za-z0-9_]){FUNCTION}(?![A-Za-z0-9_])|\bgroups\b|\{{\s*\}}|(?<![A-Za-z0-9_])picklist(?![A-Za-z0-9_])", re.IGNORECASE)
# "not a mutable default problem", "nothing to do with the default argument", ...
# Only the negated phrase is removed, so "isn't the default argument of Order -
# it's the mutable default in group_by_zone" keeps its correct second half.
NEGATED = re.compile(
    r"(?:\bnot\b|n['’]t\b|\bnothing\s+to\s+do\s+with|\bunrelated\s+to|\brather\s+than|\binstead\s+of|\bno\b|\bruled?\s+out)"
    r"\s+(?:(?:a|an|the|due\s+to|caused\s+by|related\s+to|because\s+of|from|about|actually|really|just|simply|any)\s+){0,3}"
    r"(?:(?:mutable|shared)\s+default(?:\s+(?:argument|arg|parameter|param|value|dict(?:ionary)?))?"
    r"|default\s+(?:argument|arg|parameter|param|value|dict(?:ionary)?))",
    re.IGNORECASE,
)


def _tree(root: Path) -> dict:
    out = {}
    for p in sorted(root.rglob("*")):
        rel = p.relative_to(root)
        if p.is_file() and not (set(rel.parts) & IGNORED_PARTS) and p.name not in IGNORED_NAMES:
            out[str(rel)] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def _sentences(text: str) -> list[str]:
    out = []
    for line in text.splitlines():
        if not line.strip() or re.fullmatch(r"\s*(?:```|~~~)\w*\s*", line):
            continue
        out.extend(x for x in re.split(r"(?<=[.!?])\s+", line) if x.strip())
    return out


def _def_line(original: dict) -> int:
    """Line of `def group_by_zone`, read from the trusted copy (which must match the fixture)."""
    trusted = HERE / "trusted" / SOURCE
    data = trusted.read_bytes()
    if original.get(SOURCE) != hashlib.sha256(data).hexdigest():
        raise RuntimeError(f"trusted/{SOURCE} does not match the fixture's {SOURCE}")
    for n, line in enumerate(data.decode().splitlines(), 1):
        if line.startswith(f"def {FUNCTION}("):
            return n
    raise RuntimeError(f"def {FUNCTION} not found in trusted/{SOURCE}")


def _line_hit(text: str, target: int) -> list[str]:
    lo, hi = target - LINE_TOLERANCE, target + LINE_TOLERANCE
    hits = []
    for rx in LINE_REFS:
        for m in rx.finditer(text):
            a = int(m.group(1))
            b = int(m.group(2)) if m.lastindex and m.lastindex >= 2 and m.group(2) else a
            a, b = min(a, b), max(a, b)
            if b - a <= MAX_RANGE_SPAN and a <= hi and b >= lo:
                hits.append(m.group(0).strip())
    return hits


def check(final: Path, original: dict, record: dict) -> list:
    out = []
    def_line = _def_line(original)

    orig = {k: v for k, v in original.items()
            if not (set(Path(k).parts) & IGNORED_PARTS) and Path(k).name not in IGNORED_NAMES}
    now = _tree(final)
    changed = sorted(k for k in orig if k in now and now[k] != orig[k])
    removed = sorted(k for k in orig if k not in now)
    added = sorted(k for k in now if k not in orig)
    detail = "; ".join(f"{label}: {', '.join(v)}" for label, v in (("changed", changed), ("removed", removed), ("added (tolerated)", added)) if v)
    out.append(result("existing files unchanged (nothing fixed)", not (changed or removed), detail))

    raw = record.get("finalAnswer") or ""
    answer = raw.replace("\\_", "_").replace("\\{", "{").replace("\\}", "}")
    plain = re.sub(r"[`*]", "", answer)

    has_test = bool(TEST_NAME.search(answer))
    out.append(result(f"names the failing test {FAILING_TEST}", has_test, "" if has_test else f"{FAILING_TEST} not found in answer"))

    unnegated = NEGATED.sub(" ", plain)
    sents = _sentences(unnegated)
    pair = None
    for i, sent in enumerate(sents):
        d = DEFAULT_ARG.search(sent)
        s = SHARED.search(" ".join(sents[max(0, i - 1): i + 2])) if d else None
        near = " ".join(sents[max(0, i - 2): i + 3])
        anchored = bool(ANCHOR.search(near) or _line_hit(near, def_line))
        if d and s and anchored:
            pair = (d.group(0), s.group(0))
            break
    if pair:
        why = f"default-argument wording {pair[0]!r} with shared/persisting wording {pair[1]!r}"
    elif not DEFAULT_ARG.search(unnegated):
        why = "no default-argument wording"
    elif not any(DEFAULT_ARG.search(x) and SHARED.search(" ".join(sents[max(0, i - 1): i + 2])) for i, x in enumerate(sents)):
        why = "default argument named, but not (nearby) that it is shared or persists across calls"
    else:
        why = f"shared-default wording is not tied to {FUNCTION} / groups / {{}} / picklist / line {def_line} (blames something else?)"
    if NEGATED.search(plain):
        why += " (negated mentions ignored)"
    out.append(result("root cause: shared mutable default argument", bool(pair), why))

    has_file = bool(FILE.search(plain))
    func = bool(FUNC.search(plain)) or bool(LITERAL.search(plain))
    lines = _line_hit(plain, def_line)
    located = has_file and (func or bool(lines))
    why = []
    if not has_file:
        why.append(f"{SOURCE} not named")
    if not (func or lines):
        why.append(f"neither {FUNCTION} nor a line reference within ±{LINE_TOLERANCE} of line {def_line}")
    out.append(result(f"points to {FUNCTION} in {SOURCE} (line {def_line})", located,
                      "; ".join(why) if why else f"function named: {func}; line refs: {lines}"))
    return out
