import unittest

from text_utils import slugify


class SlugifyTests(unittest.TestCase):
    def test_normal_text(self):
        self.assertEqual(slugify("Hello World"), "hello-world")
        self.assertEqual(slugify("Python 3 Tips"), "python-3-tips")

    def test_punctuation(self):
        self.assertEqual(slugify("Hello, World!"), "hello-world")
        self.assertEqual(slugify("What's new in v2.0?"), "what-s-new-in-v2-0")

    def test_repeated_separators(self):
        self.assertEqual(slugify("one   two---three"), "one-two-three")
        self.assertEqual(slugify("a -- b __ c"), "a-b-c")

    def test_leading_and_trailing_spaces(self):
        self.assertEqual(slugify("   padded title   "), "padded-title")
        self.assertEqual(slugify("--already-dashed--"), "already-dashed")

    def test_accented_characters(self):
        self.assertEqual(slugify("Crème Brûlée"), "creme-brulee")
        self.assertEqual(slugify("Déjà vu à São Paulo"), "deja-vu-a-sao-paulo")

    def test_custom_separator(self):
        self.assertEqual(slugify("Hello World", sep="_"), "hello_world")
        self.assertEqual(slugify("  Many   spaces, here ", sep="."), "many.spaces.here")

    def test_empty_input(self):
        self.assertEqual(slugify(""), "")

    def test_nothing_usable(self):
        self.assertEqual(slugify("   "), "")
        self.assertEqual(slugify("!!!???"), "")


if __name__ == "__main__":
    unittest.main()
