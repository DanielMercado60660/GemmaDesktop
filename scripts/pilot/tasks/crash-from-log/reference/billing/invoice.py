"""Invoice pricing: line items, account credit and sales tax."""
from __future__ import annotations

from dataclasses import dataclass, field
from decimal import ROUND_HALF_UP, Decimal

CENT = Decimal("0.01")


def to_money(value) -> Decimal:
    """Round to whole cents, half up (the way our accounting system rounds)."""
    return Decimal(value).quantize(CENT, rounding=ROUND_HALF_UP)


@dataclass
class LineItem:
    sku: str
    description: str
    unit_price: Decimal
    quantity: int = 1
    taxable: bool = True

    def amount(self) -> Decimal:
        return self.unit_price * self.quantity


@dataclass
class Invoice:
    number: str
    customer: str
    items: list[LineItem] = field(default_factory=list)
    tax_rate: Decimal = Decimal("0")
    credit: Decimal = Decimal("0")  # store credit / coupon for the whole invoice

    def add(self, item: LineItem) -> None:
        self.items.append(item)

    def subtotal(self) -> Decimal:
        return to_money(sum((item.amount() for item in self.items), Decimal("0")))

    def discount(self) -> Decimal:
        """Credit used on this invoice; it never exceeds the subtotal."""
        return min(to_money(self.credit), self.subtotal())

    def tax(self) -> Decimal:
        subtotal = self.subtotal()
        if subtotal == 0:
            # Nothing billed (e.g. an invoice with no line items): no tax, and
            # no share to compute -- dividing by a zero subtotal used to crash.
            return to_money(0)
        # The credit is spread over the lines in proportion to their value, so
        # tax is only charged on the share of each taxable line that is paid for.
        paid_share = (subtotal - self.discount()) / subtotal
        taxable = sum((item.amount() for item in self.items if item.taxable), Decimal("0"))
        return to_money(taxable * paid_share * self.tax_rate)

    def total(self) -> Decimal:
        return self.subtotal() - self.discount() + self.tax()
