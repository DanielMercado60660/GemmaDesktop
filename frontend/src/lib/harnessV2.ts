export interface FixturePackV2 {
  id: string;
  version: string;
  digest: string;
  graderPolicy: string;
  tasks: Array<{
    id: string;
    name: string;
    stack: string;
    split: string;
    contract: string;
  }>;
}
export interface ArmV2 {
  id: string;
  hash: string;
  name: string;
  provenance: string;
  createdAt: string;
  spec: {
    workspaceId: string;
    agentId: string;
    model: string;
    contextSize: number;
    agent: Record<string, unknown>;
    runtimeIdentity?: Record<string, unknown>;
    fixturePackId?: string;
    appVersion?: string;
    executorIdentity?: string;
  };
  revision: {
    id: string;
    hash: string;
    authoredBy?: { kind: string; identity: string };
  };
}
export interface ResultV2 {
  index: number;
  taskId: string;
  split: string;
  armId: string;
  arm: string;
  repeat: number;
  trialId: string;
  trialStatus: string;
  durationMs: number;
  tokens: number;
  accountingComplete: boolean;
  outputHash: string;
  infrastructureError?: string;
  grade: {
    passed: boolean;
    graderHash: string;
    error?: string;
    checks: Array<{ name: string; passed: boolean; error?: string }>;
  };
}
export interface RunV2 {
  id: string;
  kind: "diagnostic" | "qualification";
  packId: string;
  packDigest: string;
  armIds: string[];
  scheduleHash: string;
  status: string;
  cursor: number;
  createdAt: string;
  error?: string;
  recordedResults?: number;
  expectedResults?: number;
  schedule?: Array<{ index: number; taskId: string; armId: string }>;
  results?: ResultV2[];
  batchIds: string[];
  gate: {
    policy: string;
    eligible: boolean;
    reasons: string[];
    baselineSuccesses: number;
    candidateSuccesses: number;
    evidenceHash: string;
  };
  review?: {
    author: { kind: string; identity: string };
    verdict: string;
    evidenceHash: string;
    notes: string;
  };
}
export interface BatchV2 {
  id: string;
  runId: string;
  startCursor: number;
  nextCursor: number;
  limits: { wallSeconds: number; maxAttempts: number };
  status: string;
  stopReason: string;
  startedAt: string;
  finishedAt?: string;
}
export const standardProfileV2 = {
  mode: "native",
  contextSize: 65536,
  maxOutputTokens: 16384,
  maxSteps: 80,
  temperature: 1,
  thinking: "on",
  budgetSeconds: 1200,
} as const;
export const runMethodV2 = (kind: RunV2["kind"], operation: string) =>
  `harness.${kind === "diagnostic" ? "diagnostics" : "evaluationsV2"}.${operation}`;
export function evidenceByArm(run: RunV2) {
  return run.armIds.map((id) => {
    const rows = (run.results || []).filter((row) => row.armId === id);
    const success = rows.filter(
      (row) =>
        row.trialStatus === "completed" &&
        row.grade.passed &&
        (row.grade.checks || []).length > 0 &&
        (row.grade.checks || []).every((check) => check.passed) &&
        !row.infrastructureError &&
        !row.grade.error,
    ).length;
    const measured = rows.filter(
      (row) => row.accountingComplete && row.tokens > 0 && row.durationMs > 0,
    );
    const evidenceIssues = rows.filter(
      (row) =>
        !!row.infrastructureError ||
        !!row.grade.error ||
        !row.trialId ||
        !row.outputHash ||
        !row.grade.graderHash ||
        !(row.grade.checks || []).length ||
        (row.grade.checks || []).some(
          (check) =>
            !check.passed &&
            /^(integrity|authority|infrastructure):/i.test(check.name),
        ),
    ).length;
    return {
      id,
      total: rows.length,
      success,
      accounted: measured.length,
      evidenceIssues,
      meanTokens: measured.length
        ? measured.reduce((n, row) => n + row.tokens, 0) / measured.length
        : undefined,
      meanSeconds: measured.length
        ? measured.reduce((n, row) => n + row.durationMs, 0) /
          measured.length /
          1000
        : undefined,
    };
  });
}
