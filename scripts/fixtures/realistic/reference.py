"""Harness-only known repair used to qualify the independent acceptance suite.

Never copy this module or its solution into the model's workspace.
"""
from pathlib import Path


def apply_reference(candidate):
    root = Path(candidate) / "tracker"
    repository = root / "repository.py"
    text = repository.read_text()
    text = text.replace('" OR ".join(clauses)', '" AND ".join(clauses)')
    text = text.replace("ORDER BY created_at ASC LIMIT", "ORDER BY created_at DESC, id DESC LIMIT")
    text = text.replace(
        '        cursor = self.connection.execute("UPDATE tasks SET status=? WHERE id=?", (status, task_id))\n'
        '        if cursor.rowcount != 1:\n'
        '            raise KeyError(task_id)\n',
        '        with self.connection:\n'
        '            cursor = self.connection.execute("UPDATE tasks SET status=? WHERE id=?", (status, task_id))\n'
        '            if cursor.rowcount != 1:\n'
        '                raise KeyError(task_id)\n',
    )
    repository.write_text(text)
    service = root / "service.py"
    text = service.read_text().replace("from .db import connect", "from .csv_export import write_tasks\nfrom .db import connect")
    text = text.replace(
        '        raise NotImplementedError("CSV export has not been implemented")',
        '        _status(status, optional=True)\n'
        '        tasks, offset = [], 0\n'
        '        while True:\n'
        '            batch = self.list_tasks(status=status, query=query, limit=100, offset=offset).items\n'
        '            tasks.extend(batch)\n'
        '            if len(batch) < 100:\n'
        '                break\n'
        '            offset += len(batch)\n'
        '        return write_tasks(destination, tasks)',
    )
    service.write_text(text)
    (root / "csv_export.py").write_text('''import csv
import io
from pathlib import Path

FIELDS = ("id", "title", "status", "priority", "created_at")


def serialize(tasks):
    stream = io.StringIO(newline="")
    writer = csv.writer(stream, lineterminator="\\n")
    writer.writerow(FIELDS)
    for task in tasks:
        writer.writerow([getattr(task, field) for field in FIELDS])
    return stream.getvalue()


def write_tasks(destination, tasks):
    rows = list(tasks)
    with Path(destination).open("w", encoding="utf-8", newline="") as stream:
        stream.write(serialize(rows))
    return len(rows)
''')
    cli = root / "cli.py"
    cli.write_text(cli.read_text().replace(
        '            raise NotImplementedError("Wire the export subcommand to TaskService.export_csv")',
        '            count = service.export_csv(args.destination, status=args.status, query=args.query)\n'
        '            result = {"exported": count, "path": args.destination}',
    ))
