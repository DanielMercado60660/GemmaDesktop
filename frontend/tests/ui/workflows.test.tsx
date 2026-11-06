import { describe, it, expect, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import Workflows from "../../src/Workflows";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { fixture, deferred } from "./fixtures";
import type { Action } from "../../src/Management";
import type { Workflow } from "../../src/types";

// Keep the editor controls and real dialogs; graph geometry needs a browser.
vi.mock("@xyflow/react", () => ({
  ReactFlow: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  applyNodeChanges: (_: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_: unknown, edges: unknown) => edges,
  addEdge: (edge: unknown, edges: unknown[]) => [...edges, edge],
}));
function show(
  action: (...args: never[]) => Promise<unknown> = vi.fn(async () => undefined),
) {
  render(
    <ConfirmationProvider>
      <Workflows
        state={fixture()}
        action={action as Action}
        sessionId="s"
        workspaceId="w"
      />
    </ConfirmationProvider>,
  );
  return { action, user: userEvent.setup() };
}
async function editAndPrompt(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Open Review" }));
  await user.clear(screen.getByRole("textbox", { name: "Workflow name" }));
  await user.type(
    screen.getByRole("textbox", { name: "Workflow name" }),
    "Edited review",
  );
  await user.click(screen.getByRole("button", { name: "Save and run" }));
  await user.type(
    screen.getByRole("textbox", { name: "Task" }),
    "Check the project",
  );
}
describe("workflow editor", () => {
  it("saves visible edits and runs only the returned revision", async () => {
    const action = vi.fn(async (method, payload) =>
      method === "workflow.save"
        ? { ...payload, revision: "v2" }
        : { id: "run" },
    );
    const { user } = show(action);
    await editAndPrompt(user);
    await user.click(screen.getByRole("button", { name: "Start workflow" }));
    await waitFor(() => expect(action).toHaveBeenCalledTimes(2));
    expect(action.mock.calls[0]).toEqual([
      "workflow.save",
      expect.objectContaining({
        name: "Edited review",
        expectedRevision: "v1",
      }),
    ]);
    expect(action.mock.calls[1]).toEqual([
      "workflow.run",
      {
        id: "review",
        expectedRevision: "v2",
        sessionId: "s",
        prompt: "Check the project",
      },
    ]);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByRole("status").textContent).toBe("Saved workflow");
  });
  it("keeps unsaved edits and does not run when saving fails", async () => {
    const { user, action } = show();
    await editAndPrompt(user);
    await user.click(screen.getByRole("button", { name: "Start workflow" }));
    expect(action).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("dialog")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("status").textContent).toBe("Unsaved changes");
  });
  it("does not run edits made while the save is pending", async () => {
    const saved = deferred<Workflow>();
    const action = vi.fn(() => saved.promise);
    const { user } = show(action);
    await editAndPrompt(user);
    await user.click(screen.getByRole("button", { name: "Start workflow" }));
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    await user.type(
      screen.getByRole("textbox", { name: "Workflow name" }),
      " changed",
    );
    await act(async () =>
      saved.resolve({
        ...fixture().workflows[0],
        name: "Edited review",
        revision: "v2",
      }),
    );
    expect(action).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alert").textContent).toContain(
      "changed while saving",
    );
    expect(
      (
        screen.getByRole("textbox", {
          name: "Workflow name",
        }) as HTMLInputElement
      ).value,
    ).toBe("Edited review changed");
    expect(screen.getByRole("status").textContent).toBe("Unsaved changes");
  });
  it("requires an explicit discard and restores focus when cancelled", async () => {
    const { user } = show();
    await user.click(screen.getByRole("button", { name: "Open Review" }));
    await user.type(
      screen.getByRole("textbox", { name: "Workflow name" }),
      " changed",
    );
    await user.click(screen.getByRole("button", { name: "Back to workflows" }));
    expect(screen.getByRole("alertdialog").textContent).toContain(
      "Discard unsaved workflow",
    );
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Keep editing" }),
    );
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Back to workflows" }),
    );
    await user.click(screen.getByRole("button", { name: "Back to workflows" }));
    await user.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(screen.getByRole("button", { name: "Open Review" })).toBeTruthy();
  });
});
