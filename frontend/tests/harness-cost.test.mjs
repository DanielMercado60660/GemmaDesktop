import assert from "node:assert/strict";
import test from "node:test";
import {
  costComparisonExplanation,
  hasQualifiedCostComparison,
} from "../src/lib/harnessCost.ts";

const complete = {
  costAccountingStatus: "complete",
  costComparisonValid: true,
  timeRatio: 0.9,
  tokenRatio: 1.1,
};

test("qualified cost ratios require explicit complete accounting", () => {
  assert.equal(hasQualifiedCostComparison(complete), true);
  assert.equal(hasQualifiedCostComparison({ timeRatio: 0.9, tokenRatio: 1.1 }), false);
  assert.equal(hasQualifiedCostComparison(undefined), false);
  assert.match(costComparisonExplanation(undefined), /unavailable/);
});

test("positive partial totals and legacy eligibility cannot establish cost compliance", () => {
  const partial = { ...complete, eligible: true, costAccountingStatus: "partial" };
  assert.equal(hasQualifiedCostComparison(partial), false);
  assert.match(costComparisonExplanation(partial), /partial.*interrupted generations/);
  assert.equal(hasQualifiedCostComparison({ ...complete, costAccountingStatus: "unknown" }), false);
  assert.equal(hasQualifiedCostComparison({ ...complete, costAccountingStatus: "unexpected" }), false);
});

test("invalid or unqualified measurements never render as authoritative ratios", () => {
  assert.equal(hasQualifiedCostComparison({ ...complete, costComparisonValid: false }), false);
  assert.equal(hasQualifiedCostComparison({ ...complete, costComparisonValid: undefined }), false);
  for (const value of [0, -1, NaN, Infinity, undefined, "1.0"]) {
    assert.equal(hasQualifiedCostComparison({ ...complete, timeRatio: value }), false);
    assert.equal(hasQualifiedCostComparison({ ...complete, tokenRatio: value }), false);
  }
  assert.match(costComparisonExplanation({ ...complete, costComparisonValid: false }), /missing or invalid/);
});
