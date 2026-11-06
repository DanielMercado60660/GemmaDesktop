#!/usr/bin/env python3
"""Prove every pilot task's checker before it grades a model.

Negative control: the untouched fixture (with no answer) must FAIL.
Positive control: the fixture overlaid with tasks/<id>/reference/ (and
reference/answer.txt as the final answer, if present) must PASS.
Files listed in reference/DELETE (one relative path per line) are removed
from the positive-control copy, for tasks whose solution moves files.
No model is involved; this is checker evidence only.
"""
from __future__ import annotations

import json
from pathlib import Path
import shutil
import sys
import tempfile

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import gemma_pilot as g  # noqa: E402


def control(task_id: str) -> dict:
    task = g.load_task(task_id)
    root = task["root"]
    out = {"task": task_id}
    with tempfile.TemporaryDirectory(prefix="gemma-pilot-controls-") as tmp:
        pristine = Path(tmp) / "pristine"
        shutil.copytree(root / "workspace", pristine)
        original = g.tree(pristine)
        neg = g.run_checker(task, pristine, original, {"finalAnswer": "", "status": "completed"})
        out["negative"] = {"passed": neg["passed"], "failed": [c["name"] for c in neg["checks"] if not c["passed"]], "error": neg.get("infrastructureError")}
        ref = Path(tmp) / "reference"
        shutil.copytree(root / "workspace", ref)
        answer = ""
        rdir = root / "reference"
        if rdir.is_dir():
            for p in rdir.rglob("*"):
                rel = p.relative_to(rdir)
                if p.is_file() and str(rel) not in ("answer.txt", "DELETE"):
                    (ref / rel).parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(p, ref / rel)
            if (rdir / "answer.txt").is_file():
                answer = (rdir / "answer.txt").read_text()
            if (rdir / "DELETE").is_file():
                for line in (rdir / "DELETE").read_text().splitlines():
                    if line.strip():
                        (ref / line.strip()).unlink()
        pos = g.run_checker(task, ref, original, {"finalAnswer": answer, "status": "completed"})
        out["positive"] = {"passed": pos["passed"], "failed": [(c["name"], c["detail"][:300]) for c in pos["checks"] if not c["passed"]], "error": pos.get("infrastructureError")}
    out["ok"] = (not out["negative"]["passed"]) and out["positive"]["passed"]
    return out


def main():
    ids = sys.argv[1:] or sorted(p.name for p in (HERE / "tasks").iterdir() if (p / "task.json").is_file())
    results = [control(t) for t in ids]
    print(json.dumps(results, indent=2))
    sys.exit(0 if all(r["ok"] for r in results) else 1)


if __name__ == "__main__":
    main()
