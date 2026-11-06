import { it, expect, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { writeFileSync } from "node:fs";
import type { ComponentType } from "react";
import App from "../../src/App";
import BaselineApp from "@baseline-app";
import { ConfirmationProvider } from "../../src/components/Confirmation";
import { api } from "../../src/bridge";
import { fixture } from "../ui/fixtures";
import type { Message } from "../../src/types";

vi.mock("../../src/bridge", () => ({ api: vi.fn(), openExternal: vi.fn() }));
vi.mock("../../src/components/RunGraph", () => ({
  default: () => <div>Run graph</div>,
}));

// This is a rendered jsdom workload, not a native-webview/frame-rate benchmark.
// Responses are serialized then decoded to model the bridge payload. Native
// IPC, server query time, layout, paint and GPU cost are outside the measurement.
it("measures unchanged polling of a 10,000-message chat before and after pagination", async () => {
  const state = fixture();
  state.entityCursor = 10;
  const messages: Message[] = Array.from({ length: 10000 }, (_, index) => ({
    id: `message-${index}`,
    sessionId: "s",
    role: index % 2 ? "assistant" : "user",
    content: `Message ${index}: ${"Some representative local coding conversation text. ".repeat(4)}`,
    createdAt: "2026-11-04T12:00:00Z",
  }));
  const latest = {
    messages: messages.slice(-100),
    messageCursor: "stable",
    messageBefore: "9900",
    messagesHaveMore: true,
    events: [],
    eventCursor: 0,
    hasMore: false,
  };
  const full = { messages, events: [], eventCursor: 0, hasMore: false };
  const unchanged = { ...latest, messages: [], messagesUnchanged: true };
  const delta = {
    changes: [],
    cursor: 10,
    hasMore: false,
    resync: false,
    live: {},
    liveCursor: "same-live",
    events: [],
    eventCursor: 0,
    eventsHaveMore: false,
  };
  const results: Array<{
    variant: string;
    sample: number;
    initialMs: number;
    updateMedianMs: number;
    updateP95Ms: number;
    responseBytes: number;
    sessionPolls: number;
    visibleMessages: number;
  }> = [];
  vi.useFakeTimers({
    toFake: [
      "setTimeout",
      "clearTimeout",
      "setInterval",
      "clearInterval",
      "Date",
    ],
  });
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
  try {
    for (let sample = 0; sample < 3; sample++) {
      for (const variant of sample % 2
        ? ["current", "baseline"]
        : ["baseline", "current"]) {
        localStorage.setItem("gemma.workspace", "w");
        localStorage.setItem("gemma.session", "s");
        let responseBytes = 0,
          sessionPolls = 0;
        vi.mocked(api).mockImplementation(async (method, params) => {
          let result: unknown;
          if (method === "state") result = state;
          else if (method === "state.refresh")
            result = {
              ...delta,
              live:
                (params as { liveCursor?: string }).liveCursor ===
                delta.liveCursor
                  ? {}
                  : { runtime: state.runtime, models: state.models },
            };
          else if (method === "session.get") {
            sessionPolls++;
            result =
              variant === "baseline"
                ? full
                : (params as { messageCursor?: string }).messageCursor
                  ? unchanged
                  : latest;
          } else if (method === "draft.get")
            result = {
              ...(params as object),
              text: "",
              attachments: [],
              revision: 0,
            };
          else throw new Error(`Unexpected benchmark request: ${method}`);
          const encoded = JSON.stringify(result);
          responseBytes += Buffer.byteLength(encoded);
          return JSON.parse(encoded);
        });
        const Component: ComponentType =
          variant === "baseline" ? BaselineApp : App;
        const start = performance.now();
        await act(async () => {
          render(
            <ConfirmationProvider>
              <Component />
            </ConfirmationProvider>,
          );
          await vi.advanceTimersByTimeAsync(0);
        });
        const initialMs = performance.now() - start;
        expect(screen.getByText(/^Message 9999:/)).toBeTruthy();
        expect(document.querySelectorAll(".message").length).toBe(40);
        // Warm up each mounted implementation equally, then measure 12 separate
        // idle polls. Timing includes each React commit and bridge decode.
        await act(async () => {
          await vi.advanceTimersByTimeAsync(2200);
        });
        responseBytes = 0;
        sessionPolls = 0;
        const samples: number[] = [];
        for (let index = 0; index < 12; index++) {
          const tick = performance.now();
          await act(async () => {
            await vi.advanceTimersByTimeAsync(2200);
          });
          samples.push(performance.now() - tick);
        }
        samples.sort((a, b) => a - b);
        expect(sessionPolls).toBe(12);
        expect(document.querySelectorAll(".message").length).toBe(40);
        results.push({
          variant,
          sample: sample + 1,
          initialMs,
          updateMedianMs: samples[Math.floor(samples.length / 2)],
          updateP95Ms: samples[Math.floor(samples.length * 0.95)],
          responseBytes,
          sessionPolls,
          visibleMessages: 40,
        });
        cleanup();
        vi.clearAllTimers();
      }
    }
  } finally {
    vi.useRealTimers();
  }
  const report = {
    baseline: "d8dbce0741994373a5cc4677e62791d1ff59a806",
    environment: `Node ${process.version}, Vitest/jsdom, ${process.platform}/${process.arch}`,
    messages: messages.length,
    currentInitialPage: 100,
    pollsPerSample: 12,
    caveat:
      "No native IPC, backend-query, layout, paint, GPU or frame-rate measurement. Baseline App/conversation/composer are pinned; shared UI dependencies are current. Response sizes are UTF-8 JSON, not an actual Wails wire trace.",
    results,
  };
  writeFileSync(
    "/private/tmp/gemma-history-benchmark.json",
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report, null, 2));
});
