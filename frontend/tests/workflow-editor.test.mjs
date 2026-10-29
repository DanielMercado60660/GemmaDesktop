import test from "node:test";
import assert from "node:assert/strict";
import {
  workflowEditorNodes,
  workflowSaveNodes,
} from "../src/lib/workflowEditor.ts";

const fanout = {
  id: "review_files",
  type: "fanout",
  label: "Review each file",
  agentId: "reviewer",
  prompt: "Review this item and report findings.",
  position: { x: 20, y: 30 },
  items: [
    "src/first.py",
    "line one\nline two",
    "",
    "Unicode café 日本語",
    'literal \\n and "quotes"',
  ],
};

test("loading, editing unrelated fields, and saving preserves backend fan-out items", () => {
  const original = structuredClone(fanout);
  const editor = workflowEditorNodes([original]);
  editor[0].data.label = "Updated label";
  editor[0].position.x = 400;
  const saved = workflowSaveNodes(editor);
  assert.deepEqual(saved, [
    {
      ...fanout,
      label: "Updated label",
      condition: undefined,
      position: { x: 400, y: 30 },
    },
  ]);
  assert.deepEqual(original, fanout, "editing must not mutate the snapshot");
  assert.deepEqual(workflowSaveNodes(workflowEditorNodes(saved)), saved);
  assert.equal(
    "itemsJSON" in saved[0],
    false,
    "UI-only source must not be persisted",
  );
});

test("fan-out JSON editing changes exact item values and accepts the backend maximum", () => {
  const editor = workflowEditorNodes([fanout]);
  const items = Array.from({ length: 16 }, (_, i) => `Task ${i}\nsecond line`);
  editor[0].data.itemsJSON = JSON.stringify(items);
  assert.deepEqual(workflowSaveNodes(editor)[0].items, items);
  assert.deepEqual(fanout.items[0], "src/first.py");
});

test("omitted and explicit empty item arrays survive round-trip; clearing uses runtime input", () => {
  const { items: _items, ...dynamic } = fanout;
  const variants = [dynamic, { ...fanout, items: [] }];
  const saved = workflowSaveNodes(workflowEditorNodes(variants));
  assert.equal("items" in saved[0], false);
  assert.deepEqual(saved[1].items, []);
  const editor = workflowEditorNodes([fanout]);
  editor[0].data.itemsJSON = "  ";
  assert.equal("items" in workflowSaveNodes(editor)[0], false);
});

test("invalid or excessive fan-out input rejects serialization rather than silently dropping items", () => {
  for (const source of [
    "[",
    "null",
    '"task"',
    "{}",
    '["task", 3]',
    '["task", null]',
    JSON.stringify(Array(17).fill("task")),
  ]) {
    const editor = workflowEditorNodes([fanout]);
    editor[0].data.itemsJSON = source;
    assert.throws(
      () => workflowSaveNodes(editor),
      /Step “Review each file”: items must/,
    );
  }
});

test("ordinary node fields retain their wire representation across editor round-trip", () => {
  const nodes = [
    {
      id: "agent_1",
      type: "agent",
      label: "Investigate",
      agentId: "coder",
      prompt: "Read files",
      position: { x: 0, y: 0 },
    },
    {
      id: "condition_1",
      type: "condition",
      label: "Check",
      condition: "contains: approved",
      position: { x: 0, y: 100 },
    },
  ];
  assert.deepEqual(
    JSON.parse(JSON.stringify(workflowSaveNodes(workflowEditorNodes(nodes)))),
    nodes,
  );
});
