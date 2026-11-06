import { beforeEach, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ReliabilityWorkbench from "../../src/components/ReliabilityWorkbench";
import RunVerification from "../../src/components/RunVerification";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { api } from "../../src/bridge";
import { fixture } from "./fixtures";
import type { Action } from "../../src/Management";
import {
  evidenceByArm,
  type ArmV2,
  type FixturePackV2,
  type RunV2,
  type ResultV2,
} from "../../src/lib/harnessV2";
vi.mock("../../src/bridge", () => ({ api: vi.fn() }));
const packs: FixturePackV2[] = ["diagnostics", "qualification"].map((kind) => ({
  id: `coding-${kind}-v2`,
  version: "2",
  digest: "pack-digest",
  graderPolicy: "task-contract-v2",
  tasks: Array.from({ length: kind === "diagnostics" ? 6 : 12 }, (_, i) => ({
    id: `task-${i}`,
    name: `Task ${i + 1}`,
    stack: "go",
    split:
      kind === "diagnostics" ? "development" : i < 6 ? "validation" : "heldout",
    contract: "coding",
  })),
}));
const arms: ArmV2[] = ["small", "medium", "large"].map((id) => ({
  id,
  hash: id.repeat(12),
  name: `${id} arm`,
  provenance: "current-product",
  createdAt: "",
  spec: {
    workspaceId: "w",
    agentId: "default",
    model: id,
    contextSize: 65536,
    agent: {},
  },
  revision: { id: "revision", hash: "revision" },
}));
function run(kind: RunV2["kind"] = "diagnostic"): RunV2 {
  return {
    id: kind,
    kind,
    packId: kind === "diagnostic" ? packs[0].id : packs[1].id,
    packDigest: "pack-digest",
    armIds:
      kind === "diagnostic" ? arms.map((arm) => arm.id) : ["small", "medium"],
    scheduleHash: "schedule-hash",
    status: "checkpointed",
    cursor: 0,
    createdAt: "",
    schedule: [{ index: 0, taskId: "task-0", armId: "small" }],
    results: [],
    batchIds: [],
    gate: {
      policy: "reliability-v2",
      eligible: false,
      reasons: ["complete frozen schedule is required"],
      baselineSuccesses: 0,
      candidateSuccesses: 0,
      evidenceHash: "current-evidence",
    },
  };
}
let records: RunV2[];
beforeEach(() => {
  records = [run(), run("qualification")];
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "harness.packs.list") return { items: packs };
    if (method === "harness.arms.list") return { items: arms };
    if (method === "harness.promotionsV2.list") return { items: [] };
    if (method === "harness.diagnostics.list")
      return {
        items: records.filter((record) => record.kind === "diagnostic"),
      };
    if (method === "harness.evaluationsV2.list")
      return {
        items: records.filter((record) => record.kind === "qualification"),
      };
    if (method.endsWith(".get"))
      return records.find(
        (record) => record.id === (params as { id: string }).id,
      );
    if (method === "harness.batchesV2.list")
      return {
        items: [
          {
            id: "batch",
            runId: "diagnostic",
            startCursor: 0,
            nextCursor: 0,
            limits: { wallSeconds: 7200, maxAttempts: 3 },
            status: "checkpointed",
            stopReason: "attempt limit",
            startedAt: "",
          },
        ],
      };
    throw Error(`Unexpected method ${method}`);
  });
});
function show() {
  const action = vi.fn(async (method: string) =>
    method.endsWith(".create")
      ? run()
      : method.endsWith("exportArtifact")
        ? {
            id: "export",
            path: "/local/evidence.json",
            sha256: "1234567890abcdef",
            size: 100,
          }
        : {},
  );
  render(
    <ConfirmationProvider>
      <ReliabilityWorkbench
        state={fixture()}
        action={action as Action}
        workspaceId="w"
        sessionId="s"
      />
    </ConfirmationProvider>,
  );
  return { action, user: userEvent.setup() };
}
it("reviews and freezes the complete standard profile without starting any trial", async () => {
  const { action, user } = show();
  await screen.findByText(
    "Task 1 · Task 2 · Task 3 · Task 4 · Task 5 · Task 6",
  );
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Agent to freeze" }),
    "default",
  );
  await user.click(
    screen.getByRole("button", { name: "Review frozen profile" }),
  );
  expect(action).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Freeze arm" }));
  expect(action).toHaveBeenCalledExactlyOnceWith(
    "harness.arms.create",
    expect.objectContaining({
      workspaceId: "w",
      agentId: "default",
      runtimeId: "ollama-default",
      contextSize: 65536,
      maxOutputTokens: 16384,
      maxSteps: 80,
      temperature: 1,
      thinking: "on",
      budgetSeconds: 1200,
      mode: "native",
    }),
  );
});
it("creates a 36-attempt diagnostic schedule only after confirmation and never advances automatically", async () => {
  const { action, user } = show();
  for (const name of ["small arm", "medium arm", "large arm"])
    await user.click(
      await screen.findByRole("checkbox", { name: new RegExp(name) }),
    );
  await user.click(
    screen.getByRole("button", { name: "Create diagnostic schedule" }),
  );
  expect(screen.getByRole("alertdialog").textContent).toContain("36 attempts");
  expect(action).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Create schedule" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("harness.diagnostics.create", {
    packId: packs[0].id,
    taskIds: packs[0].tasks.map((task) => task.id),
    armIds: ["small", "medium", "large"],
    repeats: 2,
  });
});
it.each([
  ["Development screen", "diagnostics", 3],
  ["Qualification", "evaluationsV2", 2],
] as const)(
  "reviews a bounded %s batch and uses explicit attempt and wall limits",
  async (label, endpoint, maxAttempts) => {
    const { action, user } = show();
    await user.click(
      await screen.findByRole("button", { name: new RegExp(`^${label} ·`) }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Review next batch" }),
    );
    expect(screen.getByRole("alertdialog").textContent).toContain(
      "within two hours",
    );
    expect(action).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Start batch" }));
    expect(action).toHaveBeenCalledExactlyOnceWith(
      `harness.${endpoint}.advance`,
      {
        id: endpoint === "diagnostics" ? "diagnostic" : "qualification",
        wallSeconds: 7200,
        maxAttempts,
      },
    );
  },
);
it("saves evidence through the host and reveals the verified artifact", async () => {
  const { action, user } = show();
  await user.click(
    await screen.findByRole("button", { name: /^Development screen ·/ }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Save evidence export" }),
  );
  expect(action).toHaveBeenCalledWith("harness.diagnostics.exportArtifact", {
    id: "diagnostic",
  });
  expect(await screen.findByText("/local/evidence.json")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Reveal export" }));
  expect(action).toHaveBeenLastCalledWith("artifact.reveal", { id: "export" });
});
it("keeps completed status, gate eligibility and independent approval separate", async () => {
  records[1].status = "completed";
  records[1].gate.eligible = true;
  const { action, user } = show();
  await user.click(
    await screen.findByRole("button", { name: /^Qualification ·/ }),
  );
  const inspect = await screen.findByRole("button", {
    name: "Inspect qualified arm",
  });
  expect((inspect as HTMLButtonElement).disabled).toBe(true);
  const review = {
    author: { kind: "codex", identity: "independent-reviewer" },
    verdict: "approved",
    evidenceHash: "current-evidence",
    notes: "Reviewed fresh evidence.",
  };
  await user.click(
    screen.getByRole("textbox", { name: "Independent review JSON" }),
  );
  await user.paste(JSON.stringify(review));
  await user.click(
    screen.getByRole("button", { name: "Record independent review" }),
  );
  expect(action).toHaveBeenCalledExactlyOnceWith(
    "harness.evaluationsV2.review",
    { id: "qualification", review },
  );
});
it("does not treat absent usage as zero cost or an integrity failure as a success", () => {
  const record = run();
  const base: ResultV2 = {
    index: 0,
    taskId: "task-0",
    split: "development",
    armId: "small",
    arm: "arm-0",
    repeat: 0,
    trialId: "trial",
    trialStatus: "completed",
    durationMs: 0,
    tokens: 0,
    accountingComplete: false,
    outputHash: "hash",
    grade: {
      passed: true,
      graderHash: "grader",
      checks: [{ name: "unit", passed: true }],
    },
  };
  record.results = [
    base,
    { ...base, index: 1, infrastructureError: "sandbox unavailable" },
  ];
  const small = evidenceByArm(record)[0];
  expect(small).toMatchObject({
    total: 2,
    success: 1,
    accounted: 0,
    evidenceIssues: 1,
    meanTokens: undefined,
    meanSeconds: undefined,
  });
});
it("shows host verification independently of a completed run", () => {
  const run = {
    id: "run",
    sessionId: "s",
    workspaceId: "w",
    status: "completed",
    model: "test",
    prompt: "repair",
    startedAt: "",
  };
  const view = render(
    <RunVerification run={{ ...run, verification: "unverified" }} />,
  );
  expect(
    within(screen.getByRole("status")).getByText("Task remains unverified"),
  ).toBeTruthy();
  view.rerender(<RunVerification run={{ ...run, verification: "passed" }} />);
  expect(screen.getByRole("status").textContent).toBe(
    "Task verification passed",
  );
  view.rerender(<RunVerification run={run} />);
  expect(screen.queryByRole("status")).toBeNull();
});

it("freezes the actual current-product baseline without applying screen overrides", async () => {
  const { action, user } = show();
  await screen.findByText(
    "Task 1 · Task 2 · Task 3 · Task 4 · Task 5 · Task 6",
  );
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Agent to freeze" }),
    "default",
  );
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Profile source" }),
    "product",
  );
  await user.click(
    screen.getByRole("button", { name: "Review frozen profile" }),
  );
  expect(screen.getByRole("dialog").textContent).toContain(
    "without applying screen overrides",
  );
  await user.click(screen.getByRole("button", { name: "Freeze arm" }));
  expect(action).toHaveBeenCalledWith(
    "harness.arms.create",
    expect.objectContaining({
      provenance: "product:frozen",
      agentId: "default",
      workspaceId: "w",
    }),
  );
  const payload = (
    action.mock.calls[0] as unknown as [string, Record<string, unknown>]
  )[1];
  expect(payload).not.toHaveProperty("model");
  expect(payload).not.toHaveProperty("contextSize");
  expect(payload).not.toHaveProperty("temperature");
});
it("requires current independent approval and an explicit scoped activation confirmation", async () => {
  records[1].status = "completed";
  records[1].gate.eligible = true;
  records[1].review = {
    author: { kind: "codex", identity: "independent" },
    verdict: "approved",
    evidenceHash: "current-evidence",
    notes: "Reviewed",
  };
  const { action, user } = show();
  await user.click(
    await screen.findByRole("button", { name: /^Qualification ·/ }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Activate qualified profile" }),
  );
  expect(screen.getByRole("alertdialog").textContent).toContain(
    "Gemma in Fixture project",
  );
  expect(action).not.toHaveBeenCalled();
  await user.click(
    within(screen.getByRole("alertdialog")).getByRole("button", {
      name: "Activate qualified profile",
    }),
  );
  expect(action).toHaveBeenCalledExactlyOnceWith(
    "harness.evaluationsV2.promote",
    { id: "qualification" },
  );
});

it.each(["diagnostic", "qualification"] as const)(
  "resumes a cancelled %s schedule only after explicit confirmation",
  async (kind) => {
    records = [{ ...run(kind), status: "cancelled" }];
    const { user, action } = show();
    await user.click(
      await screen.findByRole("button", {
        name:
          kind === "diagnostic" ? /^Development screen ·/ : /^Qualification ·/,
      }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Review resume" }),
    );
    expect(action).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Resume batch" }));
    expect(action).toHaveBeenCalledExactlyOnceWith(
      `harness.${kind === "diagnostic" ? "diagnostics" : "evaluationsV2"}.resume`,
      {
        id: kind,
        wallSeconds: 7200,
        maxAttempts: kind === "diagnostic" ? 3 : 2,
      },
    );
  },
);
it("waits for cancelled work to checkpoint and does not offer advance for a failed schedule", async () => {
  records = [
    { ...run(), status: "cancelled" },
    { ...run("qualification"), status: "failed" },
  ];
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "harness.batchesV2.list"
      ? { items: [{ id: "batch", status: "running", startedAt: "" }] }
      : original(method, params),
  );
  const { user } = show();
  await user.click(
    await screen.findByRole("button", { name: /^Development screen ·/ }),
  );
  expect(
    (
      (await screen.findByRole("button", {
        name: "Review resume",
      })) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.getByText(/Waiting for cancelled work/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: /^Qualification ·/ }));
  await screen.findByText("Qualification details");
  expect(
    screen.queryByRole("button", { name: "Review next batch" }),
  ).toBeNull();
  expect(screen.queryByRole("button", { name: "Review resume" })).toBeNull();
});
