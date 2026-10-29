import test from "node:test";
import assert from "node:assert/strict";
import {
  restoreSelection,
  mergeEvents,
  mergeSessionPage,
  currentRun,
  streamedOutput,
  eventStatus,
  createRefreshCoordinator,
  reconcileSavedFile,
  savedMessageOutcome,
} from "../src/lib/conversation.ts";

test("saved incomplete replies retain their own outcome after a later turn", () => {
  const partial = {
    role: "assistant",
    runId: "old",
    content: "Unfinished answer",
    incomplete: true,
  };
  const old = {
    id: "old",
    status: "failed",
    incomplete: true,
    error: "Generation length limit reached",
  };
  const latest = { id: "new", status: "completed" };
  assert.deepEqual(savedMessageOutcome(partial, old), {
    label: "Incomplete response",
    detail: "Generation length limit reached",
  });
  assert.deepEqual(
    savedMessageOutcome({ ...partial, incomplete: undefined }, old),
    {
      label: "Incomplete response",
      detail: "Generation length limit reached",
    },
  );
  assert.equal(
    savedMessageOutcome(partial, latest).label,
    "Incomplete response",
  );
  assert.equal(partial.content, "Unfinished answer");
  assert.equal(
    savedMessageOutcome({ role: "assistant", runId: "new" }, old),
    undefined,
  );
});

test("a failed run is distinct from an incomplete answer or an ordinary recovered tool failure", () => {
  const message = { role: "assistant", runId: "r", content: "Observed result" };
  assert.deepEqual(
    savedMessageOutcome(message, {
      id: "r",
      status: "failed",
      error: "Command failed",
    }),
    {
      label: "Run failed",
      detail: "Command failed",
    },
  );
  assert.equal(
    savedMessageOutcome(message, {
      id: "r",
      status: "completed",
      receipt: { failedTools: 1 },
    }),
    undefined,
  );
  assert.equal(
    savedMessageOutcome(
      { ...message, role: "tool", incomplete: true },
      { id: "r", status: "failed", incomplete: true },
    ),
    undefined,
  );
  assert.equal(
    savedMessageOutcome(
      { ...message, role: "user" },
      { id: "r", status: "failed" },
    ),
    undefined,
  );
  assert.equal(
    savedMessageOutcome(message, { id: "r", status: "cancelled" }).label,
    "Run cancelled",
  );
  assert.equal(
    savedMessageOutcome(message, { id: "r", status: "interrupted" }).label,
    "Run interrupted",
  );
});

test("restores only a session belonging to the loaded workspace", () => {
  const workspaces = [{ id: "a" }, { id: "b" }],
    sessions = [{ id: "chat", workspaceId: "a" }];
  assert.deepEqual(restoreSelection(workspaces, sessions, "a", "chat"), {
    workspaceId: "a",
    sessionId: "chat",
  });
  assert.deepEqual(restoreSelection(workspaces, sessions, "b", "chat"), {
    workspaceId: "b",
    sessionId: "",
  });
});
test("merges overlapping cursor pages without losing old text or duplicating it", () => {
  const first = {
    id: "1",
    seq: 1,
    runId: "run",
    type: "text",
    detail: "Hello",
  };
  const events = mergeEvents(
    [first],
    [
      { ...first },
      { id: "2", seq: 2, runId: "run", type: "text", detail: " world" },
    ],
  );
  assert.equal(events[0], first);
  assert.equal(
    streamedOutput({ id: "run", status: "cancelled" }, [], events),
    "Hello world",
  );
  assert.equal(
    streamedOutput(
      { id: "run" },
      [{ role: "assistant", runId: "run" }],
      events,
    ),
    "",
  );
});
test("selects active continuation and separates model turns without inventing text", () => {
  assert.equal(
    currentRun([
      { id: "old", startedAt: "2026-10-01", status: "running" },
      { id: "new", startedAt: "2026-10-02", status: "completed" },
    ]).id,
    "old",
  );
  assert.equal(
    streamedOutput(
      { id: "r" },
      [],
      [
        { runId: "r", type: "text", detail: "Inspecting." },
        { runId: "r", type: "tool_started" },
        { runId: "r", type: "text", detail: "Found it." },
      ],
    ),
    "Inspecting.\n\nFound it.",
  );
  assert.equal(
    eventStatus({ type: "run_completed", data: { status: "failed" } }),
    "failed",
  );
  assert.equal(eventStatus({ type: "tool_failed" }), "failed");
  assert.equal(eventStatus({ type: "approval" }), "approval_requested");
});
test("a post-mutation refresh waits for a fresh snapshot after an in-flight poll", async () => {
  const gates = [];
  let calls = 0;
  const refresh = createRefreshCoordinator(() => {
    calls++;
    return new Promise((resolve) => gates.push(resolve));
  });
  const first = refresh();
  const mutation = refresh(true);
  refresh(true);
  assert.equal(calls, 1);
  gates.shift()();
  await Promise.resolve();
  assert.equal(calls, 2);
  gates.shift()();
  await Promise.all([first, mutation]);
  assert.equal(calls, 2);
});
test("save acknowledgement updates the baseline without erasing typing during save", () => {
  const change = { path: "a.ts", after: "saved", afterHash: "hash2" };
  assert.deepEqual(reconcileSavedFile("saved", "saved plus more", change), {
    file: { path: "a.ts", content: "saved", hash: "hash2" },
    content: "saved plus more",
  });
  assert.equal(reconcileSavedFile("saved", "saved", change).content, "saved");
});
test("cursor pages advance and retain earlier events; legacy pages still load", () => {
  const initial = { messages: [], events: [], cursor: 0, hasMore: false };
  const event = {
    id: "1",
    seq: 50,
    runId: "r",
    type: "text",
    detail: "Beginning",
  };
  const first = mergeSessionPage(initial, {
    messages: [],
    events: [event],
    eventCursor: 50,
    hasMore: true,
  });
  const second = mergeSessionPage(first, {
    messages: [{ id: "answer" }],
    events: [{ id: "2", seq: 52, runId: "r", type: "text", detail: " ending" }],
    eventCursor: 52,
    hasMore: false,
  });
  assert.equal(second.cursor, 52);
  assert.equal(second.events.length, 2);
  assert.equal(second.hasMore, false);
  assert.equal(
    streamedOutput({ id: "r" }, [], second.events),
    "Beginning ending",
  );
  assert.equal(
    mergeSessionPage(initial, { messages: [], events: [event] }).cursor,
    50,
  );
  assert.throws(
    () =>
      mergeSessionPage(first, {
        messages: [],
        events: [],
        eventCursor: 50,
        hasMore: true,
      }),
    /did not advance/,
  );
  assert.equal(mergeEvents(first.events, []), first.events);
});
