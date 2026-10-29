"""Stage the project-owned Harness bridge and pinned upstream into the app."""
from pathlib import Path
import shutil


def stage(root: Path):
    resources = root / "dist/Gemma Desktop.app/Contents/Resources"
    for relative in ("scripts/harness", "third_party/harness2"):
        source = root / relative
        if not source.is_dir():
            raise RuntimeError(f"Missing Harness resource: {relative}")
        destination = resources / relative
        if destination.exists():
            shutil.rmtree(destination)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(source, destination, ignore=shutil.ignore_patterns("__pycache__", "*.pyc", ".DS_Store"))


if __name__ == "__main__":
    stage(Path(__file__).resolve().parents[1])
