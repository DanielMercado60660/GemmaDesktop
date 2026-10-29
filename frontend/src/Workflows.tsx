import { useCallback, useEffect, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  type Node,
  type Edge,
  type Connection,
  type NodeChange,
  type EdgeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  GitBranch,
  Plus,
  Play,
  Save,
  Trash2,
  ArrowLeft,
  ShieldCheck,
  Split,
  Merge,
  CheckSquare,
  Bot,
  Waypoints,
} from "lucide-react";
import { Button, Empty, Field, Modal, SectionHeader } from "./components/ui";
import type { Workflow, WorkflowEdge } from "./types";
import { useConfirmation } from "./components/Confirmation";
import type { PageProps } from "./Management";
import { workflowEditorNodes, workflowSaveNodes } from "./lib/workflowEditor";
const kinds = [
  ["agent", "Agent"],
  ["condition", "Condition"],
  ["approval", "Approval"],
  ["parallel", "Parallel split"],
  ["fanout", "Dynamic fan-out"],
  ["join", "Join"],
  ["output", "Output"],
];
function icon(type: string) {
  return type === "approval" ? (
    <ShieldCheck size={16} />
  ) : type === "condition" ? (
    <GitBranch size={16} />
  ) : type === "parallel" || type === "fanout" ? (
    <Split size={16} />
  ) : type === "join" ? (
    <Merge size={16} />
  ) : type === "output" ? (
    <CheckSquare size={16} />
  ) : (
    <Bot size={16} />
  );
}
export default function Workflows({ state, action, sessionId }: PageProps) {
  const confirmAction = useConfirmation();
  const [draft, setDraft] = useState<Workflow | null>(null),
    [nodes, setNodes] = useState<Node[]>([]),
    [edges, setEdges] = useState<Edge[]>([]),
    [selected, setSelected] = useState(""),
    [edgeSelected, setEdgeSelected] = useState(""),
    [saving, setSaving] = useState(false),
    [saveError, setSaveError] = useState(""),
    [runModal, setRunModal] = useState(false),
    [prompt, setPrompt] = useState("");
  const selectedNode = nodes.find((n) => n.id === selected),
    selectedEdge = edges.find((e) => e.id === edgeSelected);
  const open = (workflow?: Workflow) => {
    const w = workflow || {
      id: "",
      name: "Untitled workflow",
      description: "",
      nodes: [],
      edges: [],
    };
    setDraft({ ...w });
    setNodes(workflowEditorNodes(w.nodes));
    setSaveError("");
    setEdges(
      w.edges.map((e) => ({
        ...e,
        data: { condition: e.condition },
        label: e.label || e.condition,
      })),
    );
    setSelected("");
    setEdgeSelected("");
  };
  const onNodesChange = useCallback(
    (changes: NodeChange[]) =>
      setNodes((nds) => applyNodeChanges(changes, nds)),
    [],
  );
  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) =>
      setEdges((eds) => applyEdgeChanges(changes, eds)),
    [],
  );
  const onConnect = useCallback(
    (connection: Connection) =>
      setEdges((eds) =>
        addEdge({ ...connection, id: crypto.randomUUID() }, eds),
      ),
    [],
  );
  function updateNode(key: string, value: string) {
    setNodes((nds) =>
      nds.map((n) =>
        n.id === selected
          ? {
              ...n,
              data: { ...n.data, [key]: value },
              className:
                key === "type" ? `flow-node node-${value}` : n.className,
            }
          : n,
      ),
    );
  }
  function add(type: string) {
    const id = `step_${crypto.randomUUID()}`;
    setNodes((nds) => [
      ...nds,
      {
        id,
        position: {
          x: 80 + (nds.length % 3) * 230,
          y: 80 + Math.floor(nds.length / 3) * 150,
        },
        data: {
          type,
          label: kinds.find((x) => x[0] === type)?.[1] || type,
          prompt: "",
        },
        className: `flow-node node-${type}`,
      },
    ]);
    setSelected(id);
    setEdgeSelected("");
  }
  async function save() {
    if (!draft) return;
    setSaveError("");
    let savedNodes;
    try {
      savedNodes = workflowSaveNodes(nodes);
    } catch (error) {
      setSaveError(
        error instanceof Error
          ? error.message
          : "Unable to save workflow items.",
      );
      return;
    }
    const payload = {
      ...draft,
      nodes: savedNodes,
      edges: edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        label: typeof e.label === "string" ? e.label : undefined,
        condition: e.data?.condition ? String(e.data.condition) : undefined,
      })) as WorkflowEdge[],
    };
    setSaving(true);
    try {
      const r = await action<Workflow>("workflow.save", payload);
      if (r) setDraft({ ...payload, id: r.id || draft.id });
    } finally {
      setSaving(false);
    }
  }
  async function run() {
    if (!draft?.id) return;
    const r = await action("workflow.run", { id: draft.id, sessionId, prompt });
    if (r !== undefined) {
      setRunModal(false);
      setPrompt("");
    }
  }
  useEffect(() => {
    if (!draft) return;
    setEdges((eds) =>
      eds.filter(
        (e) =>
          nodes.some((n) => n.id === e.source) &&
          nodes.some((n) => n.id === e.target),
      ),
    );
  }, [nodes.length]);
  if (!draft)
    return (
      <div className="page-scroll">
        <SectionHeader
          eyebrow="Turn a process into a practice"
          title="Workflows"
          description="Compose agents, branches, and approval checkpoints into reusable work."
          actions={
            <Button variant="default" onClick={() => open()}>
              <Plus size={15} />
              New workflow
            </Button>
          }
        />
        {state.workflows.length ? (
          <div className="card-grid">
            {state.workflows.map((w) => (
              <article className="entity-card workflow-card" key={w.id}>
                <button
                  className="workflow-thumbnail"
                  onClick={() => open(w)}
                  aria-label={`Open ${w.name}`}
                >
                  <span />
                  <i />
                  <span />
                  <i />
                  <span />
                </button>
                <h3>{w.name}</h3>
                <p>{w.description || "A reusable agent workflow."}</p>
                <div className="card-meta">
                  <span>{w.nodes.length} nodes</span>
                  <span>{w.edges.length} connections</span>
                </div>
                <div className="card-footer">
                  <Button size="small" onClick={() => open(w)}>
                    Open workflow
                    <ArrowLeft size={13} className="flip" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Delete ${w.name}`}
                    onClick={async () => {
                      if (
                        await confirmAction({
                          title: "Delete workflow?",
                          description: `Delete the saved workflow “${w.name}”?`,
                          confirmLabel: "Delete workflow",
                          danger: true,
                        })
                      )
                        await action("workflow.delete", { id: w.id });
                    }}
                  >
                    <Trash2 size={15} />
                  </Button>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <Empty
            icon={<Waypoints size={32} />}
            title="Give repeatable work a shape"
            description="Start with an agent node, connect the next step, and add approvals where decisions belong to you."
            action={
              <Button onClick={() => open()}>
                <Plus size={15} />
                Create a workflow
              </Button>
            }
          />
        )}
      </div>
    );
  return (
    <div className="workflow-editor">
      <div className="workflow-toolbar">
        <Button
          size="icon"
          variant="ghost"
          onClick={() => setDraft(null)}
          aria-label="Back to workflows"
        >
          <ArrowLeft size={18} />
        </Button>
        <input
          className="workflow-name"
          aria-label="Workflow name"
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
        <span className="tag">{draft.id ? "Saved workflow" : "Unsaved"}</span>
        <div className="spacer" />
        <Button
          onClick={save}
          loading={saving}
          disabled={!nodes.length || !draft.name.trim()}
        >
          <Save size={15} />
          Save
        </Button>
        <Button
          variant="default"
          onClick={() => setRunModal(true)}
          disabled={!draft.id || !sessionId}
          title={
            !sessionId
              ? "Open a chat in a workspace first"
              : !draft.id
                ? "Save this workflow first"
                : "Run saved workflow"
          }
        >
          <Play size={15} />
          Run
        </Button>
      </div>
      {saveError && (
        <div className="notice error" role="alert">
          {saveError}
        </div>
      )}
      <div className="workflow-body">
        <div className="node-palette">
          <span className="eyebrow">Add a step</span>
          {kinds.map(([type, label]) => (
            <button key={type} onClick={() => add(type)}>
              {icon(type)}
              <span>{label}</span>
              <Plus size={12} />
            </button>
          ))}
          <p>Drag from a node’s handle to connect it to the next step.</p>
        </div>
        <div className="flow-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_, n) => {
              setSelected(n.id);
              setEdgeSelected("");
            }}
            onEdgeClick={(_, e) => {
              setEdgeSelected(e.id);
              setSelected("");
            }}
            onPaneClick={() => {
              setSelected("");
              setEdgeSelected("");
            }}
            fitView
            colorMode={state.settings.theme === "light" ? "light" : "dark"}
            deleteKeyCode={["Backspace", "Delete"]}
            minZoom={0.3}
            maxZoom={2}
          >
            <Background gap={24} size={1} />
            <Controls />
            <MiniMap pannable zoomable nodeColor="#3869ba" />
          </ReactFlow>
          {!nodes.length && (
            <div className="flow-empty">
              <Waypoints size={37} />
              <h3>Every workflow starts with one step</h3>
              <p>Add an agent from the left to begin.</p>
            </div>
          )}
        </div>
        <aside className="node-inspector">
          {selectedNode ? (
            <>
              <div className="row-title">
                {icon(String(selectedNode.data.type))}
                <h3>Step details</h3>
              </div>
              <Field label="Label">
                <input
                  value={String(selectedNode.data.label || "")}
                  onChange={(e) => updateNode("label", e.target.value)}
                />
              </Field>
              <Field label="Type">
                <select
                  value={String(selectedNode.data.type)}
                  onChange={(e) => updateNode("type", e.target.value)}
                >
                  {kinds.map(([type, label]) => (
                    <option key={type} value={type}>
                      {label}
                    </option>
                  ))}
                </select>
              </Field>
              {(selectedNode.data.type === "agent" ||
                selectedNode.data.type === "fanout") && (
                <Field label="Agent">
                  <select
                    value={String(selectedNode.data.agentId || "")}
                    onChange={(e) => updateNode("agentId", e.target.value)}
                  >
                    <option value="">Default agent</option>
                    {state.agents.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                </Field>
              )}
              {selectedNode.data.type === "fanout" && (
                <Field
                  label="Items"
                  hint="JSON array of up to 16 strings, one agent run per item. Leave empty (or use []) to read the preceding step’s JSON array."
                >
                  <textarea
                    rows={5}
                    spellCheck={false}
                    autoCorrect="off"
                    autoCapitalize="off"
                    value={String(selectedNode.data.itemsJSON || "")}
                    onChange={(e) => updateNode("itemsJSON", e.target.value)}
                    placeholder={'["First task", "Second task"]'}
                  />
                </Field>
              )}
              {selectedNode.data.type === "condition" ? (
                <Field
                  label="Condition"
                  hint="Use contains: text, not_contains: text, or equals: text. Connect outgoing edges as true or false."
                >
                  <input
                    value={String(selectedNode.data.condition || "")}
                    onChange={(e) => updateNode("condition", e.target.value)}
                    placeholder="contains: approved"
                  />
                </Field>
              ) : (
                <Field
                  label={
                    selectedNode.data.type === "approval"
                      ? "Approval reason"
                      : "Instructions"
                  }
                >
                  <textarea
                    rows={6}
                    value={String(selectedNode.data.prompt || "")}
                    onChange={(e) => updateNode("prompt", e.target.value)}
                    placeholder="What should happen in this step?"
                  />
                </Field>
              )}
              <Button
                variant="danger"
                onClick={() => {
                  setNodes((nds) => nds.filter((n) => n.id !== selected));
                  setSelected("");
                }}
              >
                <Trash2 size={14} />
                Remove step
              </Button>
            </>
          ) : selectedEdge ? (
            <>
              <h3>Connection</h3>
              <Field label="Label">
                <input
                  value={
                    typeof selectedEdge.label === "string"
                      ? selectedEdge.label
                      : ""
                  }
                  onChange={(e) =>
                    setEdges((eds) =>
                      eds.map((x) =>
                        x.id === edgeSelected
                          ? { ...x, label: e.target.value }
                          : x,
                      ),
                    )
                  }
                />
              </Field>
              <Field
                label="Condition branch"
                hint="Only use true/false on edges leaving a condition node."
              >
                <select
                  value={String(selectedEdge.data?.condition || "")}
                  onChange={(e) =>
                    setEdges((eds) =>
                      eds.map((x) =>
                        x.id === edgeSelected
                          ? {
                              ...x,
                              data: { ...x.data, condition: e.target.value },
                            }
                          : x,
                      ),
                    )
                  }
                >
                  <option value="">Always</option>
                  <option value="true">True</option>
                  <option value="false">False</option>
                </select>
              </Field>
              <Button
                variant="danger"
                onClick={() => {
                  setEdges((eds) => eds.filter((e) => e.id !== edgeSelected));
                  setEdgeSelected("");
                }}
              >
                <Trash2 size={14} />
                Remove connection
              </Button>
            </>
          ) : (
            <>
              <h3>Workflow details</h3>
              <Field label="Description">
                <textarea
                  rows={5}
                  value={draft.description}
                  onChange={(e) =>
                    setDraft({ ...draft, description: e.target.value })
                  }
                  placeholder="Describe when this workflow is useful."
                />
              </Field>
              <p className="small muted">
                The backend validates every saved workflow. Cycles and
                unsupported joins are rejected before execution.
              </p>
            </>
          )}
        </aside>
      </div>
      <Modal
        open={runModal}
        onClose={() => setRunModal(false)}
        title={`Run ${draft.name}`}
        description="This executes the saved workflow in the selected conversation."
      >
        <div className="form-stack">
          <Field label="Task">
            <textarea
              rows={5}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="What should this workflow accomplish?"
            />
          </Field>
          <Button variant="default" onClick={run} disabled={!prompt.trim()}>
            <Play size={15} />
            Start workflow
          </Button>
        </div>
      </Modal>
    </div>
  );
}
