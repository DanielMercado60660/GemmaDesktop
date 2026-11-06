"""Plain-text pick sheets."""
from __future__ import annotations

from .models import parse_location


def render(stops, title: str = "Pick sheet") -> str:
    out = [title, "=" * len(title)]
    for n, stop in enumerate(stops, 1):
        out.append(f"{n}. Zone {stop.zone} ({stop.units} units)")
        for line in stop.lines:
            out.append(f"   [ ] {parse_location(line.location).label}  {line.sku} x{line.qty}")
    return "\n".join(out)
