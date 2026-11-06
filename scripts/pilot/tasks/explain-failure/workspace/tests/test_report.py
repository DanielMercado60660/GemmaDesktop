import unittest

from warehouse import Order, build_wave, render


class RenderTests(unittest.TestCase):
    def test_render_wave(self):
        o = Order("SO-3001").add("MUG-12", 2, "c-3-1").add("TEA-01", 1, "A-01-4")
        text = render(build_wave([o]), title="Wave 7")
        self.assertEqual(text.splitlines(), [
            "Wave 7",
            "======",
            "1. Zone A (1 units)",
            "   [ ] A-01-4  TEA-01 x1",
            "2. Zone C (2 units)",
            "   [ ] C-03-1  MUG-12 x2",
        ])

    def test_render_nothing_to_pick(self):
        self.assertEqual(render([]), "Pick sheet\n==========")
