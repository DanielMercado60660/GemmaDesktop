export type ManagedProvider = "omlx" | "mlx_serve";
export interface ManagedInstallPreview {
  kind: "runtime_install" | "model_download";
  target: string;
  destination: string;
  transferBytes: number;
  packageBytes: number;
  installedBytes: number;
  temporaryBytes: number;
  reserveBytes: number;
  requiredFreeBytes: number;
  availableBytes: number;
  bundled: boolean;
  sourceKind: "bundled" | "network" | "mixed";
  canInstall: boolean;
  reason?: string;
}
export interface RuntimeManifest {
  id: string;
  provider: ManagedProvider;
  sourceSha: string;
  architecture: string;
  minimumOs: string;
  license: string;
  readyForInference: boolean;
  unpackedBytes?: number;
  runtimeFileCount?: number;
  bundle: { size: number; url?: string; bundledPath?: string; sha256: string };
  files: Array<{ path: string; size: number; sha256: string }>;
  qualification?: {
    transport: "qualified" | "unverified";
    evidenceSha256?: string;
    coding: "experimental" | "qualified";
  };
}
export interface ManagedModelManifest {
  id: string;
  repository: string;
  revision: string;
  serveName: string;
  license: string;
  providers: ManagedProvider[];
  model: {
    name: string;
    architecture?: string;
    artifactDigest?: string;
    contextTokens?: number;
    capabilities: {
      completion: boolean;
      tools: boolean;
      thinking: boolean;
      vision: boolean;
      structuredOutput: boolean;
    };
  };
  files: Array<{
    path: string;
    size: number;
    sha256: string;
    source: { size: number; url?: string };
  }>;
}
export interface ManagedJob {
  id: string;
  kind: "runtime_install" | "model_download";
  target: string;
  status: string;
  completedBytes: number;
  totalBytes: number;
  error?: string;
  updatedAt: string;
}
export interface ManagedRuntimeSnapshot {
  inferenceBlocked?: string;
  platformSupport?: Record<string, string>;
  catalog: RuntimeManifest[];
  modelCatalog: ManagedModelManifest[];
  installed: Array<{ id: string; manifestDigest: string; installedAt: string }>;
  models: Array<{ id: string; manifestDigest: string; installedAt: string }>;
  selections: Partial<
    Record<ManagedProvider, { active?: string; previous?: string }>
  >;
  jobs: ManagedJob[];
  qualifications?: Array<{
    protocolVersion?: number;
    buildId: string;
    modelId: string;
    contextTokens: number;
    runtimeManifestDigest: string;
    modelManifestDigest: string;
    artifactDigest: string;
    templateDigest?: string;
    checkedAt: string;
    status: string;
    coding: string;
    checks: string[];
  }>;
  process: {
    buildId: string;
    modelId: string;
    status: string;
    error?: string;
    contextTokens?: number;
  };
}
export function qualifiedModel(
  managed: ManagedRuntimeSnapshot,
  buildId: string,
  modelId: string,
  contextTokens: number,
) {
  const runtime = managed.installed.find((item) => item.id === buildId),
    model = managed.models.find((item) => item.id === modelId);
  return (
    !!runtime &&
    !!model &&
    !!managed.qualifications?.some(
      (receipt) =>
        receipt.status === "qualified" &&
        receipt.protocolVersion === 2 &&
        receipt.buildId === buildId &&
        receipt.modelId === modelId &&
        receipt.contextTokens === contextTokens &&
        receipt.runtimeManifestDigest === runtime.manifestDigest &&
        receipt.modelManifestDigest === model.manifestDigest,
    )
  );
}
export const managedJobFinished = (status: string) =>
  ["complete", "failed", "cancelled", "interrupted"].includes(status);
export const providerName = (provider: ManagedProvider) =>
  provider === "omlx" ? "oMLX" : "mlx-serve";
