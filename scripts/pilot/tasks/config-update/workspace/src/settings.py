"""Load and sanity-check config/app.json.

Run `python3 src/settings.py` from the project root to check the config.
"""
import json
from pathlib import Path

CONFIG_PATH = Path(__file__).resolve().parent.parent / "config" / "app.json"
THEMES = {"light", "dark", "system"}
LOG_LEVELS = {"debug", "info", "warning", "error"}


def validate(cfg):
    problems = []
    if cfg["ui"]["theme"] not in THEMES:
        problems.append(f"unknown ui.theme {cfg['ui']['theme']!r}")
    if str(cfg["logging"]["level"]).lower() not in LOG_LEVELS:
        problems.append(f"unknown logging.level {cfg['logging']['level']!r}")
    deploy = cfg["deploy"]
    if deploy["defaultEnvironment"] not in deploy["allowedEnvironments"]:
        problems.append("deploy.defaultEnvironment is not an allowed environment")
    if not 1 <= cfg["uploads"]["maxUploadMB"] <= 100:
        problems.append("uploads.maxUploadMB must be between 1 and 100")
    return problems


def load(path=CONFIG_PATH):
    with open(path, encoding="utf-8") as f:
        cfg = json.load(f)
    problems = validate(cfg)
    if problems:
        raise ValueError("invalid config: " + "; ".join(problems))
    return cfg


if __name__ == "__main__":
    cfg = load()
    print(f"{cfg['app']['name']} {cfg['app']['version']}: config OK")
