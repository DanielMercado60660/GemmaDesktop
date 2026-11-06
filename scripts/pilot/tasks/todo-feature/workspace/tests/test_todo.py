import contextlib
import io
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import todo  # noqa: E402


class TodoTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self.tmp.name, "todo.json")

    def tearDown(self):
        self.tmp.cleanup()

    def run_cli(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = todo.main(["--file", self.path, *args])
        return code, out.getvalue(), err.getvalue()

    def test_add_then_list(self):
        self.assertEqual(self.run_cli("add", "Buy milk")[0], 0)
        code, out, _ = self.run_cli("list")
        self.assertEqual(code, 0)
        self.assertIn("[ ] 1. Buy milk", out)

    def test_ids_increase(self):
        self.run_cli("add", "Buy milk")
        self.run_cli("add", "Walk the dog")
        _, out, _ = self.run_cli("list")
        self.assertIn("1. Buy milk", out)
        self.assertIn("2. Walk the dog", out)

    def test_done_marks_item(self):
        self.run_cli("add", "Buy milk")
        self.run_cli("add", "Walk the dog")
        code, out, _ = self.run_cli("done", "2")
        self.assertEqual(code, 0)
        self.assertIn("Walk the dog", out)
        _, out, _ = self.run_cli("list")
        self.assertIn("[ ] 1. Buy milk", out)
        self.assertIn("[x] 2. Walk the dog", out)

    def test_done_unknown_id(self):
        self.run_cli("add", "Buy milk")
        code, _, err = self.run_cli("done", "7")
        self.assertEqual(code, 1)
        self.assertIn("No item with id 7", err)

    def test_empty_list(self):
        code, out, _ = self.run_cli("list")
        self.assertEqual(code, 0)
        self.assertIn("Nothing to do", out)

    def test_items_are_saved_as_json(self):
        self.run_cli("add", "Buy milk")
        with open(self.path, encoding="utf-8") as f:
            data = json.load(f)
        self.assertEqual(len(data), 1)
        self.assertEqual(data[0]["id"], 1)
        self.assertEqual(data[0]["text"], "Buy milk")
        self.assertFalse(data[0]["done"])

    def test_todo_file_env_var(self):
        other = os.path.join(self.tmp.name, "work.json")
        with mock.patch.dict(os.environ, {"TODO_FILE": other}):
            with contextlib.redirect_stdout(io.StringIO()):
                todo.main(["add", "Send report"])
        self.assertEqual(todo.load_items(other)[0]["text"], "Send report")
        self.assertFalse(os.path.exists(self.path))


if __name__ == "__main__":
    unittest.main()
