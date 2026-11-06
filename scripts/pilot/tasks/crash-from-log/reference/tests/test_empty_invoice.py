"""Regression test for the nightly billing crash on invoices with no line items."""
import unittest
from decimal import Decimal as D

from billing import Invoice
from billing.report import nightly_summary


class EmptyInvoiceTests(unittest.TestCase):
    def test_empty_invoice_totals_zero(self):
        inv = Invoice("INV-10476", "Harbor Supply Co", tax_rate=D("0.0825"))
        self.assertEqual(inv.tax(), D("0.00"))
        self.assertEqual(inv.total(), D("0.00"))

    def test_empty_invoice_with_credit_totals_zero(self):
        inv = Invoice("INV-2", "Harbor Supply Co", tax_rate=D("0.0825"), credit=D("15"))
        self.assertEqual(inv.total(), D("0.00"))

    def test_nightly_summary_survives_empty_invoice(self):
        lines, grand = nightly_summary([Invoice("INV-10476", "Harbor Supply Co")])
        self.assertEqual(len(lines), 1)
        self.assertEqual(grand, D("0.00"))


if __name__ == "__main__":
    unittest.main()
