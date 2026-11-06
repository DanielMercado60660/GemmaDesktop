"""Independent checker for wordfreq-repair.

Grades only the frozen final files. Every run of candidate code goes through
checklib's read-only, network-free sandbox. Trusted inputs (original tests,
the sample text, the original buggy package and a fixed package) come from
this task directory, never from the candidate's workspace.
"""
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, python_probe, result, sandboxed, unchanged, with_trusted  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED = HERE / "trusted"
PROTECTED = ["tests/test_textkit.py"]
REGRESSION = "tests/test_regressions.py"
MARK = "@@WORDFREQ-RESULT@@"

# Runs tests/test_regressions.py in the current directory and reports counts.
# Errors whose type signals an API mismatch (a helper or signature the
# original package never had) do not count as catching a bug.
RUNNER = r'''
import io, json, unittest
API = {"AttributeError", "ImportError", "ModuleNotFoundError", "NameError", "TypeError"}
def kind(tb):
    lines = [l for l in tb.strip().splitlines() if l.strip()]
    return (lines[-1] if lines else "").split(":", 1)[0].strip().rsplit(".", 1)[-1]
loader = unittest.TestLoader()
suite = loader.discover("tests", pattern="test_regressions.py")
stream = io.StringIO()
res = unittest.TextTestRunner(stream=stream, verbosity=2).run(suite)
genuine = [t for t, tb in res.errors if kind(tb) not in API]
print("MARK" + json.dumps({
    "loadErrors": len(loader.errors),
    "run": res.testsRun,
    "failures": len(res.failures),
    "errors": len(res.errors),
    "genuineErrors": len(genuine),
    "skipped": len(res.skipped),
    "expectedFailures": len(res.expectedFailures),
    "unexpectedSuccesses": len(res.unexpectedSuccesses),
    "log": stream.getvalue()[-1500:],
}))
'''.replace("MARK", MARK)

TOKENIZE_CASES = [
    ('"Hello," she said.', ["hello", "she", "said"]),
    ("(Really?) Yes -- it's well-known!", ["really", "yes", "it's", "well-known"]),
    ("'quoted' [bracketed] {braced} *starred* <angled>", ["quoted", "bracketed", "braced", "starred", "angled"]),
    ("... !!! --", []),
    ("", []),
    ("  MiXeD\tCASE\nlines  ", ["mixed", "case", "lines"]),
    ("...and then?!", ["and", "then"]),
    ('"Don\'t," she said; "it\'s fine."', ["don't", "she", "said", "it's", "fine"]),
]

STATS_CASES = [
    # (expression, expected JSON-able value)
    ("dict(word_counts('The the THE end. \"End\"'))", {"the": 3, "end": 2}),
    ("top_n('b a c b a c d', 2)", [["a", 2], ["b", 2]]),
    ("top_n('zeta alpha zeta alpha mid mid beta', 2)", [["alpha", 2], ["mid", 2]]),
    ("top_n('Dog cat. dog, CAT bird', 3)", [["cat", 2], ["dog", 2], ["bird", 1]]),
    ("top_n('x y z', 5)", [["x", 1], ["y", 1], ["z", 1]]),
    ("top_n('x y z', 0)", []),
    ("top_n('', 3)", []),
    ("top_n('k j i h g f e d c b a l')", [[w, 1] for w in "abcdefghij"]),
]

# python3 wordfreq.py sample.txt 5 on the trusted sample text.
CLI_WANT = [("the", 11), ("beans", 3), ("rain", 3), ("again", 2), ("and", 2)]


def _probe(w: Path, code: str) -> tuple[bool, str]:
    try:
        return python_probe(w, code)
    except subprocess.TimeoutExpired:
        return False, "timed out"


def _run(argv: list[str], cwd: Path, timeout: int = 120) -> subprocess.CompletedProcess:
    """sandboxed(), with a hang reported as a failed run instead of a checker crash."""
    try:
        return sandboxed(argv, cwd, timeout=timeout)
    except subprocess.TimeoutExpired:
        return subprocess.CompletedProcess(argv, 124, "", f"timed out after {timeout}s")


def _last_json(txt: str):
    for line in reversed(txt.strip().splitlines()):
        try:
            return json.loads(line)
        except Exception:
            continue
    return None


def _run_regressions(w: Path) -> dict | None:
    # sandboxed() directly (not python_probe): python_probe keeps only the last
    # 3000 characters of stdout+stderr, which can cut off the result line when
    # the candidate's tests write to stderr or the log has escaped non-ASCII.
    try:
        r = sandboxed([PYTHON, "-I", "-c", "import sys; sys.path.insert(0, '.')\n" + RUNNER], w)
    except subprocess.TimeoutExpired:
        return None
    for line in reversed(r.stdout.splitlines()):
        if line.startswith(MARK):
            try:
                return json.loads(line[len(MARK):])
            except Exception:
                return None
    return None


def _brief(r: dict | None) -> str:
    if r is None:
        return "regression runner produced no result"
    keys = ("loadErrors", "run", "failures", "errors", "genuineErrors", "skipped", "expectedFailures", "unexpectedSuccesses")
    return json.dumps({k: r[k] for k in keys}) + "\n" + r["log"]


def _variant(root: Path, name: str, final: Path, tokens: str, stats: str) -> Path:
    """Candidate's workspace with textkit/ replaced by a trusted package built
    from buggy/fixed files (so tests that also use wordfreq.py or sample.txt
    still load); trusted test_textkit.py and sample.txt are overlaid."""
    v = root / name
    shutil.copytree(final, v, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
    pkg = v / "textkit"
    if pkg.is_symlink() or pkg.is_file():
        pkg.unlink()
    elif pkg.exists():
        shutil.rmtree(pkg)
    pkg.mkdir(parents=True)
    shutil.copyfile(TRUSTED / "buggy/textkit/__init__.py", pkg / "__init__.py")
    shutil.copyfile(TRUSTED / tokens / "textkit/tokens.py", pkg / "tokens.py")
    shutil.copyfile(TRUSTED / stats / "textkit/stats.py", pkg / "stats.py")
    (v / "tests").mkdir(exist_ok=True)
    shutil.copyfile(TRUSTED / "tests/test_textkit.py", v / "tests/test_textkit.py")
    shutil.copyfile(TRUSTED / "sample.txt", v / "sample.txt")
    return v


def _caught(r: dict | None) -> bool:
    return bool(r) and r["loadErrors"] == 0 and r["run"] >= 1 and (r["failures"] + r["genuineErrors"]) >= 1


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    w = with_trusted(final, {
        "tests/test_textkit.py": TRUSTED / "tests/test_textkit.py",
        "sample.txt": TRUSTED / "sample.txt",
    })
    scratch = Path(tempfile.mkdtemp(prefix="gemma-pilot-wordfreq-"))
    try:
        # 1. The original tests (trusted copy) pass against the candidate package.
        r = _run([PYTHON, "-m", "unittest", "discover", "-s", "tests", "-p", "test_textkit.py", "-v"], w)
        ok = r.returncode == 0 and re.search(r"^Ran 8 tests?\b", r.stderr, re.M) and re.search(r"^OK\s*$", r.stderr, re.M)
        out.append(result("original tests pass", bool(ok), r.stderr[-1500:]))

        # 2. Hidden tokenizer behaviour described by the docstring.
        code = "import json\nfrom textkit import tokenize\nprint(json.dumps([tokenize(c) for c in %r]))" % [c for c, _ in TOKENIZE_CASES]
        ok, txt = _probe(w, code)
        got = _last_json(txt) if ok else None
        want = [e for _, e in TOKENIZE_CASES]
        bad = [f"{c!r}: got {g!r}, want {e!r}" for (c, e), g in zip(TOKENIZE_CASES, got or []) if g != e] if isinstance(got, list) else [txt[-800:]]
        out.append(result("hidden tokenize cases", got == want, "\n".join(bad)))

        # 3. Hidden word_counts / top_n behaviour (ties alphabetical, cutoff, n bounds).
        exprs = ", ".join(f"norm({e})" for e, _ in STATS_CASES)
        code = (
            "import json\nfrom textkit import top_n, word_counts\n"
            "def norm(v):\n    return v if isinstance(v, dict) else [list(p) for p in v]\n"
            f"print(json.dumps([{exprs}]))"
        )
        ok, txt = _probe(w, code)
        got = _last_json(txt) if ok else None
        want = [e for _, e in STATS_CASES]
        bad = [f"{c}: got {g!r}, want {e!r}" for (c, e), g in zip(STATS_CASES, got or []) if g != e] if isinstance(got, list) else [txt[-800:]]
        out.append(result("hidden word_counts/top_n cases", got == want, "\n".join(bad)))

        # 4. The wordfreq.py command still works end to end on the trusted sample.
        try:
            r = sandboxed([PYTHON, "wordfreq.py", "sample.txt", "5"], w, timeout=60)
            rows = []
            for line in r.stdout.splitlines():
                if not line.strip():
                    continue
                words = re.findall(r"[a-z][a-z'-]*", line.lower())
                nums = re.findall(r"\d+", line)
                rows.append((words[0] if len(words) == 1 else None, int(nums[0]) if len(nums) == 1 else None))
            cli_ok = r.returncode == 0 and rows == CLI_WANT
            detail = f"exit {r.returncode}; got {rows!r}\n{r.stderr[-800:]}"
        except subprocess.TimeoutExpired:
            cli_ok, detail = False, "timed out"
        out.append(result("wordfreq.py CLI output correct", cli_ok, detail))

        # 5. Regression tests exist, are real, and pass on the candidate.
        has_file = (final / REGRESSION).is_file()
        out.append(result("tests/test_regressions.py exists", has_file))
        green = _run_regressions(w) if has_file else None
        green_ok = bool(green) and green["loadErrors"] == 0 and green["run"] >= 2 and not any(
            green[k] for k in ("failures", "errors", "skipped", "expectedFailures", "unexpectedSuccesses"))
        out.append(result("regression tests (>=2) pass on fixed package", green_ok, _brief(green)))

        r = _run([PYTHON, "-m", "unittest", "discover", "-s", "tests", "-v"], w)
        # "OK (skipped=N)" is still a passing suite; the regression file itself
        # is separately required to have no skips above.
        ok = r.returncode == 0 and re.search(r"^OK\b", r.stderr, re.M)
        out.append(result("full test suite passes", bool(ok), r.stderr[-1500:]))

        # 6. Red/green: the regression tests must fail on the original buggy code,
        #    and catch each bug on its own.
        variants = [
            ("regression tests fail on original buggy package", "buggy", "buggy"),
            ("regression tests catch the tokenize bug", "buggy", "fixed"),
            ("regression tests catch the top_n tie bug", "fixed", "buggy"),
        ]
        for i, (name, tok, st) in enumerate(variants):
            if not has_file:
                out.append(result(name, False, "no regression test file"))
                continue
            red = _run_regressions(_variant(scratch, f"v{i}", final, tok, st))
            out.append(result(name, _caught(red), _brief(red)))
    finally:
        shutil.rmtree(w.parent, ignore_errors=True)
        shutil.rmtree(scratch, ignore_errors=True)
    return out
