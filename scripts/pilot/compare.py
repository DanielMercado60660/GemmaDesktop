#!/usr/bin/env python3
"""Summarize pilot batches side by side (markdown table plus JSON).

Usage: compare.py BATCH_DIR [BATCH_DIR ...]
Each column reads <batch>/<task>/record.json. Outcomes are reported separately:
independent correctness (the grade), host verification, run status, time,
tool calls and failed tool calls. Nothing is recomputed or regraded here.
"""
from __future__ import annotations

import json
from pathlib import Path
import sys


def load(batch: Path) -> dict:
    meta = json.loads((batch / "batch.json").read_text())
    rows = {}
    for task in meta.get("tasks", []):
        p = batch / task / "record.json"
        if not p.is_file():
            rows[task] = {"missing": True}
            continue
        r = json.loads(p.read_text())
        run = r.get("run") or {}
        failed = [c["name"] for c in (r.get("grade") or {}).get("checks", []) if not c["passed"]]
        rows[task] = {
            "correct": bool((r.get("grade") or {}).get("passed")),
            "status": r.get("status"),
            "verification": run.get("verification") or "",
            "error": (run.get("error") or "")[:160],
            "seconds": r.get("elapsedSeconds"),
            "toolCalls": (r.get("toolCalls") or {}).get("calls"),
            "toolErrors": (r.get("toolCalls") or {}).get("errors"),
            "failedChecks": failed,
            "outputTokens": (run.get("usage") or {}).get("eval_count"),
        }
    return {"label": meta.get("label"), "binarySHA256": meta.get("binarySHA256"), "model": (meta.get("cfg") or {}).get("model"), "rows": rows}


def cell(x: dict) -> str:
    if x.get("missing"):
        return "not run"
    mark = "PASS" if x["correct"] else "FAIL"
    v = {"passed": ", verified", "unverified": ", unverified"}.get(x["verification"], "")
    st = "" if x["status"] == "completed" else f", run {x['status']}"
    return f"{mark}{v}{st} · {x['seconds']:.0f}s · {x['toolCalls']} calls ({x['toolErrors']} failed)"


def main():
    batches = [load(Path(p)) for p in sys.argv[1:]]
    tasks = []
    for b in batches:
        tasks += [t for t in b["rows"] if t not in tasks]
    print("| Task | " + " | ".join(b["label"] for b in batches) + " |")
    print("|---|" + "---|" * len(batches))
    for t in tasks:
        print(f"| {t} | " + " | ".join(cell(b["rows"].get(t, {"missing": True})) for b in batches) + " |")
    totals = []
    for b in batches:
        rows = [r for r in b["rows"].values() if not r.get("missing")]
        contract = [r for r in rows if r["verification"]]
        totals.append(f"{sum(r['correct'] for r in rows)}/{len(rows)} correct; {sum(r['verification'] == 'passed' for r in contract)}/{len(contract)} host-verified")
    print("| **Total** | " + " | ".join(totals) + " |")
    print()
    print(json.dumps(batches, indent=1))


if __name__ == "__main__":
    main()
