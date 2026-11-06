import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged, with_trusted  # noqa: E402

HERE = Path(__file__).resolve().parent
PROTECTED = ["tests/test_todo.py", "README.md"]
TRUSTED = {"tests/test_todo.py": HERE / "trusted/tests/test_todo.py"}
ORIGINAL_TODO = HERE / "trusted/todo_original.py"
ORIGINAL_TEST_COUNT = 7
PRIOS = ("high", "medium", "low")
MARK = "@@TODO-PROBE@@"

# Trusted acceptance probe. It drives the candidate's main(argv) in-process
# (no subprocesses needed) against
# JSON files in the sandbox's private temp dir and reports raw observations.
PROBE = r'''
import contextlib, inspect, io, json, os, sys, tempfile, traceback
import todo

TMP = tempfile.mkdtemp()
try:
    TAKES_ARGV = bool(inspect.signature(todo.main).parameters)
except (TypeError, ValueError):
    TAKES_ARGV = True

def call(path, *args):
    argv = ["--file", path, *args]
    out, err = io.StringIO(), io.StringIO()
    code, exc = None, None
    sys.argv = ["todo.py", *argv]
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        try:
            code = todo.main(argv) if TAKES_ARGV else todo.main()
        except SystemExit as e:
            code = e.code
        except BaseException:
            exc = traceback.format_exc()[-1200:]
    msg = ""
    if isinstance(code, str):
        msg, code = code, 1
    elif isinstance(code, bool):
        code = int(code)
    elif code is not None and not isinstance(code, int):
        msg, code = repr(code), 1
    return {"args": list(args), "code": code or 0, "out": out.getvalue(), "err": err.getvalue() + msg, "exc": exc}

res = {}
f = os.path.join(TMP, "s.json")
res["s_add"] = [
    call(f, "add", "Water the plants", "--priority", "low"),
    call(f, "add", "Renew passport", "--priority", "high"),
    call(f, "add", "Book dentist appointment"),
    call(f, "add", "Call the bank", "--priority", "high"),
    call(f, "add", "Clean the garage", "--priority", "low"),
    call(f, "add", "Pay electricity bill", "--priority", "medium"),
]
res["s_list"] = call(f, "list")
res["s_sorted"] = call(f, "list", "--sort", "priority")
res["s_list_after_sort"] = call(f, "list")
res["s_done"] = call(f, "done", "4")
res["s_list_after_done"] = call(f, "list")

f = os.path.join(TMP, "i.json")
res["i_add_ok"] = call(f, "add", "Existing task")
res["i_add_bad"] = call(f, "add", "Mystery task", "--priority", "urgent")
res["i_list"] = call(f, "list")

f = os.path.join(TMP, "legacy.json")
with open(f, "w", encoding="utf-8") as fh:
    json.dump([{"id": 1, "text": "Buy milk", "done": False},
               {"id": 2, "text": "Walk the dog", "done": True},
               {"id": 3, "text": "Fix the bike", "done": False}], fh, indent=2)
res["b_list"] = call(f, "list")
res["b_add"] = [call(f, "add", "Submit expense report", "--priority", "high"),
                call(f, "add", "Order printer ink", "--priority", "low")]
res["b_sorted"] = call(f, "list", "--sort", "priority")
res["b_done"] = call(f, "done", "3")
res["b_list_after_done"] = call(f, "list")
print(MARK + json.dumps(res))
'''.replace("MARK", repr(MARK))

S_ADDED = [("Water the plants", "low"), ("Renew passport", "high"), ("Book dentist appointment", "medium"),
           ("Call the bank", "high"), ("Clean the garage", "low"), ("Pay electricity bill", "medium")]
S_SORTED = ["Renew passport", "Call the bank", "Book dentist appointment", "Pay electricity bill",
            "Water the plants", "Clean the garage"]


def ok(step) -> bool:
    return step is not None and step["exc"] is None and step["code"] == 0


def brief(step) -> str:
    if step is None:
        return "missing"
    return json.dumps({k: step[k] for k in ("args", "code", "exc") if step.get(k) is not None} | {"out": step["out"][-400:], "err": step["err"][-300:]})


def line_for(out: str, text: str):
    return next((ln for ln in out.splitlines() if text in ln), None)


def words(line: str) -> set:
    return {p for p in PRIOS if re.search(rf"\b{p}\b", line, re.I)}


def in_order(out: str, texts: list[str]) -> tuple[bool, str]:
    lines = out.splitlines()
    pos = []
    for t in texts:
        idx = next((i for i, ln in enumerate(lines) if t in ln), None)
        if idx is None:
            return False, f"{t!r} not listed"
        pos.append(idx)
    good = all(a < b for a, b in zip(pos, pos[1:]))
    return good, "" if good else "order seen: " + " | ".join(t for _, t in sorted(zip(pos, texts)))


def run_suite(w: Path, pattern: str = "test*.py"):
    try:
        r = sandboxed([PYTHON, "-m", "unittest", "discover", "-s", "tests", "-p", pattern, "-v"], w, timeout=120)
    except subprocess.TimeoutExpired:
        return -1, 0, "timed out"
    m = re.search(r"^Ran (\d+) tests?", r.stderr, re.M)
    return r.returncode, int(m.group(1)) if m else 0, r.stderr


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    temps = []
    try:
        w = with_trusted(final, TRUSTED)
        temps.append(w.parent)

        rc, ran, log = run_suite(w, "test_todo.py")
        out.append(result("original tests pass", rc == 0 and ran == ORIGINAL_TEST_COUNT and "OK" in log, log[-1500:]))

        rc_all, ran_all, log_all = run_suite(w)
        out.append(result("full test suite passes", rc_all == 0 and "OK" in log_all, log_all[-1500:]))

        tests_dir = final / "tests"
        new_tests = sorted(p for p in tests_dir.glob("test*.py") if p.name != "test_todo.py") if tests_dir.is_dir() else []
        mentions = [p.name for p in new_tests if "priorit" in p.read_text(errors="replace").lower()]
        out.append(result("new priority tests added",
                          bool(mentions) and ran_all > ORIGINAL_TEST_COUNT,
                          f"new test files: {[p.name for p in new_tests]}; mentioning priority: {mentions}; tests discovered: {ran_all}"))

        # Revert the implementation to the pre-feature code: the new tests must notice.
        m = with_trusted(final, {**TRUSTED, "todo.py": ORIGINAL_TODO})
        temps.append(m.parent)
        for p in list(m.rglob("*.py")):
            rel = str(p.relative_to(m))
            if not rel.startswith("tests/") and rel not in original:
                p.unlink()
        rc_mut, ran_mut, log_mut = run_suite(m)
        out.append(result("new tests fail on pre-feature code",
                          bool(new_tests) and ran_mut > ORIGINAL_TEST_COUNT and rc_mut != 0,
                          f"exit {rc_mut}, ran {ran_mut}\n" + log_mut[-1200:]))

        # Same as checklib.python_probe, but keeps the full stdout (the report is long).
        try:
            r = sandboxed([PYTHON, "-I", "-c", "import sys; sys.path.insert(0, '.')\n" + PROBE], w, timeout=60)
            good, txt = r.returncode == 0, r.stdout + r.stderr
        except subprocess.TimeoutExpired:
            good, txt = False, "probe timed out"
        res = None
        if good:
            for ln in r.stdout.splitlines():
                if ln.startswith(MARK):
                    try:
                        res = json.loads(ln[len(MARK):])
                    except ValueError:
                        res = None
        if res is None:
            for name in ("add --priority accepted", "list shows each priority", "list --sort priority order",
                         "plain list keeps added order", "done works with priorities", "invalid priority rejected",
                         "old files without priority still work"):
                out.append(result(name, False, "probe failed: " + txt[-1500:]))
            return out

        adds = res["s_add"]
        out.append(result("add --priority accepted", all(ok(s) for s in adds),
                          "; ".join(brief(s) for s in adds if not ok(s))))

        lst = res["s_list"]
        bad = []
        if not ok(lst):
            bad.append(brief(lst))
        else:
            for text, prio in S_ADDED:
                ln = line_for(lst["out"], text)
                if ln is None or words(ln) != {prio}:
                    bad.append(f"{text!r} (want {prio}): {ln!r}")
        out.append(result("list shows each priority", not bad, "\n".join(bad)))

        srt = res["s_sorted"]
        if ok(srt):
            good, why = in_order(srt["out"], S_SORTED)
        else:
            good, why = False, brief(srt)
        out.append(result("list --sort priority order", good, why + "\n" + srt["out"][-600:]))

        texts = [t for t, _ in S_ADDED]
        bad = []
        for key in ("s_list", "s_list_after_sort"):
            step = res[key]
            g, why = in_order(step["out"], texts) if ok(step) else (False, brief(step))
            if not g:
                bad.append(f"{key}: {why}")
        out.append(result("plain list keeps added order", not bad, "\n".join(bad)))

        bad = []
        if not ok(res["s_done"]):
            bad.append(brief(res["s_done"]))
        after = res["s_list_after_done"]
        if not ok(after):
            bad.append(brief(after))
        else:
            for text, prio in S_ADDED:
                ln = line_for(after["out"], text) or ""
                want_done = text == "Call the bank"
                if ln.lstrip().startswith("[x]") != want_done or words(ln) != {prio}:
                    bad.append(f"{text!r}: {ln!r}")
        out.append(result("done works with priorities", not bad, "\n".join(bad)))

        bad_add, inv_list = res["i_add_bad"], res["i_list"]
        msg = (bad_add["out"] + bad_add["err"]).lower()
        problems = []
        if bad_add["exc"] is not None:
            problems.append("crashed: " + bad_add["exc"][-400:])
        elif bad_add["code"] == 0:
            problems.append("exit code 0")
        if not re.search(r"priorit|urgent", msg):
            problems.append(f"no helpful message: {msg[-300:]!r}")
        if not ok(res["i_add_ok"]) or not ok(inv_list) or "Existing task" not in inv_list["out"]:
            problems.append("list broken after rejected add: " + brief(inv_list))
        elif "Mystery task" in inv_list["out"]:
            problems.append("invalid item was saved")
        out.append(result("invalid priority rejected", not problems, "\n".join(problems)))

        problems = []
        bl = res["b_list"]
        if not ok(bl):
            problems.append("list: " + brief(bl))
        else:
            for text in ("Buy milk", "Walk the dog", "Fix the bike"):
                ln = line_for(bl["out"], text)
                if ln is None or words(ln) != {"medium"}:
                    problems.append(f"{text!r} should show medium: {ln!r}")
            if not (line_for(bl["out"], "Walk the dog") or "").lstrip().startswith("[x]"):
                problems.append("'Walk the dog' lost its done mark")
        for s in res["b_add"]:
            if not ok(s):
                problems.append("add: " + brief(s))
        bs = res["b_sorted"]
        if not ok(bs):
            problems.append("sort: " + brief(bs))
        else:
            g, why = in_order(bs["out"], ["Submit expense report", "Buy milk", "Fix the bike", "Order printer ink"])
            if not g or line_for(bs["out"], "Walk the dog") is None:
                problems.append("sort: " + (why or "'Walk the dog' missing") + "\n" + bs["out"][-500:])
        if not ok(res["b_done"]):
            problems.append("done: " + brief(res["b_done"]))
        ad = res["b_list_after_done"]
        if not ok(ad):
            problems.append("list after done: " + brief(ad))
        else:
            if not (line_for(ad["out"], "Fix the bike") or "").lstrip().startswith("[x]"):
                problems.append("done 3 did not mark 'Fix the bike'")
            if not (line_for(ad["out"], "Buy milk") or "").lstrip().startswith("[ ]"):
                problems.append("'Buy milk' changed state")
            if not re.search(r"\b4\.", line_for(ad["out"], "Submit expense report") or ""):
                problems.append("new item did not get id 4: " + repr(line_for(ad["out"], "Submit expense report")))
        out.append(result("old files without priority still work", not problems, "\n".join(problems)))
        return out
    finally:
        for t in temps:
            shutil.rmtree(t, ignore_errors=True)
