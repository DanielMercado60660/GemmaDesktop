import test from "node:test";
import assert from "node:assert/strict";
import {
  createArtifactLoader,
  artifactContent,
} from "../src/lib/artifactPreview.ts";

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

test("artifact contents load only on selection; switching cannot display a late previous response", async () => {
  const requests = new Map();
  let state;
  const loader = createArtifactLoader(
    (id) => {
      const gate = deferred();
      requests.set(id, gate);
      return gate.promise;
    },
    (next) => {
      state = next;
    },
  );
  await loader.select("");
  assert.equal(requests.size, 0);
  const first = loader.select("first");
  assert.deepEqual(state, { id: "first", status: "loading" });
  const second = loader.select("second");
  requests.get("second").resolve({ id: "second", before: "old", after: "new" });
  await second;
  requests
    .get("first")
    .resolve({ id: "first", before: "wrong", after: "wrong" });
  await first;
  assert.equal(state.id, "second");
  assert.equal(state.artifact.after, "new");
});

test("closing or unmounting invalidates in-flight artifact responses", async () => {
  const gate = deferred();
  let state;
  const loader = createArtifactLoader(
    () => gate.promise,
    (next) => {
      state = next;
    },
  );
  const loading = loader.select("first");
  loader.invalidate();
  await loader.select("");
  gate.resolve({ id: "first", after: "late" });
  await loading;
  assert.deepEqual(state, { id: "", status: "idle" });
});

test("fetch errors are explicit and retry can load a real empty file snapshot", async () => {
  let attempts = 0,
    state;
  const loader = createArtifactLoader(
    async (id) => {
      if (++attempts === 1) throw new Error("Artifact not found");
      return { id, before: "deleted text", after: "" };
    },
    (next) => {
      state = next;
    },
  );
  await loader.select("a");
  assert.deepEqual(state, {
    id: "a",
    status: "error",
    error: "Artifact not found",
  });
  assert.deepEqual(artifactContent(undefined), { kind: "none" });
  await loader.select("a");
  assert.deepEqual(artifactContent(state.artifact), {
    kind: "diff",
    before: "deleted text",
    after: "",
  });
});

test("metadata and missing before images never fabricate an empty diff", async () => {
  assert.deepEqual(artifactContent({ id: "metadata" }), { kind: "none" });
  assert.deepEqual(artifactContent({ id: "text", after: "snapshot" }), {
    kind: "text",
    after: "snapshot",
  });
  let state;
  const loader = createArtifactLoader(
    async () => ({ id: "wrong", after: "other artifact" }),
    (next) => {
      state = next;
    },
  );
  await loader.select("requested");
  assert.equal(state.status, "error");
  assert.match(state.error, /different artifact/);
});
