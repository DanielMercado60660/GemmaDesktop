export interface HarnessCostGate {
  costAccountingStatus?: string;
  costComparisonValid?: boolean;
  timeRatio?: number;
  tokenRatio?: number;
}

export function hasQualifiedCostComparison(gate?: HarnessCostGate): boolean {
  return (
    gate?.costAccountingStatus === "complete" &&
    gate.costComparisonValid === true &&
    typeof gate.timeRatio === "number" &&
    Number.isFinite(gate.timeRatio) &&
    gate.timeRatio > 0 &&
    typeof gate.tokenRatio === "number" &&
    Number.isFinite(gate.tokenRatio) &&
    gate.tokenRatio > 0
  );
}

export function costComparisonExplanation(gate?: HarnessCostGate): string {
  if (gate?.costAccountingStatus === "partial") {
    return "Token totals are partial and may omit interrupted generations. No complete cost comparison is available.";
  }
  if (gate?.costAccountingStatus !== "complete") {
    return "Complete token accounting is unavailable for these results. No cost comparison is established.";
  }
  return "The cost comparison is unavailable because required measurements are missing or invalid.";
}
