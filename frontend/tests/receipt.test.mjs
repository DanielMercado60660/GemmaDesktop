import test from "node:test";
import assert from "node:assert/strict";
import { commandOutcome } from "../src/lib/receipt.ts";

test("observed command outcome cannot invent an exit or correctness grade", () => {
  assert.equal(
    commandOutcome({ status: "completed", exitCode: 0 }),
    "Completed · exit 0",
  );
  assert.equal(
    commandOutcome({ status: "failed", exitCode: 1 }),
    "Failed · exit 1",
  );
  assert.equal(
    commandOutcome({ status: "pending" }),
    "Outcome unresolved · exit not recorded",
  );
  assert.equal(
    commandOutcome({
      status: "failed",
      cancelled: true,
      exitCode: -1,
      signal: "killed",
      truncated: true,
    }),
    "Cancelled · exit -1 · signal killed · output truncated",
  );
  assert.equal(
    commandOutcome({ status: "completed", exitCode: null }),
    "Completed · exit not recorded",
  );
});
