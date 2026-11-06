import unittest

from warehouse import Order, build_wave, pick_sheet


def order(order_id, *lines):
    o = Order(order_id)
    for sku, qty, location in lines:
        o.add(sku, qty, location)
    return o


class PickSheetTests(unittest.TestCase):
    def test_pick_sheet_follows_walk_path(self):
        sheet = pick_sheet(order("SO-1001", ("MUG-12", 2, "C-03-1"), ("TEA-01", 1, "A-01-4"), ("LID-12", 2, "C-03-2")))
        self.assertEqual([stop.zone for stop in sheet], ["A", "C"])
        self.assertEqual([line.sku for line in sheet[1].lines], ["MUG-12", "LID-12"])

    def test_pick_sheet_includes_every_line(self):
        sheet = pick_sheet(order("SO-1002", ("BOX-S", 10, "D-05-2"), ("TAPE-1", 4, "D-06-1"), ("PEN-1", 3, "R-01-1")))
        picked = {line.sku for stop in sheet for line in stop.lines}
        for sku in ("BOX-S", "TAPE-1", "PEN-1"):
            self.assertIn(sku, picked)

    def test_pick_sheet_single_zone(self):
        sheet = pick_sheet(order("SO-1003", ("CUP-08", 2, "B-02-1"), ("CUP-12", 3, "B-02-4")))
        self.assertEqual([stop.zone for stop in sheet], ["B"])
        self.assertEqual(sheet[0].units, 5)


class WaveTests(unittest.TestCase):
    def test_wave_merges_orders_by_zone(self):
        wave = build_wave([
            order("SO-2001", ("MUG-12", 1, "C-03-1"), ("TEA-01", 2, "A-01-4")),
            order("SO-2002", ("MUG-12", 3, "C-03-1"), ("BAG-L", 1, "X-01-1")),
        ])
        self.assertEqual([stop.zone for stop in wave], ["A", "C", "X"])
        self.assertEqual(wave[1].units, 4)

    def test_wave_serpentine_within_zone(self):
        wave = build_wave([order("SO-2003", ("A1", 1, "A-01-1"), ("A3", 1, "A-01-3"), ("B4", 1, "A-02-4"), ("B2", 1, "A-02-2"))])
        self.assertEqual([line.sku for line in wave[0].lines], ["A1", "A3", "B4", "B2"])

    def test_empty_wave(self):
        self.assertEqual(build_wave([]), [])
