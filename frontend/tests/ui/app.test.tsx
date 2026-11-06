import { beforeEach, it, expect, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import App from "../../src/App";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { api } from "../../src/bridge";
import { fixture, deferred } from "./fixtures";
import type { State } from "../../src/types";

vi.mock("../../src/bridge", () => ({ api: vi.fn(), openExternal: vi.fn() }));
vi.mock("../../src/components/RunGraph", () => ({
  default: () => <div>Run graph</div>,
}));
vi.mock("@xyflow/react", () => ({
  ReactFlow: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  applyNodeChanges: (_: unknown, nodes: unknown) => nodes,
  applyEdgeChanges: (_: unknown, edges: unknown) => edges,
  addEdge: (edge: unknown, edges: unknown[]) => [...edges, edge],
}));
let state: State;
beforeEach(() => {
  state = fixture();
  localStorage.setItem("gemma.workspace", "w");
  localStorage.setItem("gemma.session", "s");
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "state") return state;
    if (method === "session.get")
      return { messages: [], events: [], cursor: 0, hasMore: false };
    if (method === "draft.get")
      return { ...(params as object), text: "", attachments: [], revision: 0 };
    if (method === "draft.save")
      return {
        ...(params as { expectedRevision: number }),
        revision: (params as { expectedRevision: number }).expectedRevision + 1,
      };
    if (method === "model.refresh") return {};
    throw new Error(`Unexpected bridge request: ${method}`);
  });
});
function show() {
  render(
    <ConfirmationProvider>
      <App />
    </ConfirmationProvider>,
  );
  return userEvent.setup();
}
it("blocks sending a missing default model even when the runtime is reachable", async () => {
  state.models = [];
  const user = show();
  const composer = await screen.findByRole("textbox", {
    name: "Message Gemma",
  });
  await user.type(composer, "Inspect the project");
  expect(
    (screen.getByRole("button", { name: "Send message" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(
    screen.getAllByText(/selected model.*not installed/).length,
  ).toBeGreaterThan(0);
  expect(screen.getByRole("button", { name: "Choose model" })).toBeTruthy();
});
it("allows an installed completion model to load on demand after restoring the draft", async () => {
  const user = show();
  const composer = await screen.findByRole("textbox", {
    name: "Message Gemma",
  });
  await user.type(composer, "Inspect the project");
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Send message",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  expect(
    vi.mocked(api).mock.calls.some(([method]) => method === "draft.get"),
  ).toBe(true);
});
it("blocks uncertain capabilities and offers a metadata refresh", async () => {
  state.models[0].capabilities = [];
  const user = show();
  await user.type(
    await screen.findByRole("textbox", { name: "Message Gemma" }),
    "Inspect the project",
  );
  expect(
    (screen.getByRole("button", { name: "Send message" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Refresh models" }));
  expect(
    vi.mocked(api).mock.calls.some(([method]) => method === "model.refresh"),
  ).toBe(true);
});
it("guards navigation away from a dirty workflow and retains the edits after cancel", async () => {
  const user = show();
  await screen.findByRole("textbox", { name: "Message Gemma" });
  const nav = within(
    screen.getByRole("navigation", { name: "Main navigation" }),
  );
  await user.click(nav.getByRole("button", { name: "Workflows" }));
  await user.click(await screen.findByRole("button", { name: "Open Review" }));
  await user.type(
    screen.getByRole("textbox", { name: "Workflow name" }),
    " changed",
  );
  await user.click(nav.getByRole("button", { name: "Models" }));
  expect(screen.getByRole("alertdialog")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(
    (screen.getByRole("textbox", { name: "Workflow name" }) as HTMLInputElement)
      .value,
  ).toBe("Review changed");
});

it("uses and locks a workspace-qualified model without changing personal defaults", async () => {
  state.models.push({ ...state.models[0], name: "promoted-model" });
  state.effectiveProfiles = [
    {
      workspaceId: "w",
      agentId: "default",
      armId: "arm",
      name: "Qualified coding profile",
      model: "promoted-model",
      runtimeId: "ollama-default",
      contextSize: 65536,
      status: "configured",
    },
  ];
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "run.start"
      ? { id: "run", clearedDraftRevision: 2 }
      : original(method, params),
  );
  const user = show();
  await user.type(
    await screen.findByRole("textbox", { name: "Message Gemma" }),
    "Inspect the project",
  );
  const model = screen.getByRole("combobox", {
    name: "Model for this conversation",
  }) as HTMLSelectElement;
  expect(model.value).toBe("promoted-model");
  expect(model.disabled).toBe(true);
  expect(
    screen.getByText(/Workspace profile: Qualified coding profile/),
  ).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "run.start",
      expect.objectContaining({ sessionId: "s", model: "promoted-model" }),
    ),
  );
  expect(state.settings.defaultModel).toBe("gemma4:12b-mlx");
});
it("loads an earlier page using the captured revision and prepends it to the conversation", async () => {
  const original = vi.mocked(api).getMockImplementation()!;
  const latest = {
    id: "latest",
    sessionId: "s",
    role: "user",
    content: "Latest message",
    createdAt: "2026-11-04T10:00:00Z",
  };
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "session.get")
      return {
        messages: [latest],
        messageCursor: "revision",
        messageBefore: "oldest-latest",
        messagesHaveMore: true,
        events: [],
        eventCursor: 0,
        hasMore: false,
      };
    if (method === "session.messages")
      return {
        messages: [{ ...latest, id: "earlier", content: "Earlier message" }],
        messageCursor: "revision",
        messageBefore: "oldest-earlier",
        messagesHaveMore: false,
      };
    return original(method, params);
  });
  const user = show();
  await user.click(
    await screen.findByRole("button", { name: "Load earlier messages" }),
  );
  await screen.findByText("Earlier message");
  expect(api).toHaveBeenCalledWith("session.messages", {
    id: "s",
    before: "oldest-latest",
    revision: "revision",
    limit: 100,
  });
  expect(
    Array.from(document.querySelectorAll("[data-message-id]")).map(
      (element) => (element as HTMLElement).dataset.messageId,
    ),
  ).toEqual(["earlier", "latest"]);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("retains newly loaded older messages when an in-flight poll omits unchanged bodies", async () => {
  state.models[0].capabilities = [];
  const original = vi.mocked(api).getMockImplementation()!;
  const polling = deferred<unknown>();
  let calls = 0;
  const latest = {
    id: "latest",
    sessionId: "s",
    role: "user",
    content: "Latest message",
    createdAt: "2026-11-04T10:00:00Z",
  };
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "session.get") {
      calls++;
      if (calls > 1) return polling.promise;
      return {
        messages: [latest],
        messageCursor: "revision",
        messageBefore: "latest",
        messagesHaveMore: true,
        events: [],
        eventCursor: 0,
        hasMore: false,
      };
    }
    if (method === "session.messages")
      return {
        messages: [{ ...latest, id: "older", content: "Older loaded message" }],
        messageCursor: "revision",
        messageBefore: "older",
        messagesHaveMore: false,
      };
    return original(method, params);
  });
  const user = show();
  await screen.findByRole("button", { name: "Load earlier messages" });
  await user.click(screen.getByRole("button", { name: "Refresh models" }));
  await waitFor(() => expect(calls).toBeGreaterThan(1));
  await user.click(
    screen.getByRole("button", { name: "Load earlier messages" }),
  );
  await screen.findByText("Older loaded message");
  await act(async () =>
    polling.resolve({
      messages: [],
      messagesUnchanged: true,
      messageCursor: "revision",
      events: [],
      eventCursor: 0,
      hasMore: false,
    }),
  );
  expect(screen.getByText("Older loaded message")).toBeTruthy();
});

it("enters focused continuation explicitly and sends its saved draft with the captured source", async () => {
  state.runs = [
    {
      id: "source",
      sessionId: "s",
      workspaceId: "w",
      agentId: "default",
      status: "completed",
      model: state.settings.defaultModel,
      prompt: "Prior task",
      startedAt: "2026-11-04T10:00:00Z",
      verification: "unverified",
    },
  ];
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "context.get") return { version: 1, sources: [] };
    if (method === "context.focus")
      return { id: "focused", clearedDraftRevision: 4 };
    return original(method, params);
  });
  const user = show();
  const input = await screen.findByRole("textbox", { name: "Message Gemma" });
  await user.type(input, "Check the remaining failure");
  await user.click(screen.getByRole("button", { name: "Context" }));
  await user.click(
    await screen.findByRole("button", { name: "Use focused continuation" }),
  );
  expect((input as HTMLTextAreaElement).value).toBe(
    "Check the remaining failure",
  );
  expect(screen.getByText("Focused continuation")).toBeTruthy();
  expect(
    (screen.getByRole("combobox", { name: "Agent" }) as HTMLSelectElement)
      .disabled,
  ).toBe(true);
  expect(
    vi.mocked(api).mock.calls.some(([method]) => method === "context.focus"),
  ).toBe(false);
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "context.focus",
      expect.objectContaining({
        runId: "source",
        sessionId: "s",
        agentId: "default",
        prompt: "Check the remaining failure",
        draftSessionId: "s",
        draftRevision: expect.any(Number),
      }),
    ),
  );
  expect(api).toHaveBeenCalledWith(
    "draft.save",
    expect.objectContaining({
      text: "Check the remaining failure",
      focusedRunId: "source",
    }),
  );
  expect(
    vi.mocked(api).mock.calls.some(([method]) => method === "run.start"),
  ).toBe(false);
  await waitFor(() =>
    expect(screen.queryByText("Focused continuation")).toBeNull(),
  );
  expect((input as HTMLTextAreaElement).value).toBe("");
});

it("keeps a failed focused submission and can return to ordinary mode without losing text", async () => {
  state.runs = [
    {
      id: "source",
      sessionId: "s",
      workspaceId: "w",
      agentId: "default",
      status: "failed",
      model: state.settings.defaultModel,
      prompt: "Prior task",
      startedAt: "2026-11-04T10:00:00Z",
    },
  ];
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) => {
    if (method === "draft.get")
      return {
        ...(params as object),
        text: "Saved focused task",
        attachments: ["src/main.go"],
        focusedRunId: "source",
        revision: 1,
      };
    if (method === "context.focus") throw new Error("Source authority changed");
    return original(method, params);
  });
  const user = show();
  await screen.findByText("Focused continuation");
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await screen.findByText("Source authority changed");
  expect(
    (
      screen.getByRole("textbox", {
        name: "Message Gemma",
      }) as HTMLTextAreaElement
    ).value,
  ).toBe("Saved focused task");
  expect(screen.getByText("Focused continuation")).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Remove attachment src/main.go" }),
  ).toBeTruthy();
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Use ordinary continuation",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  await user.click(
    screen.getByRole("button", { name: "Use ordinary continuation" }),
  );
  expect(screen.queryByText("Focused continuation")).toBeNull();
  expect(
    (
      screen.getByRole("textbox", {
        name: "Message Gemma",
      }) as HTMLTextAreaElement
    ).value,
  ).toBe("Saved focused task");
});

it("restores focused intent and blocks a stale source instead of sending as ordinary", async () => {
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "draft.get"
      ? {
          ...(params as object),
          text: "Retained focus",
          attachments: [],
          focusedRunId: "old-source",
          revision: 1,
        }
      : original(method, params),
  );
  const user = show();
  await screen.findByText("Focused continuation");
  expect(
    screen.getByText(/source is unavailable or is no longer/),
  ).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Send message" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(
    screen.getByRole("button", { name: "Use ordinary continuation" }),
  );
  expect(
    (screen.getByRole("button", { name: "Send message" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
  expect(
    (
      screen.getByRole("textbox", {
        name: "Message Gemma",
      }) as HTMLTextAreaElement
    ).value,
  ).toBe("Retained focus");
});

function admitRuns() {
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "run.start" || method === "context.focus"
      ? { id: "run", clearedDraftRevision: 2 }
      : original(method, params),
  );
}
const unittest = {
  command: ["python3", "-m", "unittest", "-v"],
  workingDirectory: "",
  protectedPaths: [],
  requireRegressionTest: false,
};

it("sends a declared check command as the run's verification contract and remembers it per workspace", async () => {
  admitRuns();
  const user = show();
  const composer = await screen.findByRole("textbox", {
    name: "Message Gemma",
  });
  expect(screen.queryByRole("textbox", { name: "Check command" })).toBeNull();
  const toggle = screen.getByRole("button", { name: "Check" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  await user.click(toggle);
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  const check = screen.getByRole("textbox", { name: "Check command" });
  await waitFor(() => expect(document.activeElement).toBe(check));
  expect(check.getAttribute("placeholder")).toBe("python3 -m unittest -v");
  expect(
    screen.getByText(/Gemma runs this read-only to confirm the task/),
  ).toBeTruthy();
  await user.type(check, "python3 -m unittest -v");
  expect(localStorage.getItem("gemma.checkCommand.w")).toBe(
    "python3 -m unittest -v",
  );
  expect(screen.getByRole("button", { name: "Check (on)" })).toBeTruthy();
  await user.type(composer, "Fix the calculator");
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Send message",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "run.start",
      expect.objectContaining({
        sessionId: "s",
        prompt: "Fix the calculator",
        verificationContract: unittest,
      }),
    ),
  );
});

it("blocks sending an invalid check command and explains why", async () => {
  admitRuns();
  const user = show();
  await user.type(
    await screen.findByRole("textbox", { name: "Message Gemma" }),
    "Fix the calculator",
  );
  await user.click(screen.getByRole("button", { name: "Check" }));
  const check = screen.getByRole("textbox", { name: "Check command" });
  await user.type(check, "python3 -m unittest | tee log{Enter}");
  expect(check.getAttribute("aria-invalid")).toBe("true");
  expect(
    screen.getByText("Use a single command — shells aren't available."),
  ).toBeTruthy();
  const send = screen.getByRole("button", {
    name: "Send message",
  }) as HTMLButtonElement;
  expect(send.disabled).toBe(true);
  // An invalid command cannot be hidden away while it blocks sending.
  await user.click(screen.getByRole("button", { name: "Check" }));
  expect(screen.getByRole("textbox", { name: "Check command" })).toBe(check);
  expect(
    vi.mocked(api).mock.calls.some(([method]) => method === "run.start"),
  ).toBe(false);
  await user.clear(check);
  await user.type(check, "python3 -m unittest -v");
  await waitFor(() => expect(send.disabled).toBe(false));
  await user.click(send);
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "run.start",
      expect.objectContaining({ verificationContract: unittest }),
    ),
  );
});

it("sends ordinary runs without a contract when no check is declared", async () => {
  admitRuns();
  const user = show();
  await user.type(
    await screen.findByRole("textbox", { name: "Message Gemma" }),
    "Explain the project",
  );
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Send message",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith("run.start", expect.anything()),
  );
  const [, params] = vi
    .mocked(api)
    .mock.calls.find(([method]) => method === "run.start")!;
  expect(params).not.toHaveProperty("verificationContract");
});

it("keeps a focused continuation on its source contract instead of the saved check", async () => {
  localStorage.setItem("gemma.checkCommand.w", "python3 -m unittest -v");
  state.runs = [
    {
      id: "source",
      sessionId: "s",
      workspaceId: "w",
      agentId: "default",
      status: "completed",
      model: state.settings.defaultModel,
      prompt: "Prior task",
      startedAt: "2026-11-04T10:00:00Z",
      verification: "unverified",
    },
  ];
  admitRuns();
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "draft.get"
      ? {
          ...(params as object),
          text: "Check the remaining failure",
          attachments: [],
          focusedRunId: "source",
          revision: 1,
        }
      : original(method, params),
  );
  const user = show();
  await screen.findByText("Focused continuation");
  expect(
    (screen.getByRole("button", { name: "Check" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "context.focus",
      expect.objectContaining({ runId: "source" }),
    ),
  );
  const [, params] = vi
    .mocked(api)
    .mock.calls.find(([method]) => method === "context.focus")!;
  expect(params).not.toHaveProperty("verificationContract");
});

it("keeps the check command for this window when storage is unavailable", async () => {
  admitRuns();
  const getItem = Storage.prototype.getItem,
    setItem = Storage.prototype.setItem;
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(function (
    this: Storage,
    key: string,
  ) {
    if (key.startsWith("gemma.checkCommand.")) throw new Error("denied");
    return getItem.call(this, key);
  });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
    this: Storage,
    key: string,
    value: string,
  ) {
    if (key.startsWith("gemma.checkCommand.")) throw new Error("denied");
    return setItem.call(this, key, value);
  });
  try {
    const user = show();
    await user.type(
      await screen.findByRole("textbox", { name: "Message Gemma" }),
      "Fix the calculator",
    );
    await user.click(screen.getByRole("button", { name: "Check" }));
    await user.type(
      screen.getByRole("textbox", { name: "Check command" }),
      "python3 -m unittest -v",
    );
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Send message",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "run.start",
        expect.objectContaining({ verificationContract: unittest }),
      ),
    );
  } finally {
    vi.restoreAllMocks();
  }
});

it("restores only this workspace's saved check and sends it while collapsed", async () => {
  localStorage.setItem("gemma.checkCommand.w", "pytest -k 'a or b'");
  localStorage.setItem("gemma.checkCommand.other", "make test | tee log");
  admitRuns();
  const user = show();
  await user.type(
    await screen.findByRole("textbox", { name: "Message Gemma" }),
    "Fix the calculator",
  );
  const toggle = screen.getByRole("button", { name: "Check (on)" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByRole("textbox", { name: "Check command" })).toBeNull();
  await waitFor(() =>
    expect(
      (
        screen.getByRole("button", {
          name: "Send message",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false),
  );
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "run.start",
      expect.objectContaining({
        verificationContract: {
          ...unittest,
          command: ["pytest", "-k", "a or b"],
        },
      }),
    ),
  );
});

it("never lets an invalid saved check block a focused continuation", async () => {
  localStorage.setItem("gemma.checkCommand.w", "python3 -m unittest | tee");
  state.runs = [
    {
      id: "source",
      sessionId: "s",
      workspaceId: "w",
      agentId: "default",
      status: "failed",
      model: state.settings.defaultModel,
      prompt: "Prior task",
      startedAt: "2026-11-04T10:00:00Z",
    },
  ];
  admitRuns();
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "draft.get"
      ? {
          ...(params as object),
          text: "Continue the repair",
          attachments: [],
          focusedRunId: "source",
          revision: 1,
        }
      : original(method, params),
  );
  const user = show();
  await screen.findByText("Focused continuation");
  expect(screen.queryByRole("textbox", { name: "Check command" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(api).toHaveBeenCalledWith(
      "context.focus",
      expect.objectContaining({ runId: "source" }),
    ),
  );
  const [, params] = vi
    .mocked(api)
    .mock.calls.find(([method]) => method === "context.focus")!;
  expect(params).not.toHaveProperty("verificationContract");
});

it("shows the task verification result on an ordinary run", async () => {
  state.runs = [
    {
      id: "checked",
      sessionId: "s",
      workspaceId: "w",
      agentId: "default",
      status: "completed",
      model: state.settings.defaultModel,
      prompt: "Fix the calculator",
      startedAt: "2026-11-04T10:00:00Z",
      finishedAt: "2026-11-04T10:05:00Z",
      verification: "passed",
    },
  ];
  const original = vi.mocked(api).getMockImplementation()!;
  vi.mocked(api).mockImplementation(async (method, params) =>
    method === "session.get"
      ? {
          messages: [
            {
              id: "ask",
              sessionId: "s",
              role: "user",
              content: "Fix the calculator",
              createdAt: "2026-11-04T10:00:00Z",
            },
            {
              id: "answer",
              sessionId: "s",
              role: "assistant",
              content: "The calculator is fixed.",
              createdAt: "2026-11-04T10:05:00Z",
              runId: "checked",
            },
          ],
          events: [],
          cursor: 0,
          hasMore: false,
        }
      : original(method, params),
  );
  show();
  await screen.findByText("The calculator is fixed.");
  // Both the message footer and the run outcome carry the host's result.
  const badges = screen.getAllByRole("status", { name: "Task verification" });
  expect(badges).toHaveLength(2);
  for (const badge of badges)
    expect(badge.textContent).toBe("Task verification passed");
});
