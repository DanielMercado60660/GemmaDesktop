import { it, expect } from "vitest";
import {
  cacheSession,
  refreshDelay,
  type SessionHistory,
} from "../../src/lib/conversation";

it("bounds cached conversations and protects selection during a late response", () => {
  const cache = new Map<string, SessionHistory>();
  const history: SessionHistory = {
    messages: [],
    events: [],
    cursor: 0,
    hasMore: false,
  };
  cacheSession(cache, "selected", history, "selected");
  for (let i = 0; i < 100; i++)
    cacheSession(cache, `chat-${i}`, history, "selected");
  expect(cache.size).toBe(8);
  expect(cache.has("selected")).toBe(true);
  expect(cache.has("chat-0")).toBe(false);
  expect(cache.has("chat-99")).toBe(true);
  cacheSession(cache, "chat-94", history, "selected");
  cacheSession(cache, "late-response", history, "selected");
  expect(cache.has("chat-94")).toBe(true);
  expect(cache.has("late-response")).toBe(true);
  expect(cache.size).toBe(8);
});
it("backs off failures and hidden windows while visible runs and event catch-up remain responsive", () => {
  const idle = { hidden: false, failures: 0, catchingUp: false, busy: false };
  expect(refreshDelay(idle)).toBe(2200);
  expect(refreshDelay({ ...idle, busy: true })).toBe(700);
  expect(refreshDelay({ ...idle, catchingUp: true })).toBe(80);
  expect(
    [1, 2, 3, 10].map((failures) => refreshDelay({ ...idle, failures })),
  ).toEqual([3000, 6000, 12000, 30000]);
  expect(
    refreshDelay({ ...idle, hidden: true, busy: true, catchingUp: true }),
  ).toBe(30000);
  expect(refreshDelay(idle)).toBe(2200);
});
