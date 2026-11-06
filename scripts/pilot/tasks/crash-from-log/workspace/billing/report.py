"""Nightly billing report, run by the cron job after invoices are closed."""
from __future__ import annotations

import logging
from decimal import Decimal

log = logging.getLogger("billing.report")


def nightly_summary(invoices) -> tuple[list[str], Decimal]:
    """Price every closed invoice and return the report lines and grand total."""
    lines = []
    grand_total = Decimal("0.00")
    for inv in invoices:
        log.info("pricing %s customer=%r items=%d", inv.number, inv.customer, len(inv.items))
        total = inv.total()
        grand_total += total
        lines.append(f"{inv.number:<10} {inv.customer:<24} {total:>10}")
    log.info("priced %d invoices, grand total %s", len(lines), grand_total)
    return lines, grand_total
