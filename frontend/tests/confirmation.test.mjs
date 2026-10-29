import test from "node:test";
import assert from "node:assert/strict";
import {
  createConfirmationBroker,
  confirmUnchanged,
} from "../src/lib/confirmation.ts";

const discard = {
  title: "Discard unsaved edits?",
  description: "The draft will be discarded.",
  danger: true,
};

test("confirmation waits for the matching explicit answer before a destructive action", async () => {
  const broker = createConfirmationBroker();
  let removed = false;
  const action = (async () => {
    if (await broker.request(discard)) removed = true;
  })();
  assert.equal(removed, false);
  const id = broker.current().id;
  broker.settle(id + 1, true);
  await Promise.resolve();
  assert.equal(removed, false);
  assert.equal(broker.current().id, id);
  broker.settle(id, true);
  await action;
  assert.equal(removed, true);
  assert.equal(broker.current(), null);
});

test("dialog dismissal, Escape-style cancel, and owner unmount keep draft content", async () => {
  for (const cancelBy of ["dismiss", "unmount"]) {
    const broker = createConfirmationBroker(),
      owner = new AbortController();
    let draft = "unsaved draft";
    const action = (async () => {
      if (await broker.request(discard, owner.signal)) draft = "disk content";
    })();
    if (cancelBy === "dismiss") broker.cancel();
    else owner.abort();
    await action;
    assert.equal(draft, "unsaved draft");
    assert.equal(broker.current(), null);
  }
});

test("an overlapping navigation request cannot reuse another dialog's consent", async () => {
  const broker = createConfirmationBroker();
  const first = broker.request(discard);
  const id = broker.current().id;
  const second = broker.request({
    title: "Remove workspace?",
    description: "A different operation.",
  });
  assert.equal(await second, false);
  assert.equal(broker.current().title, discard.title);
  broker.settle(id, true);
  assert.equal(await first, true);
});

test("a stale response cannot authorize a later question", async () => {
  const broker = createConfirmationBroker();
  const first = broker.request(discard),
    oldID = broker.current().id;
  broker.cancel();
  assert.equal(await first, false);
  const second = broker.request(discard),
    newID = broker.current().id;
  broker.settle(oldID, true);
  assert.equal(broker.current().id, newID);
  broker.settle(newID, false);
  assert.equal(await second, false);
});

test("discard consent cannot erase typing or a different editor selected during the wait", async () => {
  for (const change of ["content", "owner"]) {
    const broker = createConfirmationBroker();
    let state = { path: "first.ts", content: "unsaved draft" };
    const snapshot = state;
    const action = confirmUnchanged(
      snapshot,
      (captured) => captured === state,
      () => broker.request(discard),
    );
    state =
      change === "content"
        ? { ...state, content: "newer typing" }
        : { path: "other.ts", content: "other draft" };
    broker.settle(broker.current().id, true);
    assert.equal(await action, false);
    assert.notEqual(state.content, "disk content");
  }
});

test("confirmed unchanged draft can reload; a dead owner never opens a question", async () => {
  const broker = createConfirmationBroker();
  let draft = "unsaved";
  const action = (async () => {
    if (
      await confirmUnchanged(
        draft,
        (captured) => captured === draft,
        () => broker.request(discard),
      )
    )
      draft = "disk content";
  })();
  broker.settle(broker.current().id, true);
  await action;
  assert.equal(draft, "disk content");
  const owner = new AbortController();
  owner.abort();
  assert.equal(await broker.request(discard, owner.signal), false);
  assert.equal(broker.current(), null);
});
