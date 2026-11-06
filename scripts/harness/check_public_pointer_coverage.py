"""Trusted host controls for the opt-in public JSON Pointer coverage pack.

Requires explicit host execution and a new ignored artifact directory. It never
invokes a solver/model. Full command payloads, outputs and source copies remain
under artifacts/private; stdout and safe-summary.json contain counts only.
This establishes host reference/mutant behavior, not app-grader containment or
model qualification.
"""
from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import subprocess


PROJECT = Path(__file__).resolve().parents[2]
ORIGINAL = "coding-diagnostics-v2"
SIBLING = "coding-diagnostics-v2-public-coverage"
TASK = "json-pointer"
PUBLIC_PREFIX = "public-coverage-"
ORIGINAL_REFERENCE_SHA256 = "622cd6e9e88a23ef672f16076ec33856335abc3719f47041b49d50e4e4dbfde6"
SIBLING_REFERENCE_SHA256 = "3df8ea31a61fb249aac15854cc151f202d9445404f42600e39221b0768c90a69"


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def write_files(root: Path, files: dict[str, str]) -> None:
    for name, body in files.items():
        relative = PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts or str(relative) != name:
            raise ValueError("unsafe trusted fixture path")
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(body)


def json_equal(a, b) -> bool:
    # JSON numbers compare numerically; bools must not pass as Python ints.
    if isinstance(a, bool) or isinstance(b, bool):
        return type(a) is bool and type(b) is bool and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return a == b
    if type(a) is not type(b):
        return False
    if isinstance(a, list):
        return len(a) == len(b) and all(json_equal(x, y) for x, y in zip(a, b))
    if isinstance(a, dict):
        return a.keys() == b.keys() and all(json_equal(a[k], b[k]) for k in a)
    return a == b


def parse_json(text: str):
    def nonfinite(_):
        raise ValueError("non-finite JSON number")
    return json.loads(text, parse_constant=nonfinite)


class Controls:
    def __init__(self, artifacts: Path):
        self.artifacts = artifacts
        self.calls = 0
        self.reaped = 0

    def run(self, root: Path, argv: list[str], label: str):
        self.calls += 1
        target = self.artifacts / "private" / "commands" / f"{self.calls:03d}-{label}.json"
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1", GOWORK="off")
        proc = None
        row = {}
        timed_out = False
        stdout, stderr = "", ""
        captured = False
        primary_error = None
        try:
            proc = subprocess.Popen(argv, cwd=root, env=env, text=True, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, start_new_session=True)
            # Retain the handle immediately. Metadata failures are inside the
            # same cleanup guard as every other post-launch operation.
            row = {"pid": proc.pid, "ownedProcessGroup": proc.pid, "argv": argv,
                   "workspace": str(root), "status": "running"}
            write_json(target, row)
            stdout, stderr = proc.communicate(timeout=60)
            captured = True
        except subprocess.TimeoutExpired:
            timed_out = True
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            stdout, stderr = proc.communicate()
            captured = True
        except BaseException as exc:
            primary_error = exc
            raise
        finally:
            if proc is not None:
                try:
                    if proc.poll() is None:
                        try:
                            os.killpg(proc.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                finally:
                    proc.wait()
                    self.reaped += 1
                row.update({"status": "finished", "returncode": proc.returncode,
                            "timedOut": timed_out, "stdout": stdout, "stderr": stderr,
                            "outputCaptureComplete": captured, "waitedAndReaped": True})
            else:
                row = {"status": "not_started", "argv": argv,
                       "workspace": str(root), "waitedAndReaped": False}
            if primary_error is not None:
                row.update({"errorClass": type(primary_error).__name__,
                            "privateError": str(primary_error)})
            try:
                write_json(target, row)
            except Exception:
                # Preserve an earlier failure after its child is reaped; a
                # terminal-only artifact failure still fails this control.
                if primary_error is None:
                    raise
        if timed_out:
            raise RuntimeError("trusted control command timed out")
        return proc.returncode, stdout

    def evaluate(self, root: Path, task: dict, label: str):
        payload = json.dumps([case["input"] for case in task["cases"]], ensure_ascii=False)
        code, stdout = self.run(root, [*task["gradeCommand"], payload], label)
        if code != 0:
            raise RuntimeError("trusted semantic command failed")
        actual = parse_json(stdout)
        if not isinstance(actual, list) or len(actual) != len(task["cases"]):
            raise RuntimeError("trusted semantic command returned an invalid batch")
        return [json_equal(value, case["expected"]) for value, case in zip(actual, task["cases"])]


def faulty_reference(reference: dict[str, str], name: str) -> dict[str, str]:
    result = copy.deepcopy(reference)
    result["logic.go"], count = re.subn(r"\bfunc\s+solve\s*\(", "func coverageCorrectSolve(",
                                       result["logic.go"], count=1)
    if count != 1:
        raise RuntimeError("reference control requires one solve entry point")
    variants = {
        "signed-array-index": '''package main
import ("strconv"; "strings")
func solve(x map[string]any) any {
 p, _ := x["pointer"].(string)
 if node, ok := x["doc"].([]any); ok && strings.HasPrefix(p, "/+") && !strings.Contains(p[1:], "/") {
  n, err := strconv.Atoi(p[1:]); if err == nil && n >= 0 && n < len(node) { return node[n] }
 }
 return coverageCorrectSolve(x)
}
''',
        "nonleading-pointer": '''package main
import "strings"
func solve(x map[string]any) any {
 p, _ := x["pointer"].(string)
 if p != "" && !strings.HasPrefix(p, "/") {
  changed := make(map[string]any, len(x)); for k, v := range x { changed[k] = v }; changed["pointer"] = "/" + p
  return coverageCorrectSolve(changed)
 }
 return coverageCorrectSolve(x)
}
''',
        "unicode-byte-corruption": '''package main
import "strings"
func solve(x map[string]any) any {
 p, _ := x["pointer"].(string); var b strings.Builder
 for i := 0; i < len(p); i++ { b.WriteRune(rune(p[i])) }
 changed := make(map[string]any, len(x)); for k, v := range x { changed[k] = v }; changed["pointer"] = b.String()
 return coverageCorrectSolve(changed)
}
''',
        "always-null": '''package main
func solve(x map[string]any) any { return nil }
''',
        "blanket-plus-rejection": '''package main
import "strings"
func solve(x map[string]any) any {
 p, _ := x["pointer"].(string); if strings.Contains(p, "+") { return nil }; return coverageCorrectSolve(x)
}
''',
    }
    result["public_coverage_mutant.go"] = variants[name]
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", action="store_true")
    parser.add_argument("--pack-root", type=Path, default=PROJECT / "internal/harness/packs")
    parser.add_argument("--artifacts-dir", type=Path, required=True)
    args = parser.parse_args()
    if not args.host:
        parser.error("explicit --host is required for trusted fixture execution")
    artifacts = args.artifacts_dir.resolve()
    relative = artifacts.relative_to((PROJECT / ".runtime").resolve())
    if not relative.parts:
        parser.error("an unused artifact subdirectory under .runtime is required")
    artifacts.mkdir(parents=True, exist_ok=False)
    root = args.pack_root.resolve()
    paths = [root / ORIGINAL / "manifest.json", root / ORIGINAL / TASK / "task.json",
             root / ORIGINAL / TASK / "reference.json", root / SIBLING / "manifest.json",
             root / SIBLING / TASK / "task.json", root / SIBLING / TASK / "reference.json"]
    if any(path.is_symlink() or not path.is_file() for path in paths):
        raise ValueError("regular trusted fixture files required")
    before = {str(path): sha(path) for path in paths}
    summary = {"schema": "public-pointer-host-controls-v1", "status": "running", "modelCalls": 0,
               "scope": "trusted-host-oracles-only; no app-grader or model qualification"}
    write_json(artifacts / "safe-summary.json", summary)
    controls = Controls(artifacts)
    try:
        old_task = json.loads(paths[1].read_text())
        task = json.loads(paths[4].read_text())
        reference = json.loads(paths[5].read_text())
        inherited = len(old_task["cases"])
        if (task["cases"][:inherited] != old_task["cases"] or len(task["cases"]) != inherited + 8
                or {k: v for k, v in task.items() if k != "cases"} != {k: v for k, v in old_task.items() if k != "cases"}
                or sha(paths[2]) != ORIGINAL_REFERENCE_SHA256
                or sha(paths[5]) != SIBLING_REFERENCE_SHA256):
            raise RuntimeError("coverage preservation check failed")
        old_reference = json.loads(paths[2].read_text())
        if (old_reference.keys() != reference.keys()
                or any(body != reference[name] for name, body in old_reference.items() if name != "logic.go")):
            raise RuntimeError("sibling reference changed outside the permitted source fork")
        added = {case["name"]: inherited + i for i, case in enumerate(task["cases"][inherited:])}
        if len(added) != 8 or any(not name.startswith(PUBLIC_PREFIX) for name in added):
            raise RuntimeError("public coverage cases are missing or ambiguous")
        workspace = artifacts / "private" / "reference"
        workspace.mkdir(parents=True)
        write_files(workspace, task["public"])
        write_files(workspace, {p: body for p, body in reference.items() if p not in task["public"]})
        if controls.run(workspace, task["discoveryCommand"], "reference-red")[0] == 0:
            raise RuntimeError("inherited regression is not red on original implementation")
        write_files(workspace, reference)
        if not all(controls.evaluate(workspace, task, "reference-semantics")):
            raise RuntimeError("sibling reference violates coverage contract")
        for label, command in [("reference-public", task["command"]), ("reference-discovery", task["discoveryCommand"])]:
            if controls.run(workspace, command, label)[0] != 0:
                raise RuntimeError("reference verification failed")
        failures = {
            "signed-array-index": ["public-coverage-array-plus-rejected"],
            "nonleading-pointer": ["public-coverage-leading-slash-required"],
            "unicode-byte-corruption": ["public-coverage-unicode-key-preserved"],
            "always-null": ["public-coverage-canonical-index-positive", "public-coverage-leading-slash-positive",
                            "public-coverage-plus-object-key-positive", "public-coverage-empty-root-positive",
                            "public-coverage-empty-token-positive", "public-coverage-unicode-key-preserved"],
            "blanket-plus-rejection": ["public-coverage-plus-object-key-positive"],
        }
        for name, required in failures.items():
            workspace = artifacts / "private" / name
            workspace.mkdir()
            write_files(workspace, task["public"])
            write_files(workspace, faulty_reference(reference, name))
            passed = controls.evaluate(workspace, task, name)
            if any(passed[added[key]] for key in required):
                raise RuntimeError("targeted negative control was not rejected")
            if name in ("signed-array-index", "nonleading-pointer", "unicode-byte-corruption") and not all(passed[:inherited]):
                raise RuntimeError("targeted control also fails inherited coverage")
        if {str(path): sha(path) for path in paths} != before:
            raise RuntimeError("trusted fixture bytes changed during controls")
        summary.update({"status": "passed", "referencePasses": 1, "inheritedRedGreenControls": 1,
                        "targetedPublicDefectNegatives": 3, "alwaysNullNegatives": 1,
                        "blanketPlusNegatives": 1, "inheritedCases": inherited,
                        "addedPublicCases": 8, "totalCases": len(task["cases"]),
                        "commandRequests": controls.calls, "childrenWaitedAndReaped": controls.reaped,
                        "fixtureBytesUnchanged": True})
        code = 0
    except Exception as exc:
        summary.update({"status": "failed", "errorClass": type(exc).__name__,
                        "commandRequests": controls.calls, "childrenWaitedAndReaped": controls.reaped})
        code = 1
    write_json(artifacts / "safe-summary.json", summary)
    print(json.dumps(summary))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
