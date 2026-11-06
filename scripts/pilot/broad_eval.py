#!/usr/bin/env python3
"""Run a trusted, locally authored sealed suite through the ordinary app path.

The suite's private checkers are host-side trusted code. Their files are never
copied into a solver workspace. This runner neither edits candidates nor changes
grades. A sealed suite is evaluation material, not a harness proposal source.
"""
from __future__ import annotations

import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import sys

sys.dont_write_bytecode = True
import gemma_pilot as g

TOOLS = ["list_files", "read_file", "search_files", "write_file", "edit_file", "move_file", "make_directory", "run_command", "verify_task"]


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def contained(root: Path, name: str) -> Path:
    if not isinstance(name, str) or Path(name).is_absolute() or ".." in Path(name).parts:
        raise ValueError("suite paths must be relative without parent traversal")
    path = root / name
    path.resolve().relative_to(root.resolve())
    current = root
    for part in Path(name).parts:
        current /= part
        if current.is_symlink():
            raise ValueError("suite paths must not contain symlinks")
    return path


def validate_pack(pack: Path) -> tuple[dict, list[dict]]:
    pack = pack.resolve()
    manifest = json.loads((pack / "manifest.json").read_text())
    if manifest.get("schema") != "gemma-sealed-broad-eval-v1":
        raise ValueError("unsupported sealed suite")
    seal = json.loads((pack / "seal.json").read_text())
    if seal.get("schema") != "gemma-sealed-pack-hashes-v1":
        raise ValueError("missing or invalid suite seal")
    hashes = seal.get("files")
    if not isinstance(hashes, dict) or not hashes:
        raise ValueError("empty suite seal")
    # Unlisted bytecode is also identity drift: -B prevents writes, but Python
    # can otherwise read a stale cache even when the source bytes are sealed.
    actual = {"manifest.json", "controls-summary.json"} | {p.relative_to(pack).as_posix() for p in (pack / "private").rglob("*") if p.is_file()}
    if actual != set(hashes):
        raise ValueError("sealed private file inventory changed")
    for name, expected in hashes.items():
        path = contained(pack, name)
        if not path.is_file() or digest(path) != expected:
            raise ValueError("sealed suite identity changed")
    if hashes.get("manifest.json") != digest(pack / "manifest.json"):
        raise ValueError("public manifest is not sealed")
    controls = json.loads((pack / "controls-summary.json").read_text())
    if controls.get("passed") is not True or controls.get("manifestSHA256") != digest(pack / "manifest.json"):
        raise ValueError("suite controls have not passed for this manifest")
    rows = manifest.get("tasks")
    if not isinstance(rows, list) or not rows:
        raise ValueError("suite has no tasks")
    tasks, seen = [], set()
    for item in rows:
        tid = item.get("id")
        if not isinstance(tid, str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,79}", tid) or tid in seen:
            raise ValueError("invalid or repeated task identity")
        seen.add(tid)
        task_root = contained(pack, item.get("taskRoot"))
        task_path = task_root / "task.json"
        if digest(task_path) != item.get("taskSHA256"):
            raise ValueError("task metadata changed")
        required = [task_path, task_root / "check.py"]
        required += [p for p in (task_root / "workspace").rglob("*") if p.is_file()]
        if not (task_root / "workspace").is_dir():
            raise ValueError("missing task workspace")
        for path in required:
            name = path.relative_to(pack).as_posix()
            if hashes.get(name) != digest(contained(pack, name)):
                raise ValueError("task file is outside the frozen seal")
        task = json.loads(task_path.read_text())
        if task.get("prompt") != item.get("prompt") or task.get("category") != item.get("category"):
            raise ValueError("task and public manifest disagree")
        for rel in task.get("protectedPaths", []):
            if not contained(task_root / "workspace", rel).is_file():
                raise ValueError("protected task file is missing")
        task.update({"id": tid, "root": task_root})
        tasks.append(task)
    return manifest, tasks


def content_instructions(content: dict) -> str:
    if not isinstance(content, dict):
        raise ValueError("harness content must be an object")
    allowed = {"instructions", "toolGuidance", "lessons", "skillIds", "workflowId", "delegateAgentIds"}
    if set(content) - allowed:
        raise ValueError("unsupported harness content field")
    for name in ("skillIds", "delegateAgentIds"):
        value = content.get(name, [])
        if not isinstance(value, list) or value:
            raise ValueError("this ordinary-app evaluation cannot silently drop selected skills or delegates")
    if not isinstance(content.get("workflowId", ""), str) or content.get("workflowId"):
        raise ValueError("this evaluation does not execute harness workflows")
    instructions, guidance, lessons = content.get("instructions", ""), content.get("toolGuidance", ""), content.get("lessons", [])
    if not isinstance(instructions, str) or not isinstance(guidance, str) or not isinstance(lessons, list) or any(not isinstance(s, str) for s in lessons):
        raise ValueError("invalid declarative harness text")
    return "\n".join([instructions, guidance, *lessons])


def raw_inventory(root: Path) -> dict:
    files, directories = {}, []
    for path in sorted(root.rglob("*")):
        name = path.relative_to(root).as_posix()
        if path.is_symlink():
            files[name] = {"type": "symlink", "target": str(path.readlink())}
        elif path.is_file():
            files[name] = {"type": "file", "sha256": digest(path)}
        elif path.is_dir():
            directories.append(name)
        else:
            files[name] = {"type": "other"}
    return {"files": files, "directories": directories}


def raw_delta(before: dict, after: dict) -> dict:
    a, b = before["files"], after["files"]
    return {"addedFiles": sorted(b.keys() - a.keys()), "deletedFiles": sorted(a.keys() - b.keys()),
            "modifiedFiles": sorted(n for n in a.keys() & b.keys() if a[n] != b[n]),
            "addedDirectories": sorted(set(after["directories"]) - set(before["directories"])),
            "deletedDirectories": sorted(set(before["directories"]) - set(after["directories"]))}


def isolated_data_dir(value: Path | None, out: Path) -> Path:
    if value is None:
        return out / "data"
    runtime_root = (g.ROOT / ".runtime").resolve()
    resolved = value.resolve()
    relative = resolved.relative_to(runtime_root)
    if not relative.parts:
        raise ValueError("an explicit isolated data subdirectory is required")
    return contained(runtime_root, relative.as_posix())


def managed_preflight(app, runtime_id: str, model_id: str, model: str, context: int) -> dict:
    snapshot = app.call("runtime.managed.status")
    if snapshot.get("process", {}).get("status") not in (None, "stopped", "failed"):
        raise ValueError("evaluation cannot adopt an already active managed runtime")
    build = next((b for b in snapshot.get("catalog", []) if b.get("id") == runtime_id), None)
    artifact = next((m for m in snapshot.get("modelCatalog", []) if m.get("id") == model_id and m.get("serveName") == model), None)
    receipt = next((q for q in snapshot.get("qualifications", []) if q.get("buildId") == runtime_id and q.get("modelId") == model_id
                    and q.get("contextTokens") == context and q.get("status") == "qualified" and q.get("protocolVersion") == 2), None)
    if build is None or artifact is None or receipt is None:
        raise ValueError("matching managed build/model and exact-context qualification are required")
    if not any(x.get("id") == runtime_id for x in snapshot.get("installed", [])) or not any(x.get("id") == model_id for x in snapshot.get("models", [])):
        raise ValueError("managed build and artifact must already be installed")
    return {"build": build, "artifact": artifact, "qualification": receipt}


def verify_run_binding(record: dict, runtime_id: str, model: str) -> dict:
    binding = (record.get("run") or {}).get("runtimeBinding") or {}
    if binding.get("runtimeId") != runtime_id or binding.get("model") != model:
        raise ValueError("admitted run differs from the explicit runtime/model route")
    if runtime_id != "ollama-default" and binding.get("backend") not in ("omlx", "mlx_serve"):
        raise ValueError("managed evaluation admitted an unexpected backend")
    return binding


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--pack", required=True, type=Path)
    parser.add_argument("--model", required=True)
    parser.add_argument("--runtime-id", default="ollama-default")
    parser.add_argument("--managed-model-id", help="installed managed artifact ID; never acquired by this runner")
    parser.add_argument("--data-dir", type=Path, help="explicit isolated profile under repository .runtime")
    parser.add_argument("--label", required=True)
    parser.add_argument("--content", type=Path, help="frozen declarative Content JSON; no skills/delegates/workflows")
    parser.add_argument("--agent", default="{}", help="same bounded generation controls as agent.save")
    parser.add_argument("--settings", default="{}")
    parser.add_argument("--deadline", type=int, default=600)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    if not 30 <= args.deadline <= 1200:
        raise ValueError("deadline must be 30–1200 seconds")
    manifest, tasks = validate_pack(args.pack)
    overrides = json.loads(args.agent)
    if not isinstance(overrides, dict):
        raise ValueError("agent overrides must be an object")
    if "runtimeId" in overrides and overrides["runtimeId"] != args.runtime_id:
        raise ValueError("agent runtimeId conflicts with explicit runtime choice")
    managed = args.runtime_id != "ollama-default"
    if managed and (not args.managed_model_id or args.data_dir is None):
        raise ValueError("managed evaluation requires explicit model ID and isolated data directory")
    if not managed and args.managed_model_id:
        raise ValueError("managed model ID cannot be combined with Ollama routing")
    if args.content and "instructions" in overrides:
        raise ValueError("instructions cannot override frozen Content")
    instructions = content_instructions(json.loads(args.content.read_text())) if args.content else ""
    frozen_sources = {Path(__file__).resolve(): digest(Path(__file__).resolve()),
                      Path(g.__file__).resolve(): digest(Path(g.__file__).resolve()),
                      Path(__file__).resolve().parent / "checklib.py": digest(Path(__file__).resolve().parent / "checklib.py")}
    if args.content:
        frozen_sources[args.content.resolve()] = digest(args.content.resolve())
    for name in ("manifest.json", "seal.json", "controls-summary.json"):
        path = (args.pack / name).resolve()
        frozen_sources[path] = digest(path)
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    out = args.out or g.ROOT / ".runtime" / f"broad-eval-{args.label}-{stamp}"
    out.mkdir(parents=True, exist_ok=False)
    data_dir = isolated_data_dir(args.data_dir, out)
    app = g.App(args.binary.resolve(), data_dir, out / "app.log")
    meta = {"schema": "gemma-sealed-broad-batch-v1", "label": args.label, "startedAt": g.now(),
            "binarySHA256": digest(args.binary.resolve()), "packId": manifest["id"], "manifestSHA256": digest(args.pack / "manifest.json"),
            "sealSHA256": digest(args.pack / "seal.json"), "contentSHA256": digest(args.content) if args.content else None,
            "runnerSHA256": frozen_sources[Path(__file__).resolve()], "pilotDriverSHA256": frozen_sources[Path(g.__file__).resolve()],
            "driverPolicyVersion": g.DRIVER_POLICY_VERSION,
            "checkerSupportSHA256": frozen_sources[Path(__file__).resolve().parent / "checklib.py"],
            "runtimeId": args.runtime_id, "dataDirectory": str(data_dir),
            "tasks": [t["id"] for t in tasks], "categories": {t["id"]: t["category"] for t in tasks}}
    owned_managed = False
    try:
        settings = {"contextSize": 32768, "defaultModel": args.model} | json.loads(args.settings)
        if "defaultCompletionRuntimeId" in settings and settings["defaultCompletionRuntimeId"] != args.runtime_id:
            raise ValueError("settings conflict with explicit runtime choice")
        settings.update({"defaultModel": args.model, "defaultCompletionRuntimeId": args.runtime_id})
        app.call("settings.save", settings)
        managed_metadata = None
        if managed:
            managed_metadata = managed_preflight(app, args.runtime_id, args.managed_model_id, args.model, settings["contextSize"])
            # From this point a start request is ours, including uncertain HTTP failure.
            owned_managed = True
            meta["managedStart"] = app.call("runtime.managed.start", {"buildId": args.runtime_id, "modelId": args.managed_model_id, "contextTokens": settings["contextSize"]})
        profile = app.call("capability.save", {"version": 1, "revision": 0, "name": "Sealed ordinary-app evaluation", "tools": TOOLS,
                        "skillIds": [], "mcp": [], "delegateAgentIds": [], "executionProfile": "restricted", "projectGuidance": False})
        agent = {"id": "default", "name": "Sealed evaluation", "instructions": instructions, "model": args.model,
                 "maxSteps": 40, "mode": "approval", "thinking": "auto", "maxOutputTokens": 8192, "temperature": g.DEFAULT_AGENT_TEMPERATURE,
                 "toolProtocol": "native"} | overrides
        # Evaluation authority is fixed even if caller-supplied generation fields include another profile.
        agent["capabilityProfileId"] = profile["id"]
        agent["model"] = args.model
        agent["runtimeId"] = args.runtime_id
        saved = app.call("agent.save", agent)
        if managed:
            model = {"name": args.model, "managedModelId": args.managed_model_id, "metadata": managed_metadata["artifact"]}
            runtime = {"runtimeId": args.runtime_id, "metadata": managed_metadata["build"], "qualification": managed_metadata["qualification"]}
        else:
            state = app.call("model.refresh")
            model = next((m for m in state.get("models", []) if m.get("name") == args.model), None)
            if model is None:
                raise ValueError("requested local model is unavailable; no download or fallback")
            runtime = state.get("runtime")
        cfg = {"agentId": saved["id"], "agent": agent, "settings": settings, "model": args.model,
               "deadlineSeconds": args.deadline, "useContract": True}
        meta.update({"model": model, "runtime": runtime, "cfg": cfg, "profile": profile})
        g.write_json(out / "batch.json", meta)
        results = []
        for task in tasks:
            if any(digest(path) != expected for path, expected in frozen_sources.items()):
                raise ValueError("runner, support code or selected harness changed during evaluation")
            validate_pack(args.pack)  # Never continue after an unexpected checker/fixture mutation.
            print(json.dumps({"task": task["id"], "state": "starting"}), flush=True)
            record = {}
            try:
                record = g.attempt(app, task, cfg, out, {})
                record["verifiedRuntimeBinding"] = verify_run_binding(record, args.runtime_id, args.model)
                if record.get("status") not in g.FINAL:
                    record["grade"] = {"passed": False, "infrastructureError": "run_not_quiescent", "checks": []}
                    record["outcomes"]["independentlyCorrect"] = False
                    record["driverError"] = "run_not_quiescent"
                case = out / task["id"]
                record["rawWorkspaceDelta"] = raw_delta(raw_inventory(case / "original"), raw_inventory(case / "workspace"))
                g.write_json(case / "record.json", record)
            except Exception as exc:
                record = g.attempt_failure(task, out / task["id"], exc, record)
                g.write_json(out / task["id"] / "record.json", record)
            summary = {k: record.get(k) for k in ("task", "category", "status", "elapsedSeconds", "outcomes", "driverError", "driverPhase", "driverCancelled", "rawWorkspaceDelta")}
            results.append(summary)
            print(json.dumps(summary), flush=True)
            meta["results"] = results
            g.write_json(out / "batch.json", meta)
            if record.get("driverError"):
                meta["status"] = "infrastructure_error"
                meta["unattemptedTasks"] = [t["id"] for t in tasks[len(results):]]
                break
        if any(digest(path) != expected for path, expected in frozen_sources.items()):
            raise ValueError("frozen evaluation identity changed before completion")
        validate_pack(args.pack)
        meta.setdefault("status", "completed")
        meta["finishedAt"] = g.now()
        g.write_json(out / "batch.json", meta)
    except Exception as exc:
        meta.update({"status": "infrastructure_error", "driverError": type(exc).__name__, "finishedAt": g.now()})
        meta["unattemptedTasks"] = meta["tasks"][len(meta.get("results", [])):]
        g.write_json(out / "batch.json", meta)
        raise
    finally:
        if owned_managed:
            try:
                app.call("runtime.managed.stop", {}, timeout=30)
                stopped = app.call("runtime.managed.status")
                meta["managedStopAcknowledged"] = stopped.get("process", {}).get("status") == "stopped"
                if not meta["managedStopAcknowledged"]:
                    meta["status"] = "infrastructure_error"
            except Exception as exc:
                meta.update({"managedStopAcknowledged": False, "managedStopError": type(exc).__name__, "status": "infrastructure_error"})
            g.write_json(out / "batch.json", meta)
        app.close()
    return 0 if meta.get("status") == "completed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
