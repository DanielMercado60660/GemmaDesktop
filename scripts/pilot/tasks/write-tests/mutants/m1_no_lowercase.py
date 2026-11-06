"""Small text helpers shared by the blog tools."""
import re
import unicodedata

_NON_ALNUM = re.compile(r"[^A-Za-z0-9]+")


def slugify(text, sep="-"):
    """Turn a title into a URL-friendly slug.

    The text is lowercased, accents are stripped, every run of characters
    that are not ASCII letters or digits becomes a single ``sep``, and
    separators are trimmed from both ends. Text with nothing usable in it
    gives an empty string.

        >>> slugify("Hello, World!")
        'hello-world'
    """
    if not text:
        return ""
    # NFKD splits an accented letter into the base letter plus a combining
    # mark; encoding to ASCII with "ignore" then drops the mark.
    decomposed = unicodedata.normalize("NFKD", text)
    ascii_text = decomposed.encode("ascii", "ignore").decode("ascii")
    slug = _NON_ALNUM.sub(lambda _match: sep, ascii_text)
    return slug.strip(sep)
