import { it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import RunVerification from "../../src/components/RunVerification";
import RunReceipt from "../../src/components/RunReceipt";
import type { Run } from "../../src/types";

it("ties declared verification to recorded source digests without grading correctness", () => {
  const before = "a".repeat(64),
    after = "b".repeat(64),
    contract = "c".repeat(64);
  const run: Run = {
    id: "r",
    sessionId: "s",
    workspaceId: "w",
    status: "completed",
    model: "test",
    prompt: "repair",
    startedAt: "",
    verification: "passed",
    receipt: {
      source: "tool journal",
      toolCalls: 1,
      failedTools: 0,
      unresolvedTools: 0,
      commands: [
        {
          callId: "verify",
          tool: "verify_task",
          status: "completed",
          executable: "python3",
          args: ["-m", "pytest"],
          verificationPassed: true,
          sourceDigest: before,
          sourceDigestAfter: before,
          contractDigest: contract,
        },
      ],
    },
  };
  const view = render(
    <>
      <RunVerification run={run} />
      <RunReceipt run={run} />
    </>,
  );
  expect(screen.getByRole("status").getAttribute("title")).toContain(
    `Verified source: ${before}`,
  );
  expect(screen.getByText("Declared task verification")).toBeTruthy();
  expect(screen.getAllByTitle(before)).toHaveLength(2);
  expect(screen.getByTitle(contract).textContent).toContain(
    contract.slice(0, 12),
  );
  expect(
    screen.getByText(/does not independently verify task correctness/),
  ).toBeTruthy();
  const failed: Run = {
    ...run,
    verification: "unverified",
    receipt: {
      ...run.receipt!,
      commands: [
        {
          ...run.receipt!.commands![0],
          verificationPassed: false,
          sourceDigestAfter: after,
        },
      ],
    },
  };
  view.rerender(
    <>
      <RunVerification run={failed} />
      <RunReceipt run={failed} />
    </>,
  );
  expect(screen.getByRole("status").getAttribute("title")).not.toContain(
    "Verified source:",
  );
  expect(screen.getByText("Verification did not pass")).toBeTruthy();
  expect(screen.getByTitle(after).textContent).toContain("Source after check");
});

it("says who ran each declared check, keeping the badge's accessible name", () => {
  const check = {
    tool: "verify_task",
    status: "completed",
    executable: "go",
    args: ["test", "./..."],
    verificationPassed: false,
  };
  const run: Run = {
    id: "r",
    sessionId: "s",
    workspaceId: "w",
    status: "completed",
    model: "test",
    prompt: "repair",
    startedAt: "",
    verification: "passed",
    receipt: {
      source: "tool journal",
      toolCalls: 2,
      failedTools: 0,
      unresolvedTools: 0,
      commands: [
        { ...check, callId: "tool_model" },
        {
          ...check,
          callId: "tool_host",
          hostInitiated: true,
          verificationPassed: true,
          sourceDigest: "a".repeat(64),
        },
      ],
    },
  };
  const view = render(
    <>
      <RunVerification run={run} />
      <RunReceipt run={run} />
    </>,
  );
  const badge = screen.getByRole("status", { name: "Task verification" });
  expect(badge.textContent).toBe("Task verification passed");
  expect(badge.getAttribute("title")).toContain(
    "Checked by Gemma Desktop after Gemma finished.",
  );
  const rows = view.container.querySelectorAll(".receipt-entry");
  expect(rows[0].textContent).toContain("Run by Gemma");
  expect(rows[0].textContent).not.toContain("Checked by Gemma Desktop");
  expect(rows[1].textContent).toContain(
    "Checked by Gemma Desktop after Gemma finished",
  );
  const modelOnly: Run = {
    ...run,
    verification: "unverified",
    receipt: { ...run.receipt!, commands: [run.receipt!.commands![0]] },
  };
  view.rerender(
    <>
      <RunVerification run={modelOnly} />
      <RunReceipt run={modelOnly} />
    </>,
  );
  expect(
    screen
      .getByRole("status", { name: "Task verification" })
      .getAttribute("title"),
  ).toContain("Latest declared check: Run by Gemma.");
  expect(screen.queryByText(/Checked by Gemma Desktop/)).toBeNull();
  const unrecorded: Run = {
    ...modelOnly,
    receipt: {
      ...run.receipt!,
      commands: [{ ...run.receipt!.commands![0], initiatorUnrecorded: true }],
    },
  };
  view.rerender(
    <>
      <RunVerification run={unrecorded} />
      <RunReceipt run={unrecorded} />
    </>,
  );
  expect(
    screen
      .getByRole("status", { name: "Task verification" })
      .getAttribute("title"),
  ).toContain("Latest declared check: Starter not recorded.");
  expect(screen.queryByText("Run by Gemma")).toBeNull();
  expect(screen.getByText("Starter not recorded")).toBeTruthy();
});
