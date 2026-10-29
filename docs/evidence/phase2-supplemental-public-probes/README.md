# Supplemental public-contract probes — prepared, unexecuted

Execution is **not authorized yet**. Wait for root’s explicit confirmation that the active 72-trial campaign has finished and this serialized probe may run. No Docker or candidate execution occurred during preparation.

`recipe.v3.json` freezes seven exact terminal trial identities, public source hashes, unchanged prepared runtime subsets, CLI argv, literal inputs, public-contract expectations, static predictions, resource limits, cleanup rules and result meanings. The JSON input/expected files are directly inspectable. Prepared source copies are read-only; a future run must make fresh disposable copies and must never mount an original candidate workspace.

- Go usage candidate 1 and candidate 2: ordinary addition control, then two qualifying 2^62 unit values. The expected positive sum is 2^63. Both inputs and the sum are exactly representable binary64; the suspected failure is int64 accumulation overflow.
- Node config baseline 0 and candidate 0 and candidate 1: ordinary object control, then an own `__proto__` key originating from JSON.parse. The expected serialized output retains that key. This is not a global prototype-pollution claim.

The frozen campaign image is pinned in the recipe. Run one offline read-only-source container at a time with 4 CPUs, 8 GiB memory, 256 PIDs and 512 MiB private tmpfs, 120 seconds per command. No downloads, package setup, host code imports, writable original binds or shared caches. Require bounded output capture and owner-checked removal; preserve cleanup uncertainty rather than deleting active mounts.

Record future observations in a separate result file with before/after hashes. These observations supplement source review and never rewrite frozen grades, candidate content or promotion evidence. A control failure or infrastructure fault makes the probe inconclusive.

Version 1 (`recipe.json`) remains byte-for-byte unchanged, SHA256 `6772158494267b6d48d0d37081b3231e5d3e2f5846e40c9fb0fe01e87c3b265e`. Version 2 adds terminal Node candidate 1 (`97b1e933d011bc05ce7ea38919e09f23`) with its own reviewed hashes and fresh source copies. Its historical final read-only receipt is recorded separately from the still-unexecuted supplemental probe. No execution from another trial is treated as evidence for this attempt.

Version 3 preserves versions 1 and 2 and adds Node usage candidate 1 (`2b3d280e7299c221a83542e524c5ee7d`) plus representative baseline 1 (`27a20d6b163b76460ec594ae04021c90`). Each has a fresh runtime subset and an ordinary addition control followed by valid `__proto__` and `constructor` user-name cases. Expected numeric totals are public-contract-derived. The candidate’s valid historical final read-only receipt remains separate from these unexecuted observations. All seven containers remain gated on the explicit campaign-finished execution signal.
