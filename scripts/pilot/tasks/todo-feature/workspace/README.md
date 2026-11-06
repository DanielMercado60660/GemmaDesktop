# todo

A tiny command-line todo list. Plain Python 3, no dependencies.

## Usage

    python3 todo.py add "Buy milk"
    python3 todo.py list
    python3 todo.py done 1

Items are stored as JSON in `todo.json` in the current directory. To use a
different file, pass `--file` before the command or set `TODO_FILE`:

    python3 todo.py --file ~/work-todo.json list
    TODO_FILE=~/work-todo.json python3 todo.py list

## Tests

    python3 -m unittest discover -s tests -v

## Requested feature: priorities

I'd like to be able to give items a priority.

- `add` takes a `--priority` option: `high`, `medium` or `low`. If I leave it
  out, the item is `medium`. Example: `python3 todo.py add "Renew passport" --priority high`
- `list` shows each item's priority next to it, e.g. `[ ] 3. Renew passport (high)`
- `list --sort priority` shows high items first, then medium, then low. Items
  with the same priority stay in the order I added them. Plain `list` should
  still show everything in the order it was added.
- Any other priority value should be rejected with an error message and a
  non-zero exit code (and nothing gets added).
- I already have a couple of todo files full of items from before this change,
  so those need to keep working.
