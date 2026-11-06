"""Regression tests for the tokenizer punctuation and top_n tie-order bugs."""
import unittest

from textkit import tokenize, top_n


class TokenizePunctuationRegression(unittest.TestCase):
    def test_leading_punctuation_is_stripped(self):
        self.assertEqual(
            tokenize("'quoted' [bracketed] *starred* (well-known)"),
            ["quoted", "bracketed", "starred", "well-known"],
        )

    def test_punctuation_only_tokens_are_dropped(self):
        self.assertEqual(tokenize("wait -- what"), ["wait", "what"])


class TopNTieRegression(unittest.TestCase):
    def test_ties_sorted_alphabetically(self):
        self.assertEqual(top_n("zebra mango zebra mango", 2), [("mango", 2), ("zebra", 2)])

    def test_tie_at_cutoff_keeps_alphabetical_first(self):
        self.assertEqual(top_n("c b a c b a", 2), [("a", 2), ("b", 2)])


if __name__ == "__main__":
    unittest.main()
