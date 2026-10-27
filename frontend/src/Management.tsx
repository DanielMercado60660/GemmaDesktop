import { lazy, Suspense, useEffect, useState, type FormEvent } from "react";
import {
  Bot,
  Plus,
  Trash2,
  Pencil,
  ArrowDownToLine,
  RefreshCw,
  Cpu,
  Play,
  Square,
  Plug,
  BookOpen,
  FolderOpen,
  ExternalLink,
  FileBox,
  FlaskConical,
  Settings2,
  Download,
  ChevronRight,
  Shield,
  Server,
  AlertTriangle,
  Check,
} from "lucide-react";
import type { Agent, MCPServer, State } from "./types";
import { modelMemoryGuidance } from "./lib/runtimeMetrics";
const CodeEditor = lazy(() => import("./components/CodeEditor"));
import {
  Button,
  Modal,
  Field,
  Empty,
  SectionHeader,
  Stat,
  formatBytes,
  friendlyStatus,
} from "./components/ui";
export type Action = <T = unknown>(
  method: string,
  params?: unknown,
) => Promise<T | undefined>;
export interface PageProps {
  state: State;
  action: Action;
  workspaceId: string;
  sessionId: string;
}
function JsonDetails({ value }: { value: unknown }) {
  return <pre className="json-view">{JSON.stringify(value, null, 2)}</pre>;
}

export function AgentsPage({ state, action }: PageProps) {
  const [editing, setEditing] = useState<Partial<Agent> | null>(null),
    [saving, setSaving] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    const r = await action("agent.save", editing);
    setSaving(false);
    if (r !== undefined) setEditing(null);
  }
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="A team shaped by you"
        title="Agents"
        description="Give each agent a purpose, a model, and clear boundaries."
        actions={
          <Button
            variant="default"
            onClick={() =>
              setEditing({
                name: "",
                instructions: "",
                model: state.settings.defaultModel,
                maxSteps: 24,
                mode: "approval",
              })
            }
          >
            <Plus size={16} />
            New agent
          </Button>
        }
      />
      {state.agents.length ? (
        <div className="card-grid">
          {state.agents.map((a) => (
            <article className="entity-card" key={a.id}>
              <div className="entity-top">
                <span className="entity-icon">
                  <Bot size={22} />
                </span>
                <div className="row-actions">
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={() => setEditing({ ...a })}
                    aria-label={`Edit ${a.name}`}
                  >
                    <Pencil size={15} />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={() =>
                      confirm(`Delete agent “${a.name}”?`) &&
                      action("agent.delete", { id: a.id })
                    }
                    aria-label={`Delete ${a.name}`}
                  >
                    <Trash2 size={15} />
                  </Button>
                </div>
              </div>
              <h3>{a.name}</h3>
              <p className="clamp-three">
                {a.instructions || "No instructions supplied."}
              </p>
              <div className="card-meta">
                <span>{a.model || "Default model"}</span>
                <span>{a.maxSteps} steps</span>
              </div>
              <div className="card-footer">
                <Shield size={13} />
                {a.mode === "read-only" ? "Read only" : "Approval required"}
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          icon={<Bot size={32} />}
          title="Specialists for the way you work"
          description="Create an agent for code review, research, writing, or a task you repeat. Its instructions and execution limits stay visible."
          action={
            <Button
              onClick={() =>
                setEditing({
                  name: "",
                  instructions: "",
                  model: state.settings.defaultModel,
                  maxSteps: 24,
                  mode: "approval",
                })
              }
            >
              <Plus size={15} />
              Create your first agent
            </Button>
          }
        />
      )}
      <Modal
        open={!!editing}
        onClose={() => setEditing(null)}
        title={editing?.id ? "Edit agent" : "Create an agent"}
        description="Instructions guide the agent. Application permissions always remain in force."
      >
        <form className="form-stack" onSubmit={submit}>
          <Field label="Name">
            <input
              required
              value={editing?.name || ""}
              onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              placeholder="Code reviewer"
              autoFocus
            />
          </Field>
          <Field label="Instructions">
            <textarea
              required
              rows={6}
              value={editing?.instructions || ""}
              onChange={(e) =>
                setEditing({ ...editing, instructions: e.target.value })
              }
              placeholder="Describe what this agent should do, how it should work, and what a successful result looks like."
            />
          </Field>
          <div className="form-grid">
            <Field label="Model">
              <select
                value={editing?.model || ""}
                onChange={(e) =>
                  setEditing({ ...editing, model: e.target.value })
                }
              >
                <option value="">Application default</option>
                {state.models.map((m) => (
                  <option key={m.name}>{m.name}</option>
                ))}
                {editing?.model &&
                  !state.models.some((m) => m.name === editing.model) && (
                    <option>{editing.model}</option>
                  )}
              </select>
            </Field>
            <Field label="Maximum steps">
              <input
                type="number"
                min={1}
                max={100}
                value={editing?.maxSteps ?? 24}
                onChange={(e) =>
                  setEditing({ ...editing, maxSteps: Number(e.target.value) })
                }
              />
            </Field>
          </div>
          <Field label="Execution mode">
            <select
              value={editing?.mode || "approval"}
              onChange={(e) => setEditing({ ...editing, mode: e.target.value })}
            >
              <option value="approval">Ask before changes</option>
              <option value="read-only">Read only</option>
            </select>
          </Field>
          <div className="modal-actions">
            <Button type="button" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button variant="default" loading={saving} type="submit">
              Save agent
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}

export function ModelsPage({ state, action }: PageProps) {
  const [name, setName] = useState("gemma4:12b-mlx"),
    [busy, setBusy] = useState("");
  async function perform(method: string, params?: unknown) {
    setBusy(`${method}:${JSON.stringify(params)}`);
    await action(method, params);
    setBusy("");
  }
  const pulls = (
    Array.isArray(state.runtime.pulls) ? state.runtime.pulls : []
  ).filter((p) => p.status !== "success");
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="Intelligence on your Mac"
        title="Models"
        description="Choose the model that fits your work. Your local runtime does the heavy lifting."
        actions={
          <Button
            onClick={() => perform("model.refresh")}
            loading={busy === "model.refresh:undefined"}
          >
            <RefreshCw size={15} />
            Refresh
          </Button>
        }
      />
      <div className="stats-grid">
        <Stat
          label="Runtime"
          value={state.runtime.available ? "Ollama" : "Unavailable"}
          detail={
            state.runtime.version
              ? `Version ${state.runtime.version}`
              : state.runtime.error || "Set up your runtime in Settings"
          }
        />
        <Stat
          label="Installed models"
          value={state.models.length}
          detail="Discovered from your runtime"
        />
        <Stat
          label="Host memory capacity"
          value={formatBytes(state.runtime.memoryBytes)}
          detail="Total capacity, not current utilization"
        />
      </div>
      {!state.runtime.available && (
        <div className="notice">
          <AlertTriangle size={18} />
          <div>
            <strong>Ollama needs your attention</strong>
            <p>
              {state.runtime.error ||
                `Gemma Desktop could not reach ${state.settings.ollamaUrl}. Start Ollama and refresh, or update the address in Settings.`}
            </p>
          </div>
        </div>
      )}
      <div className="download-box">
        <div>
          <div className="row-title">
            <ArrowDownToLine size={18} />
            <h3>Add a model</h3>
          </div>
          <p>
            Download a model by its Ollama tag. Storage and memory needs depend
            on the model and context.
          </p>
        </div>
        <form
          className="inline-form"
          onSubmit={(e) => {
            e.preventDefault();
            void perform("model.pull", { name });
          }}
        >
          <input
            aria-label="Ollama model tag"
            list="model-tags"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="gemma4:12b-mlx"
            required
          />
          <datalist id="model-tags">
            {[
              "gemma4:e2b-mlx",
              "gemma4:e4b-mlx",
              "gemma4:12b-mlx",
              "gemma4:26b-mlx",
              "gemma4:31b-mlx",
            ].map((x) => (
              <option key={x}>{x}</option>
            ))}
          </datalist>
          <Button
            variant="default"
            disabled={!state.runtime.available || !name.trim()}
            loading={busy.startsWith("model.pull")}
            type="submit"
          >
            <Download size={15} />
            Download
          </Button>
        </form>
      </div>
      {pulls.map((p, i) => (
        <div className="pull-row" key={p.name || p.model || i}>
          <div>
            <strong>{p.name || p.model || "Model download"}</strong>
            <span>{p.error || p.status || "Downloading"}</span>
          </div>
          {p.total ? (
            <>
              <progress value={p.completed || 0} max={p.total} />
              <small>
                {formatBytes(p.completed)} / {formatBytes(p.total)}
              </small>
            </>
          ) : p.error ? null : (
            <span className="muted">Preparing download</span>
          )}
          <Button
            size="small"
            disabled={
              !!p.error || ["failed", "cancelled"].includes(p.status || "")
            }
            onClick={() =>
              perform("model.cancelPull", { name: p.name || p.model })
            }
          >
            Cancel
          </Button>
        </div>
      ))}
      <div className="list-heading">
        <h2>Your library</h2>
        <span>{state.models.length} models</span>
      </div>
      {state.models.length ? (
        <div className="model-list">
          {state.models.map((m) => (
            <article className="model-card" key={m.name}>
              <span className="model-symbol">
                <Cpu size={24} />
              </span>
              <div className="model-info">
                <div className="row-title">
                  <h3>{m.name}</h3>
                  {m.name === state.settings.defaultModel && (
                    <span className="tag accent">Default</span>
                  )}
                  {m.loaded && <span className="tag">Loaded</span>}
                  {m.remote && <span className="tag">Remote</span>}
                </div>
                <p>
                  {[
                    m.parameterSize,
                    m.quantization,
                    m.runner,
                    formatBytes(m.size),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
                <div className="capabilities">
                  {m.capabilities?.map((c) => (
                    <span key={c}>{c}</span>
                  ))}
                </div>
                {modelMemoryGuidance(m.size, state.runtime.memoryBytes) && (
                  <p className="model-memory-guidance">
                    {modelMemoryGuidance(m.size, state.runtime.memoryBytes)}
                  </p>
                )}
              </div>
              <div className="row-actions">
                <Button
                  size="small"
                  disabled={
                    m.name === state.settings.defaultModel || !!m.remote
                  }
                  onClick={() =>
                    perform("settings.save", { defaultModel: m.name })
                  }
                >
                  {m.name === state.settings.defaultModel ? (
                    <Check size={13} />
                  ) : null}
                  Use model
                </Button>
                <Button
                  size="small"
                  disabled={!!m.remote || !!busy}
                  onClick={() =>
                    perform(m.loaded ? "model.unload" : "model.load", {
                      name: m.name,
                    })
                  }
                >
                  {m.loaded ? "Unload" : "Load"}
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Delete ${m.name}`}
                  onClick={() =>
                    confirm(
                      `Delete local model “${m.name}”? You can download it again.`,
                    ) && perform("model.delete", { name: m.name })
                  }
                >
                  <Trash2 size={15} />
                </Button>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          icon={<Cpu size={30} />}
          title="Make room for your first model"
          description="Start with a model that suits your Mac. Download progress and installed models will appear here."
        />
      )}
    </div>
  );
}

export function ToolsPage({ state, action, workspaceId }: PageProps) {
  const [editing, setEditing] = useState<Partial<MCPServer> | null>(null),
    [args, setArgs] = useState(""),
    [saving, setSaving] = useState(false),
    [expanded, setExpanded] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    const r = await action("mcp.save", {
      ...editing,
      args: args.split("\n").filter(Boolean),
    });
    setSaving(false);
    if (r !== undefined) setEditing(null);
  }
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="Extend what Gemma can do"
        title="Tools & connections"
        description="Connect MCP servers with explicit configuration and scoped execution."
        actions={
          <Button
            variant="default"
            onClick={() => {
              setEditing({
                name: "",
                transport: "stdio",
                command: "",
                url: "",
                enabled: true,
              });
              setArgs("");
            }}
          >
            <Plus size={15} />
            Add MCP server
          </Button>
        }
      />
      <div className="notice subdued">
        <Shield size={18} />
        <p>
          Server tools use the same approval rules as built-in tools. A server’s
          description never grants it access to your files or credentials.
        </p>
      </div>
      {state.tools.length ? (
        <div className="stack-list">
          {state.tools.map((s) => (
            <article className="connection-card" key={s.id}>
              <div className="connection-main">
                <span className="entity-icon">
                  <Plug size={21} />
                </span>
                <div className="flex-1">
                  <div className="row-title">
                    <h3>{s.name}</h3>
                    <span className="tag">{s.status || "Not connected"}</span>
                  </div>
                  <p className="mono small">
                    {s.transport === "stdio"
                      ? [s.command, ...(s.args || [])].join(" ")
                      : s.url}
                  </p>
                  {s.error && <p className="error-text">{s.error}</p>}
                </div>
                <div className="row-actions">
                  <Button
                    size="small"
                    disabled={!workspaceId}
                    title={
                      !workspaceId
                        ? "Select a workspace to connect this server"
                        : undefined
                    }
                    onClick={() =>
                      action("mcp.connect", { id: s.id, workspaceId })
                    }
                  >
                    Connect
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Edit ${s.name}`}
                    onClick={() => {
                      setEditing({ ...s });
                      setArgs(s.args?.join("\n") || "");
                    }}
                  >
                    <Pencil size={15} />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Delete ${s.name}`}
                    onClick={() =>
                      confirm(`Remove MCP server “${s.name}”?`) &&
                      action("mcp.delete", { id: s.id })
                    }
                  >
                    <Trash2 size={15} />
                  </Button>
                </div>
              </div>
              <button
                className="expand-tools"
                onClick={() => setExpanded(expanded === s.id ? "" : s.id)}
              >
                <ChevronRight
                  size={13}
                  className={expanded === s.id ? "rotated" : ""}
                />
                {s.tools?.length || 0} discovered tools
              </button>
              {expanded === s.id && (
                <div className="discovered-tools">
                  {s.tools?.length ? (
                    s.tools.map((t) => (
                      <div key={t.name}>
                        <strong className="mono">{t.name}</strong>
                        <p>{t.description || "No description provided."}</p>
                        <details>
                          <summary>Input schema</summary>
                          <JsonDetails value={t.inputSchema} />
                        </details>
                      </div>
                    ))
                  ) : (
                    <p className="muted">
                      Connect this server to discover its tools.
                    </p>
                  )}
                </div>
              )}
            </article>
          ))}
        </div>
      ) : (
        <Empty
          icon={<Plug size={30} />}
          title="Bring your tools into the workspace"
          description="Add a local or remote MCP server. Its discovered tools and connection errors will be shown here."
        />
      )}
      <Modal
        open={!!editing}
        onClose={() => setEditing(null)}
        title={editing?.id ? "Edit MCP server" : "Add MCP server"}
        description="Local servers are executable programs. Their launch remains subject to the application’s execution policy."
      >
        <form className="form-stack" onSubmit={submit}>
          <Field label="Name">
            <input
              required
              value={editing?.name || ""}
              onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              placeholder="Research tools"
            />
          </Field>
          <Field label="Transport">
            <select
              value={editing?.transport || "stdio"}
              onChange={(e) =>
                setEditing({ ...editing, transport: e.target.value })
              }
            >
              <option value="stdio">Local process · stdio</option>
              <option value="http">Remote · Streamable HTTP</option>
            </select>
          </Field>
          {editing?.transport === "stdio" ? (
            <>
              <Field label="Executable">
                <input
                  required
                  value={editing?.command || ""}
                  onChange={(e) =>
                    setEditing({ ...editing, command: e.target.value })
                  }
                  placeholder="/absolute/path/to/server"
                />
              </Field>
              <Field
                label="Arguments"
                hint="One argument per line. Shell expansion is not used."
              >
                <textarea
                  rows={3}
                  value={args}
                  onChange={(e) => setArgs(e.target.value)}
                />
              </Field>
            </>
          ) : (
            <Field label="Server URL">
              <input
                required
                type="url"
                value={editing?.url || ""}
                onChange={(e) =>
                  setEditing({ ...editing, url: e.target.value })
                }
                placeholder="https://example.com/mcp"
              />
            </Field>
          )}
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={editing?.enabled ?? true}
              onChange={(e) =>
                setEditing({ ...editing, enabled: e.target.checked })
              }
            />
            Enable server
          </label>
          <div className="modal-actions">
            <Button type="button" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button type="submit" variant="default" loading={saving}>
              Save server
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}

export function SkillsPage({ state, action }: PageProps) {
  const [path, setPath] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    const r = await action("skill.import", { path });
    setBusy(false);
    if (r !== undefined) setPath("");
  }
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="Knowledge you can reuse"
        title="Skills"
        description="Reusable instructions for the work you do often. Review the source before adding it to your agents."
      />
      <form className="download-box" onSubmit={submit}>
        <div>
          <h3>Import an instruction package</h3>
          <p>Choose a local skill directory or SKILL.md path.</p>
        </div>
        <div className="inline-form">
          <input
            aria-label="Skill path"
            required
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/path/to/skill"
          />
          <Button type="submit" variant="default" loading={busy}>
            <Plus size={15} />
            Import skill
          </Button>
        </div>
      </form>
      {state.skills.length ? (
        <div className="card-grid">
          {state.skills.map((s) => (
            <article key={s.id} className="entity-card">
              <div className="entity-top">
                <span className="entity-icon">
                  <BookOpen size={22} />
                </span>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Remove ${s.name}`}
                  onClick={() =>
                    confirm(`Remove skill “${s.name}” from Gemma Desktop?`) &&
                    action("skill.delete", { id: s.id })
                  }
                >
                  <Trash2 size={15} />
                </Button>
              </div>
              <h3>{s.name}</h3>
              <p>{s.description || "No description supplied."}</p>
              <small className="mono break-all">{s.path}</small>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          icon={<BookOpen size={30} />}
          title="Build a library of ways to work"
          description="Skills can provide instructions and resources. They cannot change application permissions."
        />
      )}
    </div>
  );
}

export function ArtifactsPage({ state, action, workspaceId }: PageProps) {
  const [all, setAll] = useState(false),
    [selected, setSelected] = useState<string>("");
  const artifacts = state.artifacts.filter(
    (a) => all || !workspaceId || a.workspaceId === workspaceId,
  );
  const artifact = state.artifacts.find((a) => a.id === selected);
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="The work, ready to use"
        title="Artifacts"
        description="Files and deliverables produced by your runs, with their source and location."
        actions={
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={all}
              onChange={(e) => setAll(e.target.checked)}
            />
            All workspaces
          </label>
        }
      />
      {artifacts.length ? (
        <div className="artifact-grid">
          {artifacts.map((a) => (
            <article key={a.id} className="artifact-card">
              <button
                className="artifact-preview"
                onClick={() => setSelected(a.id)}
                aria-label={`Inspect ${a.name}`}
              >
                <FileBox size={38} />
                <span>{a.mediaType || "FILE"}</span>
              </button>
              <div className="artifact-body">
                <h3 title={a.name}>{a.name}</h3>
                <p>
                  {formatBytes(a.size)} <span>·</span>{" "}
                  {new Date(a.createdAt).toLocaleDateString()}
                </p>
                <div className="row-actions">
                  <Button
                    size="small"
                    onClick={() => action("artifact.open", { id: a.id })}
                  >
                    <ExternalLink size={13} />
                    Open
                  </Button>
                  <Button
                    size="small"
                    variant="ghost"
                    onClick={() => action("artifact.reveal", { id: a.id })}
                  >
                    <FolderOpen size={14} />
                    Reveal
                  </Button>
                </div>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          icon={<FileBox size={31} />}
          title="Good work leaves something behind"
          description="Generated documents, reports, and changed files will appear here as your agents work."
        />
      )}
      <Modal
        open={!!artifact}
        onClose={() => setSelected("")}
        title={artifact?.name || "Artifact"}
        description="Artifact details"
        wide={artifact?.after !== undefined}
      >
        <div className="detail-list">
          <div>
            <span>Location</span>
            <code>{artifact?.path}</code>
          </div>
          <div>
            <span>Media type</span>
            <span>{artifact?.mediaType || "Unknown"}</span>
          </div>
          <div>
            <span>Size</span>
            <span>{formatBytes(artifact?.size)}</span>
          </div>
          <div>
            <span>Run</span>
            <code>{artifact?.runId}</code>
          </div>
        </div>
        {artifact?.after !== undefined && (
          <div className="artifact-editor">
            <Suspense
              fallback={<p className="muted">Opening code comparison…</p>}
            >
              <CodeEditor
                key={artifact.id}
                path={artifact.path}
                value={artifact.after}
                original={artifact.before}
                readOnly
                theme={state.settings.theme}
              />
            </Suspense>
          </div>
        )}
        <div className="modal-actions">
          <Button
            onClick={() =>
              artifact && action("artifact.reveal", { id: artifact.id })
            }
          >
            <FolderOpen size={15} />
            Show in Finder
          </Button>
          <Button
            variant="default"
            onClick={() =>
              artifact && action("artifact.open", { id: artifact.id })
            }
          >
            Open file
            <ExternalLink size={15} />
          </Button>
        </div>
      </Modal>
    </div>
  );
}

export function SettingsPage({ state, action }: PageProps) {
  const [draft, setDraft] = useState(state.settings),
    [saving, setSaving] = useState(false),
    [diagnosticsPath, setDiagnosticsPath] = useState("");
  useEffect(() => setDraft(state.settings), [JSON.stringify(state.settings)]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    await action("settings.save", draft);
    setSaving(false);
  }
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="Make the space yours"
        title="Settings"
        description="Control your runtime, defaults, and workspace experience."
      />
      <form className="settings-form" onSubmit={submit}>
        <section className="settings-section">
          <div className="settings-label">
            <Server size={20} />
            <h3>Model runtime</h3>
            <p>Connect to your Ollama installation.</p>
          </div>
          <div className="form-stack">
            <Field label="Ollama address">
              <input
                required
                type="url"
                value={draft.ollamaUrl}
                onChange={(e) =>
                  setDraft({ ...draft, ollamaUrl: e.target.value })
                }
              />
            </Field>
            <Field label="Default model">
              <input
                required
                value={draft.defaultModel}
                onChange={(e) =>
                  setDraft({ ...draft, defaultModel: e.target.value })
                }
                list="installed-models"
              />
              <datalist id="installed-models">
                {state.models.map((m) => (
                  <option key={m.name}>{m.name}</option>
                ))}
              </datalist>
            </Field>
            <Field
              label="Context size"
              hint="Larger contexts use more memory. The runtime and selected model may impose additional limits."
            >
              <input
                required
                type="number"
                min={1024}
                max={131072}
                step={1024}
                value={draft.contextSize}
                onChange={(e) =>
                  setDraft({ ...draft, contextSize: Number(e.target.value) })
                }
              />
            </Field>
          </div>
        </section>
        <section className="settings-section">
          <div className="settings-label">
            <Shield size={20} />
            <h3>Execution defaults</h3>
            <p>Boundaries stay enforced by the application.</p>
          </div>
          <div className="form-stack">
            <Field label="Default mode">
              <select
                value={draft.mode}
                onChange={(e) => setDraft({ ...draft, mode: e.target.value })}
              >
                <option value="approval">Ask before changes</option>
                <option value="read-only">Read only</option>
              </select>
            </Field>
            <Field label="Maximum steps per run">
              <input
                type="number"
                min={1}
                max={100}
                value={draft.maxSteps}
                onChange={(e) =>
                  setDraft({ ...draft, maxSteps: Number(e.target.value) })
                }
              />
            </Field>
          </div>
        </section>
        <section className="settings-section">
          <div className="settings-label">
            <Settings2 size={20} />
            <h3>Appearance</h3>
            <p>A comfortable place for focused work.</p>
          </div>
          <div className="theme-options">
            {["dark", "light"].map((t) => (
              <button
                type="button"
                key={t}
                className={`theme-option ${draft.theme === t ? "selected" : ""}`}
                onClick={() => setDraft({ ...draft, theme: t })}
              >
                <div className={`theme-preview theme-preview-${t}`}>
                  <i />
                  <b />
                  <span />
                </div>
                {t === "dark" ? "Midnight" : "Daylight"}
                {draft.theme === t && <Check size={15} />}
              </button>
            ))}
          </div>
        </section>
        <div className="settings-footer">
          <Button
            onClick={async () => {
              const result = await action<{ path: string }>(
                "diagnostics.export",
              );
              if (result) setDiagnosticsPath(result.path);
            }}
            type="button"
          >
            <Download size={15} />
            Export diagnostics
          </Button>
          <Button type="submit" variant="default" loading={saving}>
            Save settings
          </Button>
        </div>
      </form>
      {diagnosticsPath && (
        <p className="notice">
          Diagnostics saved to{" "}
          <code className="break-all">{diagnosticsPath}</code>
        </p>
      )}
      <p className="settings-footnote">
        Gemma Desktop is an independent application. Gemma is a model family
        from Google DeepMind.
      </p>
    </div>
  );
}

export function LabPage({ state, action, workspaceId }: PageProps) {
  const [baseline, setBaseline] = useState(""),
    [candidate, setCandidate] = useState(""),
    [tasks, setTasks] = useState(""),
    [busy, setBusy] = useState(false),
    [selected, setSelected] = useState("");
  async function start(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    await action("eval.start", {
      workspaceId,
      baselineAgentId: baseline,
      candidateAgentId: candidate,
      tasks: tasks
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean),
    });
    setBusy(false);
  }
  const experiment = state.experiments.find((e) => e.id === selected);
  return (
    <div className="page-scroll">
      <SectionHeader
        eyebrow="Experiment with evidence"
        title="Harness Lab"
        description="Compare agent configurations on repeatable tasks. Keep the model, budgets, and evaluation conditions visible."
      />
      <div className="lab-layout">
        <form className="panel form-stack" onSubmit={start}>
          <div className="row-title">
            <FlaskConical size={20} />
            <h3>New comparison</h3>
          </div>
          <div className="form-grid">
            <Field label="Baseline agent">
              <select
                required
                value={baseline}
                onChange={(e) => setBaseline(e.target.value)}
              >
                <option value="">Choose baseline</option>
                {state.agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Candidate agent">
              <select
                required
                value={candidate}
                onChange={(e) => setCandidate(e.target.value)}
              >
                <option value="">Choose candidate</option>
                {state.agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <Field
            label="Evaluation tasks"
            hint="One task per line. Use repeatable tasks with an observable outcome."
          >
            <textarea
              required
              rows={6}
              value={tasks}
              onChange={(e) => setTasks(e.target.value)}
              placeholder="Describe a task to compare across both agents."
            />
          </Field>
          <div className="notice subdued">
            <Shield size={17} />
            <p>
              Both agents work from the same frozen text snapshot. Evaluation
              runs are read only, with no shell, MCP, or browser access.
            </p>
          </div>
          <Button
            type="submit"
            variant="default"
            disabled={
              !workspaceId ||
              !baseline ||
              !candidate ||
              baseline === candidate ||
              !tasks.trim()
            }
            loading={busy}
          >
            <Play size={15} />
            Run comparison
          </Button>
          {!workspaceId && (
            <p className="small muted">
              Select a workspace before starting an evaluation.
            </p>
          )}
        </form>
        <div className="panel">
          <h3>Experiment history</h3>
          {state.experiments.length ? (
            <div className="experiment-list">
              {state.experiments.map((e) => (
                <div key={e.id}>
                  <button onClick={() => setSelected(e.id)}>
                    <FlaskConical size={17} />
                    <span>
                      <strong>{new Date(e.createdAt).toLocaleString()}</strong>
                      <small>{friendlyStatus(e.status)}</small>
                    </span>
                    <ChevronRight size={15} />
                  </button>
                  {["queued", "running"].includes(e.status) && (
                    <Button
                      size="small"
                      onClick={() => action("eval.cancel", { id: e.id })}
                    >
                      <Square size={12} />
                      Stop
                    </Button>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <Empty
              icon={<FlaskConical size={25} />}
              title="A baseline comes first"
              description="Run a comparison to start collecting evidence about your agent configurations."
            />
          )}
        </div>
      </div>
      <Modal
        open={!!experiment}
        onClose={() => setSelected("")}
        title="Experiment results"
        description={
          experiment
            ? `${friendlyStatus(experiment.status)} · ${new Date(experiment.createdAt).toLocaleString()}`
            : ""
        }
        wide
      >
        {experiment?.error && (
          <div className="inline-error">{experiment.error}</div>
        )}
        {experiment?.qualification && (
          <p className="notice subdued">{experiment.qualification}</p>
        )}
        {experiment?.inputDigest && (
          <div className="detail-list">
            <div>
              <span>Input fingerprint</span>
              <code className="break-all">{experiment.inputDigest}</code>
            </div>
          </div>
        )}
        <div className="evaluation-results">
          {experiment?.results?.length ? (
            experiment.results.map((result, index) => (
              <article
                className="panel evaluation-result"
                key={`${result.agentId}-${index}`}
              >
                <div className="row-title">
                  <h3>{result.agentName}</h3>
                  <span className="tag">{friendlyStatus(result.status)}</span>
                </div>
                <p className="evaluation-task">{result.task}</p>
                <div className="card-meta">
                  <span>{(result.durationMs / 1000).toFixed(1)} seconds</span>
                  <span>{result.toolCalls} tool calls</span>
                </div>
                {result.error && <p className="error-text">{result.error}</p>}
                <pre className="evaluation-content">
                  {result.content || "No response recorded."}
                </pre>
                <details>
                  <summary>Usage details</summary>
                  <JsonDetails value={result.usage} />
                </details>
              </article>
            ))
          ) : (
            <p className="muted">Results appear here as each task finishes.</p>
          )}
        </div>
      </Modal>
    </div>
  );
}
