"""Order totals: goods, sales tax on the goods, and shipping."""
from decimal import Decimal, ROUND_HALF_UP

from .rates import calc, zone_for

TAX_RATES = {"CA": Decimal("0.0725"), "NY": Decimal("0.04"), "TX": Decimal("0.0625"), "OR": Decimal("0")}
CENT = Decimal("0.01")


def calculate_tax(amount, state):
    """Sales tax on ``amount`` for a US state code (unlisted states pay none)."""
    rate = TAX_RATES.get(state.upper(), Decimal("0"))
    return (Decimal(str(amount)) * rate).quantize(CENT, rounding=ROUND_HALF_UP)


def shipping_for(order):
    """Shipping for the whole order: calc() for every parcel, summed."""
    zone = zone_for(order["postcode"])
    express = order.get("express", False)
    return sum((calc(weight, zone, express) for weight in order["parcels"]), Decimal("0.00"))


def order_total(order):
    """Goods plus tax on the goods plus shipping (shipping is not taxed)."""
    goods = Decimal(str(order["goods"]))
    calculated = goods + calculate_tax(goods, order["state"]) + shipping_for(order)
    return calculated.quantize(CENT)
