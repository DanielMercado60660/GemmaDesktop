# Managed inference runtimes

Gemma Desktop installs complete, verified runtime archives into its private app
data. End-user installation does not run pip, Homebrew, a compiler, or a package
resolver. Models have a separate directory, inventory, and removal operation.
Ollama remains the default and owns the existing video/embedding paths.

## Trusted inputs and local build

`inputs.json` pins exact upstream release bytes, source revisions, and model
revisions. The oMLX wheel corresponds to source
`4213c3284abc7b3746a91464a7af7ccc3732b759`; it is patched narrowly to preserve
reasoning during the current Gemma tool cycle. The patch fails on unexpected
source text, rewrites wheel RECORD hashes, and retains the upstream native
kernels. CPython is the private arm64 standalone 3.12.16 distribution. The native
mlx-serve input is release 26.11.1/source
`ac00f9213214db94b96f1837282c1ce4ad9debc8`.

Run from the repository root on supported Apple Silicon macOS:

```sh
python3 scripts/runtime-build.py download
python3 scripts/runtime-build.py omlx
python3 scripts/runtime-build.py mlx-serve
python3 scripts/runtime-model-catalog.py
python3 scripts/runtime-package.py
```

The ordinary oMLX build requires the matching private wheelhouse at
`.cache/runtime-build/wheelhouse`. Its complete wheel hashes are checked against
`omlx-requirements.lock`; installation is offline with `--no-deps` and
`--require-hashes`. A release builder deliberately creating a new dependency
set can use `runtime-build.py omlx --resolve --build-id NEW_ID`, review the
resulting lock, retain the wheelhouse, and run the offline build/probes. This is
a build-machine operation, never an app installation path. A clean checkout
without those private artifacts cannot reconstruct unpublished patched/VCS
wheels merely from a requirements list and must prepare a reviewed wheelhouse.
No release hosting or publication is implied.

Build IDs are immutable. Rebuilding identical content reuses the verified
archive; changed content needs a new `--build-id`. New archives normalize tar
metadata and gzip timestamps. A file lock prevents concurrent runtime builders
from overwriting their catalog. Run model catalog generation after the builders.
The model catalog generator fetches pinned metadata/small configuration files,
not model weights.

The generated `dist/runtime-resources` directory contains real archives and a
catalog. `runtime-package.py` verifies every archive member and copies the
verified inventory into `Gemma Desktop.app/Contents/Resources/runtimes`.
`build.sh` and `build-info.py` require these inputs and verify their packaged
hashes. Missing or corrupt runtime resources fail the full installer build.
For developer launches, `GEMMA_RUNTIME_BUNDLES` may point to an absolute trusted
resource directory. App installations read their signed app Resources instead.

A public clean checkout can instead build with `./scripts/build.sh --ollama-only`.
This explicit source-build mode packages the app and Harness resources without a
managed-runtime catalog or archives; it uses separately installed Ollama. Managed
runtime installation, managed model acquisition, and protocol qualification are
unavailable from that bundle. It does not reuse private app data or a wheelhouse.
Only the owned `Resources/runtimes` and `Resources/runtimes-previous` build outputs
are removed when switching modes; inputs and unrelated resources are preserved.
Interrupted `runtimes-stage-*` residue fails the build for explicit inspection.
The capture and final manifest record `buildMode: "ollama-only"`, empty runtime
inventories, and `runtimeResourcesIncluded: false` /
`runtimeResourcesVerified: false`. Source/frontend and packaged Harness integrity
checks still run. The default `./scripts/build.sh` and unqualified packaging or
provenance commands remain strict full-installer operations; there is no fallback.

## App lifecycle and qualification

The UI exposes installation, explicit activation, rollback, model acquisition,
model check, start/stop, cancellation, and separate runtime/model removal.
Catalog entries contain only reviewed sources and hashes. New versions arrive
through an updated app-shipped catalog; there is no arbitrary executable,
command-argument, model-repository, or remote-catalog input in these host APIs.
A hash verifies integrity against that trusted catalog; it does not establish
trust in an unreviewed catalog. Runtime removal does not remove model weights.

Downloads support verified resumption; extraction rejects links, traversal,
unexpected files, and size/digest mismatches. Jobs persist through app restarts,
with interrupted work requiring explicit retry. Installation is staged and
renamed atomically. Activation and rollback update metadata only after checking
installation receipts. Disk preflight retains a default 10-GiB reserve.

Each server uses an app-owned process group, random loopback port, fresh bearer
key, private state/cache/log paths, and an allowlisted environment. Authentication
is checked with both valid and invalid keys. The oMLX model context and tool
truncation policy are configured explicitly. The native mlx-serve wrapper disables
plugins, speculation, and disk prefix caching, assigns private log/ANE cache
paths, and uses a pinned macOS sandbox profile denying `~/.mlx-serve` access.
HOME is preserved. This is vendor-state isolation, not a general untrusted-code
sandbox. mlx-serve remains unavailable for ordinary inference until a real model
passes that exact launch/protocol path; no weakening to global configuration is
allowed if it fails.

An advertised capability does not authorize ordinary model use. **Check model**
runs a bounded synthetic protocol probe without host tools, then stops its
server. A protocol-version-2 receipt binds the exact runtime/model manifests,
artifact, template, and effective context. The probe checks thinking-on tool
calls, original call IDs and result continuation, thinking-off, token receipts,
and cancellation. Cancellation reaps the owned server before shared inference
admission is released. Failed cleanup blocks all providers for the session.
Worker groups are also cleaned up after leader exit. Older receipts require a
new check. Explicit frozen runs can describe and start an installed qualified
build without changing the active/previous default selections. Candidate checks
also work before activation; ordinary manual Start still requires activation. Protocol qualification still labels coding quality experimental;
vision and structured-output support remain unverified.

## Bounded probe evidence

`scripts/runtime-probe.go` exercises the app's actual installer, downloader, and
qualification manager. It writes the exact identities, schedule, cursor, original
two-hour deadline, terminal outcome, and cleanup state under the chosen private
data directory. A previous checkpoint prevents an automatic next batch. Future
checks also capture Go probe-source hashes and per-generation token/timing
observations; unknown measurements stay absent.

Both relocatable runtime archives were built on 4 November 2026. The first real
check installed oMLX and acquired E4B weights; only the patched oMLX/E4B pair was run. At
4,096 context tokens it passed protocol version 2, including active cancellation
and owned-server teardown. The sanitized record is
[`docs/evidence/managed-mlx-e4b-2026-11-04.json`](../../docs/evidence/managed-mlx-e4b-2026-11-04.json).
It is a compatibility smoke check, not a coding campaign, context-capacity test,
or speed comparison. The receipt includes exact runtime/model/template identity;
the first helper did not capture its Go source inventory or individual request
timings. The evidence explicitly records those limits and separates the provider's
three-request aggregate from unavailable measurements. No additional inference
batch followed that checkpoint.

The E4B model is approximately 5.18 GB; 12B and 26B pinned catalogs are available
but their weights were not acquired by this first check. mlx-serve, 12B, 26B,
larger contexts, vision, and coding-quality gates remain unqualified.

## Distribution boundary

The local resources are usable without a new artifact hosting service. The
native mlx-serve vendor signature was verified during bundle construction; local
relocation/dependency/template checks ran on the packaged oMLX environment.
Those checks are separate from Developer ID signing, hardened-runtime policy,
notarization, and Gatekeeper distribution validation. No public notarized release
was produced here. Preserve bundled Python, upstream, and dependency license
notices when preparing a distributable app, and verify the final signed app and
extracted executable trust on a clean machine before claiming release readiness.
