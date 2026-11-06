"""Checker for dedupe-contacts: contacts_clean.csv deduplicated by email under the stated rule.

Truth is computed from the trusted copy of contacts.csv: two rows are the same
person when their emails match after trimming and ignoring case; the first
occurrence is kept, and its blank fields are filled from later duplicates.
Values are compared after trimming, whitespace collapsing and case folding
(phones by their digits), so re-formatting is tolerated; row order and CSV
quoting/line endings are free. The final answer must state the number of
duplicate rows removed. No candidate code is executed.
"""
import csv
import io
import re
import sys
import unicodedata
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result, unchanged  # noqa: E402

HERE = Path(__file__).resolve().parent
INPUT = "contacts.csv"
OUTPUT = "contacts_clean.csv"


def _nfc(v: str) -> str:
    return unicodedata.normalize("NFC", v or "")


def key(email: str) -> str:
    return _nfc(email).strip().casefold()


def norm(field: str, value: str) -> str:
    v = _nfc(value)
    if field == "phone":
        d = re.sub(r"\D", "", v)
        return d[1:] if len(d) == 11 and d.startswith("1") else d
    return " ".join(v.split()).casefold()


def parse(text: str):
    """(header, rows as dicts, problems). Blank lines are ignored; trailing empty cells
    (a trailing comma on every line, header included) are tolerated."""
    try:
        rows = [r for r in csv.reader(io.StringIO(text)) if any(c.strip() for c in r)]
    except csv.Error as e:
        return [], [], [f"not parseable as CSV: {e}"]
    if not rows:
        return [], [], ["file has no rows"]
    header = [c.strip() for c in rows[0]]
    while header and not header[-1]:
        header.pop()
    out, problems = [], []
    for n, r in enumerate(rows[1:], start=2):
        while len(r) > len(header) and not r[-1].strip():
            r = r[:-1]
        if len(r) != len(header):
            problems.append(f"record {n} has {len(r)} fields, header has {len(header)}: {r!r}"[:200])
            continue
        out.append(dict(zip(header, r)))
    return header, out, problems


def truth():
    header, rows, problems = parse((HERE / "trusted" / INPUT).read_text(encoding="utf-8"))
    if problems or len(rows) != 42:
        raise RuntimeError(f"trusted fixture drift: {len(rows)} rows, {problems}")
    merged, filled = {}, {}
    for r in rows:
        k = key(r["email"])
        if k not in merged:
            merged[k], filled[k] = dict(r), set()
            continue
        kept = merged[k]
        for f in header:
            if not kept[f].strip() and r[f].strip():
                kept[f] = r[f]
                filled[k].add(f)
    return header, merged, filled, len(rows)


# ---- final answer: the number of duplicate rows removed ----

WORDS = {"two": 2, "three": 3, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
         "twenty-nine": 29, "twenty nine": 29, "forty-two": 42, "forty two": 42}
KEYWORD = re.compile(r"duplicat|\bdupes?\b|remov|drop|merg|eliminat|collaps|de-?dup|consolidat|delet|discard|redundant|repeat|combin|fold", re.I)
NUM = re.compile(r"(?<![\w.\-/@])(\d+)(?![\w\-/@]|[.,]\d)")
WORD = re.compile(r"\b(" + "|".join(sorted(map(re.escape, WORDS), key=len, reverse=True)) + r")\b", re.I)
# "row 13" / "line 13" / "#13" name a position in the file, not a count.
POSITION = re.compile(r"(?:\brows?|\blines?|\brecords?|#|\bno\.?|\bid)\s*$", re.I)


def _numbers(sentence: str) -> set[int]:
    found = set()
    for m in NUM.finditer(sentence):
        if not POSITION.search(sentence[max(0, m.start() - 12):m.start()]):
            found.add(int(m.group(1)))
    for m in WORD.finditer(sentence):
        if not POSITION.search(sentence[max(0, m.start() - 12):m.start()]):
            found.add(WORDS[m.group(1).lower()])
    return found


def answer_check(answer: str, removed: int, total: int, unique: int) -> dict:
    text = re.sub(r"[*_`~|]", " ", (answer or "").replace("\\_", "_"))
    sentences = [s for line in text.splitlines() for s in re.split(r"(?<=[.!?;])\s+", line) if s.strip()]
    kw = set().union(*([_numbers(s) for s in sentences if KEYWORD.search(s)] or [set()]))
    every = set().union(*([_numbers(s) for s in sentences] or [set()]))
    ok = removed in kw or {total, unique} <= every
    return result(f"answer states {removed} duplicates removed", ok,
                  f"numbers near removal/duplicate wording: {sorted(kw)}; all numbers: {sorted(every)}; "
                  f"expected {removed} removed ({total} -> {unique}); answer: {(answer or '')[:600]!r}")


# ---- output file ----

def find_output(final: Path):
    p = final / OUTPUT
    if p.is_file():
        return p
    return next((q for q in sorted(final.iterdir()) if q.is_file() and q.name.lower() == OUTPUT), None)


def check(final: Path, original: dict, record: dict) -> list[dict]:
    header, want, filled, total = truth()
    removed = total - len(want)
    out = [unchanged(final, original, [INPUT])]
    names = ["contacts_clean.csv has the original columns",
             f"one row per unique email ({len(want)} rows)",
             "first occurrence's values kept",
             "blank fields filled from later duplicates"]

    path = find_output(final)
    text = None
    if path is None:
        why = f"no {OUTPUT} at the workspace root"
    else:
        try:
            text = path.read_bytes().decode("utf-8-sig")
        except UnicodeDecodeError as e:
            why = f"{OUTPUT} is not UTF-8: {e}"
    if text is None:
        out += [result(n, False, why) for n in names]
        out.append(answer_check(record.get("finalAnswer") or "", removed, total, len(want)))
        return out

    got_header, rows, problems = parse(text)
    header_ok = got_header == header
    out.append(result(names[0], header_ok and not problems,
                      f"header {got_header}, expected {header}" + ("; " + "; ".join(problems[:5]) if problems else "")))

    # Map by the candidate's own column names (case-insensitive) so a reordered or
    # renamed header still yields useful diagnostics; the header check above fails it.
    cols = {h.strip().casefold(): h for h in got_header}

    def val(row, f):
        h = cols.get(f.casefold())
        return row.get(h, "") if h is not None else ""

    seen, dupes, extra = {}, [], []
    for r in rows:
        k = key(val(r, "email"))
        if k in seen:
            dupes.append(k)
        elif k in want:
            seen[k] = r
        else:
            extra.append(k)
    missing = [k for k in want if k not in seen]
    detail = f"{len(rows)} data rows, expected {len(want)}"
    for label, items in (("still duplicated", dupes), ("missing", missing), ("unexpected", extra)):
        if items:
            detail += f"; {label}: {', '.join(sorted(set(items)))}"
    out.append(result(names[1], not (dupes or missing or extra) and len(rows) == len(want), detail))

    kept_bad, fill_bad = [], []
    for k, exp in want.items():
        row = seen.get(k)
        if row is None:
            continue
        for f in header:
            if f == "email":
                continue
            if norm(f, val(row, f)) != norm(f, exp[f]):
                msg = f"{k} {f}: got {val(row, f)!r}, expected {exp[f]!r}"
                (fill_bad if f in filled[k] else kept_bad).append(msg)
    nothing = "no rows to compare" if not seen else ""
    out.append(result(names[2], bool(seen) and not kept_bad, nothing or "; ".join(kept_bad)))
    out.append(result(names[3], bool(seen) and not fill_bad and all(k in seen for k in want if filled[k]),
                      nothing or "; ".join(fill_bad) or ", ".join(k for k in want if filled[k] and k not in seen)))
    out.append(answer_check(record.get("finalAnswer") or "", removed, total, len(want)))
    return out
