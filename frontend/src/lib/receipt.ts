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

// Who started a declared verify_task check. The host marks the completion
// check it runs itself when the model finishes, and flags a check whose start
// the event journal no longer holds, so that one is not credited to the model.
export function verificationRunner(command: ReceiptCommand) {
  if (command.hostInitiated)
    return "Checked by Gemma Desktop after Gemma finished";
  return command.initiatorUnrecorded ? "Starter not recorded" : "Run by Gemma";
}
