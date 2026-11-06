import type { Pull } from "../types";
export function pullJobState(pull: Pull) {
  return pull.state || pull.status || "running";
}
export function pullJobFinished(pull: Pull) {
  return ["success", "failed", "cancelled", "interrupted"].includes(
    pullJobState(pull),
  );
}
