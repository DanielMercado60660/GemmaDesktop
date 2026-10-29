import test from "node:test";
import assert from "node:assert/strict";
import {
  capabilityProfileDraft,
  capabilityToolDescription,
  unavailableCapabilitySelections,
} from "../src/lib/capabilityProfile.ts";

test("command help follows the selected executor without changing unrelated tool text", () => {
  const command = {
    name: "run_command",
    description: "Restricted macOS single process; subprocesses unsupported.",
  };
  assert.equal(
    capabilityToolDescription(command, "restricted"),
    command.description,
  );
  const docker = capabilityToolDescription(command, "docker");
  assert.match(docker, /Linux Docker image/);
  assert.match(docker, /may use subprocesses/);
  assert.doesNotMatch(docker, /subprocesses unsupported|macOS/);
  assert.equal(
    capabilityToolDescription(
      { name: "read_file", description: "Read source." },
      "docker",
    ),
    "Read source.",
  );
});

test("omitted and null lists are safe editor arrays without mutating stored identity", () => {
  for (const source of [
    { id: "old", revision: 3 },
    {
      id: "old",
      revision: 3,
      tools: null,
      skillIds: null,
      mcp: null,
      delegateAgentIds: null,
    },
  ]) {
    const before = structuredClone(source);
    const draft = capabilityProfileDraft(source);
    for (const key of [
      "tools",
      "skillIds",
      "mcp",
      "delegateAgentIds",
      "privatePaths",
    ])
      assert.deepEqual(draft[key], []);
    assert.equal(draft.revision, 3);
    assert.deepEqual(source, before);
  }
});

test("unavailable selections remain visible and removable without granting replacements", () => {
  const source = {
    id: "existing",
    revision: 4,
    tools: ["read_file", "obsolete"],
    skillIds: ["live", "archived"],
    delegateAgentIds: ["reader", "deleted"],
    mcp: [
      { serverId: "server", tools: ["known", "removed"] },
      { serverId: "deleted-server", tools: ["query"] },
    ],
  };
  const draft = capabilityProfileDraft(source);
  const unavailable = unavailableCapabilitySelections(draft, {
    tools: [{ name: "read_file" }],
    skills: [{ id: "live" }],
    agents: [{ id: "reader" }],
    servers: [{ id: "server", tools: [{ name: "known" }] }],
  });
  assert.deepEqual(unavailable, {
    tools: ["obsolete"],
    skills: ["archived"],
    delegates: ["deleted"],
    mcp: [
      { serverId: "server", tool: "removed" },
      { serverId: "deleted-server", tool: "query" },
    ],
  });
  // The same independent edit operations used by the controls preserve all
  // existing known grants and never mutate the fetched/saved revision.
  draft.skillIds = draft.skillIds.filter((id) => id !== "archived");
  draft.delegateAgentIds = draft.delegateAgentIds.filter(
    (id) => id !== "deleted",
  );
  draft.mcp[0].tools = draft.mcp[0].tools.filter((n) => n !== "removed");
  assert.deepEqual(draft.skillIds, ["live"]);
  assert.deepEqual(draft.delegateAgentIds, ["reader"]);
  assert.deepEqual(draft.mcp[0].tools, ["known"]);
  assert.deepEqual(source.skillIds, ["live", "archived"]);
  assert.deepEqual(source.mcp[0].tools, ["known", "removed"]);
});
