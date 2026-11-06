#!/usr/bin/env python3
"""Drive real Gemma task attempts through the ordinary app path, headlessly.

Each batch starts its own app instance against a fresh, isolated data directory
(never the personal one), configures one agent, runs each task in a disposable
copy of its fixture, acts as an approving user, and records everything needed
for review: prompt, settings, model identity, transcript, tool receipts, final
files, a diff and independent checks the model never sees.

Grades come only from the task's own checker run on the frozen final files.
The model's claims, the run status and host verification are recorded
separately and never substitute for that check.
"""
from __future__ import annotations

import argparse
import difflib
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
from datetime import datetime, timezone
from urllib import error, request

ROOT = Path(__file__).resolve().parents[2]
TASKS = Path(__file__).resolve().parent / "tasks"
FINAL = {"completed", "failed", "cancelled", "interrupted"}
SKIP_DIRS = {"__pycache__", ".pytest_cache", ".git"}
DRIVER_POLICY_VERSION = "terminal-cancel-observation-v2"
# Gemma 4 publisher starting point; explicit experiment overrides remain authoritative.
DEFAULT_AGENT_TEMPERATURE = 1.0


def now():
    return datetime.now(timezone.utc).isoformat()


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(root: Path) -> dict[str, str]:
    out = {}
    for p in sorted(root.rglob("*")):
        if p.is_file() and not (set(p.relative_to(root).parts) & SKIP_DIRS):
            out[str(p.relative_to(root))] = sha(p)
    return out


def write_json(path: Path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(value, indent=2, ensure_ascii=False, default=str) + "\n", encoding="utf-8")
    tmp.replace(path)


class AttemptObservationError(RuntimeError):
    """An admitted attempt whose terminal state could not be observed safely."""

    def __init__(self, message: str, record: dict):
        super().__init__(message)
        self.record = dict(record)


def exception_detail(exc: Exception) -> dict:
    # Private evidence only; safe batch summaries never include this payload.
    message = str(exc)
    return {"class": type(exc).__name__, "message": message[:2000],
            "messageSHA256": hashlib.sha256(message.encode()).hexdigest()}


def attempt_failure(task: dict, case: Path, exc: Exception, record: dict | None = None) -> dict:
    """Retain admission/error provenance without guessing an unknown API phase."""
    admitted = {}
    try:
        checkpoint = json.loads((case / "admission.json").read_text())
        if isinstance(checkpoint, dict):
            admitted = {k: checkpoint[k] for k in ("task", "category", "model", "startedAt", "runId", "sessionId", "workspaceId",
                        "admittedDeadline", "admittedBudgetSeconds", "driverPolicyVersion") if k in checkpoint}
    except (OSError, ValueError):
        pass
    failure = admitted | dict(record or {})
    if isinstance(exc, AttemptObservationError):
        failure.update(exc.record)
    failure.update({"task": task["id"], "category": task.get("category"), "driverError": type(exc).__name__})
    failure.setdefault("driverPhase", "unknown_after_admission" if failure.get("runId") else "admission_unconfirmed")
    failure["driverException"] = exception_detail(exc)
    if exc.__cause__ is not None:
        failure["driverException"]["cause"] = exception_detail(exc.__cause__)
    if "outcomes" in failure:
        failure["outcomes"] = dict(failure["outcomes"], independentlyCorrect=False)
    return failure


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class App:
    """One isolated app instance reached through its loopback development API."""

    def __init__(self, binary: Path, data_dir: Path, log_path: Path):
        self.addr = f"127.0.0.1:{free_port()}"
        data_dir.mkdir(parents=True, exist_ok=True)
        self.log = open(log_path, "ab")
        self.proc = subprocess.Popen([str(binary), "-serve", self.addr, "-data-dir", str(data_dir)], stdout=self.log, stderr=subprocess.STDOUT)
        deadline = time.time() + 60
        while True:
            try:
                self.call("state.changes", {"after": 0, "limit": 1})
                return
            except Exception:
                if self.proc.poll() is not None or time.time() > deadline:
                    raise RuntimeError(f"app did not start; see {log_path}")
                time.sleep(0.5)

    def call(self, method: str, params: dict | None = None, timeout: float = 120):
        body = json.dumps({"method": method, "params": params or {}}).encode()
        req = request.Request(f"http://{self.addr}/api/call", data=body, method="POST", headers={"Content-Type": "application/json", "Host": self.addr})
        try:
            with request.urlopen(req, timeout=timeout) as r:
                return json.loads(r.read() or b"null")
        except error.HTTPError as e:
            raise RuntimeError(f"{method}: {e.read().decode(errors='replace')[:2000]}") from None

    def close(self):
        if self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(20)
            except subprocess.TimeoutExpired:
                self.proc.kill()
        self.log.close()


# Held-out tasks live apart so they are never browsed or tuned against by default.
HELDOUT = Path(__file__).resolve().parent / "tasks-heldout"


def load_task(task_id: str) -> dict:
    root = TASKS / task_id
    if not root.is_dir() and (HELDOUT / task_id).is_dir():
        root = HELDOUT / task_id
    task = json.loads((root / "task.json").read_text())
    task["id"] = task_id
    task["root"] = root
    return task


def run_checker(task: dict, final: Path, original: dict, record: dict) -> dict:
    """Load the task's private checker and grade the frozen final files."""
    spec = importlib.util.spec_from_file_location(f"check_{task['id'].replace('-', '_')}", task["root"] / "check.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    try:
        checks = module.check(final, original, record)
    except Exception as e:  # a crashing checker is an infrastructure failure, never a pass
        return {"passed": False, "infrastructureError": f"{type(e).__name__}: {e}", "checks": []}
    # Advisory checks (critical=False) are reported but do not decide the grade.
    return {"passed": bool(checks) and all(c["passed"] for c in checks if c.get("critical", True)), "checks": checks}


def text_diff(before: Path, after: Path) -> str:
    chunks = []
    names = sorted(set(tree(before)) | set(tree(after)))
    for name in names:
        a, b = before / name, after / name
        try:
            at = a.read_text().splitlines(keepends=True) if a.exists() else []
            bt = b.read_text().splitlines(keepends=True) if b.exists() else []
        except UnicodeDecodeError:
            continue
        if at != bt:
            chunks.extend(difflib.unified_diff(at, bt, "a/" + name, "b/" + name))
    return "".join(chunks)


def session_pages(app: App, session_id: str) -> dict:
    after, events, messages = 0, [], []
    for _ in range(200):
        page = app.call("session.get", {"id": session_id, "after": after, "limit": 1000})
        events.extend(page.get("events") or [])
        if not messages:
            messages = page.get("messages") or []
        after = page.get("eventCursor") or after
        if not page.get("hasMore"):
            break
    return {"messages": messages, "events": events}


def find(snapshot: dict, kind: str, ident: str):
    return next((d for d in snapshot.get(kind) or [] if d.get("id") == ident), None)


def attempt(app: App, task: dict, cfg: dict, out: Path, workspace_id_cache: dict) -> dict:
    case = out / task["id"]
    if case.exists():
        raise RuntimeError(f"refusing to overwrite {case}")
    workspace = case / "workspace"
    shutil.copytree(task["root"] / "workspace", workspace, ignore=shutil.ignore_patterns(*SKIP_DIRS))
    pristine = case / "original"
    shutil.copytree(workspace, pristine)
    original = tree(workspace)
    for rel in task.get("protectedPaths", []):
        (workspace / rel).chmod(0o444)

    ws = app.call("workspace.add", {"path": str(workspace), "name": task["id"]})
    session = app.call("session.create", {"workspaceId": ws["id"], "title": task["id"]})
    params = {"sessionId": session["id"], "agentId": cfg["agentId"], "model": cfg["model"], "prompt": task["prompt"], "budgetSeconds": cfg["deadlineSeconds"]}
    if task.get("verificationContract") and cfg.get("useContract", True):
        params["verificationContract"] = task["verificationContract"]
    record = {"task": task["id"], "category": task.get("category"), "prompt": task["prompt"], "startedAt": now(), "model": cfg["model"], "agent": cfg["agent"], "settings": cfg["settings"], "verificationContract": params.get("verificationContract"), "approvals": []}
    started = time.time()
    run = app.call("run.start", params)
    run_id = run["id"]
    record["runId"] = run_id
    record["admittedDeadline"] = run.get("deadline")
    record["admittedBudgetSeconds"] = run.get("budgetSeconds")
    record.update({"sessionId": session["id"], "workspaceId": ws["id"], "driverPolicyVersion": DRIVER_POLICY_VERSION})
    admitted = {k: record[k] for k in ("task", "category", "model", "startedAt", "runId", "sessionId", "workspaceId",
                "admittedDeadline", "admittedBudgetSeconds", "driverPolicyVersion")}
    try:
        write_json(case / "admission.json", admitted)
    except OSError as exc:
        raise AttemptObservationError("admission checkpoint could not be saved", dict(record, driverPhase="admission_checkpoint")) from exc
    deadline = started + cfg["deadlineSeconds"]
    answered = set()
    status = run.get("status")
    while True:
        snap = app.call("state")
        run = find(snap, "runs", run_id) or run
        status = run.get("status")
        for ap in snap.get("approvals") or []:
            if ap.get("runId") != run_id or ap.get("status") != "pending" or ap["id"] in answered:
                continue
            allow, reason = decide(ap, task)
            try:
                app.call("approval.respond", {"id": ap["id"], "allow": allow})
            except RuntimeError as e:
                reason += f" (respond failed: {e})"
            answered.add(ap["id"])
            record["approvals"].append({"tool": ap.get("tool"), "arguments": ap.get("arguments"), "allowed": allow, "reason": reason, "at": round(time.time() - started, 1)})
        if status in FINAL:
            break
        if time.time() > deadline:
            record["driverCancelled"] = f"deadline {cfg['deadlineSeconds']}s"
            record["driverCancellation"] = {"requested": True, "acknowledged": False}
            try:
                app.call("run.cancel", {"id": run_id})
            except RuntimeError as exc:
                record["driverCancellation"]["error"] = exception_detail(exc)
                record["driverPhase"] = "run.cancel_terminal_observation"
                try:
                    observed = find(app.call("state"), "runs", run_id)
                except Exception as state_exc:
                    record["terminalObservation"] = {"observed": False, "readError": exception_detail(state_exc)}
                    raise AttemptObservationError("cancellation rejected and terminal state could not be read", record) from state_exc
                record["terminalObservation"] = {"observed": observed is not None, "status": observed.get("status") if observed else None}
                if observed is None or observed.get("status") not in FINAL:
                    raise AttemptObservationError("cancellation rejected without an observed terminal run", record) from None
                run = observed
                record["driverCancellation"].update({"terminalObservedAfterRejection": True, "observedTerminalStatus": run["status"]})
                record.pop("driverPhase", None)
            else:
                record["driverCancellation"]["acknowledged"] = True
                observed = None
                for _ in range(60):
                    try:
                        observed = find(app.call("state"), "runs", run_id)
                    except Exception as state_exc:
                        record["driverPhase"] = "run.cancel_terminal_observation"
                        record["terminalObservation"] = {"observed": False, "readError": exception_detail(state_exc)}
                        raise AttemptObservationError("acknowledged cancellation terminal state could not be read", record) from state_exc
                    if observed is not None and observed.get("status") in FINAL:
                        run = observed
                        record["terminalObservation"] = {"observed": True, "status": observed["status"]}
                        break
                    time.sleep(1)
                else:
                    record["driverPhase"] = "run.cancel_terminal_observation"
                    record["terminalObservation"] = {"observed": observed is not None, "status": observed.get("status") if observed else None}
                    raise AttemptObservationError("acknowledged cancellation without an observed terminal run", record)
            break
        time.sleep(1)
    record["elapsedSeconds"] = round(time.time() - started, 1)
    record["finishedAt"] = now()
    record["status"] = run.get("status")
    record["run"] = run
    final = case / "final"
    shutil.copytree(workspace, final, ignore=shutil.ignore_patterns(*SKIP_DIRS))
    for p in final.rglob("*"):
        if p.is_file():
            p.chmod(0o644)
    pages = session_pages(app, session["id"])
    try:
        record["context"] = app.call("context.get", {"runId": run_id})
    except RuntimeError as e:
        record["context"] = {"error": str(e)}
    write_json(case / "transcript.json", pages)
    record["toolCalls"] = summarize_tools(pages["events"])
    record["finalAnswer"] = run.get("summary")
    record["diff"] = text_diff(pristine, final)
    (case / "diff.patch").write_text(record["diff"])
    record["filesChanged"] = sorted(k for k in set(original) | set(tree(final)) if original.get(k) != tree(final).get(k))
    record["grade"] = run_checker(task, final, original, record)
    record["outcomes"] = {
        "modelCompleted": record["status"] == "completed" and not run.get("incomplete"),
        "hostVerification": run.get("verification"),
        "independentlyCorrect": record["grade"]["passed"],
        "protectedInputsPreserved": all(original.get(p) == tree(final).get(p) for p in task.get("protectedPaths", [])),
    }
    write_json(case / "record.json", record)
    return record


def decide(ap: dict, task: dict) -> tuple[bool, str]:
    """A cooperative user: allow governed actions inside the sandbox, refuse protected edits."""
    args = ap.get("arguments") or {}
    path = args.get("path") or args.get("destination") or ""
    if path and any(path == p or path.lstrip("./") == p for p in task.get("protectedPaths", [])):
        return False, "protected path"
    return True, "approved by cooperative-user policy"


def summarize_tools(events: list) -> dict:
    calls, errors, names, failures, kinds = 0, 0, {}, [], {}
    validation_errors, validation_failures = 0, []
    for ev in events:
        t = ev.get("type") or ""
        kinds[t] = kinds.get(t, 0) + 1
        name = ev.get("tool") or ev.get("title")
        if t == "tool_started":
            calls += 1
            names[name] = names.get(name, 0) + 1
        elif t == "tool_failed":
            errors += 1
            failures.append({"tool": name, "detail": (ev.get("detail") or "")[:400]})
        elif t == "tool_validation_failed":
            validation_errors += 1
            validation_failures.append({"tool": name, "detail": (ev.get("detail") or "")[:400], "executed": False})
    return {"calls": calls, "errors": errors, "byTool": names, "failures": failures[:40], "validationErrors": validation_errors, "validationFailures": validation_failures[:40], "eventKinds": kinds}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--binary", required=True, type=Path)
    ap.add_argument("--tasks", nargs="+", required=True)
    ap.add_argument("--model", default="gemma4:26b-mlx")
    ap.add_argument("--label", default="pilot")
    ap.add_argument("--agent", default="{}", help="JSON overrides for agent.save")
    ap.add_argument("--settings", default="{}", help="JSON overrides for settings.save")
    ap.add_argument("--profile", default="", help="JSON capability.save body (optional)")
    ap.add_argument("--no-contract", action="store_true")
    ap.add_argument("--deadline", type=int, default=1200)
    ap.add_argument("--out", type=Path)
    a = ap.parse_args()
    if not 30 <= a.deadline <= 1200:
        ap.error("--deadline must be from 30 to 1200 seconds, matching run admission")

    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    out = a.out or ROOT / ".runtime" / f"pilot-{a.label}-{stamp}"
    out.mkdir(parents=True, exist_ok=False)
    binary = a.binary.resolve()
    app = App(binary, out / "data", out / "app.log")
    try:
        settings = {"contextSize": 32768, "defaultModel": a.model} | json.loads(a.settings)
        app.call("settings.save", settings)
        agent = {"id": "default", "name": "Gemma", "instructions": "", "model": a.model, "maxSteps": 40, "mode": "approval", "thinking": "auto", "maxOutputTokens": 8192, "temperature": DEFAULT_AGENT_TEMPERATURE, "toolProtocol": "native", "capabilityProfileId": "legacy"} | json.loads(a.agent)
        if a.profile:
            prof = app.call("capability.save", json.loads(a.profile))
            agent["capabilityProfileId"] = prof["id"]
        saved = app.call("agent.save", agent)
        snap = app.call("model.refresh")
        model = next((m for m in snap.get("models") or [] if m.get("name") == a.model), None)
        cfg = {"agentId": saved.get("id", "default"), "agent": agent, "settings": settings, "model": a.model, "deadlineSeconds": a.deadline, "useContract": not a.no_contract}
        meta = {"label": a.label, "startedAt": now(), "binary": str(binary), "binarySHA256": sha(binary), "model": model, "runtime": snap.get("runtime"), "cfg": cfg, "tasks": a.tasks, "python": sys.version, "driverPolicyVersion": DRIVER_POLICY_VERSION}
        write_json(out / "batch.json", meta)
        results = []
        for tid in a.tasks:
            task = load_task(tid)
            print(f"[{now()}] {tid}: starting", flush=True)
            try:
                rec = attempt(app, task, cfg, out, {})
            except Exception as e:
                rec = attempt_failure(task, out / tid, e)
                write_json(out / tid / "record.json", rec)
            results.append({k: rec.get(k) for k in ("task", "category", "status", "elapsedSeconds", "outcomes", "driverError", "driverPhase", "driverCancelled")} | {"checks": [(c["name"], c["passed"]) for c in (rec.get("grade") or {}).get("checks", [])], "toolCalls": (rec.get("toolCalls") or {}).get("calls")})
            print(json.dumps(results[-1]), flush=True)
            if rec.get("driverError"):
                meta["status"] = "infrastructure_error"
                meta["unattemptedTasks"] = a.tasks[len(results):]
                break
        meta.setdefault("status", "completed")
        meta["finishedAt"] = now()
        meta["results"] = results
        write_json(out / "batch.json", meta)
        print(f"batch record: {out / 'batch.json'}")
    finally:
        app.close()
    return 0 if meta.get("status") == "completed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
