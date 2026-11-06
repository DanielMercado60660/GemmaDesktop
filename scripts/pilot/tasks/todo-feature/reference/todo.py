#!/usr/bin/env python3
"""A tiny command-line todo list.

    python3 todo.py add "Buy milk" [--priority high|medium|low]
    python3 todo.py list [--sort priority]
    python3 todo.py done 1

Items live in a JSON file: --file PATH, else $TODO_FILE, else ./todo.json.
"""
import argparse
import json
import os
import sys

DEFAULT_FILE = "todo.json"
PRIORITIES = ("high", "medium", "low")
DEFAULT_PRIORITY = "medium"


def todo_path(cli_path=None):
    """Pick the storage file: --file wins, then $TODO_FILE, then ./todo.json."""
    return cli_path or os.environ.get("TODO_FILE") or DEFAULT_FILE


def load_items(path):
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_items(path, items):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(items, f, indent=2)
        f.write("\n")


def priority_of(item):
    """Items saved before priorities existed count as medium."""
    return item.get("priority", DEFAULT_PRIORITY)


def add_item(items, text, priority=DEFAULT_PRIORITY):
    if priority not in PRIORITIES:
        raise ValueError(f"invalid priority {priority!r} (choose from high, medium, low)")
    next_id = max((item["id"] for item in items), default=0) + 1
    item = {"id": next_id, "text": text, "done": False, "priority": priority}
    items.append(item)
    return item


def mark_done(items, item_id):
    for item in items:
        if item["id"] == item_id:
            item["done"] = True
            return item
    return None


def sort_by_priority(items):
    # sorted() is stable, so equal priorities keep the order they were added in.
    return sorted(items, key=lambda item: PRIORITIES.index(priority_of(item)))


def format_item(item):
    box = "x" if item["done"] else " "
    return f"[{box}] {item['id']}. {item['text']} ({priority_of(item)})"


def build_parser():
    parser = argparse.ArgumentParser(prog="todo", description="A tiny todo list.")
    parser.add_argument("--file", help="JSON file to use (default: $TODO_FILE or ./todo.json)")
    sub = parser.add_subparsers(dest="command", required=True)

    add = sub.add_parser("add", help="add an item")
    add.add_argument("text", help="what needs doing")
    add.add_argument("--priority", choices=PRIORITIES, default=DEFAULT_PRIORITY,
                     help="high, medium or low (default: medium)")

    lst = sub.add_parser("list", help="show all items")
    lst.add_argument("--sort", choices=("priority",), help="order the list (only 'priority' for now)")

    done = sub.add_parser("done", help="mark an item as done")
    done.add_argument("id", type=int, help="item number shown by 'list'")
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    path = todo_path(args.file)
    items = load_items(path)

    if args.command == "add":
        item = add_item(items, args.text, args.priority)
        save_items(path, items)
        print(f"Added {item['id']}: {item['text']} ({item['priority']})")
    elif args.command == "list":
        if not items:
            print("Nothing to do!")
        shown = sort_by_priority(items) if args.sort == "priority" else items
        for item in shown:
            print(format_item(item))
    elif args.command == "done":
        item = mark_done(items, args.id)
        if item is None:
            print(f"No item with id {args.id}", file=sys.stderr)
            return 1
        save_items(path, items)
        print(f"Done: {item['text']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
