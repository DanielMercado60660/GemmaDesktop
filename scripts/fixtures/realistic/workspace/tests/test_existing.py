import pathlib
import tempfile
import unittest

from tracker import TaskService


class ExistingBehavior(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.database = pathlib.Path(self.directory.name) / "tasks.sqlite"
        self.service = TaskService(self.database)

    def tearDown(self):
        self.service.close()
        self.directory.cleanup()

    def test_create_and_read(self):
        task = self.service.create_task("Plan release", priority=2)
        self.assertEqual(self.service.get_task(task.id), task)

    def test_status_update_survives_reopen(self):
        task = self.service.create_task("Publish release")
        self.service.set_status(task.id, "done")
        self.service.close()
        self.service = TaskService(self.database)
        self.assertEqual(self.service.get_task(task.id).status, "done")

    def test_filters_are_combined(self):
        wanted = self.service.create_task("Release notes", status="open")
        self.service.create_task("Release archive", status="done")
        self.service.create_task("Other work", status="open")
        page = self.service.list_tasks(status="open", query="release")
        self.assertEqual([task.id for task in page.items], [wanted.id])
        self.assertEqual(page.total, 1)

    def test_newest_first_with_stable_ties(self):
        self.service.create_task("Old", created_at="2026-01-01T00:00:00+00:00")
        self.service.create_task("New A", created_at="2026-02-01T00:00:00+00:00")
        newest = self.service.create_task("New B", created_at="2026-02-01T00:00:00+00:00")
        self.assertEqual(self.service.list_tasks(limit=1).items[0].id, newest.id)

    def test_invalid_values_are_rejected(self):
        with self.assertRaises(ValueError):
            self.service.create_task("Invalid", status="unknown")
        with self.assertRaises(ValueError):
            self.service.list_tasks(limit=0)


if __name__ == "__main__":
    unittest.main()
