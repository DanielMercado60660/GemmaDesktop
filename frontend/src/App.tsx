import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent,
} from "react";
import {
  ArrowUp,
  ArrowUpRight,
  Paperclip,
  FolderOpen,
  Plus,
  MessageSquare,
  Bot,
  Waypoints,
  Cpu,
  Plug,
  FileBox,
  Settings2,
  PanelRight,
  PanelLeft,
  ChevronDown,
  ChevronRight,
  Search,
  X,
  FileCode2,
  TerminalSquare,
  GitCompareArrows,
  Activity,
  Check,
  Clock3,
  AlertTriangle,
  ShieldCheck,
  Square,
  LoaderCircle,
  BookOpen,
  FlaskConical,
  RefreshCw,
  Copy,
  Trash2,
  Play,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  ReactFlow,
  Background,
  Controls,
  type Node,
  type Edge,
} from "@xyflow/react";
import { api, openExternal } from "./bridge";
import { modelMemoryGuidance, runMetrics } from "./lib/runtimeMetrics";
import {
  emptyState,
  activeStatuses,
  type State,
  type Session,
  type Message,
  type Event,
  type Approval,
  type Run,
} from "./types";
import {
  Button,
  Modal,
  Field,
  Empty,
  cn,
  friendlyStatus,
  formatBytes,
} from "./components/ui";
import {
  AgentsPage,
  ModelsPage,
  ToolsPage,
  SkillsPage,
  ArtifactsPage,
  SettingsPage,
  LabPage,
  type Action,
} from "./Management";
const FilePane = lazy(() => import("./components/FilePane"));
const TerminalPane = lazy(() => import("./components/TerminalPane"));
const Workflows = lazy(() => import("./Workflows"));
type View =
  | "chat"
  | "agents"
  | "workflows"
  | "models"
  | "tools"
  | "skills"
  | "artifacts"
  | "settings"
  | "lab";
type WorkTab = "chat" | "files" | "changes" | "terminal";
const navigation: [View, string, typeof Bot][] = [
  ["chat", "Chats", MessageSquare],
  ["agents", "Agents", Bot],
  ["workflows", "Workflows", Waypoints],
  ["models", "Models", Cpu],
  ["tools", "Tools", Plug],
  ["skills", "Skills", BookOpen],
  ["artifacts", "Artifacts", FileBox],
];
function Orb({ large = false }: { large?: boolean }) {
  return (
    <div className={cn("orb", large && "orb-large")} aria-hidden="true">
      <svg viewBox="0 0 100 100">
        <defs>
          <linearGradient
            id={large ? "orb-lg" : "orb-sm"}
            x1="0"
            y1="0"
            x2="1"
            y2="1"
          >
            <stop stopColor="#89c1ff" />
            <stop offset=".5" stopColor="#5488ff" />
            <stop offset="1" stopColor="#8a75ff" />
          </linearGradient>
        </defs>
        {[0, 45, 90, 135].map((a) => (
          <ellipse
            key={a}
            cx="50"
            cy="50"
            rx="20"
            ry="39"
            transform={`rotate(${a} 50 50)`}
            fill="none"
            stroke={`url(#${large ? "orb-lg" : "orb-sm"})`}
            strokeWidth={large ? "1" : "1.5"}
            strokeDasharray="1.3 6"
            strokeLinecap="round"
          />
        ))}
        <circle cx="50" cy="50" r="5" fill="#72a6ff" />
        <circle cx="50" cy="11" r="2.4" fill="#aed3ff" />
        <circle cx="79" cy="76" r="2" fill="#7c91ff" />
      </svg>
    </div>
  );
}
function SafeMarkdown({ text }: { text: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      skipHtml
      components={{
        a: ({ href, children }) => (
          <button
            className="markdown-link"
            onClick={() => href && void openExternal(href).catch(() => {})}
          >
            {children}
            <ArrowUpRight size={11} />
          </button>
        ),
        img: ({ alt }) => (
          <span className="inline-image-label">
            [Image: {alt || "attachment"}]
          </span>
        ),
      }}
    >
      {text}
    </ReactMarkdown>
  );
}
function statusIcon(s: string) {
  return s === "completed" ? (
    <Check size={14} />
  ) : s === "failed" || s === "interrupted" ? (
    <AlertTriangle size={14} />
  ) : s === "waiting_approval" ? (
    <ShieldCheck size={14} />
  ) : s === "running" || s === "cancelling" ? (
    <LoaderCircle size={14} className="spin" />
  ) : (
    <Clock3 size={14} />
  );
}
function EventList({
  events,
  compact = false,
}: {
  events: Event[];
  compact?: boolean;
}) {
  const entries = events.filter(
    (e) => !["text", "thinking", "adk_event"].includes(e.type),
  );
  return (
    <div className={cn("event-list", compact && "compact")}>
      {entries.map((e) => (
        <details className={`event-item event-${e.type}`} key={e.id || e.seq}>
          <summary>
            <span className="event-symbol">
              {e.type.includes("failed") || e.type === "error" ? (
                <AlertTriangle size={14} />
              ) : e.type.includes("completed") ? (
                <Check size={14} />
              ) : e.type === "approval" ? (
                <ShieldCheck size={14} />
              ) : (
                <Activity size={14} />
              )}
            </span>
            <span>
              <strong>
                {e.title || e.tool || e.type.replaceAll("_", " ")}
              </strong>
              {e.detail && (
                <small>
                  {e.detail.length > 120
                    ? e.detail.slice(0, 120) + "…"
                    : e.detail}
                </small>
              )}
            </span>
            <ChevronRight size={13} />
          </summary>
          {(e.detail || e.data !== undefined) && (
            <div className="event-detail">
              {e.detail && <pre>{e.detail}</pre>}
              {e.data !== undefined && (
                <pre>
                  {typeof e.data === "string"
                    ? e.data
                    : JSON.stringify(e.data, null, 2)}
                </pre>
              )}
            </div>
          )}
        </details>
      ))}
      {!entries.length && (
        <p className="muted small">
          Tool calls and task progress will appear here.
        </p>
      )}
    </div>
  );
}
function ApprovalCard({
  approval,
  action,
}: {
  approval: Approval;
  action: Action;
}) {
  const [busy, setBusy] = useState(false);
  async function respond(allow: boolean) {
    setBusy(true);
    await action("approval.respond", { id: approval.id, allow });
    setBusy(false);
  }
  return (
    <div className="approval-card">
      <div className="approval-title">
        <ShieldCheck size={20} />
        <div>
          <strong>Your approval is needed</strong>
          <span>{approval.tool}</span>
        </div>
      </div>
      <p>{approval.reason}</p>
      <pre>
        {typeof approval.arguments === "string"
          ? approval.arguments
          : JSON.stringify(approval.arguments, null, 2)}
      </pre>
      <div className="approval-actions">
        <Button onClick={() => respond(false)} disabled={busy}>
          Deny
        </Button>
        <Button variant="default" onClick={() => respond(true)} loading={busy}>
          <Check size={14} />
          Approve this action
        </Button>
      </div>
    </div>
  );
}
function RunGraph({ state, run }: { state: State; run?: Run }) {
  const workflow = state.workflows.find((w) => w.id === run?.workflowId);
  const nodeEvents = state.events.filter(
    (e) => e.runId === run?.id && e.nodeId && e.type !== "adk_event",
  );
  const ids =
    workflow?.nodes.map((n) => n.id) ||
    Array.from(new Set(nodeEvents.map((e) => e.nodeId!)));
  const nodes: Node[] = ids.map((id, i) => {
    const n = workflow?.nodes.find((n) => n.id === id);
    const latest = nodeEvents.filter((e) => e.nodeId === id).at(-1);
    return {
      id,
      position: n?.position || { x: 25, y: i * 108 },
      data: {
        label: (
          <div className="execution-node">
            <span>{n?.label || latest?.title || id}</span>
            <small>
              {latest?.type.replace("node_", "").replaceAll("_", " ") ||
                "Waiting"}
            </small>
          </div>
        ),
      },
      className: `flow-node ${latest?.type === "node_completed" ? "node-finished" : latest?.type === "node_failed" ? "node-error" : latest ? "node-working" : ""}`,
    };
  });
  const edges: Edge[] =
    workflow?.edges.map((e) => ({ ...e })) ||
    ids
      .slice(1)
      .map((id, i) => ({ id: `${ids[i]}-${id}`, source: ids[i], target: id }));
  return nodes.length ? (
    <div className="execution-graph">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        colorMode={state.settings.theme === "light" ? "light" : "dark"}
        proOptions={{ hideAttribution: false }}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  ) : (
    <Empty
      icon={<Waypoints size={25} />}
      title="A view of the process"
      description="Workflow steps appear as the backend records their execution."
    />
  );
}
function ResizeHandle({
  value,
  onChange,
  min,
  max,
  reverse = false,
  label,
}: {
  value: number;
  onChange: (v: number) => void;
  min: number;
  max: number;
  reverse?: boolean;
  label: string;
}) {
  function start(e: PointerEvent) {
    e.preventDefault();
    const x = e.clientX;
    const move = (event: globalThis.PointerEvent) =>
      onChange(
        Math.min(
          max,
          Math.max(min, value + (event.clientX - x) * (reverse ? -1 : 1)),
        ),
      );
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }
  return (
    <div
      className="resize-handle"
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation="vertical"
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      onPointerDown={start}
      onKeyDown={(e) => {
        if (["ArrowLeft", "ArrowRight"].includes(e.key)) {
          e.preventDefault();
          onChange(
            Math.min(
              max,
              Math.max(
                min,
                value +
                  (e.key === "ArrowRight" ? 16 : -16) * (reverse ? -1 : 1),
              ),
            ),
          );
        }
      }}
    />
  );
}

export default function App() {
  const [dirtyFile, setDirtyFile] = useState(false);
  function mayLeaveFile() {
    return !dirtyFile || confirm("Discard your unsaved file changes?");
  }
  const [state, setState] = useState<State>(emptyState),
    [ready, setReady] = useState(false),
    [backendError, setBackendError] = useState(""),
    [toast, setToast] = useState<{ message: string; error: boolean } | null>(
      null,
    );
  const [view, setView] = useState<View>("chat"),
    [tab, setTab] = useState<WorkTab>("chat"),
    [workspaceId, setWorkspaceId] = useState(
      localStorage.getItem("gemma.workspace") || "",
    ),
    [sessionId, setSessionId] = useState(
      localStorage.getItem("gemma.session") || "",
    ),
    [messages, setMessages] = useState<Message[]>([]),
    [sessionEvents, setSessionEvents] = useState<Event[]>([]);
  const [inspector, setInspector] = useState(true),
    [inspectorTab, setInspectorTab] = useState<
      "activity" | "graph" | "runtime"
    >("activity"),
    [sidebarOpen, setSidebarOpen] = useState(true),
    [sidebarWidth, setSidebarWidth] = useState(238),
    [inspectorWidth, setInspectorWidth] = useState(320);
  const [workspaceModal, setWorkspaceModal] = useState(false),
    [workspacePath, setWorkspacePath] = useState(""),
    [workspaceName, setWorkspaceName] = useState(""),
    [workspaceBusy, setWorkspaceBusy] = useState(false),
    [commandOpen, setCommandOpen] = useState(false),
    [commandQuery, setCommandQuery] = useState("");
  const [approvalInbox, setApprovalInbox] = useState(false);
  const [prompt, setPrompt] = useState(""),
    [agentId, setAgentId] = useState(""),
    [model, setModel] = useState(""),
    [sending, setSending] = useState(false),
    [attachmentModal, setAttachmentModal] = useState(false),
    [attachmentPath, setAttachmentPath] = useState(""),
    [attachments, setAttachments] = useState<string[]>([]);
  const stateRef = useRef(state),
    refreshing = useRef(false),
    sessionRef = useRef(sessionId),
    scrollRef = useRef<HTMLDivElement>(null),
    composerRef = useRef<HTMLTextAreaElement>(null),
    sticky = useRef(true);
  stateRef.current = state;
  sessionRef.current = sessionId;
  const workspace = state.workspaces.find((w) => w.id === workspaceId),
    sessions = state.sessions.filter((s) => s.workspaceId === workspaceId),
    session = state.sessions.find((s) => s.id === sessionId);
  const runs = state.runs.filter((r) => r.sessionId === sessionId),
    run = runs.at(-1),
    working = !!run && activeStatuses.has(run.status),
    events = sessionEvents.length
      ? sessionEvents
      : state.events.filter((e) => runs.some((r) => r.id === e.runId)),
    approvals = state.approvals.filter(
      (a) =>
        runs.some((r) => r.id === a.runId) &&
        ["pending", "waiting", ""].includes(a.status || ""),
    );
  const pendingApprovals = state.approvals.filter((a) =>
    ["pending", "waiting", ""].includes(a.status || ""),
  );
  const globalApprovalKey = pendingApprovals
    .filter((a) => !a.runId)
    .map((a) => a.id)
    .join("|");
  useEffect(() => {
    if (globalApprovalKey) setApprovalInbox(true);
  }, [globalApprovalKey]);
  const refresh = useCallback(async () => {
    if (refreshing.current) return;
    refreshing.current = true;
    try {
      const snapshot = await api<Partial<State>>("state");
      const normalized = {
        ...emptyState,
        ...snapshot,
        settings: { ...emptyState.settings, ...snapshot.settings },
        runtime: { ...emptyState.runtime, ...snapshot.runtime },
      };
      for (const key of [
        "workspaces",
        "sessions",
        "runs",
        "agents",
        "workflows",
        "models",
        "artifacts",
        "approvals",
        "tools",
        "skills",
        "events",
        "experiments",
      ] as const) {
        if (!Array.isArray(normalized[key])) (normalized[key] as unknown) = [];
      }
      setState(normalized);
      setBackendError("");
      setReady(true);
      const sid = sessionRef.current;
      if (sid && normalized.sessions.some((s) => s.id === sid)) {
        const r = await api<{ messages: Message[]; events: Event[] }>(
          "session.get",
          { id: sid },
        );
        if (sessionRef.current === sid) {
          setMessages(r.messages || []);
          setSessionEvents(r.events || []);
        }
      }
    } catch (e) {
      setBackendError((e as Error).message);
      setReady(true);
    } finally {
      refreshing.current = false;
    }
  }, []);
  useEffect(() => {
    let disposed = false,
      timer: ReturnType<typeof setTimeout>;
    async function tick() {
      await refresh();
      if (!disposed) {
        const s = stateRef.current;
        const busy =
          s.runs.some((r) => activeStatuses.has(r.status)) ||
          s.runtime.pulls?.some(
            (p) => !["success", "failed", "cancelled"].includes(p.status || ""),
          ) ||
          s.experiments.some((e) => activeStatuses.has(e.status));
        timer = setTimeout(tick, busy ? 700 : 2200);
      }
    }
    void tick();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, [refresh]);
  useEffect(() => {
    document.documentElement.dataset.theme = state.settings.theme;
    document.documentElement.style.colorScheme =
      state.settings.theme === "light" ? "light" : "dark";
  }, [state.settings.theme]);
  useEffect(() => {
    if (toast) {
      const t = setTimeout(() => setToast(null), 6500);
      return () => clearTimeout(t);
    }
  }, [toast]);
  useEffect(() => {
    if (!ready) return;
    if (!state.workspaces.some((w) => w.id === workspaceId)) {
      setWorkspaceId(state.workspaces[0]?.id || "");
      setSessionId("");
    }
  }, [state.workspaces, ready, workspaceId]);
  useEffect(() => {
    localStorage.setItem("gemma.workspace", workspaceId);
    if (
      sessionId &&
      !stateRef.current.sessions.some(
        (s) => s.id === sessionId && s.workspaceId === workspaceId,
      )
    )
      setSessionId("");
  }, [workspaceId]);
  useEffect(() => {
    localStorage.setItem("gemma.session", sessionId);
    setMessages([]);
    setSessionEvents([]);
    if (sessionId)
      void api<{ messages: Message[]; events: Event[] }>("session.get", {
        id: sessionId,
      })
        .then((r) => {
          if (sessionRef.current === sessionId) {
            setMessages(r.messages || []);
            setSessionEvents(r.events || []);
          }
        })
        .catch((e) => setToast({ message: e.message, error: true }));
    sticky.current = true;
  }, [sessionId]);
  useEffect(() => {
    if (sticky.current && scrollRef.current)
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [messages, events, approvals]);
  useEffect(() => {
    const handle = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setCommandOpen((o) => !o);
      }
      if ((e.metaKey || e.ctrlKey) && e.key === "b") {
        e.preventDefault();
        setSidebarOpen((o) => !o);
      }
      if (
        (e.metaKey || e.ctrlKey) &&
        e.shiftKey &&
        e.key.toLowerCase() === "n"
      ) {
        e.preventDefault();
        void newSession();
      }
      if (e.key === "Escape" && working && !commandOpen) {
        composerRef.current?.blur();
      }
    };
    window.addEventListener("keydown", handle);
    return () => window.removeEventListener("keydown", handle);
  }, [workspaceId, working, commandOpen, dirtyFile]);
  const action: Action = async <T,>(method: string, params: unknown = {}) => {
    try {
      const result = await api<T>(method, params);
      await refresh();
      return result === undefined ? ({} as T) : result;
    } catch (e) {
      setToast({ message: (e as Error).message, error: true });
      return undefined;
    }
  };
  async function newSession() {
    if (!mayLeaveFile()) return;
    if (!workspaceId) {
      setWorkspaceModal(true);
      return;
    }
    const r = await action<Session>("session.create", { workspaceId });
    if (r?.id) {
      setSessionId(r.id);
      setView("chat");
      setTab("chat");
      setPrompt("");
      setTimeout(() => composerRef.current?.focus(), 0);
    }
  }
  async function addWorkspace(pick = false) {
    if (!mayLeaveFile()) return;
    setWorkspaceBusy(true);
    try {
      const r = await action<{ id: string; path?: string }>(
        "workspace." + (pick ? "pick" : "add"),
        pick ? {} : { path: workspacePath, name: workspaceName },
      );
      if (r?.id) {
        setWorkspaceId(r.id);
        setSessionId("");
        setWorkspaceModal(false);
        setWorkspacePath("");
        setWorkspaceName("");
        setView("chat");
      }
    } finally {
      setWorkspaceBusy(false);
    }
  }
  async function send() {
    if (!prompt.trim() || sending || working || !workspaceId) return;
    setSending(true);
    try {
      let sid = sessionId;
      if (!sid) {
        const s = await action<Session>("session.create", {
          workspaceId,
          title: prompt.slice(0, 70),
        });
        if (!s?.id) return;
        sid = s.id;
        setSessionId(sid);
      }
      const result = await action("run.start", {
        sessionId: sid,
        prompt: prompt.trim(),
        agentId: agentId || undefined,
        model: model || undefined,
        attachments,
      });
      if (result !== undefined) {
        setPrompt("");
        setAttachments([]);
        sticky.current = true;
        setView("chat");
        setTab("chat");
      }
    } finally {
      setSending(false);
    }
  }
  const streamRun = run && working ? run : undefined;
  const streamed = streamRun
    ? events
        .filter((e) => e.runId === streamRun.id && e.type === "text")
        .map((e) => e.detail || "")
        .join("")
    : "";
  const hasStreamMessage =
    streamRun &&
    messages.some((m) => m.role === "assistant" && m.runId === streamRun.id);
  const canSend =
    !!workspace &&
    state.runtime.available &&
    !!prompt.trim() &&
    !working &&
    !sending;
  function navigate(v: View) {
    if (v !== view && !mayLeaveFile()) return;
    setView(v);
    setCommandOpen(false);
    setCommandQuery("");
  }
  const pageProps = { state, action, workspaceId, sessionId };
  const selectedAgent = state.agents.find(
    (a) => a.id === (agentId || "default"),
  );
  const selectedModel =
    model || selectedAgent?.model || state.settings.defaultModel;
  const inspectedModelName = run?.model || selectedModel;
  const inspectedModel = state.models.find(
    (m) => m.name === inspectedModelName,
  );
  const loadedModel = state.runtime.models?.find(
    (m) => m.name === inspectedModelName || m.model === inspectedModelName,
  );
  const loadedContext =
    loadedModel?.context_length ?? inspectedModel?.loadedContextLength;
  const measuredPerformance = runMetrics(run);
  const memoryGuidance = modelMemoryGuidance(
    inspectedModel?.size,
    state.runtime.memoryBytes,
  );
  const readOnly =
    state.settings.mode === "read-only" || selectedAgent?.mode === "read-only";
  return (
    <div
      className="desktop-shell"
      style={
        {
          "--sidebar-width": `${sidebarWidth}px`,
          "--inspector-width": `${inspectorWidth}px`,
        } as CSSProperties
      }
    >
      {sidebarOpen && (
        <aside className="sidebar">
          <div className="brand" onClick={() => navigate("chat")}>
            <Orb />
            <span>
              Gemma<span>Desktop</span>
            </span>
          </div>
          <button
            className="search-button"
            onClick={() => setCommandOpen(true)}
          >
            <Search size={15} />
            <span>Find anything</span>
            <kbd>⌘ K</kbd>
          </button>
          <div className="sidebar-section-label">
            <span>WORKSPACES</span>
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setWorkspaceModal(true)}
              aria-label="Add workspace"
            >
              <Plus size={15} />
            </Button>
          </div>
          <div className="workspace-list">
            {state.workspaces.map((w) => (
              <div
                key={w.id}
                className={`workspace-item ${w.id === workspaceId ? "selected" : ""}`}
              >
                <button
                  onClick={() => {
                    if (!mayLeaveFile()) return;
                    setWorkspaceId(w.id);
                    setView("chat");
                    setTab("chat");
                  }}
                >
                  <FolderOpen size={18} />
                  <span>
                    <strong>{w.name}</strong>
                    <small title={w.path}>
                      {w.path.replace(/^\/Users\/[^/]+/, "~")}
                    </small>
                  </span>
                </button>
                <Button
                  size="icon"
                  variant="ghost"
                  className="workspace-remove"
                  aria-label={`Remove workspace ${w.name}`}
                  onClick={() =>
                    confirm(
                      `Remove “${w.name}” from Gemma Desktop? Files on disk will remain.`,
                    ) && action("workspace.remove", { id: w.id })
                  }
                >
                  <X size={12} />
                </Button>
              </div>
            ))}
            {!state.workspaces.length && (
              <button
                className="add-workspace-empty"
                onClick={() => setWorkspaceModal(true)}
              >
                <Plus size={16} />
                Add a workspace
              </button>
            )}
          </div>
          <div className="nav-divider" />
          <nav className="primary-nav" aria-label="Main navigation">
            {navigation.map(([v, label, Icon]) => (
              <button
                key={v}
                className={view === v ? "selected" : ""}
                onClick={() => navigate(v)}
              >
                <Icon size={18} />
                <span>{label}</span>
                {v === "chat" && sessions.length > 0 && (
                  <small>{sessions.length}</small>
                )}
              </button>
            ))}
          </nav>
          {view === "chat" && sessions.length > 0 && (
            <div className="recent-chats">
              <div className="sidebar-section-label">
                <span>RECENT CHATS</span>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={newSession}
                  aria-label="New chat"
                >
                  <Plus size={14} />
                </Button>
              </div>
              {sessions
                .slice()
                .reverse()
                .slice(0, 6)
                .map((s) => (
                  <button
                    key={s.id}
                    className={sessionId === s.id ? "selected" : ""}
                    onClick={() => {
                      if (!mayLeaveFile()) return;
                      setSessionId(s.id);
                      setTab("chat");
                    }}
                  >
                    <MessageSquare size={13} />
                    <span>{s.title || "New chat"}</span>
                  </button>
                ))}
            </div>
          )}
          <div className="sidebar-bottom">
            <button
              className={view === "lab" ? "selected" : ""}
              onClick={() => navigate("lab")}
            >
              <FlaskConical size={17} />
              <span>Harness Lab</span>
              <span className="mini-label">LAB</span>
            </button>
            <button
              className={view === "settings" ? "selected" : ""}
              onClick={() => navigate("settings")}
            >
              <Settings2 size={17} />
              <span>Settings</span>
            </button>
            <div className="local-note">
              <ShieldCheck size={12} />
              <span>Your space. Your machine.</span>
            </div>
          </div>
        </aside>
      )}
      {sidebarOpen && (
        <ResizeHandle
          value={sidebarWidth}
          onChange={setSidebarWidth}
          min={190}
          max={320}
          label="Sidebar width"
        />
      )}
      <div className="main-shell">
        <header className="titlebar">
          <div className="titlebar-left">
            <Button
              size="icon"
              variant="ghost"
              aria-label="Toggle sidebar"
              onClick={() => setSidebarOpen((v) => !v)}
            >
              <PanelLeft size={17} />
            </Button>
            <span className="titlebar-path">
              {workspace ? (
                <>
                  <FolderOpen size={14} />
                  {workspace.name}
                  <ChevronRight size={12} />
                  <span>
                    {view === "chat"
                      ? session?.title || "New conversation"
                      : navigation.find((n) => n[0] === view)?.[1] ||
                        { settings: "Settings", lab: "Harness Lab" }[
                          view as "settings" | "lab"
                        ]}
                  </span>
                </>
              ) : (
                "Gemma Desktop"
              )}
            </span>
          </div>
          <div className="titlebar-right">
            <button className="model-top" onClick={() => navigate("models")}>
              <Cpu size={14} />
              <span>{state.settings.defaultModel || "Choose a model"}</span>
              <ChevronDown size={12} />
            </button>
            {view === "chat" && (
              <>
                <span className="titlebar-divider" />
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="Toggle inspector"
                  aria-pressed={inspector}
                  onClick={() => setInspector((v) => !v)}
                >
                  <PanelRight size={17} />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="New conversation"
                  onClick={newSession}
                >
                  <Plus size={17} />
                </Button>
              </>
            )}
          </div>
        </header>
        {backendError && (
          <div className="backend-error" role="alert">
            <AlertTriangle size={15} />
            <span>{backendError}</span>
            <Button size="small" variant="ghost" onClick={refresh}>
              <RefreshCw size={13} />
              Retry
            </Button>
          </div>
        )}
        {pendingApprovals.length > 0 && (
          <div className="approval-banner">
            <ShieldCheck size={15} />
            <span>
              {pendingApprovals.length === 1
                ? "One action needs your approval"
                : `${pendingApprovals.length} actions need your approval`}
            </span>
            <Button size="small" onClick={() => setApprovalInbox(true)}>
              Review actions
              <ChevronRight size={12} />
            </Button>
          </div>
        )}
        {!ready ? (
          <div className="app-loading">
            <Orb large />
            <p>Opening your workspace…</p>
          </div>
        ) : view !== "chat" ? (
          <Suspense
            fallback={
              <div className="loading-pane">
                <LoaderCircle className="spin" />
                Opening workspace
              </div>
            }
          >
            {view === "agents" ? (
              <AgentsPage {...pageProps} />
            ) : view === "models" ? (
              <ModelsPage {...pageProps} />
            ) : view === "tools" ? (
              <ToolsPage {...pageProps} />
            ) : view === "skills" ? (
              <SkillsPage {...pageProps} />
            ) : view === "artifacts" ? (
              <ArtifactsPage {...pageProps} />
            ) : view === "settings" ? (
              <SettingsPage {...pageProps} />
            ) : view === "lab" ? (
              <LabPage {...pageProps} />
            ) : (
              <Workflows {...pageProps} />
            )}
          </Suspense>
        ) : (
          <div className="workbench">
            <main className="work-main">
              <div
                className="work-tabs"
                role="tablist"
                aria-label="Workspace views"
              >
                {(
                  [
                    ["chat", "Chat", MessageSquare],
                    ["files", "Files", FileCode2],
                    ["changes", "Changes", GitCompareArrows],
                    ["terminal", "Terminal", TerminalSquare],
                  ] as const
                ).map(([id, label, Icon]) => (
                  <button
                    key={id}
                    role="tab"
                    aria-selected={tab === id}
                    className={tab === id ? "selected" : ""}
                    disabled={id !== "chat" && !workspace}
                    onClick={() => {
                      if (id === tab || mayLeaveFile()) setTab(id);
                    }}
                  >
                    <Icon size={14} />
                    {label}
                  </button>
                ))}
                <div className="spacer" />
                {sessionId && (
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label="Delete conversation"
                    onClick={async () => {
                      if (
                        confirm("Delete this conversation and its history?")
                      ) {
                        const r = await action("session.delete", {
                          id: sessionId,
                        });
                        if (r !== undefined) setSessionId("");
                      }
                    }}
                  >
                    <Trash2 size={14} />
                  </Button>
                )}
              </div>
              {tab !== "chat" && workspace ? (
                <Suspense
                  fallback={
                    <div className="loading-pane">
                      <LoaderCircle className="spin" />
                      Opening {tab}
                    </div>
                  }
                >
                  {tab === "terminal" ? (
                    <TerminalPane key={workspaceId} workspaceId={workspaceId} />
                  ) : (
                    <FilePane
                      key={`${workspaceId}-${tab}`}
                      workspaceId={workspaceId}
                      theme={state.settings.theme}
                      diff={tab === "changes"}
                      onDirtyChange={setDirtyFile}
                    />
                  )}
                </Suspense>
              ) : (
                <>
                  <div
                    className="chat-scroll"
                    ref={scrollRef}
                    onScroll={() => {
                      const el = scrollRef.current;
                      if (el)
                        sticky.current =
                          el.scrollHeight - el.scrollTop - el.clientHeight <
                          100;
                    }}
                  >
                    {!messages.length && !streamed && !working ? (
                      <div className="welcome">
                        <div className="welcome-art">
                          <div className="orbit-ring ring-one" />
                          <div className="orbit-ring ring-two" />
                          <div className="orbit-ring ring-three" />
                          <Orb large />
                        </div>
                        <span className="eyebrow">
                          A WORKSPACE FOR YOUR IDEAS
                        </span>
                        <h1>
                          Make something
                          <br />
                          <span>worth making.</span>
                        </h1>
                        <p>
                          Think it through. Build it out. Let Gemma
                          <br className="wide-only" /> work alongside you, right
                          here on your Mac.
                        </p>
                        {!workspace ? (
                          <Button
                            variant="default"
                            onClick={() => setWorkspaceModal(true)}
                          >
                            <FolderOpen size={16} />
                            Choose a workspace
                            <ArrowUpRight size={15} />
                          </Button>
                        ) : !state.runtime.available ? (
                          <Button
                            variant="default"
                            onClick={() => navigate("models")}
                          >
                            <Cpu size={16} />
                            Set up your model
                            <ArrowUpRight size={15} />
                          </Button>
                        ) : (
                          <div className="suggestions">
                            {[
                              {
                                icon: FileCode2,
                                title: "Understand this project",
                                text: "Explore this workspace and explain its structure, key components, and how to run it.",
                              },
                              {
                                icon: GitCompareArrows,
                                title: "Review recent changes",
                                text: "Review the working tree changes in this project and identify concrete problems or missing verification.",
                              },
                              {
                                icon: BookOpen,
                                title: "Create a useful guide",
                                text: "Read this workspace and write a concise getting-started guide based on the actual files.",
                              },
                            ].map(({ icon: Icon, title, text }) => (
                              <button
                                key={title}
                                onClick={() => {
                                  setPrompt(text);
                                  composerRef.current?.focus();
                                }}
                              >
                                <Icon size={18} />
                                <span>{title}</span>
                                <ArrowUpRight size={13} />
                              </button>
                            ))}
                          </div>
                        )}
                        <div className="welcome-caption">
                          <ShieldCheck size={12} />
                          Local models. Visible work. You stay in control.
                        </div>
                      </div>
                    ) : (
                      <div className="conversation">
                        <div className="conversation-intro">
                          <span>{session?.title || "Conversation"}</span>
                          <small>{workspace?.name}</small>
                        </div>
                        {messages.map((m) => (
                          <article
                            key={m.id}
                            className={`message message-${m.role}`}
                          >
                            <div className="message-avatar">
                              {m.role === "user" ? <span>You</span> : <Orb />}
                            </div>
                            <div className="message-body">
                              <div className="message-meta">
                                <strong>
                                  {m.role === "user"
                                    ? "You"
                                    : m.role === "assistant"
                                      ? "Gemma"
                                      : m.role}
                                </strong>
                                <time>
                                  {new Date(m.createdAt).toLocaleTimeString(
                                    [],
                                    { hour: "numeric", minute: "2-digit" },
                                  )}
                                </time>
                                <Button
                                  size="icon"
                                  variant="ghost"
                                  aria-label="Copy message"
                                  onClick={() =>
                                    void navigator.clipboard
                                      .writeText(m.content)
                                      .catch((e) =>
                                        setToast({
                                          message: e.message,
                                          error: true,
                                        }),
                                      )
                                  }
                                >
                                  <Copy size={12} />
                                </Button>
                              </div>
                              <div className="markdown">
                                <SafeMarkdown text={m.content} />
                              </div>
                            </div>
                          </article>
                        ))}
                        {streamed && !hasStreamMessage && (
                          <article className="message message-assistant">
                            <div className="message-avatar">
                              <Orb />
                            </div>
                            <div className="message-body">
                              <div className="message-meta">
                                <strong>Gemma</strong>
                                <span className="stream-label">Writing</span>
                              </div>
                              <div className="markdown">
                                <SafeMarkdown text={streamed} />
                              </div>
                            </div>
                          </article>
                        )}
                        {working && !streamed && !hasStreamMessage && (
                          <div className="working-message">
                            <Orb />
                            <div>
                              <strong>
                                {run?.status === "waiting_approval"
                                  ? "Waiting for your decision"
                                  : "Working on your request"}
                              </strong>
                              <span>
                                {events
                                  .filter(
                                    (e) =>
                                      e.type !== "text" &&
                                      e.type !== "thinking" &&
                                      e.type !== "adk_event",
                                  )
                                  .at(-1)?.title || "Preparing the next step…"}
                              </span>
                            </div>
                            <LoaderCircle size={15} className="spin" />
                          </div>
                        )}
                        {approvals.map((a) => (
                          <ApprovalCard
                            key={a.id}
                            approval={a}
                            action={action}
                          />
                        ))}
                        {run?.error && (
                          <div className="notice error">
                            <AlertTriangle size={17} />
                            <div>
                              <strong>{friendlyStatus(run.status)}</strong>
                              <p>{run.error}</p>
                            </div>
                          </div>
                        )}
                        {run?.status === "interrupted" && run.resumable && (
                          <div className="notice">
                            <ShieldCheck size={18} />
                            <div>
                              <strong>
                                A saved workflow is ready to resume
                              </strong>
                              <p>
                                Continue from its supported approval checkpoint.
                              </p>
                            </div>
                            <Button
                              onClick={() =>
                                action("run.resume", { id: run.id })
                              }
                            >
                              <Play size={14} />
                              Resume
                            </Button>
                          </div>
                        )}
                        {run && !working && (
                          <div className={`run-outcome outcome-${run.status}`}>
                            {statusIcon(run.status)}
                            <span>{friendlyStatus(run.status)}</span>
                            {run.finishedAt && (
                              <small>
                                {new Date(run.finishedAt).toLocaleTimeString(
                                  [],
                                  { hour: "numeric", minute: "2-digit" },
                                )}
                              </small>
                            )}
                            <Button
                              variant="ghost"
                              size="small"
                              onClick={() => {
                                setInspector(true);
                                setInspectorTab("activity");
                              }}
                            >
                              View activity
                              <ChevronRight size={12} />
                            </Button>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                  <div className="composer-area">
                    {attachments.length > 0 && (
                      <div className="attachment-list">
                        {attachments.map((p) => (
                          <span key={p}>
                            <FileCode2 size={12} />
                            {p}
                            <button
                              aria-label={`Remove attachment ${p}`}
                              onClick={() =>
                                setAttachments((a) => a.filter((x) => x !== p))
                              }
                            >
                              <X size={12} />
                            </button>
                          </span>
                        ))}
                      </div>
                    )}
                    <div
                      className={`composer ${working ? "composer-working" : ""}`}
                    >
                      <textarea
                        ref={composerRef}
                        value={prompt}
                        onChange={(e) => setPrompt(e.target.value)}
                        onKeyDown={(e) => {
                          if (
                            e.key === "Enter" &&
                            !e.shiftKey &&
                            !e.nativeEvent.isComposing
                          ) {
                            e.preventDefault();
                            void send();
                          }
                        }}
                        placeholder={
                          !workspace
                            ? "Choose a workspace to get started…"
                            : "Ask Gemma to work on something…"
                        }
                        aria-label="Message Gemma"
                        disabled={!workspace}
                        rows={2}
                      />
                      <div className="composer-controls">
                        <div className="composer-tools">
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label="Attach workspace file"
                            onClick={() => setAttachmentModal(true)}
                            disabled={!workspace}
                          >
                            <Paperclip size={17} />
                          </Button>
                          <select
                            aria-label="Agent"
                            value={agentId}
                            onChange={(e) => {
                              setAgentId(e.target.value);
                              setModel("");
                            }}
                          >
                            <option value="">Default agent</option>
                            {state.agents.map((a) => (
                              <option key={a.id} value={a.id}>
                                {a.name}
                              </option>
                            ))}
                          </select>
                        </div>
                        <div className="composer-send">
                          <select
                            aria-label="Model for this conversation"
                            value={selectedModel}
                            onChange={(e) => setModel(e.target.value)}
                          >
                            {!state.models.some(
                              (m) => m.name === selectedModel,
                            ) && <option>{selectedModel}</option>}
                            {state.models
                              .filter((m) => !m.remote)
                              .map((m) => (
                                <option key={m.name}>{m.name}</option>
                              ))}
                          </select>
                          {working ? (
                            <Button
                              size="icon"
                              variant="default"
                              aria-label="Stop generation"
                              onClick={() =>
                                run && action("run.cancel", { id: run.id })
                              }
                              disabled={run?.status === "cancelling"}
                            >
                              <Square size={15} fill="currentColor" />
                            </Button>
                          ) : (
                            <Button
                              size="icon"
                              variant="default"
                              aria-label="Send message"
                              onClick={send}
                              disabled={!canSend}
                              loading={sending}
                            >
                              <ArrowUp size={20} />
                            </Button>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="composer-footer">
                      <span>
                        <ShieldCheck size={11} />
                        {readOnly ? "Read only" : "Changes require approval"}
                      </span>
                      <span>
                        Enter to send <span className="dot-divider">·</span>{" "}
                        Shift + Enter for a new line
                      </span>
                    </div>
                  </div>
                </>
              )}
            </main>
            {inspector && (
              <>
                <ResizeHandle
                  value={inspectorWidth}
                  onChange={setInspectorWidth}
                  min={265}
                  max={465}
                  reverse
                  label="Inspector width"
                />
                <aside className="inspector">
                  <div className="inspector-tabs">
                    {(["activity", "graph", "runtime"] as const).map((t) => (
                      <button
                        key={t}
                        className={inspectorTab === t ? "selected" : ""}
                        onClick={() => setInspectorTab(t)}
                      >
                        {t === "activity"
                          ? "Activity"
                          : t === "graph"
                            ? "Graph"
                            : "Runtime"}
                      </button>
                    ))}
                  </div>
                  {inspectorTab === "graph" ? (
                    <RunGraph state={state} run={run} />
                  ) : inspectorTab === "runtime" ? (
                    <div className="inspector-content">
                      <span className="eyebrow">MODEL & RUNTIME</span>
                      <div className="runtime-model">
                        <span className="entity-icon">
                          <Cpu size={22} />
                        </span>
                        <div>
                          <strong>{inspectedModelName}</strong>
                          <small>
                            {state.runtime.version
                              ? `Ollama ${state.runtime.version}`
                              : "Ollama runtime"}
                          </small>
                        </div>
                      </div>
                      <div className="detail-list">
                        <div>
                          <span>Availability</span>
                          <span>
                            {state.runtime.available
                              ? "Available"
                              : "Unavailable"}
                          </span>
                        </div>
                        <div>
                          <span>Host memory capacity</span>
                          <span>{formatBytes(state.runtime.memoryBytes)}</span>
                        </div>
                        <div>
                          <span>Requested context</span>
                          <span>
                            {state.settings.contextSize.toLocaleString()} tokens
                          </span>
                        </div>
                        {typeof loadedContext === "number" &&
                          loadedContext > 0 && (
                            <div>
                              <span>Loaded context</span>
                              <span>
                                {loadedContext.toLocaleString()} tokens
                              </span>
                            </div>
                          )}
                        {typeof loadedModel?.size_vram === "number" &&
                          loadedModel.size_vram > 0 && (
                            <div>
                              <span>Runtime allocation</span>
                              <span>{formatBytes(loadedModel.size_vram)}</span>
                            </div>
                          )}
                        <div>
                          <span>Step budget</span>
                          <span>{state.settings.maxSteps}</span>
                        </div>
                        <div>
                          <span>Model storage</span>
                          <span>{formatBytes(inspectedModel?.size)}</span>
                        </div>
                      </div>
                      {memoryGuidance && (
                        <p className="inspector-note memory-guidance">
                          {memoryGuidance}
                        </p>
                      )}
                      {measuredPerformance.length > 0 && (
                        <section className="runtime-performance">
                          <span className="eyebrow">MEASURED FOR THIS RUN</span>
                          <div className="detail-list">
                            {measuredPerformance.map((metric) => (
                              <div key={metric.label}>
                                <span>{metric.label}</span>
                                <span>{metric.value}</span>
                              </div>
                            ))}
                          </div>
                          <p className="inspector-note">
                            Decode speed measures token generation. Model time
                            is reported by inference; task elapsed also includes
                            tools and approvals.
                          </p>
                        </section>
                      )}
                      {state.runtime.error && (
                        <p className="error-text">{state.runtime.error}</p>
                      )}
                      <Button
                        className="full-width"
                        onClick={() => navigate("models")}
                      >
                        Manage models
                        <ArrowUpRight size={14} />
                      </Button>
                      <div className="inspector-note">
                        {measuredPerformance.length
                          ? "Host memory is capacity, not current utilization. Loaded context is reported separately from the requested setting."
                          : "Performance measurements appear when a run reports them. Host memory is capacity, not current utilization."}
                      </div>
                    </div>
                  ) : (
                    <div className="inspector-content">
                      <div className="inspector-heading">
                        <span className="eyebrow">EXECUTION HISTORY</span>
                        {run && (
                          <span className={`status-text status-${run.status}`}>
                            {statusIcon(run.status)}
                            {friendlyStatus(run.status)}
                          </span>
                        )}
                      </div>
                      {events.length ? (
                        <EventList events={events} compact />
                      ) : (
                        <div className="inspector-empty">
                          <Activity size={26} />
                          <h3>Follow the work</h3>
                          <p>
                            See the tools Gemma uses, decisions that need you,
                            and what changed.
                          </p>
                          <div className="empty-timeline">
                            <i />
                            <span />
                            <i />
                            <span />
                            <i />
                          </div>
                        </div>
                      )}
                      {state.artifacts.filter((a) => a.runId === run?.id)
                        .length > 0 && (
                        <div className="inspector-artifacts">
                          <h3>From this run</h3>
                          {state.artifacts
                            .filter((a) => a.runId === run?.id)
                            .map((a) => (
                              <button
                                key={a.id}
                                onClick={() =>
                                  action("artifact.open", { id: a.id })
                                }
                              >
                                <FileBox size={15} />
                                <span>{a.name}</span>
                                <ArrowUpRight size={12} />
                              </button>
                            ))}
                        </div>
                      )}
                      <div className="inspector-bottom">
                        <ShieldCheck size={17} />
                        <p>
                          Every action leaves a record.
                          <br />
                          Review the details at any time.
                        </p>
                      </div>
                    </div>
                  )}
                </aside>
              </>
            )}
          </div>
        )}
      </div>
      {toast && (
        <div
          className={`toast ${toast.error ? "toast-error" : ""}`}
          role={toast.error ? "alert" : "status"}
        >
          {toast.error ? <AlertTriangle size={17} /> : <Check size={17} />}
          <span>{toast.message}</span>
          <Button
            size="icon"
            variant="ghost"
            aria-label="Dismiss notification"
            onClick={() => setToast(null)}
          >
            <X size={14} />
          </Button>
        </div>
      )}
      <Modal
        open={approvalInbox && pendingApprovals.length > 0}
        onClose={() => setApprovalInbox(false)}
        title="Review pending actions"
        description="These actions are paused until you approve or deny them."
      >
        {pendingApprovals.map((a) => (
          <ApprovalCard key={a.id} approval={a} action={action} />
        ))}
      </Modal>
      <Modal
        open={workspaceModal}
        onClose={() => setWorkspaceModal(false)}
        title="Add a workspace"
        description="Choose the folder Gemma can work with. You can review changes before they are applied."
      >
        <form
          className="form-stack"
          onSubmit={(e) => {
            e.preventDefault();
            void addWorkspace();
          }}
        >
          <Button
            type="button"
            onClick={() => addWorkspace(true)}
            loading={workspaceBusy}
          >
            <FolderOpen size={17} />
            Choose folder on your Mac
          </Button>
          <div className="or-divider">
            <span />
            or enter a path
            <span />
          </div>
          <Field label="Folder path">
            <input
              required
              autoFocus
              value={workspacePath}
              onChange={(e) => setWorkspacePath(e.target.value)}
              placeholder="/Users/you/Projects/my-project"
            />
          </Field>
          <Field
            label="Workspace name"
            hint="Optional. Uses the folder name by default."
          >
            <input
              value={workspaceName}
              onChange={(e) => setWorkspaceName(e.target.value)}
              placeholder="My project"
            />
          </Field>
          <div className="modal-actions">
            <Button type="button" onClick={() => setWorkspaceModal(false)}>
              Cancel
            </Button>
            <Button
              variant="default"
              type="submit"
              disabled={!workspacePath.trim()}
              loading={workspaceBusy}
            >
              Add workspace
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={attachmentModal}
        onClose={() => setAttachmentModal(false)}
        title="Attach a workspace file"
        description="Use a path relative to the selected workspace. The backend validates access."
      >
        <form
          className="form-stack"
          onSubmit={(e) => {
            e.preventDefault();
            if (attachmentPath.trim()) {
              setAttachments((a) =>
                Array.from(new Set([...a, attachmentPath.trim()])),
              );
              setAttachmentPath("");
              setAttachmentModal(false);
            }
          }}
        >
          <Field label="Relative file path">
            <input
              required
              autoFocus
              value={attachmentPath}
              onChange={(e) => setAttachmentPath(e.target.value)}
              placeholder="src/main.go"
            />
          </Field>
          <div className="modal-actions">
            <Button type="submit" variant="default">
              <Paperclip size={15} />
              Attach file
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={commandOpen}
        onClose={() => setCommandOpen(false)}
        title="Go somewhere"
        description="Switch views or open a recent conversation."
      >
        <div className="command-input">
          <Search size={17} />
          <input
            autoFocus
            placeholder="Search views and conversations…"
            value={commandQuery}
            onChange={(e) => setCommandQuery(e.target.value)}
          />
          <kbd>ESC</kbd>
        </div>
        <div className="command-results">
          {[
            ...navigation,
            ["settings", "Settings", Settings2] as const,
            ["lab", "Harness Lab", FlaskConical] as const,
          ]
            .filter(([, label]) =>
              label.toLowerCase().includes(commandQuery.toLowerCase()),
            )
            .map(([v, label, Icon]) => (
              <button key={v} onClick={() => navigate(v)}>
                <Icon size={17} />
                <span>{label}</span>
                <ArrowUpRight size={14} />
              </button>
            ))}
          {state.sessions
            .filter((s) =>
              s.title.toLowerCase().includes(commandQuery.toLowerCase()),
            )
            .slice(0, 8)
            .map((s) => (
              <button
                key={s.id}
                onClick={() => {
                  if (!mayLeaveFile()) return;
                  setWorkspaceId(s.workspaceId);
                  setSessionId(s.id);
                  navigate("chat");
                  setTab("chat");
                }}
              >
                <MessageSquare size={17} />
                <span>{s.title}</span>
                <small>
                  {state.workspaces.find((w) => w.id === s.workspaceId)?.name}
                </small>
              </button>
            ))}
        </div>
      </Modal>
    </div>
  );
}
