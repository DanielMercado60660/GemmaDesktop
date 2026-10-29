#!/usr/bin/env python3
"""Capture build inputs before compilation; verify the same tree after signing.

This checks two snapshots of the local source/assets, not a hermetic or
reproducible build. Keep the workspace frozen between capture and verification.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess


def file_digest(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def fingerprint(files):
    return hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()


def input_snapshot(root):
    paths = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root
    ).split(b"\0")
    names = {os.fsdecode(name) for name in paths if name}
    assets = root / "frontend/dist"
    if not (assets / "index.html").is_file():
        raise RuntimeError("frontend/dist/index.html is missing; build the frontend before capture")
    # These generated files are ignored by Git but embedded by the Go build.
    names.update(path.relative_to(root).as_posix() for path in assets.rglob("*") if path.is_file())
    sources = {}
    for name in sorted(names):
        path = root / name
        if name.startswith(("docs/", "design/")) or not path.is_file():
            continue
        sources[name] = file_digest(path)
        if path.is_symlink():
            # Include both the link identity and the bytes consumed by the build.
            sources[name + " [symlink]"] = hashlib.sha256(os.fsencode(os.readlink(path))).hexdigest()
    return {
        "schemaVersion": 1,
        "capturedAt": datetime.now(timezone.utc).isoformat(),
        "sourceCommit": subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True
        ).strip(),
        "sourceDirty": bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=root)),
        "sourceFingerprint": fingerprint(sources),
        "frontendFingerprint": fingerprint({name: value for name, value in sources.items() if name.startswith("frontend/dist/")}),
        "sourceFiles": sources,
    }


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def verify_harness_resources(root):
    """Verify actual packaged Python bytes, not only their source locations."""
    resources = root / "dist/Gemma Desktop.app/Contents/Resources"
    hashes = {}
    for relative in ("scripts/harness", "third_party/harness2"):
        source = root / relative
        if not source.exists():
            continue
        expected = {}
        for path in source.rglob("*"):
            if not path.is_file() or "__pycache__" in path.parts or path.suffix == ".pyc" or path.name == ".DS_Store":
                continue
            expected[path.relative_to(source).as_posix()] = file_digest(path)
        destination = resources / relative
        actual = {path.relative_to(destination).as_posix(): file_digest(path)
                  for path in destination.rglob("*") if path.is_file()}
        if actual != expected:
            raise RuntimeError(f"Packaged Harness resources differ from source: {relative}")
        hashes.update({relative + "/" + name: digest for name, digest in actual.items()})
    return hashes


def capture(root):
    # A failed later build must not leave the previous build's manifest looking current.
    (root / "dist/build-info.json").unlink(missing_ok=True)
    snapshot = input_snapshot(root)
    write_json(root / "dist/build-inputs.json", snapshot)
    return snapshot


def verify(root):
    manifest = root / "dist/build-info.json"
    manifest.unlink(missing_ok=True)
    capture_path = root / "dist/build-inputs.json"
    if not capture_path.is_file():
        raise RuntimeError("build inputs were not captured before compilation; rerun scripts/build.sh")
    before = json.loads(capture_path.read_text())
    if before.get("schemaVersion") != 1 or not isinstance(before.get("sourceFiles"), dict):
        raise RuntimeError("invalid build-input snapshot; rerun scripts/build.sh")
    after = input_snapshot(root)
    changed = sorted(name for name in before["sourceFiles"].keys() | after["sourceFiles"].keys()
                     if before["sourceFiles"].get(name) != after["sourceFiles"].get(name))
    if (changed or before["sourceCommit"] != after["sourceCommit"]
            or before["sourceFingerprint"] != after["sourceFingerprint"]):
        detail = ", ".join(changed[:10]) or "Git HEAD changed"
        raise RuntimeError("build input drift detected after compilation; no manifest written: " + detail)
    binary = root / "dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop"
    resource_hashes = verify_harness_resources(root)
    info = {
        "builtAt": datetime.now(timezone.utc).isoformat(),
        "inputsCapturedAt": before["capturedAt"],
        "inputsVerifiedUnchanged": True,
        "sourceCommit": before["sourceCommit"],
        "sourceDirty": before["sourceDirty"],
        "sourceDirtyAfterBuild": after["sourceDirty"],
        "sourceFingerprint": before["sourceFingerprint"],
        "frontendFingerprint": before["frontendFingerprint"],
        "binarySHA256": file_digest(binary),
        "harnessResourceFingerprint": fingerprint(resource_hashes),
        "harnessResourceFiles": resource_hashes,
        "signing": "local ad-hoc; not notarized",
        "provenanceScope": "Source and generated frontend input trees matched before compilation and after signing; not a hermetic build attestation.",
        "sourceFiles": before["sourceFiles"],
    }
    write_json(manifest, info)
    return info


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("capture", "verify"))
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    try:
        (capture if args.mode == "capture" else verify)(root)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Build provenance failed: {error}\n")


if __name__ == "__main__":
    main()
