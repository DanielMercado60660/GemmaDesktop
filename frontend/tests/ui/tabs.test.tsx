import { it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TabList from "../../src/components/TabList";

it("supports roving keyboard focus and explicit activation without activating disabled tabs", async () => {
  const change = vi.fn();
  render(
    <TabList
      label="Views"
      value="chat"
      onChange={change}
      panelId="panel"
      items={[
        { id: "chat", label: "Chat" },
        { id: "file", label: "File", disabled: true },
        { id: "changes", label: "Changes" },
      ]}
    />,
  );
  const user = userEvent.setup();
  await user.tab();
  expect(document.activeElement).toBe(
    screen.getByRole("tab", { name: "Chat" }),
  );
  await user.keyboard("{ArrowRight}");
  expect(document.activeElement).toBe(
    screen.getByRole("tab", { name: "Changes" }),
  );
  expect(change).not.toHaveBeenCalled();
  await user.keyboard("{Enter}");
  expect(change).toHaveBeenCalledWith("changes");
  await user.keyboard("{Home}");
  expect(document.activeElement).toBe(
    screen.getByRole("tab", { name: "Chat" }),
  );
  await user.keyboard("{End}");
  expect(document.activeElement).toBe(
    screen.getByRole("tab", { name: "Changes" }),
  );
  expect(
    screen.getByRole("tab", { name: "Chat" }).getAttribute("aria-controls"),
  ).toBe("panel");
});
