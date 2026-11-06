# Gemma harness-text hypotheses — 5 November 2026

This is read-only research during the frozen v4 campaign. It changes no runtime,
template, schema, generation setting or candidate by itself. Public development
traces showed contract-to-test drift, omitted final read-only verification and
same-content run variance. These observations motivated hypotheses, rather than
establishing causes or improvements.

- **Contract-derived test expectations.** Derive new assertion expectations from
  public requirements before implementation edits; resolve disagreements before
  treating green tests as correctness. A primary preprint reports mutually
  consistent faulty code and generated tests, with reduced fault detection when
  faulty implementations are supplied as context. Its setting does not establish
  an effect for this Gemma harness. [Konstantinou, Tambon and Papadakis,
  6 July 2026](https://arxiv.org/html/2607.05139v1).
- **Literal tool fields.** A short instruction can require declared tool names,
  keys, types and required arguments, followed by reading the actual result before
  dependent work. Google's guide defines those schema elements. The instruction's
  local effect remains untested. [Gemma function calling](https://ai.google.dev/gemma/docs/capabilities/text/function-calling-gemma4).
- **Verification-to-finish condition.** After the last edit, run prescribed
  read-only verification, then give the requested supported final when requirements
  are satisfied. Further tool work needs a specific unresolved requirement.
  Google's reduced-depth system-instruction example is a proof of concept;
  its reported reduction is not a local result or transferable guarantee.
  Ollama's example ends its agent loop when no tool calls remain.
  [Gemma prompt formatting](https://ai.google.dev/gemma/docs/core/prompt-formatting-gemma4#tip-adaptive-thought-efficiency-using-system-instructions),
  [Ollama agent loop](https://docs.ollama.com/capabilities/tool-calling#multi-turn-tool-calling-agent-loop).

Probe one new text delta at a time, retaining failures and delivery/correctness
separately. The actual proposer owns packet content; research suggestions are not
adopted lessons. Compare repeated matched outcomes before making causal claims.
Existing host defaults already include calculation coverage/invariants and final
concision, so duplicate text alone is not a useful new hypothesis.

Two integration leads were checked without edits. The Go adapter preserves thought
parts as inference thinking fields, and the managed adapter maps them to reasoning
content. Function definitions use explicit schemas. Google's Python automatic
schema-conversion caveat therefore supplies no demonstrated local defect. These
source checks establish neither a new live-transport result nor model efficacy.
No raw Gemma control tokens were added, and no private evaluation material was
consulted for this research.

## Completion-policy audit

A separate read-only source audit confirmed that bounded host completion checks
and repair already exist. They activate for callable verification under the
caller's feedback policy. Ordinary unqualified non-workflow runs enable that
feedback; other frozen/profile/workflow paths retain their specified policy.
Checks share the hard run context, authority, approvals and source-digest
receipts, with bounded repair and explicit host provenance.

Execution completion and verification outcome are separate fields. Unavailable
or persistently failing checks can leave a completed answer explicitly unverified;
this is intentional best-effort policy. A writable `run_command` test result does
not establish the generic `verify_task` receipt. These source facts do not identify
which policy caused any particular campaign failure and supply no reason to change
the frozen experiment. Ordinary app performance must be measured through its own
recorded path. No additional verifier architecture was implemented by this audit.
