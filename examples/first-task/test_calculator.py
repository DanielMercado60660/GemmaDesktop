import unittest

from calculator import add


class ArithmeticTests(unittest.TestCase):
    def test_positive(self):
        self.assertEqual(add(3, 4), 7)

    def test_negative(self):
        self.assertEqual(add(-3, -4), -7)

    def test_mixed(self):
        self.assertEqual(add(-3, 4), 1)


if __name__ == "__main__":
    unittest.main()
