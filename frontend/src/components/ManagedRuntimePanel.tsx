import { useEffect, useState } from "react";
import { Download, Play, Square, RotateCcw, Trash2 } from "lucide-react";
import type { Action } from "../Management";
import type { State } from "../types";
import { api } from "../bridge";
import { Button, Field, Modal, formatBytes, friendlyStatus } from "./ui";
import { useConfirmation } from "./Confirmation";
import {
  managedJobFinished,
  providerName,
  qualifiedModel,
  type RuntimeManifest,
  type ManagedModelManifest,
  type ManagedInstallPreview,
} from "../lib/managedRuntime";

export default function ManagedRuntimePanel({
  state,
  action,
  models = true,
  workspaceId = "",
}: {
  state: State;
  action: Action;
  models?: boolean;
  workspaceId?: string;
}) {
  const managed = state.managedRuntime;
  const processActive = [
    "starting",
    "checking",
    "running",
    "stopping",
  ].includes(managed?.process.status || "");
  const confirm = useConfirmation();
  const [busy, setBusy] = useState("");
  const [preview, updatePreview] = useState<
    | { kind: "runtime"; item: RuntimeManifest }
    | { kind: "model"; item: ManagedModelManifest }
  >();
  const previewKey = preview ? `${preview.kind}:${preview.item.id}` : "";
  const [previewAttempt, setPreviewAttempt] = useState(0);
  const [previewResult, setPreviewResult] = useState<{
    key: string;
    data?: ManagedInstallPreview;
    error?: string;
  }>();
  function setPreview(value: typeof preview) {
    setPreviewResult(undefined);
    updatePreview(value);
  }
  const installPreview =
    previewResult?.key === previewKey ? previewResult.data : undefined;
  const previewError =
    previewResult?.key === previewKey ? previewResult.error : undefined;
  useEffect(() => {
    if (!preview) return;
    let valid = true;
    const key = previewKey;
    setPreviewResult({ key });
    void api<ManagedInstallPreview>(
      "runtime.managed.preview",
      preview.kind === "runtime"
        ? { buildId: preview.item.id }
        : { modelId: preview.item.id },
    )
      .then((data) => {
        if (
          data.target !== preview.item.id ||
          data.kind !==
            (preview.kind === "runtime" ? "runtime_install" : "model_download")
        )
          throw new Error(
            "Installation preview did not match the selected package.",
          );
        if (valid) setPreviewResult({ key, data });
      })
      .catch((error) => {
        if (valid)
          setPreviewResult({
            key,
            error: error instanceof Error ? error.message : String(error),
          });
      });
    return () => {
      valid = false;
    };
  }, [previewKey, previewAttempt]);
  const [modelChoices, setModelChoices] = useState<Record<string, string>>({});
  const [contexts, setContexts] = useState<Record<string, number>>({});
  async function perform(method: string, params: unknown = {}) {
    if (busy) return;
    setBusy(method);
    try {
      return await action(method, params);
    } finally {
      setBusy("");
    }
  }
  const installing = (target: string) =>
    managed?.jobs.some(
      (job) => job.target === target && !managedJobFinished(job.status),
    );
  return (
    <section className="managed-runtime-section" aria-label="App-managed MLX">
      <div className="list-heading">
        <h2>App-managed MLX</h2>
        <span>Experimental coding quality</span>
      </div>
      <p className="small muted">
        Gemma Desktop installs and manages these local runtimes. Updates and
        model downloads start only when you choose them. Ollama is managed
        separately.
      </p>
      {!managed?.catalog.length ? (
        <p className="notice" role="status">
          No MLX runtime bundle is available in this build. You can continue
          using your external Ollama installation.
        </p>
      ) : (
        <div className="model-list">
          {managed.catalog.map((build) => {
            const installed = managed.installed.some(
              (item) => item.id === build.id,
            );
            const active =
              managed.selections[build.provider]?.active === build.id;
            const previous = managed.selections[build.provider]?.previous;
            const availableModels = managed.modelCatalog.filter(
              (model) =>
                (!model.providers.length ||
                  model.providers.includes(build.provider)) &&
                managed.models.some((item) => item.id === model.id),
            );
            const selectedModel =
              availableModels.find(
                (model) => model.id === modelChoices[build.id],
              ) ||
              availableModels.find(
                (model) => model.id === managed.process.modelId,
              ) ||
              availableModels[0];
            const ownsProcess =
              managed.process.buildId === build.id &&
              ["starting", "checking", "running", "stopping"].includes(
                managed.process.status,
              );
            const activeJob = installing(build.id);
            const platform = managed.platformSupport?.[build.id];
            const supported = !platform || platform === "supported";
            const profileContext = state.effectiveProfiles?.find(
              (profile) =>
                profile.workspaceId === workspaceId &&
                profile.runtimeId === build.id &&
                profile.model === selectedModel?.serveName,
            )?.contextSize;
            const contextTokens =
              contexts[build.id] ??
              profileContext ??
              state.settings.contextSize;
            const contextFits =
              !!selectedModel?.model.contextTokens &&
              Number.isInteger(contextTokens) &&
              contextTokens >= 1024 &&
              contextTokens <= selectedModel.model.contextTokens;
            const canCheck =
              installed &&
              supported &&
              contextFits &&
              !!selectedModel &&
              !processActive &&
              !managed.inferenceBlocked;
            const checked =
              !!selectedModel &&
              qualifiedModel(
                managed,
                build.id,
                selectedModel.id,
                contextTokens,
              );
            const canStart = canCheck && checked && active;
            return (
              <article
                className="panel form-stack"
                key={build.id}
                aria-label={`Runtime ${build.id}`}
              >
                <div className="row-title">
                  <h3>{providerName(build.provider)}</h3>
                  <span className="tag">{build.id}</span>
                  {installed && <span className="tag">Installed</span>}
                  {active && <span className="tag accent">Active version</span>}
                </div>
                <p className="small muted">
                  {build.architecture} · macOS {build.minimumOs}+ ·{" "}
                  {formatBytes(build.bundle.size)} package · {build.license}
                </p>
                <p className="small muted">
                  {build.readyForInference
                    ? "Runtime checks passed. Coding quality remains experimental."
                    : "Check an installed model to establish protocol readiness for this runtime and context."}
                  {!supported && ` ${platform}`}
                </p>
                <div className="row-actions">
                  {!installed ? (
                    <Button
                      disabled={!!busy || !!activeJob || !supported}
                      onClick={() =>
                        setPreview({ kind: "runtime", item: build })
                      }
                    >
                      <Download size={14} />
                      {activeJob ? "Installing…" : "Review installation"}
                    </Button>
                  ) : (
                    <>
                      {!active && (
                        <Button
                          disabled={!!busy || !supported || processActive}
                          onClick={() =>
                            void perform("runtime.managed.activate", {
                              buildId: build.id,
                            })
                          }
                        >
                          Activate version
                        </Button>
                      )}
                      {active && previous && (
                        <Button
                          disabled={!!busy || processActive}
                          onClick={() =>
                            void perform("runtime.managed.rollback", {
                              provider: build.provider,
                            })
                          }
                        >
                          <RotateCcw size={14} />
                          Roll back to {previous}
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        disabled={!!busy || processActive || active}
                        onClick={async () => {
                          if (
                            await confirm({
                              title: "Remove managed runtime?",
                              description: `Remove ${build.id} from Gemma Desktop? Downloaded models are kept.`,
                              confirmLabel: "Remove runtime",
                              danger: true,
                            })
                          )
                            await perform("runtime.managed.remove", {
                              buildId: build.id,
                            });
                        }}
                      >
                        <Trash2 size={14} />
                        Remove
                      </Button>
                    </>
                  )}
                </div>
                {installed && (
                  <>
                    <Field label={`Model for ${build.id}`}>
                      <select
                        value={selectedModel?.id || ""}
                        onChange={(event) =>
                          setModelChoices({
                            ...modelChoices,
                            [build.id]: event.target.value,
                          })
                        }
                        disabled={ownsProcess}
                      >
                        {!availableModels.length && (
                          <option value="">
                            Download a compatible model below
                          </option>
                        )}
                        {availableModels.map((model) => (
                          <option value={model.id} key={model.id}>
                            {model.serveName}
                            {!model.providers.length && " · support unverified"}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field
                      label={`Context tokens for ${build.id}`}
                      hint="Used for model checks and startup. Personal defaults change only when you choose Use for chat."
                    >
                      <input
                        type="number"
                        min={1024}
                        max={selectedModel?.model.contextTokens || 131072}
                        step={1024}
                        value={contextTokens}
                        disabled={ownsProcess}
                        onChange={(event) =>
                          setContexts({
                            ...contexts,
                            [build.id]: Number(event.target.value),
                          })
                        }
                      />
                    </Field>
                    {selectedModel && !contextFits && (
                      <p className="inline-error">
                        Choose a context size that fits this model before
                        checking or starting it.
                      </p>
                    )}
                    <p className="small muted">
                      {checked
                        ? "Protocol check passed for this runtime, model and context. Coding quality remains experimental."
                        : "Check model runs a bounded completion and synthetic tool test without accessing your workspace. It leaves the runtime stopped."}
                    </p>
                    <div className="row-actions">
                      <Button
                        disabled={!!busy || !canCheck}
                        onClick={() =>
                          selectedModel &&
                          void perform("runtime.managed.qualify", {
                            buildId: build.id,
                            modelId: selectedModel.id,
                            contextTokens,
                          })
                        }
                      >
                        {checked ? "Recheck model" : "Check model"}
                      </Button>
                      {ownsProcess ? (
                        <Button
                          disabled={managed.process.status === "stopping"}
                          onClick={() =>
                            void action("runtime.managed.stop", {})
                          }
                        >
                          <Square size={14} />
                          Stop managed runtime
                        </Button>
                      ) : (
                        <Button
                          disabled={!!busy || !canStart}
                          onClick={() =>
                            selectedModel &&
                            void perform("runtime.managed.start", {
                              buildId: build.id,
                              modelId: selectedModel.id,
                              contextTokens,
                            })
                          }
                        >
                          <Play size={14} />
                          Start runtime
                        </Button>
                      )}
                      <Button
                        disabled={
                          !!busy ||
                          !supported ||
                          !selectedModel ||
                          !checked ||
                          (state.settings.defaultCompletionRuntimeId ===
                            build.id &&
                            state.settings.defaultModel ===
                              selectedModel.serveName &&
                            state.settings.contextSize === contextTokens)
                        }
                        onClick={() =>
                          selectedModel &&
                          void perform("settings.save", {
                            defaultCompletionRuntimeId: build.id,
                            defaultModel: selectedModel.serveName,
                            contextSize: contextTokens,
                          })
                        }
                      >
                        Use for chat
                      </Button>
                    </div>
                    {ownsProcess && (
                      <p role="status">
                        {friendlyStatus(managed.process.status)} ·{" "}
                        {managed.modelCatalog.find(
                          (model) => model.id === managed.process.modelId,
                        )?.serveName || managed.process.modelId}
                        {!!managed.process.contextTokens &&
                          ` · ${managed.process.contextTokens.toLocaleString()} loaded context tokens`}
                      </p>
                    )}
                  </>
                )}
              </article>
            );
          })}
        </div>
      )}
      {managed?.process.error && (
        <p className="inline-error" role="alert">
          Managed runtime: {managed.process.error}
        </p>
      )}
      {managed?.inferenceBlocked && (
        <p className="inline-error" role="alert">
          Inference is blocked: {managed.inferenceBlocked}
        </p>
      )}
      {models && !!managed?.modelCatalog.length && (
        <>
          <div className="list-heading">
            <h3>Models for managed MLX</h3>
            <span>Verified local files</span>
          </div>
          <div className="model-list">
            {managed.modelCatalog.map((model) => {
              const installed = managed.models.some(
                (item) => item.id === model.id,
              );
              const inUse =
                managed.process.modelId === model.id &&
                ["starting", "checking", "running", "stopping"].includes(
                  managed.process.status,
                );
              return (
                <article
                  className="panel"
                  key={model.id}
                  aria-label={`Managed model ${model.serveName}`}
                >
                  <div className="row-title">
                    <h3>{model.serveName}</h3>
                    {installed && <span className="tag">Installed</span>}
                  </div>
                  <p className="small muted">
                    {formatBytes(
                      model.files.reduce((size, file) => size + file.size, 0),
                    )}{" "}
                    · {model.license} ·{" "}
                    {model.providers.length
                      ? model.providers.map(providerName).join(", ")
                      : "Provider support unverified"}
                  </p>
                  <p className="small muted">
                    {model.repository} · revision {model.revision.slice(0, 12)}
                  </p>
                  <div className="row-actions">
                    {!installed ? (
                      <Button
                        disabled={!!busy || !!installing(model.id)}
                        onClick={() =>
                          setPreview({ kind: "model", item: model })
                        }
                      >
                        <Download size={14} />
                        {installing(model.id)
                          ? "Downloading…"
                          : "Review download"}
                      </Button>
                    ) : (
                      <Button
                        disabled={!!busy || inUse}
                        onClick={async () => {
                          if (
                            await confirm({
                              title: "Remove downloaded model?",
                              description: `Remove the managed copy of ${model.serveName}? A future download will require ${formatBytes(model.files.reduce((size, file) => size + file.size, 0))}.`,
                              confirmLabel: "Remove model",
                              danger: true,
                            })
                          )
                            await perform("runtime.managed.model.remove", {
                              modelId: model.id,
                            });
                        }}
                      >
                        <Trash2 size={14} />
                        Remove model
                      </Button>
                    )}
                  </div>
                </article>
              );
            })}
          </div>
        </>
      )}
      {!!managed?.jobs.length && (
        <div aria-label="Managed installation jobs">
          <h3>Installation activity</h3>
          {[...managed.jobs]
            .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
            .slice(0, 20)
            .map((job) => (
              <div className="pull-row" key={job.id}>
                <div>
                  <strong>{job.target}</strong>
                  <span role="status">
                    {job.error || friendlyStatus(job.status)}
                  </span>
                </div>
                {job.totalBytes > 0 && (
                  <>
                    <progress
                      aria-label={`Download progress for ${job.target}`}
                      value={job.completedBytes}
                      max={job.totalBytes}
                    />
                    <small>
                      {formatBytes(job.completedBytes)} /{" "}
                      {formatBytes(job.totalBytes)}
                    </small>
                  </>
                )}
                {!managedJobFinished(job.status) ? (
                  <Button
                    size="small"
                    disabled={!!busy}
                    onClick={() =>
                      void perform("runtime.managed.jobs.cancel", {
                        jobId: job.id,
                      })
                    }
                  >
                    Cancel installation
                  </Button>
                ) : (
                  job.status !== "complete" && (
                    <Button
                      size="small"
                      disabled={!!busy || !!installing(job.target)}
                      onClick={() => {
                        const item =
                          job.kind === "runtime_install"
                            ? managed.catalog.find(
                                (item) => item.id === job.target,
                              )
                            : managed.modelCatalog.find(
                                (item) => item.id === job.target,
                              );
                        if (item)
                          setPreview(
                            job.kind === "runtime_install"
                              ? {
                                  kind: "runtime",
                                  item: item as RuntimeManifest,
                                }
                              : {
                                  kind: "model",
                                  item: item as ManagedModelManifest,
                                },
                          );
                      }}
                    >
                      Review retry
                    </Button>
                  )
                )}
              </div>
            ))}
        </div>
      )}
      <Modal
        open={!!preview}
        onClose={() => !busy && setPreview(undefined)}
        title={
          preview?.kind === "runtime"
            ? "Install managed runtime"
            : "Download managed model"
        }
        description="Review the destination and storage before starting. Installation and network transfers start only after confirmation."
      >
        {preview && (
          <div className="form-stack">
            <strong>
              {preview.kind === "runtime"
                ? preview.item.id
                : preview.item.serveName}
            </strong>
            {!installPreview && !previewError && (
              <p role="status">
                Checking installation destination and available storage…
              </p>
            )}
            {previewError && (
              <div>
                <p className="inline-error" role="alert">
                  {previewError}
                </p>
                <Button onClick={() => setPreviewAttempt((value) => value + 1)}>
                  Retry preview
                </Button>
              </div>
            )}
            {installPreview && (
              <>
                <p>
                  {installPreview.sourceKind === "bundled"
                    ? `${formatBytes(installPreview.packageBytes)} bundled package · no network transfer`
                    : `${formatBytes(installPreview.transferBytes)} network download${installPreview.sourceKind === "mixed" ? " plus bundled files" : ""}`}{" "}
                  · {formatBytes(installPreview.installedBytes)} installed files
                </p>
                <div className="detail-list">
                  <div>
                    <span>Destination</span>
                    <code className="break-all">
                      {installPreview.destination}
                    </code>
                  </div>
                  {(
                    [
                      ["Temporary storage", installPreview.temporaryBytes],
                      ["Reserve kept free", installPreview.reserveBytes],
                      ["Free space required", installPreview.requiredFreeBytes],
                      ["Available space", installPreview.availableBytes],
                    ] as const
                  ).map(([label, bytes]) => (
                    <div key={label}>
                      <span>{label}</span>
                      <span title={`${bytes.toLocaleString()} bytes`}>
                        {formatBytes(bytes)}
                      </span>
                    </div>
                  ))}
                </div>
                {installPreview.sourceKind !== "bundled" && (
                  <p className="small muted">
                    Network transfer may be smaller when verified cached or
                    resumable files are available.
                  </p>
                )}
                {!installPreview.canInstall && (
                  <div>
                    <p className="inline-error" role="alert">
                      {installPreview.reason ||
                        "Installation is currently unavailable."}
                    </p>
                    <Button
                      onClick={() => setPreviewAttempt((value) => value + 1)}
                    >
                      Check again
                    </Button>
                  </div>
                )}
              </>
            )}
            <p>License: {preview.item.license}</p>
            {preview.kind === "runtime" ? (
              <p>
                {preview.item.architecture} · macOS {preview.item.minimumOs}+ ·
                source {preview.item.sourceSha.slice(0, 12)}
              </p>
            ) : (
              <p>
                {preview.item.repository} · revision{" "}
                {preview.item.revision.slice(0, 12)}
              </p>
            )}
            <p className="small muted">
              Progress survives closing this view. Cancel stops the job; retry
              starts a new job. Updates are installed only on request and
              activated separately.
            </p>
            <div className="modal-actions">
              <Button disabled={!!busy} onClick={() => setPreview(undefined)}>
                Cancel
              </Button>
              <Button
                variant="default"
                loading={!!busy}
                disabled={!installPreview?.canInstall}
                onClick={async () => {
                  if (!installPreview?.canInstall) return;
                  const result = await perform(
                    preview.kind === "runtime"
                      ? "runtime.managed.install"
                      : "runtime.managed.model.acquire",
                    preview.kind === "runtime"
                      ? { buildId: preview.item.id }
                      : { modelId: preview.item.id },
                  );
                  if (result !== undefined) setPreview(undefined);
                }}
              >
                {preview.kind === "runtime"
                  ? "Install runtime"
                  : "Download model"}
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </section>
  );
}
