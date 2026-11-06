"""Checker for json-to-csv: data/users.json -> users.csv with fixed columns.

Truth comes from the trusted JSON copy, never the candidate's workspace. The
CSV is parsed with the csv module, so any standard quoting style (minimal,
all, non-numeric) and either line ending is accepted, as is a UTF-8 BOM.
Cells are compared after stripping surrounding whitespace; names are compared
after NFC normalization; tags may appear in any order and with spaces around
the ';' separators. No candidate code is executed.
"""
import csv
import io
import json
import re
import sys
import unicodedata
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result, unchanged  # noqa: E402

HERE = Path(__file__).resolve().parent
PROTECTED = ["data/users.json"]
HEADER = ["id", "name", "email", "city", "tags", "signup_date"]
DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def _text(v) -> str:
    return "" if v is None else str(v)


def truth() -> dict[int, dict]:
    users = json.loads((HERE / "trusted" / "data" / "users.json").read_text(encoding="utf-8"))
    out = {}
    for u in users:
        addr = u.get("address") or {}
        ts = u.get("signed_up_at")
        out[int(u["id"])] = {
            "name": u["name"],
            "email": _text(u.get("email")),
            "city": _text(addr.get("city")),
            "tags": list(u.get("tags") or []),
            "signup_date": ts[:10] if ts else "",
        }
    return out


def nfc(s: str) -> str:
    return unicodedata.normalize("NFC", s.strip())


def tag_list(cell: str) -> list[str]:
    cell = cell.strip()
    return [t.strip() for t in cell.split(";")] if cell else []


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    want = truth()
    names = [
        "users.csv exists and is UTF-8",
        "header is exactly id,name,email,city,tags,signup_date",
        "one row per user (all ids, 6 fields each)",
        "rows sorted by id ascending",
        "names preserved exactly (incl. non-ASCII)",
        "email and city correct, empty when missing",
        "tags joined with ';', empty when none",
        "signup_date is YYYY-MM-DD, empty when missing",
    ]

    path = final / "users.csv"
    if not path.is_file():
        path = next((p for p in sorted(final.iterdir()) if p.is_file() and p.name.lower() == "users.csv"), path)
    if not path.is_file():
        others = sorted(str(p.relative_to(final)) for p in final.rglob("*.csv"))
        detail = "no users.csv at the workspace root" + (f"; csv files found: {others}" if others else "")
        return out + [result(n, False, detail) for n in names]
    try:
        text = path.read_bytes().decode("utf-8-sig")
    except UnicodeDecodeError as e:
        return out + [result(n, False, f"users.csv is not valid UTF-8: {e}") for n in names]
    out.append(result(names[0], True))

    try:
        rows = [r for r in csv.reader(io.StringIO(text, newline="")) if any(c.strip() for c in r)]
    except csv.Error as e:
        return out + [result(n, False, f"csv parse error: {e}") for n in names[1:]]
    if not rows:
        return out + [result(n, False, "users.csv has no rows") for n in names[1:]]

    header = [c.strip() for c in rows[0]]
    out.append(result(names[1], header == HEADER, f"header: {rows[0]!r}"))
    body = rows[1:]

    # Shape: exactly one well-formed row per user, matched by an integer id cell.
    shape_bad, by_id, order = [], {}, []
    for n, r in enumerate(body, start=2):
        if len(r) != len(HEADER):
            shape_bad.append(f"row {n}: {len(r)} fields {r!r}"[:200])
            continue
        cell = r[0].strip()
        if not re.fullmatch(r"\d+", cell):
            shape_bad.append(f"row {n}: id {cell!r} is not an integer")
            continue
        i = int(cell)
        order.append(i)
        if i in by_id:
            shape_bad.append(f"row {n}: duplicate id {i}")
        by_id[i] = r
    missing = sorted(set(want) - set(by_id))
    extra = sorted(set(by_id) - set(want))
    if missing:
        shape_bad.append(f"missing ids {missing}")
    if extra:
        shape_bad.append(f"unknown ids {extra}")
    if len(body) != len(want):
        shape_bad.append(f"{len(body)} data rows, expected {len(want)}")
    out.append(result(names[2], not shape_bad, "; ".join(shape_bad)))

    ordered = len(order) == len(want) and order == sorted(order)
    out.append(result(names[3], ordered, f"id order: {order}"))

    def compare(field_idxs, same) -> list[str]:
        bad = []
        for i, w in sorted(want.items()):
            r = by_id.get(i)
            if r is None:
                bad.append(f"id {i}: no row")
                continue
            for name, idx in field_idxs:
                if not same(name, r[idx], w[name]):
                    bad.append(f"id {i} {name}: got {r[idx]!r}, expected {w[name] if name != 'tags' else ';'.join(w[name])!r}")
        return bad

    bad = compare([("name", 1)], lambda _n, got, exp: nfc(got) == nfc(exp))
    non_ascii = sum(1 for w in want.values() if not w["name"].isascii())
    out.append(result(names[4], not bad, f"{len(bad)} mismatches ({non_ascii} names are non-ASCII): " + "; ".join(bad)))

    bad = compare([("email", 2), ("city", 3)], lambda _n, got, exp: nfc(got) == nfc(exp))
    out.append(result(names[5], not bad, "; ".join(bad)))

    bad = compare([("tags", 4)], lambda _n, got, exp: sorted(tag_list(got)) == sorted(exp))
    out.append(result(names[6], not bad, "; ".join(bad)))

    bad = compare([("signup_date", 5)], lambda _n, got, exp: got.strip() == exp and (not exp or DATE.match(got.strip())))
    out.append(result(names[7], not bad, "; ".join(bad)))
    return out
