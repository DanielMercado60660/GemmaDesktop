import { beforeEach, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import HarnessWorkbench from "../../src/HarnessWorkbench";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { api } from "../../src/bridge";
import type { Action } from "../../src/Management";
import { managedFixture } from "./fixtures";
vi.mock("../../src/bridge", () => ({ api: vi.fn() }));
beforeEach(() => {
  vi.mocked(api).mockImplementation(async (method) => {
    if (method === "harness.packs.list")
      return {
        items: [
          {
            id: "coding-diagnostics-v2",
            version: "2",
            digest: "pack",
            tasks: [
              { id: "task", name: "Repair", stack: "go", split: "development" },
            ],
          },
        ],
      };
    if (method.endsWith(".list")) return { items: [] };
    throw new Error(`Unexpected method ${method}`);
  });
});
it("freezes the effective native profile while stopped and requires the form's exact qualified context", async () => {
  const state = managedFixture();
  state.runtime.available = false;
  state.models = [];
  state.managedRuntime!.installed = [
    { id: "mlx-v1", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.models = [
    { id: "gemma-native", manifestDigest: "verified", installedAt: "" },
  ];
  state.managedRuntime!.qualifications = [
    {
      protocolVersion: 2,
      buildId: "mlx-v1",
      modelId: "gemma-native",
      contextTokens: 65536,
      runtimeManifestDigest: "verified",
      modelManifestDigest: "verified",
      artifactDigest: "verified",
      checkedAt: "",
      status: "qualified",
      coding: "experimental",
      checks: [],
    },
  ];
  state.managedRuntime!.inferenceBlocked = "Cleanup requires attention";
  state.effectiveProfiles = [
    {
      workspaceId: "w",
      agentId: "default",
      model: "native-gemma",
      runtimeId: "mlx-v1",
      contextSize: 65536,
      status: "configured",
    },
  ];
  const action = vi.fn(async () => ({}));
  render(
    <ConfirmationProvider>
      <HarnessWorkbench
        state={state}
        action={action as Action}
        workspaceId="w"
        sessionId="s"
        compare={null}
      />
    </ConfirmationProvider>,
  );
  const user = userEvent.setup();
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Baseline agent" }),
    "default",
  );
  await user.click(
    await screen.findByRole("checkbox", { name: "Repair (go)" }),
  );
  const freeze = screen.getByRole("button", {
    name: "Freeze experiment",
  }) as HTMLButtonElement;
  expect(freeze.disabled).toBe(false);
  expect(
    screen.getByText(/Profiles can be frozen, but inference is blocked/),
  ).toBeTruthy();
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Requested context" }),
    "32768",
  );
  expect(freeze.disabled).toBe(true);
  expect(
    screen.getByText(
      /Check this model with the selected MLX runtime and context/,
    ),
  ).toBeTruthy();
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Requested context" }),
    "65536",
  );
  await user.click(freeze);
  expect(action).toHaveBeenCalledExactlyOnceWith("harness.experiments.start", {
    workspaceId: "w",
    agentId: "default",
    mode: "native",
    taskIds: ["task"],
    contextSize: 65536,
    workflowIds: [],
    fixturePackId: "coding-diagnostics-v2",
  });
  expect(state.settings.defaultCompletionRuntimeId).not.toBe("mlx-v1");
});
