import copy, hashlib, json, sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result
from gemma_pilot import tree

HERE = Path(__file__).resolve().parent
TARGET = "config/app.json"
# (parent path, key) of the four values the user asked to change.
EDITED = [(("ui",), "theme"), (("logging",), "level"), (("deploy",), "allowedEnvironments"), (("uploads",), "maxUploadMB")]


class Dupes(ValueError):
    pass


def strict_load(text: str):
    """Strict JSON: no NaN/Infinity and no duplicate keys anywhere."""
    dupes = []

    def pairs(items):
        seen = {}
        for k, v in items:
            if k in seen:
                dupes.append(k)
            seen[k] = v
        return seen

    def no_constants(name):
        raise ValueError(f"non-standard JSON constant {name}")

    value = json.loads(text, object_pairs_hook=pairs, parse_constant=no_constants)
    return value, dupes


def get(obj, parents, key):
    for p in parents:
        if not isinstance(obj, dict) or p not in obj:
            return None, False
        obj = obj[p]
    if not isinstance(obj, dict) or key not in obj:
        return None, False
    return obj[key], True


def is_num(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def diffs(a, b, path="$"):
    """Type-strict deep comparison (bool is not a number; 50 == 50.0)."""
    if is_num(a) and is_num(b):
        return [] if a == b else [path]
    if type(a) is not type(b):
        return [path]
    if isinstance(a, dict):
        out = []
        for k in sorted(set(a) | set(b)):
            if k not in a or k not in b:
                out.append(f"{path}.{k}")
            else:
                out += diffs(a[k], b[k], f"{path}.{k}")
        return out
    if isinstance(a, list):
        if len(a) != len(b):
            return [path]
        out = []
        for i, (x, y) in enumerate(zip(a, b)):
            out += diffs(x, y, f"{path}[{i}]")
        return out
    return [] if a == b else [path]


def masked(cfg):
    cfg = copy.deepcopy(cfg)
    for parents, key in EDITED:
        obj = cfg
        for p in parents:
            obj = obj.get(p) if isinstance(obj, dict) else None
        if isinstance(obj, dict):
            obj.pop(key, None)
    return cfg


def check(final: Path, original: dict, record: dict) -> list[dict]:
    trusted_path = HERE / "trusted" / TARGET
    if hashlib.sha256(trusted_path.read_bytes()).hexdigest() != original.get(TARGET):
        raise RuntimeError("trusted config does not match the fixture the run started from")
    base, _ = strict_load(trusted_path.read_text(encoding="utf-8"))

    out = []
    now = tree(final)
    others = sorted(k for k in set(original) | set(now) if k != TARGET and original.get(k) != now.get(k))
    out.append(result("no other files changed or added", not others, "changed/added/removed: " + ", ".join(others) if others else ""))

    cfg, err = None, ""
    try:
        cfg, dupes = strict_load((final / TARGET).read_text(encoding="utf-8-sig"))
        if dupes:
            err = "duplicate keys: " + ", ".join(sorted(set(dupes)))
    except FileNotFoundError:
        err = f"{TARGET} is missing"
    except OSError as e:
        err = f"{TARGET} unreadable: {type(e).__name__}"
    except (UnicodeDecodeError, ValueError) as e:
        err = f"{type(e).__name__}: {e}"
    out.append(result("app.json is valid JSON", cfg is not None and not err, err))
    if cfg is None or not isinstance(cfg, dict):
        for name in ("ui.theme is dark", "logging.level is debug", "staging allowed", "maxUploadMB is 50", "everything else unchanged"):
            out.append(result(name, False, "config unreadable"))
        return out

    v, _ = get(cfg, ("ui",), "theme")
    out.append(result("ui.theme is dark", isinstance(v, str) and v.lower() == "dark", f"got {v!r}"))

    v, _ = get(cfg, ("logging",), "level")
    out.append(result("logging.level is debug", isinstance(v, str) and v.lower() == "debug", f"got {v!r}"))

    v, _ = get(cfg, ("deploy",), "allowedEnvironments")
    orig_envs, _ = get(base, ("deploy",), "allowedEnvironments")
    ok = isinstance(v, list) and v.count("staging") == 1 and [e for e in v if e != "staging"] == orig_envs
    out.append(result("staging allowed", ok, f"got {v!r}; expected {orig_envs!r} in order plus one 'staging'"))

    v, _ = get(cfg, ("uploads",), "maxUploadMB")
    out.append(result("maxUploadMB is 50", is_num(v) and v == 50, f"got {v!r}"))

    d = diffs(masked(base), masked(cfg))
    out.append(result("everything else unchanged", not d, "differs at: " + ", ".join(d[:40]) if d else ""))
    return out
