import type { Event, Message, Run, Session, Workspace } from "../types";

export function restoreSelection(
  workspaces: Workspace[],
  sessions: Session[],
  workspaceId: string,
  sessionId: string,
) {
  const workspace =
    workspaces.find((w) => w.id === workspaceId) || workspaces[0];
  return {
    workspaceId: workspace?.id || "",
    sessionId: sessions.some(
      (s) => s.id === sessionId && s.workspaceId === workspace?.id,
    )
      ? sessionId
      : "",
  };
}

export function mergeEvents(previous: Event[], incoming: Event[]): Event[] {
  if (!incoming.length) return previous;
  const events = new Map(previous.map((e) => [e.seq || e.id, e]));
  let added = false;
  for (const event of incoming)
    if (!events.has(event.seq || event.id)) {
      events.set(event.seq || event.id, event);
      added = true;
    }
  if (!added) return previous;
  return Array.from(events.values()).sort((a, b) => a.seq - b.seq);
}

export interface SessionHistory {
  messageCursor?: string;
  messageBefore?: string;
  messagesHaveMore?: boolean;
  newerMessages?: boolean;
  messages: Message[];
  events: Event[];
  cursor: number;
  hasMore: boolean;
}

// Keep recently viewed conversations, while protecting the selected and
// currently arriving conversation from eviction during navigation races.
export function cacheSession(
  cache: Map<string, SessionHistory>,
  id: string,
  history: SessionHistory,
  selectedId: string,
  limit = 8,
) {
  cache.delete(id);
  cache.set(id, history);
  for (const key of cache.keys()) {
    if (cache.size <= Math.max(2, limit)) break;
    if (key !== id && key !== selectedId) cache.delete(key);
  }
}

export function refreshDelay({
  hidden,
  failures,
  catchingUp,
  busy,
}: {
  hidden: boolean;
  failures: number;
  catchingUp: boolean;
  busy: boolean;
}) {
  if (hidden) return 30000;
  if (failures) return Math.min(30000, 3000 * 2 ** Math.min(failures - 1, 4));
  return catchingUp ? 80 : busy ? 700 : 2200;
}
export interface SessionPage {
  messageCursor?: string;
  messagesUnchanged?: boolean;
  messageBefore?: string;
  messagesHaveMore?: boolean;
  messages: Message[];
  events: Event[];
  eventCursor?: number;
  hasMore?: boolean;
}
export function mergeSessionPage(
  previous: SessionHistory,
  page: SessionPage,
): SessionHistory {
  const events = page.events || [];
  const cursor =
    page.eventCursor ?? Math.max(previous.cursor, ...events.map((e) => e.seq));
  if (page.hasMore && cursor <= previous.cursor)
    throw new Error(
      "Execution history did not advance. Refresh this conversation to retry.",
    );
  return {
    messages: page.messagesUnchanged
      ? previous.messages
      : (page.messages || []).slice(-1000),
    messageCursor: page.messageCursor ?? previous.messageCursor,
    messageBefore: page.messagesUnchanged
      ? previous.messageBefore
      : page.messageBefore,
    messagesHaveMore: page.messagesUnchanged
      ? previous.messagesHaveMore
      : !!page.messagesHaveMore,
    newerMessages: page.messagesUnchanged ? previous.newerMessages : false,
    events: mergeEvents(previous.events, events),
    cursor,
    hasMore: !!page.hasMore,
  };
}

export interface OlderMessagesPage {
  messages: Message[];
  messageBefore: string;
  messagesHaveMore: boolean;
  messageCursor: string;
}
export function prependMessages(
  previous: SessionHistory,
  page: OlderMessagesPage,
): SessionHistory {
  if (!previous.messageCursor || page.messageCursor !== previous.messageCursor)
    throw new Error(
      "Conversation changed while loading history. Refresh the latest messages before loading earlier ones.",
    );
  if (page.messagesHaveMore && page.messageBefore === previous.messageBefore)
    throw new Error(
      "Earlier history did not advance. Refresh the latest messages to retry.",
    );
  const seen = new Set<string>();
  const messages = [...page.messages, ...previous.messages].filter(
    (message) => {
      if (seen.has(message.id)) return false;
      seen.add(message.id);
      return true;
    },
  );
  return {
    ...previous,
    messages: messages.slice(0, 1000),
    messageBefore: page.messageBefore,
    messagesHaveMore: page.messagesHaveMore,
    newerMessages: previous.newerMessages || messages.length > 1000,
  };
}

export function currentRun(runs: Run[]): Run | undefined {
  const sorted = [...runs].sort(
    (a, b) => Date.parse(a.startedAt) - Date.parse(b.startedAt),
  );
  return (
    sorted
      .filter((r) =>
        ["queued", "running", "waiting_approval", "cancelling"].includes(
          r.status,
        ),
      )
      .at(-1) || sorted.at(-1)
  );
}

export function streamedOutput(
  run: Run | undefined,
  messages: Message[],
  events: Event[],
) {
  if (
    !run ||
    messages.some((m) => m.role === "assistant" && m.runId === run.id)
  )
    return "";
  let text = "",
    boundary = false;
  for (const event of events) {
    if (event.runId !== run.id) continue;
    if (event.type === "text") {
      if (boundary && text && event.detail) text += "\n\n";
      text += event.detail || "";
      boundary = false;
    } else if (
      event.type === "tool_started" ||
      event.type === "node_started" ||
      (event.type === "status" && event.title === "Generating")
    )
      boundary = true;
  }
  return text;
}

// A saved answer keeps its own outcome after later turns become current. Tool
// failures alone do not make an otherwise complete assistant answer partial.
export function savedMessageOutcome(message: Message, run?: Run) {
  if (message.role !== "assistant") return undefined;
  const owner = run?.id === message.runId ? run : undefined;
  if (message.incomplete === true || owner?.incomplete === true)
    return {
      label: "Incomplete response",
      detail:
        owner?.error || "The model stopped before completing this response.",
    };
  const label =
    owner?.status === "failed"
      ? "Run failed"
      : owner?.status === "cancelled"
        ? "Run cancelled"
        : owner?.status === "interrupted"
          ? "Run interrupted"
          : undefined;
  return label
    ? {
        label,
        detail:
          owner?.error ||
          "This response belongs to a run that did not complete.",
      }
    : undefined;
}

export function eventStatus(event: Event): string | undefined {
  if (
    event.type === "run_completed" &&
    event.data &&
    typeof event.data === "object" &&
    "status" in event.data
  )
    return String(event.data.status);
  if (event.type.includes("failed") || event.type === "error") return "failed";
  if (event.type.includes("completed")) return "completed";
  if (event.type === "approval") return "approval_requested";
}

// A mutation which finishes during a poll must get a subsequent snapshot.
// Coalesce simultaneous callers, but never silently drop that fresh request.
export function createRefreshCoordinator(task: () => Promise<void>) {
  let inFlight: Promise<void> | undefined,
    repeat = false;
  return (fresh = false): Promise<void> => {
    if (inFlight) {
      if (fresh) repeat = true;
      return inFlight;
    }
    inFlight = (async () => {
      do {
        repeat = false;
        await task();
      } while (repeat);
    })().finally(() => {
      inFlight = undefined;
    });
    return inFlight;
  };
}

export interface FileVersion {
  path: string;
  content: string;
  hash: string;
}
export interface SavedChange {
  path: string;
  after: string;
  afterHash: string;
}
export function reconcileSavedFile(
  submitted: string,
  latestDraft: string,
  change: SavedChange,
) {
  return {
    file: { path: change.path, content: change.after, hash: change.afterHash },
    content: latestDraft === submitted ? change.after : latestDraft,
  };
}
