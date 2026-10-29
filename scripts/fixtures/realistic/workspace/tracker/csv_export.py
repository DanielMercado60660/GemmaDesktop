FIELDS = ("id", "title", "status", "priority", "created_at")


def serialize(tasks):
    """Return a CSV document, including a header even for an empty iterable."""
    raise NotImplementedError("CSV serialization has not been implemented")


def write_tasks(destination, tasks):
    """Write UTF-8 CSV with correct quoting; return the number of task rows."""
    raise NotImplementedError("CSV writing has not been implemented")
