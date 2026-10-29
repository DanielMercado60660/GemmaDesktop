import test from "node:test";
import assert from "node:assert/strict";
import { decodeApiResponse } from "../src/lib/apiResponse.ts";
test("successful domain failure metadata remains inspectable", () => {
  assert.deepEqual(
    decodeApiResponse('{"status":"failed","error":"test failure"}', 200, true),
    { status: "failed", error: "test failure" },
  );
});
test("real HTTP failures preserve useful errors and distinguish origin failures", () => {
  assert.throws(
    () => decodeApiResponse('{"error":"Workspace changed"}', 400, false),
    /Workspace changed/,
  );
  assert.throws(
    () => decodeApiResponse("origin rejected", 403, false),
    /403.*origin rejected/,
  );
  assert.throws(
    () => decodeApiResponse("<html>proxy error</html>", 502, false),
    /502/,
  );
  assert.throws(
    () => decodeApiResponse("invalid", 200, true),
    /unreadable response/,
  );
});
