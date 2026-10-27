export interface Workspace {
  id: string;
  name: string;
  path: string;
  createdAt: string;
}
export interface Session {
  id: string;
  workspaceId: string;
  title: string;
  createdAt: string;
  updatedAt: string;
}
export interface Message {
  id: string;
  sessionId: string;
  role: string;
  content: string;
  createdAt: string;
  runId?: string;
}
export interface Run {
  id: string;
  sessionId: string;
  workspaceId: string;
  status: string;
  model: string;
  prompt: string;
  startedAt: string;
  finishedAt?: string;
  error?: string;
  agentId?: string;
  workflowId?: string;
  summary?: string;
  resumable?: boolean;
  usage?: {
    eval_count?: number;
    eval_duration?: number;
    prompt_eval_count?: number;
    prompt_eval_duration?: number;
    total_duration?: number;
  };
}
export interface Event {
  id: string;
  seq: number;
  runId: string;
  type: string;
  title: string;
  detail: string;
  createdAt: string;
  nodeId?: string;
  tool?: string;
  data?: unknown;
}
export interface Approval {
  id: string;
  runId: string;
  tool: string;
  arguments: unknown;
  reason: string;
  status: string;
  createdAt: string;
}
export interface Artifact {
  id: string;
  runId: string;
  workspaceId: string;
  name: string;
  path: string;
  mediaType: string;
  size: number;
  createdAt: string;
  before?: string;
  after?: string;
}
export interface Agent {
  id: string;
  name: string;
  instructions: string;
  model: string;
  maxSteps: number;
  mode: string;
}
export interface WorkflowNode {
  id: string;
  type: string;
  label: string;
  agentId?: string;
  prompt?: string;
  condition?: string;
  position: { x: number; y: number };
}
export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
  condition?: string;
}
export interface Workflow {
  id: string;
  name: string;
  description: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}
export interface Model {
  name: string;
  size: number;
  digest: string;
  family: string;
  parameterSize: string;
  quantization: string;
  capabilities?: string[];
  loaded?: boolean;
  remote?: boolean;
  contextLength?: number;
  loadedContextLength?: number;
  runner?: string;
}
export interface Pull {
  name?: string;
  model?: string;
  status?: string;
  completed?: number;
  total?: number;
  error?: string;
}
export interface Runtime {
  available: boolean;
  version: string;
  error?: string;
  memoryBytes?: number;
  models?: Array<{
    name: string;
    model?: string;
    size_vram?: number;
    context_length?: number;
  }>;
  pulls?: Pull[];
  [key: string]: unknown;
}
export interface Settings {
  ollamaUrl: string;
  defaultModel: string;
  contextSize: number;
  maxSteps: number;
  mode: string;
  theme: string;
}
export interface MCPServer {
  id: string;
  name: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  enabled: boolean;
  status: string;
  error?: string;
  tools?: Array<{ name: string; description?: string; inputSchema?: unknown }>;
}
export interface Skill {
  id: string;
  name: string;
  description: string;
  path: string;
}
export interface EvaluationResult {
  agentId: string;
  agentName: string;
  task: string;
  status: string;
  content: string;
  error?: string;
  durationMs: number;
  usage?: unknown;
  toolCalls: number;
}
export interface Experiment {
  id: string;
  status: string;
  createdAt: string;
  results?: EvaluationResult[];
  inputDigest?: string;
  qualification?: string;
  error?: string;
  [key: string]: unknown;
}
export interface State {
  workspaces: Workspace[];
  sessions: Session[];
  runs: Run[];
  agents: Agent[];
  workflows: Workflow[];
  models: Model[];
  artifacts: Artifact[];
  approvals: Approval[];
  tools: MCPServer[];
  skills: Skill[];
  settings: Settings;
  runtime: Runtime;
  events: Event[];
  experiments: Experiment[];
}
export const emptyState: State = {
  workspaces: [],
  sessions: [],
  runs: [],
  agents: [],
  workflows: [],
  models: [],
  artifacts: [],
  approvals: [],
  tools: [],
  skills: [],
  events: [],
  experiments: [],
  settings: {
    ollamaUrl: "http://127.0.0.1:11434",
    defaultModel: "gemma4:12b-mlx",
    contextSize: 32768,
    maxSteps: 24,
    mode: "approval",
    theme: "dark",
  },
  runtime: { available: false, version: "" },
};
export const activeStatuses = new Set([
  "queued",
  "running",
  "waiting_approval",
  "cancelling",
]);
