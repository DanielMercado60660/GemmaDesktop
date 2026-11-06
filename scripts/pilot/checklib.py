"""Helpers for independent task checkers.

Candidate code runs read-only under macOS sandbox-exec with no network; only a
private temporary directory is writable. Trusted test files come from the task
directory, never from the candidate's workspace.
"""
from __future__ import annotations

import json
from pathlib import Path
import shutil
import subprocess
import tempfile

PYTHON = "/opt/homebrew/bin/python3"
READABLE = ["/System", "/usr", "/bin", "/sbin", "/Library/Apple", "/Library/Developer", "/Applications/Xcode.app", "/opt/homebrew", "/private/var/db/timezone", "/private/etc"]


def q(v) -> str:
    return json.dumps(str(v))


def sandboxed(argv: list[str], cwd: Path, extra_readable: list[Path] = (), timeout: int = 120, stdin: str | None = None) -> subprocess.CompletedProcess:
    with tempfile.TemporaryDirectory(prefix="gemma-pilot-check-") as tmp:
        private = Path(tmp).resolve()
        roots = READABLE + [str(Path(cwd).resolve()), str(private)] + [str(Path(p).resolve()) for p in extra_readable]
        rules = " ".join(f"(subpath {q(r)})" for r in roots)
        profile = f'''(version 1)
(deny default)
(import "dyld-support.sb")
(allow syscall*)
(allow mach-bootstrap)
(allow process-exec)
(allow process-fork)
(allow signal (target self))
(allow sysctl-read)
(allow mach-lookup (global-name "com.apple.system.logger") (global-name "com.apple.system.opendirectoryd"))
(allow file-read-metadata)
(allow file-read* file-map-executable {rules} (literal "/dev/null") (literal "/dev/urandom") (literal "/dev/random") (literal "/private/var/select/sh"))
(allow file-write* (subpath {q(private)}) (literal "/dev/null"))
'''
        env = {"PATH": "/opt/homebrew/bin:/usr/bin:/bin", "HOME": str(private), "TMPDIR": str(private), "PYTHONDONTWRITEBYTECODE": "1", "LANG": "en_US.UTF-8"}
        return subprocess.run(["/usr/bin/sandbox-exec", "-p", profile, *argv], cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout, input=stdin)


def with_trusted(final: Path, trusted: dict[str, Path]) -> Path:
    """Copy candidate files to a private directory and overlay trusted files."""
    tmp = Path(tempfile.mkdtemp(prefix="gemma-pilot-grade-"))
    shutil.copytree(final, tmp / "w", ignore=shutil.ignore_patterns("__pycache__"))
    for rel, src in trusted.items():
        dst = tmp / "w" / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(src, dst)
    return tmp / "w"


def result(name: str, passed: bool, detail: str = "", critical: bool = True) -> dict:
    return {"name": name, "passed": bool(passed), "critical": critical, "detail": detail[:3000]}


def unchanged(final: Path, original: dict, rels: list[str]) -> dict:
    import hashlib
    bad = [r for r in rels if not (final / r).is_file() or hashlib.sha256((final / r).read_bytes()).hexdigest() != original.get(r)]
    return result("protected inputs unchanged", not bad, "changed: " + ", ".join(bad) if bad else "")


def python_probe(final: Path, code: str, timeout: int = 60) -> tuple[bool, str]:
    """Run trusted probe code against the candidate package; it prints JSON."""
    r = sandboxed([PYTHON, "-I", "-c", "import sys; sys.path.insert(0, '.')\n" + code], final, timeout=timeout)
    return r.returncode == 0, (r.stdout + r.stderr)[-3000:]
