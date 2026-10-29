# Capability and Harness² reference reconciliation

27 October 2026. Implementation audit for the [accepted phase](../implementation/PHASE2.md). Qualification results are recorded separately; a source mapping is not proof of a successful live task.

The six project READMEs below were re-read at the previously inspected revisions through the GitHub connector. The MCP SDK authorization-code implementation was also checked at v1.8.0. The pinned Harness² source is vendored with its license and per-file SHA-256 manifest in `third_party/harness2`. No dependency was silently upgraded to a newer branch during this phase.

| Reference | Applied contract and local implementation | Boundary retained |
|---|---|---|
| [Gemini CLI](https://github.com/google-gemini/gemini-cli/tree/363a13119a57354f469732577518b9d6b319d1d6), including its [planning](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/docs/tools/planning.md) and [project-context](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/docs/cli/tutorials/memory-management.md) contracts | App-owned durable task plans, bounded hierarchical project guidance, immutable run permissions, and recorded recovery state. Context sources remain lower-authority material. | A plan status cannot approve a tool or establish correctness. Approval recovery and starting another turn remain distinct operations. |
| [Qwen Code](https://github.com/QwenLM/qwen-code/tree/32f6713ce6394b48dc384d301ee761fb9a1b5b80) | Full skill packages, inspectable context, and configured specialists. Existing exact revision checks and failed-edit recovery are retained. | Skill import never expands an older agent's profile. Measured model usage and heuristic context estimates have different labels. |
| [OpenWork](https://github.com/different-ai/openwork/tree/fa29840a71b371951df3b0cd2bee3fb3fb48539d) | Explicit capability selection and connection configuration, with credentials separated from reusable skills and tool declarations. | No organization/cloud control plane or background provider connection. Donor license terms are not assumed to apply uniformly across directories. |
| [OpenWorker](https://github.com/andrewyng/openworker/tree/59151e3b306d9edc2b22e6e2bb4443316f563f47) | Separate execution, permission, credential and lifecycle ownership; independent verification remains distinct from the solver's claim. | Commands receive private copies, not original-project/home/socket mounts. Persistent background processes and stdio MCP do not inherit the new executor's authority. |
| [ADK-Go v2.6.0](https://github.com/google/adk-go/tree/v2.6.0) | ADK remains the model/tool and workflow infrastructure. Shared budgets, frozen agent identities, durable child records and cancellation wrap its existing orchestration. | No parallel handwritten agent runtime. Newer recovery fixes remain a separate dependency decision; bounded local delegation does not require A2A. |
| [MCP Go SDK v1.8.0 authorization](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/auth/authorization_code.go) | Native browser, PKCE, state/issuer validation, constrained endpoint discovery, Keychain persistence and refresh lifecycle. SDK discovery requires protected-resource and issuer metadata; explicit preregistered endpoints need a separately identified compatibility path. | Reauthentication never blindly repeats a possibly effective action. No external-provider compatibility claim without exercising that provider. |
| [Harness² pinned source](https://github.com/google-research/harness2/tree/1cfe254e8d25b15f37048c3730134686abe60335) | Native and upstream Propose–Probe–Compose paths share Go-owned trials, permissions, fixtures and evidence. The adapter adds an explicit declarative Gemma surface while retaining the published core source bytes. | Four solver rollouts and three improvement stages for k=1. Codex packets supply proposals and independent review; Gemma remains solver. Local parity is not reproduction of Google's benchmark scores. |
| [A2A Go](https://github.com/a2aproject/a2a-go/tree/2692308d0926f05fa8e7892e2dacb099848200fe) | Preserve identity/version/cancellation concepts for a possible later remote boundary. | Remote federation remains deferred. No A2A endpoint or external-agent connection is added here. |

## Integration issues found during this pass

The review found concrete gaps beyond navigation: uncertain container cleanup initially did not block the next command; private dependency environments initially lacked lockfile identities; workflow resolvers initially retained the primary profile's permissions; the UI initially used the wrong waiting-packet status; and Python imported donor modules before checking their hashes. Each was repaired and regression-tested at its owning boundary before the 0.2.0 binary was frozen. Further integration checks repaired skill-import recovery, durable plan receipt links, metadata-only list queries, immutable cache cleanup ownership, and packaged upstream resources. The qualification report records the frozen build identities and separates infrastructure checks from live solver outcomes.

The experiment corpus has eighteen tasks with equal language and split counts. Three larger projects exercise a TypeScript worker, Python transactional persistence, and a Go HTTP service, with multiple source changes. The other fifteen are narrower contract regressions. This is deliberately reported as mixed-depth coverage, not eighteen large applications.

Execution constraints remain explicit in [the executor contract](../../internal/execution/README.md): offline guests, finite commands, bounded snapshots/publication, checksum-selected public dependencies, and monitored retained storage. The storage monitor is not a hard filesystem quota; publication is journaled per-file recovery, not an atomic transaction against unrelated editors.

Native Xcode execution, Apple-container backend, persistent server previews, remote A2A, arbitrary executable optimizer plugins, expanded media understanding and public notarization remain outside this phase. Historical pre-phase documents are retained for provenance; their old statements about missing capability profiles, full skill imports and coding harnesses are superseded only by the implementation and final qualification records for this phase.

## Final qualification reconciliation — 28 October 2026

The [completed phase report](../evidence/phase2-qualification.md) now records both
matched native/upstream cycles, all 72 paired attempts, an independent rejection
and baseline retention. The references informed host-enforced authority and
execution boundaries; they did not ensure solver compliance. Fresh read-only
verification, test discovery and public-contract edge cases remained practical
failure modes even when guidance reached the model correctly.

The separate 0.2.1 correction adds explicit complete/partial/unknown token
accounting, bounded command metadata preservation and durable status fixes.
Its tests, real Docker connection-loss/build/multiprocessing checks, native
workbench inspection and local package are complete. These do not rewrite the
frozen 0.2.0 campaign or establish Google's published benchmark performance.
All reference revisions and dependency pins above remain unchanged. The
[delivery coverage closure](../evidence/phase2-delivery-coverage-audit.md) maps
remaining proof gaps to their final evidence; no additional mandatory reference
feature was identified. External compatibility and agreed deferrals stay explicit.
