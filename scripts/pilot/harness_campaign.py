#!/usr/bin/env python3
"""Persistent external-Codex supervisor for twenty real upstream k=1 rounds.

No provider/CLI authors are invoked. This process writes immutable public
packet requests and waits for actual Codex author/reviewer response files.
Gemma alone solves the four fresh fixture trials in each round. Existing
application APIs own grading, authority, restart checks and upstream recursion.
"""
from __future__ import annotations

import argparse
from datetime import datetime
import json
import math
from pathlib import Path
import sys
import time

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import gemma_pilot as g
from harness_pilot import IMAGE, TOOLS

TASKS = ["duration-units", "event-join", "literal-spans", "json-pointer", "path-boundary"]
MODEL = "gemma4:26b-mlx"
MODEL_DIGEST = "sha256:f0fc7e0ae4947d382989b1f57db3d098a86b87c5b6e9523b9561ba81a2a64879"
ROUNDS = 20
AUTHOR = "codex:/root/harness_campaign"
REVIEWER = "codex:/root/calculation_explore"
TERMINAL = {"completed", "failed", "cancelled"}


def emit(kind, **values):
    print(json.dumps({"event": kind, "at": g.now(), **values}), flush=True)


def immutable_json(path, value):
    if path.exists():
        if json.loads(path.read_text()) != value:
            raise RuntimeError(f"refusing to replace immutable request {path.name}")
    else:
        g.write_json(path, value)


def opaque_diagnostic(run, initial_arm, selected_arm):
    """No hidden checker names, assertions, errors or expected values escape."""
    labels = {initial_arm: "initial", selected_arm: "selected"}
    results = []
    for raw in run.get("results", []):
        if raw["armId"] not in labels:
            raise ValueError("unexpected final diagnostic arm")
        grade = raw.get("grade") or {}
        checks = grade.get("checks") or []
        infrastructure = bool(raw.get("infrastructureError") or grade.get("error"))
        results.append({"taskId": raw["taskId"], "armId": raw["armId"], "arm": labels[raw["armId"]], "repeat": raw.get("repeat", 0), "trialId": raw.get("trialId"), "trialStatus": raw.get("trialStatus"), "passed": bool(grade.get("passed")) and raw.get("trialStatus") == "completed" and bool(checks) and not infrastructure, "checkCount": len(checks), "failedCount": sum(not bool(c.get("passed")) for c in checks), "infrastructure": infrastructure, "durationMs": raw.get("durationMs"), "tokens": raw.get("tokens"), "accountingComplete": raw.get("accountingComplete"), "outputHash": raw.get("outputHash")})
    return {"id": run["id"], "status": run["status"], "results": results}


def diagnostic_summary(opaque):
    results = opaque["results"]
    identities = {(r["taskId"], r["arm"], r["repeat"]) for r in results}
    expected = {(task, arm, 0) for task in TASKS for arm in ("initial", "selected")}
    if len(results) != len(expected) or identities != expected:
        raise RuntimeError("final diagnostic denominator mismatch")
    return {"results": len(results), "initialSuccesses": sum(r["passed"] for r in results if r["arm"] == "initial"), "selectedSuccesses": sum(r["passed"] for r in results if r["arm"] == "selected"), "infrastructureErrors": sum(r["infrastructure"] for r in results)}


def valid_temperature(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= 2:
        raise ValueError("temperature must be a finite number from 0 to 2")
    return float(value)


class Campaign:
    def __init__(self, binary, out, resume=False, finish_reserve=60, temperature=None):
        if temperature is not None:
            temperature = valid_temperature(temperature)
        self.binary, self.out = binary.resolve(), out.resolve()
        try:
            self.out.relative_to(g.ROOT / ".runtime")
        except ValueError:
            raise ValueError("campaign output must be an isolated .runtime directory") from None
        self.out.mkdir(parents=True, exist_ok=True)
        self.path = self.out / "campaign.json"
        self.app = None
        self.last_notice = None
        if self.path.exists():
            if not resume:
                raise ValueError("existing campaign requires --resume")
            self.state = json.loads(self.path.read_text())
            if self.state["binarySHA256"] != g.sha(self.binary) or self.state["operatorSHA256"] != g.sha(Path(__file__)):
                raise ValueError("frozen binary/operator changed; preserve this campaign")
            try:
                recorded = valid_temperature(self.state["temperature"])
            except (KeyError, ValueError):
                raise ValueError("campaign has no valid frozen temperature; use its retained historical operator") from None
            if temperature is not None and temperature != recorded:
                raise ValueError("explicit temperature conflicts with the frozen campaign")
        else:
            if resume:
                raise ValueError("cannot resume a campaign without its ledger")
            self.state = {"schema": "gemma-harness-campaign-v1", "createdAt": g.now(), "binary": str(self.binary), "binarySHA256": g.sha(self.binary), "operatorSHA256": g.sha(Path(__file__)), "model": MODEL, "modelDigest": MODEL_DIGEST, "packId": "coding-diagnostics-v2", "taskOrder": [TASKS[i % len(TASKS)] for i in range(ROUNDS)], "roundCount": ROUNDS, "plannedOptimizationTrials": ROUNDS * 4, "plannedFinalDiagnosticTrials": len(TASKS) * 2, "finishReserveSeconds": finish_reserve, "temperature": g.DEFAULT_AGENT_TEMPERATURE if temperature is None else temperature, "author": AUTHOR, "reviewer": REVIEWER, "status": "initializing", "rounds": []}
            self.save()

    def save(self):
        self.state["updatedAt"] = g.now()
        g.write_json(self.path, self.state)

    def call(self, method, params=None):
        return self.app.call(method, params, timeout=600)

    def setup(self):
        self.app = g.App(self.binary, self.out / "data", self.out / "app.log")
        g.write_json(self.out / "connection.json", {"addr": self.app.addr, "pid": self.app.proc.pid, "binarySHA256": self.state["binarySHA256"]})
        if "agentId" in self.state:
            return
        self.call("execution.configure", {"image": IMAGE})
        packs = self.call("harness.packs.list")["items"]
        pack = next(p for p in packs if p["id"] == self.state["packId"])
        metadata = {t["id"]: t for t in pack["tasks"]}
        if any(metadata[task]["split"] != "development" or metadata[task]["contract"] != "coding" for task in TASKS):
            raise ValueError("campaign permits the five preselected development coding tasks only")
        profile = self.call("capability.save", {"version": 1, "revision": 0, "name": "Harness campaign Docker", "tools": TOOLS, "skillIds": [], "mcp": [], "delegateAgentIds": [], "executionProfile": "docker", "projectGuidance": False})
        workspace_path = self.out / "workspace"
        workspace_path.mkdir(exist_ok=True)
        workspace = self.call("workspace.add", {"path": str(workspace_path), "name": "Harness campaign development"})
        agent = self.call("agent.save", {"name": "Gemma26B Harness campaign", "instructions": "", "model": MODEL, "maxSteps": 40, "mode": "approval", "thinking": "auto", "maxOutputTokens": 8192, "temperature": self.state["temperature"], "toolProtocol": "native", "finishReserveSeconds": self.state["finishReserveSeconds"], "capabilityProfileId": profile["id"]})
        self.state.update({"agentId": agent["id"], "workspaceId": workspace["id"], "profileId": profile["id"], "packDigest": pack["digest"], "status": "running"})
        self.save()

    def admission(self, task):
        request = {"mode": "upstream", "workspaceId": self.state["workspaceId"], "agentId": self.state["agentId"], "fixturePackId": self.state["packId"], "taskIds": [task], "model": MODEL, "runtimeId": "ollama-default", "contextSize": 32768, "thinking": "auto", "temperature": self.state["temperature"], "maxOutputTokens": 8192, "maxSteps": 40, "toolProtocol": "native", "budgetSeconds": 600}
        if self.state.get("retainedRevisionId"):
            request["baseRevisionId"] = self.state["retainedRevisionId"]
        x = self.call("harness.experiments.start", request)
        spec = x["spec"]
        if spec["modelDigest"] != MODEL_DIGEST or spec["fixtureDigest"] != self.state["packDigest"]:
            raise ValueError("frozen model or fixture identity drift")
        identity_keys = ["mode", "model", "modelDigest", "runtimeVersion", "runtimeIdentity", "contextSize", "agent", "frozenCapability", "executorIdentity", "toolchainVersion", "graderVersion", "fixtureDigest", "appVersion", "solverTools", "approvedSkills", "approvedWorkflows", "frozenAgents", "approvedDelegateIds", "pythonIdentity", "upstreamAdapterHash", "budgetSeconds", "authorityHash", "profileBindingHash", "upstreamRevision", "cachePolicy"]
        conditions = {key: spec.get(key) for key in identity_keys}
        if "frozenConditions" in self.state and conditions != self.state["frozenConditions"]:
            raise ValueError("campaign environment changed between rounds")
        self.state.setdefault("frozenConditions", conditions)
        self.state.setdefault("initialRevisionId", spec["baseRevisionId"])
        self.state.setdefault("retainedRevisionId", spec["baseRevisionId"])
        return x

    def evidence(self, x, row):
        trials, grades = [], []
        for trial_id in x.get("trialIds", []):
            trial = self.call("harness.trials.get", {"id": trial_id})
            trials.append(trial)
            if trial_id not in row["grades"]:
                grade = self.call("harness.trials.gradeDevelopment", {"experimentId": x["id"], "trialId": trial_id})
                row["grades"][trial_id] = grade
                self.save()
                emit("trial_graded", round=row["index"], task=row["taskId"], trial=trial_id, label=trial["label"], modelStatus=trial["status"], grade=grade)
            grade = row["grades"][trial_id]
            if grade.get("infrastructure"):
                raise RuntimeError("opaque development-grading infrastructure: " + grade["infrastructure"])
            grades.append(grade)
        row["trialIds"] = [t["id"] for t in trials]
        return trials, grades

    def response(self, request_id, request):
        path = self.out / "requests" / (request_id + ".json")
        immutable_json(path, request)
        response_path = self.out / "responses" / (request_id + ".json")
        if not response_path.exists():
            if self.last_notice != request_id:
                emit("awaiting_codex", request=str(path), role=request["role"], round=request["round"])
                self.last_notice = request_id
            return None
        return json.loads(response_path.read_text())

    def packet(self, x, row, packet, trials, grades):
        review = packet["stage"] == "reflection"
        request = {"schema": "gemma-harness-packet-request-v1", "round": row["index"], "role": "independent_review" if review else "proposal", "packet": packet, "trials": trials, "grades": grades, "baseRevision": self.call("harness.revisions.get", {"id": row["baseRevisionId"]}), "retentionOptions": [row["baseRevisionId"], packet.get("baseRevisionId")] if review else [], "retentionPolicy": "Retaining a changed candidate requires a completed, non-incomplete final solver answer AND a finished independent passing semantic grade, with no unsupported regression. Baseline is the fallback. Passing both baseline and final once is nonregression evidence, not general improvement."}
        wrapper = self.response(packet["id"], request)
        if wrapper is None:
            return False
        reply = self.remember_response(row, packet, wrapper, trials, grades)
        submitted = self.call("harness.packets.submit", reply)
        self.record_submission(row, packet, submitted)
        self.save()
        emit("packet_submitted", round=row["index"], stage=packet["stage"], packet=packet["id"])
        return True

    def remember_response(self, row, packet, wrapper, trials, grades):
        review = packet["stage"] == "reflection"
        reply = wrapper.get("response", {})
        expected = REVIEWER if review else AUTHOR
        if reply.get("id") != packet["id"] or reply.get("requestHash") != packet["requestHash"] or reply.get("authoredBy") != {"kind": "codex", "identity": expected}:
            raise ValueError("packet response identity mismatch")
        if review:
            retained = wrapper.get("retainRevisionId")
            allowed = [row["baseRevisionId"], packet["baseRevisionId"]]
            if retained not in allowed or (reply.get("verdict") != "approved" and retained != row["baseRevisionId"]):
                raise ValueError("reflection selected an unreviewed revision")
            if retained != row["baseRevisionId"]:
                final = next(t for t in trials if t["label"] == "final")
                grade = next(gd for gd in grades if gd["trialId"] == final["id"])
                if final["status"] != "completed" or final.get("result", {}).get("incomplete") or not grade.get("passed") or not grade.get("gradingFinished") or grade.get("infrastructure"):
                    raise ValueError("candidate retention requires final delivery and independent correctness")
            row["review"] = {"requestId": packet["id"], "reviewer": REVIEWER, "retainRevisionId": retained, "verdict": reply["verdict"], "notes": reply.get("notes", "")}
        return reply

    def record_submission(self, row, packet, submitted):
        row.setdefault("submittedPackets", {})[packet["id"]] = {"stage": packet["stage"], "requestHash": packet["requestHash"], "responseFileSHA256": g.sha(self.out / "responses" / (packet["id"] + ".json")), "revisionId": submitted.get("revisionId")}

    def recover_submissions(self, row, metadata, trials, grades):
        # Submission can commit before this process checkpoints its ledger.
        # Reconstruct only from the actual persisted packet and its original
        # external response file; never invent a replacement reviewer decision.
        for item in metadata:
            if item["status"] != "submitted" or item["id"] in row.get("submittedPackets", {}):
                continue
            packet = self.call("harness.packets.get", {"id": item["id"]})
            path = self.out / "responses" / (packet["id"] + ".json")
            if not path.exists():
                raise RuntimeError("submitted packet has no original external response file")
            wrapper = json.loads(path.read_text())
            reply = wrapper.get("response", {})
            persisted = packet.get("response") or {}
            if any(persisted.get(key) != value for key, value in reply.items() if key != "createdAt"):
                raise RuntimeError("external response differs from immutable submitted packet")
            self.remember_response(row, packet, wrapper, trials, grades)
            self.record_submission(row, packet, packet)
            self.save()

    def terminal_review(self, x, row, trials, grades):
        if row.get("review"):
            return True
        if x["status"] == "completed":
            raise RuntimeError("completed round is missing its original independent reflection")
        request_id = f"round-{row['index']:02d}-terminal-review"
        request = {"schema": "gemma-harness-terminal-review-v1", "round": row["index"], "role": "independent_review", "experimentStatus": x["status"], "trials": trials, "grades": grades, "baseRevision": self.call("harness.revisions.get", {"id": row["baseRevisionId"]}), "retentionOptions": [row["baseRevisionId"]], "instruction": "The upstream round failed. Review all four actual probes and retain the prior base; do not invent an upstream reflection packet or successful candidate."}
        wrapper = self.response(request_id, request)
        if wrapper is None:
            return False
        if wrapper.get("reviewer") != REVIEWER or wrapper.get("retainRevisionId") != row["baseRevisionId"] or wrapper.get("verdict") != "rejected":
            raise ValueError("failed round requires independent baseline retention")
        row["review"] = wrapper
        self.save()
        return True

    def run_round(self, row):
        while True:
            x = self.call("harness.experiments.get", {"id": row["experimentId"]})
            status = x["status"]
            if status == "running":
                time.sleep(5)
                continue
            trials, grades = self.evidence(x, row)
            packets = [p for p in self.call("harness.packets.list")["items"] if p["experimentId"] == x["id"]]
            self.recover_submissions(row, packets, trials, grades)
            if status in TERMINAL:
                if len(trials) != 4 or {t["label"] for t in trials} != {"base", "inv", "dom", "final"}:
                    raise RuntimeError("upstream stopped before four fresh probes; preserve the partial round")
                if not self.terminal_review(x, row, trials, grades):
                    time.sleep(2)
                    continue
                selected = row["review"]["retainRevisionId"]
                if status != "completed" and selected != row["baseRevisionId"]:
                    raise ValueError("failed upstream round cannot supply a retained candidate")
                completed = {t["id"]: t["status"] == "completed" and not t.get("result", {}).get("incomplete") for t in trials}
                row.update({"status": status, "finishedAt": g.now(), "selectedRevisionId": selected, "candidateRevisionId": x.get("candidateId"), "semanticSuccesses": sum(bool(gd["passed"]) for gd in grades), "completedCorrect": sum(bool(gd["passed"]) and gd.get("gradingFinished", False) and not gd.get("infrastructure") and completed[gd["trialId"]] for gd in grades), "modelCompletions": sum(completed.values())})
                self.state["retainedRevisionId"] = selected
                bundle = self.call("harness.experiments.export", {"id": x["id"]})
                g.write_json(self.out / "exports" / f"round-{row['index']:02d}.json", bundle)
                self.save()
                emit("round_completed", round=row["index"], task=row["taskId"], status=status, correct=row["completedCorrect"], semanticPasses=row["semanticSuccesses"], retained=selected)
                return
            pending = [p for p in packets if p["status"] == "awaiting_codex"]
            if pending:
                packet = self.call("harness.packets.get", {"id": pending[0]["id"]})
                self.packet(x, row, packet, trials, grades)
                time.sleep(2)
            elif status in {"ready", "checkpointed", "interrupted", "awaiting_codex"}:
                self.call("harness.experiments.advance", {"id": x["id"]})
                time.sleep(2)
            else:
                raise RuntimeError("unrecognized upstream state " + status)

    def diagnostics(self):
        if "selectedForFinalDiagnostics" not in self.state:
            self.state["selectedForFinalDiagnostics"] = self.state["retainedRevisionId"]
            self.save()
        if "diagnosticId" not in self.state:
            common = {"workspaceId": self.state["workspaceId"], "agentId": self.state["agentId"], "mode": "native", "runtimeId": "ollama-default", "model": MODEL, "contextSize": 32768, "budgetSeconds": 600, "fixturePackId": self.state["packId"], "taskIds": TASKS}
            arms = []
            for label, revision in [("Initial", self.state["initialRevisionId"]), ("Selected", self.state["selectedForFinalDiagnostics"])]:
                arm = self.call("harness.arms.create", common | {"name": label + " frozen development harness", "provenance": "development-campaign:" + self.out.name + ":" + label.lower(), "revisionId": revision})
                arms.append(arm["id"])
            self.state["initialArmId"], self.state["selectedArmId"] = arms
            self.save()
            run = self.call("harness.diagnostics.create", {"packId": self.state["packId"], "taskIds": TASKS, "armIds": arms, "repeats": 1})
            self.state["diagnosticId"] = run["id"]
            self.save()
        while True:
            run = self.call("harness.diagnostics.get", {"id": self.state["diagnosticId"]})
            private = self.out / "private-audit"
            private.mkdir(mode=0o700, exist_ok=True)
            private.chmod(0o700)
            private_path = private / "final-diagnostic-full-reviewer-only.json"
            g.write_json(private_path, run)
            private_path.chmod(0o600)
            opaque = opaque_diagnostic(run, self.state["initialArmId"], self.state["selectedArmId"])
            g.write_json(self.out / "final-diagnostic.json", opaque)
            if run["status"] == "completed":
                self.state["diagnosticSummary"] = diagnostic_summary(opaque)
                self.save()
                return
            if run["status"] in {"pending", "ready", "checkpointed", "interrupted"}:
                self.call("harness.diagnostics.advance", {"id": run["id"], "maxAttempts": 3, "wallSeconds": 7200})
            elif run["status"] not in {"running", "queued"}:
                raise RuntimeError("final diagnostic stopped: " + run["status"])
            time.sleep(5)

    def run(self):
        try:
            self.setup()
            for index, task in enumerate(self.state["taskOrder"], 1):
                if len(self.state["rounds"]) < index:
                    x = self.admission(task)
                    self.state["rounds"].append({"index": index, "taskId": task, "experimentId": x["id"], "baseRevisionId": x["spec"]["baseRevisionId"], "startedAt": g.now(), "status": "active", "grades": {}})
                    self.save()
                    emit("round_started", round=index, task=task, experiment=x["id"])
                row = self.state["rounds"][index - 1]
                if row["status"] == "active":
                    self.run_round(row)
            if sum(len(r["trialIds"]) for r in self.state["rounds"]) != ROUNDS * 4:
                raise RuntimeError("optimization denominator mismatch")
            self.diagnostics()
            self.state.update({"status": "completed", "finishedAt": g.now()})
            self.save()
            emit("campaign_completed", rounds=ROUNDS, optimizationTrials=ROUNDS * 4, diagnosticTrials=len(TASKS) * 2)
        except BaseException as error:
            self.state.update({"status": "attention", "operatorError": type(error).__name__ + ": " + str(error), "stoppedAt": g.now()})
            self.save()
            emit("campaign_attention", error=self.state["operatorError"])
            raise
        finally:
            if self.app:
                self.app.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--out", type=Path, default=g.ROOT / ".runtime" / ("harness-campaign-26b-" + datetime.now().strftime("%Y%m%d-%H%M%S")))
    parser.add_argument("--resume", action="store_true")
    parser.add_argument("--temperature", type=float, help="New campaigns default to 1.0; resume retains the recorded value")
    parser.add_argument("--finish-reserve-seconds", type=int, choices=[0, 60], default=60)
    args = parser.parse_args()
    Campaign(args.binary, args.out, args.resume, args.finish_reserve_seconds, temperature=args.temperature).run()


if __name__ == "__main__":
    main()
