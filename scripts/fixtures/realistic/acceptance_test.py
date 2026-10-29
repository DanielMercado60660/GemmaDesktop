"""Independent acceptance suite. Never copy this file into the agent workspace."""
import argparse
import contextlib
import csv
import io
import json
import pathlib
import sys
import tempfile
import unittest

arguments = argparse.ArgumentParser()
arguments.add_argument("--candidate", required=True)
options = arguments.parse_args()
sys.path.insert(0, str(pathlib.Path(options.candidate).resolve()))

from tracker import TaskService
from tracker.cli import main
from tracker.csv_export import FIELDS, serialize


class Acceptance(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.directory.name)
        self.database = self.root / "tasks.sqlite"
        self.service = TaskService(self.database)

    def tearDown(self):
        self.service.close()
        self.directory.cleanup()

    def create(self, title, status="open", stamp="2026-03-01T00:00:00+00:00"):
        return self.service.create_task(title, status, 2, stamp)

    def rows(self, path):
        with path.open(encoding="utf-8", newline="") as stream:
            return list(csv.DictReader(stream))

    def test_status_is_durable_and_unknown_id_is_rejected(self):
        task = self.create("Résumé planning")
        self.service.set_status(task.id, "in_progress")
        self.service.close()
        self.service = TaskService(self.database)
        self.assertEqual(self.service.get_task(task.id).status, "in_progress")
        with self.assertRaises(KeyError):
            self.service.set_status(99999, "done")

    def test_filter_intersection_and_filtered_total(self):
        first = self.create("ALPHA one")
        second = self.create("alpha two")
        self.create("alpha closed", "done")
        self.create("unrelated open")
        page = self.service.list_tasks(status="open", query="alpha", limit=1, offset=1)
        self.assertEqual(page.total, 2)
        self.assertEqual([t.id for t in page.items], [first.id])
        self.assertGreater(second.id, first.id)

    def test_literal_wildcards_and_quote_are_not_sql(self):
        literal = self.create("Budget 100%_ready \\\"quoted\\\"")
        self.create("Budget 100XXready anything")
        self.assertEqual([t.id for t in self.service.list_tasks(query="100%_").items], [literal.id])
        self.assertEqual(self.service.list_tasks(query="' OR 1=1 --").total, 0)

    def test_pages_are_disjoint_stable_and_newest_first(self):
        oldest = self.create("Old", stamp="2025-01-01T00:00:00+00:00")
        tied = [self.create("Tied " + str(i)) for i in range(7)]
        expected = [t.id for t in reversed(tied)] + [oldest.id]
        actual = []
        for offset in (0, 3, 6):
            page = self.service.list_tasks(limit=3, offset=offset)
            self.assertEqual(page.total, 8)
            actual.extend(t.id for t in page.items)
        self.assertEqual(actual, expected)
        self.assertEqual(self.service.list_tasks(offset=99).items, [])

    def test_validation_survives_repairs(self):
        for options in ({"limit": 0}, {"limit": 101}, {"offset": -1}, {"status": "not-a-status"}):
            with self.assertRaises(ValueError):
                self.service.list_tasks(**options)
        for title, priority in ((" ", 2), ("title", 0), ("title", True)):
            with self.assertRaises(ValueError):
                self.service.create_task(title, priority=priority)

    def test_csv_quotes_newlines_and_unicode_round_trip(self):
        titles = ['Review, "quoted" proposal', "Line one\nLine two", "Résumé 東京 🧪"]
        tasks = [self.create(title) for title in titles]
        path = self.root / "export.csv"
        self.assertEqual(self.service.export_csv(path), 3)
        data = path.read_bytes()
        self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
        self.assertNotIn(b"\r\n", data)
        rows = self.rows(path)
        self.assertEqual(list(rows[0]), list(FIELDS))
        self.assertEqual([row["title"] for row in rows], list(reversed(titles)))
        self.assertEqual([int(row["id"]) for row in rows], [task.id for task in reversed(tasks)])

    def test_export_is_not_limited_to_one_page(self):
        for i in range(205):
            self.create("keep " + str(i), "done")
        self.create("exclude", "open")
        self.create("irrelevant", "done")
        path = self.root / "large.csv"
        self.assertEqual(self.service.export_csv(path, status="done", query="keep"), 205)
        rows = self.rows(path)
        self.assertEqual(len(rows), 205)
        self.assertEqual([int(row["id"]) for row in rows], list(range(205, 0, -1)))
        self.assertEqual(self.service.list_tasks().total, 207)

    def test_empty_export_header_and_invalid_filter(self):
        path = self.root / "empty.csv"
        self.assertEqual(self.service.export_csv(path), 0)
        self.assertEqual(path.read_text(encoding="utf-8"), ",".join(FIELDS) + "\n")
        self.assertEqual(serialize([]), ",".join(FIELDS) + "\n")
        with self.assertRaises(ValueError):
            self.service.export_csv(path, status="invalid")

    def test_cli_uses_service_filter_and_writes_real_file(self):
        wanted = self.create("CLI keep", "done")
        self.create("CLI omit", "open")
        path = self.root / "cli.csv"
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            code = main(["--database", str(self.database), "export", str(path), "--status", "done", "--query", "keep"])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(stdout.getvalue()), {"exported": 1, "path": str(path)})
        self.assertEqual([int(row["id"]) for row in self.rows(path)], [wanted.id])


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]], verbosity=2)
