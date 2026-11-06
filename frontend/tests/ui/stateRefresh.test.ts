import { it, expect } from "vitest";
import {
  applyStateRefresh,
  normalizeState,
  type StateRefreshPage,
} from "../../src/lib/stateRefresh";
import { mergeSessionPage } from "../../src/lib/conversation";
import { fixture } from "./fixtures";
import type { Event, Message } from "../../src/types";
function page(): StateRefreshPage {
  const state = fixture();
  return {
    changes: [],
    cursor: 10,
    hasMore: false,
    resync: false,
    live: { runtime: state.runtime, models: state.models },
    events: [],
    eventCursor: 20,
    eventsHaveMore: false,
  };
}
it("applies out-of-order changes by sequence and handles deletion without losing unrelated entities", () => {
  const state = fixture();
  state.sessions.push({ ...state.sessions[0], id: "other" });
  const changes = page();
  changes.changes = [
    {
      sequence: 10,
      kind: "workspace",
      id: "w",
      entity: { ...state.workspaces[0], name: "latest" },
    },
    { sequence: 8, kind: "session", id: "other", deleted: true },
    {
      sequence: 7,
      kind: "workspace",
      id: "w",
      entity: { ...state.workspaces[0], name: "earlier" },
    },
  ];
  const result = applyStateRefresh(state, changes, 6, 20)!;
  expect(result.state.workspaces[0].name).toBe("latest");
  expect(result.state.sessions.map((session) => session.id)).toEqual(["s"]);
  expect(state.sessions).toHaveLength(2);
  expect(result.cursor).toBe(10);
});
it("ignores a late old page including its live state and duplicate changes", () => {
  const state = fixture();
  const stale = page();
  stale.cursor = 8;
  stale.live.runtime = { available: false, version: "old" };
  expect(applyStateRefresh(state, stale, 10, 20)?.state).toBe(state);
  const replay = page();
  replay.changes = [{ sequence: 9, kind: "workspace", id: "w", deleted: true }];
  expect(applyStateRefresh(state, replay, 10, 20)?.state.workspaces).toEqual(
    state.workspaces,
  );
});
it("requires a full snapshot for a journal gap, unknown kind or malformed identity", () => {
  const state = fixture();
  expect(
    applyStateRefresh(state, { ...page(), resync: true }, 6, 20),
  ).toBeNull();
  expect(
    applyStateRefresh(
      state,
      {
        ...page(),
        changes: [{ sequence: 7, kind: "future", id: "x", deleted: true }],
      },
      6,
      20,
    ),
  ).toBeNull();
  expect(
    applyStateRefresh(
      state,
      {
        ...page(),
        changes: [
          { sequence: 7, kind: "workspace", id: "w", entity: { id: "other" } },
        ],
      },
      6,
      20,
    ),
  ).toBeNull();
  expect(
    normalizeState({ ...state, entityCursor: 9, workspaces: [] }),
  ).toMatchObject({ entityCursor: 9, workspaces: [] });
});
it("deduplicates activity and bounds it to 500 while preserving event catch-up", () => {
  const state = fixture();
  const event = (seq: number): Event => ({
    id: String(seq),
    seq,
    runId: "r",
    type: "status",
    title: "",
    detail: "",
    createdAt: "",
  });
  state.events = Array.from({ length: 500 }, (_, i) => event(i + 1));
  const change = {
    ...page(),
    events: [event(500), event(501)],
    eventCursor: 501,
    eventsHaveMore: true,
  };
  const result = applyStateRefresh(state, change, 10, 500)!;
  expect(result.state.events).toHaveLength(500);
  expect(result.state.events[0].seq).toBe(2);
  expect(result.state.events.at(-1)?.seq).toBe(501);
  expect(result.hasMore).toBe(true);
  expect(() =>
    applyStateRefresh(state, { ...page(), hasMore: true }, 10, 20),
  ).toThrow("did not advance");
});
it("retains cached message identity when the server sends only unchanged metadata and replaces real edits", () => {
  const messages: Message[] = [
    { id: "m", sessionId: "s", role: "user", content: "hello", createdAt: "" },
  ];
  const history = {
    messages,
    events: [],
    cursor: 0,
    hasMore: false,
    messageCursor: "1:10",
  };
  const unchanged = mergeSessionPage(history, {
    messages: [],
    events: [],
    messagesUnchanged: true,
    messageCursor: "1:10",
  });
  expect(unchanged.messages).toBe(messages);
  const updated = mergeSessionPage(unchanged, {
    messages: [{ ...messages[0], content: "changed" }],
    events: [],
    messageCursor: "1:11",
  });
  expect(updated.messages[0].content).toBe("changed");
  expect(updated.messageCursor).toBe("1:11");
  const removed = mergeSessionPage(updated, {
    messages: [],
    events: [],
    messageCursor: "0:0",
  });
  expect(removed.messages).toEqual([]);
});

it("moves a bounded history window through older messages and resets on a changed revision", async () => {
  const { prependMessages } = await import("../../src/lib/conversation");
  const message = (n: number): Message => ({
    id: String(n),
    sessionId: "s",
    role: "user",
    content: String(n),
    createdAt: "",
  });
  const previous = {
    messages: Array.from({ length: 1000 }, (_, i) => message(i + 100)),
    events: [],
    cursor: 20,
    hasMore: false,
    messageCursor: "stable",
    messageBefore: "100",
    messagesHaveMore: true,
  };
  const older = prependMessages(previous, {
    messages: Array.from({ length: 101 }, (_, i) => message(i)),
    messageCursor: "stable",
    messageBefore: "0",
    messagesHaveMore: false,
  });
  expect(older.messages).toHaveLength(1000);
  expect(older.messages[0].id).toBe("0");
  expect(older.messages.at(-1)?.id).toBe("999");
  expect(older.newerMessages).toBe(true);
  expect(() =>
    prependMessages(previous, {
      messages: [],
      messageCursor: "changed",
      messageBefore: "0",
      messagesHaveMore: false,
    }),
  ).toThrow("Conversation changed");
  const latest = mergeSessionPage(older, {
    messages: [message(1100)],
    events: [],
    messageCursor: "changed",
    messageBefore: "1100",
    messagesHaveMore: true,
  });
  expect(latest.messages.map((item) => item.id)).toEqual(["1100"]);
  expect(latest.newerMessages).toBe(false);
});

it("preserves unchanged live objects and advances their response hash", () => {
  const state = fixture();
  state.liveCursor = "before";
  const result = applyStateRefresh(
    state,
    { ...page(), live: {}, liveCursor: "after" },
    10,
    20,
  )!;
  expect(result.state.runtime).toBe(state.runtime);
  expect(result.state.models).toBe(state.models);
  expect(result.state.managedRuntime).toBe(state.managedRuntime);
  expect(result.state.liveCursor).toBe("after");
  const stale = applyStateRefresh(
    result.state,
    { ...page(), cursor: 9, liveCursor: "stale" },
    10,
    20,
  )!;
  expect(stale.state.liveCursor).toBe("after");
});

it("keeps the entire state identity for a no-change poll", () => {
  const state = fixture();
  state.entityCursor = 10;
  state.liveCursor = "unchanged";
  const result = applyStateRefresh(
    state,
    { ...page(), live: {}, liveCursor: "unchanged" },
    10,
    20,
  )!;
  expect(result.state).toBe(state);
});
