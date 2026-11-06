import unittest

from textkit import tokenize, top_n, word_counts


class TokenizeTests(unittest.TestCase):
    def test_lowercases_words(self):
        self.assertEqual(tokenize("The Cat sat"), ["the", "cat", "sat"])

    def test_strips_sentence_punctuation(self):
        self.assertEqual(tokenize("Stop. Wait, go!"), ["stop", "wait", "go"])

    def test_keeps_inner_apostrophes_and_hyphens(self):
        self.assertEqual(tokenize("Don't over-think it"), ["don't", "over-think", "it"])

    def test_strips_quotes_and_brackets(self):
        self.assertEqual(tokenize('She said "hi" (twice).'), ["she", "said", "hi", "twice"])


class WordCountTests(unittest.TestCase):
    def test_counts_ignore_case(self):
        self.assertEqual(word_counts("Red red RED blue"), {"red": 3, "blue": 1})


class TopNTests(unittest.TestCase):
    def test_orders_by_count(self):
        self.assertEqual(top_n("a b b c c c", 2), [("c", 3), ("b", 2)])

    def test_ties_are_alphabetical(self):
        self.assertEqual(
            top_n("pear apple pear apple fig", 3),
            [("apple", 2), ("pear", 2), ("fig", 1)],
        )

    def test_returns_everything_when_n_is_large(self):
        self.assertEqual(top_n("one two", 10), [("one", 1), ("two", 1)])


if __name__ == "__main__":
    unittest.main()
