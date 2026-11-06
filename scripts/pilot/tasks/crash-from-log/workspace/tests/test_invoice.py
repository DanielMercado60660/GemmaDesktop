import unittest
from decimal import Decimal as D

from billing import Invoice, LineItem
from billing.report import nightly_summary


def sample(**kwargs):
    inv = Invoice("INV-1", "Acme Corp", **kwargs)
    inv.add(LineItem("WID-1", "Widget", D("19.99"), 3))
    inv.add(LineItem("SVC-9", "Setup (not taxable)", D("40.00"), 1, taxable=False))
    return inv


class InvoiceTests(unittest.TestCase):
    def test_subtotal_multiplies_quantity(self):
        self.assertEqual(sample().subtotal(), D("99.97"))

    def test_tax_only_on_taxable_lines(self):
        inv = sample(tax_rate=D("0.08"))
        self.assertEqual(inv.tax(), D("4.80"))
        self.assertEqual(inv.total(), D("104.77"))

    def test_credit_reduces_total_and_tax(self):
        inv = sample(tax_rate=D("0.08"), credit=D("20.00"))
        self.assertEqual(inv.discount(), D("20.00"))
        self.assertEqual(inv.tax(), D("3.84"))
        self.assertEqual(inv.total(), D("83.81"))

    def test_credit_never_exceeds_subtotal(self):
        inv = sample(tax_rate=D("0.08"), credit=D("500"))
        self.assertEqual(inv.discount(), D("99.97"))
        self.assertEqual(inv.total(), D("0.00"))

    def test_nightly_summary_adds_up(self):
        lines, grand = nightly_summary([sample(), sample(credit=D("9.97"))])
        self.assertEqual(len(lines), 2)
        self.assertEqual(grand, D("189.97"))


if __name__ == "__main__":
    unittest.main()
