import { it, expect, vi, afterEach } from "vitest";
import {
  createComposerStore,
  submitDraft,
  type StoredDraft,
  type ComposerOwner,
  type DraftPersistence,
  type ComposerAPI,
} from "../../src/lib/composer";
import { deferred } from "./fixtures";

const owner = { workspaceId: "w", sessionId: "s" };
function persisted(initial?: Partial<StoredDraft>) {
  let saved: StoredDraft = {
    ...owner,
    text: "",
    attachments: [],
    revision: 0,
    ...initial,
  };
  const persistence: DraftPersistence = {
    get: vi.fn(async (o: ComposerOwner) => ({ ...saved, ...o })),
    save: vi.fn(async (input) => {
      if (input.expectedRevision !== saved.revision)
        throw new Error("draft revision conflict");
      saved = { ...input, revision: saved.revision + 1 };
      return saved;
    }),
  };
  return {
    persistence,
    current: () => saved,
    remote: (patch: Partial<StoredDraft>) => {
      saved = { ...saved, ...patch };
    },
  };
}
afterEach(() => vi.useRealTimers());

it("restores text and attachments after a restart and does not write during hydration", async () => {
  vi.useFakeTimers();
  const data = persisted();
  const first = createComposerStore(owner, undefined, data.persistence);
  await first.load();
  first.update({
    text: "unfinished task",
    attachments: ["src/main.go"],
    focusedRunId: "stopped-run",
  });
  await first.flush();
  const restarted = createComposerStore(owner, undefined, data.persistence);
  await restarted.load();
  expect(restarted.read()).toMatchObject({
    text: "unfinished task",
    attachments: ["src/main.go"],
    focusedRunId: "stopped-run",
  });
  expect(data.persistence.save).toHaveBeenCalledTimes(1);
});

it("binds late draft hydration to its source and preserves typing during hydration", async () => {
  vi.useFakeTimers();
  const get = deferred<StoredDraft>();
  const persistence: DraftPersistence = {
    get: () => get.promise,
    save: vi.fn(),
  };
  const store = createComposerStore(owner, undefined, persistence);
  const loading = store.load();
  store.update({ text: "new local text" });
  store.select({ workspaceId: "other", sessionId: "other" });
  store.update({ text: "other chat" });
  get.resolve({
    ...owner,
    text: "stored text",
    attachments: ["stored.txt"],
    revision: 4,
  });
  await loading;
  expect(store.read(owner).text).toBe("new local text");
  expect(store.read().text).toBe("other chat");
});

it("serializes saves and advances the expected revision before saving newer typing", async () => {
  vi.useFakeTimers();
  const first = deferred<StoredDraft>();
  const data = persisted();
  const save = vi
    .fn()
    .mockImplementationOnce(() => first.promise)
    .mockImplementationOnce(async (input) => ({ ...input, revision: 2 }));
  const store = createComposerStore(owner, undefined, {
    ...data.persistence,
    save,
  });
  await store.load();
  store.update({ text: "first" });
  const saving = store.flush();
  await vi.waitFor(() => expect(save).toHaveBeenCalledTimes(1));
  store.update({ text: "second" });
  const alsoSaving = store.flush();
  expect(save).toHaveBeenCalledTimes(1);
  first.resolve({ ...owner, text: "first", attachments: [], revision: 1 });
  await Promise.all([saving, alsoSaving]);
  expect(save).toHaveBeenCalledTimes(2);
  expect(save.mock.calls[1][0]).toMatchObject({
    text: "second",
    expectedRevision: 1,
  });
});

it("clears only the admitted snapshot and saves newer typing against the returned tombstone", async () => {
  vi.useFakeTimers();
  const data = persisted();
  const store = createComposerStore(owner, undefined, data.persistence);
  await store.load();
  store.update({ text: "submitted" });
  const submission = store.begin()!;
  const receipt = deferred<{ clearedDraftRevision: number }>();
  const startRun = vi.fn(
    (_input: Parameters<ComposerAPI["startRun"]>[0]) => receipt.promise,
  );
  const sending = submitDraft(store, submission, {
    createSession: vi.fn(),
    startRun,
  });
  await vi.waitFor(() => expect(startRun).toHaveBeenCalled());
  expect(startRun.mock.calls[0][0]).toMatchObject({
    draftRevision: 1,
    draftSessionId: "s",
  });
  store.update({ text: "next task" });
  data.remote({ text: "", revision: 2 });
  receipt.resolve({ clearedDraftRevision: 2 });
  await sending;
  store.finish(submission);
  await store.flush();
  expect(data.current()).toMatchObject({ text: "next task", revision: 3 });
});

it("blocks writes after a conflicting admission receipt until the user restores the saved draft", async () => {
  vi.useFakeTimers();
  const data = persisted();
  const store = createComposerStore(owner, undefined, data.persistence);
  await store.load();
  store.update({ text: "submitted" });
  const submission = store.begin()!;
  await submitDraft(store, submission, {
    createSession: vi.fn(),
    startRun: async () => {
      data.remote({ text: "newer server text", revision: 2 });
      return { draftRevision: 2 };
    },
  });
  store.finish(submission);
  expect(store.read().text).toBe("submitted");
  expect(store.persistenceStatus().conflict).toBe(true);
  await expect(store.flush()).rejects.toThrow("saved draft changed");
  expect(data.current().text).toBe("newer server text");
  expect(data.persistence.save).toHaveBeenCalledTimes(1);
  await store.restore();
  expect(store.read().text).toBe("newer server text");
  expect(store.persistenceStatus().conflict).toBe(false);
  store.update({ text: "reconciled" });
  await store.flush();
  expect(data.current()).toMatchObject({ text: "reconciled", revision: 3 });
});

it("keeps a failed draft save and never admits the run using an unsaved snapshot", async () => {
  vi.useFakeTimers();
  const data = persisted();
  const store = createComposerStore(owner, undefined, data.persistence);
  await store.load();
  store.update({ text: "local" });
  data.remote({ text: "remote", revision: 4 });
  const run = vi.fn();
  await expect(
    submitDraft(store, store.begin()!, {
      createSession: vi.fn(),
      startRun: run,
    }),
  ).rejects.toThrow("revision conflict");
  expect(run).not.toHaveBeenCalled();
  expect(store.read().text).toBe("local");
  expect(store.persistenceStatus().conflict).toBe(true);
});

it("does not overwrite edits made while the user restores a saved draft", async () => {
  vi.useFakeTimers();
  const data = persisted();
  const store = createComposerStore(owner, undefined, data.persistence);
  await store.load();
  const restored = deferred<StoredDraft>();
  vi.mocked(data.persistence.get).mockImplementationOnce(
    () => restored.promise,
  );
  const restoring = store.restore();
  store.update({ text: "typed during restore" });
  restored.resolve({ ...owner, text: "remote", attachments: [], revision: 2 });
  await expect(restoring).rejects.toThrow("draft changed");
  expect(store.read().text).toBe("typed during restore");
});

it("moves newer typing to an admitted new chat without resurrecting its cleared source", async () => {
  vi.useFakeTimers();
  const source = { workspaceId: "w", sessionId: "" };
  const records = new Map<string, StoredDraft>();
  const persistence: DraftPersistence = {
    get: async (owner) =>
      records.get(owner.sessionId) || {
        ...owner,
        text: "",
        attachments: [],
        revision: 0,
      },
    save: vi.fn(async (input) => {
      const previous = records.get(input.sessionId)?.revision || 0;
      if (input.expectedRevision !== previous)
        throw new Error("draft revision conflict");
      const next = { ...input, revision: previous + 1 };
      records.set(input.sessionId, next);
      return next;
    }),
  };
  const store = createComposerStore(source, undefined, persistence);
  await store.load();
  store.update({ text: "first task" });
  const submission = store.begin()!;
  const receipt = deferred<{ clearedDraftRevision: number }>();
  const startRun = vi.fn(() => receipt.promise);
  const sending = submitDraft(store, submission, {
    createSession: async () => ({ id: "created" }),
    startRun,
  });
  await vi.waitFor(() => expect(startRun).toHaveBeenCalled());
  store.update({ text: "next task", attachments: ["next.txt"] });
  records.set("", { ...source, text: "", attachments: [], revision: 2 });
  receipt.resolve({ clearedDraftRevision: 2 });
  await sending;
  store.finish(submission);
  await store.flush();
  await store.flush(source);
  expect(store.selection().sessionId).toBe("created");
  expect(records.get("created")).toMatchObject({
    text: "next task",
    attachments: ["next.txt"],
  });
  expect(records.get("")).toMatchObject({ text: "", revision: 2 });
  const restarted = createComposerStore(source, undefined, persistence);
  await restarted.load();
  expect(restarted.read().text).toBe("");
});
