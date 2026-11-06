import contextlib
import io
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import todo  # noqa: E402


class PriorityTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self.tmp.name, "todo.json")

    def tearDown(self):
        self.tmp.cleanup()

    def run_cli(self, *args):
        out, err = io.StringIO(), io.StringIO()
        code = None
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            try:
                code = todo.main(["--file", self.path, *args])
            except SystemExit as e:
                code = e.code
        return code, out.getvalue(), err.getvalue()

    def test_default_priority_is_medium(self):
        self.run_cli("add", "Buy milk")
        _, out, _ = self.run_cli("list")
        self.assertIn("[ ] 1. Buy milk (medium)", out)

    def test_add_with_priority(self):
        self.assertEqual(self.run_cli("add", "Renew passport", "--priority", "high")[0], 0)
        _, out, _ = self.run_cli("list")
        self.assertIn("[ ] 1. Renew passport (high)", out)

    def test_sort_by_priority_keeps_added_order_for_ties(self):
        self.run_cli("add", "a", "--priority", "low")
        self.run_cli("add", "b", "--priority", "high")
        self.run_cli("add", "c")
        self.run_cli("add", "d", "--priority", "high")
        _, out, _ = self.run_cli("list", "--sort", "priority")
        texts = [line.split(". ", 1)[1].split(" (")[0] for line in out.splitlines()]
        self.assertEqual(texts, ["b", "d", "c", "a"])
        _, out, _ = self.run_cli("list")
        texts = [line.split(". ", 1)[1].split(" (")[0] for line in out.splitlines()]
        self.assertEqual(texts, ["a", "b", "c", "d"])

    def test_invalid_priority_is_rejected(self):
        code, _, err = self.run_cli("add", "Mystery", "--priority", "urgent")
        self.assertNotEqual(code, 0)
        self.assertIn("priority", err)
        self.assertFalse(os.path.exists(self.path))

    def test_old_items_without_priority_are_medium(self):
        with open(self.path, "w", encoding="utf-8") as f:
            json.dump([{"id": 1, "text": "Old task", "done": False}], f)
        self.run_cli("add", "Urgent task", "--priority", "high")
        _, out, _ = self.run_cli("list", "--sort", "priority")
        self.assertEqual(out.splitlines(), ["[ ] 2. Urgent task (high)", "[ ] 1. Old task (medium)"])


if __name__ == "__main__":
    unittest.main()
