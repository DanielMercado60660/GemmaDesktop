"""Parcel rate card.

All prices are in US dollars and are returned as Decimal rounded to the cent.
"""
from decimal import Decimal, ROUND_CEILING, ROUND_HALF_UP

ZONES = (1, 2, 3, 4)
BASE = {1: Decimal("4.50"), 2: Decimal("6.25"), 3: Decimal("8.90"), 4: Decimal("12.40")}
PER_KG = {1: Decimal("0.80"), 2: Decimal("1.10"), 3: Decimal("1.45"), 4: Decimal("2.05")}
INCLUDED_KG = Decimal("0.5")
MAX_KG = Decimal("30")
EXPRESS_FACTOR = Decimal("1.5")
TENTH = Decimal("0.1")
CENT = Decimal("0.01")


def calc(weight_kg, zone, express=False):
    """Return the charge for one parcel of ``weight_kg`` sent to ``zone``.

    The first 0.5 kg is included in the zone's base price. Extra weight is
    billed per kg, rounded up to the next 0.1 kg. Express service multiplies
    the whole charge by 1.5.
    """
    if zone not in BASE:
        raise ValueError(f"unknown zone {zone!r}")
    weight = Decimal(str(weight_kg))
    if weight <= 0 or weight > MAX_KG:
        raise ValueError(f"weight must be more than 0 and at most {MAX_KG} kg, got {weight_kg}")
    extra = max(weight - INCLUDED_KG, Decimal(0)).quantize(TENTH, rounding=ROUND_CEILING)
    charge = BASE[zone] + PER_KG[zone] * extra
    if express:
        charge *= EXPRESS_FACTOR
    return charge.quantize(CENT, rounding=ROUND_HALF_UP)


def zone_for(postcode):
    """Map a US ZIP code to a shipping zone using its first digit."""
    first = str(postcode).strip()[:1]
    if not first.isdigit():
        raise ValueError(f"bad postcode {postcode!r}")
    digit = int(first)
    if digit <= 2:
        return 1
    if digit <= 4:
        return 2
    if digit <= 7:
        return 3
    return 4
