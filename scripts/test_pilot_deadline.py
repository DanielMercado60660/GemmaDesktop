import importlib.util
import ast
import contextlib
import io
import sys
from pathlib import Path
import json
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("deadline_pilot", Path(__file__).parent / "pilot/gemma_pilot.py")
pilot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pilot)


class PilotDeadlineTests(unittest.TestCase):
    def test_selected_budget_reaches_admission_and_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = root / "fixture"
            (fixture / "workspace").mkdir(parents=True)
            started = []
            completed = {"id": "run", "status": "completed", "summary": "Answer.", "deadline": "2026-11-05T10:00:00Z", "budgetSeconds": 600}

            class App:
                def call(self, method, params=None):
                    if method == "workspace.add":
                        return {"id": "workspace"}
                    if method == "session.create":
                        return {"id": "session"}
                    if method == "run.start":
                        started.append(dict(params))
                        return completed
                    if method == "state":
                        return {"runs": [completed]}
                    if method == "context.get":
                        return {}
                    raise AssertionError(method)

            cfg = {"agentId": "agent", "model": "local-model", "agent": {}, "settings": {}, "deadlineSeconds": 600}
            task = {"id": "synthetic", "root": fixture, "prompt": "Compute from observed inputs."}
            with patch.object(pilot, "session_pages", return_value={"messages": [], "events": []}), patch.object(pilot, "run_checker", return_value={"passed": True, "checks": []}):
                record = pilot.attempt(App(), task, cfg, root / "results", {})
            self.assertEqual(started[0]["budgetSeconds"], 600)
            self.assertEqual(record["admittedBudgetSeconds"], 600)
            self.assertEqual(record["admittedDeadline"], completed["deadline"])
            self.assertNotIn("driverCancelled", record)


    def deadline_case(self, observation="failed", cancel_rejected=True, allow_wait=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = root / "fixture"
            (fixture / "workspace").mkdir(parents=True)
            (fixture / "workspace/input.txt").write_text("unchanged synthetic input")
            calls, cancelled, waits = [], False, []
            running = {"id": "run", "status": "running", "deadline": "synthetic-deadline", "budgetSeconds": 600}

            class App:
                def call(self, method, params=None):
                    nonlocal cancelled
                    calls.append(method)
                    if method == "workspace.add": return {"id": "workspace"}
                    if method == "session.create": return {"id": "session"}
                    if method == "run.start": return dict(running)
                    if method == "run.cancel":
                        cancelled = True
                        if cancel_rejected: raise RuntimeError("private synthetic cancel rejection")
                        return {}
                    if method == "state":
                        if not cancelled: return {"runs": [dict(running)]}
                        if observation == "read_error": raise RuntimeError("private synthetic state failure")
                        if observation == "missing": return {"runs": []}
                        if observation == "other": return {"runs": [{"id": "other-run", "status": "completed"}]}
                        return {"runs": [dict(running, status=observation, summary="Observed terminal result.")]}
                    if method == "context.get": return {}
                    raise AssertionError(method)

            cfg = {"agentId": "agent", "model": "synthetic-model", "agent": {}, "settings": {}, "deadlineSeconds": 600}
            task = {"id": "synthetic", "root": fixture, "category": "synthetic", "prompt": "Synthetic offline input."}
            # FakeApp is the only API implementation. Neither checker loading,
            # HTTP, App startup nor subprocess execution is permitted here.
            def sleep(seconds):
                if not allow_wait: raise AssertionError("unexpected sleep")
                waits.append(seconds)

            with patch.object(pilot.time, "time", side_effect=[0, 601, 602]), patch.object(pilot.time, "sleep", side_effect=sleep), patch.object(pilot, "session_pages", return_value={"messages": [], "events": []}) as pages, patch.object(pilot, "run_checker", return_value={"passed": False, "checks": []}) as checker, patch.object(pilot.subprocess, "Popen", side_effect=AssertionError("real subprocess")), patch.object(pilot.request, "urlopen", side_effect=AssertionError("real HTTP")):
                caught = None
                try:
                    record = pilot.attempt(App(), task, cfg, root / "results", {})
                except pilot.AttemptObservationError as exc:
                    caught, record = exc, exc.record
                case = root / "results/synthetic"
                result = {"record": record, "error": caught, "calls": calls, "waits": waits,
                          "checkpoint": json.loads((case / "admission.json").read_text()),
                          "finalExists": (case / "final").exists(), "recordExists": (case / "record.json").exists(),
                          "transcriptExists": (case / "transcript.json").exists(),
                          "checkerCalls": checker.call_count, "pageCalls": pages.call_count}
            self.assertEqual(calls.count("run.start"), 1)
            self.assertEqual(calls.count("run.cancel"), 1)
            self.assertEqual(result["checkpoint"]["runId"], "run")
            self.assertEqual(result["checkpoint"]["driverPolicyVersion"], pilot.DRIVER_POLICY_VERSION)
            return result

    def test_cancel_rejection_observes_terminal_run_and_exports_once(self):
        for status in ("failed", "completed", "cancelled"):
            with self.subTest(status=status):
                result = self.deadline_case(status)
                record = result["record"]
                self.assertIsNone(result["error"])
                self.assertEqual(record["status"], status)
                self.assertEqual(record["outcomes"]["modelCompleted"], status == "completed")
                self.assertFalse(record["driverCancellation"]["acknowledged"])
                self.assertEqual(record["driverCancellation"]["observedTerminalStatus"], status)
                self.assertTrue(record["driverCancellation"]["terminalObservedAfterRejection"])
                self.assertTrue(result["finalExists"] and result["recordExists"] and result["transcriptExists"])
                self.assertEqual(result["checkerCalls"], 1)
                self.assertEqual(result["pageCalls"], 1)
                self.assertNotIn("driverError", record)

    def test_cancel_rejection_without_exact_terminal_state_aborts_export(self):
        for observation in ("running", "missing", "other", "read_error"):
            with self.subTest(observation=observation):
                result = self.deadline_case(observation)
                self.assertIsInstance(result["error"], pilot.AttemptObservationError)
                self.assertEqual(result["record"]["driverPhase"], "run.cancel_terminal_observation")
                self.assertEqual(result["record"]["runId"], "run")
                self.assertFalse(result["finalExists"] or result["recordExists"] or result["transcriptExists"])
                self.assertEqual(result["checkerCalls"], 0)
                self.assertEqual(result["pageCalls"], 0)

    def test_ordinary_acknowledged_cancellation_keeps_terminal_export(self):
        result = self.deadline_case("cancelled", cancel_rejected=False)
        self.assertIsNone(result["error"])
        self.assertEqual(result["record"]["status"], "cancelled")
        self.assertTrue(result["record"]["driverCancellation"]["acknowledged"])
        self.assertNotIn("error", result["record"]["driverCancellation"])
        self.assertEqual(result["checkerCalls"], 1)
        self.assertTrue(result["finalExists"] and result["transcriptExists"])


    def test_acknowledged_cancellation_requires_observed_terminal_before_export(self):
        for observation in ("running", "missing", "other", "read_error"):
            with self.subTest(observation=observation):
                result = self.deadline_case(observation, cancel_rejected=False, allow_wait=True)
                self.assertIsInstance(result["error"], pilot.AttemptObservationError)
                record = result["record"]
                self.assertTrue(record["driverCancellation"]["acknowledged"])
                self.assertEqual(record["driverPhase"], "run.cancel_terminal_observation")
                self.assertEqual(record["runId"], "run")
                self.assertFalse(result["finalExists"] or result["recordExists"] or result["transcriptExists"])
                self.assertEqual(result["checkerCalls"], 0)
                self.assertEqual(result["pageCalls"], 0)
                if observation == "read_error":
                    self.assertEqual(result["calls"].count("state"), 2)
                    self.assertEqual(result["waits"], [])
                    self.assertEqual(record["terminalObservation"]["readError"]["class"], "RuntimeError")
                else:
                    self.assertEqual(result["calls"].count("state"), 61)
                    self.assertEqual(result["waits"], [1] * 60)
                    self.assertEqual(record["terminalObservation"], {"observed": observation == "running", "status": "running" if observation == "running" else None})

    def test_standalone_cli_exit_status_cleanup_and_safe_summary(self):
        # Execute the actual __main__ guard with the real main function and
        # fake dependencies. No shell/subprocess, App, HTTP or model is used.
        source = ast.parse(Path(pilot.__file__).read_text())
        entry_test = ast.parse("__name__ == '__main__'", mode="eval").body
        guards = [node for node in source.body if isinstance(node, ast.If)
                  and ast.dump(node.test) == ast.dump(entry_test)]
        self.assertEqual(len(guards), 1)
        entry = compile(ast.Module(body=guards, type_ignores=[]), str(pilot.__file__), "exec")
        for fail, temperature in ((True, None), (False, None), (False, 0.2), (False, 0)):
            with self.subTest(fail=fail, temperature=temperature), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                binary = root / "synthetic-binary"; binary.write_text("no executable model")
                attempted, loaded, saved_agents = [], [], []
                marker = "PRIVATE-SYNTHETIC-CLI-EXCEPTION"

                class App:
                    closed = False
                    def __init__(self, *args): pass
                    def call(self, method, params=None):
                        if method == "settings.save": return {}
                        if method == "agent.save": saved_agents.append(dict(params)); return {"id": "agent"}
                        if method == "model.refresh": return {"models": [{"name": "fixture"}]}
                        raise AssertionError(method)
                    def close(self): App.closed = True

                def load(task_id):
                    loaded.append(task_id)
                    return {"id": task_id, "category": "synthetic", "root": root, "prompt": "Synthetic only."}

                def attempt(app, task, cfg, out, cache):
                    attempted.append(task["id"])
                    case = out / task["id"]
                    pilot.write_json(case / "admission.json", {"runId": "admitted-run", "admittedBudgetSeconds": 600, "driverPolicyVersion": pilot.DRIVER_POLICY_VERSION})
                    if fail:
                        raise pilot.AttemptObservationError(marker, {"runId": "admitted-run", "driverPhase": "run.cancel_terminal_observation"})
                    record = {"task": task["id"], "category": "synthetic", "status": "completed", "outcomes": {"modelCompleted": True, "independentlyCorrect": True}, "grade": {"passed": True, "checks": []}}
                    pilot.write_json(case / "record.json", record)
                    return record

                argv = ["gemma_pilot", "--binary", str(binary), "--tasks", "one", "two", "--model", "fixture", "--label", "synthetic", "--deadline", "600", "--out", str(root / "out")]
                if temperature is not None:
                    argv += ["--agent", json.dumps({"temperature": temperature})]
                printed = io.StringIO()
                with patch.object(sys, "argv", argv), patch.object(pilot, "App", App), patch.object(pilot, "load_task", load), patch.object(pilot, "attempt", attempt), patch.object(pilot.subprocess, "Popen", side_effect=AssertionError("real subprocess")), patch.object(pilot.request, "urlopen", side_effect=AssertionError("real HTTP")), contextlib.redirect_stdout(printed):
                    with self.assertRaises(SystemExit) as stopped:
                        exec(entry, {"__name__": "__main__", "main": pilot.main})
                self.assertEqual(stopped.exception.code, 1 if fail else 0)
                self.assertTrue(App.closed)
                self.assertEqual(attempted, ["one"] if fail else ["one", "two"])
                self.assertEqual(loaded, attempted)
                batch = json.loads((root / "out/batch.json").read_text())
                self.assertEqual(batch["status"], "infrastructure_error" if fail else "completed")
                expected_temperature = 1.0 if temperature is None else temperature
                self.assertEqual(saved_agents[0]["temperature"], expected_temperature)
                self.assertEqual(batch["cfg"]["agent"]["temperature"], expected_temperature)
                self.assertNotIn(marker, printed.getvalue())
                self.assertNotIn(marker, json.dumps(batch["results"]))
                if fail:
                    self.assertEqual(batch["unattemptedTasks"], ["two"])
                    record = json.loads((root / "out/one/record.json").read_text())
                    self.assertEqual(record["runId"], "admitted-run")
                    self.assertEqual(record["driverException"]["message"], marker)


if __name__ == "__main__":
    unittest.main()
