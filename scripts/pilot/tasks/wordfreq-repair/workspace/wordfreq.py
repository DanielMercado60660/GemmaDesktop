"""Print the most common words in a text file.

Usage: python3 wordfreq.py FILE [N]
"""
import sys

from textkit import top_n


def main(argv):
    if len(argv) < 2:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    n = int(argv[2]) if len(argv) > 2 else 10
    with open(argv[1], encoding="utf-8") as fh:
        text = fh.read()
    for word, count in top_n(text, n):
        print(f"{count:>5}  {word}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
