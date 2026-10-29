export interface CapabilityProfile {
  version: number;
  id: string;
  name: string;
  tools: string[];
  skillIds: string[];
  mcp: Array<{ serverId: string; tools: string[] }>;
  delegateAgentIds: string[];
  executionProfile: "restricted" | "docker";
  projectGuidance: boolean;
  privatePaths?: string[];
  revision?: number;
  digest?: string;
}

// Older/API-created records can omit empty arrays. Normalize only editor
// copies: do not change the stored revision or infer new permission grants.
export function capabilityProfileDraft(
  profile: CapabilityProfile,
): CapabilityProfile {
  return {
    ...profile,
    tools: [...(Array.isArray(profile.tools) ? profile.tools : [])],
    skillIds: [...(Array.isArray(profile.skillIds) ? profile.skillIds : [])],
    mcp: (Array.isArray(profile.mcp) ? profile.mcp : []).map((s) => ({
      ...s,
      tools: [...(Array.isArray(s.tools) ? s.tools : [])],
    })),
    delegateAgentIds: [
      ...(Array.isArray(profile.delegateAgentIds)
        ? profile.delegateAgentIds
        : []),
    ],
    privatePaths: [
      ...(Array.isArray(profile.privatePaths) ? profile.privatePaths : []),
    ],
  };
}

export function capabilityToolDescription(
  tool: { name: string; description?: string },
  executionProfile: CapabilityProfile["executionProfile"],
): string {
  if (tool.name === "run_command" && executionProfile === "docker") {
    return "Run an approved program in the pinned Linux Docker image. Builds and tests may use subprocesses. Commands have no network access. Permitted source changes are checked before publication; readOnly:true protects source while private dependencies and caches remain writable.";
  }
  return tool.description || "";
}

export function unavailableCapabilitySelections(
  profile: CapabilityProfile,
  available: {
    tools: Array<{ name: string }>;
    skills: Array<{ id: string }>;
    agents: Array<{ id: string }>;
    servers: Array<{ id: string; tools?: Array<{ name: string }> }>;
  },
) {
  return {
    tools: profile.tools.filter(
      (name) => !available.tools.some((t) => t.name === name),
    ),
    skills: profile.skillIds.filter(
      (id) => !available.skills.some((s) => s.id === id),
    ),
    delegates: profile.delegateAgentIds.filter(
      (id) => !available.agents.some((a) => a.id === id),
    ),
    mcp: profile.mcp.flatMap((s) => {
      const server = available.servers.find((v) => v.id === s.serverId);
      return s.tools
        .filter((name) => !server?.tools?.some((t) => t.name === name))
        .map((tool) => ({ serverId: s.serverId, tool }));
    }),
  };
}
