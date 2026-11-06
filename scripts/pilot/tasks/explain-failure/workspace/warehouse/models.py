"""Orders, order lines and rack location labels."""
from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(frozen=True)
class Location:
    zone: str
    aisle: int
    bay: int  # 0-based internally; the printed labels start at 1

    @property
    def label(self) -> str:
        return f"{self.zone}-{self.aisle:02d}-{self.bay + 1}"


def parse_location(text: str) -> Location:
    """Parse a rack label such as 'B-07-3' (zone, aisle, bay)."""
    parts = text.strip().upper().split("-")
    if len(parts) != 3:
        raise ValueError(f"bad location label: {text!r}")
    zone, aisle, bay = parts
    if not zone.isalpha() or not aisle.isdigit() or not bay.isdigit() or int(bay) < 1:
        raise ValueError(f"bad location label: {text!r}")
    return Location(zone, int(aisle), int(bay) - 1)


@dataclass(frozen=True)
class OrderLine:
    sku: str
    qty: int
    location: str

    def __post_init__(self):
        if self.qty < 1:
            raise ValueError(f"{self.sku}: quantity must be at least 1")


@dataclass
class Order:
    order_id: str
    lines: list[OrderLine] = field(default_factory=list)

    def add(self, sku: str, qty: int, location: str) -> "Order":
        self.lines.append(OrderLine(sku, qty, location))
        return self
