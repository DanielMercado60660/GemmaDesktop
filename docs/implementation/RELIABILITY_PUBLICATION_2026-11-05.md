# Calculation correctness, final delivery and measured results

5–6 November 2026 · **model evidence closed; reviewed source changes applied; committed-source verification pending**

Twenty full Harness² refinement rounds are complete. The selected specialist harness has
mixed development evidence and misses the original fresh broad acceptance gate.
Later general-profile controls reuse those exposed tasks and remain diagnostic.
No reported profile has met the practical acceptance targets, including the
completed single fresh confirmation pass. The retained
specialist remains opt-in. This record separates automatic checks, independent actual-output review,
final delivery and source-build verification. It does not establish a general
success rate, an autonomous coding qualification or a Gemma capability ceiling.

## Changes built before evaluation

- Calculation guidance asks for representative parsing and type checks,
  scanned/parsed/skipped/selected coverage, units and relevant invariants, and
  investigation of surprising empty results. It asks for a concise supported
  final answer; successful process exit alone does not establish correctness.
- Optional caller `budgetSeconds` can narrow admission. The absolute deadline is
  saved for recovery. Optional `finishReserveSeconds` defaults to zero; these
  experiments explicitly use 60 seconds within a 600-second ceiling. At a safe
  boundary it permits one outer status-only generation capped at 1,024 tokens.
  It cannot interrupt a long in-flight generation or guarantee a solved final
  answer. Cancellation, exhaustion and incomplete work retain failure status.
- Development carry-forward preserves immutable Content, authority and
  provenance. Grading existing stopped outputs returns opaque hash-bound
  results. Packet/revision/stage identities distinguish equal-content branches
  across checkpoint and resume. Proposal authors and independent reviewers are
  separate roles; candidate guidance does not receive hidden grading details.
- Python/Node grading recognizes attributable executed-test failures in original
  production code while rejecting import, setup, test-only, malformed and
  resource failures. Version `2.1.0` uses policy `task-contract-v3`; task and
  reference data remain unchanged. Earlier grades are preserved.
- `./scripts/build.sh --ollama-only` supports a public source build without
  unpublished managed-runtime archives. It explicitly omits those resources and
  labels the omission in provenance. The default full-installer build remains
  strict.

Custom instructions and historical results are preserved. Experimental reserves,
candidate Content and managed runtime/model choices are explicit; the experiment
does not silently promote a profile or replace personal defaults.

## Frozen 26B development campaign

The twenty k=1 rounds used four fresh baseline/INV/DOM/composed solver probes per
round on five repeatedly used coding-repair stress tasks. They required contract
interpretation, code changes, independently designed regression tests, correct
verification and final delivery. Duration parsing tested software grammar and
validation, rather than ordinary arithmetic.

Conditions were local `gemma4:26b-mlx`, Ollama `0.40.11`, requested 32K context,
40 steps, Auto thinking, temperature 0.2, 8,192 ordinary output tokens, native
tools, a 600-second ceiling and serial inference. The model digest was
`sha256:f0fc7e0ae4947d382989b1f57db3d098a86b87c5b6e9523b9561ba81a2a64879`.
The frozen v4 executable SHA-256 was
`44356ed3562358207a5d93babbc4a167cbff0915c463777267cf772573b4ffab`;
its source fingerprint was
`155f8b5b2c5adaed02267e363748de72a850a70dc26e1e57a20acf21873bff76`.
These are experiment identities; a later publication build must record its own.

| Measurement | Observed result |
| --- | --- |
| Full four-probe rounds | 20; 19 normal reflections and one terminal rejection after an upstream failure |
| Unique solver probes, including failures | 80 |
| Delivered completed probes | 76/80 |
| Model budget/deadline failures | 4/80 |
| Frozen automatic check-and-delivery passes | 34/80 |
| Separately confirmed public contradictions among those passes | 4; frozen grades unchanged |
| Changed retentions | 6 |
| Durable lessons approved | 0 |

The selected revision was
`068df2c366ab41da57206fdcfb21bd7abb3d901526cbe27dca37a8b7351dc4bf`,
frozen before final diagnostic outcomes. Four automatic passes were independently
vetoed using public-contract evidence. The remaining automatic passes are not
exhaustive correctness certificates. Adaptive selection on reused tasks does not
support a causal or broad improvement claim.

Eight actual trials from stopped v2/v3 infrastructure-invalid segments remain
separate cost/protocol evidence, outside the eighty valid probes. They were not
erased or counted as completed refinement rounds.

During round 14, a source-symbol search accidentally rendered unrelated legacy
go-inventory reference/probe snippets. Independent scope and chronology review
found no selected-task or then-sealed-pool material overlap and placed both
proposals before the query, so the campaign continued with its evidence intact.
**Zero reference exposure is not claimed.** The exposed legacy task and materially
derived family are author-exposed for future unseen evaluation. Later searches
use explicit implementation-file allowlists and exclude fixture/private trees.

## Frozen paired development diagnostics

After selection, one fresh attempt per arm ran on each of the five development
tasks. No diagnostic outcome changed the retained revision.

| Arm | Final delivery | Frozen automatic check-and-delivery passes |
| --- | ---: | ---: |
| Initial harness | 5/5 | 0/5 |
| Selected harness | 4/5 | 2/5 |

Independent public-only static review supported the selected event-correlation
and literal-search outputs across all reviewed conditions. It found public
contradictions elsewhere and unresolved duration-token spelling scope. The
selected path task expired at 600.288 seconds without a final deliverable.
Its partial token accounting prevents a complete cross-arm cost comparison.
This is mixed stretch evidence from one pair per reused task, not broad
qualification or a measured speedup.

## Fresh broad evaluation: original 26B / 0.2 profile

The ten-task suite was independently authored and sealed before exposure. Its
seven noncoding tasks represent everyday work; three coding tasks require new
regression-test design and are reported as stretch work. Frozen automatic results
and separate original-contract static adjudication are both retained.

| Group | Final delivery | Frozen automatic combined passes | Clear independent successes | Ambiguous, uncredited | Genuine failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| Everyday | 6/7 | 4/7 | 5/7 | 1 | 1 |
| Stretch coding | 3/3 | 2/3 | 2/3 | 0 | 1 |
| Total | 9/10 | 6/10 | 7/10 | 1 | 2 |

The independent review identified one evaluator false negative, one
under-specified answer contract held uncredited, one incomplete functional repair
and one calculation deadline failure without the required final computation.
One calculation is a clear completed success; another has substantively supported
arithmetic but remains ambiguous at the answer-contract boundary. The third did
not complete. Only three of six critical numeric/coding tasks are clear successes.
Every category has a clear reviewed success. All ten protected inputs and frozen
output snapshots remained intact, with no observed unauthorized effects or
driver/grader infrastructure failures.

**The acceptance gate remains unmet.** Before exposure, the full-suite gate was
at least 9/10 completed and independently correct, all six critical numeric/coding
cases completed and correct, every category represented, and zero authority or
protected-input failures. Even crediting the ambiguity would yield only 8/10 and
leave critical failures. No result, threshold or requirement was rewritten.

The separate scoped-utility target was all seven everyday tasks completed and
correct, at least four of five supplied-test repairs completed and correct, and
zero authority/protected-input failures. The original everyday result already
misses that target. Status-only closure and a partly correct workspace do not
count as solved tasks. The exposed broad pool is now diagnostic evidence;
later selected profiles require untouched confirmation.

## Five bounded supplied-test repairs

All five original tasks were admitted exactly once under the original driver.
The initial batch stopped after task three when a recording exception lost its
terminal export. A byte-exact, independently identity-reviewed suffix admitted
only the previously unattempted fourth and fifth tasks. No failed attempt was
replaced, rerun or removed from the denominator.

| Measurement | Original five attempts |
| --- | --- |
| Required final delivery and clear independent correctness | 1/5 |
| Delivery failures | 4/5 |
| Preserved automatic grades | One pass, three failures, one unknown |
| Separate driver recording failures | One; exact exception cause/phase unknown |
| Independently preserved protected inputs and scope | 5/5; no observed unauthorized effects |

The third task's persisted application state independently establishes a deadline
failure without required delivery. Its lost automatic grade remains unknown;
it was not reconstructed or rewritten. The four unsuccessful cases retain starter
source. Recorded trajectories show inspection/provided-test work followed by
length-capped generations without visible answer or tools. Two interrupted final
attempts have unknown terminal counts. These observations do not establish an
internal reasoning cause or a cancellation race. The successful case has one
permitted production edit, recorded passing provided tests and an accurate final
answer. Independent adjudication was static, without replay or regrading.

The supplied-test threshold of at least four clear completed repairs out of five
is unmet. This finite sample adds practical evidence about this frozen profile;
it does not establish a general coding ceiling.

## Post-stop driver observation repair

After all owned bounded processes stopped, a reviewed **driver-only** repair was
applied as policy `terminal-cancel-observation-v2`. A cancellation rejection now
requires a fresh terminal observation of the exact admitted run before export or
grading; acknowledged cancellation that remains nonterminal also aborts before
export. The driver retains an admission checkpoint and bounded private exception
metadata, excludes exception payloads from safe summaries, and makes the standalone
pilot stop with nonzero exit on infrastructure failure. It does not resubmit runs
or fabricate terminal status. A synthetic control demonstrates a possible race,
not the unidentified cause of the third original recording exception.

The source tree changed; the solver executable and selected Content did not.
The revised `gemma_pilot.py` SHA-256 is
`5a096746dfaa1a98c32520f5c588d9b04dbc0c36c153f4f24585866b228c064f`;
the revised broad runner SHA-256 is
`44143223bedaafb1e7a5b8666b67339f38753c804913b95a1992c276b3a693b3`.
Thirteen focused offline tests, all 74 repository Python tests and eight Harness
adapter tests passed. Independent applied-source review cleared the exact hashes
and existing validation evidence without another test/model run. The earlier
68-test pre-v4 record remains a separate dated observation.

The completed eighteen-attempt temperature diagnostic and later A1/A3 controls
freeze this revised driver. The completed A2 cap control uses it too. Historical 26B
results retain their original driver hashes, automatic grades and artifacts.
Comparisons with that baseline disclose this observation-policy boundary. The
software repair adds evidence retention; its passing tests do not establish a new
model success or remedy for the observed delivery failures.

## Completed temperature-only diagnostic

The diagnostic copied three preselected everyday cases byte-for-byte and made
three attempts per case at each temperature. Its fixed six-batch order was
`0.2, 1.0, 1.0, 0.2, 0.2, 1.0`, with three cases per batch. Model, specialist
Content, task order, tools, authority, Auto thinking, 8,192-token cap, requested
32K context, 40 steps, 600-second deadline and 60-second finishing reserve stayed
fixed. The revised driver was identical in both arms. No attempt was replaced.

| Temperature | Attempts | Final delivery | Clear independent successes | Protected scope | Median elapsed |
| --- | ---: | ---: | ---: | ---: | ---: |
| 0.2 | 9 | 9/9 | 9/9 | 9/9 | 156.2 s |
| 1.0 | 9 | 9/9 | 9/9 | 9/9 | 99.5 s |

The nine paired elapsed differences had a median of −33.1 seconds for 1.0 minus
0.2. These timings describe this small repeated set; they do not establish a
causal speedup or general reliability advantage. Six nonfatal tool errors and
five ordinary generation-retry events remain recorded across the eighteen
attempts. There were no observed authority/protected-input violations or
recorded driver/grader infrastructure failures. Provider counters are reported
observations, not a claim of complete physical allocation or all publisher
sampling controls.

**Why 0.2 was used:** the earlier coding profile supplied that inherited value,
and the fixed comparison retained it as the explicit control. It was not proved
optimal. Google's published recommendation is temperature 1.0, top-p 0.95 and
top-k 64. Both temperatures passed all nine attempts, so new practical candidates
start at 1.0. Installed-tag metadata reported top-p 0.95 and top-k 64; these two
settings were not explicitly overridden in the requests. This observes tag
metadata and admitted requests, rather than every effective serving detail.
[Official sampling guidance](https://huggingface.co/google/gemma-4-26B-A4B-it#1-sampling-parameters).

## Later controls on exposed tasks

A1 uses the general default instructions with an empty supplemental Content,
Auto thinking, temperature 1.0 and the original 8,192-token output cap. It first
ran the same five bounded tasks, then the original ten-task broad pool, once per
task in isolated profiles. Original task bytes, authority and answer-contract
ambiguity were retained. Content and temperature differ from the historical
specialist profile, and the driver changed after that baseline; no isolated
cause is assigned to the difference in outcomes.

| A1 group | Delivery | Frozen automatic passes | Clear independent successes | Ambiguous, uncredited | Genuine failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| Bounded repairs | 3/5 | 3/5 | 3/5 | 0 | 2 |
| Everyday | 7/7 | 6/7 | 6/7 | 1 | 0 |
| Stretch coding | 3/3 | 2/3 | 2/3 | 0 | 1 |
| Total | 13/15 | 11/15 | 11/15 | 1 | 3 |

All three calculation outputs have substantively supported arithmetic and
coverage; only two are credited because the original answer-contract ambiguity
remains uncredited. Four of six critical primary tasks are clear completed
successes. Two bounded tasks failed to deliver after length-capped generations
without implemented repairs. A stretch repair remained functionally incomplete
despite passing local tests and host verification. All fifteen protected scopes
were preserved, with no observed unauthorized effects or recorded driver/grader
infrastructure failures. Both acceptance targets remain unmet.

A3 changes only the requested thinking setting from Auto to Off relative to A1's
bounded controls. It keeps the same five exposed tasks, temperature 1.0,
8,192-token cap and remaining admission controls.

| Bounded control | Delivery | Frozen automatic passes | Clear independent successes | Genuine failures |
| --- | ---: | ---: | ---: | ---: |
| A1 / Auto / 8,192 | 3/5 | 3/5 | 3/5 | 2 |
| A3 / requested Off / 8,192 | 5/5 | 4/5 | 3/5 | 2 |
| A2 / Auto / 16,384 | 3/5 | 3/5 | 3/5 | 2 |

One A3 automatic pass was independently vetoed for a public-contract failure;
its original grade is unchanged. Faster delivery alone does not qualify the
profile. Requested Off, source-derived wire intent and zero thinking characters
in captured emitted-channel metrics are recorded separately. The actual
received wire flag and absence of internal reasoning remain unknown. No
executable counterexample, task replacement or assistance was used in this
adjudication. A3 also misses the required four clear bounded successes.

A2 is complete and independently audited after owned-process stop. It changed
only A1's bounded output cap from 8,192 to 16,384, keeping general default
instructions, Auto thinking, temperature 1.0 and all other frozen controls. Three
of five attempts delivered independently correct repairs; two reached genuine
deadline failures. Automatic passes were also three of five. All five protected
scopes were preserved, with no observed authority violations or recorded
driver/grader infrastructure failures. Terminal provider counts for the two
interrupted calls remain unknown.

A1 and A2 have the same three-of-five aggregate result with different successful
cases. One exposed stochastic attempt per task/configuration does not establish
causal benefit or a general reliability rate. The 16K candidate missed its
predeclared all-five-delivered condition, so it was not selected for the fresh
confirmation policy. A1, A3 and A2 reuse exposed pools; none is fresh confirmation.

## Completed single fresh confirmation pass

The user bounded the remaining model work to one fresh pass, followed by measured
capabilities/limits, reviewed source/build checks and publication. All fifteen
planned model attempts are now terminal, all were independently statically
reviewed, and the owned processes are stopped. The final safe checkpoint,
independent audit and owned-stop proof are hash-bound below. Model evidence is
closed; no further model phase belongs to this closeout.

The prospectively frozen policy used general default-only Content, Auto thinking,
temperature 1.0, an 8,192-token output cap, requested 32K context, 40 steps, a
600-second deadline, a 60-second finishing reserve, native tools, restricted
approval authority and the revised driver. One broad ten-task batch preceded one
five-task supplied-test batch in two isolated profiles. There were no task
reruns, replacements, fallback, rescue or tuning after exposure. All fifteen
outcomes remain in the denominator.

| Tier | Delivery | Frozen automatic passes | Clear independent successes | Ambiguous, uncredited | Genuine failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| Everyday | 5/7 | 3/7 | 3/7 | 1 | 3 |
| Stretch coding | 3/3 | 2/3 | 1/3 | 0 | 2 |
| Supplied-test repairs | 4/5 | 4/5 | 4/5 | 0 | 1 |
| Total | 12/15 | 9/15 | 8/15 | 1 | 6 |

The broad ten-task subtotal is eight deliveries, five automatic passes, four
clear successes, one uncredited document answer-representation ambiguity and
five genuine failures. The ambiguity remains uncredited; no hidden intended
representation is inferred. One stretch automatic pass was independently vetoed
for a public-contract gap, with the original grade unchanged. Green provided
checks, a host receipt, completed status and model prose alone do not establish
full contract correctness.

Of the three calculation tasks, one is a clear completed success with supported
units, coverage and arithmetic. The other two failed to produce the required
final answer; they are delivery failures, not evidence of two wrong arithmetic
answers. The four credited supplied-test repairs each have a fresh read-only
host verification receipt. The fifth reached its deadline without required
final delivery. Although four repairs were correct, the scoped target also
required all five bounded final deliveries. **Both the full and scoped acceptance
gates remain unmet, with the original thresholds unchanged.**

All fifteen original protected inputs were preserved. One task-instruction scope
violation created an extra helper inside its admitted disposable workspace but
outside the task's allowed-file set. Static receipt/source-scope review observed
zero platform authority-grant violations and zero writes outside the admitted
workspace. Protected-input preservation does not mean zero effects; the
in-workspace instruction breach remains part of the result. No driver/grader
infrastructure error was recorded in the fifteen reviewed outcomes.

One successful coding row recorded one host context-compaction operation. The
checkpoint counts two compaction markers for that single operation. Of 166
complete generation metrics, 165 ordinary responses used the frozen 8,192 cap
and one separate compaction summary used 3,072; all recorded contexts were 32K.
The missing terminal accounting for one interrupted finishing call prepared at
1,024 remains unknown and is never replaced with zero. Seven ordinary retries
and seventeen tool-error events remain recorded. These observations do not
establish physical allocation, received wire options or an internal reasoning
cause. Final lifecycle, receipts, automatic grades and independent adjudication
stay separate evidence layers.

The fresh broad pool's archived pre-exposure alignment addressed public answer
representation and narrative handling without changing numeric expected values,
datasets or reference behavior. The separate fresh bounded pool supplied tests
and explicitly bound host verification without requiring new regression-test
design. That verification contract differs from the old bounded pool and is an
admission boundary, not an isolated harness improvement. Both pools cleared
independent evaluator controls before first model exposure. Evaluator controls
are not model success.

This is bounded evidence from **one pass**. It is not formal repeated
qualification, a general success-rate estimate or a published-benchmark
reproduction. The observed strengths are individual successful file/configuration
work, one calculation, one stretch repair and four supplied-test repairs; the
same pass also demonstrates document coverage/representation limits, incomplete
coding, missed final answers and one file-scope instruction breach. Those results
support supervised experimentation with independent checks, not broad autonomous
reliability.

## Remaining closeout and deferred follow-up

| Work item | Recorded status |
| --- | --- |
| Fresh fifteen attempts and owned-process stop | Complete; all fifteen independently statically reviewed |
| Final safe checkpoint, actual-output audit and owned-stop bindings | Complete; exact SHA-256 identities below |
| Product temperature default 1.0 | Exact reviewed thirteen-file proposal applied; focused checks passed; explicit saved 0.2/0 values preserved |
| Opt-in public pointer-coverage sibling pack | Exact reviewed eight-file change applied; focused source/host controls passed; original corpus/reference/grades preserved |
| Final committed-source clean-clone build/checks and GitHub ref | Pending source commit, clean-clone verification and remote-ref proof |
| Prior 45-attempt repeated schedule | Never started; archived/preserved and deferred |
| 12B quality comparison | Unexecuted follow-up only |
| E4B requested 32K qualification and quality comparison | Unexecuted follow-up only; normal qualification remains a prerequisite |
| Further harness optimization | Follow-up only; no more model phases in this closeout |

Published Gemma benchmark scores use different tasks and procedures. They cannot
supply this application's expected success rates or replace independent review
of its actual outputs.
[Official model card](https://huggingface.co/google/gemma-4-26B-A4B-it#benchmark-results).

## Source validation and publication verification

All live-model results above use the frozen v4 executable, not a newly built
publication binary. The later driver changes and documentation make the current
source tree differ from that original build. After model work closed, the exact
reviewed thirteen-file temperature-default proposal was applied. Default 1.0
now reaches backend, UI and ordinary pilot admissions while explicit saved
values remain preserved. No database/profile/history migration or historical
trial rewrite occurred. The separate reviewed eight-file public-coverage change
was also applied and passed its focused source/host controls. These checks do not
replace committed-source clean-clone validation.

Before the v4 freeze, full app Go race tests passed. Other Go race packages passed
except four retained media-version qualification failures: the host had ffprobe
9.0.2 while the media path requires 8.1. Repository Python tests passed 68 checks,
Harness adapter tests passed 8, and frontend checks passed 76 unit plus 73 UI tests,
TypeScript and the production build. The full Go suite is not reported as green.

Source-bound grading controls passed 118 subtests, including all 18 existing
reference fixtures, two negative controls, seven synthetic Python full-grade
controls and ten Node regression-gate controls. Node regression-gate checks do
not qualify the entire semantic grader. The production full-installer build,
local ad-hoc signature and input/resource provenance checks passed. These source
checks do not establish live-model correctness, video qualification or hardware
performance.

The applied temperature-default change passed focused validation: four app API
functions under Go race checks (2.927 seconds reported), 21 Python tests, five
frontend unit tests and three UI tests. This evidence is the actual recorded
tool output plus a hash-bound receipt; no standalone raw stdout log was retained
for those focused checks. It does not claim the final committed tree or clean
clone has passed. The application and validation SHA-256 identities are
`6e3b1cd62ff9ab60d26f5c535fb0956bf0b99ce90967b48201665d1cf212ea69`
and `a4ad55f4a4a73c700c321f4ac802859dab54b0a888d100899dcefb6cdbbf90fd`.
The former driver-only source hashes above remain historical experiment
identities; the post-evaluation default source has its own application identity.

The opt-in public pointer-coverage sibling pack applied exactly eight reviewed
files while preserving all 38 original catalog sources, original reference/data,
default routing and historical model grades. Two Go preservation tests, three
observer tests and trusted-host controls passed: the corrected sibling reference
covered 15 cases, five mutant controls were rejected, and all nine child handles
were waited and reaped. Formatting showed no drift. These are focused source and
host-oracle checks, without model calls, app launches, profile changes or a full
app-grader/containment qualification. The application receipt SHA-256 is
`dcc0e2ac3160b39f5195041010286c47403c3aaf4ca55f15dd559cd8acf8ebd7`;
focused validation is
`29bdaab191e1da04a14463876521f8c3c19e7481cdaf2325d5e20f60c777ce7e`.

The historical private held-out fixture directory is now ignored. All 143 local
files were preserved, with zero tracked files and zero unignored eligible files;
no staging occurred in that exclusion step. Its safe metadata receipt SHA-256 is
`94c2a68cf475fc1d275f82ea528aa5de12f69fc50d0a945e2a1abe5eb29d2ade`.
The final staged inventory and clean clone still require their own checks.

| Publication requirement | Current observation |
| --- | --- |
| Reviewed final source/docs and sanitized public evidence | Pending final review; private raw evaluations, historical held-out fixtures and model artifacts excluded |
| Final committed source identity / publication ref | Pending; no public ref asserted |
| Fresh local committed-source clone | Not yet verified for the final publication tree |
| Clean-clone Ollama-only build, provenance and signature | Pending |
| Clean-clone mocked ordinary-app delivery and appropriate tests | Pending; retain media failures and explicit skips |
| Final build identity and sanitized verification evidence | Pending |

The private fixture ignore rule is applied and the files remain local. Before
committing, audit the actual staged path list to confirm none are included. The
clean clone must validate committed software without those fixtures. The initial
publication inventory overclassified this tree and is not the final allowlist.

Model evaluation is closed. After the reviewed changes are committed, verify
that exact source identity in a separate clean local clone. Do not copy ignored
runtime data, model weights, managed archives, dependency caches or build output.
Use `./scripts/build.sh --ollama-only`, verify `dist/build-info.json` and the ad-hoc
signature, then run the ordinary-app mock regression and appropriate repository
checks. Preserve all failures and explicit skips. Record the commit, source and
frontend fingerprints, binary hash, host and command results in sanitized public
evidence before publication. A later public GitHub clone replay is a separate
observation.

An Ollama-only build must report its omitted managed resources honestly. A full
installer additionally requires verified runtime archives. Successful local
compilation and ad-hoc signing are not notarization, Gatekeeper distribution
validation, a clean-machine UI review, minimum-hardware qualification or video
qualification. Historical October and 4 November records retain their dated
scope and do not substitute for final-tree verification.

## Public aggregate provenance

The [selected aggregate derivative](../evidence/reliability-round3-summary-2026-11-05.json)
contains source-report SHA-256 identities, separate automatic and independent
counts, experiment identities and remaining fields. It omits private task inputs,
expected answers, probes, prompts, transcripts and local user paths. Full original
records remain unchanged locally. Its committed-build/publication fields must be updated from actual verification
evidence. Reviewed source changes and focused checks are recorded above; no
clean-clone or GitHub completion is asserted before those actions occur.

### Final single-pass evidence identities

| Safe original | SHA-256 |
| --- | --- |
| Independent actual-output audit | `4e2bb4d28150eb73573df3ae1635c9fe2ab2d7de8a9a0d1549bc6c4dcf5c457b` |
| Final owner checkpoint | `bb8b2891868d3880fe55016897219250a00ef1fcd4bfb7b63ffa3dc48fc807ab` |
| Owned-process stop proof | `fd2c9889e5a94db259be87ee16171411efc145b620581a4ae8cc8f372d28ff67` |

These identify the frozen experiment and its preserved local safe originals.
Post-evaluation source application has its own identities above. The clean-clone
build and GitHub publication need their actual verification identities; none
retroactively changes the model results.
