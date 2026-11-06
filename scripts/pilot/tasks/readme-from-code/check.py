"""Checker for readme-from-code: README.md for the backup_rotate.py CLI.

Truth comes from the trusted copy of the tool: its real argparse parser is
built in a sandbox and asked (a) which subcommands, options and defaults exist
and (b) whether each command line found in the README actually parses. README
text is only ever data handed to that trusted parser; no candidate code runs.

Checks: README.md exists with some descriptive prose; every subcommand and
every option flag is mentioned; every non-trivial default is stated correctly
next to its option (tables with a Default column, "(default: 7)", "defaults to
`backups`", grouped "defaults: 7 daily / 4 weekly / 12 monthly", ...); each
subcommand has at least one example command the real parser accepts; every
tool command line uses only flags that exist at that position (global flags
before the subcommand, subcommand flags after it); no nonexistent long option
is documented (mentions explicitly marked as old/renamed are allowed); the
tool sources are unchanged. Headings, layout and wording are free.
"""
import json
import re
import shlex
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import PYTHON, result, sandboxed, unchanged  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED = HERE / "trusted"
PROTECTED = ["backup_rotate.py", "rotation.py"]
PROGRAM_NAMES = {"backup_rotate.py", "backup_rotate", "backup-rotate"}

PROBE = r'''
import argparse, contextlib, io, json, sys
sys.path.insert(0, ".")
import backup_rotate as tool

def describe(p):
    opts = []
    for a in p._actions:
        if isinstance(a, argparse._SubParsersAction) or not a.option_strings:
            continue
        d = a.default
        flaglike = isinstance(a, (argparse._HelpAction, argparse._VersionAction, argparse._StoreTrueAction, argparse._StoreFalseAction))
        trivial = flaglike or d is None or d is False or d == [] or d == argparse.SUPPRESS
        opts.append({"strings": list(a.option_strings), "default": None if trivial else d,
                     "isint": isinstance(d, int) and not isinstance(d, bool),
                     "help": isinstance(a, argparse._HelpAction)})
    return opts

parser = tool.build_parser()
sub = next(a for a in parser._actions if isinstance(a, argparse._SubParsersAction))
spec = {"main": describe(parser), "subcommands": {n: describe(p) for n, p in sub.choices.items()}}
results = []
for argv in json.load(sys.stdin):
    sink = io.StringIO()
    try:
        with contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
            ns = parser.parse_args(argv)
        results.append({"ok": True, "command": ns.command})
    except SystemExit as e:
        results.append({"ok": False, "command": None, "error": (sink.getvalue().strip().splitlines() or [""])[-1][:200]})
print(json.dumps({"spec": spec, "results": results}))
'''

# ---------------------------------------------------------------- markdown
FENCE_OPEN = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
FENCE_CLOSE = re.compile(r"^ {0,3}(`{3,}|~{3,})\s*$")
HEADING = re.compile(r"^ {0,3}#{1,6}\s")
LIST_ITEM = re.compile(r"^(\s*)(?:[-*+]|\d+[.)])\s+")
TABLE_SEP = re.compile(r"^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$")
INLINE = re.compile(r"(`+)(.+?)\1")
# A bare command line (indented code block or a line starting with a shell prompt).
START = re.compile(r"^\s*(?:[-*+>]\s+|\d+[.)]\s+)?(?:[$%#]\s+)?(?:\w+=\S+\s+)*"
                   r"(?:(?:\S*/)?(?:python3?(?:\.\d+)?|py)\s+(?:-\S+\s+)*)?(?:\S*/)?backup[_-]rotate(?:\.py)?(?:\s|$)")
LONG_FLAG = re.compile(r"(?<![\w-])--[A-Za-z][\w-]*")
SHELL_OPS = {"|", "||", "&&", ";", "&"}
# A command shown on purpose as a mistake ("`backup_rotate.py plan -v` is an error").
NEGATIVE_EXAMPLE = re.compile(r"\b(?:wrong|invalid|incorrect|fails?|won['’]t work|doesn['’]t work|does not work|not valid)\b|❌|✗", re.I)
NEGATIVE_PROSE = re.compile(r"\b(?:an? error|errors? out|unrecognized)\b", re.I)
EXCUSE = re.compile(r"renam|formerly|previous|deprecat|removed|replaced|no longer|used to|\bold(?:er)?\b|legacy|"
                    r"was called|\bbefore\b|\bprior\b|\b1\.[0-2](?:\.\d)?\b|changelog|upgrad|migrat|instead of", re.I)

CUE = re.compile(r"\bdefault(?:s|ed)?\b|\b(?:if|when) (?:omitted|not (?:given|set|specified|provided|passed))\b|"
                 r"\bunless\b|\botherwise\b|\bout of the box\b", re.I)
EXPLICIT = re.compile(r"\bdefault(?:s|ed)?\b(?:\s+(?:value|setting))?[\s*_`'\"]*"
                      r"(?:(?:(?:is|of|to)\b|=|:|→|->|—|–)[\s*_]*)*(?P<span>[^;|\n]{0,80})", re.I)
BRACE = re.compile(r"(?<![\w-])(--[A-Za-z][\w-]*-)\{([\w-]+(?:,[\w-]+)+)\}")
WORDS = {0: "zero", 1: "one", 2: "two", 3: "three", 4: "four", 5: "five", 6: "six", 7: "seven", 8: "eight",
         9: "nine", 10: "ten", 12: "twelve", 30: "thirty", 90: "ninety"}
NUMBER_START = re.compile(r"(?:\d|" + "|".join(WORDS.values()) + r")\b|\d", re.I)


def classify(text: str):
    lines = text.splitlines()
    # Tolerate a whole-file ```markdown wrapper (a common LLM artifact).
    nonblank = [i for i, l in enumerate(lines) if l.strip()]
    if nonblank and re.match(r"^ {0,3}`{3,}\s*(markdown|md)\s*$", lines[nonblank[0]], re.I) and FENCE_CLOSE.match(lines[nonblank[-1]]):
        lines = lines[nonblank[0] + 1:nonblank[-1]]
    kinds, fence = [], None
    for line in lines:
        if fence:
            m = FENCE_CLOSE.match(line)
            if m and m.group(1)[0] == fence[0] and len(m.group(1)) >= len(fence):
                kinds.append("fence")
                fence = None
            else:
                kinds.append("code")
            continue
        m = FENCE_OPEN.match(line)
        if m and not (m.group(1)[0] == "`" and "`" in m.group(2)):
            fence = m.group(1)
            kinds.append("fence")
            continue
        kinds.append("text")
    return lines, kinds


def split_row(line: str) -> list[str]:
    s = line.replace("\\|", "\x00").strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|"):
        s = s[:-1]
    return [c.replace("\x00", "|").strip() for c in s.split("|")]


def tables(lines, kinds) -> dict[int, int | None]:
    """Map each pipe-table row line to the index of its 'Default' column (or None)."""
    rows: dict[int, int | None] = {}
    i = 0
    while i < len(lines):
        if kinds[i] == "text" and "|" in lines[i] and i + 1 < len(lines) and kinds[i + 1] == "text" and TABLE_SEP.match(lines[i + 1]) and "|" in lines[i + 1]:
            header = split_row(lines[i])
            col = next((k for k, h in enumerate(header) if re.search(r"default", h, re.I)), None)
            j = i + 2
            rows[i] = None
            while j < len(lines) and kinds[j] == "text" and "|" in lines[j] and lines[j].strip():
                rows[j] = col
                j += 1
            i = j
            continue
        i += 1
    return rows


def strip_markup(s: str) -> str:
    return re.sub(r"[`*_:\[\]()]", "", s).strip()


# ---------------------------------------------------------------- commands
def bare_command(line: str) -> bool:
    """An indented-code or prompt-prefixed line that runs the tool (not prose that names it)."""
    return bool(re.match(r"^(?:\t| {4,}|\s*[$%]\s)", line)) and "`" not in line and START.match(line) is not None


def is_program(tok: str) -> bool:
    t = tok.strip("`'\"")
    return t.rstrip("/").rsplit("/", 1)[-1] in PROGRAM_NAMES


def invocations(shell_line: str) -> list[list[str]]:
    """argv lists (after the program name) for each tool invocation on one shell line."""
    try:
        toks = shlex.split(shell_line, comments=True)
    except ValueError:
        toks = shell_line.split()
    cmds, cur = [], []
    for t in toks:
        if t in SHELL_OPS or t.startswith((">", "2>", "1>", "&>")):
            cmds.append(cur)
            cur = []
            continue
        cur.append(t)
    cmds.append(cur)
    out = []
    for c in cmds:
        idx = next((k for k, t in enumerate(c) if is_program(t)), None)
        if idx is not None:
            out.append(c[idx + 1:])
    return out


def command_sources(lines, kinds) -> list[tuple[int, str, bool]]:
    """(line number, shell text, shown-as-a-mistake) for every place the README shows a tool command."""
    out = []
    i = 0
    while i < len(lines):
        if kinds[i] == "code":
            text, j = lines[i], i
            while text.rstrip().endswith("\\") and j + 1 < len(lines) and kinds[j + 1] == "code":
                j += 1
                text = text.rstrip()[:-1] + " " + lines[j]
            if any(is_program(t) for t in text.replace("`", " ").split()):
                comment = text.split(" #", 1)[1] if " #" in text else ""
                out.append((i + 1, text, bool(NEGATIVE_EXAMPLE.search(comment))))
            i = j + 1
            continue
        if kinds[i] == "text":
            line = lines[i]
            if bare_command(line):
                comment = line.split(" #", 1)[1] if " #" in line else ""
                out.append((i + 1, line, bool(NEGATIVE_EXAMPLE.search(comment))))
            else:
                for m in INLINE.finditer(line):
                    if any(is_program(t) for t in m.group(2).split()):
                        rest = INLINE.sub(" ", line)
                        out.append((i + 1, m.group(2), bool(NEGATIVE_EXAMPLE.search(rest) or NEGATIVE_PROSE.search(rest))))
        i += 1
    return out


def flag_ok(flag: str, allowed: set[str]) -> bool:
    if flag in allowed:
        return True
    if flag.startswith("--"):
        return len(flag) > 3 and sum(1 for a in allowed if a.startswith(flag)) == 1
    if len(flag) > 2 and f"-{flag[1]}" in allowed:  # -nv clusters, -d/srv/backups
        return True
    return False


def flag_problems(argv: list[str], main: set[str], subs: dict[str, set[str]]) -> list[str]:
    union = set(main).union(*subs.values())
    allowed, where, bad = main, "before the subcommand", []
    state = "main"
    for raw in argv:
        tok = raw.strip("[](){}<>|,`'\"")
        if tok in ("", "-", "...") or tok.startswith("..."):
            continue
        if tok == "--":
            break
        if tok.startswith("-") and not re.match(r"^-\d", tok):
            for part in tok.split("|"):  # synopsis alternatives such as [-v|--verbose]
                flag = part.strip("[](){}<>").split("=", 1)[0].rstrip(".")
                if flag.startswith("-") and flag != "-" and not flag_ok(flag, allowed):
                    bad.append(f"{flag} ({where})")
        elif state == "main":
            if tok in subs:
                state, allowed, where = tok, subs[tok], f"not an option of '{tok}'"
            else:
                state, allowed, where = "?", union, "not an option of this tool"
    return bad


# ---------------------------------------------------------------- defaults
def value_re(value, isint: bool):
    if isint:
        alts = [rf"(?<![\w.]){value}(?!\w|\.\d)"]
        if value in WORDS:
            alts.append(rf"\b{WORDS[value]}\b")
        return re.compile("|".join(alts), re.I)
    s = re.escape(str(value))
    # The value as its own token: "backups", `./backups/`, ".tar.gz"; not "/srv/backups" or "backupsX".
    return re.compile(rf"(?<![^\s`'\"(\[:=,{{.])(?:\./)?{s}/?(?!\w)")


def clause(span: str) -> str:
    """Cut at the sentence end or at a ')' closing a paren opened before the span."""
    depth = 0
    for k, ch in enumerate(span):
        if ch == "(":
            depth += 1
        elif ch == ")":
            if depth == 0:
                return span[:k]
            depth -= 1
        elif ch == "." and (k + 1 == len(span) or span[k + 1].isspace()):
            return span[:k]
    return span


def statements(seg: str, truth: re.Pattern, isint: bool) -> list[bool]:
    """Explicit 'default ... <value>' statements in seg; True where the value is right."""
    out = []
    for m in EXPLICIT.finditer(seg):
        span = clause(m.group("span"))
        bare = span.lstrip(" *_`'\"(")
        if not bare:
            continue
        if isint:
            is_stmt = bool(NUMBER_START.match(bare))
        else:
            first = span.lstrip(" *_")[:1]
            is_stmt = first in "`'\"$." or truth.match(bare) is not None or len(bare.split()) == 1
        if is_stmt:
            out.append(truth.search(span) is not None)
    return out


def judge(seg: str, truth: re.Pattern, isint: bool):
    st = statements(seg, truth, isint)
    if st:
        return "correct" if any(st) else "wrong"
    if CUE.search(seg) and truth.search(seg):
        return "correct"
    return None


def paragraph(lines, kinds, table_rows, i) -> str:
    """The plain prose paragraph around line i (no headings, list items or table rows)."""
    def inside(j):
        return (0 <= j < len(lines) and kinds[j] == "text" and lines[j].strip() and j not in table_rows
                and not HEADING.match(lines[j]) and not LIST_ITEM.match(lines[j]))
    a = b = i
    while inside(a - 1):
        a -= 1
    while inside(b + 1):
        b += 1
    return "\n".join(lines[a:b + 1])


def default_verdict(lines, kinds, table_rows, mentions, key, value, isint):
    """'correct', 'wrong' or 'missing' for one option, with evidence."""
    truth = value_re(value, isint)
    verdicts = []
    for (i, s, e, k) in mentions:
        if k != key:
            continue
        line = lines[i]
        others = [(s2, e2) for (i2, s2, e2, k2) in mentions if i2 == i and k2 != key]
        after_end = min([s2 for s2, _ in others if s2 >= e], default=len(line))
        before_start = max([e2 for _, e2 in others if e2 <= s], default=0)
        after, before = line[e:after_end], line[before_start:s]
        cont = []
        if kinds[i] == "text" and i not in table_rows and after_end == len(line):
            heading = HEADING.match(line) is not None
            term = len(strip_markup(after)) <= 24
            j = i + 1
            while j < len(lines) and kinds[j] == "text":
                lj = lines[j]
                if not lj.strip():
                    if heading or (term and not cont):
                        j += 1
                        continue
                    break
                if HEADING.match(lj) or j in table_rows:
                    break
                lm = LIST_ITEM.match(lj)
                if lm and not heading and not term:
                    own = LIST_ITEM.match(line)
                    if not own or len(lm.group(1)) <= len(own.group(1)):
                        break
                cut = [s2 for (i2, s2, e2, k2) in mentions if i2 == j and k2 != key]
                if cut:
                    cont.append(lj[:min(cut)])
                    break
                cont.append(lj)
                j += 1
                if len(cont) >= 12:
                    break
        after_full = after + "\n" + "\n".join(cont)
        item = line + "\n" + "\n".join(cont)
        v = None
        if i in table_rows and table_rows[i] is not None:
            cells = split_row(line)
            col = table_rows[i]
            cell = cells[col] if col < len(cells) else ""
            if strip_markup(cell).lower() not in ("", "-", "—", "–", "none", "n/a", "na"):
                v = "correct" if truth.search(cell) else "wrong"
        if v is None:
            v = judge(after_full, truth, isint)
        if v is None:
            if CUE.search(before) and truth.search(before):
                v = "correct"
            elif any(statements(item, truth, isint)) or (CUE.search(item) and (truth.search(after_full) or truth.search(before))):
                v = "correct"
            elif kinds[i] == "text" and i not in table_rows and not HEADING.match(line) and not LIST_ITEM.match(line):
                # Grouped prose: "retention defaults to 7 daily, 4 weekly and 12 monthly
                # archives (`--keep-daily`, `--keep-weekly`, `--keep-monthly`)".
                para = paragraph(lines, kinds, table_rows, i)
                if any(statements(para, truth, isint)) or (CUE.search(para) and truth.search(para)):
                    v = "correct"
        if v:
            verdicts.append((v, i + 1, item.strip()[:160]))
    if any(v == "wrong" for v, _, _ in verdicts):
        return "wrong", list(dict.fromkeys(x for x in verdicts if x[0] == "wrong"))
    if any(v == "correct" for v, _, _ in verdicts):
        return "correct", []
    return "missing", []


# ---------------------------------------------------------------- check
def run_probe(argvs):
    r = sandboxed([PYTHON, "-I", "-B", "-c", PROBE], TRUSTED, timeout=60, stdin=json.dumps(argvs))
    try:
        return json.loads(r.stdout.strip().splitlines()[-1]), ""
    except Exception:
        return None, (r.stdout + r.stderr)[-2000:]


def find_readme(final: Path):
    p = final / "README.md"
    if p.is_file():
        return p
    return next((q for q in sorted(final.iterdir()) if q.is_file() and q.name.lower() == "readme.md"), None)


NAMES = ["README.md exists", "describes the tool in prose", "mentions every subcommand", "mentions every option flag",
         "non-trivial defaults stated correctly", "working example for every subcommand",
         "commands use only real flags in valid positions", "no nonexistent options documented"]


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    path = find_readme(final)
    text = path.read_text(errors="replace") if path else ""
    if not path or not text.strip():
        out.append(result(NAMES[0], False, "no README.md at the workspace root" if not path else "README.md is empty"))
        out.extend(result(n, False, "no README.md") for n in NAMES[1:])
        return out
    out.append(result(NAMES[0], True, path.name))

    lines, kinds = classify(text)
    lines = [BRACE.sub(lambda m: "/".join(m.group(1) + x for x in m.group(2).split(",")), l) if k == "text" else l
             for l, k in zip(lines, kinds)]
    table_rows = tables(lines, kinds)
    sources = command_sources(lines, kinds)
    cmds = [(ln, src, argv, neg) for ln, src, neg in sources for argv in invocations(src)]
    probe, err = run_probe([argv for _, _, argv, _ in cmds])
    if probe is None:
        raise RuntimeError("trusted parser probe failed: " + err)
    spec = probe["spec"]
    subs_spec = spec["subcommands"]
    main_strings = {s for o in spec["main"] for s in o["strings"]}
    sub_strings = {n: {s for o in opts for s in o["strings"]} for n, opts in subs_spec.items()}
    all_strings = main_strings.union(*sub_strings.values())

    options = {}  # key -> (strings, default, isint)
    for o in spec["main"] + [o for opts in subs_spec.values() for o in opts]:
        if o["help"]:
            continue
        key = tuple(sorted(o["strings"]))
        options.setdefault(key, (o["strings"], o["default"], o["isint"]))

    # prose
    words = 0
    for line, kind in zip(lines, kinds):
        if kind == "text" and not HEADING.match(line) and "|" not in line:
            words += len(re.findall(r"[A-Za-z]{2,}", INLINE.sub(" ", line)))
    out.append(result(NAMES[1], words >= 40, f"{words} words of prose outside code, tables and headings"))

    # subcommands mentioned
    missing = [n for n in subs_spec if not re.search(rf"(?<![\w-]){re.escape(n)}(?![\w-])", text)]
    out.append(result(NAMES[2], not missing, "missing: " + ", ".join(missing) if missing else ", ".join(subs_spec)))

    # options mentioned
    mentions = []  # (line index, start, end, key)
    for key, (strings, _, _) in options.items():
        for s in strings:
            pat = re.compile(rf"(?<![\w-]){re.escape(s)}(?![\w-])")
            for i, line in enumerate(lines):
                for m in pat.finditer(line):
                    mentions.append((i, m.start(), m.end(), key))
    mentions.sort()
    mentioned = {k for (_, _, _, k) in mentions}
    missing = ["/".join(v[0]) for k, v in options.items() if k not in mentioned]
    out.append(result(NAMES[3], not missing, "missing: " + ", ".join(missing) if missing else f"{len(options)} options mentioned"))

    # defaults
    problems, ok = [], []
    for key, (strings, default, isint) in options.items():
        if default is None:
            continue
        name = max(strings, key=len)
        verdict, evidence = default_verdict(lines, kinds, table_rows, mentions, key, default, isint)
        if verdict == "correct":
            ok.append(f"{name}={default}")
        elif verdict == "wrong":
            problems.append(f"{name}: expected {default}; wrong default stated at " + "; ".join(f"line {ln}: {t!r}" for _, ln, t in evidence[:2]))
        else:
            problems.append(f"{name}: default {default} not stated next to the option")
    out.append(result(NAMES[4], not problems, "; ".join(problems) if problems else "ok: " + ", ".join(ok)))

    # examples per subcommand
    have = {}
    for (ln, src, argv, neg), res in zip(cmds, probe["results"]):
        if not neg and res["ok"] and res["command"] and res["command"] not in have:
            have[res["command"]] = f"line {ln}: {src.strip()[:120]}"
    missing = [n for n in subs_spec if n not in have]
    rejected = [f"line {ln}: {src.strip()[:100]!r} -> {res.get('error')}" for (ln, src, _, _), res in zip(cmds, probe["results"]) if not res["ok"]]
    detail = ("no example the real parser accepts for: " + ", ".join(missing) + ". ") if missing else ""
    detail += "accepted: " + "; ".join(f"{k} ({v})" for k, v in have.items())
    if rejected:
        detail += " | rejected by the parser: " + "; ".join(rejected[:6])
    out.append(result(NAMES[5], not missing, detail))

    # flags in commands
    bad = []
    for ln, src, argv, neg in cmds:
        if neg:
            continue
        for p in flag_problems(argv, main_strings, sub_strings):
            bad.append(f"line {ln}: {p} in {src.strip()[:100]!r}")
    out.append(result(NAMES[6], not bad, "; ".join(bad[:8]) if bad else f"{len(cmds)} command lines checked"))

    # invented options in prose / inline code
    invented = []
    for i, (line, kind) in enumerate(zip(lines, kinds)):
        if kind != "text" or bare_command(line):
            continue
        found = []
        for m in INLINE.finditer(line):
            span = m.group(2)
            toks = span.split()
            if toks and not any(is_program(t) for t in toks) and toks[0].startswith("-"):
                found += LONG_FLAG.findall(span)
        found += LONG_FLAG.findall(INLINE.sub(" ", line))
        for f in found:
            if flag_ok(f, all_strings) or f.endswith("-") or EXCUSE.search(line):
                continue
            invented.append(f"line {i + 1}: {f}")
    out.append(result(NAMES[7], not invented, "; ".join(invented[:8])))
    return out
