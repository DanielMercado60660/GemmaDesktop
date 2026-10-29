# Coding execution profiles

`restricted` retains the macOS single-process Seatbelt executor. `docker` uses
the locally installed Docker Desktop unix socket, a frozen immutable linux/arm64
image identity, and no host fallback. Ordinary runs never start Docker or pull an
image. `InstalledToolchains` discovers only local images bearing the Gemma Desktop
toolchain title label.

File tools continue editing the original project immediately. Each Docker command
captures current permitted source, then publishes allowed regular-file changes
before returning. The whole input manifest is rechecked before publication;
external changes cause a conflict. The per-root app operation lease uses filesystem
device/inode identity, covering symlink/case aliases. It is an in-process lease,
not a lock imposed on unrelated editors or other app instances.

Publication journals retain before/after bytes and hashes. File and containing
directory syncs precede commit. Publication is a recoverable sequence of per-file
operations, not an atomic whole-project transaction or a filesystem compare-and-
swap against arbitrary external writers. Restart reconciliation rolls back an
interrupted journal only where current content/mode still matches the journal;
external edits block recovery. Unfinished leases/journals block command and
dependency admission. Reconciliation removes only owner-labelled containers,
including labelled orphans without local leases. Never auto-replay failed code.

Only regular files are copied; symlinks, hardlinks, protected credentials, live Git
metadata and instruction metadata are excluded or rejected. Snapshot limits are
20,000 entries, 16 MiB per file and 256 MiB total. A publication permits 256 changed
files and 32 MiB combined before/after journal bytes. File bytes and POSIX modes
are preserved; ACL/xattr replication is not provided. Parent directories are
created as needed. Empty-directory-only changes/deletions are not published.
Binary regular-file changes use the same bounded journal and hash checks.

`readOnly:true` mounts source genuinely read-only. Temporary space and private
dependency/cache mounts remain writable. Private roots always include
`node_modules`, `.venv`, `__pycache__`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`
and `.cache`. Frozen profile extras are unioned with these defaults. Existing
source under an explicitly private root is not copied or published. There are no
implicit `build`/`dist` exclusions: configure those when they are private outputs.
Results/artifacts identify private command provenance; `read_file` still addresses
the original project. Historical Git inspection remains a governed host operation;
the command guest does not receive `.git`.

Each container has 4 CPUs, an 8 GiB memory limit (no additional swap allowance),
256 PIDs, no network attachment, no capabilities, no-new-privileges, read-only root,
non-root user and Docker's default seccomp profile. `/tmp` is an executable 512 MiB
tmpfs so compilers can run test binaries; it is nosuid/nodev. Per-file output is
limited to 64 MiB. Aggregate execution-state storage is monitored at 2 GiB/100,000
entries, with possible interval overshoot; this is not a hard filesystem quota.
Limits are qualification defaults, not measured optimal values for every project.
Ordinary project caches and journals are retained within this bound; general
automatic eviction is not implemented. Completed disposable harness fixtures may
explicitly release their current-image workspace cache through
`ReleaseWorkspaceCache`. This app lifecycle operation takes the canonical workspace
lease and executor guest slot, refuses pending execution recovery, and removes only
that exact cache key. Source, artifacts and command/publication journals remain.
The 100,000-entry work limit permits retry after partial removal. Directory nesting
deeper than 128 requires explicit maintenance; retry alone cannot make progress on
a single overdeep chain. Release never exposes a model cleanup tool; callers
retain cleanup failures as infrastructure evidence. Cold-per-trial behavior
is preserved. Execution cleanup failures require successful reconciliation.
Settings inspection reports a bounded, non-atomic measurement of logical file
bytes and entries in this execution state. It marks incomplete measurements and
their reason; its scan deadline is 750 ms. Docker image storage, VM allocation and
physical allocated-block usage are outside that measurement.

Cancellation removes the whole owned container, including detached process trees,
before permitting another command. Cancelled, timed-out, OOM, or uncertain results
do not publish source. Normally completed nonzero exits may have published permitted
changes; the exit code and publication state must both be inspected. Publication
already in progress finishes or rolls back before reporting cancellation.

## Explicit public dependency setup

`setup_dependencies` is exposed only for a Docker executor. Each setup asks for
approval of its lock identity, registries, package count and download bound. Host
fetches use origin-bound HTTPS with public-IP validation and response limits; no
guest receives network access or user package-manager credentials. Limits are
32 MiB per package response and 256 MiB per setup, five-minute total deadline.

* npm: package-lock v2/v3, at most 200 entries, public registry.npmjs.org tarballs
  with SHA-512 integrity. No workspace/link dependencies or private registries.
  Installation uses offline `npm ci --ignore-scripts --no-audit --no-fund` in
  private node_modules. Lifecycle scripts are never executed during setup.
  A separately approved ordinary `run_command` may run `npm rebuild --offline`
  when lifecycle execution is needed; it receives the same isolated, network-none
  environment and ordinary source-write policy. This is not a permanent ban on
  explicitly approved offline package scripts.
* pip: `requirements.lock` containing exact `name==version` and SHA-256 hashes,
  at most 100 requirements. PyPI metadata selects only matching wheels from
  files.pythonhosted.org. Offline pip installs with require-hashes, only-binary,
  no-index and no-deps into private .venv. List all transitive dependencies.
  Source distributions, markers, editable/direct-URL installs and source builds
  are unsupported. A lock without a compatible wheel fails explicitly.
* Go: public proxy.golang.org module files listed in go.sum, at most 100 module
  versions. No replace directives, VCS fallback or toolchain download. The
  installed Go tool downloads from the private file proxy offline and verifies
  go.sum/module-cache checksums. Missing checksums or unsupported cross-origin
  registry redirects fail closed; no source build runs during host fetching.

Private dependency/output environments are keyed by workspace, image, and captured
package/lockfile hashes. Changing a manifest/lock does not silently reuse the old
node_modules/.venv; provision the new environment before use. Download/build caches
remain workspace/image-scoped untrusted data. Approved commands can modify private
dependencies; installed content is not claimed immutable after setup.

Arbitrary allowed-origin upload prevention is not claimed. Registry configuration
files and host credentials are not copied. Filename exclusions do not detect every
possible secret embedded in arbitrary source files. MCP stdio retains its existing
restricted executor; persistent dev-server/MCP lifecycle is not supplied by this
finite command executor.

## Toolchain qualification

The Dockerfile pins official Python3.12, Node22 and Go1.26.10 image manifests and
TypeScript5.9.3's verified public tarball. It does no network package installation
during image build. Its build context must contain the integrity-verified
`typescript.tgz`; the exact checksum is recorded in the Dockerfile. Runtime image
identity is discovered from Docker inspect after building, not inferred from a tag
or Dockerfile digest. Qualification tests are opt-in via
`GEMMA_TEST_DOCKER_IMAGE=sha256:…`; public dependency tests additionally require
`GEMMA_TEST_DEPENDENCIES=1`. They do not perform model inference.

## Explicit toolchain image setup

From the repository root, with Docker Desktop already running, invoke:

```sh
python3 scripts/build-toolchain.py
```

This is a manual setup operation; the application never invokes it. It downloads
only `https://registry.npmjs.org/typescript/-/typescript-5.9.3.tgz` for the build
context, with a 16 MiB response bound, redirects disabled, no ambient HTTP proxy
or package-manager credentials, and exact SHA-512 verification against both the
helper pin and the checksum in `toolchain.Dockerfile`. Verification happens
before creating the isolated build context. That context contains only a copy of
the pinned Dockerfile and the verified archive, never the repository or home.

The printed Docker operation uses the local Docker Desktop socket, a temporary
Docker configuration with no credentials and only Docker Desktop's bundled
CLI-plugin path, `--platform=linux/arm64`,
`--network=none`, and `--pull`. Pulling the three official digest-pinned base
images requires network access; Dockerfile `RUN` instructions have no network.
The helper neither starts Docker nor configures Gemma Desktop. It inspects the
resulting immutable image ID and checks Linux/arm64 plus the toolchain label,
then prints JSON with that actual `sha256:…` identity. Select that installed
image in Execution settings. The convenience tag is
`gemma-desktop-toolchain:local`; runs freeze the image ID rather than relying on
the tag. Pinned inputs make setup repeatable, but bit-for-bit identical image
bytes across Docker/build versions are not claimed.

Offline helper checks, which perform no downloads or builds:

```sh
python3 -m unittest discover -s scripts -p 'test_build_toolchain.py' -v
```
