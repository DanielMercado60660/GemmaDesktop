import { emptyState, type State, type Event } from "../types";
import { mergeEvents } from "./conversation";
export interface StateRefreshPage {
  changes: Array<{
    sequence: number;
    kind: string;
    id: string;
    deleted?: boolean;
    entity?: Record<string, unknown>;
  }>;
  cursor: number;
  hasMore: boolean;
  resync: boolean;
  liveCursor?: string;
  live: Partial<
    Pick<State, "runtime" | "models" | "managedRuntime" | "effectiveProfiles">
  >;
  events: Event[];
  eventCursor: number;
  eventsHaveMore: boolean;
}
export function normalizeState(snapshot: Partial<State>): State {
  const next = {
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
  ] as const)
    if (!Array.isArray(next[key])) (next[key] as unknown) = [];
  next.events = next.events.slice(-500);
  return next;
}
const collections = {
  workspace: "workspaces",
  session: "sessions",
  run: "runs",
  agent: "agents",
  workflow: "workflows",
  approval: "approvals",
} as const;
// A null result requests a full snapshot. Older pages never regress either
// cursor or overwrite newer live values, even if callers race in the future.
export function applyStateRefresh(
  previous: State,
  page: StateRefreshPage,
  after: number,
  eventAfter: number,
): {
  state: State;
  cursor: number;
  eventCursor: number;
  hasMore: boolean;
} | null {
  if (page.resync) return null;
  if (page.cursor < after || page.eventCursor < eventAfter)
    return {
      state: previous,
      cursor: after,
      eventCursor: eventAfter,
      hasMore: false,
    };
  if (
    (page.hasMore && page.cursor <= after) ||
    (page.eventsHaveMore && page.eventCursor <= eventAfter)
  )
    throw new Error("State updates did not advance. Refresh to retry.");
  let next = previous;
  for (const change of [...page.changes].sort(
    (a, b) => a.sequence - b.sequence,
  )) {
    if (change.sequence <= after) continue;
    if (
      change.sequence > page.cursor ||
      (!change.deleted && (!change.entity || change.entity.id !== change.id))
    )
      return null;
    if (change.kind === "settings") {
      if (change.deleted || !change.entity) return null;
      next = { ...next, settings: { ...next.settings, ...change.entity } };
      continue;
    }
    const key = collections[change.kind as keyof typeof collections];
    if (!key) return null;
    const existing = next[key];
    const index = existing.findIndex((item) => item.id === change.id);
    const items = change.deleted
      ? existing.filter((item) => item.id !== change.id)
      : index < 0
        ? [...existing, change.entity]
        : existing.map((item, i) => (i === index ? change.entity : item));
    next = { ...next, [key]: items };
  }
  const mergedEvents = mergeEvents(next.events, page.events || []);
  const events =
    mergedEvents.length > 500 ? mergedEvents.slice(-500) : mergedEvents;
  const liveCursor = page.liveCursor ?? next.liveCursor;
  if (
    events !== next.events ||
    next.entityCursor !== page.cursor ||
    liveCursor !== next.liveCursor ||
    Object.entries(page.live).some(
      ([key, value]) => next[key as keyof State] !== value,
    )
  ) {
    next = {
      ...next,
      ...page.live,
      events,
      entityCursor: page.cursor,
      liveCursor,
    };
  }
  return {
    state: next,
    cursor: page.cursor,
    eventCursor: page.eventCursor,
    hasMore: page.hasMore || page.eventsHaveMore,
  };
}
