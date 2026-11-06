"""Grade sqlite-question: the top 2025 spender (cancelled orders excluded) and the SQL used.

Truth comes from the trusted database copy, computed here with Decimal. The
candidate's query.sql is never trusted to be harmless: it runs only inside the
checklib sandbox, through a read-only SQLite connection, against private
copies of the trusted database and of a variant database (same schema and
traps, different correct answer). The variant run shows the query computes
the answer rather than restating it.
"""
from __future__ import annotations

from decimal import Decimal, InvalidOperation
import hashlib
import json
from pathlib import Path
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import unicodedata

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged  # noqa: E402

HERE = Path(__file__).resolve().parent
DB = "data/shop.db"
TRUSTED_DB = HERE / "trusted" / "data" / "shop.db"
VARIANT_DB = HERE / "trusted" / "variant" / "shop.db"
TOL = Decimal("0.01")
MAX_SQL_BYTES = 200_000

TRUTH_SQL = """
SELECT c.id, c.name, c.email, i.quantity, i.unit_price
FROM customers c
JOIN orders o ON o.customer_id = c.id
JOIN order_items i ON i.order_id = o.id
WHERE o.status <> 'cancelled' AND o.order_date >= '2025-01-01' AND o.order_date < '2026-01-01'
"""

# Runs inside the sandbox with cwd = a private directory holding only shop.db.
# Reads the candidate SQL from stdin, skips sqlite3-shell dot-commands, runs
# each statement on a read-only connection and prints every result set.
PROBE = r'''
import json, re, sqlite3, sys, time
sql = sys.stdin.read()
def blank(s):
    return not re.sub(r"--[^\n]*|/\*.*?\*/", "", s, flags=re.S).strip()
kept, pending = [], ""
for line in sql.splitlines(keepends=True):
    if blank(pending) and line.lstrip().startswith("."):
        continue
    kept.append(line)
    pending = "" if sqlite3.complete_statement(pending + line) else pending + line
text = "".join(kept)
stmts, start = [], 0
for i, ch in enumerate(text):
    if ch == ";" and sqlite3.complete_statement(text[start:i + 1]):
        stmts.append(text[start:i + 1])
        start = i + 1
if not blank(text[start:]):
    stmts.append(text[start:])
con = sqlite3.connect("file:shop.db?mode=ro", uri=True)
deadline = time.monotonic() + 30
con.set_progress_handler(lambda: 1 if time.monotonic() > deadline else 0, 10000)
out = []
for st in stmts:
    if blank(st):
        continue
    try:
        cur = con.execute(st)
        if cur.description is None:
            continue
        rows = cur.fetchmany(500)
        out.append({"columns": [d[0] for d in cur.description],
                    "rows": [[v if v is None or isinstance(v, (int, float, str)) else repr(v) for v in r] for r in rows]})
    except Exception as e:
        out.append({"error": type(e).__name__ + ": " + str(e)[:300], "sql": st.strip()[:300]})
print(json.dumps({"statements": len(stmts), "results": out}))
'''

NUM = re.compile(r"\d[\d,]*(?:\.\d+)?")


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def truth(db: Path) -> dict:
    """Top 2025 spender in a trusted database (checker process; never candidate data)."""
    con = sqlite3.connect(db.resolve().as_uri() + "?mode=ro&immutable=1", uri=True)
    try:
        totals: dict[int, Decimal] = {}
        who: dict[int, tuple[str, str]] = {}
        for cid, name, email, qty, price in con.execute(TRUTH_SQL):
            totals[cid] = totals.get(cid, Decimal(0)) + int(qty) * Decimal(str(price))
            who[cid] = (name, email)
    finally:
        con.close()
    ranked = sorted(totals.items(), key=lambda kv: kv[1], reverse=True)
    (top_id, amount), (_, second) = ranked[0], ranked[1]
    if amount - second < 1:
        raise RuntimeError(f"fixture has no clear winner in {db}")
    return {"id": top_id, "name": who[top_id][0], "email": who[top_id][1], "amount": amount}


def clean(text: str) -> str:
    text = unicodedata.normalize("NFKC", text or "").replace(" ", " ")
    return re.sub(r"[*_`~\\]", "", text)


def mentions_name(text: str, name: str) -> bool:
    first, last = name.split(" ", 1)
    pat = rf"(?<![A-Za-z]){re.escape(first)}\s+{re.escape(last)}(?![A-Za-z])|(?<![A-Za-z]){re.escape(last)},\s*{re.escape(first)}(?![A-Za-z])"
    return re.search(pat, clean(text), re.IGNORECASE) is not None


def money_values(text: str) -> list[Decimal]:
    vals = []
    for m in NUM.finditer(clean(text)):
        txt = m.group(0).rstrip(",").replace(",", "")
        try:
            vals.append(Decimal(txt))
        except InvalidOperation:
            continue
    return vals


def has_amount(text: str, amount: Decimal) -> bool:
    return any(abs(v - amount) <= TOL for v in money_values(text))


def cell_amount(cell, amount: Decimal) -> bool:
    if isinstance(cell, bool) or cell is None:
        return False
    if isinstance(cell, (int, float)):
        try:
            return abs(Decimal(str(cell)) - amount) <= TOL
        except InvalidOperation:
            return False
    return has_amount(str(cell), amount)


def cell_identity(cell, want: dict) -> bool:
    if isinstance(cell, bool) or cell is None:
        return False
    if isinstance(cell, int):
        return cell == want["id"]
    if isinstance(cell, str):
        s = cell.strip()
        return s == str(want["id"]) or s.lower() == want["email"].lower() or mentions_name(s, want["name"])
    return False


def run_sql(sql: str, db: Path):
    """Run candidate SQL in the sandbox against a private copy of a trusted db."""
    with tempfile.TemporaryDirectory(prefix="gemma-pilot-sql-") as tmp:
        work = Path(tmp).resolve()
        shutil.copyfile(db, work / "shop.db")
        try:
            r = sandboxed([PYTHON, "-I", "-c", PROBE], work, timeout=60, stdin=sql)
        except subprocess.TimeoutExpired:
            return None, "query timed out after 60s"
        if sha(work / "shop.db") != sha(db):
            return None, "database copy changed while the query ran"
    try:
        return json.loads(r.stdout.strip().splitlines()[-1]), (r.stdout + r.stderr)[-1500:]
    except Exception:
        return None, f"probe failed (rc={r.returncode}): " + (r.stdout + r.stderr)[-1500:]


def grade_sql(sql: str, db: Path, want: dict) -> tuple[bool, str]:
    data, raw = run_sql(sql, db)
    if data is None:
        return False, raw
    sets = data.get("results") or []
    for rs in sets:
        for row in rs.get("rows") or []:
            if any(cell_identity(c, want) for c in row) and any(cell_amount(c, want["amount"]) for c in row):
                return True, f"row {row} matches {want['name']} {want['amount']}"
    shown = []
    for rs in sets:
        if "error" in rs:
            shown.append(f"error: {rs['error']} in {rs['sql'][:120]!r}")
        else:
            shown.append(f"{rs['columns']}: {rs['rows'][:5]}")
    return False, (f"{data.get('statements')} statement(s); no result row has {want['name']} (id {want['id']}) "
                   f"with {want['amount']}; results: " + " | ".join(shown))[:3000]


def find_query(final: Path) -> Path | None:
    p = final / "query.sql"
    if p.is_file():
        return p
    return next((q for q in sorted(final.iterdir()) if q.is_file() and q.name.lower() == "query.sql"), None)


def check(final: Path, original: dict, record: dict) -> list[dict]:
    if original.get(DB) != sha(TRUSTED_DB):
        raise RuntimeError("workspace fixture does not match trusted/data/shop.db; rerun trusted/make_shop_db.py")
    want = truth(TRUSTED_DB)
    variant = truth(VARIANT_DB)
    if variant["id"] == want["id"]:
        raise RuntimeError("variant database must have a different top customer")

    out = [unchanged(final, original, [DB])]

    answer = record.get("finalAnswer") or ""
    out.append(result(f"answer names the top customer ({want['name']})", mentions_name(answer, want["name"]),
                      f"answer: {clean(answer)[:400]!r}"))
    out.append(result(f"answer gives the amount ({want['amount']})", has_amount(answer, want["amount"]),
                      f"numbers in answer: {[str(v) for v in money_values(answer)][:30]}"))

    q = find_query(final)
    if q is None:
        out.append(result("query.sql exists", False, "no query.sql at the workspace root"))
        out.append(result("query.sql returns the top customer and amount", False, "no query.sql"))
        out.append(result("query.sql computes the answer on different data", False, "no query.sql"))
        return out
    out.append(result("query.sql exists", True))
    raw = q.read_bytes()[:MAX_SQL_BYTES]
    sql = raw.decode("utf-8", errors="replace").lstrip("﻿")
    ok, detail = grade_sql(sql, TRUSTED_DB, want)
    out.append(result("query.sql returns the top customer and amount", ok, detail))
    ok, detail = grade_sql(sql, VARIANT_DB, variant)
    out.append(result("query.sql computes the answer on different data", ok, detail))
    return out
