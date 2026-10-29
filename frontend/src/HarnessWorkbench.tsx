import { useEffect, useState, type ReactNode } from "react";
import {
  BookOpen,
  Copy,
  FlaskConical,
  GitBranch,
  Play,
  RefreshCw,
  Square,
} from "lucide-react";
import { api } from "./bridge";
import type { PageProps } from "./Management";
import { parsePacketResponse } from "./lib/harnessPacket";
import {
  costComparisonExplanation,
  hasQualifiedCostComparison,
  type HarnessCostGate,
} from "./lib/harnessCost";
import {
  Button,
  Empty,
  Field,
  Modal,
  SectionHeader,
  friendlyStatus,
} from "./components/ui";

type RecordData = Record<string, unknown>;
interface Task {
  id: string;
  name: string;
  stack: string;
  split: string;
}
interface Experiment {
  id: string;
  status: string;
  createdAt: string;
  rollouts?: number;
  improvementStages?: number;
  candidateId?: string;
  error?: string;
  spec?: { mode?: string; agentId?: string; workspaceId?: string };
}
interface Packet {
  id: string;
  requestHash: string;
  stage: string;
  status: string;
  experimentId: string;
  mission?: string;
}
interface Revision {
  id: string;
  hash: string;
  parentId?: string;
  createdAt: string;
  authoredBy?: { identity: string };
}
interface Lesson {
  id: string;
  cue: string;
  guidance: string;
  status: string;
  evidence?: string[];
}
interface Evaluation {
  id: string;
  experimentId: string;
  status: string;
  reviewPacketId?: string;
  error?: string;
  recordedResults?: number;
  expectedResults?: number;
  gate?: HarnessCostGate & {
    eligible: boolean;
    reasons: string[];
    baselineSuccesses: number;
    candidateSuccesses: number;
    timeRatio: number;
    tokenRatio: number;
  };
}
interface Promotion {
  id: string;
  kind: string;
  agentId: string;
  workspaceId: string;
  revisionId: string;
  previousId: string;
  createdAt: string;
}
type Tab = "compare" | "optimize" | "lessons" | "revisions";

export default function HarnessWorkbench({
  state,
  action,
  workspaceId,
  compare,
}: PageProps & { compare: ReactNode }) {
  const [tab, setTab] = useState<Tab>("optimize");
  const [tasks, setTasks] = useState<Task[]>([]),
    [experiments, setExperiments] = useState<Experiment[]>([]),
    [packets, setPackets] = useState<Packet[]>([]),
    [lessons, setLessons] = useState<Lesson[]>([]),
    [revisions, setRevisions] = useState<Revision[]>([]),
    [evaluations, setEvaluations] = useState<Evaluation[]>([]),
    [promotions, setPromotions] = useState<Promotion[]>([]);
  const [agentId, setAgentId] = useState(""),
    [mode, setMode] = useState("native"),
    [contextSize, setContextSize] = useState(65536),
    [workflowIds, setWorkflowIds] = useState<string[]>([]),
    [taskIds, setTaskIds] = useState<string[]>([]);
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [detail, setDetail] = useState<{ title: string; value: RecordData }>();
  const [packet, setPacket] = useState<Packet>(),
    [response, setResponse] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  useEffect(() => {
    let valid = true;
    let running = false;
    async function load() {
      if (running) return;
      running = true;
      try {
        const [t, e, p, l, r, v, a] = await Promise.all([
          api<{ items: Task[] }>("harness.fixtures.list"),
          api<{ items: Experiment[] }>("harness.experiments.list"),
          api<{ items: Packet[] }>("harness.packets.list"),
          api<{ items: Lesson[] }>("harness.lessons.list"),
          api<{ items: Revision[] }>("harness.revisions.list"),
          api<{ items: Evaluation[] }>("harness.evaluations.list"),
          api<{ items: Promotion[] }>("harness.promotions.list"),
        ]);
        if (valid) {
          setTasks(t.items);
          setExperiments(e.items);
          setPackets(p.items);
          setLessons(l.items);
          setRevisions(r.items);
          setEvaluations(v.items);
          setPromotions(a.items);
          setError("");
        }
      } catch (e) {
        if (valid)
          setError(
            e instanceof Error ? e.message : "Could not load Harness Lab",
          );
      } finally {
        running = false;
      }
    }
    void load();
    const timer = window.setInterval(load, 5000);
    return () => {
      valid = false;
      window.clearInterval(timer);
    };
  }, [refreshKey]);
  async function inspect(method: string, id: string, title: string) {
    try {
      setDetail({ title, value: await api<RecordData>(method, { id }) });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not inspect record");
    }
  }
  async function mutate(method: string, params: unknown) {
    setBusy(true);
    try {
      const value = await action(method, params);
      if (value !== undefined) setRefreshKey((key) => key + 1);
      return value;
    } finally {
      setBusy(false);
    }
  }
  const pending = packets.filter((p) => p.status === "awaiting_codex");
  const experimentTarget = (e?: Experiment) =>
    e
      ? `${state.agents.find((a) => a.id === e.spec?.agentId)?.name || "Recorded agent"} · ${state.workspaces.find((w) => w.id === e.spec?.workspaceId)?.name || "Recorded workspace"}`
      : "Recorded experiment";
  return (
    <div className="page-scroll harness-workbench">
      <SectionHeader
        eyebrow="Improve with evidence"
        title="Harness Lab"
        description="Gemma solves the tasks. Codex proposes improvements and independently reviews the results."
        actions={
          <Button onClick={() => setRefreshKey((key) => key + 1)}>
            <RefreshCw size={15} />
            Refresh
          </Button>
        }
      />
      <div className="phase-tabs" role="tablist" aria-label="Harness Lab views">
        {(["compare", "optimize", "lessons", "revisions"] as const).map(
          (name) => (
            <button
              role="tab"
              aria-selected={tab === name}
              key={name}
              onClick={() => setTab(name)}
            >
              {name[0].toUpperCase() + name.slice(1)}
            </button>
          ),
        )}
      </div>
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
      {tab === "compare" && <div className="legacy-comparison">{compare}</div>}
      {tab === "optimize" && (
        <>
          <div className="lab-layout">
            <form
              className="panel form-stack"
              onSubmit={async (e) => {
                e.preventDefault();
                await mutate("harness.experiments.start", {
                  workspaceId,
                  agentId,
                  mode,
                  taskIds,
                  contextSize,
                  workflowIds,
                });
              }}
            >
              <div className="row-title">
                <FlaskConical size={20} />
                <h3>Start a coding experiment</h3>
              </div>
              <Field label="Baseline agent">
                <select
                  required
                  value={agentId}
                  onChange={(e) => setAgentId(e.target.value)}
                >
                  <option value="">Choose an agent</option>
                  {state.agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Implementation">
                <select value={mode} onChange={(e) => setMode(e.target.value)}>
                  <option value="native">Gemma Desktop · native</option>
                  <option value="upstream">
                    Pinned upstream Harness² core
                  </option>
                </select>
              </Field>
              <Field
                label="Requested context"
                hint="Frozen for this experiment. Personal model settings remain unchanged."
              >
                <select
                  value={contextSize}
                  onChange={(e) => setContextSize(Number(e.target.value))}
                >
                  <option value={32768}>32K tokens</option>
                  <option value={65536}>64K tokens · primary comparison</option>
                  <option value={131072}>128K tokens</option>
                </select>
              </Field>
              <fieldset className="capability-options">
                <legend>Development tasks</legend>
                {tasks
                  .filter((t) => t.split === "development")
                  .map((t) => (
                    <label className="checkbox-label" key={t.id}>
                      <input
                        type="checkbox"
                        aria-label={`${t.name} (${t.stack})`}
                        checked={taskIds.includes(t.id)}
                        onChange={(e) =>
                          setTaskIds(
                            e.target.checked
                              ? [...taskIds, t.id]
                              : taskIds.filter((id) => id !== t.id),
                          )
                        }
                      />
                      <span>
                        {t.name}
                        <small className="muted"> {t.stack}</small>
                      </span>
                    </label>
                  ))}
              </fieldset>
              {!!state.workflows.length && (
                <fieldset className="capability-options">
                  <legend>Approved workflow options</legend>
                  <p className="small muted">
                    Selected definitions are frozen now. Proposals may select
                    only these workflows and the agent’s permitted specialists.
                  </p>
                  {state.workflows.map((workflow) => (
                    <label className="checkbox-label" key={workflow.id}>
                      <input
                        type="checkbox"
                        aria-label={`Allow workflow ${workflow.name}`}
                        checked={workflowIds.includes(workflow.id)}
                        onChange={(event) =>
                          setWorkflowIds(
                            event.target.checked
                              ? [...workflowIds, workflow.id]
                              : workflowIds.filter((id) => id !== workflow.id),
                          )
                        }
                      />
                      <span>{workflow.name}</span>
                    </label>
                  ))}
                </fieldset>
              )}
              <p className="small muted">
                Each task uses four solver rollouts and three Codex improvement
                stages. Trials use private project copies and frozen conditions.
              </p>
              <Button
                variant="default"
                type="submit"
                loading={busy}
                disabled={!workspaceId || !agentId || !taskIds.length}
              >
                <Play size={15} />
                Freeze experiment
              </Button>
              {!workspaceId && (
                <p className="small muted">
                  Select a workspace to scope the experiment and its revisions.
                </p>
              )}
            </form>
            <section className="panel">
              <h3>Experiments</h3>
              {!experiments.length && (
                <Empty
                  icon={<FlaskConical size={26} />}
                  title="Establish a baseline"
                  description="Start with one development task per language. Qualification results remain separate from optimization feedback."
                />
              )}
              <div className="phase-records">
                {experiments
                  .slice()
                  .reverse()
                  .map((e) => (
                    <article key={e.id}>
                      <button
                        className="phase-record-title"
                        onClick={() =>
                          inspect("harness.experiments.get", e.id, "Experiment")
                        }
                      >
                        <strong>
                          {new Date(e.createdAt).toLocaleString()}
                        </strong>
                        <span className="tag">{friendlyStatus(e.status)}</span>
                      </button>
                      <p className="small muted">
                        {experimentTarget(e)}
                      </p>
                      <p className="small muted">
                        {e.spec?.mode || "Recorded mode"} · {e.rollouts ?? 0}{" "}
                        rollouts · {e.improvementStages ?? 0} improvement stages
                      </p>
                      {e.error && <p className="error-text">{e.error}</p>}
                      <div className="row-actions">
                        {!["completed", "cancelled", "failed"].includes(
                          e.status,
                        ) && (
                          <Button
                            size="small"
                            disabled={
                              busy ||
                              e.status === "running" ||
                              pending.some((p) => p.experimentId === e.id)
                            }
                            onClick={() =>
                              mutate("harness.experiments.advance", {
                                id: e.id,
                              })
                            }
                          >
                            {e.status === "ready"
                              ? "Run experiment"
                              : "Continue experiment"}
                          </Button>
                        )}
                        {!["completed", "cancelled", "failed"].includes(
                          e.status,
                        ) && (
                          <Button
                            size="small"
                            onClick={() =>
                              mutate("harness.experiments.cancel", { id: e.id })
                            }
                          >
                            <Square size={12} />
                            Stop
                          </Button>
                        )}
                        {e.candidateId &&
                          e.status === "completed" &&
                          !evaluations.some((v) => v.experimentId === e.id) && (
                            <Button
                              size="small"
                              disabled={busy}
                              onClick={() =>
                                mutate("harness.evaluations.start", {
                                  experimentId: e.id,
                                })
                              }
                            >
                              Qualify candidate
                            </Button>
                          )}
                      </div>
                      <Button
                        size="small"
                        onClick={async () => {
                          try {
                            const trials = await api<{ items: RecordData[] }>(
                              "harness.trials.list",
                            );
                            setDetail({
                              title: "Trial history",
                              value: {
                                trials: trials.items.filter(
                                  (t) => t.experimentId === e.id,
                                ),
                              },
                            });
                          } catch (err) {
                            setError(String(err));
                          }
                        }}
                      >
                        Inspect trials
                      </Button>
                      <Button
                        size="small"
                        disabled={busy || e.status === "running"}
                        onClick={async () => {
                          const result = await mutate(
                            "harness.experiments.exportArtifact",
                            { id: e.id },
                          );
                          if (result !== undefined)
                            setDetail({
                              title: "Exported experiment",
                              value: result as RecordData,
                            });
                        }}
                      >
                        Export evidence
                      </Button>
                    </article>
                  ))}
              </div>
            </section>
          </div>
          <section className="panel phase-packets">
            <div className="row-title">
              <BookOpen size={19} />
              <h3>Codex review queue</h3>
              <span className="tag">{pending.length} waiting</span>
            </div>
            {!pending.length && (
              <p className="muted">
                Proposal, composition, and independent-review packets appear
                here when they are ready.
              </p>
            )}
            <div className="phase-records">
              {pending.map((p) => (
                <article key={p.id}>
                  <strong>{friendlyStatus(p.stage)}</strong>
                  <p>
                    {p.mission ||
                      "Inspect the frozen request and supply the corresponding Codex response."}
                  </p>
                  <div className="row-actions">
                    <Button
                      size="small"
                      onClick={() =>
                        inspect(
                          "harness.packets.get",
                          p.id,
                          "Codex request packet",
                        )
                      }
                    >
                      Inspect request
                    </Button>
                    <Button
                      size="small"
                      onClick={async () => {
                        try {
                          const full = await api<Packet>(
                            "harness.packets.get",
                            { id: p.id },
                          );
                          setPacket(full);
                          setResponse("");
                        } catch (e) {
                          setError(String(e));
                        }
                      }}
                    >
                      Import response
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          </section>
          {!!evaluations.length && (
            <section className="panel phase-packets">
              <h3>Independent qualification</h3>
              <div className="phase-records">
                {evaluations.map((v) => (
                  <article key={v.id}>
                    <div className="row-title">
                      <strong>{v.id.slice(0, 12)}</strong>
                      <span className="tag">{friendlyStatus(v.status)}</span>
                    </div>
                    <p className="small muted">
                      {experimentTarget(experiments.find((e) => e.id === v.experimentId))}
                    </p>
                    {v.recordedResults !== undefined && (
                      <p className="small muted">
                        {v.recordedResults}{v.expectedResults !== undefined ? ` / ${v.expectedResults}` : ""} paired trials recorded, including failures
                      </p>
                    )}
                    {v.status === "completed" && v.gate && (
                      <>
                        <p>
                          Held-out successes: {v.gate.baselineSuccesses}{" "}
                          baseline · {v.gate.candidateSuccesses} candidate
                        </p>
                        <p className="small muted">
                          {hasQualifiedCostComparison(v.gate)
                            ? `Median elapsed ratio ${v.gate.timeRatio.toFixed(2)} · token ratio ${v.gate.tokenRatio.toFixed(2)}`
                            : costComparisonExplanation(v.gate)}
                        </p>
                        {v.gate.reasons?.map((reason) => (
                          <p key={reason} className="small muted">
                            {reason}
                          </p>
                        ))}
                      </>
                    )}
                    {v.error && <p className="error-text">{v.error}</p>}
                    <div className="row-actions">
                      <Button
                        size="small"
                        onClick={() =>
                          inspect(
                            "harness.evaluations.get",
                            v.id,
                            "Independent qualification",
                          )
                        }
                      >
                        Inspect evidence
                      </Button>
                      {v.status === "running" && (
                        <Button
                          size="small"
                          onClick={() =>
                            mutate("harness.evaluations.cancel", { id: v.id })
                          }
                        >
                          <Square size={12} />
                          Stop qualification
                        </Button>
                      )}
                      {["interrupted", "cancelled"].includes(v.status) && (
                        <Button
                          size="small"
                          disabled={busy}
                          onClick={() =>
                            mutate("harness.evaluations.resume", { id: v.id })
                          }
                        >
                          Resume qualification
                        </Button>
                      )}
                      {v.status === "completed" && v.gate?.eligible && hasQualifiedCostComparison(v.gate) && (
                        <Button
                          size="small"
                          disabled={busy}
                          onClick={() =>
                            mutate("harness.promote", { evaluationId: v.id })
                          }
                        >
                          Activate reviewed revision
                        </Button>
                      )}
                    </div>
                  </article>
                ))}
              </div>
            </section>
          )}
        </>
      )}
      {tab === "lessons" && (
        <div className="card-grid">
          {lessons.length ? (
            lessons.map((l) => (
              <article className="entity-card" key={l.id}>
                <div className="row-title">
                  <BookOpen size={20} />
                  <span className="tag">{friendlyStatus(l.status)}</span>
                </div>
                <h3>{l.cue}</h3>
                <p>{l.guidance}</p>
                <small>{l.evidence?.length || 0} evidence references</small>
                <Button
                  size="small"
                  onClick={() =>
                    inspect("harness.lessons.history", l.id, "Lesson lineage")
                  }
                >
                  Inspect lineage
                </Button>
                <div className="row-actions">
                  {["provisional", "accepted", "rejected"]
                    .filter((status) => status !== l.status)
                    .map((status) => (
                      <Button
                        key={status}
                        size="small"
                        disabled={busy}
                        onClick={() =>
                          mutate("harness.lessons.setStatus", {
                            id: l.id,
                            status,
                          })
                        }
                      >
                        Mark {status}
                      </Button>
                    ))}
                </div>
              </article>
            ))
          ) : (
            <Empty
              icon={<BookOpen size={28} />}
              title="Lessons need evidence"
              description="Experiment lessons retain their scope and supporting trials. They do not automatically become global instructions."
            />
          )}
        </div>
      )}
      {tab === "revisions" && (
        <div className="phase-records panel">
          {!!promotions.length && (
            <section>
              <h3>Activation history</h3>
              {promotions.map((p) => (
                <article key={p.id}>
                  <div className="row-title">
                    <strong>{friendlyStatus(p.kind)}</strong>
                    <span className="small muted">
                      {new Date(p.createdAt).toLocaleString()}
                    </span>
                  </div>
                  <p className="small">
                    {state.agents.find((a) => a.id === p.agentId)?.name ||
                      p.agentId}{" "}
                    ·{" "}
                    {state.workspaces.find((w) => w.id === p.workspaceId)
                      ?.name || p.workspaceId}
                  </p>
                  <code>{p.revisionId.slice(0, 16)}</code>
                  <div className="row-actions">
                    <Button
                      size="small"
                      onClick={() =>
                        inspect(
                          "harness.promotions.get",
                          p.id,
                          "Activation record",
                        )
                      }
                    >
                      Inspect activation
                    </Button>
                    {p.kind === "promote" &&
                      !promotions.some(
                        (r) =>
                          r.kind === "rollback" &&
                          r.previousId === p.revisionId,
                      ) && (
                        <Button
                          size="small"
                          disabled={busy}
                          onClick={() =>
                            mutate("harness.rollback", { id: p.id })
                          }
                        >
                          Restore previous revision
                        </Button>
                      )}
                  </div>
                </article>
              ))}
            </section>
          )}
          {revisions.length ? (
            revisions
              .slice()
              .reverse()
              .map((r) => (
                <article key={r.id}>
                  <div className="row-title">
                    <GitBranch size={18} />
                    <strong>{r.id.slice(0, 12)}</strong>
                  </div>
                  <p className="small muted">
                    {r.authoredBy?.identity} ·{" "}
                    {new Date(r.createdAt).toLocaleString()}
                  </p>
                  <code className="break-all">{r.hash}</code>
                  <div className="row-actions">
                    <Button
                      size="small"
                      onClick={() =>
                        inspect(
                          "harness.revisions.get",
                          r.id,
                          "Immutable harness revision",
                        )
                      }
                    >
                      Inspect revision
                    </Button>
                  </div>
                </article>
              ))
          ) : (
            <Empty
              icon={<GitBranch size={28} />}
              title="Every change has a revision"
              description="Baseline, candidate, activation, and rollback records preserve the conditions behind each decision."
            />
          )}
        </div>
      )}
      <Modal
        open={!!detail}
        onClose={() => setDetail(undefined)}
        title={detail?.title || "Details"}
        wide
      >
        {detail?.title === "Exported experiment" && (
          <Button
            onClick={() => action("artifact.reveal", { id: detail.value.id })}
          >
            Locate export file
          </Button>
        )}
        {detail?.title === "Trial history" &&
          Array.isArray(detail.value.trials) && (
            <div className="phase-records">
              {(detail.value.trials as RecordData[]).map((trial) => (
                <Button
                  key={String(trial.id)}
                  size="small"
                  onClick={() =>
                    inspect(
                      "harness.trials.get",
                      String(trial.id),
                      "Trial trace",
                    )
                  }
                >
                  {String(trial.taskId)} · {String(trial.label)} ·{" "}
                  {String(trial.status)}
                </Button>
              ))}
            </div>
          )}
        <pre className="json-view">
          {JSON.stringify(detail?.value, null, 2)}
        </pre>
        <Button
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(
                JSON.stringify(detail?.value, null, 2),
              );
            } catch {
              setError(
                "Clipboard unavailable. Select and copy the record text.",
              );
            }
          }}
        >
          <Copy size={14} />
          Copy record
        </Button>
      </Modal>
      <Modal
        open={!!packet}
        onClose={() => setPacket(undefined)}
        title="Import Codex response"
        description="The packet identity and input hash must match. Proposal and independent-review authors are recorded separately."
        wide
      >
        <form
          className="form-stack"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!packet) return;
            try {
              const value = parsePacketResponse(response, packet);
              if (
                (await mutate("harness.packets.submit", value)) !== undefined
              ) {
                setPacket(undefined);
                setResponse("");
              }
            } catch (e) {
              setError(e instanceof Error ? e.message : String(e));
            }
          }}
        >
          <Field label="Response JSON">
            <textarea
              rows={15}
              className="mono"
              value={response}
              onChange={(e) => setResponse(e.target.value)}
              required
              spellCheck={false}
            />
          </Field>
          <p className="small mono break-all">
            Packet {packet?.id} · {packet?.requestHash}
          </p>
          {error && (
            <p className="error-text" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" loading={busy} variant="default">
            Validate and import
          </Button>
        </form>
      </Modal>
    </div>
  );
}
