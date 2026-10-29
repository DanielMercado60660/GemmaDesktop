#!/usr/bin/env python3
"""Explicitly fetch the pinned public TypeScript archive and build the coding image.

This is a user-run setup operation, never an application/runtime fallback.
"""
import argparse
import base64
import hashlib
import hmac
import json
from pathlib import Path
import re
import shlex
import shutil
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


TYPESCRIPT_URL = "https://registry.npmjs.org/typescript/-/typescript-5.9.3.tgz"
TYPESCRIPT_SRI = "sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw=="
MAX_ARCHIVE_BYTES = 16 << 20
IMAGE_TITLE = "Gemma Desktop isolated coding toolchain"
OFFICIAL_IMAGES = [
    "node@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d",
    "golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af",
    "python@sha256:21206b8f02efda6d737e402e1c716b650ad30605843a36e4bcb55463a7ebdf37",
]
SAFE_PATH = "/usr/local/bin:/opt/homebrew/bin:/Applications/Docker.app/Contents/Resources/bin:/usr/bin:/bin"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("TypeScript download redirects are not allowed")


def verify_dockerfile(data):
    text = data.decode("utf-8")
    from_lines = [line for line in text.splitlines() if re.match(r"^FROM\s", line)]
    images = []
    for line in from_lines:
        match = re.fullmatch(r"FROM (\S+)(?: AS [a-z][a-z0-9_-]*)?", line)
        if not match:
            raise RuntimeError("Unsupported Dockerfile FROM syntax; review pinned setup helper")
        images.append(match.group(1))
    if images != OFFICIAL_IMAGES:
        raise RuntimeError("Dockerfile official image digests differ from the reviewed setup pins")
    checksums = re.findall(r"^# typescript5\.9\.3 (sha512-\S+)$", text, re.MULTILINE)
    if checksums != [TYPESCRIPT_SRI]:
        raise RuntimeError("Dockerfile TypeScript 5.9.3 checksum differs from the setup pin")
    # Build-wide --network=none must not be overridden by a later recipe edit.
    if re.search(r"--network\s*=", text, re.IGNORECASE):
        raise RuntimeError("Per-instruction network overrides are not supported")
    return TYPESCRIPT_SRI


def read_verified_archive(response, expected_sri, *, limit=MAX_ARCHIVE_BYTES):
    if response.status != 200 or response.geturl() != TYPESCRIPT_URL:
        raise RuntimeError("TypeScript download did not return the exact pinned HTTPS resource")
    length = response.headers.get("Content-Length")
    if length is not None and (not length.isdecimal() or int(length) > limit):
        raise RuntimeError("TypeScript archive exceeds the download size limit")
    if response.headers.get("Content-Encoding", "identity") != "identity":
        raise RuntimeError("Unexpected TypeScript response encoding")
    deadline = time.monotonic() + 180
    chunks, size = [], 0
    digest = hashlib.sha512()
    while True:
        if time.monotonic() >= deadline:
            raise RuntimeError("TypeScript download exceeded its total deadline")
        chunk = response.read(min(64 << 10, limit + 1 - size))
        if not chunk:
            break
        size += len(chunk)
        if size > limit:
            raise RuntimeError("TypeScript archive exceeds the download size limit")
        chunks.append(chunk)
        digest.update(chunk)
    actual = "sha512-" + base64.b64encode(digest.digest()).decode("ascii")
    if not hmac.compare_digest(actual, expected_sri):
        raise RuntimeError("TypeScript SHA512 integrity verification failed; no build context created")
    return b"".join(chunks)


def download_archive(expected_sri):
    # No ambient proxy, package-manager config, cookies, netrc or auth headers.
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context()),
    )
    request = urllib.request.Request(TYPESCRIPT_URL, headers={"Accept-Encoding": "identity", "User-Agent": "GemmaDesktop-explicit-toolchain-setup/1"})
    with opener.open(request, timeout=30) as response:
        return read_verified_archive(response, expected_sri)


def prepare_context(root, dockerfile, archive):
    context = root / "build-context"
    context.mkdir(mode=0o700)
    # These are the only bytes sent to the builder. The repo and home directory
    # are never used as build contexts, and credentials are not copied.
    (context / "Dockerfile").write_bytes(dockerfile)
    (context / "typescript.tgz").write_bytes(archive)
    return context


def build_command(docker, host, context, iidfile):
    return [docker, "--host", host, "build", "--platform=linux/arm64", "--network=none", "--pull", "--iidfile", str(iidfile), "--tag", "gemma-desktop-toolchain:local", "--file", str(context / "Dockerfile"), str(context)]


def docker_environment(root):
    config, home = root / "docker-config", root / "home"
    config.mkdir(mode=0o700)
    home.mkdir(mode=0o700)
    # Docker Desktop's bundled buildx is outside user config on macOS. Make
    # that reviewed plugin location visible without copying ~/.docker/config.
    (config / "config.json").write_text(json.dumps({"cliPluginsExtraDirs": ["/Applications/Docker.app/Contents/Resources/cli-plugins"]}))
    return {"PATH": SAFE_PATH, "HOME": str(home), "DOCKER_CONFIG": str(config), "DOCKER_BUILDKIT": "1", "LANG": "C.UTF-8"}


def validate_inspected_image(value, expected_id):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", expected_id):
        raise RuntimeError("Builder did not produce an immutable SHA256 image ID")
    if value.get("Id") != expected_id or value.get("Os") != "linux" or value.get("Architecture") != "arm64":
        raise RuntimeError("Inspected image identity/platform differs from the requested build")
    if (value.get("Config", {}).get("Labels") or {}).get("org.opencontainers.image.title") != IMAGE_TITLE:
        raise RuntimeError("Inspected image lacks the expected Gemma Desktop toolchain label")
    return expected_id


def main():
    parser = argparse.ArgumentParser(description=__doc__, epilog="Downloads the pinned public npm archive and official base images; build RUN instructions have network disabled. Docker Desktop must already be running. No app settings are changed.")
    parser.parse_args()
    try:
        docker = shutil.which("docker", path=SAFE_PATH)
        if not docker:
            raise RuntimeError("Install Docker Desktop and start it explicitly before setup")
        socket = Path.home() / ".docker/run/docker.sock"
        if not socket.is_socket():
            raise RuntimeError("Docker Desktop's local socket is unavailable; start Docker Desktop explicitly")
        recipe = Path(__file__).resolve().parents[1] / "internal/execution/toolchain.Dockerfile"
        dockerfile = recipe.read_bytes()
        sri = verify_dockerfile(dockerfile)
        print(f"Fetch only {TYPESCRIPT_URL}\nVerify {sri}\nOfficial image pins: " + ", ".join(OFFICIAL_IMAGES), file=sys.stderr)
        archive = download_archive(sri)
        # The archive has already passed exact SHA512 verification.
        with tempfile.TemporaryDirectory(prefix="gemma-toolchain-") as temporary:
            root = Path(temporary)
            context = prepare_context(root, dockerfile, archive)
            env = docker_environment(root)
            host, iidfile = "unix://" + str(socket), root / "image-id"
            command = build_command(docker, host, context, iidfile)
            print("Build operation: " + shlex.join(command), file=sys.stderr)
            subprocess.run(command, cwd=context, env=env, stdin=subprocess.DEVNULL, stdout=sys.stderr, check=True, timeout=1800)
            image_id = iidfile.read_text().strip()
            if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
                raise RuntimeError("Invalid builder image ID")
            inspected = subprocess.run([docker, "--host", host, "image", "inspect", "--format", "{{json .}}", image_id], cwd=root, env=env, stdin=subprocess.DEVNULL, check=True, capture_output=True, text=True, timeout=30)
            identity = validate_inspected_image(json.loads(inspected.stdout), image_id)
            print(json.dumps({"imageId": identity, "platform": "linux/arm64", "typescriptIntegrity": sri, "dockerfileSHA256": hashlib.sha256(dockerfile).hexdigest(), "tag": "gemma-desktop-toolchain:local", "note": "Select this inspected image in Gemma Desktop Execution settings. Setup does not change app settings or promise bit-for-bit reproducible image bytes."}, indent=2))
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, urllib.error.URLError) as error:
        parser.exit(1, f"Toolchain setup failed: {error}\n")


if __name__ == "__main__":
    main()
