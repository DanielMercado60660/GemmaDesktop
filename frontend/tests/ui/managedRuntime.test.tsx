import { beforeEach, it, expect, vi } from "vitest";
import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ManagedRuntimePanel from "../../src/components/ManagedRuntimePanel";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { completionModels, completionReadiness } from "../../src/lib/readiness";
import { managedFixture, deferred } from "./fixtures";
import { api } from "../../src/bridge";
import type { ManagedInstallPreview } from "../../src/lib/managedRuntime";
import type { State } from "../../src/types";
import type { Action } from "../../src/Management";
vi.mock("../../src/bridge", () => ({ api: vi.fn() }));
function previewFixture(params: {
  buildId?: string;
  modelId?: string;
}): ManagedInstallPreview {
  const runtime = !!params.buildId;
  const installedBytes = runtime ? 200000000 : 2000000000;
  const packageBytes = runtime ? 100000000 : 2000000000;
  const reserveBytes = 10 * 1024 ** 3;
  return {
    kind: runtime ? "runtime_install" : "model_download",
    target: params.buildId || params.modelId!,
    destination: `/fixture/managed/${runtime ? "runtimes/mlx-v1" : "models/gemma-native/native-gemma"}`,
    transferBytes: packageBytes,
    packageBytes,
    installedBytes,
    temporaryBytes: packageBytes,
    reserveBytes,
    requiredFreeBytes: installedBytes + packageBytes + reserveBytes,
    availableBytes: 100 * 1024 ** 3,
    bundled: false,
    sourceKind: "network",
    canInstall: true,
  };
}
beforeEach(() => {
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "runtime.managed.preview")
      return previewFixture(params as { buildId?: string; modelId?: string });
    throw new Error(`Unexpected preview request ${method}`);
  });
});
function show(state = managedFixture()) {
  const action = vi.fn(async () => ({}));
  const view = render(
    <ConfirmationProvider>
      <ManagedRuntimePanel state={state} action={action as Action} />
    </ConfirmationProvider>,
  );
  return {
    action,
    user: userEvent.setup(),
    update: (next: State) =>
      view.rerender(
        <ConfirmationProvider>
          <ManagedRuntimePanel state={next} action={action as Action} />
        </ConfirmationProvider>,
      ),
  };
}
it("starts no installation until the runtime package preview is confirmed", async () => {
  const { action, user } = show();
  expect(action).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Review installation" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText(/Apache-2.0/)).toBeTruthy();
  expect(dialog.getByText(/download.*installed files/)).toBeTruthy();
  expect(dialog.getByText(/arm64.*macOS 14.0/)).toBeTruthy();
  expect(action).not.toHaveBeenCalled();
  await user.click(dialog.getByRole("button", { name: "Install runtime" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("runtime.managed.install", {
    buildId: "mlx-v1",
  });
});
it("requires a separate confirmation before acquiring a large model", async () => {
  const { action, user } = show();
  await user.click(screen.getByRole("button", { name: "Review download" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText(/example\/gemma.*revision/)).toBeTruthy();
  expect(action).not.toHaveBeenCalled();
  await user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(action).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Review download" }));
  await user.click(screen.getByRole("button", { name: "Download model" }));
  expect(action).toHaveBeenCalledExactlyOnceWith(
    "runtime.managed.model.acquire",
    { modelId: "gemma-native" },
  );
});
it("requires activation before starting with the exact model and context and offers manual rollback", async () => {
  const state = managedFixture();
  state.managedRuntime!.catalog[0].readyForInference = false;
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  const { action, user, update } = show(state);
  expect(
    (screen.getByRole("button", { name: "Start runtime" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Activate version" }));
  expect(action).toHaveBeenLastCalledWith("runtime.managed.activate", {
    buildId: "mlx-v1",
  });
  const active = structuredClone(state);
  active.managedRuntime!.selections.omlx = {
    active: "mlx-v1",
    previous: "mlx-old",
  };
  update(active);
  expect(
    (screen.getByRole("button", { name: "Start runtime" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Check model" }));
  expect(action).toHaveBeenLastCalledWith("runtime.managed.qualify", {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    contextTokens: 32768,
  });
  active.managedRuntime!.qualifications = [qualification()];
  update(structuredClone(active));
  await user.click(screen.getByRole("button", { name: "Start runtime" }));
  expect(action).toHaveBeenLastCalledWith("runtime.managed.start", {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    contextTokens: 32768,
  });
  await user.click(
    screen.getByRole("button", { name: "Roll back to mlx-old" }),
  );
  expect(action).toHaveBeenLastCalledWith("runtime.managed.rollback", {
    provider: "omlx",
  });
});
it("restores job progress, allows cancellation, and previews retry after failure", async () => {
  const state = managedFixture();
  state.managedRuntime!.jobs = [
    {
      id: "job",
      kind: "model_download",
      target: "gemma-native",
      status: "downloading",
      completedBytes: 10,
      totalBytes: 2000000000,
      updatedAt: "2026-11-04",
    },
  ];
  const { action, user, update } = show(state);
  expect(
    screen
      .getByRole("progressbar", { name: "Download progress for gemma-native" })
      .getAttribute("value"),
  ).toBe("10");
  await user.click(screen.getByRole("button", { name: "Cancel installation" }));
  expect(action).toHaveBeenLastCalledWith("runtime.managed.jobs.cancel", {
    jobId: "job",
  });
  const failed = structuredClone(state);
  failed.managedRuntime!.jobs[0].status = "failed";
  failed.managedRuntime!.jobs[0].error = "Checksum mismatch";
  update(failed);
  expect(screen.getByText("Checksum mismatch")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Review retry" }));
  expect(screen.getByRole("dialog")).toBeTruthy();
  expect(action).toHaveBeenCalledTimes(1);
});
it("stops only the owned process and reports a failed startup", async () => {
  const state = managedFixture();
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.process = {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    status: "running",
    contextTokens: 32768,
  };
  const { action, user, update } = show(state);
  await user.click(
    screen.getByRole("button", { name: "Stop managed runtime" }),
  );
  expect(action).toHaveBeenCalledExactlyOnceWith("runtime.managed.stop", {});
  const failed = structuredClone(state);
  failed.managedRuntime!.process = {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    status: "failed",
    error: "Runtime exited during model load",
  };
  update(failed);
  expect(screen.getByRole("alert").textContent).toContain("Runtime exited");
});
it("uses verified native metadata and owned readiness independently of external Ollama", () => {
  const state = managedFixture();
  state.settings.defaultCompletionRuntimeId = "mlx-v1";
  state.settings.defaultModel = "native-gemma";
  state.runtime.available = false;
  expect(completionReadiness(state, "native-gemma").ready).toBe(false);
  expect(completionModels(state)).toEqual([]);
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  expect(completionModels(state).map((model) => model.name)).toEqual([
    "native-gemma",
  ]);
  expect(completionReadiness(state, "native-gemma").reason).toContain("Check");
  state.managedRuntime!.qualifications = [qualification()];
  expect(completionReadiness(state, "native-gemma").ready).toBe(true);
  state.managedRuntime!.process = {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    status: "running",
    contextTokens: 32768,
  };
  expect(completionReadiness(state, "native-gemma").ready).toBe(true);
  expect(
    completionReadiness(state, state.models[0].name, "ollama-default").ready,
  ).toBe(false);
  state.settings.contextSize = 16384;
  expect(completionReadiness(state, "native-gemma").reason).toContain("Check");
  state.managedRuntime!.catalog[0].readyForInference = false;
  state.settings.contextSize = 32768;
  expect(completionReadiness(state, "native-gemma").ready).toBe(true);
});

function qualification() {
  return {
    protocolVersion: 2,
    buildId: "mlx-v1",
    modelId: "gemma-native",
    contextTokens: 32768,
    runtimeManifestDigest: "verified",
    modelManifestDigest: "verified",
    artifactDigest: "verified",
    checkedAt: "",
    status: "qualified",
    coding: "experimental",
    checks: ["completion", "synthetic tool"],
  };
}

it("offers an unverified installed model for checking but cannot start it without an exact receipt", async () => {
  const state = managedFixture();
  state.managedRuntime!.modelCatalog[0].providers = [];
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.selections.omlx = { active: "mlx-v1" };
  const { user, action } = show(state);
  expect(
    screen.getByRole("option", { name: "native-gemma · support unverified" }),
  ).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Start runtime" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Check model" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("runtime.managed.qualify", {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    contextTokens: 32768,
  });
});

it("uses compact runtime manifest sizes in the install preview", async () => {
  const state = managedFixture();
  state.managedRuntime!.catalog[0].files = [];
  state.managedRuntime!.catalog[0].unpackedBytes = 200000000;
  const { user } = show(state);
  await user.click(screen.getByRole("button", { name: "Review installation" }));
  expect(
    within(screen.getByRole("dialog")).getByText(/190.7 MB installed files/),
  ).toBeTruthy();
});

it("requires rechecking legacy and unknown protocol qualification receipts", () => {
  const state = managedFixture();
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.qualifications = [
    { ...qualification(), protocolVersion: undefined },
  ];
  state.managedRuntime!.process = {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    status: "running",
    contextTokens: 32768,
  };
  expect(completionReadiness(state, "native-gemma", "mlx-v1").reason).toContain(
    "Check",
  );
  state.managedRuntime!.qualifications[0].protocolVersion = 3;
  expect(completionReadiness(state, "native-gemma", "mlx-v1").ready).toBe(
    false,
  );
  state.managedRuntime!.qualifications[0].protocolVersion = 2;
  expect(completionReadiness(state, "native-gemma", "mlx-v1").ready).toBe(true);
});

it("blocks live inference after unconfirmed cleanup without blocking metadata inspection", async () => {
  const { completionMetadataReadiness } = await import(
    "../../src/lib/readiness"
  );
  const state = managedFixture();
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.qualifications = [qualification()];
  state.managedRuntime!.inferenceBlocked =
    "Owned process cleanup not confirmed";
  expect(
    completionMetadataReadiness(state, "native-gemma", "mlx-v1").ready,
  ).toBe(true);
  expect(completionReadiness(state, "native-gemma", "mlx-v1").reason).toContain(
    "cleanup not confirmed",
  );
  expect(
    completionReadiness(state, state.models[0].name, "ollama-default").ready,
  ).toBe(false);
});
it("checks inactive builds while keeping manual Start gated on activation", async () => {
  const state = managedFixture();
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  const { user, action } = show(state);
  await user.click(screen.getByRole("button", { name: "Check model" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("runtime.managed.qualify", {
    buildId: "mlx-v1",
    modelId: "gemma-native",
    contextTokens: 32768,
  });
  expect(
    (screen.getByRole("button", { name: "Start runtime" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("shows host-owned bundled destination and numeric staging requirements before allowing installation", async () => {
  const pending = deferred<ManagedInstallPreview>();
  vi.mocked(api).mockImplementationOnce(() => pending.promise);
  const { user, action } = show();
  await user.click(screen.getByRole("button", { name: "Review installation" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(
    (
      dialog.getByRole("button", {
        name: "Install runtime",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  const data = {
    ...previewFixture({ buildId: "mlx-v1" }),
    bundled: true,
    sourceKind: "bundled" as const,
    transferBytes: 0,
  };
  await act(async () => pending.resolve(data));
  expect(
    dialog.getByText(/95.4 MB bundled package.*no network transfer/),
  ).toBeTruthy();
  expect(dialog.getByText(data.destination)).toBeTruthy();
  expect(dialog.getByText("Temporary storage")).toBeTruthy();
  expect(
    dialog.getByTitle(`${data.temporaryBytes.toLocaleString()} bytes`)
      .textContent,
  ).toBe("95.4 MB");
  expect(
    dialog.getByTitle(`${data.requiredFreeBytes.toLocaleString()} bytes`),
  ).toBeTruthy();
  expect(dialog.queryByText(/network download/)).toBeNull();
  expect(action).not.toHaveBeenCalled();
  await user.click(dialog.getByRole("button", { name: "Install runtime" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("runtime.managed.install", {
    buildId: "mlx-v1",
  });
});
it("keeps failed and insufficient-space previews blocked and retries without starting a download", async () => {
  vi.mocked(api).mockRejectedValueOnce(
    new Error("Could not inspect free space"),
  );
  vi.mocked(api).mockResolvedValueOnce({
    ...previewFixture({ modelId: "gemma-native" }),
    canInstall: false,
    reason: "Insufficient disk space",
  });
  const { user, action } = show();
  await user.click(screen.getByRole("button", { name: "Review download" }));
  await screen.findByText("Could not inspect free space");
  expect(
    (
      screen.getByRole("button", {
        name: "Download model",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Retry preview" }));
  await screen.findByText("Insufficient disk space");
  expect(
    (
      screen.getByRole("button", {
        name: "Download model",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(action).not.toHaveBeenCalled();
});
it("ignores a late runtime preview after switching to a model preview", async () => {
  const pending = deferred<ManagedInstallPreview>();
  vi.mocked(api).mockImplementationOnce(() => pending.promise);
  const { user } = show();
  await user.click(screen.getByRole("button", { name: "Review installation" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Review download" }));
  await screen.findByText("/fixture/managed/models/gemma-native/native-gemma");
  await act(async () => pending.resolve(previewFixture({ buildId: "mlx-v1" })));
  expect(screen.queryByText("/fixture/managed/runtimes/mlx-v1")).toBeNull();
  expect(
    (
      screen.getByRole("button", {
        name: "Download model",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(false);
});
