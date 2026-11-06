"""A tiny JSON-file note store."""
from __future__ import annotations

import json
import os
import threading
import time
from pathlib import Path

from service import config


class NoteStore:
    def __init__(self, path: str):
        self.path = Path(path)
        self._lock = threading.Lock()

    def _load(self) -> list[dict]:
        if not self.path.exists():
            return []
        return json.loads(self.path.read_text(encoding="utf-8"))

    def _save(self, notes: list[dict]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(".tmp")
        tmp.write_text(json.dumps(notes, indent=2), encoding="utf-8")
        os.replace(tmp, self.path)

    def add(self, title: str, body: str) -> dict:
        title, body = str(title).strip(), str(body)
        if not title:
            raise ValueError("title is required")
        if len(title) > config.MAX_TITLE_LENGTH:
            raise ValueError("title too long")
        if len(body) > config.MAX_NOTE_LENGTH:
            raise ValueError("note too long")
        with self._lock:
            notes = self._load()
            note = {"id": len(notes) + 1, "title": title, "body": body, "created": time.time()}
            notes.append(note)
            self._save(notes)
        return note

    def list(self, limit: int = config.PAGE_SIZE) -> list[dict]:
        limit = max(1, min(limit, config.MAX_PAGE_SIZE))
        with self._lock:
            notes = self._load()
        return sorted(notes, key=lambda n: n["created"], reverse=True)[:limit]
