import type { Model, Runtime, State } from "../types";
import { isCompletionModel } from "./modelCapabilities";
import { qualifiedModel } from "./managedRuntime";

export interface ModelReadiness {
  ready: boolean;
  reason: string;
  action: "models" | "refresh" | null;
}

export function completionProfile(
  state: State,
  workspaceId: string,
  agentId = "default",
) {
  const agent = state.agents.find((item) => item.id === agentId);
  const effective = state.effectiveProfiles?.find(
    (item) => item.workspaceId === workspaceId && item.agentId === agentId,
  );
  return {
    effective,
    model: effective?.model || agent?.model || state.settings.defaultModel,
    runtimeId:
      effective?.runtimeId ||
      agent?.runtimeId ||
      state.settings.defaultCompletionRuntimeId ||
      "ollama-default",
    contextSize: effective?.contextSize || state.settings.contextSize,
  };
}

export function completionModels(
  state: State,
  runtimeId = state.settings.defaultCompletionRuntimeId || "ollama-default",
): Model[] {
  if (runtimeId === "ollama-default") return state.models;
  const managed = state.managedRuntime;
  const build = managed?.catalog.find((item) => item.id === runtimeId);
  if (!build) return [];
  return (managed?.modelCatalog || [])
    .filter(
      (model) =>
        (!model.providers.length || model.providers.includes(build.provider)) &&
        managed?.models.some((installed) => installed.id === model.id),
    )
    .map((model) => ({
      name: model.serveName,
      size: model.files.reduce((bytes, file) => bytes + file.size, 0),
      digest: model.model.artifactDigest || "",
      family: model.model.architecture || "",
      parameterSize: "",
      quantization: "",
      capabilities: Object.entries(model.model.capabilities)
        .filter(([, supported]) => supported)
        .map(([name]) => name),
      loaded:
        managed?.process.status === "running" &&
        managed.process.buildId === runtimeId &&
        managed.process.modelId === model.id,
      contextLength: model.model.contextTokens,
    }));
}

export function completionMetadataReadiness(
  state: State,
  selected: string,
  runtimeId = state.settings.defaultCompletionRuntimeId || "ollama-default",
  contextSize = state.settings.contextSize,
): ModelReadiness {
  if (runtimeId === "ollama-default")
    return modelReadiness(state.runtime, state.models, selected);
  const managed = state.managedRuntime;
  const build = managed?.catalog.find((item) => item.id === runtimeId);
  if (
    !build ||
    (managed?.platformSupport?.[runtimeId] &&
      managed.platformSupport[runtimeId] !== "supported") ||
    !managed?.installed.some((item) => item.id === runtimeId)
  )
    return {
      ready: false,
      reason: "Install a verified MLX runtime in Models to continue.",
      action: "models",
    };
  const model = managed.modelCatalog.find(
    (item) =>
      item.serveName === selected &&
      (!item.providers.length || item.providers.includes(build.provider)),
  );
  if (!model || !managed.models.some((item) => item.id === model.id))
    return {
      ready: false,
      reason: `Download the selected model “${selected}” in Models to continue.`,
      action: "models",
    };
  if (!model.model.capabilities.completion)
    return {
      ready: false,
      reason: "Choose a completion model for this runtime.",
      action: "models",
    };
  if (
    !model.model.contextTokens ||
    contextSize > model.model.contextTokens ||
    contextSize < 1024 ||
    !Number.isInteger(contextSize)
  )
    return {
      ready: false,
      reason: "Choose a context size that fits this model before continuing.",
      action: "models",
    };
  if (!qualifiedModel(managed, runtimeId, model.id, contextSize))
    return {
      ready: false,
      reason:
        "Check this model with the selected MLX runtime and context in Models before starting.",
      action: "models",
    };
  return { ready: true, reason: "", action: null };
}

export function completionReadiness(
  state: State,
  selected: string,
  runtimeId = state.settings.defaultCompletionRuntimeId || "ollama-default",
  contextSize = state.settings.contextSize,
): ModelReadiness {
  if (state.managedRuntime?.inferenceBlocked)
    return {
      ready: false,
      reason: `Inference is blocked: ${state.managedRuntime.inferenceBlocked}`,
      action: "models",
    };
  const metadata = completionMetadataReadiness(
    state,
    selected,
    runtimeId,
    contextSize,
  );
  if (!metadata.ready || runtimeId === "ollama-default") return metadata;
  const managed = state.managedRuntime!;
  const model = managed.modelCatalog.find(
    (item) => item.serveName === selected,
  )!;
  // Verified native profiles start on demand after host admission, including a
  // frozen build that differs from the user's manually active version.
  if (["stopped", "failed"].includes(managed.process.status)) return metadata;
  if (["starting", "checking", "stopping"].includes(managed.process.status))
    return {
      ready: false,
      reason:
        "Wait for the managed runtime operation to finish, or stop it in Models.",
      action: "models",
    };
  if (
    managed.process.status !== "running" ||
    managed.process.buildId !== runtimeId ||
    managed.process.modelId !== model.id
  )
    return {
      ready: false,
      reason:
        "Stop the current managed runtime in Models before using a different profile.",
      action: "models",
    };
  if (managed.process.contextTokens !== contextSize)
    return {
      ready: false,
      reason:
        "Restart the managed runtime to apply the context size selected in Settings.",
      action: "models",
    };
  return { ready: true, reason: "", action: null };
}

// Reachability is not model readiness. An installed model need not be loaded:
// the local runtime can load it on demand, after ordinary run admission.
export function modelReadiness(
  runtime: Runtime,
  models: Model[],
  selected: string,
): ModelReadiness {
  if (!runtime.available)
    return {
      ready: false,
      reason: "Start your local runtime to continue.",
      action: "models",
    };
  const model = models.find((item) => item.name === selected);
  if (!selected || !model)
    return {
      ready: false,
      reason: selected
        ? `The selected model “${selected}” is not installed.`
        : "Choose an installed model to continue.",
      action: "models",
    };
  if (model.remote || !isCompletionModel(model))
    return {
      ready: false,
      reason: "Choose a local completion model to continue.",
      action: "models",
    };
  if (!model.capabilities?.length)
    return {
      ready: false,
      reason:
        "Model capabilities are not available yet. Refresh to check them.",
      action: "refresh",
    };
  return { ready: true, reason: "", action: null };
}
