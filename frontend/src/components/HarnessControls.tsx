import type { Agent } from "../types";
import { Field } from "./ui";
const mechanisms = [
  ["repeatedFailureFeedback", "Explain repeated tool failures"],
  ["streamRepetitionGuard", "Stop repeated streamed output"],
  ["thinkingCycleGuard", "Interrupt repeated thinking and retry"],
  ["explorationReminder", "Remind before excessive exploration"],
  ["residentReadReuse", "Reuse unchanged reads within a run"],
  ["duplicateResultProjection", "Shorten duplicate tool results"],
  ["deferredToolDiscovery", "Discover optional tools as needed"],
] as const;
export default function HarnessControls({
  agent,
  onChange,
}: {
  agent: Partial<Agent>;
  onChange: (agent: Partial<Agent>) => void;
}) {
  const selected = agent.harnessMechanisms || {};
  return (
    <details className="panel form-stack">
      <summary>Experimental harness controls</summary>
      <p className="small muted">
        Enable mechanisms independently for measured comparisons. These options
        stay off by default; the capability profile continues to enforce
        permissions.
      </p>
      <Field label="Initial tool set">
        <select
          value={selected.toolSet || "all"}
          onChange={(event) =>
            onChange({
              ...agent,
              harnessMechanisms: {
                ...selected,
                toolSet: event.target.value as NonNullable<
                  Agent["harnessMechanisms"]
                >["toolSet"],
              },
            })
          }
        >
          <option value="all">All permitted tools</option>
          <option value="inspect">Project inspection</option>
          <option value="coding">Coding</option>
          <option value="research">Research</option>
        </select>
      </Field>
      {mechanisms.map(([key, label]) => (
        <label className="checkbox-label" key={key}>
          <input
            type="checkbox"
            checked={!!selected[key]}
            onChange={(event) =>
              onChange({
                ...agent,
                harnessMechanisms: { ...selected, [key]: event.target.checked },
              })
            }
          />
          <span>{label}</span>
        </label>
      ))}
      <label className="checkbox-label">
        <input
          type="checkbox"
          checked={!!agent.verificationFeedback}
          onChange={(event) =>
            onChange({ ...agent, verificationFeedback: event.target.checked })
          }
        />
        <span>Request missing task verification before finishing</span>
      </label>
    </details>
  );
}
export function HarnessControlSummary({ agent }: { agent?: Partial<Agent> }) {
  const enabled: string[] = mechanisms
    .filter(([key]) => agent?.harnessMechanisms?.[key])
    .map(([, label]) => label);
  if (agent?.verificationFeedback)
    enabled.push("Request missing task verification before finishing");
  return (
    <p className="small muted">
      Initial tools: {agent?.harnessMechanisms?.toolSet || "all"}. Experimental
      controls: {enabled.length ? enabled.join("; ") : "all off"}.
    </p>
  );
}
