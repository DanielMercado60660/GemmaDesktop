# notes-service

A small JSON API for personal notes. Standard library only, no external
dependencies.

## Running

    python3 -m service.server

Settings are read from environment variables when the server starts:

| Variable          | Meaning                                         |
| ----------------- | ----------------------------------------------- |
| `NOTES_HOST`      | interface to bind (default `127.0.0.1`)         |
| `NOTES_DATA_FILE` | where notes are stored (default `data/notes.json`) |
| `NOTES_DEBUG`     | set to `1` for debug logging                    |

## API

- `GET /healthz` - liveness check
- `GET /api/v2/notes` - list notes, newest first
- `POST /api/v2/notes` - create a note from `{"title": "...", "body": "..."}`

## Tests

    python3 -m unittest discover -s tests

## Layout

- `service/` - the current service (2.x)
- `legacy/` - the original v1 prototype
- `tests/` - unit tests

## Deployment notes (2023)

The service runs behind nginx on the shared box. It listens on port 8000 and
nginx proxies `notes.internal:80` to it. Restart it with
`sudo systemctl restart notes`. Logs go to `/var/log/notes/notes.log`.
