"""Grade report.md: per-region Q3 2026 revenue table, sorted, with a summary naming the top region.

Truth comes from the trusted CSV copy. The table is located by content, not by
header wording: some pipe table must list every region exactly once, and one of
its columns must hold the correct revenue for every region (to the displayed
precision), in descending row order. No candidate code is executed.
"""
import csv
import re
import sys
from decimal import Decimal
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result, unchanged

HERE = Path(__file__).resolve().parent
START, END = "2026-07-01", "2026-09-30"

SEP = re.compile(r"^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$")
NUM = re.compile(r"-?\d[\d,]*(?:\.\d+)?")


def truth() -> dict[str, Decimal]:
    totals: dict[str, Decimal] = {}
    with open(HERE / "trusted" / "data" / "sales.csv", newline="") as f:
        for row in csv.DictReader(f):
            if START <= row["date"].strip() <= END:
                region = row["region"].strip()
                totals[region] = totals.get(region, Decimal(0)) + int(row["units"]) * Decimal(row["unit_price"])
    return totals


def split_row(line: str) -> list[str]:
    s = line.strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|"):
        s = s[:-1]
    return [c.strip() for c in s.split("|")]


def norm_label(cell: str) -> str:
    c = re.sub(r"[*_`~]", "", cell).strip().lower()
    c = re.sub(r"^#?\d+[.)]?\s+", "", c)  # leading rank such as "1." or "#1"
    c = re.sub(r"[^a-z ]", " ", c)
    c = re.sub(r"\s+", " ", c).strip()
    c = re.sub(r"\s+region$", "", c)
    return c


def parse_money(cell: str):
    """First number in the cell that is not a percentage, with its tolerance."""
    c = re.sub(r"[*_`~$€£]", " ", cell).replace("USD", " ")
    for m in NUM.finditer(c):
        rest = c[m.end():].lstrip()
        if rest.startswith("%"):
            continue
        txt = m.group(0).replace(",", "")
        try:
            val = Decimal(txt)
        except Exception:
            continue
        decimals = len(txt.split(".", 1)[1]) if "." in txt else 0
        tol = max(Decimal("0.01"), Decimal("0.5") * Decimal(10) ** -decimals)
        return val, tol
    return None


def tables(lines: list[str]) -> list[list[int]]:
    """Blocks of >= 2 consecutive lines containing a pipe (line indices)."""
    blocks, cur = [], []
    for i, line in enumerate(lines):
        if "|" in line:
            cur.append(i)
        else:
            if len(cur) >= 2:
                blocks.append(cur)
            cur = []
    if len(cur) >= 2:
        blocks.append(cur)
    return blocks


def evaluate(lines, blocks, want):
    regions = {r.lower(): r for r in want}
    best = None
    for block in blocks:
        rows = []
        for i in block:
            if SEP.match(lines[i]):
                continue
            cells = split_row(lines[i])
            reg = next((regions[norm_label(c)] for c in cells if norm_label(c) in regions), None)
            if reg:
                rows.append((reg, cells))
        if not rows:
            continue
        listed = [r for r, _ in rows]
        present = sorted(listed) == sorted(want)
        ncols = max(len(c) for _, c in rows)
        for j in range(ncols):
            vals = [(reg, parse_money(cells[j]) if j < len(cells) else None) for reg, cells in rows]
            correct = [reg for reg, v in vals if v is not None and abs(v[0] - want[reg]) <= v[1]]
            nums = [v[0] for _, v in vals if v is not None]
            ordered = len(nums) == len(vals) and all(a >= b for a, b in zip(nums, nums[1:]))
            all_ok = present and sorted(correct) == sorted(want)
            score = (present, all_ok, len(set(correct)), ordered)
            shown = ", ".join(f"{reg}={v[0] if v else None}" for reg, v in vals)
            if best is None or score > best[0]:
                best = (score, listed, shown, correct)
    return best


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, ["data/sales.csv"])]
    want = truth()
    top = max(want, key=want.get)
    expected = ", ".join(f"{r}={want[r]}" for r in sorted(want, key=want.get, reverse=True))

    path = final / "report.md"
    if not path.is_file():
        path = next((p for p in sorted(final.iterdir()) if p.is_file() and p.name.lower() == "report.md"), path)
    if not path.is_file():
        out.append(result("report.md exists", False, "no report.md at the workspace root"))
        for name in ("table lists every region once", "Q3 2026 revenue per region correct", "rows sorted highest to lowest", "summary names top region"):
            out.append(result(name, False, "no report.md"))
        return out
    out.append(result("report.md exists", True))

    lines = path.read_text(errors="replace").splitlines()
    blocks = tables(lines)
    best = evaluate(lines, blocks, want)
    if best is None:
        detail = "no pipe table with region rows found"
        out.append(result("table lists every region once", False, detail))
        out.append(result("Q3 2026 revenue per region correct", False, detail))
        out.append(result("rows sorted highest to lowest", False, detail))
    else:
        (present, all_ok, _, ordered), listed, shown, correct = best
        out.append(result("table lists every region once", present, f"listed {listed}; expected {sorted(want)}"))
        out.append(result("Q3 2026 revenue per region correct", present and all_ok, f"best column: {shown}; expected {expected}"))
        out.append(result("rows sorted highest to lowest", present and ordered, f"row order: {shown}"))

    in_table = {i for b in blocks for i in b}
    prose = "\n".join(re.sub(r"[*_`~]", "", l) for i, l in enumerate(lines) if i not in in_table)
    named = re.search(rf"\b{re.escape(top)}\b", prose, re.IGNORECASE) is not None
    out.append(result("summary names top region", named, f"top region is {top}; text outside the table: {prose.strip()[:600]!r}"))
    return out
