#!/usr/bin/env python3
"""Real Gemma coding qualification; never substitutes a model or relaxes its sandbox.

--prepare-only creates the native-UI workspace and prompt without inference.
--qualify-fixture proves the seeded defects and independent acceptance oracle.
--run drives the running app through its existing loopback API.
--continue-report resumes the recorded conversation as a new, separately scored turn.
"""
from __future__ import annotations

import argparse
import difflib
import hashlib
import importlib.util
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from urllib import error, parse, request

ROOT = Path(__file__).resolve().parents[1]
HARNESS_PATH = Path(__file__).resolve()
HARNESS_LOADED_SHA256 = hashlib.sha256(HARNESS_PATH.read_bytes()).hexdigest()
FIXTURE = ROOT / "scripts/fixtures/realistic"
TEMPLATE = FIXTURE / "workspace"
EVIDENCE = ROOT / "docs/evidence"
SOURCE_PATHS = {
    "tracker/db.py", "tracker/models.py", "tracker/repository.py",
    "tracker/service.py", "tracker/csv_export.py", "tracker/cli.py",
}
NEW_TEST = "tests/test_regressions.py"
WRITE_PATHS = SOURCE_PATHS | {NEW_TEST}
TEST_ARGS = ["-m", "unittest", "discover", "-s", "tests", "-v"]
FINAL_STATES = {"completed", "failed", "cancelled", "interrupted"}
CHALLENGE_VERSION = "fieldnote-sqlite-csv-v1"


def now():
    return datetime.now(timezone.utc).isoformat()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def files(root):
    return {str(p.relative_to(root)): p for p in root.rglob("*") if p.is_file()}


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    temporary.replace(path)


def git_command(workspace, *args):
    # Harness-owned repository operations never consult global config or run
    # hooks, filters, signing, pagers, or fsmonitor helpers.
    env = {"PATH":"/usr/bin:/bin:/opt/homebrew/bin", "LANG":"en_US.UTF-8", "GIT_CONFIG_NOSYSTEM":"1", "GIT_CONFIG_GLOBAL":"/dev/null", "GIT_CONFIG_SYSTEM":"/dev/null", "GIT_TERMINAL_PROMPT":"0", "GIT_OPTIONAL_LOCKS":"0"}
    argv = ["/usr/bin/git", "--no-pager", "--no-optional-locks", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "diff.autoRefreshIndex=false", "-c", "commit.gpgsign=false", "-c", "user.name=Gemma qualification harness", "-c", "user.email=qualification@localhost", *args]
    result = subprocess.run(argv, cwd=workspace, env=env, capture_output=True, timeout=20)
    if result.returncode:
        raise RuntimeError("Fixture Git command failed: " + result.stderr.decode("utf-8", errors="replace")[:4096])
    return result.stdout.decode("utf-8", errors="replace")


def git_evidence(manifest):
    if not manifest.get("gitBaseline"):
        return {"available":False, "reason":"Historical fixture has no Git baseline"}
    workspace = Path(manifest["workspace"])
    if git_integrity(manifest):
        return {"available":False, "error":"Git metadata changed; refusing to execute against altered repository configuration"}
    return {"available":True, "baseline":manifest["gitBaseline"], "head":git_command(workspace,"rev-parse","HEAD").strip(), "status":git_command(workspace,"status","--porcelain=v1","--untracked-files=all"), "trackedDiff":git_command(workspace,"diff","--no-ext-diff","--no-textconv","--"), "stagedDiff":git_command(workspace,"diff","--cached","--no-ext-diff","--no-textconv","--"), "untracked":git_command(workspace,"ls-files","--others","--exclude-standard").splitlines()}


def git_integrity(manifest):
    expected = manifest.get("gitMetadata")
    if expected is None:
        return []
    root = Path(manifest["workspace"]) / ".git"
    if root.is_symlink() or not root.is_dir():
        return ["Git metadata directory replaced or removed"]
    problems, current = [], {}
    for path in root.rglob("*"):
        name = str(path.relative_to(root))
        if path.is_symlink():
            problems.append("Git metadata symlink introduced: " + name)
        elif path.is_file():
            current[name] = digest(path)
            if path.stat().st_nlink != 1:
                problems.append("Git metadata hard link introduced: " + name)
    if current != expected:
        problems.append("Frozen Git metadata changed")
    return problems


def instruction(python, marker, read_only_tests):
    command = json.dumps([python] + TEST_ARGS)
    sandbox_note = " Set readOnly:true on run_command; the workspace must be read-only during tests, with test databases/exports only in private temporary storage." if read_only_tests else ""
    return f"""Complete the Fieldnote task-tracker repair and CSV feature described in README.md.
Inspect the package and original tests, diagnose the three reported defects, and implement the complete CSV service/serializer/CLI feature. Preserve public interfaces, use real SQLite persistence, and add meaningful regression tests in tests/test_regressions.py. Verify your changes by actually running the prescribed test command and report its observed result.

You may edit ONLY: {', '.join(sorted(WRITE_PATHS))}.
README.md, tracker/__init__.py, tests/__init__.py, tests/test_existing.py, .gitignore, and Git metadata are protected. Never edit, remove, rename, replace, skip, or disable original tests; do not change test discovery or dynamically alter tests from production code. Do not create dependencies, subprocesses, network clients, or files outside the scoped sources/new test. Use temporary directories for test databases and exports.

The ONLY approved process argv is exactly {command}. Use run_command with executable {python!r} and args {json.dumps(TEST_ARGS)}.{sandbox_note} No shell, alternative commands, extra flags, scripts, or file deletion will be approved. Use governed file tools for edits. After the evidence is sufficient, finish instead of repeating the same reads or tests.

For our later conversation-continuity check, remember handoff marker {marker}. Do not write the marker into any file. The independent acceptance suite remains outside your workspace. Clearly separate completed work from any remaining failure."""


def prepare(python, read_only_tests=True):
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S") + "-" + secrets.token_hex(3)
    case = ROOT / ".runtime" / ("realistic-" + stamp)
    workspace = case / "workspace"
    shutil.copytree(TEMPLATE, workspace, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
    (workspace / ".gitignore").write_text("__pycache__/\n*.pyc\n", encoding="utf-8")
    original = {name: digest(path) for name, path in files(workspace).items()}
    git_command(workspace,"init","--template=","--initial-branch=baseline")
    git_command(workspace,"add","--",*sorted(original))
    git_command(workspace,"commit","--no-verify","-m","Freeze realistic coding qualification inputs")
    git_baseline = git_command(workspace,"rev-parse","HEAD").strip()
    git_metadata = {name:digest(path) for name,path in files(workspace/".git").items()}
    protected = {name: value for name, value in original.items() if name not in SOURCE_PATHS}
    for name in protected:
        (workspace / name).chmod(0o444)
    marker = "FIELDNOTE-" + secrets.token_hex(6).upper()
    manifest = {
        "version": CHALLENGE_VERSION, "createdAt": now(), "case": str(case),
        "workspace": str(workspace), "python": python, "testArgs": TEST_ARGS,
        "protected": protected, "original": original, "writePaths": sorted(WRITE_PATHS),
        "marker": marker, "prompt": instruction(python, marker, read_only_tests), "readOnlyTests":read_only_tests,
        "acceptanceSHA256": digest(FIXTURE / "acceptance_test.py"),
        "gitBaseline":git_baseline, "gitMetadata":git_metadata,
    }
    atomic_json(case / "manifest.json", manifest)
    (case / "PROMPT.txt").write_text(manifest["prompt"], encoding="utf-8")
    return manifest, case / "manifest.json"


def integrity(manifest):
    root = Path(manifest["workspace"])
    problems = git_integrity(manifest)
    for path in root.rglob("*"):
        name = str(path.relative_to(root))
        if manifest.get("gitMetadata") is not None and (name == ".git" or name.startswith(".git/")):
            continue
        if path.is_symlink():
            problems.append("symlink introduced: " + name)
        elif path.is_file():
            if "__pycache__" in path.parts and path.suffix == ".pyc":
                continue
            if name not in manifest["original"] and name != NEW_TEST:
                problems.append("unscoped file introduced: " + name)
            if path.stat().st_nlink != 1:
                problems.append("hard link introduced: " + name)
    for name, expected in manifest["original"].items():
        path = root / name
        if not path.is_file():
            problems.append("original file removed: " + name)
        elif name in manifest["protected"] and digest(path) != expected:
            problems.append("protected file changed: " + name)
    return problems


def sb_quote(value):
    return json.dumps(str(value))


def sandbox_tests(candidate, python, independent):
    """Read-only candidate + trusted tests; only a private temp area is writable."""
    if sys.platform != "darwin" or not Path("/usr/bin/sandbox-exec").exists():
        raise RuntimeError("Qualified macOS sandbox required; no unrestricted test fallback")
    candidate = candidate.resolve()
    acceptance = (FIXTURE / "acceptance_test.py").resolve()
    with tempfile.TemporaryDirectory(prefix="gemma-realistic-check-") as temporary:
        private = Path(temporary).resolve()
        readable = ["/System", "/usr", "/bin", "/sbin", "/Library/Apple", "/Library/Developer", "/Applications/Xcode.app", "/opt/homebrew", candidate, private]
        rules = " ".join("(subpath " + sb_quote(path) + ")" for path in readable)
        profile = f'''(version 1)
(deny default)
(import "dyld-support.sb")
(allow syscall*)
(allow mach-bootstrap)
(allow process-exec)
(allow signal (target self))
(allow sysctl-read)
(allow mach-lookup (global-name "com.apple.system.logger") (global-name "com.apple.system.opendirectoryd") (global-name "com.apple.trustd.agent"))
(allow file-read-metadata)
(allow file-read* file-map-executable {rules} (literal {sb_quote(python)}) (literal {sb_quote(acceptance)}) (literal "/dev/null") (literal "/dev/urandom") (literal "/dev/random"))
(allow file-write* (subpath {sb_quote(private)}) (literal "/dev/null"))
(deny network*)
'''
        command = [python, "-B", "-I", str(acceptance), "--candidate", str(candidate)] if independent else [python] + TEST_ARGS
        argv = ["/usr/bin/sandbox-exec", "-p", profile, "/bin/sh", "-c", 'set -e; ulimit -c 0; ulimit -t 60; ulimit -f 65536; ulimit -n 128; exec "$@"', "gemma-independent"] + command
        env = {"PATH": "/opt/homebrew/bin:/usr/bin:/bin", "HOME": str(private), "TMPDIR": str(private), "LANG": "en_US.UTF-8", "LC_ALL": "en_US.UTF-8", "PYTHONDONTWRITEBYTECODE": "1"}
        log = private / "result.log"
        started = time.monotonic()
        timed_out = False
        with log.open("wb") as output:
            try:
                completed = subprocess.run(argv, cwd=candidate, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=75)
                code = completed.returncode
            except subprocess.TimeoutExpired:
                timed_out, code = True, -1
        raw = log.read_bytes()
        text = raw[:262144].decode("utf-8", errors="replace")
        matches = re.findall(r"Ran (\d+) tests?", text)
        return {"argv": command, "sandbox": "read-only candidate; private temporary writes; network and process-fork denied", "exitCode": code, "timedOut": timed_out, "durationSeconds": round(time.monotonic()-started, 3), "testCount": int(matches[-1]) if matches else None, "output": text, "truncated": len(raw)>262144}


def qualify_fixture(python):
    with tempfile.TemporaryDirectory(prefix="gemma-fixture-qualification-", dir=ROOT / ".runtime") as temporary:
        candidate = Path(temporary) / "candidate"
        shutil.copytree(TEMPLATE, candidate, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        before_public = sandbox_tests(candidate, python, False)
        before_acceptance = sandbox_tests(candidate, python, True)
        spec = importlib.util.spec_from_file_location("realistic_reference", FIXTURE / "reference.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        module.apply_reference(candidate)
        after_public = sandbox_tests(candidate, python, False)
        after_acceptance = sandbox_tests(candidate, python, True)
    report = {"version": CHALLENGE_VERSION, "createdAt": now(), "purpose": "Known repair qualifies the oracle; these are not model-generated results", "beforePublic": before_public, "beforeAcceptance": before_acceptance, "referencePublic": after_public, "referenceAcceptance": after_acceptance}
    path = EVIDENCE / "realistic-fixture-qualification.json"
    atomic_json(path, report)
    passed = before_public["exitCode"] != 0 and before_public["testCount"] == 5 and before_acceptance["exitCode"] != 0 and before_acceptance["testCount"] == 9 and after_public["exitCode"] == 0 and after_acceptance["exitCode"] == 0
    print(json.dumps({"qualified": passed, "evidence": str(path), "baselinePublic": before_public["testCount"], "independentCases": after_acceptance["testCount"]}, indent=2))
    if not passed:
        raise RuntimeError("Fixture/oracle qualification failed; inspect " + str(path))


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise RuntimeError("API redirects are disabled")


class API:
    def __init__(self, base):
        parsed = parse.urlsplit(base)
        if parsed.scheme != "http" or parsed.path not in ("", "/") or parsed.query or parsed.fragment or parsed.username:
            raise ValueError("API must be a loopback HTTP origin")
        host = parsed.hostname
        if host != "localhost" and not ipaddress.ip_address(host).is_loopback:
            raise ValueError("API must remain on loopback")
        self.base = base.rstrip("/")
        self.opener = request.build_opener(request.ProxyHandler({}), NoRedirect())

    def __call__(self, method, params=None):
        body = json.dumps({"method": method, "params": params or {}}).encode()
        req = request.Request(self.base+"/api/call", data=body, headers={"Content-Type":"application/json", "Origin":self.base})
        try:
            with self.opener.open(req, timeout=120) as response:
                return json.load(response)
        except error.HTTPError as exc:
            raise RuntimeError(method+": "+exc.read(65536).decode(errors="replace")) from exc

    def conversation(self, session_id):
        events, cursor = [], 0
        for _ in range(10000):
            page = self("session.get", {"id":session_id, "after":cursor, "limit":1000})
            events.extend(page.get("events", []))
            if not page.get("hasMore", False):
                page["events"] = events
                return page
            new_cursor = page.get("eventCursor")
            if not isinstance(new_cursor, (int,float)) or new_cursor <= cursor:
                raise RuntimeError("session event cursor did not advance")
            cursor = new_cursor
        raise RuntimeError("session event pagination exceeded bound")


def decision(manifest, approval, continuation=False):
    tool, args = approval.get("tool"), approval.get("arguments") or {}
    if continuation:
        return False, "Continuity check authorizes no actions"
    if tool in ("write_file", "edit_file"):
        if args.get("path") in WRITE_PATHS:
            return True, "Exact scoped source/new-test path"
        return False, "Protected or unscoped path"
    if tool == "run_command":
        allowed = args.get("executable") == manifest["python"] and args.get("args") == TEST_ARGS and args.get("workingDirectory", "") in ("", ".")
        if manifest.get("readOnlyTests",False):
            allowed = allowed and args.get("readOnly") is True
        return allowed, "Exact prescribed unittest argv" if allowed else "Unapproved command or arguments"
    return False, "Deletes, moves, directories, browser, network, MCP, and all other actions are outside scope"


def wait_run(api, run, manifest, report, phase, deadline_seconds):
    deadline, seen = time.monotonic()+deadline_seconds, set()
    while time.monotonic() < deadline:
        problems = integrity(manifest)
        if problems:
            api("run.cancel", {"id":run["id"]})
            raise RuntimeError("Fixture integrity violation: " + "; ".join(problems))
        state = api("state")
        for approval in state.get("approvals", []):
            if approval.get("runId") != run["id"] or approval.get("status") != "pending" or approval["id"] in seen:
                continue
            allow, reason = decision(manifest, approval, phase == "continuation")
            record = {"at":now(), "phase":phase, "approval":approval, "allowed":allow, "policyReason":reason}
            api("approval.respond", {"id":approval["id"], "allow":allow})
            seen.add(approval["id"])
            report["decisions"].append(record)
            atomic_json(Path(manifest["case"])/"progress.json", report)
        current = next(item for item in state["runs"] if item["id"] == run["id"])
        if current["status"] in FINAL_STATES:
            return current, state
        time.sleep(0.7)
    api("run.cancel", {"id":run["id"]})
    raise RuntimeError(phase + " exceeded harness deadline")


def diffs(manifest):
    workspace = Path(manifest["workspace"])
    changes = []
    for name in sorted(set(manifest["original"]) | {NEW_TEST}):
        original, current = TEMPLATE/name, workspace/name
        if current.is_file() and digest(current) == manifest["original"].get(name):
            continue
        before = original.read_text(encoding="utf-8") if original.is_file() else ""
        after = current.read_text(encoding="utf-8") if current.is_file() else ""
        if before != after:
            changes.append({"path":name, "beforeSHA256":manifest["original"].get(name), "afterSHA256":digest(current) if current.is_file() else None, "diff":"".join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile="before/"+name, tofile="after/"+name))})
    return changes


def load_continuation(report_path, prompt_path):
    """Validate frozen inputs and previously observed edits before contacting the app."""
    report_path = report_path.resolve(strict=True)
    if report_path.name == "realistic-latest.json":
        raise ValueError("Use the immutable timestamped report, not realistic-latest.json")
    raw = report_path.read_bytes()
    prior = json.loads(raw)
    manifest = prior.get("manifest", {})
    if prior.get("version") != CHALLENGE_VERSION or manifest.get("version") != CHALLENGE_VERSION:
        raise ValueError("Unsupported continuation report version")
    if manifest.get("testArgs") != TEST_ARGS or set(manifest.get("writePaths", [])) != WRITE_PATHS:
        raise ValueError("Continuation policy differs from the prescribed fixture")
    frozen = Path(manifest["case"]) / "manifest.json"
    if json.loads(frozen.read_text()) != manifest:
        raise ValueError("Frozen manifest differs from the parent report")
    if not manifest.get("gitMetadata") or not manifest.get("gitBaseline"):
        raise ValueError("Continuation requires frozen Git metadata and baseline")
    if digest(FIXTURE / "acceptance_test.py") != manifest.get("acceptanceSHA256"):
        raise ValueError("Acceptance suite changed since preparation")
    problems = integrity(manifest)
    if problems:
        raise ValueError("Continuation integrity violation: " + "; ".join(problems))
    workspace = Path(manifest["workspace"])
    expected = dict(manifest["original"])
    seen = set()
    for change in prior.get("diffs", []):
        name = change.get("path")
        if name not in WRITE_PATHS or name in seen or not change.get("afterSHA256"):
            raise ValueError("Invalid or unscoped parent diff: " + str(name))
        expected[name] = change["afterSHA256"]
        seen.add(name)
    for name in set(expected) | {NEW_TEST}:
        path = workspace / name
        actual = digest(path) if path.is_file() else None
        if actual != expected.get(name):
            raise ValueError("Workspace changed after the parent report: " + name)
    agent, session, registered = prior.get("agent", {}), prior.get("session", {}), prior.get("workspace", {})
    previous_run = prior.get("run") or prior.get("interruptedRun") or {}
    if not agent.get("id") or not session.get("id") or not registered.get("id"):
        raise ValueError("Parent report lacks workspace/session/agent identity")
    if (registered.get("path") != str(workspace) or session.get("workspaceId") != registered["id"]
            or previous_run.get("status") not in FINAL_STATES
            or previous_run.get("sessionId") != session["id"]
            or previous_run.get("workspaceId") != registered["id"]
            or previous_run.get("model") != agent.get("model") or agent.get("mode") != "approval"):
        raise ValueError("Parent report identities or approval policy are inconsistent")
    prompt_path = prompt_path.resolve(strict=True)
    prompt_bytes = prompt_path.read_bytes()
    prompt = prompt_bytes.decode("utf-8")
    if not prompt.strip() or len(prompt_bytes) > 65536:
        raise ValueError("Follow-up prompt must contain 1–65536 UTF-8 bytes")
    return {"report":prior, "path":str(report_path), "sha256":hashlib.sha256(raw).hexdigest(),
            "prompt":prompt, "promptPath":str(prompt_path), "promptSHA256":hashlib.sha256(prompt_bytes).hexdigest()}


def verify_continuation_state(state, continuation):
    prior = continuation["report"]
    for key, item_key in (("workspaces", "workspace"), ("agents", "agent"), ("sessions", "session")):
        expected = prior[item_key]
        actual = next((item for item in state.get(key, []) if item.get("id") == expected["id"]), None)
        if actual is None:
            raise ValueError("Recorded " + item_key + " is absent from this app profile")
        fields = {"workspace": ("path",), "session": ("workspaceId",),
                  "agent": ("model", "mode", "instructions", "maxSteps", "thinking", "maxOutputTokens", "temperature", "toolProtocol")}[item_key]
        # Old immutable reports and unsaved profiles omit this opt-in field.
        # Only absence means native; explicit null/invalid values are not defaults.
        if any(actual.get(field, "native" if field == "toolProtocol" else None)
               != expected.get(field, "native" if field == "toolProtocol" else None) for field in fields):
            raise ValueError("Recorded " + item_key + " configuration changed")


def agent_model_overrides(options, agent):
    updated = dict(agent)
    thinking = getattr(options, "thinking", None)
    limit = getattr(options, "max_output_tokens", None)
    temperature = getattr(options, "temperature", None)
    tool_protocol = getattr(options, "tool_protocol", None)
    if thinking is not None:
        if thinking not in ("auto", "on", "off"):
            raise ValueError("Unsupported thinking setting")
        updated["thinking"] = thinking
    if limit is not None:
        if isinstance(limit, bool) or not isinstance(limit, int) or not 256 <= limit <= 32768:
            raise ValueError("Response token limit must be 256–32768")
        updated["maxOutputTokens"] = limit
    if temperature is not None:
        if isinstance(temperature, bool) or not isinstance(temperature, (int, float)) or not math.isfinite(temperature) or not 0 <= temperature <= 2:
            raise ValueError("Temperature must be a finite number from 0 to 2")
        updated["temperature"] = temperature
    if tool_protocol is not None:
        if tool_protocol not in ("native", "structured_json"):
            raise ValueError("Tool protocol must be native or structured_json")
        updated["toolProtocol"] = tool_protocol
    return updated


def verification_result(manifest, current, public, acceptance, problems, events=None):
    commands = current.get("receipt", {}).get("commands", [])
    def prescribed(command):
        return (command.get("executable") == manifest["python"] and command.get("args") == TEST_ARGS
                and command.get("workingDirectory", "") in ("", ".", manifest["workspace"]))

    verified = [command for command in commands
                if command.get("status") == "completed" and type(command.get("exitCode")) is int and command["exitCode"] == 0
                and not command.get("cancelled") and prescribed(command)
                and (not manifest.get("readOnlyTests") or command.get("readOnly") is True)]
    ordered = [event for event in (events or [])
               if current.get("id") and event.get("runId") == current["id"]
               and isinstance(event.get("seq"), int) and not isinstance(event.get("seq"), bool)
               and event["seq"] > 0]
    # Cursor pages may overlap, but journal sequence numbers identify events.
    ordered = sorted({event["seq"]: event for event in ordered}.values(), key=lambda event: event["seq"])
    mutation_tools = {"write_file", "edit_file", "move_file", "delete_file", "make_directory"}
    last_mutation = max((event["seq"] for event in ordered
                         if event.get("type") == "tool_completed" and event.get("tool") in mutation_tools), default=0)
    command_starts, attempts = {}, []
    receipts = {}
    for command in commands:
        call_id = command.get("callId")
        if isinstance(call_id, str) and call_id:
            receipts.setdefault(call_id, []).append(command)
    for event in ordered:
        data = event.get("data") or {}
        if event.get("type") != "tool_started" or data.get("name") != "run_command":
            continue
        call_id = data.get("id")
        valid_id = isinstance(call_id, str) and bool(call_id)
        linked = receipts.get(call_id, []) if valid_id else []
        if valid_id:
            command_starts.setdefault(call_id, []).append(event["seq"])
        arguments = data.get("arguments")
        # An omitted outcome/argument projection is uncertain, never permission
        # to fall back to an earlier success. Known unrelated commands are not
        # claims of running the prescribed suite.
        if (prescribed(arguments) if isinstance(arguments, dict) else not linked) or any(prescribed(command) for command in linked):
            attempts.append({"callId":call_id if valid_id else None, "eventSequence":event["seq"]})
    def uniquely_linked(command):
        call_id = command.get("callId")
        return (isinstance(call_id, str) and bool(call_id)
                and len(receipts.get(call_id, [])) == 1 and len(command_starts.get(call_id, [])) == 1)

    after_mutation = [command for command in verified
                      if uniquely_linked(command) and command_starts[command["callId"]][0] > last_mutation]
    latest = attempts[-1] if attempts else None
    latest_verified = bool(latest and any(command["callId"] == latest["callId"] for command in after_mutation))
    unlinked = [command.get("callId") for command in commands if prescribed(command) and not uniquely_linked(command)]
    unsequenced = [command.get("callId") for command in commands if prescribed(command)
                   and not command_starts.get(command.get("callId"))]
    verification = {"successfulPrescribedCommands": verified,
                    "successfulCommandsAfterLastMutation": after_mutation,
                    "lastMutationEventSequence": last_mutation,
                    "latestPrescribedAttempt": latest,
                    "latestPrescribedAttemptSucceeded": latest_verified,
                    "unlinkedOrAmbiguousPrescribedCallIds": unlinked,
                    "unsequencedPrescribedCallIds": unsequenced,
                    "discoveredAdditionalTests": (public.get("testCount") or 0) > 5,
                    "independentCases": acceptance.get("testCount"),
                    "note": "The latest prescribed attempt must have a uniquely linked successful receipt and start after the final recorded mutation. Replayed/ambiguous IDs or missing outcomes cannot verify edits. Additional tests still require human review; command exit zero alone is not acceptance."}
    passed = (current["status"] == "completed" and public["exitCode"] == 0 and acceptance["exitCode"] == 0
              and acceptance.get("testCount") == 9 and not problems
              and (Path(manifest["workspace"]) / NEW_TEST).is_file()
              and latest_verified and not unsequenced and verification["discoveredAdditionalTests"])
    return verification, bool(passed)


def harness_provenance():
    return {"path":str(HARNESS_PATH), "loadedSHA256":HARNESS_LOADED_SHA256,
            "startSHA256":digest(HARNESS_PATH)}


def finish_harness_provenance(provenance):
    try:
        provenance["finishSHA256"] = digest(Path(provenance["path"]))
    except OSError as exc:
        provenance["finishReadError"] = str(exc)
    provenance["unchanged"] = (provenance.get("loadedSHA256") == provenance.get("startSHA256")
                               == provenance.get("finishSHA256"))
    return provenance["unchanged"]


def run_challenge(options, manifest, continuation=None):
    source_provenance = harness_provenance()
    api = API(options.url)
    state = api("state")
    if any(run.get("status") not in FINAL_STATES for run in state.get("runs", [])) or any(experiment.get("status") == "running" for experiment in state.get("experiments", [])):
        raise RuntimeError("Wait for other app runs/evaluations before starting the realistic challenge")
    model_name = continuation["report"]["agent"]["model"] if continuation else options.model
    matching = [model for model in state.get("models", []) if model.get("name") == model_name and not model.get("remote")]
    if not state.get("runtime", {}).get("available") or not matching:
        raise RuntimeError("Requested local model is not installed/available; run model.refresh first")
    workspace = Path(manifest["workspace"])
    if continuation:
        verify_continuation_state(state, continuation)
    elif integrity(manifest) or any(digest(workspace/name) != sha for name,sha in manifest["original"].items()):
        raise RuntimeError("Prepared workspace is not the unchanged fixture; prepare a fresh attempt")
    if digest(FIXTURE/"acceptance_test.py") != manifest["acceptanceSHA256"]:
        raise RuntimeError("Acceptance suite changed since preparation")
    report = {"version":CHALLENGE_VERSION, "createdAt":now(), "manifest":manifest, "runtime":state["runtime"], "models":matching, "settings":state.get("settings"), "decisions":[], "qualification":"Actual local Gemma via application API. Independent acceptance is separate from model claims."}
    report["harnessProvenance"] = source_provenance
    report["attemptKind"] = "followup-turn" if continuation else "initial-attempt"
    report["resultScope"] = "Multi-turn recovery; does not change the initial attempt's result" if continuation else "Fresh fixture initial coding attempt plus no-tool continuity check"
    report["codingTurnNumber"] = continuation["report"].get("codingTurnNumber", 1) + 1 if continuation else 1
    output_stamp = (datetime.now().strftime("%Y%m%d-%H%M%S") + "-" + secrets.token_hex(4)) if continuation else Path(manifest["case"]).name.removeprefix("realistic-")
    output = EVIDENCE / (("realistic-followup-" if continuation else "realistic-") + output_stamp + ".json")
    if output.exists():
        raise RuntimeError("Refusing to overwrite existing qualification report: " + str(output))
    if continuation:
        report["parentReport"] = {"path":continuation["path"], "sha256":continuation["sha256"], "runId":(continuation["report"].get("run") or continuation["report"].get("interruptedRun") or {}).get("id"), "passed":continuation["report"].get("passed"), "codingPassed":continuation["report"].get("codingPassed")}
        report["followupPrompt"] = {"path":continuation["promptPath"], "sha256":continuation["promptSHA256"], "text":continuation["prompt"]}
    report["baselineGit"] = git_evidence(manifest)
    report["baselinePublic"] = sandbox_tests(workspace, manifest["python"], False)
    report["baselineAcceptance"] = sandbox_tests(workspace, manifest["python"], True)
    if not continuation and (report["baselinePublic"]["exitCode"] == 0 or report["baselineAcceptance"]["exitCode"] == 0 or report["baselinePublic"]["testCount"] != 5 or report["baselineAcceptance"]["testCount"] != 9):
        raise RuntimeError("Seeded fixture unexpectedly passed or test environment failed before model work")
    if continuation:
        model_agent, registered, session = (continuation["report"][key] for key in ("agent", "workspace", "session"))
        adjusted = agent_model_overrides(options, model_agent)
        if adjusted != model_agent:
            report["configurationChange"] = {"before":model_agent, "after":adjusted,
                "note":"Explicit model-setting qualification variant. Model identity, permissions, source scope, and acceptance checks are unchanged; prior failures remain failures."}
            model_agent = api("agent.save", adjusted)
        report["beforeConversation"] = api.conversation(session["id"])
        report["beforeDiffs"] = diffs(manifest)
        report["baselineMeaning"] = "State before this follow-up, including the prior model's authorized edits"
    else:
        model_agent = api("agent.save", agent_model_overrides(options, {"name":"Realistic tracker repair", "instructions":"Follow the explicit source scope and test policy in the user request. Preserve all original tests. Complete the task using evidence, and state any remaining failure honestly.", "model":model_name, "maxSteps":options.max_steps, "mode":"approval"}))
        registered = api("workspace.add", {"path":str(workspace), "name":"Qualification · SQLite tracker + CSV"})
        session = api("session.create", {"workspaceId":registered["id"], "title":"Repair tracker persistence and implement CSV export"})
    report.update({"agent":model_agent, "workspace":registered, "session":session})
    started, run, active_run = time.monotonic(), None, None
    try:
        if source_provenance["loadedSHA256"] != source_provenance["startSHA256"]:
            raise RuntimeError("Harness source changed after module initialization; qualification cannot start")
        run = api("run.start", {"sessionId":session["id"], "agentId":model_agent["id"], "model":model_name, "prompt":continuation["prompt"] if continuation else manifest["prompt"]})
        active_run = run
        report["submittedRun"] = run
        report["followupRun" if continuation else "initialRun"] = run
        current, state = wait_run(api, run, manifest, report, "coding", options.timeout)
        report["run"] = current
        report["codingSeconds"] = round(time.monotonic()-started, 3)
        report["conversation"] = api.conversation(session["id"])
        report["artifacts"] = [a for a in state.get("artifacts",[]) if a.get("runId") == run["id"]]
        report["afterPublic"] = sandbox_tests(workspace, manifest["python"], False)
        report["afterAcceptance"] = sandbox_tests(workspace, manifest["python"], True)
        report["integrityProblems"] = integrity(manifest)
        report["agentVerification"], coding_passed = verification_result(manifest, current, report["afterPublic"], report["afterAcceptance"], report["integrityProblems"], report["conversation"].get("events", []))
        report["codingPassed"] = coding_passed
        if coding_passed:
            continuity_prompt = "Do not open files, run commands, or call any tool. From our prior conversation only, state the exact handoff marker I gave you, summarize the three concrete defects you fixed, name the CSV feature entry points, and report the test outcome you actually observed. Do not invent an independent acceptance result."
            # Continuity has a different permission/instruction policy. Create
            # its own profile so later coding follow-ups retain their frozen
            # agent identity and configuration.
            continuity_profile = {key:value for key,value in model_agent.items() if key != "id"}
            continuity_profile.update({"name":"Continuity check · " + model_agent.get("name", "Realistic tracker repair"), "mode":"read-only", "maxSteps":3, "instructions":"This turn is a conversation continuity check. Answer only from saved conversation; do not call tools."})
            continuity_agent = api("agent.save", continuity_profile)
            report["continuationPrompt"], report["continuationAgent"] = continuity_prompt, continuity_agent
            next_run = api("run.start", {"sessionId":session["id"], "agentId":continuity_agent["id"], "model":model_name, "prompt":continuity_prompt})
            active_run = next_run
            continuity_run, state = wait_run(api, next_run, manifest, report, "continuation", 300)
            conversation = api.conversation(session["id"])
            actions = [e for e in conversation.get("events",[]) if e.get("runId") == next_run["id"] and e.get("type") == "tool_started"]
            report["continuation"] = {"run":continuity_run, "toolEvents":actions, "markerRecalled":manifest["marker"] in continuity_run.get("summary", "")}
            report["conversation"] = conversation
            report["passed"] = continuity_run["status"] == "completed" and report["continuation"]["markerRecalled"] and not actions and not integrity(manifest)
        else:
            report["passed"] = False
    except BaseException as exc:
        report["harnessError"] = repr(exc)
        report["passed"] = False
        if active_run:
            try: api("run.cancel", {"id":active_run["id"]})
            except Exception: pass
            for _ in range(40):
                try:
                    state = api("state")
                    current = next(item for item in state["runs"] if item["id"] == active_run["id"])
                    if current["status"] in FINAL_STATES:
                        report["interruptedRun"] = current
                        break
                except Exception:
                    break
                time.sleep(0.25)
        try: report["conversation"] = api.conversation(session["id"])
        except Exception as collect_error: report["collectionError"] = str(collect_error)
    finally:
        if "afterPublic" not in report:
            try:
                report["afterPublic"] = sandbox_tests(workspace, manifest["python"], False)
                report["afterAcceptance"] = sandbox_tests(workspace, manifest["python"], True)
            except Exception as verify_error:
                report["verificationError"] = str(verify_error)
        report["finishedAt"] = now()
        report["durationSeconds"] = round(time.monotonic()-started, 3)
        report["integrityProblems"] = integrity(manifest)
        report["diffs"] = diffs(manifest)
        try: report["git"] = git_evidence(manifest)
        except Exception as git_error: report["git"] = {"available":False, "error":str(git_error)}
        try: report["appGitReview"] = api("git.diff", {"workspaceId":registered["id"]})
        except Exception as review_error: report["appGitReview"] = {"error":str(review_error)}
        report["integrityProblems"] = integrity(manifest)
        if report["integrityProblems"]:
            report["passed"] = False
        if not finish_harness_provenance(source_provenance):
            report["passed"] = False
            report["qualificationInvalidated"] = "Harness source changed during this run"
        if continuation and digest(Path(continuation["path"])) != continuation["sha256"]:
            report["parentReportChanged"] = True
            report["passed"] = False
        if output.exists():
            raise RuntimeError("Refusing to overwrite existing qualification report: " + str(output))
        atomic_json(output, report)
        atomic_json(EVIDENCE/"realistic-latest.json", report)
        print(json.dumps({"passed":report.get("passed"), "attemptKind":report["attemptKind"], "runId":report.get("run",report.get("submittedRun",{})).get("id"), "evidence":str(output), "workspace":str(workspace), "durationSeconds":report["durationSeconds"]}, indent=2))
    if not report.get("passed"):
        raise RuntimeError("Realistic coding qualification failed; evidence preserved")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument("--prepare-only", action="store_true")
    modes.add_argument("--qualify-fixture", action="store_true")
    modes.add_argument("--run", action="store_true")
    modes.add_argument("--continue-report", type=Path, help="Start a new coding turn from an immutable prior report")
    parser.add_argument("--followup-prompt-file", type=Path, help="Exact UTF-8 user follow-up prompt for --continue-report")
    parser.add_argument("--manifest", type=Path, help="Use an unchanged prepared fixture")
    parser.add_argument("--python", default=sys.executable)
    parser.add_argument("--url", default=os.environ.get("GEMMA_TEST_URL", "http://127.0.0.1:8787"))
    parser.add_argument("--model", default="gemma4:12b-mlx")
    parser.add_argument("--max-steps", type=int, default=80)
    parser.add_argument("--thinking", choices=("auto", "on", "off"), help="Explicit model-setting variant; preserve the recorded setting when omitted")
    parser.add_argument("--max-output-tokens", type=int, help="Explicit per-response token cap, including thinking (256–32768)")
    parser.add_argument("--temperature", type=float, help="Explicit sampling variant (0–2); preserve the recorded setting when omitted")
    parser.add_argument("--tool-protocol", choices=("native", "structured_json"), help="Explicit tool-protocol variant; preserve the recorded setting when omitted (legacy default: native)")
    parser.add_argument("--read-only-tests", action=argparse.BooleanOptionalAction, default=True, help="Require readOnly:true for new prepared fixtures; existing manifests retain their frozen policy")
    parser.add_argument("--timeout", type=int, default=1200)
    options = parser.parse_args()
    if not 1 <= options.max_steps <= 100: parser.error("max-steps must be1–100")
    if options.max_output_tokens is not None and not 256 <= options.max_output_tokens <= 32768:
        parser.error("max-output-tokens must be 256–32768")
    if options.temperature is not None and (not math.isfinite(options.temperature) or not 0 <= options.temperature <= 2):
        parser.error("temperature must be a finite number from 0 to 2")
    if bool(options.continue_report) != bool(options.followup_prompt_file):
        parser.error("--continue-report and --followup-prompt-file must be used together")
    if options.continue_report:
        if options.manifest:
            parser.error("--continue-report uses the prior report's frozen manifest")
        continuation = load_continuation(options.continue_report, options.followup_prompt_file)
        run_challenge(options, continuation["report"]["manifest"], continuation)
        return
    python = str(Path(options.python).resolve(strict=True))
    (ROOT/".runtime").mkdir(exist_ok=True)
    if options.qualify_fixture:
        qualify_fixture(python)
        return
    if options.manifest:
        manifest = json.loads(options.manifest.read_text())
        if manifest.get("version") != CHALLENGE_VERSION: raise ValueError("Unsupported prepared challenge version")
        manifest_path = options.manifest
    else:
        manifest, manifest_path = prepare(python, options.read_only_tests)
    if options.prepare_only:
        print(json.dumps({"workspace":manifest["workspace"], "manifest":str(manifest_path), "prompt":str(Path(manifest["case"])/"PROMPT.txt"), "command":[manifest["python"]]+TEST_ARGS, "inferenceStarted":False}, indent=2))
    else:
        run_challenge(options, manifest)


if __name__ == "__main__":
    main()
