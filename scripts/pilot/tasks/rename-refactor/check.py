"""Checker for rename-refactor: parcels.rates.calc must become shipping_cost everywhere,
with no alias left behind and no change in behavior.

Static checks read the candidate's files (nothing executed). Behavior checks run
trusted probe code against the candidate package and against the trusted
original package in the read-only sandbox, and compare the observations. The
original test suite, with the rename applied by us (trusted/tests), must pass
against the candidate package.
"""
import ast
import io
import json
import re
import shutil
import subprocess
import sys
import tokenize
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, with_trusted  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED_TESTS = HERE / "trusted" / "tests"
ORIGINAL_ROOT = HERE / "trusted" / "original"
OLD, NEW = "calc", "shipping_cost"
WORD = re.compile(rf"(?<![A-Za-z0-9_]){OLD}(?![A-Za-z0-9_])")
# A string naming the old function by dotted path, e.g. mock.patch("parcels.orders.calc").
DOTTED = re.compile(rf"\bparcels(?:\.\w+)*\.{OLD}\b")
IGNORED_PARTS = {"__pycache__", ".pytest_cache", ".git"}
TEST_MODULES = ["tests.test_rates", "tests.test_orders", "tests.test_quotes", "tests.test_cli"]
ORIGINAL_TEST_COUNT = 21
# Original test methods per file, and which files referenced the function.
ORIGINAL_TESTS = {"tests/test_rates.py": 8, "tests/test_orders.py": 5, "tests/test_quotes.py": 5, "tests/test_cli.py": 3}
REFERENCED = ["tests/test_rates.py", "tests/test_orders.py", "tests/test_quotes.py"]

PROBE = r'''
import contextlib, functools, inspect, io, json, sys
from decimal import Decimal as D
FN = sys.argv[1]
import parcels
from parcels import cli, orders, quotes, rates

def cell(v):
    if isinstance(v, (tuple, list)):
        return {"seq": [cell(x) for x in v]}
    if isinstance(v, dict):
        return {"map": [[str(k), cell(x)] for k, x in v.items()]}
    return {"type": type(v).__name__, "value": str(v)}

def call(f, *a, **k):
    try:
        return cell(f(*a, **k))
    except Exception as e:
        return {"error": type(e).__name__}

def attr(mod, name):
    # A missing function is reported per call (AttributeError) instead of aborting the probe.
    found = getattr(mod, name, None)
    if found is not None:
        return found
    def missing(*a, **k):
        raise AttributeError(f"{mod.__name__}.{name} is missing")
    return missing

out = {}
fn = attr(rates, FN)
try:
    sig = inspect.signature(fn)
    out["signature"] = [[p.name, str(p.kind), repr(p.default)] for p in sig.parameters.values()]
except (TypeError, ValueError) as e:
    out["signature"] = "unavailable: " + type(e).__name__

ARGS = [
    ((0.5, 1), {}), ((0.01, 1), {}), ((0.51, 1), {}), ((0.6, 3), {}), ((1, 2), {}), ((1.23, 2), {}),
    ((1.23, 2), {"express": True}), ((2.45, 3), {}), ((2.45, 3), {"express": True}), ((9.999, 4), {}),
    ((10, 4), {"express": 1}), ((29.95, 1), {}), ((30, 4), {}), ((30, 4), {"express": True}),
    (("2.5", 2), {}), ((D("0.55"), 3), {}), ((D("0.55"), 3), {"express": True}), ((7, 1), {"express": False}),
    ((), {"weight_kg": 3.3, "zone": 2}), ((), {"weight_kg": 3.3, "zone": 2, "express": True}),
    ((0.7, 3, True), {}), ((4.04, 1, True), {}),
    ((0, 1), {}), ((-1, 2), {}), ((30.01, 3), {}), ((1, 0), {}), ((1, 5), {}), ((1, "1"), {}), (("heavy", 1), {}),
]
out["fn"] = [call(fn, *a, **k) for a, k in ARGS]

out["zone_for"] = [call(attr(rates, "zone_for"), z) for z in ("02134", "29401", "33101", "48201", "50309", "73301", "80202", "99501", " 60601", "", "A1B")]
out["tax"] = [call(attr(orders, "calculate_tax"), a, s) for a, s in (("99.99", "CA"), ("10", "tx"), (12.5, "NY"), ("50", "OR"), ("50", "WA"), ("0.07", "CA"))]
ORDERS = [
    {"goods": "120.00", "state": "CA", "postcode": "94103", "parcels": [1.2, 3.75]},
    {"goods": "35.50", "state": "tx", "postcode": "73301", "parcels": [0.6, 0.6, 2.45], "express": True},
    {"goods": 19.99, "state": "NY", "postcode": "10001", "parcels": [29.95]},
    {"goods": "5", "state": "WA", "postcode": "33101", "parcels": []},
    {"goods": "5", "state": "CA", "postcode": "94103", "parcels": [31]},
]
out["shipping_for"] = [call(attr(orders, "shipping_for"), o) for o in ORDERS]
out["order_total"] = [call(attr(orders, "order_total"), o) for o in ORDERS]
out["quote"] = [call(attr(quotes, "quote"), w, z, s) for w, z, s in ((2, 3, "express"), (0.6, 3, "standard"), (12.34, 4, "express"), (1, 2, "overnight"), (0, 1, "standard"))]
out["quote_table"] = [call(attr(quotes, "quote_table"), *a) for a in ((2,), (0.6, "express"), (17.77,), (40,))]
out["split"] = [call(attr(quotes, "split_shipment"), *a, **k) for a, k in (((25, 2), {}), ((7, 1), {"express": True}), ((61, 3), {}), ((19.5, 4), {"max_parcel_kg": 6}), ((95, 2), {"max_parcel_kg": 30, "express": True}))]

def run_cli(*argv):
    so, se = io.StringIO(), io.StringIO()
    with contextlib.redirect_stderr(se):
        try:
            code = attr(cli, "main")(list(argv), out=so)
        except SystemExit as e:
            code = e.code
        except Exception as e:
            code = "raised " + type(e).__name__
    return {"code": code, "out": so.getvalue(), "stderr": bool(se.getvalue().strip())}

out["cli"] = [run_cli(*a) for a in (("2.4", "94103"), ("0.6", "73301", "--express"), ("1", "--table", "--express"), ("12.5", "--table"), ("45", "10001"), ("1", "XYZ"), ("1",))]

ns = {}
try:
    exec("from parcels import *", ns)
    out["star"] = sorted(k for k in ns if not k.startswith("__"))
except Exception as e:
    out["star"] = "error: " + type(e).__name__
out["all"] = sorted(getattr(parcels, "__all__", []))
services = getattr(quotes, "SERVICES", None) or {}
std, exp = services.get("standard"), services.get("express")
out["wiring"] = {
    "package": getattr(parcels, FN, None) is fn,
    "orders": getattr(orders, FN, None) is fn,
    "standard": std is fn,
    "express": isinstance(exp, functools.partial) and exp.func is fn and exp.args == () and exp.keywords == {"express": True},
}
out["old_attrs"] = sorted(m.__name__ for m in (parcels, rates, orders, quotes, cli) if hasattr(m, "calc"))
print(json.dumps(out))
'''


def _run(argv, cwd: Path, timeout: int = 120) -> subprocess.CompletedProcess:
    try:
        return sandboxed(argv, cwd, timeout=timeout)
    except subprocess.TimeoutExpired:
        return subprocess.CompletedProcess(argv, 124, "", f"timed out after {timeout}s")


def probe(root: Path, fn_name: str):
    r = _run([PYTHON, "-I", "-c", "import sys; sys.path.insert(0, '.')\n" + PROBE, fn_name], root, timeout=60)
    try:
        return json.loads(r.stdout.strip().splitlines()[-1]), ""
    except Exception:
        return None, (r.stdout + r.stderr)[-2000:]


def unittest_run(root: Path, args: list[str]):
    r = _run([PYTHON, "-m", "unittest", *args, "-v"], root)
    text = r.stdout + r.stderr
    m = re.findall(r"^Ran (\d+) tests?", text, re.M)
    return r.returncode, int(m[-1]) if m else 0, text


def files(final: Path, top: str):
    base = final / top
    if not base.is_dir():
        return []
    return sorted(p for p in base.rglob("*.py") if p.is_file() and not (set(p.relative_to(final).parts) & IGNORED_PARTS))


def tokens(text: str):
    try:
        return list(tokenize.generate_tokens(io.StringIO(text).readline))
    except (tokenize.TokenError, SyntaxError, IndentationError):
        return None


def old_name_hits(final: Path) -> list[str]:
    """Package files: the old name may not appear at all (code, strings, docstrings, comments).
    Test files: no code reference and no dotted-path string such as a mock.patch target; other
    strings and comments are allowed (e.g. a test asserting hasattr(rates, "calc") is False).
    Files outside parcels/ and tests/ (notes, helper scripts) are not graded."""
    hits = []
    for p in files(final, "parcels"):
        text = p.read_text(errors="replace")
        for n, line in enumerate(text.splitlines(), 1):
            if WORD.search(line):
                hits.append(f"{p.relative_to(final)}:{n}: {line.strip()[:120]}")
    for p in files(final, "tests"):
        text = p.read_text(errors="replace")
        toks = tokens(text)
        if toks is None:
            hits += [f"{p.relative_to(final)}:{n}: {line.strip()[:120]}" for n, line in enumerate(text.splitlines(), 1) if WORD.search(line)]
            continue
        for t in toks:
            if (t.type == tokenize.NAME and t.string == OLD) or (t.type == tokenize.STRING and DOTTED.search(t.string)):
                hits.append(f"{p.relative_to(final)}:{t.start[0]}: {t.line.strip()[:120]}")
    return hits


def definitions(final: Path):
    """Every def named NEW or OLD, and module-level rebinding of either name, across the package and tests."""
    defs, binds, broken = [], [], []
    for top in ("parcels", "tests"):
        for p in files(final, top):
            rel = str(p.relative_to(final))
            try:
                tree = ast.parse(p.read_text(errors="replace"))
            except SyntaxError as e:
                broken.append(f"{rel}: {e.msg} (line {e.lineno})")
                continue
            top_level = set(map(id, tree.body))
            for node in ast.walk(tree):
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name in (NEW, OLD):
                    defs.append((rel, node.name, id(node) in top_level))
                elif isinstance(node, (ast.Assign, ast.AnnAssign)) and rel.startswith("parcels/"):
                    targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                    for t in targets:
                        if isinstance(t, ast.Name) and t.id in (NEW, OLD):
                            binds.append(f"{rel}: {t.id} = ...")
    return defs, binds, broken


def test_methods(path: Path):
    try:
        tree = ast.parse(path.read_text(errors="replace"))
    except SyntaxError:
        return None
    return sum(1 for n in ast.walk(tree) if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name.startswith("test"))


def mentions_new(path: Path) -> bool:
    toks = tokens(path.read_text(errors="replace"))
    return bool(toks) and any(t.type == tokenize.NAME and t.string == NEW for t in toks)


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = []

    hits = old_name_hits(final)
    out.append(result(f"old name '{OLD}' gone from the package and tests", not hits, "; ".join(hits[:15])))

    defs, binds, broken = definitions(final)
    good = defs == [("parcels/rates.py", NEW, True)] and not binds and not broken
    detail = f"defs: {defs}; rebinds: {binds}; syntax errors: {broken}"
    out.append(result(f"{NEW} defined exactly once, in parcels/rates.py, with no alias", good, detail))

    temps = []
    try:
        w = with_trusted(final, {f"tests/{p.name}": p for p in TRUSTED_TESTS.glob("*.py")})
        temps.append(w.parent)
        rc, ran, text = unittest_run(w, TEST_MODULES)
        out.append(result("original tests (renamed by the checker) pass", rc == 0 and ran == ORIGINAL_TEST_COUNT, f"ran {ran}, rc={rc}\n" + text[-1500:]))

        mine = with_trusted(final, {})
        temps.append(mine.parent)
        rc, ran, text = unittest_run(mine, ["discover", "-s", "tests"])
        ok = rc == 0 and ran >= ORIGINAL_TEST_COUNT and "_FailedTest" not in text
        out.append(result("the workspace's own test suite passes", ok, f"ran {ran}, rc={rc}\n" + text[-1500:]))

        problems = []
        for rel, n in ORIGINAL_TESTS.items():
            p = final / rel
            if not p.is_file():
                problems.append(f"{rel} missing")
                continue
            count = test_methods(p)
            if count is None or count < n:
                problems.append(f"{rel}: {count} test methods, originally {n}")
            if rel in REFERENCED and not mentions_new(p):
                problems.append(f"{rel} does not use {NEW}")
        out.append(result("existing tests updated, none removed", not problems, "; ".join(problems)))

        got, raw = probe(mine, NEW)
        want, wraw = probe(ORIGINAL_ROOT, OLD)
        if want is None:
            raise RuntimeError("probe failed on the trusted original package: " + wraw)
        if got is None:
            for name in (f"{NEW} behaves exactly like the original {OLD}", "rest of the package behaves the same", f"no runtime alias for {OLD}"):
                out.append(result(name, False, "probe failed: " + raw))
        else:
            fn_diffs = [f"case {i}: expected {a}, got {b}" for i, (a, b) in enumerate(zip(want["fn"], got["fn"])) if a != b]
            if got["signature"] != want["signature"]:
                fn_diffs.insert(0, f"signature: expected {want['signature']}, got {got['signature']}")
            out.append(result(f"{NEW} behaves exactly like the original {OLD}", not fn_diffs, "; ".join(fn_diffs[:10])))

            rename = lambda names: sorted(NEW if n == OLD else n for n in names) if isinstance(names, list) else names  # noqa: E731
            diffs = []
            for key in ("zone_for", "tax", "shipping_for", "order_total", "quote", "quote_table", "split", "cli"):
                for i, (a, b) in enumerate(zip(want[key], got[key])):
                    if a != b:
                        diffs.append(f"{key}[{i}]: expected {a}, got {b}")
            for key in ("star", "all"):
                if rename(want[key]) != got[key]:
                    diffs.append(f"{key}: expected {rename(want[key])}, got {got[key]}")
            unwired = [k for k, v in got["wiring"].items() if not v]
            if unwired:
                diffs.append(f"{NEW} not wired the same way in: {', '.join(unwired)}")
            out.append(result("rest of the package behaves the same", not diffs, "; ".join(diffs[:10])))
            out.append(result(f"no runtime alias for {OLD}", not got["old_attrs"], f"modules still exposing {OLD}: {got['old_attrs']}"))
    finally:
        for t in temps:
            shutil.rmtree(t, ignore_errors=True)
    return out
