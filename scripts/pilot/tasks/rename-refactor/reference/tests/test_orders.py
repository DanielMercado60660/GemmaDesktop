import unittest
from decimal import Decimal
from unittest import mock

from parcels import shipping_cost, calculate_tax, order_total
from parcels.orders import shipping_for

ORDER = {"goods": "120.00", "state": "CA", "postcode": "94103", "parcels": [1.2, 3.75]}


class OrderTests(unittest.TestCase):
    def test_tax_by_state(self):
        self.assertEqual(calculate_tax("120.00", "CA"), Decimal("8.70"))
        self.assertEqual(calculate_tax("120.00", "ny"), Decimal("4.80"))
        self.assertEqual(calculate_tax("120.00", "WA"), Decimal("0.00"))

    def test_shipping_is_sum_of_parcels(self):
        self.assertEqual(shipping_for(ORDER), shipping_cost(1.2, 4) + shipping_cost(3.75, 4))
        self.assertEqual(shipping_for(ORDER), Decimal("33.01"))

    def test_shipping_prices_each_parcel_once(self):
        with mock.patch("parcels.orders.shipping_cost", return_value=Decimal("1.00")) as fake:
            self.assertEqual(shipping_for(ORDER), Decimal("2.00"))
        self.assertEqual(fake.call_count, 2)
        fake.assert_any_call(3.75, 4, False)

    def test_order_total(self):
        self.assertEqual(order_total(ORDER), Decimal("161.71"))

    def test_express_order_total(self):
        order = dict(ORDER, express=True, state="ny", postcode="10001")
        self.assertEqual(order_total(order), Decimal("143.10"))


if __name__ == "__main__":
    unittest.main()
