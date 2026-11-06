"""Grade candidate unit tests for slugify() by running them, not by reading them.

1. The tests must pass against the real (trusted) text_utils.py.
2. They must hold at least MIN_CASES passing cases (test methods; a method
   that uses subTest counts once per subtest).
3. They must still pass against a behaviour-identical refactor of slugify, so
   kills below come from behaviour, not from coupling to source text.
4. Mutation testing: each file in mutants/m*.py is a slugify with one
   realistic bug; the suite must fail for at least MIN_KILLS of them.
All candidate code runs through checklib.sandboxed (read-only, no network).
"""
import hashlib
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged, with_trusted

HERE = Path(__file__).resolve().parent
TEST_FILE = "test_text_utils.py"
TRUSTED = {"text_utils.py": HERE / "trusted/text_utils.py", "posts.py": HERE / "trusted/posts.py"}
MUTANTS = sorted((HERE / "mutants").glob("m[0-9]_*.py"))
EQUIVALENT = HERE / "mutants/equivalent_refactor.py"
MIN_CASES = 6
MIN_KILLS = 4
MARK = "@@WRITE-TESTS-COUNT@@"

COUNTER = r'''
import io, json, sys, unittest
sys.path.insert(0, ".")


class Counting(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.passed_ids, self.with_subtests, self.passed_subtests = [], set(), 0

    def addSuccess(self, test):
        super().addSuccess(test)
        self.passed_ids.append(test.id())

    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        self.with_subtests.add(test.id())
        if err is None:
            self.passed_subtests += 1


suite = unittest.defaultTestLoader.loadTestsFromName("test_text_utils")
res = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0, resultclass=Counting).run(suite)
cases = sum(1 for t in res.passed_ids if t not in res.with_subtests) + res.passed_subtests
print("@@WRITE-TESTS-COUNT@@" + json.dumps({"testsRun": res.testsRun, "subtests": res.passed_subtests, "skipped": len(res.skipped), "cases": cases, "ok": res.wasSuccessful()}))
'''


def _sha(p: Path) -> str:
    return hashlib.sha256(p.read_bytes()).hexdigest()


def _run_suite(w: Path, verbose: bool = False, timeout: int = 30):
    """The user's own command; None means it timed out (counts as a kill for a mutant)."""
    argv = [PYTHON, "-m", "unittest"] + (["-v"] if verbose else []) + ["test_text_utils"]
    try:
        return sandboxed(argv, w, timeout=timeout)
    except subprocess.TimeoutExpired:
        return None


def _with(final: Path, overrides: dict) -> Path:
    return with_trusted(final, {**TRUSTED, **overrides})


def _cleanup(w: Path):
    shutil.rmtree(w.parent, ignore_errors=True)


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, ["text_utils.py"])]

    other_code = sorted(r for r in original if r.endswith(".py") and r != "text_utils.py")
    touched = [r for r in other_code if not (final / r).is_file() or _sha(final / r) != original[r]]
    out.append(result("other source files unchanged", not touched, "changed: " + ", ".join(touched) if touched else ""))

    test = final / TEST_FILE
    out.append(result("test_text_utils.py written", test.is_file(), "" if test.is_file() else "missing " + TEST_FILE))
    if not test.is_file():
        return out

    real_ok = False
    w = _with(final, {})
    try:
        r = _run_suite(w, verbose=True, timeout=90)
        ran = re.findall(r"^Ran (\d+) tests? in", r.stderr, re.M) if r else []
        ok = r is not None and r.returncode == 0 and bool(ran) and int(ran[-1]) > 0
        detail = "timed out" if r is None else f"exit {r.returncode}; " + r.stderr[-1500:]
        out.append(result("tests pass against the real slugify", ok, detail))
        real_ok = ok

        try:
            c = sandboxed([PYTHON, "-I", "-c", COUNTER], w, timeout=90)
            line = next((l for l in reversed(c.stdout.splitlines()) if l.startswith(MARK)), None)
            counts = json.loads(line[len(MARK):]) if line else None
            cdetail = c.stderr[-800:] if counts is None else ""
        except subprocess.TimeoutExpired:
            counts, cdetail = None, "timed out"
        cases = counts["cases"] if counts else 0
        if counts:
            cdetail = f"{cases} passing cases ({counts['testsRun']} tests run, {counts['subtests']} passing subtests, {counts['skipped']} skipped)"
        out.append(result(f"at least {MIN_CASES} passing test cases", cases >= MIN_CASES, cdetail))
    finally:
        _cleanup(w)

    ew = _with(final, {"text_utils.py": EQUIVALENT})
    try:
        er = _run_suite(ew)
    finally:
        _cleanup(ew)
    eok = er is not None and er.returncode == 0
    out.append(result("tests accept a behaviour-identical refactor", eok, "" if eok else ("timed out" if er is None else er.stderr[-1200:])))

    killed, survived = [], []
    for m in MUTANTS:
        mw = _with(final, {"text_utils.py": m})
        try:
            mr = _run_suite(mw)
        finally:
            _cleanup(mw)
        (killed if mr is None or mr.returncode != 0 else survived).append(m.stem)
    # A kill only means something when the suite passes on the real implementation.
    note = "" if real_ok else " (not counted: the suite does not pass against the real slugify)"
    out.append(result(
        f"mutation testing: kills at least {MIN_KILLS} of {len(MUTANTS)} buggy slugify variants",
        real_ok and len(MUTANTS) == 6 and len(killed) >= MIN_KILLS,
        f"killed {len(killed)}/{len(MUTANTS)}{note}; killed: {', '.join(killed) or '-'}; survived: {', '.join(survived) or '-'}",
    ))
    return out
