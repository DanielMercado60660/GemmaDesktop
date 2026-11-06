"""Group order lines into stops and put them in walking order."""
from __future__ import annotations

from dataclasses import dataclass

from .models import Order, OrderLine, parse_location

# Pickers visit zones in this order; any other zone comes last, alphabetically.
WALK_ORDER = ["R", "A", "B", "C", "D", "M"]


@dataclass
class Stop:
    zone: str
    lines: list[OrderLine]

    @property
    def units(self) -> int:
        return sum(line.qty for line in self.lines)


def zone_rank(zone: str) -> tuple[int, str]:
    if zone in WALK_ORDER:
        return (WALK_ORDER.index(zone), zone)
    return (len(WALK_ORDER), zone)


def walk_key(line: OrderLine) -> tuple[int, int, str]:
    """Serpentine path: odd aisles are walked bay 1 first, even aisles in reverse."""
    loc = parse_location(line.location)
    bay = loc.bay if loc.aisle % 2 else -loc.bay
    return (loc.aisle, bay, line.sku)


def group_by_zone(lines, groups={}):
    """Group order lines by zone.

    Pass ``groups`` to add the lines to an existing grouping, for example
    when several orders are merged into one picking wave.
    """
    for line in lines:
        zone = parse_location(line.location).zone
        groups.setdefault(zone, []).append(line)
    return groups


def _stops(groups: dict[str, list[OrderLine]]) -> list[Stop]:
    return [Stop(zone, sorted(groups[zone], key=walk_key)) for zone in sorted(groups, key=zone_rank)]


def pick_sheet(order: Order) -> list[Stop]:
    """Stops for a single order, in walking order."""
    return _stops(group_by_zone(order.lines))


def build_wave(orders: list[Order]) -> list[Stop]:
    """Merge several orders into one picking wave."""
    groups: dict[str, list[OrderLine]] = {}
    for order in orders:
        group_by_zone(order.lines, groups)
    return _stops(groups)
