#!/usr/bin/env python3
"""Regrade frozen pilot outputs with the current checkers, without rewriting records.

Usage: regrade.py BATCH_DIR [TASK ...]
Grades <batch>/<task>/final against the hashes of <batch>/<task>/original and
the recorded final answer, then prints old vs new grades. Original record.json
files are never modified; results go to <batch>/<task>/regrade.json.
"""
from __future__ import annotations

import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import gemma_pilot as g  # noqa: E402


def main():
    batch = Path(sys.argv[1])
    tasks = sys.argv[2:] or json.loads((batch / "batch.json").read_text()).get("tasks", [])
    for t in tasks:
        case = batch / t
        if not (case / "record.json").is_file():
            continue
        record = json.loads((case / "record.json").read_text())
        grade = g.run_checker(g.load_task(t), case / "final", g.tree(case / "original"), record)
        g.write_json(case / "regrade.json", {"regradedAt": g.now(), "previous": record.get("grade"), "grade": grade})
        failed = [c["name"] + ("" if c.get("critical", True) else " (advisory)") for c in grade["checks"] if not c["passed"]]
        print(f"{batch.name} {t}: was {'PASS' if record['grade']['passed'] else 'FAIL'} -> now {'PASS' if grade['passed'] else 'FAIL'}; failing: {failed}")


if __name__ == "__main__":
    main()
