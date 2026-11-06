# Public JSON Pointer development coverage

The explicitly selected `coding-diagnostics-v2-public-coverage` pack adds coverage
for three publicly demonstrated defects: signed array indices, missing initial
slashes and corruption of ordinary Unicode key characters. It is development
material from an exposed family, not unseen qualification data.

Version `2.2.0` retains grading policy `task-contract-v3`. The original `2.1.0`
pack remains intact. The sibling contains the same JSON Pointer task ID, public
files, prompt, authority and inherited cases, plus eight public cases. Its
reference is a documented sibling-only fork correcting ordinary Unicode-token
byte preservation. The original reference stays byte-exact in the original pack.
No duration-parser rule or other task is changed.

The added positive cases distinguish canonical array indices from object keys
such as `+1`, and retain normal slash-prefixed, Unicode, empty-root and empty-key
lookups. These are existing public-contract behaviors. RFC 6901 defines Unicode
reference tokens and distinguishes array indices from exact object member
names; its examples also distinguish the empty pointer from `/`.
[Syntax](https://www.rfc-editor.org/rfc/rfc6901.html#section-3),
[evaluation](https://www.rfc-editor.org/rfc/rfc6901.html#section-4),
[JSON string examples](https://www.rfc-editor.org/rfc/rfc6901.html#section-5).

The loader derives a distinct grading hash and pack digest from the new cases
and pack identity. Original public bytes and exposure family stay the same and
remain in `development`. Pack-scoped task lookup permits the shared task ID.
No campaign, Workbench selection, qualification threshold or promotion route
automatically changes to this sibling. Select its pack ID explicitly.

After source application, qualify its trusted evaluator controls in an idle
window. The corrected sibling reference must pass all inherited and added cases. Three
targeted faulty variants must retain inherited behavior while failing their
corresponding new cases. Always-null and blanket-plus-rejection variants must
fail the adjacent positive guards. A separate loader test verifies preservation
of the old public contract and raw inherited cases, pins all original catalog
source identities, and confines the reference fork to the sibling.

The opt-in trusted host control command is:

```sh
source scripts/env.sh
python3 -B scripts/harness/check_public_pointer_coverage.py --host \
  --artifacts-dir .runtime/public-pointer-controls-20261105
```

Use a new artifact directory for each attempt. Raw command payloads, outputs and
source copies remain under its private directory; stdout and the safe summary
contain counts only. Ownership-artifact failures are covered by a synthetic
cleanup regression; every post-launch operation is inside the cleanup guard. Host controls do not establish app grading integration,
Docker containment or model quality. Run the focused loader/grader integration
checks separately. Historical grades, outputs and v4 model results stay frozen;
any supplementary replay under the sibling requires its own identity and label.

Existing source/reference attribution and repository licensing are retained.
The new tests and control variants were derived from public behavior, not copied
from RFC implementation code.
