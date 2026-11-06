import { it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ModelsPage, type Action } from "../../src/Management";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { pullJobFinished } from "../../src/lib/modelDownloads";
import { fixture } from "./fixtures";

it("restores interrupted external downloads as terminal and retries only on user action", async () => {
  const state = fixture();
  state.runtime.pulls = [
    { name: "large-model", state: "interrupted", status: "pulling layers" },
  ];
  const action = vi.fn(async () => ({}));
  render(
    <ConfirmationProvider>
      <ModelsPage
        state={state}
        action={action as Action}
        workspaceId="w"
        sessionId="s"
      />
    </ConfirmationProvider>,
  );
  expect(screen.getByText(/interrupted/i)).toBeTruthy();
  expect(screen.queryByText("Preparing download")).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  expect(action).not.toHaveBeenCalled();
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Retry download" }));
  expect(action).toHaveBeenCalledExactlyOnceWith("model.pull", {
    name: "large-model",
  });
  expect(
    pullJobFinished({ state: "interrupted", status: "pulling layers" }),
  ).toBe(true);
  expect(pullJobFinished({ state: "running", status: "success" })).toBe(false);
});
