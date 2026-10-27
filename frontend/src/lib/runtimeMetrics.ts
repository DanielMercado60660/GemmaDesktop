import type { Run } from "../types";

export interface MetricRow {
  label: string;
  value: string;
}
function measured(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}
function seconds(value: number) {
  return `${value.toFixed(2)} s`;
}

// Ollama durations are nanoseconds. Never substitute missing measurements.
export function runMetrics(run?: Run): MetricRow[] {
  const rows: MetricRow[] = [];
  const usage = run?.usage;
  if (usage) {
    if (
      measured(usage.eval_count) &&
      measured(usage.eval_duration) &&
      usage.eval_duration > 0
    )
      rows.push({
        label: "Decode speed",
        value: `${(usage.eval_count / (usage.eval_duration / 1e9)).toFixed(1)} tokens/s`,
      });
    if (measured(usage.eval_count))
      rows.push({
        label: "Generated tokens",
        value: usage.eval_count.toLocaleString(),
      });
    if (measured(usage.eval_duration))
      rows.push({
        label: "Decode time",
        value: seconds(usage.eval_duration / 1e9),
      });
    if (measured(usage.prompt_eval_count))
      rows.push({
        label: "Prompt tokens evaluated",
        value: usage.prompt_eval_count.toLocaleString(),
      });
    if (measured(usage.prompt_eval_duration))
      rows.push({
        label: "Prompt processing",
        value: seconds(usage.prompt_eval_duration / 1e9),
      });
    if (measured(usage.total_duration))
      rows.push({
        label: "Model time",
        value: seconds(usage.total_duration / 1e9),
      });
  }
  if (run?.startedAt && run.finishedAt) {
    const elapsed = Date.parse(run.finishedAt) - Date.parse(run.startedAt);
    if (measured(elapsed))
      rows.push({ label: "Task elapsed", value: seconds(elapsed / 1000) });
  }
  return rows;
}

export function modelMemoryGuidance(
  weightsBytes?: number,
  hostBytes?: number,
): string | undefined {
  if (
    !measured(weightsBytes) ||
    !measured(hostBytes) ||
    !weightsBytes ||
    !hostBytes
  )
    return;
  const share = weightsBytes / hostBytes;
  const comparison =
    share >= 1
      ? "Model weights meet or exceed host memory capacity; consider a smaller model."
      : `Model weights are about ${(share * 100).toFixed(0)}% of host memory capacity.`;
  return `${comparison} Weights size only; context and runtime overhead add memory. This is not current memory utilization or a fit guarantee.`;
}
