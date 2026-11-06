import type { Agent } from "../types";

// Gemma 4 publisher starting point, not a proven optimum for every model/task.
export const DEFAULT_AGENT_TEMPERATURE = 1;

// Resolve omitted fields to application defaults. Saving sends the displayed
// values; explicit choices, including zero, remain unchanged.
export function agentProfileDraft(agent: Partial<Agent>): Partial<Agent> {
  return {
    ...agent,
    thinking: agent.thinking ?? "auto",
    maxOutputTokens: agent.maxOutputTokens ?? 8192,
    temperature: agent.temperature ?? DEFAULT_AGENT_TEMPERATURE,
  };
}

export function agentSavePayload(agent: Partial<Agent>): Partial<Agent> {
  const result = agentProfileDraft(agent);
  if (!["auto", "on", "off"].includes(result.thinking!)) {
    throw new Error("Choose Model default, On, or Off for thinking.");
  }
  if (
    !Number.isInteger(result.maxOutputTokens) ||
    result.maxOutputTokens! < 256 ||
    result.maxOutputTokens! > 32768
  ) {
    throw new Error(
      "Response token limit must be a whole number from 256 to 32768.",
    );
  }
  if (
    !Number.isFinite(result.temperature) ||
    result.temperature! < 0 ||
    result.temperature! > 2
  ) {
    throw new Error("Temperature must be a finite number from 0 to 2.");
  }
  return result;
}
