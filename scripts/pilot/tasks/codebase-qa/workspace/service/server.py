"""HTTP entry point for the notes service: python3 -m service.server"""
from __future__ import annotations

import json
import logging
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from service import config
from service.settings_loader import load_settings
from service.storage import NoteStore

log = logging.getLogger("notes")


class NotesHandler(BaseHTTPRequestHandler):
    store: NoteStore  # assigned in main()

    def _send(self, status: int, payload) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            return self._send(200, {"ok": True})
        if self.path.startswith(config.API_PREFIX + "/notes"):
            return self._send(200, {"notes": self.store.list(limit=config.PAGE_SIZE)})
        self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path != config.API_PREFIX + "/notes":
            return self._send(404, {"error": "not found"})
        length = int(self.headers.get("Content-Length") or 0)
        if length > config.MAX_BODY_BYTES:
            return self._send(413, {"error": "body too large"})
        try:
            data = json.loads(self.rfile.read(length) or b"{}")
            note = self.store.add(data.get("title", ""), data.get("body", ""))
        except (ValueError, AttributeError) as e:
            return self._send(400, {"error": str(e)})
        self._send(201, note)

    def log_message(self, fmt, *args):
        log.debug("%s - " + fmt, self.address_string(), *args)


def main() -> None:
    settings = load_settings()
    logging.basicConfig(level=logging.DEBUG if settings.debug else logging.INFO)
    NotesHandler.store = NoteStore(settings.data_file)
    server = ThreadingHTTPServer((settings.host, settings.port), NotesHandler)
    log.info("%s listening on http://%s:%d", config.APP_NAME, settings.host, settings.port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
