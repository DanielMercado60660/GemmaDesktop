import { useEffect, useState } from "react";
import { Play, Square, RefreshCw, Copy, Download } from "lucide-react";
import type { PageProps } from "../Management";
import { api } from "../bridge";
import { completionModels } from "../lib/readiness";
import { isCompletionModel } from "../lib/modelCapabilities";
import { providerName } from "../lib/managedRuntime";
import {
  standardProfileV2,
  runMethodV2,
  evidenceByArm,
  type ArmV2,
  type FixturePackV2,
  type RunV2,
  type BatchV2,
} from "../lib/harnessV2";
import { Button, Field, Modal, friendlyStatus } from "./ui";
import { useConfirmation } from "./Confirmation";
import { HarnessControlSummary } from "./HarnessControls";

interface PromotionV2 {
  id: string;
  workspaceId: string;
  agentId: string;
  armId: string;
  status: string;
  active?: boolean;
  createdAt: string;
}

type Selection = { id: string; kind: RunV2["kind"] };
export default function ReliabilityWorkbench({
  state,
  action,
  workspaceId,
}: PageProps) {
  const confirm = useConfirmation();
  const [packs, setPacks] = useState<FixturePackV2[]>([]),
    [arms, setArms] = useState<ArmV2[]>([]),
    [runs, setRuns] = useState<RunV2[]>([]);
  const [promotions, setPromotions] = useState<PromotionV2[]>([]);
  const [purpose, setPurpose] = useState("screen");
  const [selected, setSelected] = useState<Selection>(),
    [detail, setDetail] = useState<RunV2>(),
    [batches, setBatches] = useState<BatchV2[]>([]);
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [refresh, setRefresh] = useState(0);
  const [agentId, setAgentId] = useState(""),
    [runtimeId, setRuntimeId] = useState(
      state.settings.defaultCompletionRuntimeId || "ollama-default",
    ),
    [model, setModel] = useState(""),
    [name, setName] = useState(""),
    [revisionId, setRevisionId] = useState(""),
    [provenance, setProvenance] = useState("development-screen");
  const [chosenArms, setChosenArms] = useState<string[]>([]),
    [baseline, setBaseline] = useState(""),
    [candidate, setCandidate] = useState("");
  const [artifact, setArtifact] = useState<{
    id: string;
    path: string;
    size: number;
    sha256: string;
  }>();
  const [freezePreview, setFreezePreview] = useState<Record<string, unknown>>(),
    [exported, setExported] = useState<{ title: string; json: string }>(),
    [review, setReview] = useState("");
  useEffect(() => {
    let valid = true;
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const [p, a, d, q, promotions] = await Promise.all([
          api<{ items: FixturePackV2[] }>("harness.packs.list"),
          api<{ items: ArmV2[] }>("harness.arms.list"),
          api<{ items: RunV2[] }>("harness.diagnostics.list"),
          api<{ items: RunV2[] }>("harness.evaluationsV2.list"),
          api<{ items: PromotionV2[] }>("harness.promotionsV2.list"),
        ]);
        if (!valid) return;
        setPacks(p.items);
        setPromotions(promotions.items);
        setArms(a.items);
        setRuns(
          [...d.items, ...q.items].sort((a, b) =>
            b.createdAt.localeCompare(a.createdAt),
          ),
        );
        setError("");
        if (selected) {
          const [record, b] = await Promise.all([
            api<RunV2>(runMethodV2(selected.kind, "get"), { id: selected.id }),
            api<{ items: BatchV2[] }>("harness.batchesV2.list", {
              runId: selected.id,
            }),
          ]);
          if (valid) {
            setDetail(record);
            setBatches(b.items);
          }
        }
      } catch (error) {
        if (valid) setError((error as Error).message);
      } finally {
        if (valid) timer = setTimeout(load, document.hidden ? 30000 : 5000);
      }
    }
    void load();
    return () => {
      valid = false;
      clearTimeout(timer);
    };
  }, [refresh, selected?.id, selected?.kind]);
  async function mutate<T>(
    method: string,
    input: unknown,
  ): Promise<T | undefined> {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await action<T>(method, input);
      if (result !== undefined) setRefresh((value) => value + 1);
      return result;
    } finally {
      setBusy(false);
    }
  }
  function select(run: RunV2) {
    setSelected({ id: run.id, kind: run.kind });
    setDetail(undefined);
    setBatches([]);
    setReview("");
  }
  const diagnosticPack = packs.find(
    (pack) => pack.id === "coding-diagnostics-v2",
  );
  const qualificationPack = packs.find(
    (pack) => pack.id === "coding-qualification-v2",
  );
  const tasks =
    diagnosticPack?.tasks.filter((task) => task.split === "development") || [];
  const availableArms = arms.filter(
    (arm) => arm.spec.workspaceId === workspaceId,
  );
  const models = completionModels(state, runtimeId).filter(isCompletionModel);
  const modelName = models.some((item) => item.name === model)
    ? model
    : models[0]?.name || "";
  const selectedAgent = state.agents.find((agent) => agent.id === agentId);
  const lastBatch = [...batches].sort((a, b) =>
    b.startedAt.localeCompare(a.startedAt),
  )[0];
  return (
    <div className="form-stack reliability-workbench">
      <div className="row-title">
        <h3>Coding reliability</h3>
        <Button size="small" onClick={() => setRefresh((value) => value + 1)}>
          <RefreshCw size={14} />
          Refresh evidence
        </Button>
      </div>
      <p className="small muted">
        Freeze complete configurations, screen them on development tasks, then
        qualify a candidate on fresh validation and held-out tasks. Each batch
        stops for review within two hours.
      </p>
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
      {state.managedRuntime?.inferenceBlocked && (
        <p className="notice" role="status">
          Profiles can be frozen, but inference is blocked:{" "}
          {state.managedRuntime.inferenceBlocked}
        </p>
      )}
      <form
        className="panel form-stack"
        onSubmit={(event) => {
          event.preventDefault();
          if (purpose !== "product" && provenance === "product:frozen") {
            setError(
              "Choose Current product baseline as the profile source to freeze the actual product configuration.",
            );
            return;
          }
          setFreezePreview({
            ...(purpose === "product"
              ? {}
              : { ...standardProfileV2, model: modelName, runtimeId }),
            workspaceId,
            agentId,
            name:
              name.trim() ||
              (purpose === "product"
                ? `${selectedAgent?.name} · current product`
                : `${modelName} · standard profile`),
            provenance: purpose === "product" ? "product:frozen" : provenance,
            fixturePackId: diagnosticPack?.id,
            taskIds: tasks.map((task) => task.id),
            ...(purpose !== "product" && revisionId.trim()
              ? { revisionId: revisionId.trim() }
              : {}),
          });
        }}
      >
        <h3>Freeze an arm</h3>
        <Field label="Profile source">
          <select
            value={purpose}
            onChange={(event) => setPurpose(event.target.value)}
          >
            <option value="screen">Standard development profile</option>
            <option value="product">Current product baseline</option>
          </select>
        </Field>
        <div className="form-grid">
          <Field label="Agent to freeze">
            <select
              required
              value={agentId}
              onChange={(event) => setAgentId(event.target.value)}
            >
              <option value="">Choose an agent</option>
              {state.agents.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agent.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Runtime for this arm">
            <select
              disabled={purpose === "product"}
              value={runtimeId}
              onChange={(event) => {
                setRuntimeId(event.target.value);
                setModel("");
              }}
            >
              <option value="ollama-default">External Ollama</option>
              {state.managedRuntime?.catalog
                .filter((build) =>
                  state.managedRuntime?.installed.some(
                    (item) => item.id === build.id,
                  ),
                )
                .map((build) => (
                  <option key={build.id} value={build.id}>
                    {providerName(build.provider)} · {build.id}
                  </option>
                ))}
            </select>
          </Field>
          <Field label="Model for this arm">
            <select
              required
              disabled={purpose === "product"}
              value={modelName}
              onChange={(event) => setModel(event.target.value)}
            >
              {!models.length && (
                <option value="">Install a compatible model in Models</option>
              )}
              {models.map((model) => (
                <option key={model.name}>{model.name}</option>
              ))}
            </select>
          </Field>
          <Field label="Arm name">
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={
                purpose === "product"
                  ? "Current product baseline"
                  : `${modelName || "Model"} · standard profile`
              }
              maxLength={200}
            />
          </Field>
          <Field
            label="Provenance"
            hint="Development screen label or the completed development experiment ID."
          >
            <input
              required
              disabled={purpose === "product"}
              value={purpose === "product" ? "product:frozen" : provenance}
              onChange={(event) => setProvenance(event.target.value)}
              maxLength={1000}
            />
          </Field>
          <Field
            label="Existing revision ID (optional)"
            hint="Leave empty to freeze this agent’s current instructions and approved skills."
          >
            <input
              disabled={purpose === "product"}
              value={revisionId}
              onChange={(event) => setRevisionId(event.target.value)}
            />
          </Field>
        </div>
        <p className="notice">
          {purpose === "product"
            ? "The host freezes the actual current profile for this workspace and agent, including any active qualified promotion. Screen overrides are not applied."
            : "Standard screen: 65,536 context tokens · 16,384 response tokens · 80 steps · temperature 1 · thinking on · 20-minute task limit. This profile applies only to this frozen arm."}
        </p>
        <p className="small muted">
          {selectedAgent?.name || "The agent"} must have an explicit Docker
          capability profile. The host verifies runtime, model, source,
          executor, tools and fixture identities.
        </p>
        <Button
          type="submit"
          disabled={
            busy ||
            !workspaceId ||
            !agentId ||
            (purpose !== "product" && !modelName) ||
            !diagnosticPack
          }
          variant="default"
        >
          Review frozen profile
        </Button>
      </form>
      <div className="lab-layout">
        <section className="panel form-stack">
          <h3>Development screen</h3>
          <p className="small muted">
            Select three frozen arms for six development tasks, two repeats
            each: 36 scheduled attempts. Creating the schedule runs no
            inference.
          </p>
          <fieldset className="capability-options">
            <legend>Frozen arms</legend>
            {availableArms.map((arm) => (
              <label className="checkbox-label" key={arm.id}>
                <input
                  type="checkbox"
                  checked={chosenArms.includes(arm.id)}
                  disabled={
                    !chosenArms.includes(arm.id) && chosenArms.length >= 3
                  }
                  onChange={(event) =>
                    setChosenArms(
                      event.target.checked
                        ? [...chosenArms, arm.id]
                        : chosenArms.filter((id) => id !== arm.id),
                    )
                  }
                />
                <span>
                  {arm.name}
                  <small className="muted">
                    {" "}
                    · {arm.spec.model} · {arm.hash.slice(0, 10)}
                  </small>
                </span>
              </label>
            ))}
          </fieldset>
          <p className="small muted">
            {tasks.map((task) => task.name).join(" · ") ||
              "Loading fixture pack…"}
          </p>
          <Button
            disabled={busy || chosenArms.length !== 3 || tasks.length !== 6}
            onClick={async () => {
              if (
                !diagnosticPack ||
                !(await confirm({
                  title: "Create development screen?",
                  description:
                    "Freeze six development tasks × three arms × two repeats (36 attempts). You will start each batch separately; no task runs now.",
                  confirmLabel: "Create schedule",
                }))
              )
                return;
              const run = await mutate<RunV2>("harness.diagnostics.create", {
                packId: diagnosticPack.id,
                taskIds: tasks.map((task) => task.id),
                armIds: chosenArms,
                repeats: 2,
              });
              if (run) select(run);
            }}
          >
            Create diagnostic schedule
          </Button>
        </section>
        <section className="panel form-stack">
          <h3>Fresh qualification</h3>
          <p className="small muted">
            Paired baseline and candidate attempts, three repeats per validation
            and held-out task. The host prevents reused held-out material and
            requires independent review.
          </p>
          <Field label="Frozen baseline">
            <select
              value={baseline}
              onChange={(event) => setBaseline(event.target.value)}
            >
              <option value="">Choose current-product baseline</option>
              {availableArms
                .filter((arm) => arm.provenance === "product:frozen")
                .map((arm) => (
                  <option key={arm.id} value={arm.id}>
                    {arm.name}
                  </option>
                ))}
            </select>
          </Field>
          <Field label="Frozen candidate">
            <select
              value={candidate}
              onChange={(event) => setCandidate(event.target.value)}
            >
              <option value="">Choose candidate arm</option>
              {availableArms.map((arm) => (
                <option key={arm.id} value={arm.id}>
                  {arm.name}
                </option>
              ))}
            </select>
          </Field>
          <Button
            disabled={
              busy ||
              !qualificationPack ||
              !baseline ||
              !candidate ||
              baseline === candidate
            }
            onClick={async () => {
              if (
                !qualificationPack ||
                !(await confirm({
                  title: "Reserve fresh qualification?",
                  description: `Reserve ${qualificationPack.tasks.filter((task) => task.split !== "development").length * 6} paired attempts on fresh validation and held-out tasks. This records exposure ownership. You will start each two-attempt batch separately.`,
                  confirmLabel: "Create qualification",
                }))
              )
                return;
              const run = await mutate<RunV2>("harness.evaluationsV2.create", {
                packId: qualificationPack.id,
                baselineArmId: baseline,
                candidateArmId: candidate,
              });
              if (run) select(run);
            }}
          >
            Create qualification schedule
          </Button>
        </section>
      </div>
      <section className="panel form-stack">
        <h3>Schedules and checkpoints</h3>
        {!runs.length && <p className="muted">No versioned schedules yet.</p>}
        <div className="experiment-list">
          {runs.map((run) => (
            <button
              className="row-title"
              key={run.id}
              onClick={() => select(run)}
              aria-pressed={selected?.id === run.id}
            >
              <strong>
                {run.kind === "diagnostic"
                  ? "Development screen"
                  : "Qualification"}{" "}
                · {run.id.slice(0, 8)}
              </strong>
              <span>
                {friendlyStatus(run.status)} ·{" "}
                {run.recordedResults ?? run.cursor}/
                {run.expectedResults ?? run.schedule?.length ?? "—"} attempts
              </span>
            </button>
          ))}
        </div>
      </section>
      {detail && (
        <section
          className="panel form-stack"
          aria-label="Versioned run details"
        >
          <div className="row-title">
            <h3>
              {detail.kind === "diagnostic"
                ? "Development screen"
                : "Qualification"}{" "}
              details
            </h3>
            <span className="tag" role="status">
              {friendlyStatus(detail.status)} · {detail.results?.length || 0}/
              {detail.schedule?.length || 0} recorded
            </span>
          </div>
          <p className="small muted">
            Pack {detail.packId} · schedule {detail.scheduleHash.slice(0, 12)} ·
            checkpoint cursor {detail.cursor}
          </p>
          {lastBatch && (
            <p role="status">
              Last batch: {friendlyStatus(lastBatch.status)} ·{" "}
              {lastBatch.stopReason || "In progress"} · attempts{" "}
              {lastBatch.startCursor}–{lastBatch.nextCursor}.
            </p>
          )}
          {detail.error && (
            <p className="inline-error" role="alert">
              {detail.error}
            </p>
          )}
          <div className="row-actions">
            {detail.status === "running" ? (
              <Button
                disabled={busy}
                onClick={() =>
                  void mutate(runMethodV2(detail.kind, "cancel"), {
                    id: detail.id,
                  })
                }
              >
                <Square size={14} />
                Cancel batch
              </Button>
            ) : (
              ["ready", "checkpointed", "interrupted", "cancelled"].includes(
                detail.status,
              ) && (
                <Button
                  disabled={busy || lastBatch?.status === "running"}
                  variant="default"
                  onClick={async () => {
                    const maxAttempts = detail.kind === "qualification" ? 2 : 3;
                    if (
                      await confirm({
                        title:
                          detail.status === "cancelled"
                            ? "Resume cancelled schedule?"
                            : "Start next bounded batch?",
                        description: `Run at most ${maxAttempts} ${detail.kind === "qualification" ? "paired " : ""}attempts within two hours, then checkpoint for review. No further batch starts automatically.`,
                        confirmLabel:
                          detail.status === "cancelled"
                            ? "Resume batch"
                            : "Start batch",
                      })
                    )
                      await mutate(
                        runMethodV2(
                          detail.kind,
                          detail.status === "cancelled" ? "resume" : "advance",
                        ),
                        {
                          id: detail.id,
                          wallSeconds: 7200,
                          maxAttempts,
                        },
                      );
                  }}
                >
                  <Play size={14} />
                  {detail.status === "cancelled"
                    ? "Review resume"
                    : "Review next batch"}
                </Button>
              )
            )}
            <Button
              disabled={busy || detail.status === "running"}
              onClick={async () => {
                const value = await mutate<{
                  id: string;
                  path: string;
                  size: number;
                  sha256: string;
                }>(runMethodV2(detail.kind, "exportArtifact"), {
                  id: detail.id,
                });
                if (value) setArtifact(value);
              }}
            >
              <Download size={14} />
              Save evidence export
            </Button>
          </div>
          {detail.status === "cancelled" && lastBatch?.status === "running" && (
            <p role="status">
              Waiting for cancelled work to finish its checkpoint before
              resuming.
            </p>
          )}
          {artifact && (
            <div className="notice" role="status">
              <span>
                Evidence export saved to <code>{artifact.path}</code> · SHA-256{" "}
                {artifact.sha256.slice(0, 12)}
              </span>
              <Button
                size="small"
                onClick={() =>
                  void action("artifact.reveal", { id: artifact.id })
                }
              >
                Reveal export
              </Button>
            </div>
          )}
          <div className="table-wrap">
            <table>
              <caption>Observed results and measurement coverage</caption>
              <thead>
                <tr>
                  <th>Arm</th>
                  <th>Successes</th>
                  <th>Complete accounting</th>
                  <th>Mean tokens</th>
                  <th>Mean elapsed</th>
                  <th>Evidence issues</th>
                </tr>
              </thead>
              <tbody>
                {evidenceByArm(detail).map((row) => (
                  <tr key={row.id}>
                    <td>
                      {arms.find((arm) => arm.id === row.id)?.name ||
                        row.id.slice(0, 10)}
                    </td>
                    <td>
                      {row.success}/{row.total}
                    </td>
                    <td>
                      {row.accounted}/{row.total}
                    </td>
                    <td>
                      {row.meanTokens === undefined
                        ? "Not reported"
                        : Math.round(row.meanTokens).toLocaleString()}
                    </td>
                    <td>
                      {row.meanSeconds === undefined
                        ? "Not reported"
                        : `${row.meanSeconds.toFixed(1)} s`}
                    </td>
                    <td>{row.evidenceIssues}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="small muted">
            Success, evidence integrity and usage coverage are separate. Means
            use only attempts with complete positive usage and elapsed
            measurements; missing measurements are not zero. Cost does not
            override the reliability gate.
          </p>
          {detail.kind === "qualification" && (
            <>
              <h4>Qualification gate</h4>
              <p role="status">
                {detail.gate.eligible
                  ? "Gate satisfied; independent approval required before activation."
                  : "Gate not satisfied."}
              </p>
              <ul>
                {detail.gate.reasons?.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
              <p>
                Held-out successes: baseline {detail.gate.baselineSuccesses} ·
                candidate {detail.gate.candidateSuccesses}
              </p>
              {detail.review ? (
                <p>
                  Independent review: {detail.review.verdict} ·{" "}
                  {detail.review.author.identity}
                </p>
              ) : (
                <>
                  <Field
                    label="Independent review JSON"
                    hint={`Paste the independent Codex review with author, verdict, evidenceHash and notes. Current evidence hash: ${detail.gate.evidenceHash || "available after completion"}`}
                  >
                    <textarea
                      rows={5}
                      value={review}
                      onChange={(event) => setReview(event.target.value)}
                    />
                  </Field>
                  <Button
                    disabled={
                      busy || detail.status !== "completed" || !review.trim()
                    }
                    onClick={async () => {
                      try {
                        const value = JSON.parse(review);
                        await mutate("harness.evaluationsV2.review", {
                          id: detail.id,
                          review: value,
                        });
                      } catch (error) {
                        setError((error as Error).message);
                      }
                    }}
                  >
                    Record independent review
                  </Button>
                </>
              )}
              <Button
                disabled={
                  busy ||
                  detail.status !== "completed" ||
                  !detail.gate.eligible ||
                  detail.review?.verdict !== "approved" ||
                  detail.review.evidenceHash !== detail.gate.evidenceHash
                }
                onClick={async () => {
                  const arm = await mutate<ArmV2>(
                    "harness.evaluationsV2.qualifiedArm",
                    { id: detail.id },
                  );
                  if (arm)
                    setExported({
                      title: "Qualified frozen arm",
                      json: JSON.stringify(arm, null, 2),
                    });
                }}
              >
                Inspect qualified arm
              </Button>
              <Button
                variant="default"
                disabled={
                  busy ||
                  detail.status !== "completed" ||
                  !detail.gate.eligible ||
                  detail.review?.verdict !== "approved" ||
                  detail.review.evidenceHash !== detail.gate.evidenceHash
                }
                onClick={async () => {
                  const arm = arms.find((item) => item.id === detail.armIds[1]);
                  if (!arm) return;
                  const workspace =
                    state.workspaces.find(
                      (item) => item.id === arm.spec.workspaceId,
                    )?.name || arm.spec.workspaceId;
                  const agent =
                    state.agents.find((item) => item.id === arm.spec.agentId)
                      ?.name || arm.spec.agentId;
                  if (
                    await confirm({
                      title: "Activate qualified workspace profile?",
                      description: `Activate ${arm.name} for ${agent} in ${workspace}, including runtime, model, generation controls and harness. Personal defaults stay unchanged.`,
                      confirmLabel: "Activate qualified profile",
                    })
                  )
                    await mutate("harness.evaluationsV2.promote", {
                      id: detail.id,
                    });
                }}
              >
                Activate qualified profile
              </Button>
            </>
          )}
        </section>
      )}
      {!!promotions.filter((item) => item.workspaceId === workspaceId)
        .length && (
        <section className="panel form-stack">
          <h3>Workspace profile activation</h3>
          {promotions
            .filter((item) => item.workspaceId === workspaceId)
            .map((item) => (
              <div className="row-title" key={item.id}>
                <span>
                  {arms.find((arm) => arm.id === item.armId)?.name ||
                    item.armId.slice(0, 12)}{" "}
                  · {item.active ? "Active" : friendlyStatus(item.status)}
                </span>
                <Button
                  disabled={busy || !item.active}
                  onClick={async () => {
                    if (
                      await confirm({
                        title: "Restore previous workspace profile?",
                        description:
                          "Restore the complete previous runtime, model, generation and harness profile for this workspace and agent. Personal defaults stay unchanged.",
                        confirmLabel: "Restore previous profile",
                      })
                    )
                      await mutate("harness.promotionsV2.rollback", {
                        id: item.id,
                      });
                  }}
                >
                  Restore previous profile
                </Button>
              </div>
            ))}
        </section>
      )}
      <Modal
        open={!!freezePreview}
        onClose={() => !busy && setFreezePreview(undefined)}
        title="Review frozen arm"
        description="The host freezes verified runtime and model metadata. Freezing does not start a managed runtime or run any fixture task."
      >
        {freezePreview && (
          <div className="form-stack">
            <p>
              {String(freezePreview.name)} · {selectedAgent?.name}
              {freezePreview.provenance !== "product:frozen" &&
                ` · ${freezePreview.runtimeId} · ${freezePreview.model}`}
            </p>
            <p>
              {freezePreview.provenance === "product:frozen"
                ? "Freeze the actual active product profile for this workspace and agent. The host resolves its runtime, model, context, generation controls and approved revision without applying screen overrides."
                : "65,536 context · 16,384 response · 80 steps · temperature 1 · thinking on · 1,200 seconds per attempt."}
            </p>
            <p className="small muted">
              Instructions, tools, skills, runtime identity and executor are
              frozen. Personal defaults stay unchanged.
            </p>
            {freezePreview.provenance !== "product:frozen" && (
              <HarnessControlSummary agent={selectedAgent} />
            )}
            <div className="modal-actions">
              <Button
                disabled={busy}
                onClick={() => setFreezePreview(undefined)}
              >
                Cancel
              </Button>
              <Button
                variant="default"
                loading={busy}
                onClick={async () => {
                  const result = await mutate<ArmV2>(
                    "harness.arms.create",
                    freezePreview,
                  );
                  if (result) setFreezePreview(undefined);
                }}
              >
                Freeze arm
              </Button>
            </div>
          </div>
        )}
      </Modal>
      <Modal
        open={!!exported}
        onClose={() => setExported(undefined)}
        title={exported?.title || "Evidence export"}
        description="Review before sharing. This local export includes exact public artifact bytes and excludes private grader sources."
      >
        {exported && (
          <div className="form-stack">
            <textarea
              aria-label="Evidence JSON preview"
              readOnly
              rows={12}
              value={exported.json.slice(0, 24000)}
            />
            {exported.json.length > 24000 && (
              <p className="small muted">
                Preview limited to 24,000 characters. Copy includes the full
                export.
              </p>
            )}
            <Button
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(exported.json);
                } catch (error) {
                  setError((error as Error).message);
                }
              }}
            >
              <Copy size={14} />
              Copy full JSON
            </Button>
          </div>
        )}
      </Modal>
    </div>
  );
}
