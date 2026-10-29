#!/usr/bin/env python3
"""Supplemental checks derived only from the public Fieldnote README/review.

This is separate from, and does not replace, the original acceptance gate.
Use --candidate to select a frozen workspace. All databases/exports are private
temporary files; the candidate source is imported with bytecode writing off.
"""

import argparse
import csv
from pathlib import Path
import sys
import tempfile
import unittest


class PublicSearchContract(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="public-search-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.service = TaskService(self.root / "tasks.sqlite")
        self.addCleanup(self.service.close)

    def create(self, title, status="open"):
        return self.service.create_task(
            title, status=status, created_at="2026-01-01T00:00:00+00:00"
        )

    def assert_query(self, query, expected, status=None):
        filters = {"query": query, "status": status}
        page = self.service.list_tasks(**filters, limit=1)
        self.assertEqual(page.total, len(expected), "filtered count changed")
        self.assertEqual([task.id for task in page.items], [task.id for task in expected[:1]])
        full = self.service.list_tasks(**filters, limit=100)
        self.assertEqual([task.id for task in full.items], [task.id for task in expected])
        self.assertEqual([task.title for task in full.items], [task.title for task in expected])
        destination = self.root / "filtered.csv"
        count = self.service.export_csv(destination, **filters)
        with destination.open(encoding="utf-8", newline="") as stream:
            rows = list(csv.DictReader(stream))
        self.assertEqual(count, len(expected), "exported count changed")
        self.assertEqual([int(row["id"]) for row in rows], [task.id for task in expected])
        self.assertEqual([row["title"] for row in rows], [task.title for task in expected])

    def test_non_ascii_characters_remain_literal(self):
        upper = self.create("Éclair")
        lower = self.create("éclair")
        self.assert_query("É", [upper])
        self.assert_query("é", [lower])

    def test_only_ascii_letters_are_case_insensitive(self):
        upper = self.create("Éclair")
        lower = self.create("éclair")
        self.assert_query("ÉC", [upper])
        self.assert_query("éC", [lower])
        self.assert_query("CLAIR", [lower, upper])

    def assert_literal_filter(self, title, decoy, query):
        wanted = self.create(title)
        self.create(decoy)
        closed = self.create(title, status="done")
        self.assert_query(query, [wanted], status="open")
        self.assert_query(query, [closed, wanted])

    def test_percent_is_literal(self):
        self.assert_literal_filter("Progress 100% ready", "Progress 100 percent ready", "%")

    def test_underscore_is_literal(self):
        self.assert_literal_filter("release_notes", "releaseXnotes", "_")

    def test_backslash_is_literal(self):
        self.assert_literal_filter(r"folder\notes", "folder/notes", "\\")

    def test_quotes_are_literal(self):
        self.assert_literal_filter("owner's \"draft\"", "owners draft", "'s \"")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    options, unittest_args = parser.parse_known_args()
    candidate = options.candidate.resolve(strict=True)
    if not (candidate / "tracker" / "__init__.py").is_file():
        parser.error("candidate must contain the tracker package")
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(candidate))
    from tracker import TaskService

    unittest.main(argv=[sys.argv[0], *unittest_args], verbosity=2)
