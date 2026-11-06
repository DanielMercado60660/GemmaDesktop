#!/usr/bin/env python3
"""Package verified managed runtimes, or explicitly omit them for Ollama-only builds."""
import argparse
from pathlib import Path
import shutil
import tempfile

from runtime_resources import (BUILD_MODES, assert_no_packaged_runtimes,
                               resource_inventory, source_directory, validate_build_mode)


def remove_owned_output(path):
    if path.is_symlink() or path.is_file():
        path.unlink()
    elif path.exists():
        shutil.rmtree(path)


def package(root, build_mode="full-installer"):
    validate_build_mode(build_mode)
    source = source_directory(root) if build_mode == "full-installer" else None
    # Validate full installer inputs before altering any existing bundle.
    expected = resource_inventory(source) if source is not None else {}
    resources = root / "dist/Gemma Desktop.app/Contents/Resources"
    # Never follow a linked bundle ancestor when removing owned runtime output.
    for relative in ("dist", "dist/Gemma Desktop.app", "dist/Gemma Desktop.app/Contents",
                     "dist/Gemma Desktop.app/Contents/Resources"):
        directory = root / relative
        if directory.is_symlink():
            raise RuntimeError("App bundle output directories must not be symlinks")
        directory.mkdir(exist_ok=True)
    destination = resources / "runtimes"
    backup = resources / "runtimes-previous"
    if build_mode == "ollama-only":
        remove_owned_output(destination)
        remove_owned_output(backup)
        # Unexpected staging residue fails closed; unrelated resources stay intact.
        assert_no_packaged_runtimes(resources)
        return expected
    stage = Path(tempfile.mkdtemp(prefix="runtimes-stage-", dir=resources))
    try:
        for name in expected:
            target = stage / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source / name, target)
        if resource_inventory(stage) != expected:
            raise RuntimeError("Packaged runtime resource inventory changed during copying")
        remove_owned_output(backup)
        if destination.exists() or destination.is_symlink():
            destination.rename(backup)
        stage.rename(destination)
        remove_owned_output(backup)
    except Exception:
        if not destination.exists() and backup.exists():
            backup.rename(destination)
        raise
    finally:
        remove_owned_output(stage)
    return expected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build-mode", choices=BUILD_MODES, default="full-installer")
    args = parser.parse_args()
    try:
        expected = package(Path(__file__).resolve().parent.parent, args.build_mode)
    except (OSError, ValueError, RuntimeError) as error:
        parser.exit(1, f"Runtime packaging failed: {error}\n")
    if args.build_mode == "ollama-only":
        print("Ollama-only build: managed runtime resources explicitly omitted")
    else:
        print(f"Packaged {len(expected) - 1} verified runtime resource files")


if __name__ == "__main__":
    main()
