"""Checker for crash-from-log: empty invoices must total 0.00, nothing else may change,
and the model's new regression test must be red on the original code and green on the fix."""
import json
import re
import shutil
import subprocess
import sys
from decimal import Decimal, InvalidOperation
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged, with_trusted  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED = HERE / "trusted"
PROTECTED = ["tests/test_invoice.py", "logs/nightly.log"]
ORIGINAL_TEST_COUNT = 5
ORIGINAL_MODULE = {f"billing/{n}": TRUSTED / "billing" / n for n in ("__init__.py", "invoice.py", "report.py")}

# Each case builds one invoice; the probe prints subtotal/discount/tax/total (or the error).
PROBE = r'''
import json
from decimal import Decimal as D
from billing import Invoice, LineItem

def L(price, qty=1, taxable=True):
    return LineItem("SKU", "item", D(price), qty, taxable)

def build(items, **kw):
    inv = Invoice("INV-T", "Probe Customer", **kw)
    for it in items:
        inv.add(it)
    return inv

CASES = {
    "empty-defaults": lambda: Invoice("INV-E1", "Harbor Supply Co"),
    "empty-taxed": lambda: Invoice("INV-E2", "Harbor Supply Co", tax_rate=D("0.0825")),
    "empty-credit": lambda: Invoice("INV-E3", "Harbor Supply Co", items=[], tax_rate=D("0.0825"), credit=D("15.00")),
    "empty-after-clear": lambda: (lambda inv: (inv.items.clear(), inv)[1])(build([L("12.00")], tax_rate=D("0.07"), credit=D("2"))),
    "taxed": lambda: build([L("19.99", 3), L("40.00", 1, False)], tax_rate=D("0.08")),
    "taxed-credit": lambda: build([L("19.99", 3), L("40.00", 1, False)], tax_rate=D("0.08"), credit=D("20.00")),
    "credit-capped": lambda: build([L("5.00", 2)], tax_rate=D("0.0825"), credit=D("250")),
    "under-a-dollar": lambda: build([L("0.75")], tax_rate=D("0.08")),
    "under-a-dollar-credit": lambda: build([L("0.75")], tax_rate=D("0.08"), credit=D("0.25")),
    "half-cent": lambda: build([L("0.25")], tax_rate=D("0.10")),
    "odd-share": lambda: build([L("10.00"), L("7.50", 2), L("2.49", 4, False)], tax_rate=D("0.0825"), credit=D("9.99")),
    "sub-cent-prices": lambda: build([L("0.333", 3), L("1.005", 1)], tax_rate=D("0.0625")),
    "no-tax-no-credit": lambda: build([L("129.00", 2), L("3.10", 7)]),
    "untaxed-credit": lambda: build([L("60.00", 1, False)], tax_rate=D("0.09"), credit=D("12.34")),
}

out = {}
for name, make in CASES.items():
    row = {}
    try:
        inv = make()
        for attr in ("subtotal", "discount", "tax", "total"):
            try:
                v = getattr(inv, attr)()
                row[attr] = {"type": type(v).__name__, "value": str(v)}
            except Exception as e:
                row[attr] = {"error": type(e).__name__ + ": " + str(e)[:200]}
    except Exception as e:
        row["build"] = {"error": type(e).__name__ + ": " + str(e)[:200]}
    out[name] = row
print(json.dumps(out))
'''

EMPTY = ["empty-defaults", "empty-taxed", "empty-credit", "empty-after-clear"]


def _run(argv: list[str], cwd: Path, timeout: int = 120) -> subprocess.CompletedProcess:
    """sandboxed(), with a hang reported as a failed run instead of a checker crash."""
    try:
        return sandboxed(argv, cwd, timeout=timeout)
    except subprocess.TimeoutExpired:
        return subprocess.CompletedProcess(argv, 124, "", f"timed out after {timeout}s")


def run_probe(root: Path):
    # Same sandbox as checklib.python_probe, but keeps the whole stdout (the JSON is long).
    r = _run([PYTHON, "-I", "-c", "import sys; sys.path.insert(0, '.')\n" + PROBE], root, timeout=60)
    try:
        return json.loads(r.stdout.strip().splitlines()[-1]), r.stdout + r.stderr
    except Exception:
        return None, (r.stdout + r.stderr)[-3000:]


def num(cell):
    if not isinstance(cell, dict) or "value" not in cell or cell.get("type") not in ("int", "float", "Decimal"):
        return None
    try:
        return Decimal(cell["value"])
    except (InvalidOperation, ValueError):
        return None


def unittest_run(root: Path, pattern: str = "test*.py"):
    r = _run([PYTHON, "-m", "unittest", "discover", "-s", "tests", "-p", pattern, "-v"], root)
    text = r.stdout + r.stderr
    m = re.findall(r"^Ran (\d+) tests?", text, re.M)
    return r.returncode, int(m[-1]) if m else 0, text


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    temps = []
    try:
        w = with_trusted(final, {"tests/test_invoice.py": TRUSTED / "tests/test_invoice.py"})
        temps.append(w.parent)

        rc, ran, text = unittest_run(w, "test_invoice.py")
        out.append(result("original tests pass", rc == 0 and ran == ORIGINAL_TEST_COUNT, text[-1500:]))

        got, raw = run_probe(w)
        want, wraw = run_probe(TRUSTED)
        if got is None or want is None:
            out.append(result("empty invoice totals 0.00", False, "probe failed: " + (raw if got is None else wraw)[-1500:]))
            out.append(result("non-empty invoices unchanged", False, "probe failed"))
        else:
            bad = []
            for name in EMPTY:
                cell = got.get(name, {}).get("total")
                v = num(cell)
                if v is None or v != 0:
                    bad.append(f"{name}: {cell or got.get(name)}")
            out.append(result("empty invoice totals 0.00", not bad, "; ".join(bad)))
            diffs = []
            for name, row in want.items():
                if name in EMPTY:
                    continue
                for attr, cell in row.items():
                    a, b = num(got.get(name, {}).get(attr)), num(cell)
                    if a is None or a != b:
                        diffs.append(f"{name}.{attr}: expected {cell.get('value')}, got {got.get(name, {}).get(attr)}")
            out.append(result("non-empty invoices unchanged", not diffs, "; ".join(diffs)))

        new_tests = sorted(
            str(p.relative_to(final)) for p in (final / "tests").rglob("test*.py")
            if p.is_file() and "__pycache__" not in p.parts and str(p.relative_to(final)) not in original
        ) if (final / "tests").is_dir() else []
        out.append(result("regression test file added under tests/", bool(new_tests), ", ".join(new_tests) or "no new tests/test*.py file"))

        rc, ran, text = unittest_run(w)
        green = rc == 0 and ran > ORIGINAL_TEST_COUNT and "_FailedTest" not in text
        out.append(result("full suite passes including the new test", bool(new_tests) and green, f"ran {ran} tests, rc={rc}\n" + text[-1500:]))

        red_root = with_trusted(final, {"tests/test_invoice.py": TRUSTED / "tests/test_invoice.py", **ORIGINAL_MODULE})
        temps.append(red_root.parent)
        rc, ran, text = unittest_run(red_root)
        red = rc != 0 and ran > ORIGINAL_TEST_COUNT and "_FailedTest" not in text and re.search(r"^FAILED \((failures|errors)=", text, re.M)
        out.append(result("new test fails on the original (crashing) code", bool(new_tests) and bool(red), f"ran {ran} tests against original module, rc={rc}\n" + text[-1500:]))
    finally:
        for t in temps:
            shutil.rmtree(t, ignore_errors=True)
    return out
