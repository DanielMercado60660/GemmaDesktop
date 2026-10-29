import test from "node:test";
import assert from "node:assert/strict";
import {
  agentProfileDraft,
  agentSavePayload,
} from "../src/lib/agentProfile.ts";

test("legacy profiles retain model-default thinking and persist displayed defaults", () => {
  const legacy = {
    id: "existing-agent",
    name: "Reviewer",
    model: "local-model",
    instructions: "Review the patch",
    maxSteps: 24,
    mode: "approval",
  };
  const draft = agentProfileDraft(legacy);
  assert.equal(draft.thinking, "auto");
  assert.equal(draft.maxOutputTokens, 8192);
  assert.equal(draft.temperature, 0.2);
  assert.deepEqual(agentSavePayload(draft), {
    ...legacy,
    thinking: "auto",
    maxOutputTokens: 8192,
    temperature: 0.2,
  });
  assert.equal(Object.hasOwn(legacy, "thinking"), false);
  assert.equal(Object.hasOwn(legacy, "temperature"), false);
});

test("explicit saved choices survive reopen/save without switching thinking off", () => {
  for (const thinking of ["auto", "on", "off"]) {
    for (const maxOutputTokens of [256, 4096, 32768]) {
      const saved = {
        id: "agent",
        thinking,
        maxOutputTokens,
        temperature: 0.2,
        instructions: "Preserve me",
      };
      assert.deepEqual(agentSavePayload(agentProfileDraft(saved)), saved);
    }
  }
  assert.deepEqual(agentSavePayload({ name: "New" }), {
    name: "New",
    thinking: "auto",
    maxOutputTokens: 8192,
    temperature: 0.2,
  });
});

test("explicit zero and saved temperature choices round-trip without changing thinking", () => {
  for (const temperature of [0, 0.1, 1, 2]) {
    const saved = {
      id: "agent",
      thinking: "auto",
      maxOutputTokens: 8192,
      temperature,
    };
    assert.deepEqual(agentSavePayload(agentProfileDraft(saved)), saved);
  }
});

test("invalid temperatures produce a field-specific error instead of a silent default", () => {
  for (const temperature of [-0.1, 2.1, NaN, Infinity, -Infinity, "1", true]) {
    assert.throws(
      () => agentSavePayload({ temperature }),
      /Temperature must be a finite number from 0 to 2/,
    );
  }
});

test("invalid token limits and thinking modes cannot become saved defaults", () => {
  for (const maxOutputTokens of [0, 255, 32769, 8192.5, NaN, Infinity]) {
    assert.throws(
      () => agentSavePayload({ thinking: "auto", maxOutputTokens }),
      /whole number/,
    );
  }
  assert.throws(
    () => agentSavePayload({ thinking: "unsupported" }),
    /Choose Model default/,
  );
});
