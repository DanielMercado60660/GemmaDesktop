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
import type { Agent, Artifact, MCPServer, State } from "./types";
import { api } from "./bridge";
import {
  createArtifactLoader,
  artifactContent,
  type ArtifactPreviewState,
} from "./lib/artifactPreview";
import { useConfirmation } from "./components/Confirmation";
import { modelMemoryGuidance } from "./lib/runtimeMetrics";
import { agentProfileDraft, agentSavePayload } from "./lib/agentProfile";
import { isCompletionModel } from "./lib/modelCapabilities";
import ExecutionSettings from "./components/ExecutionSettings";
import HarnessWorkbench from "./HarnessWorkbench";
import Capabilities, { CapabilitySelector } from "./Capabilities";
import MCPSignIn from "./components/MCPSignIn";
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
  const confirmAction = useConfirmation();
  const [editing, setEditing] = useState<Partial<Agent> | null>(null),
    [saving, setSaving] = useState(false),
    [formError, setFormError] = useState("");
  function openEditor(agent: Partial<Agent>) {
    setFormError("");
    setEditing(agentProfileDraft(agent));
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!editing) return;
    setFormError("");
    setSaving(true);
    try {
      if (
        !isCompletionModel(
          state.models.find((m) => m.name === editing.model) || {
            name: editing.model || state.settings.defaultModel,
          },
        )
      )
        throw new Error(
          "Choose a local completion model for this agent. Embedding models build indexes and cannot answer chat requests.",
        );
      const r = await action("agent.save", agentSavePayload(editing));
      if (r !== undefined) setEditing(null);
    } catch (error) {
      setFormError(
        error instanceof Error ? error.message : "Could not save agent.",
      );
    } finally {
      setSaving(false);
    }
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
              openEditor({
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
                    onClick={() => openEditor(a)}
                    aria-label={`Edit ${a.name}`}
                  >
                    <Pencil size={15} />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={async () => {
                      if (
                        await confirmAction({
                          title: "Delete agent?",
                          description: `Delete “${a.name}” from your agent configurations?`,
                          confirmLabel: "Delete agent",
                          danger: true,
                        })
                      )
                        await action("agent.delete", { id: a.id });
                    }}
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
                openEditor({
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
      <Capabilities state={state} action={action} />
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
                {state.models.filter(isCompletionModel).map((m) => (
                  <option key={m.name}>{m.name}</option>
                ))}
                {editing?.model &&
                  !state.models.some(
                    (m) => m.name === editing.model && isCompletionModel(m),
                  ) && (
                    <option
                      disabled={state.models.some(
                        (m) =>
                          m.name === editing.model && !isCompletionModel(m),
                      )}
                    >
                      {editing.model}
                    </option>
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
          <div className="form-grid">
            <Field
              label="Thinking"
              hint="Model default preserves the runtime setting. Support depends on the selected model."
            >
              <select
                value={editing?.thinking ?? "auto"}
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    thinking: e.target.value as Agent["thinking"],
                  })
                }
              >
                <option value="auto">Model default</option>
                <option value="on">On</option>
                <option value="off">Off</option>
              </select>
            </Field>
            <Field
              label="Response token limit"
              hint="Includes thinking when supported by the model. Reaching the limit stops the run as incomplete."
            >
              <input
                type="number"
                required
                min={256}
                max={32768}
                step={1}
                value={
                  Number.isNaN(editing?.maxOutputTokens)
                    ? ""
                    : (editing?.maxOutputTokens ?? 8192)
                }
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    maxOutputTokens: e.target.valueAsNumber,
                  })
                }
              />
            </Field>
          </div>
          <div className="form-grid">
            <Field
              label="Temperature"
              hint="Controls sampling variation. Use the model publisher’s recommendation when available."
            >
              <input
                type="number"
                required
                min={0}
                max={2}
                step={0.1}
                value={
                  Number.isNaN(editing?.temperature)
                    ? ""
                    : (editing?.temperature ?? 0.2)
                }
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    temperature: e.target.valueAsNumber,
                  })
                }
              />
            </Field>
            <Field label="Execution mode">
              <select
                value={editing?.mode || "approval"}
                onChange={(e) =>
                  setEditing({ ...editing, mode: e.target.value })
                }
              >
                <option value="approval">Ask before changes</option>
                <option value="read-only">Read only</option>
              </select>
            </Field>
          </div>
          <CapabilitySelector
            value={editing?.capabilityProfileId}
            onChange={(id) =>
              setEditing({ ...editing, capabilityProfileId: id })
            }
            refreshKey={editing ? 1 : 0}
          />
          {formError && <p role="alert">{formError}</p>}
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
  const confirmAction = useConfirmation();
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
                    m.name === state.settings.defaultModel ||
                    !isCompletionModel(m)
                  }
                  title={
                    !isCompletionModel(m)
                      ? "Only local completion models can be used for chat. Embedding models are used for indexing."
                      : undefined
                  }
                  onClick={() =>
                    perform("settings.save", { defaultModel: m.name })
                  }
                >
                  {m.name === state.settings.defaultModel ? (
                    <Check size={13} />
                  ) : null}
                  {isCompletionModel(m)
                    ? "Use model"
                    : m.remote
                      ? "Remote model"
                      : "Embedding model"}
                </Button>
                <Button
                  size="small"
                  disabled={
                    !!m.remote || !!busy || (!m.loaded && !isCompletionModel(m))
                  }
                  title={
                    !m.loaded && !isCompletionModel(m)
                      ? "The encoder loads when an indexing or retrieval job needs it."
                      : undefined
                  }
                  onClick={() =>
                    perform(m.loaded ? "model.unload" : "model.load", {
                      name: m.name,
                    })
                  }
                >
                  {m.loaded
                    ? "Unload"
                    : !isCompletionModel(m) && !m.remote
                      ? "Loads on demand"
                      : "Load"}
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Delete ${m.name}`}
                  onClick={async () => {
                    if (
                      await confirmAction({
                        title: "Delete local model?",
                        description: `Delete “${m.name}” from local model storage? You can download it again.`,
                        confirmLabel: "Delete model",
                        danger: true,
                      })
                    )
                      await perform("model.delete", { name: m.name });
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
          icon={<Cpu size={30} />}
          title="Make room for your first model"
          description="Start with a model that suits your Mac. Download progress and installed models will appear here."
        />
      )}
    </div>
  );
}

export function ToolsPage({ state, action, workspaceId }: PageProps) {
  const confirmAction = useConfirmation();
  const [editing, setEditing] = useState<Partial<MCPServer> | null>(null),
    [args, setArgs] = useState(""),
    [oauthScopes, setOAuthScopes] = useState(""),
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
              setOAuthScopes("");
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
                      setOAuthScopes(s.oauth?.scopes.join(" ") || "");
                    }}
                  >
                    <Pencil size={15} />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Delete ${s.name}`}
                    onClick={async () => {
                      if (
                        await confirmAction({
                          title: "Remove MCP server?",
                          description: `Remove the saved connection for “${s.name}”?`,
                          confirmLabel: "Remove server",
                          danger: true,
                        })
                      )
                        await action("mcp.delete", { id: s.id });
                    }}
                  >
                    <Trash2 size={15} />
                  </Button>
                </div>
              </div>
              {s.oauth && <MCPSignIn id={s.id} action={action} />}
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
          {editing?.transport !== "stdio" && (
            <>
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={!!editing?.oauth}
                  onChange={(e) =>
                    setEditing({
                      ...editing,
                      oauth: e.target.checked
                        ? {
                            issuer: "",
                            authorizationEndpoint: "",
                            tokenEndpoint: "",
                            clientId: "",
                            scopes: [],
                          }
                        : undefined,
                    })
                  }
                />
                Use OAuth sign-in
              </label>
              {editing?.oauth && (
                <fieldset className="capability-options">
                  <legend>Public OAuth client</legend>
                  <p className="small muted">
                    Use your provider’s registered public-client configuration.
                    Tokens are kept in macOS Keychain; no client secret is
                    stored here.
                  </p>
                  {(
                    [
                      ["issuer", "Issuer URL"],
                      ["authorizationEndpoint", "Authorization endpoint"],
                      ["tokenEndpoint", "Token endpoint"],
                      ["clientId", "Client ID"],
                    ] as const
                  ).map(([key, label]) => (
                    <Field key={key} label={label}>
                      <input
                        required
                        type={key === "clientId" ? "text" : "url"}
                        value={editing.oauth![key]}
                        onChange={(e) =>
                          setEditing({
                            ...editing,
                            oauth: { ...editing.oauth!, [key]: e.target.value },
                          })
                        }
                      />
                    </Field>
                  ))}
                  <Field
                    label="Scopes"
                    hint="Separate scope names with spaces."
                  >
                    <input
                      value={oauthScopes}
                      onChange={(e) => {
                        setOAuthScopes(e.target.value);
                        setEditing({
                          ...editing,
                          oauth: {
                            ...editing.oauth!,
                            scopes: e.target.value.split(/\s+/).filter(Boolean),
                          },
                        });
                      }}
                    />
                  </Field>
                </fieldset>
              )}
            </>
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
  const confirmAction = useConfirmation();
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
          <h3>Import a complete skill package</h3>
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
                  onClick={async () => {
                    if (
                      await confirmAction({
                        title: "Remove skill?",
                        description: `Remove “${s.name}” from Gemma Desktop?`,
                        confirmLabel: "Remove skill",
                        danger: true,
                      })
                    )
                      await action("skill.delete", { id: s.id });
                  }}
                >
                  <Trash2 size={15} />
                </Button>
              </div>
              <h3>{s.name}</h3>
              <p>{s.description || "No description supplied."}</p>
              <small className="mono break-all">{s.path}</small>
              {s.digest && (
                <small className="mono break-all">
                  Version {s.digest.slice(0, 20)}
                </small>
              )}
              {s.files && (
                <p className="small muted">
                  {s.files.length} immutable files · {formatBytes(s.bytes)}
                </p>
              )}
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
  const [preview, setPreview] = useState<ArtifactPreviewState>({
    status: "idle",
    id: "",
  });
  const [loader] = useState(() =>
    createArtifactLoader(
      (id) => api<Artifact>("artifact.get", { id }),
      setPreview,
    ),
  );
  useEffect(() => {
    void loader.select(selected);
    return () => loader.invalidate();
  }, [loader, selected]);
  const artifacts = state.artifacts.filter(
    (a) => all || !workspaceId || a.workspaceId === workspaceId,
  );
  const artifact = state.artifacts.find((a) => a.id === selected);
  const selectedPreview = preview.id === selected ? preview : undefined;
  const details =
    selectedPreview?.status === "ready" ? selectedPreview.artifact : undefined;
  const contents = artifactContent(details);
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
        wide={contents.kind !== "none"}
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
        {selected &&
          (!selectedPreview || selectedPreview.status === "loading") && (
            <p className="muted" role="status">
              Loading artifact contents…
            </p>
          )}
        {selectedPreview?.status === "error" && (
          <div className="notice error" role="alert">
            <div>
              <strong>Artifact contents could not be loaded</strong>
              <p>{selectedPreview.error}</p>
            </div>
            <Button onClick={() => void loader.select(selected)}>Retry</Button>
          </div>
        )}
        {selectedPreview?.status === "ready" && contents.kind === "none" && (
          <p className="muted">
            This artifact has no saved inline text preview. Use Open file or
            Show in Finder to inspect it.
          </p>
        )}
        {details && contents.kind !== "none" && (
          <div className="artifact-editor">
            <Suspense
              fallback={<p className="muted">Opening code comparison…</p>}
            >
              <CodeEditor
                key={details.id}
                path={details.path}
                value={contents.after}
                original={
                  contents.kind === "diff" ? contents.before : undefined
                }
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
    [modelError, setModelError] = useState(""),
    [diagnosticsPath, setDiagnosticsPath] = useState("");
  useEffect(() => setDraft(state.settings), [JSON.stringify(state.settings)]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setModelError("");
    if (
      !isCompletionModel(
        state.models.find((m) => m.name === draft.defaultModel) || {
          name: draft.defaultModel,
        },
      )
    ) {
      setModelError(
        "The default must be a local completion model. Embedding models are used for indexing.",
      );
      return;
    }
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
                {state.models.filter(isCompletionModel).map((m) => (
                  <option key={m.name}>{m.name}</option>
                ))}
              </datalist>
              {modelError && (
                <small className="error-text" role="alert">
                  {modelError}
                </small>
              )}
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
      <ExecutionSettings action={action} />
      <p className="settings-footnote">
        Gemma Desktop is an independent application. Gemma is a model family
        from Google DeepMind.
      </p>
    </div>
  );
}

export function LabPage(props: PageProps) {
  return (
    <HarnessWorkbench {...props} compare={<ComparisonPage {...props} />} />
  );
}

function ComparisonPage({ state, action, workspaceId }: PageProps) {
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
