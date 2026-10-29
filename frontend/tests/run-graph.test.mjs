import test from "node:test";
import assert from "node:assert/strict";
import { buildRunGraph, createRunGraphLoader } from "../src/lib/runGraph.ts";

const run = { id: "r", status: "completed" };

test("accounting audit events do not create execution nodes or displace real activity", () => {
  const visible = [{ id: "visible", runId: "r", type: "tool_completed", tool: "read_file", title: "Read source", seq: 1, data: { callId: "call-1" } }];
  const accounting = Array.from({ length: 2500 }, (_, i) => ({ id: `audit-${i}`, runId: "r", type: "token_accounting", nodeId: "provider-only", seq: i + 2, data: { status: "partial" } }));
  const projection = { runId: "r", source: "events" };
  assert.deepEqual(buildRunGraph(projection, run, [...visible, ...accounting]), buildRunGraph(projection, run, visible));
});
const projection = {
  runId: "r",
  source: "checkpoint",
  workflow: {
    id: "flow",
    name: "Frozen workflow",
    nodes: [
      {
        id: "fan",
        type: "fanout",
        label: "Executed fan-out",
        position: { x: 10, y: 20 },
      },
      {
        id: "end",
        type: "output",
        label: "Output",
        position: { x: 10, y: 200 },
      },
    ],
    edges: [{ id: "path", source: "fan", target: "end", condition: "true" }],
  },
};
function event(seq, type, nodeId, data, tool) {
  return {
    id: `e${seq}`,
    seq,
    runId: "r",
    type,
    nodeId,
    title: tool || nodeId || type,
    tool,
    data,
    detail: `Evidence ${seq}`,
  };
}

test("graph uses only saved definition and labels unobserved branches without claiming traversal", () => {
  const original = JSON.stringify(projection);
  const graph = buildRunGraph(projection, run, [
    event(1, "node_completed", "fan"),
  ]);
  assert.equal(
    graph.nodes.find((n) => n.id === "step:fan").label,
    "Executed fan-out",
  );
  assert.equal(
    graph.nodes.find((n) => n.id === "step:fan").status,
    "Completed",
  );
  assert.equal(
    graph.nodes.find((n) => n.id === "step:end").status,
    "No execution recorded",
  );
  assert.deepEqual(
    graph.edges.map((e) => [e.kind, e.label]),
    [["definition", "Definition · true"]],
  );
  assert.equal(JSON.stringify(projection), original);
  assert.deepEqual(buildRunGraph(projection, { id: "another" }, []), {
    nodes: [],
    edges: [],
    omitted: 0,
  });
});

test("runtime fan-out children are retained without invented prefix or chronological parent edges", () => {
  const history = [
    event(1, "node_started", "fan"),
    event(2, "node_started", "fan_item_0"),
    event(3, "node_completed", "fan_item_0"),
    event(4, "node_failed", "other_child", { parentNodeId: "fan" }),
  ];
  const graph = buildRunGraph(projection, run, history);
  assert.equal(
    graph.nodes.find((n) => n.id === "step:fan_item_0").status,
    "Completed",
  );
  assert.equal(
    graph.nodes.find((n) => n.id === "step:other_child").status,
    "Failed",
  );
  assert.equal(
    graph.edges.some((e) => e.target === "step:fan_item_0"),
    false,
  );
  assert.equal(
    graph.edges.find((e) => e.target === "step:other_child").label,
    "Recorded parent",
  );
  assert.deepEqual(
    graph.nodes
      .find((n) => n.id === "step:fan_item_0")
      .events.map((e) => e.seq),
    [2, 3],
  );
});

test("tool events group only by explicit identity and preserve unlinked failures", () => {
  const history = [
    event(
      1,
      "tool_started",
      "fan_item_0",
      { id: "call1", arguments: { path: "a.py" } },
      "read_file",
    ),
    event(2, "tool_started", "fan_item_0", { id: "call2" }, "read_file"),
    event(
      3,
      "tool_failed",
      "fan_item_0",
      { callId: "call1", error: "Read denied" },
      "read_file",
    ),
    event(4, "tool_completed", "fan_item_0", {}, "read_file"),
    event(
      5,
      "tool_validation_failed",
      "fan_item_0",
      { callId: "bad", executed: false },
      "edit_file",
    ),
    {
      ...event(
        6,
        "tool_completed",
        "fan_item_0",
        { callId: "call2" },
        "read_file",
      ),
      runId: "unrelated",
    },
    event(7, "adk_event", "fan_item_0", { private: "hidden" }),
  ];
  const graph = buildRunGraph(projection, run, history);
  const tools = graph.nodes.filter((n) => n.id.startsWith("tool:"));
  const failed = tools.find((n) => n.events.some((e) => e.seq === 1));
  assert.equal(failed.status, "Failed");
  assert.deepEqual(
    failed.events.map((e) => e.seq),
    [1, 3],
  );
  assert.equal(
    tools.find((n) => n.events.some((e) => e.seq === 2)).status,
    "Outcome not linked",
  );
  const unlinked = graph.nodes.find((n) => n.id === "tool-event:e4");
  assert.equal(unlinked.status, "Completed");
  assert.match(unlinked.note, /no recorded call ID/);
  assert.equal(
    tools.find((n) => n.kind === "Tool proposal").status,
    "Rejected before execution",
  );
  assert.ok(
    graph.edges
      .filter((e) => e.target.startsWith("tool"))
      .every(
        (e) => e.source === "step:fan_item_0" && e.label === "Recorded in",
      ),
  );
  assert.ok(
    graph.nodes.every((n) =>
      n.events.every((e) => e.runId === "r" && e.type !== "adk_event"),
    ),
  );
});

test("legacy/event-only graph never fabricates a sequential path and reports display omissions", () => {
  const noDefinition = { runId: "r", source: "events" };
  const history = Array.from({ length: 210 }, (_, i) =>
    event(i + 1, "node_completed", `child${i}`),
  );
  const graph = buildRunGraph(noDefinition, run, history, 80);
  assert.equal(graph.nodes.length, 80);
  assert.equal(graph.omitted, 130);
  assert.deepEqual(graph.edges, []);
  assert.equal(graph.nodes.at(-1).id, "step:child209");
  assert.deepEqual(buildRunGraph(noDefinition, run, []), {
    nodes: [],
    edges: [],
    omitted: 0,
  });
});

test("late graph reads and failures cannot replace the selected run; explicit retry is supported", async () => {
  const pending = new Map();
  let state;
  const loader = createRunGraphLoader(
    (id) =>
      new Promise((resolve, reject) => pending.set(id, { resolve, reject })),
    (value) => {
      state = value;
    },
  );
  const first = loader.select("r");
  const second = loader.select("new");
  pending
    .get("new")
    .resolve({ runId: "new", source: "events", reason: "No saved workflow" });
  await second;
  pending.get("r").reject(new Error("late failure"));
  await first;
  assert.equal(state.status, "ready");
  assert.equal(state.id, "new");
  const wrong = loader.select("wrong");
  pending.get("wrong").resolve(projection);
  await wrong;
  assert.equal(state.status, "error");
  const retry = loader.select("wrong");
  pending.get("wrong").resolve({ runId: "wrong", source: "events" });
  await retry;
  assert.equal(state.status, "ready");
  const cancelled = loader.select("r");
  loader.invalidate();
  await loader.select("");
  pending.get("r").resolve(projection);
  await cancelled;
  assert.deepEqual(state, { status: "idle", id: "" });
});
