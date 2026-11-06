"""Checker for meeting-actions: grade ACTIONS.md extracted from the meeting notes.

Only the frozen final files are read; no candidate code is executed.
"""
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result, unchanged  # noqa: E402

NOTES = "notes/2026-11-03-planning.md"


def _date(month_day: int, weekday: str) -> str:
    d = month_day
    return (
        rf"\bnov(?:ember)?\.?\s*0?{d}(?:st|nd|rd|th)?\b"  # Nov 6, November 6th
        rf"|\b0?{d}(?:st|nd|rd|th)?\s+(?:of\s+)?nov"  # 6 Nov, 6th of November
        rf"|\b11/0?{d}\b"  # 11/6, 11/06/2026
        rf"|\b2026-11-0?{d}\b"  # ISO
        rf"|\b{weekday}"  # weekday name
    )


# owner aliases, task keyword regex, due-date regex (None when the notes gave no date)
ITEMS = [
    {"id": "Priya: release announcement email", "owner": ("priya", "raman"),
     "task": r"announc|e-?mail|#release|release[\s-]+(?:notes?|comms|communication)",
     "due": _date(6, r"fri(?:day)?\b"), "dueLabel": "Fri Nov 6"},
    {"id": "Lena: onboarding screens", "owner": ("lena", "fischer"),
     "task": r"on-?\s?boarding|illustration",
     "due": None},
    {"id": "Marco: rate limiting on booking API", "owner": ("marco", "bellini"),
     "task": r"rate[\s-]?limit|throttl|request[\s-]limit",
     "due": _date(12, r"thu(?:r|rs)?(?:sday)?\b"), "dueLabel": "Thu Nov 12"},
    {"id": "Jonah: waitlist ticket summary", "owner": ("jonah", "price"),
     "task": r"wait[\s-]?list|ticket|summary",
     "due": _date(9, r"mon(?:day)?\b"), "dueLabel": "Mon Nov 9"},
    {"id": "Aiko: schedule pilot kickoff call", "owner": ("aiko", "tanaka"),
     "task": r"kick[\s-]?off|pilot|harbor|pulse|studios",
     "due": None},
]

# Items that are in the notes but are not open action items.
NOT_ACTIONS = {
    "completed item (Sam's timezone fix)": r"time[\s-]?zone|\bdst\b|recurring",
    "speculative item (Marco's dark mode)": r"dark[\s-]?mode",
}

CHECKLIST = re.compile(r"^\s*(?:>\s*)?(?:[-*+]|\d+[.)])\s*\[\s*[xX✓✔]?\s*\]\s*(.*\S)\s*$")
OWNER_SPLIT = re.compile(r"^(.{1,60}?)(?::|\s[—–-]{1,2}\s)")


def find_actions_file(final: Path):
    root = final / "ACTIONS.md"
    if root.is_file():
        return root
    cands = sorted(p for p in final.rglob("*") if p.is_file() and p.name.lower() == "actions.md"
                   and "__pycache__" not in p.parts)
    return cands[0] if cands else None


def parse_lines(text: str) -> list[dict]:
    out = []
    for raw in text.splitlines():
        m = CHECKLIST.match(raw)
        if not m:
            continue
        body = re.sub(r"[*_`~]", "", m.group(1)).strip()
        body = re.sub(r"^owner\s*:\s*", "", body, flags=re.I)
        om = OWNER_SPLIT.match(body)
        owner = om.group(1) if om else " ".join(body.split()[:2])
        out.append({"raw": raw.strip(), "low": body.lower(),
                    "ownerWords": set(re.findall(r"[a-z]+", owner.lower()))})
    return out


def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, [NOTES])]
    path = find_actions_file(final)
    out.append(result("ACTIONS.md exists", path is not None,
                      "" if path else "no ACTIONS.md in the workspace"))
    text = ""
    if path is not None:
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError as e:
            text = ""
            out[-1] = result("ACTIONS.md exists", False, f"unreadable: {e}")
    lines = parse_lines(text)
    out.append(result("checklist lines in '- [ ] Owner: task' form", len(lines) > 0,
                      f"{len(lines)} checklist line(s) parsed"))

    used: set[int] = set()
    for item in ITEMS:
        cands = [i for i, ln in enumerate(lines) if i not in used
                 and ln["ownerWords"] & set(item["owner"]) and re.search(item["task"], ln["low"])]
        # If the item was split over several lines, match the one carrying the due date.
        dated = [i for i in cands if item["due"] and re.search(item["due"], lines[i]["low"])]
        hit = (dated or cands or [None])[0]
        if hit is not None:
            used.add(hit)
        out.append(result(f"action item listed: {item['id']}", hit is not None,
                          f"matched: {lines[hit]['raw']}" if hit is not None else "no checklist line with this owner and task"))
        if item["due"]:
            ok = hit is not None and re.search(item["due"], lines[hit]["low"]) is not None
            out.append(result(f"due date kept: {item['id']} ({item['dueLabel']})", ok,
                              lines[hit]["raw"] if hit is not None else "item missing"))

    bad = []
    for label, rx in NOT_ACTIONS.items():
        for ln in lines:
            if re.search(rx, ln["low"]):
                bad.append(f"{label}: {ln['raw']}")
    out.append(result("completed / speculative items not listed", not bad, "; ".join(bad)))

    extras = [ln["raw"] for i, ln in enumerate(lines) if i not in used]
    out.append(result("no more than one extra checklist line", len(extras) <= 1,
                      f"{len(extras)} unmatched line(s): " + " | ".join(extras)))
    return out
