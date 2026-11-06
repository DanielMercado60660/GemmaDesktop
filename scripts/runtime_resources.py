"""Shared strict inventory checks for packaged managed-runtime resources."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import tarfile

BUILD_MODES = ("full-installer", "ollama-only")


def validate_build_mode(mode):
    if mode not in BUILD_MODES:
        raise ValueError("Unknown build mode: " + str(mode))
    return mode


def runtime_input_inventory(root, build_mode):
    validate_build_mode(build_mode)
    return resource_inventory(source_directory(root)) if build_mode == "full-installer" else {}


def assert_no_packaged_runtimes(resources):
    # Failed/interrupted full builds may leave a previous inventory or staging
    # directory. An Ollama-only manifest must never attest to those bytes.
    if any(path.name in ("runtimes", "runtimes-previous") or path.name.startswith("runtimes-stage-")
           for path in resources.iterdir()):
        raise RuntimeError("Ollama-only build contains managed runtime resources")


def file_digest(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def source_directory(root):
    configured = os.environ.get("GEMMA_RUNTIME_BUNDLES")
    directory = Path(configured) if configured else root / "dist/runtime-resources"
    if not directory.is_absolute():
        raise RuntimeError("GEMMA_RUNTIME_BUNDLES must be absolute")
    return directory


def resource_inventory(directory):
    catalog_path = directory / "catalog.json"
    if not catalog_path.is_file():
        raise RuntimeError("Managed runtime catalog is missing; build verified runtime resources before packaging")
    catalog = json.loads(catalog_path.read_text())
    if catalog.get("version") != 1 or not catalog.get("runtimes"):
        raise RuntimeError("A full installer build requires a nonempty runtime catalog")
    inventory = {"catalog.json": file_digest(catalog_path)}
    root = directory.resolve()
    seen = set()
    for manifest in catalog["runtimes"]:
        if manifest.get("version") != 1 or manifest.get("provider") not in ("omlx", "mlx_serve") or manifest.get("id") in seen:
            raise RuntimeError("Invalid or duplicate runtime manifest")
        seen.add(manifest["id"])
        if not manifest.get("files") or not manifest.get("executable"):
            raise RuntimeError("Runtime manifest lacks a complete executable inventory")
        bundle = manifest["bundle"]
        relative = PurePosixPath(bundle.get("bundledPath", ""))
        if not str(relative) or str(relative) == "." or relative.is_absolute() or ".." in relative.parts or bundle.get("url"):
            raise RuntimeError("Full installer runtime must use a safe app-bundled artifact")
        artifact = (root / str(relative)).resolve()
        if not artifact.is_relative_to(root) or not artifact.is_file():
            raise RuntimeError("Runtime bundle escapes or is absent from resources")
        if artifact.stat().st_size != bundle["size"] or file_digest(artifact) != bundle["sha256"]:
            raise RuntimeError("Runtime bundle size or digest differs from its trusted catalog")
        expected_files = {entry["path"]: entry for entry in manifest["files"]}
        if len(expected_files) != len(manifest["files"]) or len({name.lower() for name in expected_files}) != len(expected_files):
            raise RuntimeError("Runtime file inventory contains duplicate paths")
        seen_files = set()
        with tarfile.open(artifact, "r:gz") as archive:
            for entry in archive:
                name = PurePosixPath(entry.name)
                if name.is_absolute() or ".." in name.parts:
                    raise RuntimeError("Runtime archive contains an unsafe path")
                if entry.isdir():
                    if not any(path.startswith(str(name) + "/") for path in expected_files):
                        raise RuntimeError("Runtime archive contains an undeclared directory")
                    continue
                expected = expected_files.get(str(name))
                if not entry.isfile() or expected is None or str(name) in seen_files or entry.size != expected["size"]:
                    raise RuntimeError("Runtime archive differs from its complete file inventory")
                digest = hashlib.sha256()
                with archive.extractfile(entry) as data:
                    for chunk in iter(lambda: data.read(1 << 20), b""):
                        digest.update(chunk)
                if digest.hexdigest() != expected["sha256"] or bool(entry.mode & 0o111) != bool(expected.get("executable")):
                    raise RuntimeError("Runtime archive file content/mode differs from inventory")
                seen_files.add(str(name))
        if seen_files != expected_files.keys():
            raise RuntimeError("Runtime archive omitted declared files")
        inventory[str(relative)] = bundle["sha256"]
        if manifest.get("readyForInference"):
            evidence = root / (manifest["id"] + ".evidence.json")
            expected = manifest.get("qualification", {}).get("evidenceSha256")
            if not evidence.is_file() or file_digest(evidence) != expected:
                raise RuntimeError("Runtime transport qualification evidence is missing or changed")
            inventory[evidence.name] = expected
    return inventory
