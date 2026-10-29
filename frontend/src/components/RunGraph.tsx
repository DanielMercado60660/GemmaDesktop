import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Background,
  Controls,
  ReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Waypoints } from "lucide-react";
import { api } from "../bridge";
import type { Event, Run } from "../types";
import {
  buildRunGraph,
  createRunGraphLoader,
  type GraphLoadState,
  type RunGraphProjection,
} from "../lib/runGraph";
import { Button, Empty, Modal } from "./ui";

export default function RunGraph({
  run: current,
  runs,
  history,
  theme,
  renderEvents,
}: {
  run?: Run;
  runs: Run[];
  history: Event[];
  theme: string;
  renderEvents: (events: Event[]) => ReactNode;
}) {
  const [chosenRun, setChosenRun] = useState("");
  const run = runs.find((r) => r.id === chosenRun) || current;
  const [loaded, setLoaded] = useState<GraphLoadState>({
    status: "idle",
    id: "",
  });
  const [selected, setSelected] = useState("");
  const loader = useMemo(
    () =>
      createRunGraphLoader(
        (id) => api<RunGraphProjection>("run.graph", { id }),
        setLoaded,
      ),
    [],
  );
  useEffect(() => {
    setSelected("");
    void loader.select(run?.id || "");
    return () => loader.invalidate();
  }, [loader, run?.id]);
  const projection =
    loaded.status === "ready" && loaded.id === run?.id
      ? loaded.projection
      : undefined;
  const graph = useMemo(
    () =>
      run && projection
        ? buildRunGraph(projection, run, history)
        : { nodes: [], edges: [], omitted: 0 },
    [projection, run, history],
  );
  const selectedNode = graph.nodes.find((n) => n.id === selected);
  const nodes: Node[] = graph.nodes.map((n) => ({
    id: n.id,
    position: n.position,
    selected: selected === n.id,
    data: {
      label: (
        <button
          type="button"
          className="execution-node nodrag nopan"
          onClick={() => setSelected(n.id)}
          aria-label={`Inspect ${n.label}: ${n.status}`}
        >
          <span>{n.label}</span>
          <small>
            {n.kind} · {n.status}
          </small>
        </button>
      ),
    },
    className: `flow-node ${n.tone === "finished" ? "node-finished" : n.tone === "error" ? "node-error" : n.tone === "working" ? "node-working" : ""}`,
  }));
  const edges: Edge[] = graph.edges.map((e) => ({
    ...e,
    style: e.kind === "definition" ? { strokeDasharray: "5 5" } : undefined,
    labelStyle: { fontSize: 9 },
    labelBgStyle: { fill: theme === "light" ? "#eef2f8" : "#182130" },
  }));
  return (
    <div className="run-graph-panel">
      <div className="run-graph-heading">
        <label className="field">
          <span>Recorded run</span>
          <select
            value={run?.id || ""}
            onChange={(e) => {
              setSelected("");
              setChosenRun(e.target.value);
            }}
            aria-label="Select recorded run"
          >
            {!runs.length && <option value="">No runs yet</option>}
            {runs.map((r) => (
              <option key={r.id} value={r.id}>
                {new Date(r.startedAt).toLocaleString()} ·{" "}
                {r.status.replaceAll("_", " ")} · {r.prompt.slice(0, 60)}
              </option>
            ))}
          </select>
        </label>
        {projection && (
          <p className="muted small">
            {projection.source === "checkpoint"
              ? `Saved definition: ${projection.workflow?.name || "Workflow"}. Dashed lines show declared paths, not traversal.`
              : projection.reason || "Recorded activity only."}
          </p>
        )}
        {graph.nodes.length > 0 && (
          <label className="field">
            <span>Inspect recorded node</span>
            <select
              value={selectedNode?.id || ""}
              onChange={(e) => setSelected(e.target.value)}
              aria-label="Inspect recorded node"
            >
              <option value="">Choose a step or tool…</option>
              {graph.nodes.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.label} · {n.status}
                </option>
              ))}
            </select>
          </label>
        )}
        {projection && (
          <p className="muted small">
            Select a node for activity from loaded history. Solid lines show
            recorded ownership, not order.
            {graph.omitted > 0
              ? ` ${graph.omitted} nodes omitted from this bounded view; full activity remains in the Activity tab.`
              : ""}
          </p>
        )}
      </div>
      {!run ? (
        <Empty
          icon={<Waypoints size={25} />}
          title="A view of the process"
          description="Run activity and saved workflow definitions will appear here."
        />
      ) : loaded.status === "error" && loaded.id === run.id ? (
        <div className="run-graph-heading" role="alert">
          <p>{loaded.error}</p>
          <Button onClick={() => void loader.select(run.id)}>
            Retry saved diagram
          </Button>
        </div>
      ) : !projection ? (
        <p className="run-graph-heading muted" role="status">
          Loading saved run diagram…
        </p>
      ) : graph.nodes.length ? (
        <div className="execution-graph">
          <ReactFlow
            key={run.id}
            nodes={nodes}
            edges={edges}
            fitView
            minZoom={0.1}
            fitViewOptions={{ minZoom: 0.1, maxZoom: 1, padding: 0.15 }}
            nodesDraggable={false}
            nodesConnectable={false}
            onNodeClick={(_, node) => setSelected(node.id)}
            colorMode={theme === "light" ? "light" : "dark"}
            proOptions={{ hideAttribution: false }}
          >
            <Background gap={20} size={1} />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>
      ) : (
        <Empty
          icon={<Waypoints size={25} />}
          title="No recorded graph activity"
          description="No saved steps or tool events are available for this run yet."
        />
      )}
      <Modal
        open={!!selectedNode}
        onClose={() => setSelected("")}
        title={selectedNode?.label || "Recorded node"}
        description={
          selectedNode
            ? `${selectedNode.kind} · ${selectedNode.status}`
            : undefined
        }
        wide
      >
        {selectedNode && (
          <>
            <p className="muted small">
              {selectedNode.note ||
                "This step comes from the workflow saved for this run. Its state is derived only from recorded node events."}
            </p>
            <div className="run-graph-details">
              {selectedNode.events.length ? (
                renderEvents(selectedNode.events)
              ) : (
                <p className="muted">
                  No related activity is recorded in the loaded history.
                </p>
              )}
            </div>
          </>
        )}
      </Modal>
    </div>
  );
}
