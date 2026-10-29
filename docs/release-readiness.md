# GitHub release preparation

29 October 2026 · experimental source release · MIT licensed

The release presents Gemma Desktop as a local Gemma/ADK desktop project for
Apple Silicon. Feature development and the recorded Harness² campaign are
complete for this release. Notarized binary distribution is deferred.

## Public entry points

- The root [README](../README.md) introduces the app, requirements, first task,
  architecture, and limitations.
- The [first-task example](../examples/first-task) is intentionally broken and is
  intended to be copied to a disposable workspace.
- The former detailed README is retained as the [workbench guide](workbench.md).
- The [evidence index](evidence/README.md) points to qualification summaries.
  Historical raw reports remain unchanged; failures and rejected promotion remain
  part of the record. Generated evidence and vendored code are identified in
  `.gitattributes`.
- [LICENSE](../LICENSE) applies MIT to original work; [NOTICE](../NOTICE.md)
  preserves third-party attribution and the separate status of model weights.

## Fresh verification

A source snapshot containing the non-ignored project files was copied into a new
Git repository outside the development checkout. It began clean, with no
`node_modules`, frontend build output, Go dependency cache, or app bundle. The
snapshot's temporary Git commit is a verification identity, not a release tag.

| Check | Result |
| --- | --- |
| `./scripts/build.sh` from that clean snapshot | Passed; Go toolchain, modules, and npm packages downloaded afresh |
| Native arm64 compilation and local ad-hoc signature verification | Passed |
| Build input and packaged Harness resource identity | Passed; source/assets matched before and after compilation |
| `GEMMA_TEST_SANDBOX=1 ./scripts/test.sh` | Passed across all Go packages, including race checks and real macOS restricted-process tests; 33 script tests and 65 frontend tests passed |
| `python3 -m unittest discover -s scripts/harness -p 'test_*.py'` | Four tests passed |
| Frontend type check and production bundle | Passed; existing large-editor-chunk advisory remains |

The host was Apple Silicon with 64 GiB RAM, macOS 27.0.1, and Ollama 0.40.11.
Tests requiring Docker, Chrome, terminal access, or an explicitly selected live
model remain opt-in; the general suite's pass does not claim those paths were all
rerun. This check does not establish minimum hardware requirements or compatibility
with every macOS version at or above the deployment target.

Logs and exact build inputs: [build](evidence/release-prep-20261029/build.txt),
[tests](evidence/release-prep-20261029/tests.txt),
[adapter checks](evidence/release-prep-20261029/adapter-tests.txt),
[build identity](evidence/release-prep-20261029/build-info.json).

## Publication review

A targeted text scan covered the eligible working tree and Git history for common
provider tokens, private-key headers, credential-bearing URLs, and literal secret
assignments. Matches were reviewed as synthetic OAuth/endpoint-policy test values.
No real credential was identified by this scan. Eligible filenames were also
checked for credential files and app databases; none were found. This was a
bounded pattern review, not a security certification.

Historical evidence intentionally retains local paths from the author's test
machine and synthetic task transcripts. Local caches, model weights, app data,
build products, credentials, and machine-specific agent settings are excluded
from the publication tree. New ignore rules cover `.env` files and common private
key/certificate formats. The source contains the synthetic credential fixtures
needed to test rejection and OAuth handling.

No upstream dependency versions or application behavior were changed during this
preparation. The preserved Harness² campaign remains rejected; later passing
checks do not replace its grades or justify candidate activation.

## First-run setup finding

The initial live arithmetic run used Python.org's framework interpreter. Gemma
made the correct source edit, but the restricted executor blocked the interpreter's
shared library under `/Library/Frameworks`, and repeated tool attempts exhausted
the step budget. Independent tests passed outside the executor; the agent run
itself failed and is retained as a failure. See the [full record](evidence/release-prep-20261029/demo-python-framework-failed.json).

The README now calls out Homebrew Python for the restricted-executor demo, and
the workbench's live-test command explicitly selects it. The executor's permitted
paths and approval policy were not expanded.

A new isolated attempt with Homebrew Python 3.14.4 completed in **39.61 seconds**
using `gemma4:12b-mlx`. The agent observed the three failing tests, changed only
`calculator.py`, and ran a successful test command after the edit. An independent
rerun also passed all three tests; the original test file's bytes were unchanged.
This is one bounded demo, not a reliability benchmark. See the
[successful run](evidence/release-prep-20261029/demo-homebrew-passed.json).

The headless test process was stopped and the newly built native app opened with
the same isolated data directory. The saved conversation, source edit, tool
receipt, completed status, and runtime metrics were visible after restart. The
[README screenshot](images/agent-task.jpg) was captured from that native window.
Personal app settings were not used or modified.

The published build path requires a Git clone because its provenance helper reads
Git metadata. A local source bundle can be cloned with
`git clone /path/to/GemmaDesktop-source.bundle GemmaDesktop`; GitHub's source ZIP
is not a supported build input.
