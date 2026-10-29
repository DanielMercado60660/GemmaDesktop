import type { Agent } from "../types";

// Old profiles omit these fields. Keep their model-default behavior visible
// while sending the same explicit values displayed in the editor on save.
export function agentProfileDraft(agent: Partial<Agent>): Partial<Agent> {
  return {
    ...agent,
    thinking: agent.thinking ?? "auto",
    maxOutputTokens: agent.maxOutputTokens ?? 8192,
    temperature: agent.temperature ?? 0.2,
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
