"""Quotes for the shipping calculator page on the website."""
from decimal import Decimal, ROUND_CEILING
from functools import partial

from . import rates

# Service level -> pricing function taking (weight_kg, zone).
SERVICES = {
    "standard": rates.calc,
    "express": partial(rates.calc, express=True),
}


def quote(weight_kg, zone, service="standard"):
    """Price one parcel for a service level ("standard" or "express")."""
    try:
        price = SERVICES[service]
    except KeyError:
        raise ValueError(f"unknown service {service!r}") from None
    return price(weight_kg, zone)


def quote_table(weight_kg, service="standard"):
    """Price for every zone, e.g. {1: Decimal("5.30"), 2: ..., 4: ...}."""
    return {zone: quote(weight_kg, zone, service) for zone in rates.ZONES}


def split_shipment(total_kg, zone, max_parcel_kg=10, express=False):
    """Split ``total_kg`` into the fewest equal parcels of at most
    ``max_parcel_kg`` each. Returns (parcel count, total charge), where every
    parcel is priced with rates.calc at its rounded-up weight."""
    total = Decimal(str(total_kg))
    count = int((total / Decimal(str(max_parcel_kg))).to_integral_value(rounding=ROUND_CEILING))
    each = (total / count).quantize(Decimal("0.001"), rounding=ROUND_CEILING)
    return count, rates.calc(each, zone, express=express) * count
