import test from "node:test";
import assert from "node:assert/strict";
import { createComposerStore, submitDraft } from "../src/lib/composer.ts";

const owner = (workspaceId, sessionId = "") => ({ workspaceId, sessionId });
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

test("drafts and attachments belong to each workspace, conversation, and new-chat composer", () => {
  const store = createComposerStore(owner("a"));
  store.update({ text: "new A", attachments: ["a.txt"] });
  store.select(owner("a", "chat"));
  store.update({ text: "existing A", attachments: ["src/a.ts"] });
  store.select(owner("b"));
  assert.equal(store.read().text, "");
  assert.deepEqual(store.read().attachments, []);
  store.update({ text: "new B" });
  store.select(owner("a"));
  assert.equal(store.read().text, "new A");
  assert.deepEqual(store.read().attachments, ["a.txt"]);
  store.select(owner("a", "chat"));
  assert.equal(store.read().text, "existing A");
  assert.deepEqual(store.read().attachments, ["src/a.ts"]);
});

test("late run admission remains bound to its captured session and cannot clear another draft", async () => {
  const store = createComposerStore(owner("a", "one"));
  store.update({ text: "  original task  ", attachments: ["source.txt"] });
  const submission = store.begin({ agentId: "reviewer", model: "local" });
  const receipt = deferred();
  let sent;
  const request = submitDraft(store, submission, {
    createSession: async () => {
      throw new Error("existing chat must not create");
    },
    startRun: (input) => {
      sent = input;
      return receipt.promise;
    },
  });
  store.select(owner("b", "two"));
  store.update({ text: "different task", attachments: ["private.txt"] });
  receipt.resolve({ id: "run" });
  await request;
  assert.deepEqual(sent, {
    sessionId: "one",
    prompt: "original task",
    attachments: ["source.txt"],
    agentId: "reviewer",
    model: "local",
  });
  assert.equal(store.selection().sessionId, "two");
  assert.equal(store.read().text, "different task");
  assert.deepEqual(store.read().attachments, ["private.txt"]);
  assert.equal(store.read(owner("a", "one")).text, "");
  store.finish(submission);
});

test("navigation away and back invalidates automatic new-chat selection", async () => {
  const store = createComposerStore(owner("a"));
  store.update({ text: "first task" });
  const submission = store.begin();
  const created = deferred();
  let sent;
  const request = submitDraft(store, submission, {
    createSession: () => created.promise,
    startRun: async (input) => {
      sent = input;
    },
  });
  store.select(owner("b", "elsewhere"));
  store.select(owner("a"));
  store.update({ text: "new draft after returning" });
  created.resolve({ id: "created" });
  await request;
  assert.equal(sent.sessionId, "created");
  assert.equal(sent.prompt, "first task");
  assert.equal(store.selection().sessionId, "");
  assert.equal(store.read().text, "new draft after returning");
});

test("typing during admission is preserved even when it returns to the submitted text", async () => {
  const store = createComposerStore(owner("a", "chat"));
  store.update({ text: "same text" });
  const submission = store.begin();
  const receipt = deferred();
  const request = submitDraft(store, submission, {
    createSession: async () => ({ id: "unused" }),
    startRun: () => receipt.promise,
  });
  store.update({ text: "replacement" });
  store.update({ text: "same text", attachments: ["new.txt"] });
  receipt.resolve({});
  await request;
  assert.equal(store.read().text, "same text");
  assert.deepEqual(store.read().attachments, ["new.txt"]);
});

test("the selected new-chat draft adopts its admitted session and preserves newer typing", async () => {
  const store = createComposerStore(owner("a"));
  store.update({ text: "first task", attachments: ["first.txt"] });
  const submission = store.begin();
  const receipt = deferred();
  const request = submitDraft(store, submission, {
    createSession: async () => ({ id: "created" }),
    startRun: () => receipt.promise,
  });
  await Promise.resolve();
  store.update({ text: "next task", attachments: ["second.txt"] });
  receipt.resolve({});
  await request;
  assert.equal(store.selection().sessionId, "created");
  assert.equal(store.read().text, "next task");
  assert.deepEqual(store.read().attachments, ["second.txt"]);
  assert.equal(store.read(owner("a")).text, "");
});

test("failed admission preserves the source draft and synchronous locking prevents duplicate submits", async () => {
  const store = createComposerStore(owner("a", "chat"));
  store.update({ text: "retry me", attachments: ["input.txt"] });
  const submission = store.begin();
  assert.equal(store.begin(), undefined);
  await assert.rejects(
    submitDraft(store, submission, {
      createSession: async () => ({ id: "unused" }),
      startRun: async () => {
        throw new Error("offline");
      },
    }),
    /offline/,
  );
  assert.equal(store.read().text, "retry me");
  assert.deepEqual(store.read().attachments, ["input.txt"]);
  store.finish(submission);
  assert.ok(store.begin());
});

test("deleting a different conversation cannot change the current selection", () => {
  const store = createComposerStore(owner("a", "old"));
  store.update({ text: "old draft" });
  store.select(owner("b", "current"));
  store.update({ text: "keep me" });
  store.remove(owner("a", "old"));
  assert.equal(store.selection().sessionId, "current");
  assert.equal(store.read().text, "keep me");
});
