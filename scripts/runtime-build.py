#!/usr/bin/env python3
"""Build private, relocatable runtime bundles. Never installs into system Python.

The first release-build resolves wheels into a local wheelhouse and writes a
complete hash lock. Later runs install that wheelhouse offline. End-user
installation only verifies/extracts the resulting archive.
"""
import argparse
import csv
import fcntl
import gzip
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.parse
import urllib.request
import zipfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
INPUTS = json.loads((ROOT / "packaging/runtimes/inputs.json").read_text())
WORK = ROOT / ".cache/runtime-build"
DOWNLOADS = WORK / "downloads"
RESOURCES = ROOT / "dist/runtime-resources"


def sha(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def run(args, **kwargs):
    print("Running", pathlib.Path(str(args[0])).name, *map(str, args[1:4]), flush=True)
    subprocess.run(list(map(str, args)), check=True, **kwargs)


def download(pin):
    DOWNLOADS.mkdir(parents=True, exist_ok=True)
    name = pathlib.Path(urllib.parse.unquote(urllib.parse.urlparse(pin["url"]).path)).name
    destination = DOWNLOADS / name
    if destination.exists() and destination.stat().st_size == pin["size"] and sha(destination) == pin["sha256"]:
        return destination
    temporary = destination.with_suffix(destination.suffix + ".part")
    print("Downloading pinned", name, flush=True)
    request = urllib.request.Request(pin["url"], headers={"User-Agent": "GemmaDesktop-runtime-builder", "Accept-Encoding": "identity"})
    with urllib.request.urlopen(request, timeout=60) as response, temporary.open("wb") as target:
        size = 0
        while chunk := response.read(1024 * 1024):
            size += len(chunk)
            if size > pin["size"]:
                raise ValueError("Pinned input exceeded its expected size")
            target.write(chunk)
    if temporary.stat().st_size != pin["size"] or sha(temporary) != pin["sha256"]:
        raise ValueError("Pinned input checksum/size mismatch: " + name)
    temporary.replace(destination)
    return destination


def extract(archive, destination):
    destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:gz") as source:
        # Only trusted, hash-pinned build inputs reach this extraction. Python's
        # data filter independently rejects escapes and unsafe links/devices.
        source.extractall(destination, filter="data")


def wheel_lock(wheelhouse, lock_path):
    entries = []
    for wheel_path in sorted(wheelhouse.glob("*.whl")):
        with zipfile.ZipFile(wheel_path) as wheel:
            metadata_names = [name for name in wheel.namelist() if name.endswith(".dist-info/METADATA")]
            if len(metadata_names) != 1:
                raise ValueError("Wheel metadata is ambiguous: " + wheel_path.name)
            metadata = wheel.read(metadata_names[0]).decode()
        name = next(line[6:] for line in metadata.splitlines() if line.startswith("Name: "))
        version = next(line[9:] for line in metadata.splitlines() if line.startswith("Version: "))
        entries.append(f"{name}=={version} --hash=sha256:{sha(wheel_path)}")
    if not entries:
        raise ValueError("Cannot create an empty dependency lock")
    lock_path.write_text("# Generated from the exact runtime wheelhouse; install with --no-deps.\n" + "\n".join(entries) + "\n")


def package_tree(tree, provider, build_id, source_sha, minimum_os, patch_sha="", evidence=None):
    if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,95}", build_id):
        raise ValueError("Build ID must be a short lowercase artifact identifier")
    RESOURCES.mkdir(parents=True, exist_ok=True)
    files = []
    for path in sorted(tree.rglob("*")):
        if path.is_file():
            # Dereference trusted build-input links into plain files: the app
            # installer accepts no links and never reconstructs absolute paths.
            files.append({"path": path.relative_to(tree).as_posix(), "sha256": sha(path), "size": path.stat().st_size, **({"executable": True} if path.stat().st_mode & 0o111 else {})})
        elif path.is_symlink() and path.is_dir():
            raise ValueError("Directory symlink must be normalized before bundling: " + str(path))
    executable = "python/bin/python3.12" if provider == "omlx" else "mlx-serve"
    catalog_path = RESOURCES / "catalog.json"
    catalog = json.loads(catalog_path.read_text()) if catalog_path.exists() else {"version": 1, "runtimes": [], "models": []}
    existing = next((item for item in catalog["runtimes"] if item["id"] == build_id), None)
    identity = {"provider": provider, "sourceSha": source_sha, "architecture": "arm64", "minimumOs": minimum_os, "files": files, "executable": executable}
    if existing:
        if any(existing.get(key) != value for key, value in identity.items()) or existing.get("patchSha256", "") != patch_sha:
            raise ValueError("Build ID is immutable; changed content requires --build-id with a new version")
        archive = RESOURCES / existing["bundle"]["bundledPath"]
        if not archive.is_file() or archive.stat().st_size != existing["bundle"]["size"] or sha(archive) != existing["bundle"]["sha256"]:
            raise ValueError("Existing immutable runtime bundle is missing or corrupt; restore the verified artifact")
        print(json.dumps({"buildId": build_id, "reusedVerifiedBundle": True}), flush=True)
        return
    archive = RESOURCES / (build_id + ".tar.gz")
    temporary = archive.with_suffix(archive.suffix + ".part")
    with temporary.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed, tarfile.open(fileobj=compressed, mode="w", dereference=True) as output:
        for file in files:
            info = tarfile.TarInfo(file["path"])
            info.size = file["size"]
            info.mode = 0o555 if file.get("executable") else 0o444
            info.mtime = 0
            with (tree / file["path"]).open("rb") as content:
                output.addfile(info, content)
    temporary.replace(archive)
    qualification = {"transport": "unverified", "coding": "experimental"}
    if evidence is not None:
        evidence_path = RESOURCES / (build_id + ".evidence.json")
        evidence_path.write_text(json.dumps(evidence, indent=2) + "\n")
        qualification = {"transport": "qualified", "evidenceSha256": sha(evidence_path), "coding": "experimental"}
    manifest = {"version": 1, "id": build_id, "provider": provider, "sourceSha": source_sha, "architecture": "arm64", "minimumOs": minimum_os, "bundle": {"bundledPath": archive.name, "sha256": sha(archive), "size": archive.stat().st_size}, "files": files, "executable": executable, "license": "Apache-2.0" if provider == "omlx" else "MIT AND Apache-2.0; bundled dependency notices apply", "readyForInference": evidence is not None, "qualification": qualification}
    if patch_sha:
        manifest["patchSha256"] = patch_sha
    if provider == "mlx_serve":
        profile = '(version 1)\n(allow default)\n(deny file-read* file-write* (subpath {{GLOBAL_STATE}}))\n'
        manifest["sandboxProfileSha256"] = hashlib.sha256(profile.encode()).hexdigest()
    catalog["runtimes"] = [item for item in catalog["runtimes"] if item["id"] != build_id] + [manifest]
    temporary_catalog = catalog_path.with_suffix(".json.tmp")
    temporary_catalog.write_text(json.dumps(catalog, indent=2) + "\n")
    temporary_catalog.replace(catalog_path)
    print(json.dumps({"buildId": build_id, "archive": str(archive), "sha256": manifest["bundle"]["sha256"], "bytes": manifest["bundle"]["size"], "qualification": qualification}), flush=True)


def build_omlx(resolve, build_id=None):
    python_archive = download(INPUTS["python"])
    upstream = download(INPUTS["omlx"])
    bootstrap = WORK / "bootstrap"
    if not (bootstrap / "python/bin/python3.12").exists():
        extract(python_archive, bootstrap)
    python = bootstrap / "python/bin/python3.12"
    wheelhouse = WORK / "wheelhouse"
    wheelhouse.mkdir(parents=True, exist_ok=True)
    patched = WORK / "patched" / upstream.name
    run([sys.executable, ROOT / "scripts/runtime-patch-omlx.py", upstream, patched])
    lock = ROOT / "packaging/runtimes/omlx-requirements.lock"
    if resolve:
        # This is a release BUILD operation in isolated app-owned directories.
        # The resulting wheelhouse/hash lock, not the resolver, ships to users.
        run([python, "-I", "-m", "pip", "wheel", "--wheel-dir", wheelhouse, "--prefer-binary", "--no-cache-dir", patched])
        wheel_lock(wheelhouse, lock)
    if not lock.exists():
        raise ValueError("A complete dependency lock is required; first build with --resolve")
    stage = WORK / "omlx-runtime"
    if stage.exists():
        shutil.rmtree(stage)
    extract(python_archive, stage)
    runtime_python = stage / "python/bin/python3.12"
    run([runtime_python, "-I", "-m", "pip", "install", "--no-index", "--no-deps", "--require-hashes", "--only-binary=:all:", "--find-links", wheelhouse, "-r", lock])
    run([runtime_python, "-I", "-m", "pip", "check"])
    environment = {key: value for key, value in os.environ.items() if key in ("HOME", "USER", "LOGNAME", "LANG", "__CF_USER_TEXT_ENCODING")}
    environment.update(PATH="/usr/bin:/bin", HF_HUB_OFFLINE="1", TRANSFORMERS_OFFLINE="1", HF_HOME=str(WORK / "probe-hf"), OMLX_BASE_PATH=str(WORK / "probe-omlx"))
    # Prove relocation by moving the completed prefix before any probe.
    relocated = WORK / "omlx-relocated"
    if relocated.exists():
        shutil.rmtree(relocated)
    stage.rename(relocated)
    runtime_python = relocated / "python/bin/python3.12"
    run([runtime_python, "-I", "-B", "-m", "omlx.cli", "--version"], env=environment)
    check = subprocess.run([str(runtime_python), "-I", "-B", str(ROOT / "scripts/runtime-template-check.py")], env=environment, check=True, capture_output=True, text=True)
    template_evidence = json.loads(check.stdout.strip().splitlines()[-1])
    # Remove interpreter-generated bytecode and stale entrypoint scripts that
    # contain build paths. All runtime launches use the private interpreter.
    for cache in list(relocated.rglob("__pycache__")):
        shutil.rmtree(cache)
    for script in (relocated / "python/bin").iterdir():
        if script.is_file() and script.read_bytes()[:2] == b"#!" and b"runtime-build" in script.read_bytes().split(b"\n", 1)[0]:
            script.unlink()
    patch = sha(ROOT / "scripts/runtime-patch-omlx.py")
    evidence = {"sourceSha": INPUTS["omlx"]["sourceSha"], "patchSha256": patch, "dependencyLockSha256": sha(lock), "relocatedOfflineVersionProbe": True, "templateChecks": template_evidence, "scope": "Offline package/template checks only; model load and live protocol qualification pending"}
    # Model-free probes alone do not mark the managed server ready. A later
    # explicit qualification step must add actual launch/protocol evidence.
    (WORK / "omlx-build-evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
    package_tree(relocated, "omlx", build_id or "omlx-4213c328-gemma1", INPUTS["omlx"]["sourceSha"], "15.0", patch_sha=patch)


def build_mlxserve(build_id=None):
    archive = download(INPUTS["mlxServe"])
    stage = WORK / "mlx-serve-runtime"
    if stage.exists():
        shutil.rmtree(stage)
    extract(archive, stage)
    tree = stage / "mlx-serve-macos-arm64"
    run(["/usr/bin/codesign", "--verify", "--deep", "--strict", tree / "mlx-serve"])
    run([tree / "mlx-serve", "--version"])
    package_tree(tree, "mlx_serve", build_id or "mlx-serve-26.11.1", INPUTS["mlxServe"]["sourceSha"], "26.2")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("provider", choices=("download", "omlx", "mlx-serve", "all"))
    parser.add_argument("--resolve", action="store_true", help="Resolve/build the initial wheelhouse in isolated build directories")
    parser.add_argument("--build-id", help="New immutable identity when changing runtime contents")
    args = parser.parse_args()
    WORK.mkdir(parents=True, exist_ok=True)
    if args.build_id and args.provider not in ("omlx", "mlx-serve"):
        parser.error("--build-id requires exactly one provider")
    build_lock = (WORK / "build.lock").open("a")
    fcntl.flock(build_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    if args.provider == "download":
        for name in ("python", "omlx", "mlxServe"):
            download(INPUTS[name])
    if args.provider in ("omlx", "all"):
        build_omlx(args.resolve, args.build_id)
    if args.provider in ("mlx-serve", "all"):
        build_mlxserve(args.build_id)


if __name__ == "__main__":
    main()
