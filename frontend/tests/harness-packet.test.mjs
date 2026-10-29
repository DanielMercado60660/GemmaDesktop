import test from "node:test";
import assert from "node:assert/strict";
import { parsePacketResponse } from "../src/lib/harnessPacket.ts";

const packet = {id: "packet-a", requestHash: "immutable-input"};
const response = {...packet, authoredBy: {kind: "codex", identity: "independent-reviewer"}, notes: "Evidence reviewed"};
test("packet responses retain exact identity and content", () => assert.deepEqual(parsePacketResponse(JSON.stringify(response), packet), response));
test("stale or cross-experiment responses are rejected before submission", () => {
  for (const changed of [{id: "packet-b"}, {requestHash: "older-input"}]) assert.throws(() => parsePacketResponse(JSON.stringify({...response, ...changed}), packet), /different packet/);
});
test("solver-authored grading and missing reviewer identity are rejected", () => {
  for (const authoredBy of [{kind: "gemma", identity: "solver"}, {kind: "codex", identity: " "}, null]) assert.throws(() => parsePacketResponse(JSON.stringify({...response, authoredBy}), packet), /Codex/);
});
