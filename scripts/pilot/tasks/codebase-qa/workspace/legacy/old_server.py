"""Original v1 prototype of the notes API (single file).

Kept for reference while the last v1 exports are migrated.
"""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

HOST = "0.0.0.0"
PORT = 5000
NOTES = []


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps(NOTES).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        NOTES.append(json.loads(self.rfile.read(length) or b"{}"))
        self.send_response(201)
        self.end_headers()


if __name__ == "__main__":
    print(f"v1 notes server on http://{HOST}:{PORT}")
    HTTPServer((HOST, PORT), Handler).serve_forever()
