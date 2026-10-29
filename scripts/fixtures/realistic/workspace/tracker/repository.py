from datetime import datetime, timezone

from .models import Task


def _task(row):
    return Task(**dict(row))


def _where(status=None, query=None):
    clauses, values = [], []
    if status is not None:
        clauses.append("status = ?")
        values.append(status)
    if query:
        escaped = query.lower().replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
        clauses.append("LOWER(title) LIKE ? ESCAPE '\\'")
        values.append("%" + escaped + "%")
    return (" WHERE " + " OR ".join(clauses) if clauses else ""), values


class TaskRepository:
    def __init__(self, connection):
        self.connection = connection

    def create(self, title, status, priority, created_at=None):
        stamp = created_at or datetime.now(timezone.utc).isoformat(timespec="seconds")
        with self.connection:
            cursor = self.connection.execute(
                "INSERT INTO tasks(title,status,priority,created_at) VALUES (?,?,?,?)",
                (title, status, priority, stamp),
            )
        return self.get(cursor.lastrowid)

    def get(self, task_id):
        row = self.connection.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()
        if row is None:
            raise KeyError(task_id)
        return _task(row)

    def set_status(self, task_id, status):
        cursor = self.connection.execute("UPDATE tasks SET status=? WHERE id=?", (status, task_id))
        if cursor.rowcount != 1:
            raise KeyError(task_id)
        return self.get(task_id)

    def search(self, status=None, query=None, limit=20, offset=0):
        where, values = _where(status, query)
        rows = self.connection.execute(
            "SELECT * FROM tasks" + where + " ORDER BY created_at ASC LIMIT ? OFFSET ?",
            values + [limit, offset],
        ).fetchall()
        return [_task(row) for row in rows]

    def count(self, status=None, query=None):
        where, values = _where(status, query)
        return self.connection.execute("SELECT COUNT(*) FROM tasks" + where, values).fetchone()[0]
