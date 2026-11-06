import tempfile
import unittest
from pathlib import Path

from service.storage import NoteStore


class NoteStoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.store = NoteStore(str(Path(self.tmp.name) / "notes.json"))

    def tearDown(self):
        self.tmp.cleanup()

    def test_add_and_list(self):
        self.store.add("groceries", "milk, eggs")
        notes = self.store.list()
        self.assertEqual([n["title"] for n in notes], ["groceries"])

    def test_title_required(self):
        with self.assertRaises(ValueError):
            self.store.add("   ", "body")

    def test_empty_store(self):
        self.assertEqual(self.store.list(), [])


if __name__ == "__main__":
    unittest.main()
