import json, sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged, with_trusted, python_probe

HERE = Path(__file__).resolve().parent


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, ["test_calculator.py"])]
    w = with_trusted(final, {"test_calculator.py": HERE / "trusted/test_calculator.py"})
    r = sandboxed([PYTHON, "-m", "unittest", "-v"], w)
    out.append(result("original tests pass", r.returncode == 0 and "OK" in r.stderr, r.stderr[-1500:]))
    ok, txt = python_probe(w, "from calculator import add\nimport json\nprint(json.dumps([add(0,0), add(1.5,2.25), add(-10,3), add(10**20, 1)]))")
    want = [0, 3.75, -7, 10**20 + 1]
    try:
        got = json.loads(txt.strip().splitlines()[-1]) if ok else None
    except Exception:
        got = None
    out.append(result("additional addition cases", got == want, f"got {got!r}"))
    return out
