import importlib.util
from pathlib import Path
import signal
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "public_pointer_controls", Path(__file__).parent / "harness/check_public_pointer_coverage.py"
)
controls = importlib.util.module_from_spec(spec)
spec.loader.exec_module(controls)


class FakeChild:
    pid = 43210

    def __init__(self):
        self.returncode = None
        self.communicate_calls = 0
        self.wait_calls = 0

    def poll(self):
        return self.returncode

    def communicate(self, timeout=None):
        self.communicate_calls += 1
        self.returncode = 0
        return "[]", ""

    def wait(self):
        self.wait_calls += 1
        return self.returncode


class PointerControlObservationTests(unittest.TestCase):
    def test_ownership_artifact_failure_kills_and_reaps_before_propagating(self):
        child = FakeChild()

        def kill_owned(group, sig):
            self.assertEqual((group, sig), (child.pid, signal.SIGKILL))
            child.returncode = -signal.SIGKILL

        with tempfile.TemporaryDirectory() as tmp:
            runner = controls.Controls(Path(tmp))
            with patch.object(controls.subprocess, "Popen", return_value=child) as launch, \
                    patch.object(controls.os, "killpg", side_effect=kill_owned) as kill, \
                    patch.object(controls, "write_json", side_effect=OSError("synthetic artifact write failure")):
                with self.assertRaisesRegex(OSError, "synthetic artifact write failure"):
                    runner.run(Path(tmp), ["synthetic-no-execution"], "ownership-write-failure")
            self.assertTrue(launch.call_args.kwargs["start_new_session"])
            self.assertEqual(launch.call_count, 1)
            self.assertEqual(kill.call_count, 1)
            self.assertEqual(child.communicate_calls, 0)
            self.assertEqual(child.wait_calls, 1)
            self.assertEqual((runner.calls, runner.reaped), (1, 1))

    def test_terminal_artifact_failure_occurs_after_completed_child_is_reaped(self):
        child = FakeChild()
        writes = []

        def write_observation(path, row):
            writes.append(dict(row))
            if len(writes) == 2:
                raise OSError("synthetic terminal write failure")

        with tempfile.TemporaryDirectory() as tmp:
            runner = controls.Controls(Path(tmp))
            with patch.object(controls.subprocess, "Popen", return_value=child), \
                    patch.object(controls.os, "killpg") as kill, \
                    patch.object(controls, "write_json", side_effect=write_observation):
                with self.assertRaisesRegex(OSError, "synthetic terminal write failure"):
                    runner.run(Path(tmp), ["synthetic-no-execution"], "terminal-write-failure")
            self.assertEqual(kill.call_count, 0)
            self.assertEqual(child.communicate_calls, 1)
            self.assertEqual(child.wait_calls, 1)
            self.assertEqual(runner.reaped, 1)
            self.assertEqual(writes[0]["status"], "running")
            self.assertTrue(writes[1]["waitedAndReaped"])

    def test_ordinary_observation_retains_success_and_exactly_one_wait(self):
        child = FakeChild()
        rows = []
        with tempfile.TemporaryDirectory() as tmp:
            runner = controls.Controls(Path(tmp))
            with patch.object(controls.subprocess, "Popen", return_value=child), \
                    patch.object(controls.os, "killpg") as kill, \
                    patch.object(controls, "write_json", side_effect=lambda path, row: rows.append(dict(row))):
                code, stdout = runner.run(Path(tmp), ["synthetic-no-execution"], "ordinary")
            self.assertEqual((code, stdout), (0, "[]"))
            self.assertEqual(child.wait_calls, 1)
            self.assertEqual((runner.calls, runner.reaped), (1, 1))
            self.assertEqual(kill.call_count, 0)
            self.assertEqual(rows[0]["status"], "running")
            self.assertTrue(rows[1]["outputCaptureComplete"])


if __name__ == "__main__":
    unittest.main()
