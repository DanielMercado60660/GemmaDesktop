import { useEffect, useState } from "react";
import { Shield, Plus, Pencil } from "lucide-react";
import { api } from "./bridge";
import type { PageProps } from "./Management";
import { Button, Field, Modal } from "./components/ui";
import {
  capabilityProfileDraft,
  capabilityToolDescription,
  unavailableCapabilitySelections,
  type CapabilityProfile,
} from "./lib/capabilityProfile";
export type { CapabilityProfile } from "./lib/capabilityProfile";
interface Catalogue {
  items: CapabilityProfile[];
  tools?: Array<{ name: string; description?: string }>;
}
const blank = (): CapabilityProfile => ({
  version: 1,
  id: "",
  name: "",
  tools: [],
  skillIds: [],
  mcp: [],
  delegateAgentIds: [],
  executionProfile: "restricted",
  projectGuidance: true,
  revision: 0,
  privatePaths: [],
});
function toggle(values: string[], value: string, on: boolean) {
  return on
    ? [...new Set([...values, value])]
    : values.filter((v) => v !== value);
}

export function CapabilitySelector({
  value,
  onChange,
  refreshKey = 0,
}: {
  value?: string;
  onChange: (id: string) => void;
  refreshKey?: number;
}) {
  const [profiles, setProfiles] = useState<CapabilityProfile[]>([]),
    [error, setError] = useState("");
  useEffect(() => {
    let current = true;
    api<CapabilityProfile[]>("capability.list")
      .then((r) => {
        if (current) {
          setProfiles(r.map(capabilityProfileDraft));
          setError("");
        }
      })
      .catch((e) => {
        if (current) setError(String(e));
      });
    return () => {
      current = false;
    };
  }, [refreshKey]);
  return (
    <Field
      label="Capability profile"
      hint="Select tools, skill versions, connections, delegates, and the coding executor."
    >
      <select
        value={value || "legacy"}
        onChange={(e) => onChange(e.target.value)}
      >
        {profiles.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
        {!profiles.some((p) => p.id === (value || "legacy")) && (
          <option value={value || "legacy"}>
            {value || "Existing workspace capabilities"}
          </option>
        )}
      </select>
      {error && <small className="error-text">{error}</small>}
    </Field>
  );
}

export default function Capabilities({
  state,
  action,
}: Pick<PageProps, "state" | "action">) {
  const [catalogue, setCatalogue] = useState<Catalogue>({ items: [] }),
    [editing, setEditing] = useState<CapabilityProfile>(),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [version, setVersion] = useState(0);
  const [privatePathsText, setPrivatePathsText] = useState("");
  useEffect(
    () => setPrivatePathsText((editing?.privatePaths || []).join("\n")),
    [editing?.id],
  );
  useEffect(() => {
    let current = true;
    Promise.all([
      api<CapabilityProfile[]>("capability.list"),
      api<NonNullable<Catalogue["tools"]>>("capability.catalogue"),
    ])
      .then(([items, tools]) => {
        if (current)
          setCatalogue({ items: items.map(capabilityProfileDraft), tools });
      })
      .catch((e) => {
        if (current) setError(String(e));
      });
    return () => {
      current = false;
    };
  }, [version]);
  const unavailable = editing
    ? unavailableCapabilitySelections(editing, {
        tools: catalogue.tools || [],
        skills: state.skills,
        agents: state.agents,
        servers: state.tools,
      })
    : undefined;
  return (
    <section className="phase-packets">
      <div className="section-header">
        <div>
          <span className="eyebrow">Explicit boundaries</span>
          <h2>Capability profiles</h2>
          <p>
            Profiles are frozen for each run. Instructions and imported skills
            cannot expand their permissions.
          </p>
        </div>
        <Button onClick={() => setEditing(blank())}>
          <Plus size={15} />
          New profile
        </Button>
      </div>
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
      <div className="card-grid">
        {catalogue.items.map((p) => (
          <article className="entity-card" key={p.id}>
            <div className="entity-top">
              <Shield size={21} />
              <Button
                size="icon"
                variant="ghost"
                aria-label={`Edit ${p.name}`}
                onClick={() => {
                  setError("");
                  setEditing(capabilityProfileDraft(p));
                }}
              >
                <Pencil size={15} />
              </Button>
            </div>
            <h3>{p.name}</h3>
            <p>
              {p.tools.length} tools · {p.skillIds.length} skills ·{" "}
              {p.mcp.length} connections
            </p>
            <div className="card-meta">
              <span>
                {p.executionProfile === "docker"
                  ? "Docker coding"
                  : "Restricted commands"}
              </span>
              <span>{p.delegateAgentIds.length} delegates</span>
            </div>
            {p.revision !== undefined && <small>Revision {p.revision}</small>}
          </article>
        ))}
      </div>
      <Modal
        open={!!editing}
        onClose={() => setEditing(undefined)}
        title={
          editing?.id ? "Edit capability profile" : "Create capability profile"
        }
        description="Saving affects future runs. Existing runs retain their recorded capabilities."
        wide
      >
        {editing && (
          <form
            className="form-stack"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              try {
                const result = await action("capability.save", {
                  ...editing,
                  privatePaths: privatePathsText
                    .split("\n")
                    .map((path) => path.trim())
                    .filter(Boolean),
                });
                if (result !== undefined) {
                  setEditing(undefined);
                  setVersion((v) => v + 1);
                }
              } finally {
                setBusy(false);
              }
            }}
          >
            <div className="form-grid">
              <Field label="Profile name">
                <input
                  required
                  value={editing.name}
                  onChange={(e) =>
                    setEditing({ ...editing, name: e.target.value })
                  }
                />
              </Field>
              <Field label="Command executor">
                <select
                  value={editing.executionProfile}
                  onChange={(e) =>
                    setEditing({
                      ...editing,
                      executionProfile: e.target
                        .value as CapabilityProfile["executionProfile"],
                    })
                  }
                >
                  <option value="restricted">Restricted macOS commands</option>
                  <option value="docker">
                    Docker · Linux builds and tests
                  </option>
                </select>
              </Field>
            </div>
            {editing.executionProfile === "docker" && (
              <Field
                label="Private build-output directories"
                hint="Optional project-relative directories, one per line. Their contents stay in the command cache and are not published as source. Dependency and tool caches are private automatically."
              >
                <textarea
                  rows={3}
                  value={privatePathsText}
                  onChange={(e) => setPrivatePathsText(e.target.value)}
                  placeholder={"dist\ncoverage"}
                />
              </Field>
            )}
            <fieldset className="capability-options">
              <legend>Built-in tools</legend>
              {(
                catalogue.tools ||
                editing.tools.map((name) => ({ name, description: "" }))
              ).map((tool) => (
                <label className="checkbox-label" key={tool.name}>
                  <input
                    type="checkbox"
                    checked={editing.tools.includes(tool.name)}
                    onChange={(e) =>
                      setEditing({
                        ...editing,
                        tools: toggle(
                          editing.tools,
                          tool.name,
                          e.target.checked,
                        ),
                      })
                    }
                  />
                  <span>
                    {tool.name.replaceAll("_", " ")}
                    <small className="muted">
                      {capabilityToolDescription(
                        tool,
                        editing.executionProfile,
                      )}
                    </small>
                  </span>
                </label>
              ))}
              {unavailable?.tools.map((name) => (
                <label className="checkbox-label" key={`unavailable-${name}`}>
                  <input
                    type="checkbox"
                    checked
                    onChange={() =>
                      setEditing({
                        ...editing,
                        tools: toggle(editing.tools, name, false),
                      })
                    }
                  />
                  <span>
                    {name}
                    <small className="muted">
                      Unavailable tool · uncheck to remove from future runs
                    </small>
                  </span>
                </label>
              ))}
            </fieldset>
            <fieldset className="capability-options">
              <legend>Immutable skill packages</legend>
              {state.skills.map((skill) => (
                <label className="checkbox-label" key={skill.id}>
                  <input
                    type="checkbox"
                    checked={editing.skillIds.includes(skill.id)}
                    onChange={(e) =>
                      setEditing({
                        ...editing,
                        skillIds: toggle(
                          editing.skillIds,
                          skill.id,
                          e.target.checked,
                        ),
                      })
                    }
                  />
                  <span>
                    {skill.name}
                    <small className="mono muted">
                      {skill.digest?.slice(0, 16) || skill.id}
                    </small>
                  </span>
                </label>
              ))}
              {unavailable?.skills.map((id) => (
                <label className="checkbox-label" key={`unavailable-${id}`}>
                  <input
                    type="checkbox"
                    checked
                    onChange={() =>
                      setEditing({
                        ...editing,
                        skillIds: toggle(editing.skillIds, id, false),
                      })
                    }
                  />
                  <span className="break-all">
                    {id}
                    <small className="muted">
                      Archived or missing skill version · uncheck to remove from
                      future runs
                    </small>
                  </span>
                </label>
              ))}
              {!state.skills.length && (
                <p className="muted">
                  Import a skill package to select it here.
                </p>
              )}
            </fieldset>
            <fieldset className="capability-options">
              <legend>MCP tool selection</legend>
              {state.tools.map((server) => (
                <div key={server.id}>
                  <strong>{server.name}</strong>
                  {server.tools?.map((tool) => (
                    <label className="checkbox-label" key={tool.name}>
                      <input
                        type="checkbox"
                        checked={editing.mcp.some(
                          (s) =>
                            s.serverId === server.id &&
                            s.tools.includes(tool.name),
                        )}
                        onChange={(e) => {
                          const existing = editing.mcp.find(
                            (s) => s.serverId === server.id,
                          );
                          const names = toggle(
                            existing?.tools || [],
                            tool.name,
                            e.target.checked,
                          );
                          setEditing({
                            ...editing,
                            mcp: [
                              ...editing.mcp.filter(
                                (s) => s.serverId !== server.id,
                              ),
                              ...(names.length
                                ? [{ serverId: server.id, tools: names }]
                                : []),
                            ],
                          });
                        }}
                      />
                      {tool.name}
                    </label>
                  ))}
                  {!server.tools?.length && (
                    <p className="small muted">
                      Connect this server to discover its exact tool names.
                    </p>
                  )}
                </div>
              ))}
              {unavailable?.mcp.map(({ serverId, tool }) => (
                <label className="checkbox-label" key={`${serverId}:${tool}`}>
                  <input
                    type="checkbox"
                    checked
                    onChange={() =>
                      setEditing({
                        ...editing,
                        mcp: editing.mcp
                          .map((s) =>
                            s.serverId === serverId
                              ? {
                                  ...s,
                                  tools: s.tools.filter((n) => n !== tool),
                                }
                              : s,
                          )
                          .filter((s) => s.tools.length > 0),
                      })
                    }
                  />
                  <span className="break-all">
                    {serverId} / {tool}
                    <small className="muted">
                      Server or tool is not currently listed · uncheck to remove
                      the selection
                    </small>
                  </span>
                </label>
              ))}
            </fieldset>
            <fieldset className="capability-options">
              <legend>Read-only specialist agents</legend>
              {state.agents.map((a) => (
                <label className="checkbox-label" key={a.id}>
                  <input
                    type="checkbox"
                    checked={editing.delegateAgentIds.includes(a.id)}
                    onChange={(e) =>
                      setEditing({
                        ...editing,
                        delegateAgentIds: toggle(
                          editing.delegateAgentIds,
                          a.id,
                          e.target.checked,
                        ),
                      })
                    }
                  />
                  {a.name}
                </label>
              ))}
              {unavailable?.delegates.map((id) => (
                <label className="checkbox-label" key={`unavailable-${id}`}>
                  <input
                    type="checkbox"
                    checked
                    onChange={() =>
                      setEditing({
                        ...editing,
                        delegateAgentIds: toggle(
                          editing.delegateAgentIds,
                          id,
                          false,
                        ),
                      })
                    }
                  />
                  <span className="break-all">
                    {id}
                    <small className="muted">
                      Missing specialist agent · uncheck to remove from future
                      runs
                    </small>
                  </span>
                </label>
              ))}
              <p className="small muted">
                The delegate tool must also be enabled. Specialists inherit the
                parent’s authority and shared budgets.
              </p>
            </fieldset>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={editing.projectGuidance}
                onChange={(e) =>
                  setEditing({ ...editing, projectGuidance: e.target.checked })
                }
              />
              Include bounded project guidance in run context
            </label>
            <div className="modal-actions">
              <Button type="button" onClick={() => setEditing(undefined)}>
                Cancel
              </Button>
              <Button type="submit" variant="default" loading={busy}>
                Save profile
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </section>
  );
}
