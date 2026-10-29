import sqlite3


def connect(path):
    connection = sqlite3.connect(str(path))
    connection.row_factory = sqlite3.Row
    connection.execute(
        """CREATE TABLE IF NOT EXISTS tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        title TEXT NOT NULL,
        status TEXT NOT NULL CHECK(status IN ('open','in_progress','done')),
        priority INTEGER NOT NULL CHECK(priority BETWEEN 1 AND 5),
        created_at TEXT NOT NULL
        )"""
    )
    connection.commit()
    return connection
