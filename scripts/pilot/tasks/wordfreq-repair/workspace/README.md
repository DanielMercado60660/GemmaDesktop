# textkit

Tiny helpers for word-frequency counts in plain text.

```python
from textkit import tokenize, word_counts, top_n

tokenize('She said "hi".')      # ['she', 'said', 'hi']
top_n("b a b a c", 2)           # [('a', 2), ('b', 2)]
```

Command line:

    python3 wordfreq.py sample.txt 5

Run the tests (standard library only):

    python3 -m unittest discover -s tests -v
