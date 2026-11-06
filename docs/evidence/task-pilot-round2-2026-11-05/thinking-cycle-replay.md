# Production thinking-cycle v1 frozen corpus replay

The unchanged input contains 134 ordinary thinking generations from two failed development traces and seven successful baseline/v2 traces. Production v1 exactly reproduces all 5 expected prototype triggers, including block width, repetition count, repeated normalized bytes and observed prefix bytes. No extra triggers or missed expected triggers.

Frozen thresholds remain 2–12 completed nonempty lines repeated eight times, at least 2,048 repeated normalized bytes, and a maximum 96-line comparison window. Visible output, tool arguments, summaries and budget reports are excluded by adapter tests.

Production v1 corrects the exploratory prototype’s unsafe fence toggling: only delimiter lines open fenced code, and only a standalone same-character delimiter at least as long closes it. Embedded, opposite-type and shorter markers inside code cannot close it. An oversized line containing ambiguous fence markers suppresses this optional detector for the rest of that provider attempt; inference still continues. This is a parser safety correction, not threshold tuning; the production guard remains version thinking_cycle_v1.

The interruption event separates the trigger prefix (`guard.bytesSeen`) from all thinking already received in the trigger chunk (`thinkingBytesObserved`, `thinkingCharactersObserved`). `elapsedMs` is recorded after Generate returns and includes backend cleanup; it is not exact trigger time or proven saved latency.

| Generation | Pattern lines | Repetitions | Repeated bytes | Trigger prefix bytes |
| --- | ---: | ---: | ---: | ---: |
| baseline-failures/wordfreq-repair/9/1 | 4 | 8 | 2384 | 2645 |
| baseline-failures/wordfreq-repair/10/2 | 9 | 8 | 5616 | 6706 |
| successful/candidate-v2/log-question/5/1 | 3 | 8 | 2376 | 8145 |
| successful/candidate-v2/todo-feature/23/1 | 5 | 8 | 2520 | 3179 |
| successful/candidate-v2/wordfreq-repair/13/1 | 6 | 8 | 2560 | 7607 |

Input SHA-256: `0d6af647b26b15badd32d6dda10b552dc2bcbd200222bed294d4565affca8fd3`.
Exploratory detector SHA-256: `4ac50455f839733d233fd52df0ed300904c332e234d51aa7d303ed8f9d5c828b`.

Limitations: this is a small exposed development corpus, with no qualified false-positive rate or general reliability estimate. Completed successful tasks include interrupted-by-length generations: a trace’s eventual success does not establish that every internal generation was productive. Replay cannot establish live recovery quality, latency, provider shutdown, or task success. Long nonrepeating enumeration remains undetected. No held-out content or new inference was used.

The unchanged replay and synthetic/native mock safety tests support the planned false-by-default, bounded live experiment. They do not support default activation or general reliability/performance claims.
