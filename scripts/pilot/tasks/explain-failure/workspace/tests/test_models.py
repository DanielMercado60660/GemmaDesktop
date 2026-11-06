import unittest

from warehouse import Location, Order, OrderLine, parse_location


class LocationTests(unittest.TestCase):
    def test_parse_label(self):
        self.assertEqual(parse_location("B-07-3"), Location("B", 7, 2))

    def test_parse_is_case_and_space_tolerant(self):
        self.assertEqual(parse_location(" c-3-1 "), Location("C", 3, 0))

    def test_label_round_trip(self):
        for text in ("A-01-1", "M-12-9", "R-02-10"):
            self.assertEqual(parse_location(text).label, text)

    def test_rejects_bad_labels(self):
        for text in ("B-07", "7-07-3", "B-x-3", "B-07-0", ""):
            with self.assertRaises(ValueError, msg=text):
                parse_location(text)


class OrderTests(unittest.TestCase):
    def test_add_returns_order_for_chaining(self):
        order = Order("SO-1").add("PEN-1", 3, "A-01-1").add("INK-2", 1, "A-01-2")
        self.assertEqual([line.sku for line in order.lines], ["PEN-1", "INK-2"])

    def test_quantity_must_be_positive(self):
        with self.assertRaises(ValueError):
            OrderLine("PEN-1", 0, "A-01-1")
