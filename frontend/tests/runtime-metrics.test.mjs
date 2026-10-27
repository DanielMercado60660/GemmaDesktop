import test from "node:test";
import assert from "node:assert/strict";
import { runMetrics, modelMemoryGuidance } from "../src/lib/runtimeMetrics.ts";

test("converts Ollama nanoseconds and separates decode from task wall time", () => {
  const values = Object.fromEntries(
    runMetrics({
      startedAt: "2026-10-27T10:00:00Z",
      finishedAt: "2026-10-27T10:00:30Z",
      usage: {
        eval_count: 120,
        eval_duration: 2e9,
        prompt_eval_count: 500,
        prompt_eval_duration: 5e8,
        total_duration: 3e9,
      },
    }).map((row) => [row.label, row.value]),
  );
  assert.equal(values["Decode speed"], "60.0 tokens/s");
  assert.equal(values["Decode time"], "2.00 s");
  assert.equal(values["Prompt processing"], "0.50 s");
  assert.equal(values["Model time"], "3.00 s");
  assert.equal(values["Task elapsed"], "30.00 s");
});

test("does not invent metrics, divide by zero, or accept invalid measurements", () => {
  assert.deepEqual(runMetrics(), []);
  assert.deepEqual(runMetrics({ usage: {} }), []);
  assert.equal(
    runMetrics({ usage: { eval_count: 100 } }).some(
      (row) => row.label === "Decode speed",
    ),
    false,
  );
  assert.equal(
    runMetrics({ usage: { eval_count: 100, eval_duration: 0 } }).some(
      (row) => row.label === "Decode speed",
    ),
    false,
  );
  assert.deepEqual(
    runMetrics({
      startedAt: "invalid",
      finishedAt: "invalid",
      usage: { eval_duration: NaN, total_duration: -1 },
    }),
    [],
  );
});

test("weights guidance depends on real host capacity and does not imply utilization", () => {
  assert.equal(modelMemoryGuidance(1024), undefined);
  assert.equal(modelMemoryGuidance(0, 4096), undefined);
  assert.match(modelMemoryGuidance(1024, 4096), /25%/);
  assert.match(
    modelMemoryGuidance(1024, 4096),
    /context and runtime overhead add memory/,
  );
  assert.match(modelMemoryGuidance(8192, 4096), /consider a smaller model/);
});
