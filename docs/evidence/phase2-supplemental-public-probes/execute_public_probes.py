"""Explicitly authorized post-campaign controller. Never imports candidate code."""
import hashlib
import json
import os
from pathlib import Path
import selectors
import shutil
import signal
import stat
import subprocess
import tempfile
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[3]
BASE = Path(__file__).resolve().parent
RECIPE = BASE / "recipe.v3.json"
EXPECTED_RECIPE = "f3d90ca293b35ac556761febeda6e4329287dbc2b9267809f73c94863ccc3118"
sha = lambda b: hashlib.sha256(b).hexdigest()
now = lambda: datetime.now(timezone.utc).isoformat()


def checked_files(directory, manifest):
    observed = []
    for entry in manifest:
        relative = Path(entry["path"])
        assert not relative.is_absolute() and ".." not in relative.parts
        path = directory / relative
        assert stat.S_ISREG(path.lstat().st_mode) and not path.is_symlink()
        content = path.read_bytes()
        assert sha(content) == entry["sha256"] and len(content) == entry["bytes"], str(path)
        observed.append({"path": entry["path"], "sha256": sha(content), "bytes": len(content)})
    return observed


def bounded(argv, env, seconds=20, limit=8192):
    start = time.monotonic()
    proc = subprocess.Popen(argv, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            stdin=subprocess.DEVNULL, start_new_session=True)
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    selector = selectors.DefaultSelector()
    for name, pipe in (("stdout", proc.stdout), ("stderr", proc.stderr)):
        os.set_blocking(pipe.fileno(), False)
        selector.register(pipe, selectors.EVENT_READ, name)
    timed_out = truncated = False
    try:
        while selector.get_map():
            if time.monotonic() - start > seconds:
                timed_out = True
                break
            for key, _ in selector.select(.1):
                chunk = os.read(key.fd, 8192)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                dest = streams[key.data]
                remaining = max(0, limit-len(dest))
                dest.extend(chunk[:remaining])
                if len(chunk) > remaining:
                    truncated = True
                    break
            if truncated:
                break
        if timed_out or truncated:
            os.killpg(proc.pid, signal.SIGKILL)
        try:
            proc.wait(timeout=max(1, seconds-(time.monotonic()-start)))
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=5)
    finally:
        selector.close()
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=5)
        proc.stdout.close()
        proc.stderr.close()
    return {"argv": argv, "exitCode": proc.returncode, "durationSeconds": round(time.monotonic()-start, 3),
            "stdout": bytes(streams["stdout"]).decode("utf-8", "replace"),
            "stderr": bytes(streams["stderr"]).decode("utf-8", "replace"),
            "timedOut": timed_out, "outputTruncated": truncated}


def good(result):
    return result["exitCode"] == 0 and not result["timedOut"] and not result["outputTruncated"]


def same_json(actual, expected):
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, dict):
        return actual.keys() == expected.keys() and all(same_json(actual[k], v) for k, v in expected.items())
    if isinstance(expected, list):
        return len(actual) == len(expected) and all(same_json(a, b) for a, b in zip(actual, expected))
    return actual == expected


def main():
    import sys
    if sys.argv[1:] != ["--authorized-after-campaign"]:
        raise SystemExit("Requires explicit post-campaign authorization; no Docker operation performed.")
    raw = RECIPE.read_bytes()
    assert sha(raw) == EXPECTED_RECIPE
    recipe = json.loads(raw)
    assert recipe["executionCount"] == 0 and recipe["status"] == "prepared_unexecuted"
    result_path = BASE / "observations.v3.json"
    if result_path.exists():
        raise SystemExit("Refusing to replace existing supplemental observations")
    started = time.monotonic()
    work = Path(tempfile.mkdtemp(prefix="gemma-public-probes-", dir="/tmp")).resolve()
    nonce = work.name.removeprefix("gemma-public-probes-")
    host = "unix://" + str(Path.home()/".docker/run/docker.sock")
    env = {"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": str(work), "LANG": "C.UTF-8",
           "DOCKER_CONFIG": str(work/"docker-config")}
    prefix = ["/usr/local/bin/docker", "--host", host, "--config", env["DOCKER_CONFIG"]]
    output = {"schemaVersion": 1, "reviewer": "/root/ollama_model_research", "startedAt": now(),
              "recipeSHA256": sha(raw), "controllerSHA256": sha(Path(__file__).read_bytes()),
              "authorization": "Root explicitly authorized after all72 trials completed at2026-10-28T07:43:26Z; exclusive Docker lane.",
              "image": recipe["image"]["id"], "runLabel": nonce, "status": "running", "results": [],
              "qualification": "Supplemental public CLI observations only. Original trials/grades untouched; no hidden assertions, candidate host imports or inference."}
    def save():
        result_path.write_text(json.dumps(output, indent=2)+"\n")
    clean = True
    try:
        image = bounded(prefix+["image", "inspect", "--format", "{{.Id}} {{.Architecture}} {{.Os}}", output["image"]], env)
        output["imageInspection"] = image
        assert good(image) and image["stdout"].strip() == output["image"]+" arm64 linux", image
        save()
        for index, probe in enumerate(recipe["probes"]):
            assert time.monotonic()-started < recipe["executionPlan"]["campaignIndependentProbeDeadlineSeconds"]
            source = Path(probe["originalWorkspace"])
            stage = ROOT/probe["preparedRuntimeSourceSubset"]["directory"]
            manifest = probe["preparedRuntimeSourceSubset"]["files"]
            observed = {"probeId": probe["probeId"], "trialId": probe["trialId"], "startedAt": now(),
                        "sourceBefore": checked_files(source, probe["reviewedPublicFileManifest"]),
                        "preparedBefore": checked_files(stage, manifest)}
            disposable = work/str(index)
            copy = disposable/"source"
            copy.mkdir(parents=True)
            for file in manifest:
                target = copy/file["path"]
                target.write_bytes((stage/file["path"]).read_bytes())
                target.chmod(0o444)
            copy.chmod(0o555)
            observed["disposableCopy"] = checked_files(copy, manifest)
            name = "gemma-public-probe-"+nonce+"-"+str(index)
            replacements = {"{RUN_NONCE}": nonce, "{PROBE_INDEX}": str(index),
                            "{PROBE_ID}": probe["probeId"], "{PRIVATE_DISPOSABLE_DIR}": str(disposable),
                            "{HOST_UID}": str(os.getuid()), "{HOST_GID}": str(os.getgid())}
            create = []
            for value in recipe["executionPlan"]["createArgvTemplate"]:
                if value == "{GUEST_ARGV_AS_SEPARATE_ARGUMENTS}":
                    create.extend(probe["guestArgv"])
                    continue
                for old, new in replacements.items():
                    value = value.replace(old, new)
                create.append(value)
            try:
                observed["create"] = bounded(prefix+create, env)
                assert good(observed["create"]), observed["create"]
                observed["run"] = bounded(prefix+["start", "--attach", name], env, seconds=120, limit=65536)
                assert good(observed["run"]), observed["run"]
                parsed = json.loads(observed["run"]["stdout"])
                assert isinstance(parsed, list) and len(parsed) == len(probe["expectedParsedOutput"])
                observed["actualParsedOutput"] = parsed
                observed["caseMatches"] = [same_json(a, b) for a, b in zip(parsed, probe["expectedParsedOutput"])]
                assert observed["caseMatches"][0], "ordinary control failed"
                observed["verdict"] = "public-contract mismatch observed" if not all(observed["caseMatches"]) else "all prepared cases passed"
            except BaseException as error:
                observed["error"] = str(error)
                observed["verdict"] = "inconclusive/infrastructure"
                raise
            finally:
                inspect = bounded(prefix+["inspect", "--format", '{{index .Config.Labels "com.gemmadesktop.public-review"}}', name], env)
                observed["cleanupInspect"] = inspect
                if good(inspect) and inspect["stdout"].strip() == nonce:
                    observed["stop"] = bounded(prefix+["stop", "--time=2", name], env)
                    observed["remove"] = bounded(prefix+["rm", "--force", name], env)
                    clean = good(observed["remove"])
                else:
                    absent = bounded(prefix+["ps", "-aq", "--filter", "name=^/"+name+"$"], env)
                    observed["cleanupAbsence"] = absent
                    clean = good(absent) and not absent["stdout"].strip()
                leftovers = bounded(prefix+["ps", "-aq", "--filter", "label=com.gemmadesktop.public-review="+nonce], env)
                observed["ownedGuestsAfter"] = leftovers
                clean = clean and good(leftovers) and not leftovers["stdout"].strip()
                observed["cleanupConfirmed"] = clean
                observed["sourceAfter"] = checked_files(source, probe["reviewedPublicFileManifest"])
                observed["preparedAfter"] = checked_files(stage, manifest)
                observed["originalsUnchanged"] = observed["sourceBefore"] == observed["sourceAfter"]
                observed["finishedAt"] = now()
                output["results"].append(observed)
                save()
                if not clean:
                    raise RuntimeError("owned cleanup uncertain; retained "+str(work))
                copy.chmod(0o755)
                shutil.rmtree(disposable)
            print(json.dumps({"probeId": observed["probeId"], "verdict": observed["verdict"], "caseMatches": observed["caseMatches"], "cleanupConfirmed": clean}), flush=True)
        output["status"] = "completed"
    except BaseException as error:
        output["status"] = "failed"
        output["error"] = str(error)
        raise
    finally:
        output["finishedAt"] = now()
        output["durationSeconds"] = round(time.monotonic()-started, 3)
        output["allOwnedGuestsRemoved"] = clean
        if clean:
            shutil.rmtree(work)
        else:
            output["retainedPrivateDirectory"] = str(work)
        save()


if __name__ == "__main__":
    main()
