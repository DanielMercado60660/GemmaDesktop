#!/usr/bin/env python3
"""Export compact, durable summaries of pilot batches into docs/evidence.

Usage: export_evidence.py OUT_DIR BATCH_DIR [BATCH_DIR ...]
Writes OUT_DIR/<label>.json per batch: identities (binary, model, runtime),
settings, and per task the prompt, outcomes, checks, failures, final answer,
diff and receipt commands. Transcripts and data directories stay in .runtime.
Paths are made relative to the repository so records are machine-neutral.
"""
from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]


def rel(text: str) -> str:
    return text.replace(str(ROOT) + "/", "").replace(str(Path.home()), "~")


def export(batch: Path, out: Path) -> Path:
    meta = json.loads((batch / "batch.json").read_text())
    model = meta.get("model") or {}
    record = {
        "schema": "gemma-pilot-batch-summary-v1",
        "label": meta.get("label"),
        "startedAt": meta.get("startedAt"),
        "finishedAt": meta.get("finishedAt"),
        "binarySHA256": meta.get("binarySHA256"),
        "model": {k: model.get(k) for k in ("name", "digest", "size", "details") if k in model},
        "runtimeVersion": (meta.get("runtime") or {}).get("version"),
        "agent": (meta.get("cfg") or {}).get("agent"),
        "settings": (meta.get("cfg") or {}).get("settings"),
        "sourceFolder": rel(str(batch)),
        "tasks": [],
    }
    for task in meta.get("tasks", []):
        p = batch / task / "record.json"
        if not p.is_file():
            record["tasks"].append({"task": task, "missing": True})
            continue
        r = json.loads(p.read_text())
        run = r.get("run") or {}
        receipt = run.get("receipt") or {}
        record["tasks"].append({
            "task": task,
            "category": r.get("category"),
            "prompt": r.get("prompt"),
            "verificationContract": r.get("verificationContract"),
            "status": r.get("status"),
            "error": run.get("error"),
            "elapsedSeconds": r.get("elapsedSeconds"),
            "outcomes": r.get("outcomes"),
            "checks": [{k: c.get(k) for k in ("name", "passed", "critical", "detail")} for c in (r.get("grade") or {}).get("checks", [])],
            "graderError": (r.get("grade") or {}).get("infrastructureError"),
            "usage": run.get("usage"),
            "toolCalls": {k: (r.get("toolCalls") or {}).get(k) for k in ("calls", "errors", "byTool", "failures", "validationErrors", "validationFailures")},
            "receiptCommands": receipt.get("commands"),
            "filesChanged": r.get("filesChanged"),
            "finalAnswer": r.get("finalAnswer"),
            "diff": r.get("diff"),
        })
    out.mkdir(parents=True, exist_ok=True)
    dest = out / f"{record['label']}.json"
    dest.write_text(rel(json.dumps(record, indent=1, ensure_ascii=False)) + "\n")
    return dest


def main():
    out = Path(sys.argv[1])
    for b in sys.argv[2:]:
        print(export(Path(b), out))


if __name__ == "__main__":
    main()
