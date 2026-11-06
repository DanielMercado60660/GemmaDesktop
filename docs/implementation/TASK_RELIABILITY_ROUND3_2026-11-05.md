# Calculation correctness, final delivery and Harness² campaign

## Final closeout status — 6 November 2026 UTC

The requested twenty full refinement rounds, paired development diagnostics,
temperature comparison, exposed A1/A3/A2 controls and **one** fresh fifteen-task
confirmation pass are complete. No further model phases belong to this closeout.
The fresh pass produced eight independently clear successes, one uncredited
answer-representation ambiguity and six genuine failures: nine automatic passes,
twelve final deliveries and fifteen preserved protected originals. Everyday
results were 3/7 clear, stretch 1/3 and supplied-test repairs 4/5. Both reliability
gates remain unmet, including the requirement for all five bounded final
deliveries. One task created an extra helper inside its admitted workspace but
outside its allowed-file list; zero platform authority-grant violations or
outside-workspace writes were observed in static receipts/scope review.

The reviewed temperature-default and opt-in public-coverage source changes are
applied and their focused checks passed. Explicit stored temperatures remain
preserved; no database/profile/history migration or historical result rewrite
occurred. The private 143-file fixture tree is ignored and locally preserved.
Source snapshot [A, `58cf9346c62d`](https://github.com/DanielMercado60660/GemmaDesktop/commit/58cf9346c62d257aaff5fc86d0892aa5ca0fc9ff), on the [review branch](https://github.com/DanielMercado60660/GemmaDesktop/tree/codex/reliability-closeout-20261105), built successfully from a clean local clone using `--ollama-only`; managed runtime resources were explicitly omitted. The standard Go stage retained only four known ffprobe-version qualification failures, while twelve other tested Go packages passed; separate Python 79 and adapter 8 checks passed. Frontend follow-ups passed 76 Node unit tests and 75 Vitest tests across 12 files, typecheck and production build, with zero frontend skips. Post-test source/frontend/binary/resource identity fields and all 1,330 tracked files remained unchanged, README stayed fixed and strict signature/build-info verification passed with Git clean.

The dated [measured-results record](RELIABILITY_PUBLICATION_2026-11-05.md),
[selected aggregate](../evidence/reliability-round3-summary-2026-11-05.json) and
[committed-source verification](../evidence/source-closeout-verification-2026-11-06.md)
carry final model/source identities and limits. This document update changes
`docs/` only; README and executable inputs stay fixed. The model-study binary
is not the later clean build of A.

Repeated qualification, further harness optimization and matched 12B/E4B
comparisons—including normal E4B 32K qualification first—are **follow-up only**.
The proposed 45-attempt repeated schedule never started and remains archived.
Historical 12B pilots and the older 4K E4B protocol smoke retain their original scope;
no matched smaller-model comparison or 32K E4B qualification is claimed here.
Local source A verification is complete, with the four known media failures
retained. This docs-only follow-up records those actual receipts. The review
branch URL identifies the intended publication location; this record does not
assert remote push or public CI. No further research, optimization or model
reruns are required by this plan.

## Historical explore → plan → build record

The sections below preserve the original decisions and recovery chronology.
Forward schedules in this record are historical and are superseded by the final
closeout status above. The original source-A version remains available in
[Git history](https://github.com/DanielMercado60660/GemmaDesktop/blob/58cf9346c62d257aaff5fc86d0892aa5ca0fc9ff/docs/implementation/TASK_RELIABILITY_ROUND3_2026-11-05.md).

## Explore

The previous round's failed log script selected the wrong quote boundary and
reported no matching errors. Exit zero did not establish a correct calculation.
The missing generic steps were sample parsing, coverage/invariants and checking
surprising empty results. Successful output was sometimes followed by lengthy
reasoning rather than the requested final answer.

The pilot's 600-second limit was external; ordinary app admission used twenty
minutes. Existing status reporting only handles model-call exhaustion. A
boundary-only deadline reserve can produce an honest unfinished status, but
cannot guarantee closure during a long in-flight operation.

Current pinned Harness² k=1 performs baseline, INV, DOM and composed probes,
then independent reflection. User confirmed **20 full refinement rounds**:
80 solver attempts, with three improvement stages per round. Upstream solver
completion is separate from independent semantic correctness.

## Plan and build

1. Add generic calculation guidance: validate parsed samples and field types,
   expose scanned/parsed/skipped/selected coverage, check invariants and
   investigate surprising zero results without assuming they must be nonzero.
   Map requested answers to observations and finish concisely once supported.
2. Allow validated caller budgetSeconds to narrow admission. Freeze the hard
   deadline and pass the pilot limit into run.start. Add opt-in finishing
   reserve, shared single outer reporting allowance, bounded status-only cap,
   original history/authority and explicit incomplete/failure status. Do not
   rescue cancelled contexts or change ordinary response caps.
3. Add development-only baseRevisionId admission preserving complete Content,
   target/authority checks, hashes and provenance. Add bounded grading of
   immutable existing development trials using the frozen grader. Expose only
   opaque pass/check-count results, never hidden assertions or references.
4. Run meaningful race/integration, reporting and frontend checks as affected;
   rebuild and freeze a single production binary before the campaign.

## Campaign protocol

- Initially 20 full outer rounds on local Gemma 26B, actual pinned upstream core,
  k=1, four fresh solver trials each. No model-authored proposals: Codex authors
  declarative harness content and a distinct Codex reviewer handles reflection.
- Use only development coding tasks from coding-diagnostics-v2. Select and
  freeze their cyclic order before inference. Historical read_only contracts
  prohibit calculation commands and retain that authority.
- Fixed requested 32K, 40 steps, Auto thinking, temperature 0.2, 8192 output tokens,
  600-second trial ceiling, pinned Docker image and model/runtime identity.
  At the ceiling,80 trials permit13h20 solver time plus grading/review; this is
  a bound, not a forecast. Execution stays serial and checkpointed.
- Each round starts from the last independently retained development revision;
  failed candidates and reflection rejections remain in the ledger. Preserve
  all80 attempts, independent grades, delivery status, usage completeness,
  revision/packet hashes, decisions and lessons. No fabricated empty iterations.
- Grade development outputs without editing solver files or checkers. Authors
  may read public requirements and observed public tool results, not hidden
  graders, probes, references or held-out task contents. Numeric expected
  answers do not enter harness guidance.
- Freeze the final candidate and run independent development diagnostics
  against the initial harness. No default promotion from development results;
  new blind qualification material is required for broader quality claims.
- Preserve existing dirty changes and historical evidence. No credential
  changes or cloud model inference.

The user's subsequent overnight authorization prioritizes 26B, permits fresh
evaluation creation, continued work toward broad task performance and GitHub
publication when justified. After the first 20 rounds, run a fresh independently
authored ten-task broad suite with the selected frozen harness. Target at least
9/10 completed and independently correct overall, all numeric and coding cases
completed and correct, coverage in every category,
and zero authority/protected-input failures. Report completion separately from
semantic grades. Compare 12B and E4B separately when compatible runtimes exist.
Small samples establish observed behavior, not a general reliability rate.

If the broad result misses the gate, preserve it as exposed evidence and
investigate a concrete cause. The user's subsequent tiered-utility steering
prioritizes practical qualification and controlled sampling comparisons after
the requested twenty rounds. Additional harness rounds are not the default
response to a low stress-test score. Fresh blind confirmation uses an untouched
pool after any later candidate freeze. Do not repeatedly tune against a suite
while calling it unseen. Publish reviewed code, opt-in content and honest,
clearly scoped evidence after the applicable acceptance and clean-checkout
checks; do not silently activate an unqualified product profile.

## Historical pre-campaign status and recovery chronology

At that pre-campaign checkpoint, exploration, build and integrated checks had
completed and the frozen 26B campaign was dispatched. It later completed as
recorded above. Baseline and candidate were retained under
`.runtime/reliability-round3-20261105/`.

- Candidate binary SHA-256:
  `44356ed3562358207a5d93babbc4a167cbff0915c463777267cf772573b4ffab`
  (frozen v4).
- Source fingerprint:
  `155f8b5b2c5adaed02267e363748de72a850a70dc26e1e57a20acf21873bff76`.
- Native production build, ad-hoc signature and input provenance checks passed.
- Final repository Python suite68 tests and Harness adapter suite8 tests passed.
- Full Go race run passed every package except the same four media tests
  requiring FFprobe8.1 while installed version remains9.0.2. These are retained
  baseline failures, not reported as a green full suite.
- Independent source reviews cleared deadline behavior, grading redaction,
  immutable identity and concurrency after fixes.

Broad qualification requires actual final delivery as well as semantic success
on all six critical coding/numeric tasks. A status-only report or partly correct
workspace cannot count as a solved critical task.

The first campaign preflight rejected a noncanonical expected model-digest
string before any solver attempt. The operator constant and regression test
were corrected, build provenance refreshed and a fresh v2 campaign dispatched.
Compiled application bytes are identical; the operator source fingerprint
changed. The zero-trial preflight remains evidence and is not counted as a
refinement round.

The v2 run exposed an owned-adapter resume defect after five actual solver
trials: content-only fingerprint lookup aliased identical baseline/composed
revisions and dispatched an extra baseline. The run was cancelled cleanly;
all five trials and grades remain infrastructure-invalid evidence outside
the 20 valid-round target. A task/stage binding regression and pre-inference
validation are required before the fresh v3 campaign.

During this stopped interval, an independently reviewed explicit
`./scripts/build.sh --ollama-only` source-build option was applied. It omits
managed bundles deliberately and labels omission in provenance; the default
full-installer still requires verified managed artifacts. Scratch compilation,
mock app delivery and mode/identity regressions passed. This addresses the
public clean-checkout dependency on ignored private runtime bundles. The
actual final-tree build will be verified before the v3 freeze.

The repair passed the real pinned-core checkpoint/restart test with identical
content across all branches: exactly three probes before checkpoint and four
total stage identities after owner restart. Host-side pre-inference stage
checks and cached input/output identity checks reject aliases or tampering.
Full Harness/app race suites and independent review cleared the change. The
full-installer v3 build then passed signing and provenance checks and was
dispatched to a fresh campaign directory. Earlier version artifacts remain
separate; valid-round accounting restarts from zero.

The v3 run was stopped after three actual probes when an independent audit
confirmed a pre-existing grading false negative: an executed regression test
that exposes a runtime exception in the original implementation can be valid
red evidence. The old classifier only accepted assertion failures. A narrowly
attributed classifier repair is under review with synthetic positive and
negative controls. Task requirements, cases, reference implementations and
expected values remain unchanged, and the earlier grades remain immutable.
The eight actual solver trials across v2 and v3 are retained as cost and
infrastructure evidence outside the target of twenty valid full rounds.
The fresh v4 campaign will start only after review, integrated checks and a
new production freeze.

Those checks passed and v4 was dispatched on 5 November. The production
build, signature and resource provenance checks passed. Full app race tests
passed in 40.878 seconds; non-app race suites retained only the same four
media qualification failures. Repository Python tests (68), adapter tests (8),
frontend unit tests (76), frontend UI tests (73) and the frontend build passed.
The source-bound grading run passed 118 subtests, covering all 18 existing
reference fixtures, two existing negative controls, seven synthetic Python
full-grade controls, ten synthetic Node regression-gate controls and focused
parser cases. The Node controls establish regression-gate behavior, rather
than the entire semantic grader. Independent review cleared the exact frozen
source hashes and task/reference integrity. The v4 campaign uses manifest
version 2.1.0 and grading policy `task-contract-v3` throughout.

## Historical tiering decision and subsequent amendments

Before any broad-suite exposure, the user asked to separate everyday usefulness,
bounded repairs and stretch coding. The completed campaign comprises twenty full
rounds, eighty unique solver probes and twenty independent reviews, including one
rejected external terminal review after an upstream failure. Seventy-six probes
delivered; four failed on model budgets. Thirty-four passed frozen automatic
checks, including four separately documented public-contract contradictions.
These counts are changing development-harness draws, not a general success rate.
The retained full revision was frozen before ten paired diagnostics.

Google reports the 26B A4B model as MoE with 3.8B active parameters, and a slightly
higher three-domain tool-use average for 12B than 26B. Its published sampling
guidance specifies temperature 1.0, top-p 0.95 and top-k 64. Publisher benchmarks
and sampling recommendations do not predict this application's task outcomes.
[Official model card](https://huggingface.co/google/gemma-4-26B-A4B-it#benchmark-results),
[sampling guidance](https://huggingface.co/google/gemma-4-26B-A4B-it#1-sampling-parameters).

The original ten-task broad suite and its full-suite gate remain unchanged.
Report its seven noncoding tasks as everyday work and its three independently
designed regression-repair tasks as stretch work. Add five separately authored
bounded standard-library Python repairs with existing tests and one small
production edit each. Original tests and requirements remain protected; no new
test-design obligation is added to that tier.

Pre-exposure scoped-utility targets are all seven everyday tasks completed and
independently correct, at least four of five bounded repairs completed and
correct, and zero authority or protected-input failures. The original full-suite
gate continues to govern a full-suite claim; a scoped release must disclose any
missed stretch/full-suite gate. These small samples establish observed behavior,
not a general reliability rate or autonomous-coding qualification.

### Post-baseline driver boundary, before temperature outcomes

The original 26B/0.2 broad batch delivered nine of ten tasks. Its immutable
automatic result is six of ten; separate original-contract static review finds
seven clear successes, one uncredited public-contract ambiguity and two genuine
failures. The everyday tier has five clear successes among seven. Both the full
and scoped gates remain unmet.

All five original bounded tasks were admitted exactly once. A driver recording
error stopped the initial batch after task three; its persisted application state
independently confirms a deadline failure, while its original automatic grade
remains unknown. A byte-exact independently reviewed suffix ran only previously
unattempted tasks four and five. Four tasks failed final delivery; the fifth
delivered and passed automatic checks. Independent output review confirms one
clear completed success and four failed deliveries, with protected scope intact.
The known automatic results are three failures, one unknown and one pass. No
failed task was replaced, rerun or silently excluded.

After owned suffix processes stopped, a separately reviewed driver-only repair
was applied as policy `terminal-cancel-observation-v2`. Rejected cancellation now
requires a fresh terminal observation of the exact admitted run before export or
grading. Acknowledged cancellation that never reaches terminal state also stops
before export. Admission and bounded private exception metadata are retained;
safe summaries omit exception payloads. The standalone pilot stops on an
infrastructure error and exits nonzero. A synthetic reproduction proves a possible
cancellation race, not the actual third attempt's unidentified exception cause.

The solver executable, Content, task/checker bytes, model authority and generation
limits remain unchanged. The source tree and driver identity have changed: future
batches must bind the new driver separately from the executable's original build
provenance. Historical batches keep their original driver hashes and grades.
All eighteen temperature trials later used this same revised driver. Any future
matched models must disclose their driver identity and observation-policy
boundary; those comparisons are follow-up work, not an active closeout step.

The revised driver SHA-256 is
`5a096746dfaa1a98c32520f5c588d9b04dbc0c36c153f4f24585866b228c064f`;
the broad runner SHA-256 is
`44143223bedaafb1e7a5b8666b67339f38753c804913b95a1992c276b3a693b3`.
Thirteen focused offline tests, all 74 repository Python tests and all eight
Harness adapter tests passed after application. No new model outcome is claimed
by these software checks.

The unused confirmation pool independently showed the same two generic
measurement weaknesses as the primary pool. A separately archived/versioned
alignment passed author controls and independent disposable-copy replay before
any model exposure. Numeric expected values,
datasets and reference behavior stay fixed; public representation requirements
and narrative handling are aligned, with new positive and negative controls.
Original seals and prior structural evidence remain intact. This work neither
relaxes acceptance targets nor regrades the original primary outcomes.

The cleared confirmation-v5 manifest SHA-256 is
`59876e8e234133c729009895f024811a9a782828365aaedd3e92be0f1b72aef5`;
its seal SHA-256 is
`17a267ea7748da72ee2ae552d2fcf4444e40020348af6d45b5064ab9e2ae4a9e`.
The old v4 archive remains byte-exact. The v5 pool was subsequently used once in
the completed fresh confirmation pass; its initial pre-exposure state is
historical, and no repeated qualification is claimed.

### Completed sampling diagnostic and now-completed 1.0 controls

All eighteen predeclared temperature attempts completed and were independently
correct, with protected scope preserved: nine of nine at 0.2 and nine of nine at
1.0. Each condition repeated the same three tasks three times. Observed median
elapsed times were 156.2 seconds at 0.2 and 99.5 seconds at 1.0; the paired median
difference was -33.1 seconds. These are descriptive results for this small local
set. Six nonfatal tool failures, five retry events and provider-accounting limits
remain in the independent report. No general speed or reliability rate is claimed.

The final operator completed at 22:43:12 UTC. An intermediate progress derivative
still showed seventeen records and was refreshed from immutable exports later.
The earlier seventeen-row view is reconstructed and labeled as such; overwritten
original bytes are not claimed. Operator state and terminal exports determine
completion. All owned processes stopped before the next control window.

The user subsequently questioned the continued 0.2 setting. It was inherited from
earlier coding runs; Google's verified guidance recommends 1.0, top-p 0.95 and
top-k 64. New practical candidates start at **1.0**, unless completed evidence
supports a concrete alternative. The original 0.2 results remain historical
evidence. Any future matched 12B/E4B comparison requires a separately frozen
prospective policy. It is follow-up work; the earlier proposed 0.2 extension is
not an active schedule.

The general default-only Content control subsequently ran as A1 at Auto/1.0
and an 8K cap across the original exposed fifteen tasks. It produced eleven clear
successes, one uncredited ambiguity and three failures. Requested-Off A3 and
Auto/16K A2 each reached three clear bounded repairs out of five. Their exposed
outcomes do not establish causal or general reliability gains. The specialist
instruction/catalog mismatch around unconditional `verify_task` was real, but
its role in earlier timeouts remains unproven.

The policy was then frozen before the single fresh 15-task pass: general default-only
Content, Auto thinking, temperature 1.0 and an 8K cap for both broad and bounded roles,
with unchanged authority, 40 steps, 32K context, a 600-second ceiling and a 60-second
finishing reserve. The user's explicit bounded-closeout instruction superseded
the unstarted 45-attempt/repeat and smaller-model schedules. The completed fresh result
and missed gates are recorded at the top of this document.

The reviewed 13-file default-temperature source change is now applied, with
explicit values and frozen historical requests preserved. The opt-in public
coverage sibling is also applied and focused-source validated, preserving old
catalog data/reference/default routing and model grades. These software checks
do not change model scores or establish that 1.0 has an accuracy advantage.

The earlier eighteen-attempt temperature protocol and all 0.2 results remain
historical evidence. No active next step here calls for another temperature
run, repeated qualification, extra optimization or matched 12B/E4B comparison.
Committed-source A verification is now recorded above; the authorized branch
publication remains distinct from model or software qualification.
