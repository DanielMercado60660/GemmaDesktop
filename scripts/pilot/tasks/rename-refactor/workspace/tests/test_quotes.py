import unittest
from decimal import Decimal

from parcels import quote, quote_table, rates, split_shipment
from parcels.quotes import SERVICES


class QuoteTests(unittest.TestCase):
    def test_standard_service_is_the_rate_card(self):
        self.assertIs(SERVICES["standard"], rates.calc)

    def test_express_quote_matches_calc(self):
        self.assertEqual(quote(2, 3, "express"), rates.calc(2, 3, express=True))
        self.assertEqual(quote(2, 3, "express"), Decimal("16.61"))

    def test_unknown_service(self):
        with self.assertRaises(ValueError):
            quote(2, 3, "overnight")

    def test_quote_table_covers_every_zone(self):
        self.assertEqual(
            quote_table(2),
            {1: Decimal("5.70"), 2: Decimal("7.90"), 3: Decimal("11.08"), 4: Decimal("15.48")},
        )

    def test_split_shipment(self):
        self.assertEqual(split_shipment(25, 2), (3, Decimal("44.82")))
        self.assertEqual(split_shipment(7, 1, express=True), (1, Decimal("14.55")))


if __name__ == "__main__":
    unittest.main()
