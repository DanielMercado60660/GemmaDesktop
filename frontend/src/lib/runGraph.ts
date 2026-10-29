import type { Event, Run } from "../types";

export interface RunGraphProjection {
  runId: string;
  source: "checkpoint" | "events";
  reason?: string;
  workflow?: {
    id: string;
    name: string;
    nodes: Array<{
      id: string;
      type: string;
      label: string;
      position?: { x: number; y: number };
    }>;
    edges: Array<{
      id: string;
      source: string;
      target: string;
      condition?: string;
    }>;
  };
}
export type GraphLoadState =
  | { status: "idle"; id: "" }
  | { status: "loading"; id: string }
  | { status: "ready"; id: string; projection: RunGraphProjection }
  | { status: "error"; id: string; error: string };

export function createRunGraphLoader(
  fetchGraph: (id: string) => Promise<RunGraphProjection>,
  publish: (state: GraphLoadState) => void,
) {
  let generation = 0;
  return {
    invalidate() {
      generation++;
    },
    async select(id: string) {
      const request = ++generation;
      if (!id) {
        publish({ status: "idle", id: "" });
        return;
      }
      publish({ status: "loading", id });
      try {
        const projection = await fetchGraph(id);
        if (request !== generation) return;
        if (
          projection?.runId !== id ||
          !["checkpoint", "events"].includes(projection.source) ||
          (projection.source === "checkpoint" &&
            (!Array.isArray(projection.workflow?.nodes) ||
              !Array.isArray(projection.workflow?.edges)))
        ) {
          throw new Error(
            "The saved run diagram response is invalid. Retry to load it again.",
          );
        }
        publish({ status: "ready", id, projection });
      } catch (error) {
        if (request === generation)
          publish({
            status: "error",
            id,
            error:
              error instanceof Error
                ? error.message
                : "The saved run diagram could not be loaded.",
          });
      }
    },
  };
}

export interface GraphItem {
  id: string;
  label: string;
  kind: string;
  status: string;
  tone: "working" | "finished" | "error" | "neutral";
  position: { x: number; y: number };
  events: Event[];
  note?: string;
}
export interface GraphLink {
  id: string;
  source: string;
  target: string;
  label: string;
  kind: "definition" | "ownership";
}
const nodeKey = (id: string) => `step:${id}`;
const ignored = new Set([
  "text",
  "thinking",
  "adk_event",
  "generation_metrics",
  "token_accounting",
  "context_prepared",
]);
const nodeKinds = new Set(["node_started", "node_completed", "node_failed"]);
const toolKinds = new Set([
  "tool_started",
  "tool_completed",
  "tool_failed",
  "tool_validation_failed",
]);
const active = new Set(["running", "queued", "waiting_approval", "cancelling"]);
const dataOf = (event: Event): Record<string, unknown> =>
  event.data && typeof event.data === "object"
    ? (event.data as Record<string, unknown>)
    : {};
const stringOf = (value: unknown) => (typeof value === "string" ? value : "");

function outcome(event: Event | undefined, run: Run, tool = false) {
  if (event?.type === "tool_validation_failed")
    return { status: "Rejected before execution", tone: "error" as const };
  if (event?.type.endsWith("_failed"))
    return { status: "Failed", tone: "error" as const };
  if (event?.type.endsWith("_completed"))
    return { status: "Completed", tone: "finished" as const };
  if (!event)
    return { status: "No execution recorded", tone: "neutral" as const };
  if (active.has(run.status))
    return { status: "Started", tone: "working" as const };
  return {
    status: tool ? "Outcome not linked" : "Outcome not recorded",
    tone: "neutral" as const,
  };
}

// Definition edges are explicitly labelled as declared, not proof that a
// branch executed. Runtime edges require a recorded owner/parent identity.
// Chronology, label similarity, and fan-out ID naming never invent causality.
export function buildRunGraph(
  projection: RunGraphProjection,
  run: Run,
  history: Event[],
  limit = 160,
) {
  if (projection.runId !== run.id)
    return { nodes: [] as GraphItem[], edges: [] as GraphLink[], omitted: 0 };
  const events = history
    .filter((e) => e.runId === run.id && !ignored.has(e.type))
    .sort((a, b) => a.seq - b.seq);
  const workflow =
    projection.source === "checkpoint" ? projection.workflow : undefined;
  const all = new Map<string, GraphItem>();
  const links: GraphLink[] = [];
  const byOwner = new Map<string, Event[]>();
  for (const e of events) {
    if (e.nodeId) {
      const list = byOwner.get(e.nodeId) || [];
      list.push(e);
      byOwner.set(e.nodeId, list);
    }
  }
  for (const [i, node] of (workflow?.nodes || []).entries()) {
    const related = byOwner.get(node.id) || [];
    const latest = related.filter((e) => nodeKinds.has(e.type)).at(-1);
    const p = node.position;
    all.set(nodeKey(node.id), {
      id: nodeKey(node.id),
      label: node.label || node.id,
      kind: node.type || "Workflow step",
      ...outcome(latest, run),
      position:
        p &&
        Number.isFinite(p.x) &&
        Number.isFinite(p.y) &&
        Math.abs(p.x) < 100000 &&
        Math.abs(p.y) < 100000
          ? p
          : { x: 0, y: i * 110 },
      events: related,
    });
  }
  for (const edge of workflow?.edges || []) {
    const source = nodeKey(edge.source),
      target = nodeKey(edge.target);
    if (all.has(source) && all.has(target))
      links.push({
        id: `definition:${edge.id}`,
        source,
        target,
        kind: "definition",
        label: edge.condition ? `Definition · ${edge.condition}` : "Definition",
      });
  }
  let runtimeIndex = 0;
  const runtimeX =
    Math.max(0, ...Array.from(all.values(), (n) => n.position.x)) + 270;
  for (const [id, related] of byOwner) {
    if (all.has(nodeKey(id))) continue;
    const latest = related.filter((e) => nodeKinds.has(e.type)).at(-1);
    all.set(nodeKey(id), {
      id: nodeKey(id),
      label: latest?.title || id,
      kind: "Runtime step",
      ...outcome(latest, run),
      position: { x: runtimeX, y: runtimeIndex++ * 110 },
      events: related,
      note: `Recorded node ID: ${id}. A parent relationship is shown only when explicitly recorded.`,
    });
  }
  for (const [id, related] of byOwner) {
    const parents = new Set(
      related.map((e) => stringOf(dataOf(e).parentNodeId)).filter(Boolean),
    );
    if (parents.size === 1) {
      const parent = nodeKey([...parents][0]);
      if (
        parent !== nodeKey(id) &&
        workflow?.nodes.some((n) => nodeKey(n.id) === parent)
      )
        links.push({
          id: `parent:${id}`,
          source: parent,
          target: nodeKey(id),
          kind: "ownership",
          label: "Recorded parent",
        });
    }
  }
  const toolGroups = new Map<string, Event[]>();
  for (const e of events) {
    if (!toolKinds.has(e.type)) continue;
    const d = dataOf(e);
    const callId =
      stringOf(d.callId) || (e.type === "tool_started" ? stringOf(d.id) : "");
    // A call ID is scoped to its recorded node and tool. Outcomes without a
    // call ID remain separate; guessing the latest same-named call is unsafe.
    const id = callId
      ? `tool:${JSON.stringify([e.nodeId || "", e.tool || e.title, callId])}`
      : `tool-event:${e.id || e.seq}`;
    const group = toolGroups.get(id) || [];
    group.push(e);
    toolGroups.set(id, group);
  }
  const root = `run:${run.id}`;
  let toolIndex = 0;
  for (const [id, related] of toolGroups) {
    const latest = related.at(-1)!;
    const owner = latest.nodeId ? nodeKey(latest.nodeId) : root;
    if (owner === root && !all.has(root)) {
      all.set(root, {
        id: root,
        label: "Agent run",
        kind: "Run",
        status: run.status.replaceAll("_", " "),
        tone: "neutral",
        position: { x: 0, y: 0 },
        events: events.filter((e) => !e.nodeId),
        note: "This node represents the recorded run. Its related activity has no separate node owner.",
      });
    }
    const unlinked =
      id.startsWith("tool-event:") && latest.type !== "tool_started";
    all.set(id, {
      id,
      label: latest.tool || latest.title || "Tool",
      kind:
        latest.type === "tool_validation_failed"
          ? "Tool proposal"
          : "Tool activity",
      ...outcome(latest, run, true),
      position: { x: runtimeX + 280, y: toolIndex++ * 110 },
      events: related,
      note: unlinked
        ? "This outcome has no recorded call ID. It is not paired with a start event."
        : "Related activity is matched only by the explicit call ID, owner and tool.",
    });
    if (all.has(owner))
      links.push({
        id: `owner:${id}`,
        source: owner,
        target: id,
        kind: "ownership",
        label: "Recorded in",
      });
  }
  // Keep the saved topology, then recent runtime activity. Every omission is
  // visible; full chronological activity remains in the inspector's other tab.
  const values = [...all.values()];
  const definitions = values.filter((n) =>
    workflow?.nodes.some((w) => nodeKey(w.id) === n.id),
  );
  const runtime = values.filter((n) => !definitions.includes(n));
  const nodes = [
    ...definitions.slice(0, limit),
    ...runtime.slice(-Math.max(0, limit - definitions.length)),
  ].slice(0, limit);
  const kept = new Set(nodes.map((n) => n.id));
  return {
    nodes,
    edges: links.filter((e) => kept.has(e.source) && kept.has(e.target)),
    omitted: Math.max(0, all.size - nodes.length),
  };
}
