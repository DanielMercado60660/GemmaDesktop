from .db import connect
from .models import STATUSES, TaskPage
from .repository import TaskRepository


def _status(value, optional=False):
    if optional and value is None:
        return
    if value not in STATUSES:
        raise ValueError("status must be open, in_progress, or done")


class TaskService:
    def __init__(self, database):
        self.connection = connect(database)
        self.repository = TaskRepository(self.connection)

    def close(self):
        self.connection.close()

    def create_task(self, title, status="open", priority=3, created_at=None):
        _status(status)
        if not isinstance(title, str) or not title.strip():
            raise ValueError("a nonempty title is required")
        if not isinstance(priority, int) or isinstance(priority, bool) or not 1 <= priority <= 5:
            raise ValueError("priority must be an integer from 1 to 5")
        return self.repository.create(title.strip(), status, priority, created_at)

    def get_task(self, task_id):
        return self.repository.get(task_id)

    def set_status(self, task_id, status):
        _status(status)
        return self.repository.set_status(task_id, status)

    def list_tasks(self, *, status=None, query=None, limit=20, offset=0):
        _status(status, optional=True)
        if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 100:
            raise ValueError("limit must be from 1 to 100")
        if not isinstance(offset, int) or isinstance(offset, bool) or offset < 0:
            raise ValueError("offset must be nonnegative")
        return TaskPage(
            self.repository.search(status, query, limit, offset),
            self.repository.count(status, query),
            limit,
            offset,
        )

    def export_csv(self, destination, *, status=None, query=None):
        """Write every matching task in listing order; return the exported count."""
        raise NotImplementedError("CSV export has not been implemented")
