import argparse
import json
from dataclasses import asdict

from .service import TaskService


def main(argv=None):
    parser = argparse.ArgumentParser(description="Local SQLite task tracker")
    parser.add_argument("--database", required=True)
    commands = parser.add_subparsers(dest="command", required=True)
    add = commands.add_parser("add")
    add.add_argument("title")
    add.add_argument("--status", default="open")
    add.add_argument("--priority", type=int, default=3)
    listing = commands.add_parser("list")
    listing.add_argument("--status")
    listing.add_argument("--query")
    listing.add_argument("--limit", type=int, default=20)
    listing.add_argument("--offset", type=int, default=0)
    export = commands.add_parser("export")
    export.add_argument("destination")
    export.add_argument("--status")
    export.add_argument("--query")
    args = parser.parse_args(argv)
    service = TaskService(args.database)
    try:
        if args.command == "add":
            result = asdict(service.create_task(args.title, args.status, args.priority))
        elif args.command == "list":
            result = asdict(service.list_tasks(status=args.status, query=args.query, limit=args.limit, offset=args.offset))
        else:
            raise NotImplementedError("Wire the export subcommand to TaskService.export_csv")
        print(json.dumps(result, ensure_ascii=False))
        return 0
    finally:
        service.close()


if __name__ == "__main__":
    raise SystemExit(main())
