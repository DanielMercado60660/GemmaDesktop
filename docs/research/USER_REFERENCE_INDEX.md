# Gemma Desktop user reference index

Recovered on 4 November 2026 from the saved original product brief, the project
chats and subsequent research reports. These are the references Daniel supplied
or explicitly carried into the project plan. The supporting-research section is
separate. This preserves historical references and source pins; it is not a new
audit of current upstream heads.

## Original product brief and earlier project work

The original list is preserved in
[product-specification-v1.md](../reference/product-specification-v1.md), section 13.
The [October reference audit](reference-audit-2026-10-27.md) and
[capability reconciliation](phase2-reference-audit-2026-10-27.md) explain what was
borrowed and what was deliberately excluded.

| Reference | Why it belongs in the handoff |
| --- | --- |
| [Google ADK Go](https://github.com/google/adk-go) | Agent and workflow runtime; current application dependency is 2.7.1. Investigate actual Go behavior before replacing the loop. |
| [ADK documentation](https://adk.dev/llms-full.txt) | Runtime guidance; verify against the pinned Go implementation. |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Tool policy, scoped context, plans, recovery and specialist-agent patterns. October inspected pin: `363a13119a57354f469732577518b9d6b319d1d6`. |
| [Ollama](https://github.com/ollama/ollama) and [API](https://docs.ollama.com/api) | Existing local runtime and native protocol; historical/current recorded baseline 0.40.11. |
| [Gemma 4 Ollama catalogue](https://ollama.com/library/gemma4) | Existing 12B/26B and possible E4B artifacts; freeze full identity rather than relying on a tag. |
| [Wails](https://wails.io) | Native desktop shell and bridge; current Go/JS dependency is 3.1.0. |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Tools, transports and authentication; host still owns permissions and uncertain effects. |
| [Qwen Code](https://github.com/QwenLM/qwen-code) | Tool/edit handling, skills, scoped context and exploration. See the newer pin below. |
| [OpenWork](https://github.com/different-ai/openwork) | Workspace, capability and permission UX. October inspected pin: `fa29840a71b371951df3b0cd2bee3fb3fb48539d`. This is distinct from OpenCowork. |
| [OpenWorker](https://github.com/andrewyng/openworker) | Provider/executor/lifecycle separation. See the newer pin below. |
| [Harness²](https://github.com/google-research/harness2) | Propose–Probe–Compose refinement, frozen trials and lessons. Historical pin `1cfe254e8d25b15f37048c3730134686abe60335`; current vendored pin `6868817518876de8fd170c6a8354daca273b6191`. |
| [A2A Go](https://github.com/a2aproject/a2a-go) | Possible future interoperability; not required for current local delegation. October inspected pin: `2692308d0926f05fa8e7892e2dacb099848200fe`. |
| [Gemma visual reference](https://deepmind.google/models/gemma/) | Original visual direction; local supplied concept is `design/reference/gemma-desktop-concept.png`. |

## The eight references supplied for the MLX round

Detailed source-level findings and file links are in
[the second November exploration](mlx-donors-harness-round2-2026-11-04.md).

| User reference | Inspected source pin | Concrete question |
| --- | --- | --- |
| [mlx-serve](https://github.com/ddalcu/mlx-serve) | `5b6fae449722a5dd9b93a108ee3a6e01e8e3c8f5` | Does native serving improve complete task performance while preserving context/thinking/tool semantics? The packaged stable v26.11.1 binary instead maps to `ac00f9213214db94b96f1837282c1ce4ad9debc8`; qualify that exact build. |
| [oMLX](https://github.com/jundot/omlx) | `4213c3284abc7b3746a91464a7af7ccc3732b759` | Prefix reuse, serving metrics and Gemma tool conversion. App bundle adds a reviewed current-tool-cycle reasoning patch; one E4B/4K smoke passed. |
| [mlx-lm](https://github.com/ml-explore/mlx-lm) | `736ab0d8c1d0edc79c3d798044fe6904e2fa23be` | Reference model/template implementation and diagnostic control. |
| [Hermes Agent](https://github.com/nousresearch/hermes-agent) | `ed1afb082eb3e46cf2edae9d8f68fb67b09ee4df` | Bounded verify-before-finish feedback, stream repetition guards and context projections. |
| [Qwen Code](https://github.com/qwenLM/qwen-code) | `b9e20830f7727c97a5db6e129d542b0d572cadd3` | Reuse file reads only while content remains in model context; bounded exploration reminders and scoped tool discovery. |
| [ZeroClaw](https://github.com/zeroclaw-labs/zeroclaw) | `683d10139bc200ab8db768fcb4515f79404846ac` | Outcome-aware repeated-failure detection with controlled feedback and false-positive checks. |
| [OpenCowork](https://github.com/OpenCoworkAI/open-cowork) | `e94ee2c50239e64ba0e18921e339d49b93dae31f` | Separate loop detection policy from warning, stopping and cancellation effects. |
| [OpenWorker](https://github.com/andrewyng/openworker) | `5e3d7261f203bbd94a91fa10856d29aad0bfaeae` | Narrow completion-provider interface and persistent draft lifecycle. |

These are ideas to compare against observed failures. Borrowed mechanisms remain
experimental until task results support them. Source popularity, model-card
scores and raw tokens per second do not establish this app's reliability.

## Other references Daniel supplied in project chats

| Reference | Original context and present scope |
| --- | --- |
| [EmbeddingGemma 2 announcement](https://blog.google/innovation-and-ai/technology/developers-tools/embeddinggemma-2/) | Requested video-question retrieval after the realistic coding workflow. Preserve the existing video/embedding path on Ollama; it is not the next coding milestone. Recovered from the “Plan new project” chat. |
| [Gemma 4 Developer Agent competition](https://www.kaggle.com/competitions/gemma-4-developer-agent) | Discussed as a possible later application of harness work, with Gemini assistance and possible training. Recovered from “Plan Gemma competition harness”. Competition work and training were not implemented. Recheck current rules/deadlines before pursuing it; it is not authorization to add cloud inference or train now. |

## Supporting sources found during research

These were research additions, not all links supplied directly by Daniel:

- [Google Gemma 4 model card](https://ai.google.dev/gemma/docs/core/model_card_4)
  and [thinking guide](https://ai.google.dev/gemma/docs/capabilities/thinking):
  compare E4B, 12B and 26B and inspect model-specific thinking templates.
- [Harness² paper](https://arxiv.org/abs/2610.14224) and
  [current vendored revision](https://github.com/google-research/harness2/tree/6868817518876de8fd170c6a8354daca273b6191):
  methodology does not guarantee the same gains for this Gemma harness.
- Anthropic on [tool design](https://www.anthropic.com/research/writing-tools-for-agents),
  [context engineering](https://www.anthropic.com/research/effective-context-engineering-for-ai-agents),
  and [agent evaluations](https://www.anthropic.com/research/demystifying-evals-for-ai-agents):
  methods for concrete tool contracts and outcome-based evaluation.
- Ollama [thinking](https://docs.ollama.com/capabilities/thinking),
  [tool calling](https://docs.ollama.com/capabilities/tool-calling), and
  [context length](https://docs.ollama.com/context-length): distinguish requested
  controls from effective support and measured behavior.
- [Pinned E4B artifact](https://huggingface.co/mlx-community/gemma-4-e4b-it-4bit/tree/475b9088d29754a3379866cf5aeb6b41acd313c2),
  [12B artifact](https://huggingface.co/mlx-community/gemma-4-12B-it-4bit/tree/73bcf09092aa277861d5a191b989b666f7f32e8f),
  and [26B artifact](https://huggingface.co/mlx-community/gemma-4-26b-a4b-it-4bit/tree/0d77464eeb233a2da68ebf9d7dc4edaac7db956d):
  exact managed catalog inputs; only E4B was acquired and exercised in this round.

Use [the Claude handoff](../implementation/CLAUDE_CODE_HANDOFF.md) for the current
objective, known outcomes, first task pilot, and the checkpoint/promotion rules.
