# Calculation correctness, final delivery and Harness² campaign

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

## Status

Exploration, build and integrated checks are complete. The frozen 26B campaign
has been dispatched. Baseline and candidate are retained under
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

## User-directed interpretation and next evaluation

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
All eighteen temperature trials will freeze the same revised driver before the
first temperature outcome. Later matched models use that revision too; their
observation-policy difference from the original 26B baseline must be disclosed.

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
The old v4 archive remains byte-exact. Neither version has been exposed to a model.

### Completed sampling diagnostic and prospective 1.0 candidate

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
evidence. Prospective practical comparisons for 12B and E4B will use the same
selected 1.0 candidate and fixed tasks, rather than automatically extending the
earlier proposed 0.2 campaign; this decision precedes any secondary-model trial.

The next prepared candidate uses empty supplemental Content, retaining the
binary's general default instructions, Auto thinking and temperature 1.0. It
removes the selected coding-specific supplement, which unconditionally requested
`verify_task` even when the original bounded runs had no verification contract
and no such tool in their actual catalog. That instruction/catalog mismatch is
established; its role in the four pre-edit timeouts is unproven.

Preparation covers fifteen exposed diagnostic attempts: the original five
bounded tasks, followed by the original ten broad tasks, with fresh profiles,
unchanged task/checker/authority bytes and no retroactive verification contracts.
Every attempt stays counted. The original primary ambiguity remains uncredited;
this exposed pool supports diagnosis rather than fresh qualification. Because
Content, temperature and historical driver identity differ, this is a new overall
candidate rather than an isolated causal Content comparison. It has not started
at the time this amendment is written. Later configuration selection must freeze
before untouched confirmation, with repetition counts declared before inference.

A separately reviewed source proposal also aligns unspecified backend, UI and
CLI defaults to 1.0, preserving explicit values and frozen historical requests.
It remains unapplied while frozen-binary model trials continue. It does not
rewrite stored user settings or claim 1.0 is a universal optimum.

Keep the original 0.2 campaign and diagnostics unchanged. A separate temperature
diagnostic will copy three preselected everyday cases (currency reconciliation,
incident evidence and precise configuration) into a sealed subset without changing
their bytes. Test 0.2 and 1.0 three times each, in fixed batch order
`0.2, 1.0, 1.0, 0.2, 0.2, 1.0`: eighteen attempts, with the same frozen content,
model, runtime, task order, limits and authority. Record other sampling defaults;
do not claim the full publisher configuration when they are unavailable or
unsupported. This is repeated matched diagnostic evidence, not another unseen
qualification pool or permission to choose harness text from those outcomes.

Run primary 26B practical/bounded evaluation first, then its sampling diagnostic,
with matched 0.2 comparisons for 12B and compatible E4B reported separately.
No overlapping local inference, automatic model fallback, qualification bypass,
or additional model download is part of this plan. Any later configuration
selection needs fresh untouched confirmation and independent actual-output review.
