import test from "node:test";
import assert from "node:assert/strict";
import { commandOutcome, verificationRunner } from "../src/lib/receipt.ts";

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

test("only a host-marked check is attributed to Gemma Desktop", () => {
  const check = { tool: "verify_task", status: "completed", callId: "tool_x" };
  assert.equal(
    verificationRunner({ ...check, hostInitiated: true }),
    "Checked by Gemma Desktop after Gemma finished",
  );
  // The call ID prefix is not provenance; an unmarked check is the model's.
  assert.equal(verificationRunner(check), "Run by Gemma");
  assert.equal(
    verificationRunner({ ...check, hostInitiated: false }),
    "Run by Gemma",
  );
  // A check whose start left the journal window is not credited to the model.
  assert.equal(
    verificationRunner({ ...check, initiatorUnrecorded: true }),
    "Starter not recorded",
  );
});
