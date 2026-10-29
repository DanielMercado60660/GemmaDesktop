"""Continuation policy/report fixtures. No app API, model, or candidate execution."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("realistic_e2e", Path(__file__).with_name("realistic-e2e.py"))
harness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(harness)


class ContinuationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / ".runtime").mkdir()
        self.patches = [patch.object(harness, "ROOT", self.root), patch.object(harness, "EVIDENCE", self.root / "reports")]
        for mocked in self.patches:
            mocked.start()
            self.addCleanup(mocked.stop)
        self.manifest, _ = harness.prepare(str(Path(sys.executable).resolve()))
        self.workspace = Path(self.manifest["workspace"])
        with (self.workspace / "tracker/service.py").open("a") as stream:
            stream.write("\n# Previously observed model edit\n")
        self.agent = {"id":"agent-1", "model":"local-gemma", "mode":"approval", "maxSteps":80, "instructions":"Frozen instructions"}
        self.registered = {"id":"workspace-1", "path":str(self.workspace)}
        self.session = {"id":"session-1", "workspaceId":"workspace-1"}
        self.previous_run = {"id":"run-1", "workspaceId":"workspace-1", "sessionId":"session-1", "model":"local-gemma", "status":"cancelled"}
        self.report = {"version":harness.CHALLENGE_VERSION, "manifest":self.manifest, "agent":self.agent,
                       "workspace":self.registered, "session":self.session, "run":self.previous_run,
                       "diffs":harness.diffs(self.manifest), "passed":False, "codingPassed":False}
        self.report_path = self.root / "initial-report.json"
        harness.atomic_json(self.report_path, self.report)
        self.prompt_path = self.root / "FOLLOWUP.txt"
        self.prompt_path.write_text("Continue the task, preserve the original scope, and run the prescribed tests.")

    def load(self):
        return harness.load_continuation(self.report_path, self.prompt_path)

    def test_authorized_prior_changes_and_policy_are_preserved(self):
        context = self.load()
        self.assertEqual(context["sha256"], harness.digest(self.report_path))
        self.assertEqual(context["report"]["manifest"], self.manifest)
        exact = {"tool":"run_command", "arguments":{"executable":self.manifest["python"], "args":harness.TEST_ARGS, "readOnly":True}}
        self.assertTrue(harness.decision(self.manifest, exact)[0])
        exact["arguments"]["args"] = harness.TEST_ARGS + ["--extra"]
        self.assertFalse(harness.decision(self.manifest, exact)[0])
        self.assertFalse(harness.decision(self.manifest, {"tool":"write_file", "arguments":{"path":"tracker/service.py"}}, continuation=True)[0])

    def test_unrecorded_source_changes_are_rejected(self):
        with (self.workspace / "tracker/service.py").open("a") as stream:
            stream.write("# Unrecorded edit\n")
        with self.assertRaisesRegex(ValueError, "changed after the parent report"):
            self.load()

    def test_protected_and_git_changes_are_rejected(self):
        path = self.workspace / "README.md"
        path.chmod(0o644)
        path.write_text("tampered")
        with self.assertRaisesRegex(ValueError, "protected file changed"):
            self.load()
        path.write_bytes((harness.TEMPLATE / "README.md").read_bytes())
        (self.workspace / ".git/config").write_text("changed")
        with self.assertRaisesRegex(ValueError, "Frozen Git metadata changed"):
            self.load()

    def test_frozen_manifest_and_acceptance_changes_are_rejected(self):
        frozen = Path(self.manifest["case"]) / "manifest.json"
        frozen.write_text("{}")
        with self.assertRaisesRegex(ValueError, "Frozen manifest differs"):
            self.load()
        harness.atomic_json(frozen, self.manifest)
        with patch.object(harness, "FIXTURE", self.root):
            (self.root / "acceptance_test.py").write_text("changed acceptance")
            with self.assertRaisesRegex(ValueError, "Acceptance suite changed"):
                self.load()

    def test_wrong_profile_or_agent_config_is_rejected(self):
        context = self.load()
        state = {"workspaces":[self.registered], "sessions":[self.session], "agents":[self.agent]}
        harness.verify_continuation_state(state, context)
        state["agents"] = [{**self.agent, "mode":"read-only"}]
        with self.assertRaisesRegex(ValueError, "agent configuration changed"):
            harness.verify_continuation_state(state, context)
        state["workspaces"] = []
        with self.assertRaisesRegex(ValueError, "absent from this app profile"):
            harness.verify_continuation_state(state, context)

    def test_pass_requires_receipt_discovered_tests_and_nine_acceptance_cases(self):
        (self.workspace / harness.NEW_TEST).write_text("# fixture only; not executed")
        command = {"callId":"test-command", "status":"completed", "exitCode":0, "executable":self.manifest["python"], "args":harness.TEST_ARGS, "readOnly":True, "workingDirectory":str(self.workspace)}
        current = {"id":"run-2", "status":"completed", "receipt":{"commands":[command]}}
        events = [{"runId":"run-2", "seq":10, "type":"tool_completed", "tool":"write_file"},
                  {"runId":"run-2", "seq":11, "type":"tool_started", "data":{"id":"test-command", "name":"run_command"}}]
        public, acceptance = {"exitCode":0, "testCount":6}, {"exitCode":0, "testCount":9}
        self.assertTrue(harness.verification_result(self.manifest, current, public, acceptance, [], events)[1])
        self.assertFalse(harness.verification_result(self.manifest, current, public, acceptance, [])[1])
        self.assertFalse(harness.verification_result(self.manifest, current, public, acceptance, [], events + [{"runId":"run-2", "seq":12, "type":"tool_completed", "tool":"edit_file"}])[1])
        wrong_run = [{**event, "runId":"other-run"} for event in events]
        self.assertFalse(harness.verification_result(self.manifest, current, public, acceptance, [], wrong_run)[1])
        for run, visible, hidden in [(current, {**public, "testCount":5}, acceptance),
                                     (current, public, {**acceptance, "testCount":8}),
                                     ({"status":"completed", "summary":"All tests passed"}, public, acceptance),
                                     ({"status":"completed", "receipt":{"commands":[{**command, "exitCode":1}]}}, public, acceptance)]:
            self.assertFalse(harness.verification_result(self.manifest, run, visible, hidden, [], events)[1])

    def test_model_overrides_are_explicit_and_do_not_change_permissions_or_parent(self):
        self.assertEqual(harness.agent_model_overrides(SimpleNamespace(), self.agent), self.agent)
        updated = harness.agent_model_overrides(SimpleNamespace(thinking="off", max_output_tokens=8192), self.agent)
        self.assertEqual(updated, {**self.agent, "thinking":"off", "maxOutputTokens":8192})
        self.assertNotIn("thinking", self.agent)
        for limit in (0, 255, 32769, True):
            with self.assertRaises(ValueError):
                harness.agent_model_overrides(SimpleNamespace(max_output_tokens=limit), self.agent)

    def verification_fixture(self):
        (self.workspace / harness.NEW_TEST).write_text("# Test fixture; never executed")
        command = {"callId":"check-1", "status":"completed", "exitCode":0,
                   "executable":self.manifest["python"], "args":harness.TEST_ARGS, "readOnly":True}
        current = {"id":"run-2", "status":"completed", "receipt":{"commands":[command]}}
        event = {"runId":"run-2", "seq":11, "type":"tool_started", "data":{"id":"check-1", "name":"run_command", "arguments":{"executable":self.manifest["python"], "args":harness.TEST_ARGS, "readOnly":True}}}
        events = [{"runId":"run-2", "seq":10, "type":"tool_completed", "tool":"edit_file"}, event]
        return command, current, events

    def verify(self, current, events):
        return harness.verification_result(self.manifest, current, {"exitCode":0, "testCount":6}, {"exitCode":0, "testCount":9}, [], events)

    def test_later_failed_check_supersedes_success_and_fresh_success_can_recover(self):
        command, current, events = self.verification_fixture()
        failed = {**command, "callId":"check-2", "status":"failed", "exitCode":1}
        # Receipt array order is not execution order.
        current["receipt"]["commands"] = [failed, command]
        events.append({**events[-1], "seq":12, "data":{**events[-1]["data"], "id":"check-2"}})
        result, passed = self.verify(current, list(reversed(events)))
        self.assertFalse(passed)
        self.assertEqual(result["latestPrescribedAttempt"]["callId"], "check-2")
        recovered = {**command, "callId":"check-3"}
        current["receipt"]["commands"].insert(0, recovered)
        events.append({**events[-1], "seq":13, "data":{**events[-1]["data"], "id":"check-3"}})
        self.assertTrue(self.verify(current, events)[1])

    def test_cached_call_id_replay_after_edit_is_not_fresh_verification(self):
        command, current, events = self.verification_fixture()
        events.insert(0, {**events[-1], "seq":5})
        result, passed = self.verify(current, events)
        self.assertFalse(passed)
        self.assertEqual(result["successfulCommandsAfterLastMutation"], [])
        fresh = {**command, "callId":"fresh-check"}
        current["receipt"]["commands"].append(fresh)
        events.append({**events[-1], "seq":12, "data":{**events[-1]["data"], "id":"fresh-check"}})
        self.assertTrue(self.verify(current, events)[1])

    def test_missing_receipt_or_call_identity_cannot_borrow_earlier_success(self):
        command, current, events = self.verification_fixture()
        later = {**events[-1], "seq":12, "data":{**events[-1]["data"], "id":"missing-outcome"}}
        self.assertFalse(self.verify(current, events + [later])[1])
        current["receipt"]["commands"].append({**command, "callId":"unsequenced-failure", "status":"failed", "exitCode":1})
        self.assertFalse(self.verify(current, events)[1])
        current["receipt"]["commands"] = [{key:value for key,value in command.items() if key != "callId"}]
        events[-1]["data"].pop("id")
        self.assertFalse(self.verify(current, events)[1])

    def test_cursor_pages_include_later_mutation_and_failed_check(self):
        command, current, events = self.verification_fixture()
        later = {**events[-1], "seq":1002, "data":{**events[-1]["data"], "id":"check-2"}}
        current["receipt"]["commands"].append({**command, "callId":"check-2", "status":"failed", "exitCode":1})
        first = events + [{"runId":"run-2", "seq":seq, "type":"text"} for seq in range(12, 1001)]
        last = [{"runId":"run-2", "seq":1001, "type":"tool_completed", "tool":"write_file"}, later]
        requests = []
        class PagedAPI(harness.API):
            def __init__(inner): pass
            def __call__(inner, method, params=None):
                requests.append((method, params))
                return {"events":first if params["after"] == 0 else last,
                        "eventCursor":1000 if params["after"] == 0 else 1002,
                        "hasMore":params["after"] == 0}
        conversation = PagedAPI().conversation("session-1")
        result, passed = self.verify(current, conversation["events"])
        self.assertFalse(passed)
        self.assertEqual(result["lastMutationEventSequence"], 1001)
        self.assertEqual(result["latestPrescribedAttempt"]["callId"], "check-2")
        self.assertEqual([params["after"] for _,params in requests], [0,1000])

    def test_temperature_variant_is_frozen_and_validated(self):
        for temperature in (0, 0.2, 1.0, 2):
            updated = harness.agent_model_overrides(SimpleNamespace(temperature=temperature), self.agent)
            self.assertEqual(updated, {**self.agent, "temperature":temperature})
        self.assertNotIn("temperature", self.agent)
        for temperature in (-0.1, 2.1, True, "1", float("nan"), float("inf")):
            with self.assertRaises(ValueError):
                harness.agent_model_overrides(SimpleNamespace(temperature=temperature), self.agent)
        context = self.load()
        state = {"workspaces":[self.registered], "sessions":[self.session], "agents":[{**self.agent, "temperature":1.0}]}
        with self.assertRaisesRegex(ValueError, "agent configuration changed"):
            harness.verify_continuation_state(state, context)

    def test_tool_protocol_override_is_explicit_and_validated(self):
        self.assertNotIn("toolProtocol", harness.agent_model_overrides(SimpleNamespace(), self.agent))
        structured = {**self.agent, "toolProtocol":"structured_json"}
        self.assertEqual(harness.agent_model_overrides(SimpleNamespace(), structured), structured)
        for protocol in ("native", "structured_json"):
            updated = harness.agent_model_overrides(SimpleNamespace(tool_protocol=protocol), self.agent)
            self.assertEqual(updated, {**self.agent, "toolProtocol":protocol})
        self.assertNotIn("toolProtocol", self.agent)
        for protocol in ("", "structured", "native ", "NATIVE", True, 1, {}):
            with self.assertRaises(ValueError):
                harness.agent_model_overrides(SimpleNamespace(tool_protocol=protocol), self.agent)

    def test_tool_protocol_identity_defaults_only_legacy_absence_to_native(self):
        context = self.load()
        state = {"workspaces":[self.registered], "sessions":[self.session], "agents":[self.agent]}
        for expected in ({}, {"toolProtocol":"native"}, {"toolProtocol":"structured_json"}):
            context["report"]["agent"] = {**self.agent, **expected}
            for actual in ({}, {"toolProtocol":"native"}, {"toolProtocol":"structured_json"}, {"toolProtocol":None}, {"toolProtocol":""}):
                state["agents"] = [{**self.agent, **actual}]
                if actual.get("toolProtocol", "native") == expected.get("toolProtocol", "native"):
                    harness.verify_continuation_state(state, context)
                else:
                    with self.assertRaisesRegex(ValueError, "agent configuration changed"):
                        harness.verify_continuation_state(state, context)

    def test_followup_report_links_parent_reuses_identity_and_checks_continuity(self):
        self.fake_followup()

    def test_harness_source_drift_invalidates_otherwise_passing_qualification(self):
        self.fake_followup(mutate_harness=True)

    def fake_followup(self, mutate_harness=False):
        context = self.load()
        original_bytes = self.report_path.read_bytes()
        calls = []
        state = {"runtime":{"available":True}, "models":[{"name":"local-gemma"}], "runs":[],
                 "agents":[self.agent], "sessions":[self.session], "workspaces":[self.registered]}
        command = {"callId":"test-command", "status":"completed", "exitCode":0, "executable":self.manifest["python"], "args":harness.TEST_ARGS, "readOnly":True}
        coding = {**self.previous_run, "id":"run-2", "status":"completed", "receipt":{"commands":[command]}}
        continuity = {**coding, "id":"run-3", "summary":self.manifest["marker"], "receipt":{"commands":[]}}
        script = self.root / "driver.py"
        script.write_text("# Frozen harness source\n")
        loaded_hash = harness.digest(script)

        class FakeAPI:
            def __init__(inner, _): pass
            def __call__(inner, method, params=None):
                calls.append((method, params))
                if method == "state": return state
                if method == "run.start":
                    return coding if sum(name == method for name, _ in calls) == 1 else continuity
                if method == "agent.save":
                    # Match the app's create-versus-update identity semantics;
                    # returning params alone would hide accidental overwrites.
                    saved_agent = {**params, "id":params.get("id", "continuity-agent")}
                    state["agents"] = [agent for agent in state["agents"] if agent["id"] != saved_agent["id"]] + [saved_agent]
                    return saved_agent
                if method == "git.diff": return {"diff":"fixture diff", "scope":"combined"}
                raise AssertionError("Unexpected API operation: " + method)
            def conversation(inner, session_id):
                self.assertEqual(session_id, "session-1")
                return {"session":self.session, "messages":[{"content":"saved history"}], "events":[
                    {"runId":"run-2", "seq":1, "type":"tool_completed", "tool":"write_file"},
                    {"runId":"run-2", "seq":2, "type":"tool_started", "data":{"id":"test-command", "name":"run_command"}}]}

        def fake_wait(api, run, manifest, report, phase, timeout):
            if phase == "coding":
                (self.workspace / harness.NEW_TEST).write_text("# Simulated model regression test")
                if mutate_harness:
                    script.write_text("# Harness changed during model work\n")
            return run, state

        checks = [{"exitCode":0, "testCount":5}, {"exitCode":1, "testCount":9},
                  {"exitCode":0, "testCount":6}, {"exitCode":0, "testCount":9}]
        options = SimpleNamespace(url="http://127.0.0.1:1", model="must-not-use-override", max_steps=1, timeout=10, thinking="off", max_output_tokens=8192, temperature=1.0, tool_protocol="structured_json")
        with patch.object(harness, "API", FakeAPI), patch.object(harness, "wait_run", fake_wait), patch.object(harness, "sandbox_tests", side_effect=checks), patch.object(harness, "HARNESS_PATH", script), patch.object(harness, "HARNESS_LOADED_SHA256", loaded_hash):
            if mutate_harness:
                with self.assertRaisesRegex(RuntimeError, "qualification failed"):
                    harness.run_challenge(options, self.manifest, context)
            else:
                harness.run_challenge(options, self.manifest, context)
        self.assertEqual(self.report_path.read_bytes(), original_bytes)
        paths = list((self.root / "reports").glob("realistic-followup-*.json"))
        self.assertEqual(len(paths), 1)
        saved = json.loads(paths[0].read_text())
        self.assertEqual(saved["passed"], not mutate_harness)
        self.assertEqual(saved["harnessProvenance"]["unchanged"], not mutate_harness)
        self.assertEqual(saved["harnessProvenance"]["loadedSHA256"], loaded_hash)
        self.assertEqual(saved["harnessProvenance"]["startSHA256"], loaded_hash)
        self.assertEqual(saved["harnessProvenance"]["finishSHA256"], harness.digest(script))
        if mutate_harness:
            self.assertIn("Harness source changed", saved["qualificationInvalidated"])
        self.assertEqual(saved["attemptKind"], "followup-turn")
        self.assertEqual(saved["codingTurnNumber"], 2)
        self.assertEqual(saved["parentReport"]["sha256"], context["sha256"])
        self.assertFalse(saved["parentReport"]["passed"])
        self.assertEqual(saved["configurationChange"]["before"], self.agent)
        self.assertEqual(saved["agent"]["thinking"], "off")
        self.assertEqual(saved["agent"]["maxOutputTokens"], 8192)
        self.assertEqual(saved["agent"]["temperature"], 1.0)
        self.assertEqual(saved["agent"]["toolProtocol"], "structured_json")
        self.assertNotIn("toolProtocol", saved["configurationChange"]["before"])
        self.assertEqual(saved["configurationChange"]["after"]["toolProtocol"], "structured_json")
        self.assertIn("beforeConversation", saved)
        self.assertNotIn("initialRun", saved)
        starts = [params for method, params in calls if method == "run.start"]
        self.assertEqual(len(starts), 2)
        self.assertEqual(starts[0], {"sessionId":"session-1", "agentId":"agent-1", "model":"local-gemma", "prompt":context["prompt"]})
        continuity_agent = saved["continuationAgent"]
        self.assertEqual(starts[1]["agentId"], continuity_agent["id"])
        self.assertNotEqual(continuity_agent["id"], saved["agent"]["id"])
        self.assertNotEqual(continuity_agent["name"], saved["agent"].get("name"))
        self.assertEqual(continuity_agent["mode"], "read-only")
        self.assertEqual(continuity_agent["maxSteps"], 3)
        self.assertEqual(starts[1]["sessionId"], starts[0]["sessionId"])
        for field in ("model", "thinking", "maxOutputTokens", "temperature", "toolProtocol"):
            self.assertEqual(continuity_agent.get(field), saved["agent"].get(field))
        saves = [params for method, params in calls if method == "agent.save"]
        self.assertEqual(len(saves), 2)
        self.assertNotIn("id", saves[-1])
        coding_profile = next(agent for agent in state["agents"] if agent["id"] == saved["agent"]["id"])
        self.assertEqual(coding_profile, saved["agent"])
        self.assertEqual(coding_profile["mode"], "approval")
        harness.verify_continuation_state(state, {"report":saved})
        self.assertFalse(any(method in ("workspace.add", "session.create") for method, _ in calls))


if __name__ == "__main__":
    unittest.main()
