import importlib.util
import contextlib
import hashlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch


PILOT = Path(__file__).parent / "pilot"
sys.path.insert(0, str(PILOT))
spec = importlib.util.spec_from_file_location("broad_eval", PILOT / "broad_eval.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class BroadEvalTests(unittest.TestCase):
    def test_complete_harness_text_and_unsupported_authority(self):
        content = {"instructions": "retain goal", "toolGuidance": "validate fields", "lessons": ["check coverage"],
                   "skillIds": [], "delegateAgentIds": [], "workflowId": ""}
        self.assertEqual(module.content_instructions(content), "retain goal\nvalidate fields\ncheck coverage")
        for key, value in [("skillIds", ["s"]), ("skillIds", ""), ("delegateAgentIds", ["d"]), ("workflowId", "w"), ("workflowId", 0), ("unexpected", True), ("lessons", [4])]:
            bad = dict(content); bad[key] = value
            with self.assertRaises(ValueError): module.content_instructions(bad)

    def test_inventory_includes_caches_directories_and_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "source.txt").write_text("same")
            self.assertEqual(module.contained(root, "source.txt"), root / "source.txt")
            before = module.raw_inventory(root)
            (root / ".pytest_cache").mkdir()
            (root / ".pytest_cache" / "entry").write_text("cache")
            (root / "empty").mkdir()
            (root / "link").symlink_to(root / "source.txt")
            delta = module.raw_delta(before, module.raw_inventory(root))
            self.assertEqual(delta["addedFiles"], [".pytest_cache/entry", "link"])
            self.assertEqual(delta["addedDirectories"], [".pytest_cache", "empty"])
            self.assertEqual(delta["modifiedFiles"], [])
            with self.assertRaises(ValueError): module.contained(root, "../escape")
            with self.assertRaises(ValueError): module.contained(root, "link")

    def test_sealed_pack_rejects_changed_and_unlisted_private_inputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); task = root / "private/tasks/synthetic"
            (task / "workspace").mkdir(parents=True)
            (task / "workspace/README.md").write_text("Synthetic public input")
            (task / "check.py").write_text("def check(*args): return []\n")
            payload = {"category": "synthetic", "prompt": "Inspect input", "protectedPaths": ["README.md"]}
            (task / "task.json").write_text(json.dumps(payload))
            manifest = {"schema": "gemma-sealed-broad-eval-v1", "tasks": [{"id": "synthetic", "category": "synthetic", "prompt": "Inspect input", "taskRoot": "private/tasks/synthetic", "taskSHA256": module.digest(task / "task.json")}]}
            (root / "manifest.json").write_text(json.dumps(manifest))
            (root / "controls-summary.json").write_text(json.dumps({"passed": True, "manifestSHA256": module.digest(root / "manifest.json")}))
            names = [p for p in (root / "private").rglob("*") if p.is_file()] + [root / "manifest.json", root / "controls-summary.json"]
            (root / "seal.json").write_text(json.dumps({"schema": "gemma-sealed-pack-hashes-v1", "files": {p.relative_to(root).as_posix(): module.digest(p) for p in names}}))
            self.assertEqual(module.validate_pack(root)[1][0]["id"], "synthetic")
            (task / "workspace/README.md").write_text("changed")
            with self.assertRaises(ValueError): module.validate_pack(root)
            (task / "workspace/README.md").write_text("Synthetic public input")
            (root / "private/unlisted.py").write_text("pass\n")
            with self.assertRaises(ValueError): module.validate_pack(root)
            (root / "private/unlisted.py").unlink()
            cache = root / "private/__pycache__"
            cache.mkdir(); (cache / "checker.pyc").write_bytes(b"untrusted cache")
            with self.assertRaises(ValueError): module.validate_pack(root)
            (cache / "checker.pyc").unlink()
            (root / "controls-summary.json").write_text('{"passed":true}')
            with self.assertRaises(ValueError): module.validate_pack(root)

    def test_uncertain_run_aborts_before_another_trial(self):
        class FakeApp:
            closed = False
            def __init__(self, *args): pass
            def call(self, method, params=None):
                if method == "model.refresh": return {"models": [{"name": "fixture"}]}
                return {"id": "synthetic"}
            def close(self): FakeApp.closed = True

        attempts = []
        def uncertain(app, task, cfg, out, cache):
            attempts.append(task["id"])
            case = out / task["id"]
            (case / "original").mkdir(parents=True); (case / "workspace").mkdir()
            return {"task": task["id"], "status": "running", "run": {"runtimeBinding": {"runtimeId": "ollama-default", "model": "fixture", "backend": "ollama"}}, "outcomes": {"modelCompleted": False, "independentlyCorrect": True}}

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); binary = root / "fake-binary"; binary.write_text("synthetic")
            pack = root / "pack"; pack.mkdir()
            for name in ["manifest.json", "seal.json", "controls-summary.json"]: (pack / name).write_text("{}")
            tasks = [{"id": "one", "category": "synthetic"}, {"id": "two", "category": "synthetic"}]
            argv = ["broad_eval", "--binary", str(binary), "--pack", str(pack), "--model", "fixture", "--label", "synthetic", "--out", str(root / "out")]
            with patch.object(sys, "argv", argv), patch.object(module, "validate_pack", return_value=({"id": "synthetic"}, tasks)), patch.object(module.g, "App", FakeApp), patch.object(module.g, "attempt", uncertain), contextlib.redirect_stdout(io.StringIO()):
                code = module.main()
            self.assertEqual(code, 1)
            batch = json.loads((root / "out/batch.json").read_text())
            self.assertEqual(attempts, ["one"])
            self.assertTrue(FakeApp.closed)
            self.assertEqual(batch["status"], "infrastructure_error")
            self.assertEqual(batch["unattemptedTasks"], ["two"])
            self.assertEqual(batch["results"][0]["driverError"], "run_not_quiescent")
            self.assertFalse(batch["results"][0]["outcomes"]["independentlyCorrect"])

    def test_managed_preflight_requires_exact_qualified_route(self):
        snapshot = {"catalog": [{"id": "managed-build"}], "modelCatalog": [{"id": "artifact", "serveName": "fixture-e4b"}],
                    "installed": [{"id": "managed-build"}], "models": [{"id": "artifact"}], "process": {"status": "stopped"},
                    "qualifications": [{"buildId": "managed-build", "modelId": "artifact", "contextTokens": 32768, "status": "qualified", "protocolVersion": 2}]}
        class App:
            def call(self, method):
                self.method = method
                return snapshot
        app = App()
        self.assertEqual(module.managed_preflight(app, "managed-build", "artifact", "fixture-e4b", 32768)["qualification"]["protocolVersion"], 2)
        self.assertEqual(app.method, "runtime.managed.status")
        with self.assertRaises(ValueError): module.managed_preflight(app, "managed-build", "artifact", "fixture-e4b", 65536)
        with self.assertRaises(ValueError): module.managed_preflight(app, "managed-build", "artifact", "other", 32768)
        snapshot["process"]["status"] = "running"
        with self.assertRaises(ValueError): module.managed_preflight(app, "managed-build", "artifact", "fixture-e4b", 32768)
        with self.assertRaises(ValueError): module.verify_run_binding({"run": {"runtimeBinding": {"runtimeId": "ollama-default", "model": "fixture-e4b"}}}, "managed-build", "fixture-e4b")

    def test_managed_route_uses_metadata_and_stops_its_owned_runtime(self):
        calls = []
        class App:
            closed = False
            process = {"status": "stopped"}
            def __init__(self, *args): pass
            def call(self, method, params=None, **kwargs):
                calls.append((method, params))
                if method == "model.refresh": raise AssertionError("managed route used Ollama membership")
                if method == "runtime.managed.status":
                    return {"catalog": [{"id": "managed-build"}], "modelCatalog": [{"id": "artifact", "serveName": "fixture-e4b"}],
                            "installed": [{"id": "managed-build"}], "models": [{"id": "artifact"}], "process": dict(App.process),
                            "qualifications": [{"buildId": "managed-build", "modelId": "artifact", "contextTokens": 32768, "status": "qualified", "protocolVersion": 2}]}
                if method == "runtime.managed.start": App.process = {"status": "running"}; return {"process": dict(App.process)}
                if method == "runtime.managed.stop": App.process = {"status": "stopped"}; return {}
                return {"id": "synthetic"}
            def close(self): App.closed = True
        def attempt(app, task, cfg, out, cache):
            case = out / task["id"]; (case / "original").mkdir(parents=True); (case / "workspace").mkdir()
            return {"task": task["id"], "status": "completed", "outcomes": {"modelCompleted": True, "independentlyCorrect": True},
                    "run": {"runtimeBinding": {"runtimeId": "managed-build", "model": "fixture-e4b", "backend": "omlx"}}}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); (root / ".runtime").mkdir(); binary = root / "binary"; binary.write_text("synthetic")
            pack = root / "pack"; pack.mkdir()
            for name in ["manifest.json", "seal.json", "controls-summary.json"]: (pack / name).write_text("{}")
            argv = ["broad_eval", "--binary", str(binary), "--pack", str(pack), "--model", "fixture-e4b", "--runtime-id", "managed-build", "--managed-model-id", "artifact", "--data-dir", str(root / ".runtime/profile"), "--label", "synthetic", "--out", str(root / "out")]
            with patch.object(sys, "argv", argv), patch.object(module.g, "ROOT", root), patch.object(module, "validate_pack", return_value=({"id": "synthetic"}, [{"id": "one", "category": "synthetic"}])), patch.object(module.g, "App", App), patch.object(module.g, "attempt", attempt), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(module.main(), 0)
            batch = json.loads((root / "out/batch.json").read_text())
            self.assertEqual(batch["runtimeId"], "managed-build")
            self.assertEqual(batch["cfg"]["agent"]["runtimeId"], "managed-build")
            self.assertTrue(batch["managedStopAcknowledged"])
            self.assertTrue(App.closed)
            self.assertEqual(sum(name == "runtime.managed.start" for name, _ in calls), 1)
            self.assertEqual(sum(name == "runtime.managed.stop" for name, _ in calls), 1)
            self.assertFalse(any("qualify" in name or "acquire" in name or "install" in name for name, _ in calls))
            with self.assertRaises(ValueError): module.isolated_data_dir(root / "outside", root / "out")


    def test_admitted_failure_retains_provenance_and_stops_without_payload_in_summary(self):
        for typed, temperature in ((False, None), (True, 0.2), (True, 0)):
            with self.subTest(typed=typed, temperature=temperature), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                binary = root / "fake-binary"; binary.write_text("synthetic")
                pack = root / "pack"; pack.mkdir()
                for name in ("manifest.json", "seal.json", "controls-summary.json"):
                    (pack / name).write_text("{}")
                calls, attempted, saved_agents = [], [], []
                marker = "PRIVATE-SYNTHETIC-EXCEPTION-PAYLOAD"

                class App:
                    closed = False
                    def __init__(self, *args): pass
                    def call(self, method, params=None):
                        calls.append(method)
                        if method == "agent.save": saved_agents.append(dict(params))
                        if method == "model.refresh": return {"models": [{"name": "fixture"}]}
                        return {"id": "synthetic"}
                    def close(self): App.closed = True

                def failed(app, task, cfg, out, cache):
                    attempted.append(task["id"])
                    case = out / task["id"]
                    module.g.write_json(case / "admission.json", {"runId": "admitted-run", "sessionId": "session", "workspaceId": "workspace", "admittedBudgetSeconds": 600, "driverPolicyVersion": module.g.DRIVER_POLICY_VERSION})
                    if typed:
                        raise module.g.AttemptObservationError(marker, {"runId": "admitted-run", "driverPhase": "run.cancel_terminal_observation"})
                    raise RuntimeError(marker)

                tasks = [{"id": "one", "category": "synthetic"}, {"id": "two", "category": "synthetic"}]
                argv = ["broad_eval", "--binary", str(binary), "--pack", str(pack), "--model", "fixture", "--label", "synthetic", "--out", str(root / "out")]
                if temperature is not None:
                    argv += ["--agent", json.dumps({"temperature": temperature})]
                printed = io.StringIO()
                with patch.object(sys, "argv", argv), patch.object(module, "validate_pack", return_value=({"id": "synthetic"}, tasks)), patch.object(module.g, "App", App), patch.object(module.g, "attempt", failed), patch.object(module.g.subprocess, "Popen", side_effect=AssertionError("real subprocess")), patch.object(module.g.request, "urlopen", side_effect=AssertionError("real HTTP")), contextlib.redirect_stdout(printed):
                    code = module.main()
                batch = json.loads((root / "out/batch.json").read_text())
                record = json.loads((root / "out/one/record.json").read_text())
                self.assertEqual(code, 1)
                self.assertEqual(attempted, ["one"])
                self.assertTrue(App.closed)
                self.assertNotIn("run.start", calls)
                self.assertEqual(batch["status"], "infrastructure_error")
                self.assertEqual(batch["unattemptedTasks"], ["two"])
                self.assertEqual(batch["driverPolicyVersion"], module.g.DRIVER_POLICY_VERSION)
                expected_temperature = 1.0 if temperature is None else temperature
                self.assertEqual(saved_agents[0]["temperature"], expected_temperature)
                self.assertEqual(batch["cfg"]["agent"]["temperature"], expected_temperature)
                self.assertEqual(record["runId"], "admitted-run")
                self.assertEqual(record["admittedBudgetSeconds"], 600)
                self.assertEqual(record["driverPhase"], "run.cancel_terminal_observation" if typed else "unknown_after_admission")
                self.assertEqual(record["driverException"]["message"], marker)
                self.assertEqual(record["driverException"]["messageSHA256"], hashlib.sha256(marker.encode()).hexdigest())
                self.assertNotIn(marker, printed.getvalue())
                self.assertNotIn(marker, json.dumps(batch["results"]))
                self.assertNotIn("grade", record)


if __name__ == "__main__": unittest.main()
