import unittest
from decimal import Decimal

from parcels.rates import calc, zone_for


class CalcTests(unittest.TestCase):
    def test_included_weight_is_base_price(self):
        self.assertEqual(calc(0.5, 1), Decimal("4.50"))
        self.assertEqual(calc(0.01, 1), Decimal("4.50"))

    def test_extra_weight_rounds_up_to_next_tenth(self):
        # 1.23 kg is 0.73 kg over the included weight, billed as 0.8 kg.
        self.assertEqual(calc(1.23, 2), Decimal("7.13"))

    def test_half_cents_round_up(self):
        # 8.90 + 1.45 * 0.1 = 9.045
        self.assertEqual(calc(0.6, 3), Decimal("9.05"))

    def test_express_multiplies_whole_charge(self):
        self.assertEqual(calc(1.23, 2, express=True), Decimal("10.70"))

    def test_heaviest_allowed_parcel(self):
        self.assertEqual(calc(30, 4), Decimal("72.88"))

    def test_calc_rejects_bad_input(self):
        for weight, zone in [(1, 9), (0, 1), (-2, 1), (30.5, 2)]:
            with self.subTest(weight=weight, zone=zone):
                with self.assertRaises(ValueError):
                    calc(weight, zone)


class ZoneTests(unittest.TestCase):
    def test_first_digit_picks_zone(self):
        self.assertEqual([zone_for(z) for z in ("10001", "30301", "60601", "94103")], [1, 2, 3, 4])

    def test_bad_postcode(self):
        with self.assertRaises(ValueError):
            zone_for("SW1A 1AA")


if __name__ == "__main__":
    unittest.main()
