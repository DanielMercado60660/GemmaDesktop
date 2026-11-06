# Prospective agent temperature default

New agents and configurations that omit temperature start at **1.0**, matching
Google's published Gemma 4 starting recommendation. This is a publisher starting
point, not a locally proven optimum for every model or task. It does not imply
that all publisher sampling parameters are explicitly configured by this change.
[Official sampling guidance](https://huggingface.co/google/gemma-4-26B-A4B-it#1-sampling-parameters).

Explicit saved temperatures, including 0.2 and 0, remain authoritative. Agent
admission resolves a legacy missing field to the new default prospectively;
it does not rewrite the stored agent or past requests. The editor shows that
prospective default and writes the displayed value only when the user saves.
The existing agent.save API replaces the supplied profile: omitting temperature
on a save uses the current default even for an existing ID; callers preserving
an explicit choice must include it. This is not a partial-update migration.

No database migration, historical checkpoint/experiment rewrite, model-name
heuristic or automatic user-data update is included. Historical trials with
explicit temperature 0.2 retain that exact setting and their original meaning.
Temperature 1.0 is not a claim of proven broad-task reliability or a reason to
reinterpret prior scores.

The ordinary pilot/broad runners use the same new unspecified default while
preserving --agent overrides. New campaigns freeze their default or explicit
--temperature value in the ledger and use it for both agent creation and trial
admission. Resume preserves the recorded value and rejects a conflicting explicit
override; it neither infers nor migrates a missing legacy value. The frozen
operator-hash guard still applies. Use the retained historical operator for an
old ledger, without bypassing that guard. The separate harness pilot already
uses temperature 1 and its explicit profile is unchanged.

Apply/rebuild only after frozen model phases finish, with one reviewed source
boundary. Existing frozen binaries and explicit experimental configurations are
not changed in place. Future comparisons must record their actual temperature,
source/binary/driver identities and independent correctness/delivery evidence.
