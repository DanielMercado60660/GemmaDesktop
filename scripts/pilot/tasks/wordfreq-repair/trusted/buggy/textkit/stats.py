"""Word-frequency statistics built on :func:`textkit.tokens.tokenize`."""

from collections import Counter

from .tokens import tokenize


def word_counts(text):
    """Return a Counter mapping each word in ``text`` to its number of uses."""
    return Counter(tokenize(text))


def top_n(text, n=10):
    """Return the ``n`` most frequent words in ``text`` as (word, count) pairs.

    Pairs are ordered by count, highest first. Words with the same count are
    ordered alphabetically, so the result does not depend on the order in
    which words appear in the text. If there are fewer than ``n`` distinct
    words, all of them are returned.
    """
    counts = word_counts(text)
    ranked = sorted(counts.items(), key=lambda item: item[1], reverse=True)
    return ranked[:n]
