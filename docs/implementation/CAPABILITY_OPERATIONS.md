# Capability and coding operations

These controls apply to future runs. A running or resumed task keeps its recorded
permissions, resource versions, remaining budgets and absolute deadline.

## Configure a coding agent

1. In Settings, choose an installed, digest-pinned coding toolchain. Docker must
   already be running. The app neither starts Docker nor pulls images implicitly.
   The explicit setup helper is `python3 scripts/build-toolchain.py`.
2. In Agents, create a capability profile. Select the Docker executor for Linux
   builds, workers, multiprocessing, SQLite and temporary server/client tests.
3. Select the required tools. File tools edit the chosen project immediately;
   `run_command` uses a private copy and publishes permitted changes before returning.
   Enable `setup_dependencies` only when approved public dependency setup is needed.
4. Select immutable skill versions, exact MCP tools and read-only specialists.
   Instructions never add authority. To delegate, both `delegate_readonly` and the
   selected specialist must be permitted.
5. Assign the profile to the intended agent. Model, thinking, temperature, output
   and step controls remain explicit agent settings. Ordinary personal defaults
   are separate from experiment settings.

Profile revisions are immutable. Editing a profile creates a new revision; importing
a new skill does not silently add it to existing agents. Missing or archived
selections remain visible as removable entries in the editor. Previously frozen
runs retain their exact resource versions.

## Files, dependencies and verification

Set private output directories such as `dist` or `coverage` in the profile when
they should remain command-cache data. Dependency and standard tool caches are
private automatically; arbitrary `build`/`dist` source is not silently excluded.

Commands use Linux command names from the image PATH. Pass arguments as separate
array items. Network is denied. Supported public dependency acquisition is a
separate visible approval; locked packages are fetched with integrity checks and
installed offline. See the [executor contract](../../internal/execution/README.md)
for exact npm, pip and Go support and bounds.

Use `readOnly:true` for final verification. Source is then genuinely read-only,
while private temporary/dependency/cache locations remain writable. Run the
prescribed checks again after the last source or test change. A successful command
observes one process outcome; it does not independently prove correctness.

Inspect both exit status and publication status. A normal nonzero exit may still
publish valid source edits. Cancelled, timed-out and uncertain work does not publish
new changes. Conflicts preserve external edits. If Settings reports recovery is
required, use its recovery action after active work ends; do not rerun uncertain
actions as a substitute for reconciliation.

Storage reports count logical execution-state file sizes. They exclude Docker
images and VM allocation. The retained-state monitor is a soft limit with possible
interval overshoot, and partial scans are labeled. There is no automatic pruning.

## Skill bundles and provenance

Import a bounded skill directory containing `SKILL.md` and its resources. The app
records exact hashes and exposes binary files as artifacts. `skill_resources`
lists approved versions; `skill_materialize` copies an exact selected resource to
an approved workspace target using revision checks. Materialization never executes
a script. A later command requires the normal executor and approval boundary.

The Context inspector distinguishes trusted host configuration, user input and
untrusted project/skill/tool content. It records bounded guidance discovery,
omissions, hashes, token estimates and observed runtime usage. Estimates and
cumulative model input measurements are not interchangeable.

The Plan inspector retains item history and links to execution receipts. An item
marked complete is a progress claim. Independent Harness grading is a separate
record. Specialist child runs retain identities, cancellation and shared budgets;
they cannot write, run commands, invoke MCP/browser tools or recursively delegate.

## MCP HTTP sign-in

Configure the HTTP resource plus its issuer, authorization endpoint, token endpoint,
public-client ID and scopes. Use Sign in to open the native browser. The callback
validates PKCE, state and issuer; token refresh and restart persistence use Keychain.
SDK discovery is preferred. The explicitly configured endpoint path is used only
when supported discovery metadata is absent, not when it is malformed or mismatched.

Credentials are excluded from model context and guests. A failed action with an
uncertain outcome is not blindly replayed after reauthentication. Sign out clears
the local credential; provider-side token revocation is not implemented. Dynamic
client registration and external-provider interoperability remain unqualified.

Stdio MCP servers retain the restricted executor and do not inherit Docker's
subprocess or workspace permissions.

## Experiments

Use [the Harness campaign guide](HARNESS_CAMPAIGN.md) for native/upstream optimization,
Codex packets, qualification, exports and rollback. The fixed corpus contains
three larger multi-file development projects and fifteen smaller contract tasks.
Task outcomes, infrastructure qualification and published research benchmark
results are distinct claims.
