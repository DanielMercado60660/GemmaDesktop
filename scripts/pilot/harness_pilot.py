#!/usr/bin/env python3
"""One production Harness diagnostic attempt (real solver + Docker grader).

Starts an isolated app instance, pins the toolchain image by digest, creates a
Docker capability profile, a dedicated agent and one frozen arm, then runs a
development-pack diagnostic with repeats=1 and maxAttempts=1. Everything is
recorded under a fresh output folder; nothing historical is touched.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys
import time
from datetime import datetime

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import gemma_pilot as g  # noqa: E402

IMAGE = "sha256:09c68348396bd61f72b3570279e009741490473e40f8cc70517531696767578c"
TOOLS = ["list_files", "read_file", "search_files", "write_file", "edit_file", "move_file", "make_directory", "run_command", "verify_task"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--binary", required=True, type=Path)
    ap.add_argument("--pack", default="coding-diagnostics-v2")
    ap.add_argument("--tasks", nargs="+", default=["event-join"])
    ap.add_argument("--model", default="gemma4:26b-mlx")
    ap.add_argument("--label", default="harness")
    ap.add_argument("--arm", default="{}", help="JSON overrides for harness.arms.create")
    a = ap.parse_args()
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    out = g.ROOT / ".runtime" / f"pilot-{a.label}-{stamp}"
    out.mkdir(parents=True)
    (out / "ws").mkdir()
    app = g.App(a.binary.resolve(), out / "data", out / "app.log")
    rec = {"startedAt": g.now(), "binarySHA256": g.sha(a.binary.resolve()), "pack": a.pack, "tasks": a.tasks, "model": a.model}
    try:
        app.call("execution.configure", {"image": IMAGE})
        prof = app.call("capability.save", {"version": 1, "revision": 0, "name": "Harness pilot (Docker)", "tools": TOOLS, "skillIds": [], "mcp": [], "delegateAgentIds": [], "executionProfile": "docker", "projectGuidance": False})
        ws = app.call("workspace.add", {"path": str(out / "ws"), "name": "harness-pilot"})
        agent = app.call("agent.save", {"name": "Gemma harness pilot", "instructions": "", "model": a.model, "maxSteps": 80, "mode": "approval", "thinking": "on", "maxOutputTokens": 16384, "temperature": 1, "toolProtocol": "native", "capabilityProfileId": prof["id"]})
        packs = app.call("harness.packs.list")
        rec["packs"] = [{"id": p["id"], "digest": p.get("digest"), "tasks": [(t["id"], t.get("split")) for t in p.get("tasks", [])]} for p in packs.get("items", [])]
        arm_req = {"workspaceId": ws["id"], "agentId": agent["id"], "name": f"{a.model} pilot", "provenance": f"pilot:{a.label}:{stamp}", "mode": "native", "runtimeId": "ollama-default", "model": a.model, "contextSize": 65536, "thinking": "on", "temperature": 1, "maxOutputTokens": 16384, "maxSteps": 80, "toolProtocol": "native", "budgetSeconds": 1200, "fixturePackId": a.pack, "taskIds": a.tasks} | json.loads(a.arm)
        rec["armRequest"] = arm_req
        arm = app.call("harness.arms.create", arm_req)
        rec["arm"] = arm
        run = app.call("harness.diagnostics.create", {"packId": a.pack, "taskIds": a.tasks, "armIds": [arm["id"]], "repeats": 1})
        rec["diagnostic"] = run
        g.write_json(out / "harness.json", rec)
        app.call("harness.diagnostics.advance", {"id": run["id"], "maxAttempts": len(a.tasks), "wallSeconds": 7200})
        started = time.time()
        while True:
            cur = app.call("harness.diagnostics.get", {"id": run["id"]})
            st = cur.get("status")
            if st not in ("running", "queued", "pending") or time.time() - started > 2 * 3600:
                break
            time.sleep(10)
        rec["final"] = cur
        rec["elapsedSeconds"] = round(time.time() - started, 1)
        trials = []
        for res in cur.get("results") or []:
            tid = res.get("trialId")
            if tid:
                try:
                    trials.append(app.call("harness.trials.get", {"id": tid}))
                except RuntimeError as e:
                    trials.append({"id": tid, "error": str(e)})
        g.write_json(out / "trials.json", trials)
        try:
            rec["exportArtifact"] = app.call("harness.diagnostics.exportArtifact", {"id": run["id"]})
        except RuntimeError as e:
            rec["exportArtifact"] = {"error": str(e)}
        rec["finishedAt"] = g.now()
        g.write_json(out / "harness.json", rec)
        summary = [{"task": r.get("taskId"), "trialStatus": r.get("trialStatus"), "passed": (r.get("grade") or {}).get("passed"), "checks": [(c.get("name"), c.get("passed")) for c in (r.get("grade") or {}).get("checks", [])], "infra": r.get("infrastructureError"), "seconds": (r.get("durationMs") or 0) / 1000} for r in cur.get("results") or []]
        print(json.dumps({"status": cur.get("status"), "error": cur.get("error"), "results": summary}, indent=1))
        print("record:", out)
    except Exception as e:
        rec["driverError"] = f"{type(e).__name__}: {e}"
        g.write_json(out / "harness.json", rec)
        print(rec["driverError"])
        print("record:", out)
    finally:
        app.close()


if __name__ == "__main__":
    main()
