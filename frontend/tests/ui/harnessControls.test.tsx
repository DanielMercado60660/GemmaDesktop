import { useState } from "react";
import { it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import HarnessControls from "../../src/components/HarnessControls";
import {
  agentProfileDraft,
  agentSavePayload,
} from "../../src/lib/agentProfile";
import type { Agent } from "../../src/types";

it.each([
  { label: "unspecified", temperature: undefined, expectedTemperature: 1 },
  { label: "saved low", temperature: 0.2, expectedTemperature: 0.2 },
  { label: "saved zero", temperature: 0, expectedTemperature: 0 },
])("preserves $label sampling while independently saving experimental controls", async ({ temperature, expectedTemperature }) => {
  const save = vi.fn();
  function Form() {
    const [agent, setAgent] = useState<Partial<Agent>>(
      agentProfileDraft({ id: "a", model: "local", temperature }),
    );
    return (
      <>
        <HarnessControls agent={agent} onChange={setAgent} />
        <button onClick={() => save(agentSavePayload(agent))}>
          Save test profile
        </button>
      </>
    );
  }
  render(<Form />);
  const user = userEvent.setup();
  await user.click(screen.getByText("Experimental harness controls"));
  expect(
    screen
      .getAllByRole("checkbox")
      .every((box) => !(box as HTMLInputElement).checked),
  ).toBe(true);
  expect(
    (
      screen.getByRole("combobox", {
        name: "Initial tool set",
      }) as HTMLSelectElement
    ).value,
  ).toBe("all");
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Initial tool set" }),
    "coding",
  );
  await user.click(
    screen.getByRole("checkbox", {
      name: "Reuse unchanged reads within a run",
    }),
  );
  await user.click(
    screen.getByRole("checkbox", {
      name: "Request missing task verification before finishing",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Save test profile" }));
  expect(save).toHaveBeenCalledWith(
    expect.objectContaining({
      model: "local",
      temperature: expectedTemperature,
      verificationFeedback: true,
      harnessMechanisms: { toolSet: "coding", residentReadReuse: true },
    }),
  );
  expect(
    (
      screen.getByRole("checkbox", {
        name: "Stop repeated streamed output",
      }) as HTMLInputElement
    ).checked,
  ).toBe(false);
});
