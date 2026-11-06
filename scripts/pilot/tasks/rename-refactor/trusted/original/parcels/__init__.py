"""Parcel shipping prices for the web shop."""
from .orders import calculate_tax, order_total
from .quotes import quote, quote_table, split_shipment
from .rates import calc, zone_for

__all__ = [
    "calc",
    "calculate_tax",
    "order_total",
    "quote",
    "quote_table",
    "split_shipment",
    "zone_for",
]
