import type { Run } from "../types";
import { activeStatuses } from "../types";
import { verificationRunner } from "../lib/receipt";

export default function RunVerification({ run }: { run?: Run }) {
  if (!run?.verification || activeStatuses.has(run.status)) return null;
  const checks =
    run.receipt?.commands?.filter(
      (command) => command.tool === "verify_task",
    ) || [];
  const verified = checks
    .filter((command) => command.verificationPassed)
    .at(-1);
  const latest = checks.at(-1);
  return (
    <span
      className={`tag ${run.verification === "passed" ? "accent" : ""}`}
      role="status"
      aria-label="Task verification"
      title={
        run.verification === "passed"
          ? `The declared verification command passed after the latest source change.${verified ? ` ${verificationRunner(verified)}.` : ""}${verified?.sourceDigest ? ` Verified source: ${verified.sourceDigest}.` : ""}`
          : `A completed run is not evidence that task verification passed.${latest ? ` Latest declared check: ${verificationRunner(latest)}.` : ""}`
      }
    >
      {run.verification === "passed"
        ? "Task verification passed"
        : "Task remains unverified"}
    </span>
  );
}
