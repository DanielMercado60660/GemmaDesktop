# Verification of committed source A

6 November 2026 UTC · local verification of committed source A complete

The clean local clone verified [source commit `58cf9346c62d`](https://github.com/DanielMercado60660/GemmaDesktop/commit/58cf9346c62d257aaff5fc86d0892aa5ca0fc9ff) on the [review branch](https://github.com/DanielMercado60660/GemmaDesktop/tree/codex/reliability-closeout-20261105). No ignored runtime data, private held-out fixtures, dependency caches or build products were copied into it. This records a local committed-source check, not a public GitHub clone replay.

| Check | Observed result |
| --- | --- |
| `./scripts/build.sh --ollama-only` | Passed in 62.5 seconds; production arm64 app, local ad-hoc signature and source/frontend input verification |
| Build source state | Clean before and after build |
| Managed runtime resources | Explicitly omitted; `runtimeResourcesIncluded:false` and `runtimeResourcesVerified:false` |
| Standard `test.sh` | Exit 1 at the Go stage after 61.53 seconds; only four known ffprobe-version qualification failures |
| Other tested Go packages | Twelve passed; desktop package has no tests |
| Explicit repository Python stage | 79 passed, 0 skipped |
| Explicit Harness adapters | 8 passed, 0 skipped |
| Explicit frontend tests | 76 Node unit tests and 75 Vitest tests across 12 files passed; zero skips |
| Frontend typecheck and production build | Passed; nonfatal large-chunk advisory retained |
| Post-test source/provenance/signature recheck | Passed; strict codesign/build-info verification exit 0; all 1,330 tracked files and identity fields unchanged, Git clean |

The standard script is fail-fast: Python and frontend stages did not execute in that invocation. The separate Python/adapter results above are explicit follow-ups. The host has ffprobe 9.0.2 while media qualification requires 8.1; the full Go suite is not green. The host is arm64, macOS 27.0.1 with 64 GiB memory.

Build binary SHA-256: `5f7a4cce53c5320ca5b93742796b7374005f43546b1d169368af578bd3a8ae41`. Source fingerprint: `d03b337716ce8c4a3559a21837ea286466438e543136ca366f1e2df8ac342604`. Frontend fingerprint: `83e9ba28b1e5e3b08f089fd545a55a6d35c518fa023bdd96c2420efacdd9d445`. The [JSON record](source-closeout-verification-2026-11-06.json) holds the selected identities and hash-bound local source reports.

This build differs from the model-study executable. Per-file comparison found the production Go default-temperature change in `internal/app/agent_controls.go` and the two frontend default-temperature files; engine, execution and upstream Harness code inputs were unchanged. This does not establish byte-identical binaries or equal whole-tree fingerprints.

All six validation-stage child handles were waited and reaped, owned groups were empty and no cleanup signals were needed. The verification operator launched no model or desktop app. The safe software report and post-test receipt are hash-bound in the JSON record. The initial and final build-info whole-file hashes are recorded separately; the source/frontend/binary/resource identity fields were unchanged.

The follow-up verification commit changes `docs/` only. README is frozen at `995cdab3d3306f40283116a19a43e394507c7d621f09012175f9bd117f5c7c5b` and stays unchanged, since it participates in the source fingerprint while `docs/` is excluded. Verification describes A's actual build; it does not claim B was separately rebuilt or remotely published.
