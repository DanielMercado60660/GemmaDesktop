"""Resolve runtime settings from the process environment.

Deployment-specific values are read here once at startup; the rest of the
code receives a frozen Settings object and never touches os.environ.
"""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Mapping

from service import config

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8085


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    data_file: str
    debug: bool


def _flag(value: str | None) -> bool:
    return (value or "").strip().lower() in {"1", "true", "yes", "on"}


def _port(value: str | None) -> int:
    if value is None or not value.strip():
        return DEFAULT_PORT
    port = int(value)
    if not 0 < port < 65536:
        raise ValueError(f"NOTES_PORT out of range: {port}")
    return port


def load_settings(env: Mapping[str, str] | None = None) -> Settings:
    env = os.environ if env is None else env
    return Settings(
        host=env.get("NOTES_HOST", DEFAULT_HOST),
        port=_port(env.get("NOTES_PORT")),
        data_file=env.get("NOTES_DATA_FILE", config.DEFAULT_DATA_FILE),
        debug=_flag(env.get("NOTES_DEBUG")),
    )
