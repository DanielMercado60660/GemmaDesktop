"""Turn raw text into normalised word tokens."""

# Punctuation that can end a word ("dog." -> "dog").
_TRAILING = ".,;:!?"


def tokenize(text):
    """Split ``text`` into a list of lowercase words.

    Words are separated by whitespace. Leading and trailing punctuation
    (any character in ``string.punctuation``) is removed from each word,
    while apostrophes and hyphens inside a word are kept: ``"Don't"``
    becomes ``"don't"`` and ``"(well-known)"`` becomes ``"well-known"``.
    Tokens made up only of punctuation, such as ``"--"``, are dropped.
    """
    tokens = []
    for raw in text.split():
        word = raw.lower().rstrip(_TRAILING)
        if word:
            tokens.append(word)
    return tokens
