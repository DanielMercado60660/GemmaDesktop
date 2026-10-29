import type { ReceiptCommand } from "../types";

export function commandOutcome(command: ReceiptCommand) {
  const state = command.cancelled
    ? "Cancelled"
    : command.status === "completed"
      ? "Completed"
      : command.status === "failed"
        ? "Failed"
        : "Outcome unresolved";
  const exit = Number.isInteger(command.exitCode)
    ? `exit ${command.exitCode}`
    : "exit not recorded";
  return [
    state,
    exit,
    command.signal ? `signal ${command.signal}` : "",
    command.truncated ? "output truncated" : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
