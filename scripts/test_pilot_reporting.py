import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).parent / "pilot" / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


pilot = load("gemma_pilot")
exporter = load("export_evidence")


class PilotReportingTests(unittest.TestCase):
    def test_rejections_remain_separate_from_executed_tool_errors(self):
        events = [
            {"type": "tool_validation_failed", "tool": "run_command", "detail": "args must be an array"},
            {"type": "tool_started", "tool": "run_command"},
            {"type": "tool_completed", "tool": "run_command"},
            {"type": "tool_started", "tool": "read_file"},
            {"type": "tool_failed", "tool": "read_file", "detail": "not found"},
        ]
        before = copy.deepcopy(events)
        summary = pilot.summarize_tools(events)
        self.assertEqual((summary["calls"], summary["errors"], summary["validationErrors"]), (2, 1, 1))
        self.assertEqual(summary["byTool"], {"run_command": 1, "read_file": 1})
        self.assertEqual(summary["validationFailures"][0]["executed"], False)
        self.assertEqual(summary["failures"], [{"tool": "read_file", "detail": "not found"}])
        self.assertEqual(events, before)
        many = pilot.summarize_tools([{"type": "tool_validation_failed", "tool": "read_file", "detail": "x" * 1000}] * 50)
        self.assertEqual((many["calls"], many["errors"], many["validationErrors"]), (0, 0, 50))
        self.assertEqual(len(many["validationFailures"]), 40)
        self.assertEqual(len(many["validationFailures"][0]["detail"]), 400)

    def test_export_preserves_validation_evidence_without_changing_outcomes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            batch = root / "batch"
            case = batch / "synthetic"
            case.mkdir(parents=True)
            (batch / "batch.json").write_text(json.dumps({"label": "synthetic", "tasks": ["synthetic"]}))
            outcomes = {"modelCompleted": False, "independentlyCorrect": False}
            tools = pilot.summarize_tools([{"type": "tool_validation_failed", "tool": "run_command", "detail": "array required"}])
            record = {"outcomes": outcomes, "toolCalls": tools}
            source = case / "record.json"
            source.write_text(json.dumps(record))
            original = source.read_bytes()
            result = json.loads(exporter.export(batch, root / "out").read_text())["tasks"][0]
            self.assertEqual(result["outcomes"], outcomes)
            self.assertEqual(result["toolCalls"]["validationErrors"], 1)
            self.assertEqual(result["toolCalls"]["validationFailures"], tools["validationFailures"])
            self.assertEqual(source.read_bytes(), original)
            record["toolCalls"] = {"calls": 0, "errors": 0}
            source.write_text(json.dumps(record))
            old = json.loads(exporter.export(batch, root / "out").read_text())["tasks"][0]
            self.assertIsNone(old["toolCalls"]["validationErrors"])


if __name__ == "__main__":
    unittest.main()
