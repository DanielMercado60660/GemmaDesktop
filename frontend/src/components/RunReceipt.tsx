import type { Run } from "../types";
import { activeStatuses } from "../types";
import { commandOutcome } from "../lib/receipt";

export default function RunReceipt({ run }: { run?: Run }) {
  const receipt = run?.receipt;
  if (!receipt || !run || activeStatuses.has(run.status)) return null;
  const warning = receipt.failedTools > 0 || receipt.unresolvedTools > 0;
  return (
    <details className={`run-receipt ${warning ? "receipt-warning" : ""}`}>
      <summary>
        <strong>Observed actions</strong>
        <span>{receipt.toolCalls} tool calls</span>
        {warning && (
          <span className="receipt-warning-count">
            {receipt.failedTools} failed · {receipt.unresolvedTools} unresolved
          </span>
        )}
      </summary>
      <div className="receipt-content">
        <p className="receipt-source">
          Recorded by {receipt.source || "the tool journal"}.
        </p>
        {!!receipt.commands?.length && (
          <div className="receipt-section">
            <h4>Commands</h4>
            {receipt.commands.map((command, index) => (
              <div className="receipt-entry" key={`${command.callId}-${index}`}>
                <code>
                  {[
                    command.executable,
                    ...(command.args || []).map((arg) => JSON.stringify(arg)),
                  ].join(" ")}
                </code>
                <span
                  className={
                    command.status === "failed" ||
                    command.cancelled ||
                    command.status !== "completed"
                      ? "receipt-failed"
                      : ""
                  }
                >
                  {commandOutcome(command)}
                </span>
                {command.workingDirectory && (
                  <small>Directory: {command.workingDirectory}</small>
                )}
                {command.packageCount !== undefined && (
                  <small>
                    {command.packageCount} locked packages ·{" "}
                    {(command.downloadedBytes || 0).toLocaleString()} downloaded
                    bytes
                  </small>
                )}
                {command.publication && (
                  <small>
                    Source changes: {command.publication.replaceAll("_", " ")}
                  </small>
                )}
                {command.workspaceChanged && (
                  <p className="receipt-failed">
                    The project changed during this command. Its result
                    describes the captured snapshot.
                  </p>
                )}
                {command.image && (
                  <details>
                    <summary>Execution provenance</summary>
                    <small className="mono break-all">
                      Image: {command.image}
                    </small>
                    <small className="mono break-all">
                      Input: {command.snapshotDigest}
                      {command.dependencyDigest && (
                        <> · Dependencies: {command.dependencyDigest}</>
                      )}
                    </small>
                    <small className="mono break-all">
                      Command: {command.commandId}
                    </small>
                  </details>
                )}
                {!!command.changes?.length && (
                  <details>
                    <summary>{command.changes.length} source changes</summary>
                    {command.changes.map((change) => (
                      <div key={change.path}>
                        <code>{change.path}</code>
                      </div>
                    ))}
                  </details>
                )}
                {command.error && (
                  <p className="receipt-failed">{command.error}</p>
                )}
              </div>
            ))}
          </div>
        )}
        {!!receipt.changes?.length && (
          <div className="receipt-section">
            <h4>File operations</h4>
            {receipt.changes.map((change, index) => (
              <div className="receipt-entry" key={`${change.callId}-${index}`}>
                <code>
                  {change.path}
                  {change.destination ? ` → ${change.destination}` : ""}
                </code>
                <span>{change.tool.replaceAll("_", " ")}</span>
              </div>
            ))}
          </div>
        )}
        {!receipt.commands?.length && !receipt.changes?.length && (
          <p>No command or file mutation was recorded.</p>
        )}
        {receipt.entriesLimited && (
          <p>
            Some entries are omitted here. Inspect activity for the full
            journal.
          </p>
        )}
        <p className="receipt-note">
          {receipt.note ||
            "Observed actions only. An exit code of zero does not independently verify task correctness."}
        </p>
      </div>
    </details>
  );
}
