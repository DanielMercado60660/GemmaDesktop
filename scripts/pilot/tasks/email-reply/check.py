"""Checker for email-reply: a policy-grounded customer-service reply in reply.md.

Truth comes from trusted/ copies of the email and the policy, never from the
candidate's workspace. Only reply.md is read; no candidate code is executed.
Quoted lines ("> ...") are ignored, and e-mail header lines (Subject:, To:, ...)
count only for the order number, so echoing the customer's subject line does
not address anything by itself.

Facts the reply must carry (policies/refunds.md):
  duplicate charge -> reversed, 3-5 business days to show on the statement
  replacement      -> shipping is free for the customer; needs a photo first
  late Express     -> the Express shipping fee ($12.95) is refunded
Things it must not do: invent or misapply timeframes/amounts/percentages,
offer goodwill (store credit, discount codes...), ask for the lamp back.
"""
import re
import sys
from itertools import combinations
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from checklib import result, unchanged  # noqa: E402

HERE = Path(__file__).resolve().parent
TRUSTED = HERE / "trusted"
PROTECTED = ["inbox/complaint.txt", "policies/refunds.md"]
MAX_WORDS = 250

# ---------------------------------------------------------------- patterns
NUMWORDS = {"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
            "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
            "fifteen": 15, "twenty": 20, "thirty": 30, "forty-five": 45, "sixty": 60, "ninety": 90}
_NW = "|".join(sorted((re.escape(w) for w in NUMWORDS), key=len, reverse=True))
NUM = rf"(?:\d+(?:\.\d+)?|(?:{_NW})\b)"
DURATION = re.compile(
    rf"(?<![\w.$])(?:between\s+)?(?P<a>{NUM})(?:\s*(?:-to-|-|–|—|‑|\bto\b|\band\b)\s*(?P<b>{NUM}))?[\s-]+"
    rf"(?:(?:business|working|calendar|bank(?:ing)?)[\s-]+)?(?P<unit>days?|hours?|hrs?|weeks?|months?)\b",
    re.I)
# Context that marks a duration as describing the delivery delay, not a promise.
LATE_AFTER = re.compile(r"^\s*(?:late\b|later than|behind|overdue|past (?:the|its|your)|beyond (?:the|its|your)|delay|"
                        r"after (?:the|its|your|it was) (?:\w+ ){0,2}?(?:promised|guarantee\w*|expected|estimated|scheduled|delivery|due)|"
                        # elapsed time since the delivery ("you reported it 4 days after delivery")
                        r"(?:after|of|since|from) (?:the )?(?:delivery|receiv\w*|it arrived|arrival|the order arrived))",
                        re.I)
LATE_BEFORE = re.compile(r"(?:arriv\w*|took|taking|waited|waiting|delayed(?:\s+by)?|late by|delay of)\s+"
                         r"(?:(?:about|nearly|almost|around|roughly|over|more than|just|by|a full)\s+)*$", re.I)

ORDER = re.compile(r"(?<![A-Za-z0-9])A\s?[-‐‑–—]?\s?10482(?!\d)|#\s?10482(?!\d)|order\s+(?:number\s+|no\.?\s+|#\s?)?10482(?!\d)", re.I)
POLITE = re.compile(r"sorry|apolog|thank|appreciate|regret|understand (?:how|your|that|why)|frustrat", re.I)

LATE = re.compile(r"\blate\b|\blateness\b|\bdelay|behind schedule|took longer|later than|\bon time\b|overdue|"
                  r"guarantee\w*\s+(?:\w+\s+){0,3}?(?:date|by|for)\b|(?:arrived|came|delivered|showed up)\s+(?:on\s+)?(?:thursday,?\s+)?oct\w*\.?\s*29|"
                  r"(?:didn't|did not|failed to)\s+(?:arrive|get there|reach)|"
                  r"(?:not|wasn't|weren't)\s+(?:delivered\s+|arrive\w*\s+)?on time|"
                  r"missed\s+(?:the|your|its)\s+(?:delivery|guarante|promised|deadline|date)|"
                  r"after\s+(?:the|its|your)\s+(?:\w+\s+)?(?:guarante|promised|expected|estimated|scheduled)", re.I)
DAMAGE = re.compile(r"damag|\bbroke|crack|\bchip|shatter|defect", re.I)
DOUBLE = re.compile(r"duplicate|double[\s-]?(?:charg|bill)|\btwice\b|two\s+(?:separate\s+)?charges|second\s+charge|"
                    r"extra\s+charge|charged\s+(?:you\s+)?(?:more\s+than\s+once|two\s+times)|overcharg", re.I)
DUP_CUE = re.compile(DOUBLE.pattern + r"|revers|refund(?:ed)? (?:of )?the (?:extra|second)|164\.95|extra \$", re.I)

REPL = re.compile(r"replac", re.I)
FREE = re.compile(r"\bfree\b|free of charge|\bno\s+(?:extra\s+|additional\s+|shipping\s+)?(?:cost|charge|fee)s?\b|"
                  r"at no (?:extra |additional )?(?:cost|charge)|"
                  r"(?:won't|will not|don't|do not|wouldn't|shouldn't|never|not)\s+(?:\w+\s+){0,3}?(?:pay|be charged|charge you)|"
                  r"(?:we|we'll|we will|fernhill)\s+(?:will\s+)?(?:cover|pay for|pay the|take care of)|"
                  r"\bon us\b|covered by (?:us|fernhill|our)|paid by us|at our (?:own )?(?:cost|expense)|complimentary|pays? nothing|nothing (?:extra |more )?(?:to pay|for)|cost you nothing|"
                  r"without (?:any )?(?:charge|cost)|\$0(?:\.00)?\b", re.I)
PAYS = re.compile(r"(?:you(?:'ll| will)?|customer)\s+(?:need to |have to |must |would need to |will need to )?"
                  r"(?:pay|cover)\s+(?:for\s+)?(?:the\s+)?(?:standard\s+|return\s+|replacement\s+)?(?:shipping|postage|delivery)|"
                  r"shipping\s+(?:fees?|costs?|charges?)\s+(?:of\s+\$[\d.]+\s+)?(?:will\s+)?appl|"
                  r"standard (?:shipping )?rates apply|at your (?:own )?(?:cost|expense)", re.I)
NEGATION = re.compile(r"\bno\b|\bnot\b|n't\b|\bnever\b|\bfree\b|\bwithout\b", re.I)

REFUNDISH = re.compile(r"refund|reimburs|\bwaiv|credit(?:ed)?\s+back|pay\s+(?:you\s+)?back|money back|"
                       r"back (?:on|to) your (?:card|account)|return (?:the|your) (?:\$12\.95|express|shipping)", re.I)
FEE_STRONG = re.compile(r"12\.95|express", re.I)
FEE_WEAK = re.compile(r"shipping (?:fee|charge|cost)|(?:fee|charge|cost) (?:for|of) (?:the )?shipping|paid for shipping|"
                      r"shipping you paid|delivery fee|shipping (?:you|that you) paid|refund(?:ed)?\s+(?:of\s+)?(?:your|the)\s+shipping\b", re.I)
REFUSAL = re.compile(r"(?:can't|cannot|can not|won't|will not|unable to|not able to|isn't|is not|"
                     r"aren't|are not|don't|do not)\s+(?:\w+\s+){0,3}?(?:refund|compensat|reimburs|eligible)|"
                     r"\bno compensation\b|not eligible", re.I)
# A firm commitment to refund; a sentence with one is not a refusal even if it also
# contrasts with the Standard-shipping rule ("Standard isn't eligible, but we'll refund...").
COMMIT = re.compile(r"(?:\bwe(?:'ll| will| have|'ve| are going to| can| are)|will be|is being|has been|"
                    r"you(?:'ll| will) (?:get|receive|see))\s+(?:\w+\s+){0,4}?(?:refund|reimburs|waiv|credit|return)", re.I)

PHOTO = re.compile(r"\bphoto|\bpicture|\bimage|\bpics?\b|snapshot", re.I)
PHOTO_NEG = re.compile(r"(?:no need|n't need|not need|n't have to|not necessary|unnecessary)\s+(?:\w+\s+){0,4}?"
                       r"(?:photo|picture|image|pics?\b|snapshot)|(?:photo|picture|image|pics?)\w*\s+(?:\w+\s+){0,2}?(?:isn't|is not|aren't|are not|not)\s+(?:needed|necessary|required)", re.I)
# "without a photo" / "no photo" waive the photo unless the sentence states it as a requirement
# ("we can't approve a claim without a photo", "no claim is approved without a photo").
PHOTO_NEG_WEAK = re.compile(r"(?:\bwithout|\bno)\s+(?:\w+\s+){0,4}?(?:photo|picture|image|pics?\b|snapshot)", re.I)
PHOTO_REQUIRED = re.compile(r"can't|cannot|can not|unable|not able|won't|will not|\buntil\b|\bmust\b|requir|\bno\b[^.;]*\bwithout\b", re.I)


def photo_waived(s: str) -> bool:
    return bool(PHOTO_NEG.search(s) or (PHOTO_NEG_WEAK.search(s) and not PHOTO_REQUIRED.search(s)))


AMOUNT = re.compile(r"\$\s?(\d[\d,]*(?:\.\d{1,2})?)|(?<![\w.$,])(\d[\d,]*\.\d{2})(?![\d])")
PERCENT = re.compile(r"(?<!\d)(?!100\s?%)\d+\s?%|\bper\s?cent\b", re.I)
PHONE = re.compile(r"(?<!\d)\d{3}[-.\s]\d{4}(?!\d)")
GOODWILL = re.compile(r"discount|coupon|voucher|promo(?:tional)?\s*code|store credit|gift card|loyalty points|free gift", re.I)
GOODWILL_OK = re.compile(r"can't|cannot|unable|not able|isn't|is not|won't|\bnot\b|supervisor", re.I)
RETURN_ASK = re.compile(r"\b(?:send|ship|mail|post|bring)\s+(?:it|them|the\s+(?:\w+\s+){0,2}?(?:lamp|item|one|product|base))\s+back\b|"
                        r"\breturn\s+(?:the\s+(?:\w+\s+){0,2}?(?:lamp|item|one|product))\b|"
                        r"\breturn(?:\s+shipping)?\s+label\b|\bprepaid\s+(?:shipping\s+)?label\b|\bship(?:ping)?\s+it\s+to\s+us\b", re.I)
RETURN_OK = re.compile(r"\bno need\b|(?:\bnot\b|n't|\bnever\b|\bwithout\b)\s+(?:\w+\s+){0,4}?(?:send|ship|mail|post|return|bring)|"
                       r"\bkeep\b|recycl|dispos|discard|no (?:return|requirement)", re.I)

HEADER = re.compile(r"^\s*(?:subject|re|to|from|cc|bcc|date)\s*:", re.I)


# ---------------------------------------------------------------- helpers
def num_value(token: str):
    t = token.lower()
    if t in NUMWORDS:
        return NUMWORDS[t]
    try:
        v = float(t)
    except ValueError:
        return None
    return int(v) if v == int(v) else v


def unit_of(u: str) -> str:
    u = u.lower()
    return {"d": "day", "h": "hour", "w": "week", "m": "month"}[u[0]]


def clean(text: str) -> str:
    t = text.replace("\\-", "-").replace("\\_", "_").replace("\\$", "$")
    t = re.sub(r"[*_`~]", "", t)
    t = re.sub(r"\(\s*\d+\s*\)", " ", t)  # "five (5) business days"
    t = t.replace("\u00a0", " ").replace("\u2019", "'").replace("\u2018", "'").replace("\u201c", '"').replace("\u201d", '"')
    return t


def durations(text: str, skip_lateness: bool = True):
    out = []
    for m in DURATION.finditer(text):
        a, b = num_value(m.group("a")), num_value(m.group("b")) if m.group("b") else None
        if a is None:
            continue
        if b is None:
            b = a
        if skip_lateness:
            after = text[m.end(): m.end() + 60]
            before = text[max(0, m.start() - 40): m.start()]
            if LATE_AFTER.match(after) or LATE_BEFORE.search(before):
                continue
        out.append(((min(a, b), max(a, b), unit_of(m.group("unit"))), m.group(0)))
    return out


def sentences(lines: list[str]) -> list[str]:
    out = []
    for line in lines:
        out.extend(s.strip() for s in re.split(r"(?<=[.!?])\s+(?=[^\d])", line) if s.strip())
    return out


def amounts(text: str) -> list[float]:
    vals = []
    for m in AMOUNT.finditer(text):
        raw = (m.group(1) or m.group(2)).replace(",", "")
        try:
            vals.append(round(float(raw), 2))
        except ValueError:
            pass
    return vals


def allowed_amounts(email: str, policy: str) -> set[float]:
    base = amounts(email)  # 76.00, 12.95, 164.95, 164.95
    items = [76.0, 76.0] + [v for v in base if v != 76.0]
    ok = {0.0} | set(base) | set(amounts(policy))
    for r in range(1, len(items) + 1):
        for combo in combinations(items, r):
            ok.add(round(sum(combo), 2))
    return ok


def find_reply(final: Path):
    p = final / "reply.md"
    if p.is_file():
        return p
    cands = sorted((q for q in final.rglob("*") if q.is_file() and q.name.lower() == "reply.md"
                    and "__pycache__" not in q.parts), key=lambda q: (len(q.parts), str(q)))
    return cands[0] if cands else None


# ---------------------------------------------------------------- check
def check(final: Path, original: dict, record: dict) -> list[dict]:
    out = [unchanged(final, original, PROTECTED)]
    email = (TRUSTED / "inbox" / "complaint.txt").read_text()
    policy = (TRUSTED / "policies" / "refunds.md").read_text()

    names = ["under 250 words", "mentions order #A-10482", "polite (apology or thanks)",
             "addresses the late delivery", "addresses the damaged lamp", "addresses the double charge",
             "duplicate charge: 3-5 business days to show on the card", "answers: replacement shipping is free",
             "late Express order: shipping fee refunded", "asks for a photo of the damage",
             "no invented or misapplied numbers / goodwill offers", "does not ask for the damaged lamp back"]

    path = find_reply(final)
    if path is None:
        out.append(result("reply.md exists", False, "no reply.md in the workspace"))
        out.extend(result(n, False, "no reply.md") for n in names)
        return out
    raw = path.read_text(encoding="utf-8", errors="replace")
    out.append(result("reply.md exists", bool(raw.strip()), str(path.relative_to(final)) + ("" if raw.strip() else " is empty")))

    all_lines = [l for l in clean(raw).splitlines() if not l.lstrip().startswith(">")]
    body_lines = [l for l in all_lines if not HEADER.match(l)]
    whole = "\n".join(all_lines)
    body = "\n".join(body_lines)
    sents = sentences(body_lines)

    def window(i: int) -> str:
        return " ".join(sents[max(0, i - 1): i + 2])

    # length (quoted lines excluded; markdown markers are not words)
    words = re.findall(r"[A-Za-z0-9$#£€][^\s]*", whole)
    out.append(result("under 250 words", 0 < len(words) < MAX_WORDS, f"{len(words)} words"))

    seen = re.findall(r"[A-Z]?-?\d{4,}", whole)[:8]
    # Advisory only: the prompt never asks the reply to cite the order number,
    # so omitting it is not a failure of the user's request.
    out.append(result("mentions order #A-10482", bool(ORDER.search(whole)),
                      "" if ORDER.search(whole) else f"order number not found; numbers seen: {seen}", critical=False))
    out.append(result("polite (apology or thanks)", bool(POLITE.search(body)), "" if POLITE.search(body) else "no apology/thanks"))

    for name, rx in (("addresses the late delivery", LATE), ("addresses the damaged lamp", DAMAGE),
                     ("addresses the double charge", DOUBLE)):
        m = rx.search(body)
        out.append(result(name, bool(m), f"matched {m.group(0)!r}" if m else "no mention outside the header/quoted lines"))

    # duplicate charge timeline: 3-5 (business) days near a duplicate/reversal cue
    hits = []
    for i, s in enumerate(sents):
        for key, txt in durations(s, skip_lateness=False):
            if key == (3, 5, "day") and DUP_CUE.search(window(i)):
                hits.append(s)
    out.append(result("duplicate charge: 3-5 business days to show on the card", bool(hits),
                      hits[0][:300] if hits else "no '3-5 business days' next to the duplicate charge / reversal"))

    # replacement shipping is free, and nothing says the customer pays for it
    free_hits, pay_hits = [], []
    for i, s in enumerate(sents):
        if REPL.search(window(i)):
            if FREE.search(s):
                free_hits.append(s)
            if PAYS.search(s) and not NEGATION.search(s):
                pay_hits.append(s)
    ok = bool(free_hits) and not pay_hits
    out.append(result("answers: replacement shipping is free", ok,
                      ("says the customer pays: " + " | ".join(pay_hits))[:600] if pay_hits else
                      (free_hits[0][:300] if free_hits else "no statement that the replacement ships free")))

    # late Express order -> shipping fee refunded (not refused)
    fee_hits, refusals = [], []
    for s in sents:
        fee = FEE_STRONG.search(s) or (FEE_WEAK.search(s) and not REPL.search(s))
        if fee and REFUSAL.search(s) and not COMMIT.search(s):
            refusals.append(s)
        elif fee and REFUNDISH.search(s):
            fee_hits.append(s)
    ok = bool(fee_hits) and not refusals
    out.append(result("late Express order: shipping fee refunded", ok,
                      ("refuses: " + " | ".join(refusals))[:600] if refusals else
                      (fee_hits[0][:300] if fee_hits else "no refund of the $12.95 Express shipping fee offered")))

    asked = [s for s in sents if PHOTO.search(s) and not photo_waived(s)]
    waived = [s for s in sents if PHOTO.search(s) and photo_waived(s)]
    out.append(result("asks for a photo of the damage", bool(asked) and not waived,
                      ("says no photo is needed: " + " | ".join(waived))[:600] if waived else
                      (asked[0][:300] if asked else "no photo/picture requested (policy needs one)")))

    # invented / misapplied numbers and offers
    problems = []
    allowed_d = {k for k, _ in durations(policy, skip_lateness=False)}
    for i, s in enumerate(sents):
        for key, txt in durations(s):
            if key not in allowed_d:
                problems.append(f"timeframe not in policy: {txt!r} in {s[:160]!r}")
    ok_amounts = allowed_amounts(email, policy)
    for s in sents:
        for v in amounts(s):
            if v == 5.95 and not NEGATION.search(s) and not re.search(r"waiv|doesn't apply|does not apply", s, re.I):
                problems.append(f"$5.95 return fee applied: {s[:160]!r}")
            elif v not in ok_amounts and v != 5.95:
                problems.append(f"amount not in email/policy: {v} in {s[:160]!r}")
        if PERCENT.search(s):
            problems.append(f"percentage offered: {s[:160]!r}")
        if GOODWILL.search(s) and not GOODWILL_OK.search(s):
            problems.append(f"goodwill offer (needs supervisor): {s[:160]!r}")
        for ph in PHONE.findall(s):
            if re.sub(r"\D", "", ph) != "5550142":
                problems.append(f"phone number not in policy: {ph!r}")
    out.append(result("no invented or misapplied numbers / goodwill offers", not problems, " | ".join(problems)))

    asks = [s for s in sents if RETURN_ASK.search(s) and not RETURN_OK.search(s)]
    out.append(result("does not ask for the damaged lamp back", not asks, " | ".join(asks)[:600]))
    return out
