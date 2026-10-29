import test from "node:test";
import assert from "node:assert/strict";
import {
  createVideoSelectionGuard,
  createVideoSeeker,
  createVideoDrafts,
  scopedMediaURL,
  validatedVideoCitations,
  visibleVideoJobs,
  videoJobMessage,
  videoClipMessage,
} from "../src/lib/video.ts";
import {
  isCompletionModel,
  isVisionAnswerModel,
  VIDEO_EMBEDDING_MODEL,
} from "../src/lib/modelCapabilities.ts";

const deferred = () => {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
};

test("job summary retains every active cancellation control and only the newest finished outcome", () => {
  const finished = Array.from({ length: 8 }, (_, i) => ({
    id: `done-${i}`,
    status: i === 0 ? "failed" : "completed",
    error: i === 0 ? "Decoder failed" : undefined,
  }));
  const jobs = [
    ...finished,
    { id: "running", status: "running" },
    { id: "cancel", status: "cancelling" },
    { id: "waiting", status: "queued" },
  ];
  const snapshot = JSON.stringify(jobs);
  assert.deepEqual(
    visibleVideoJobs(jobs).map((job) => job.id),
    ["running", "cancel", "waiting", "done-0"],
  );
  assert.equal(visibleVideoJobs(jobs).at(-1).error, "Decoder failed");
  assert.equal(
    JSON.stringify(jobs),
    snapshot,
    "historical job records are retained unchanged",
  );
  assert.deepEqual(visibleVideoJobs([]), []);
});

test("cancellation presentation follows the matching index outcome without hiding unrelated failures", () => {
  const transportError =
    'Ollama request: Post "http://127.0.0.1:11434/api/show": context canceled';
  const cancelled = {
    id: "index",
    kind: "index",
    status: "cancelled",
    error: transportError,
  };
  const clip = {
    id: "clip",
    status: "ready",
    indexId: "previous",
    error: transportError,
  };
  const jobs = [
    { id: "later-answer", kind: "answer", status: "completed" },
    cancelled,
  ];
  assert.equal(videoJobMessage(cancelled), "Indexing was cancelled.");
  assert.equal(
    videoClipMessage(clip, jobs),
    "Indexing was cancelled. The previous index is still available.",
  );
  assert.equal(
    videoClipMessage({ ...clip, status: "imported", indexId: undefined }, jobs),
    "Indexing was cancelled. You can retry indexing this clip.",
  );
  assert.equal(
    videoClipMessage({ ...clip, error: "File changed" }, jobs),
    "File changed",
  );
  assert.equal(
    videoJobMessage({ ...cancelled, status: "failed" }),
    transportError,
  );
  assert.equal(
    videoClipMessage(clip, [{ ...cancelled, status: "failed" }, ...jobs]),
    transportError,
  );
  assert.equal(videoClipMessage({ ...clip, error: undefined }, jobs), "");
  assert.equal(
    videoJobMessage({ kind: "answer", status: "cancelled" }),
    "Answer generation was cancelled.",
  );
});

test("late clip admission cannot select an old owner, including A to B to A navigation", async () => {
  const guard = createVideoSelectionGuard();
  guard.select("workspace-a:clip-a");
  const admitted = deferred();
  const owner = guard.capture();
  let selectedAnswer = "new-answer";
  const pending = admitted.promise.then((result) => {
    if (guard.current(owner)) selectedAnswer = result.answerId;
  });
  guard.select("workspace-b:clip-b");
  guard.select("workspace-a:clip-a");
  admitted.resolve({ answerId: "old-answer" });
  await pending;
  assert.equal(selectedAnswer, "new-answer");
  assert.equal(guard.current(guard.capture()), true);
  guard.select("");
  assert.equal(guard.current(owner), false);
});

test("accepted questions clear only their submitted draft and preserve newer typing and other clips", async () => {
  const drafts = createVideoDrafts();
  const key = "workspace-a:clip-a";
  drafts.write(key, "Original question");
  drafts.write("workspace-b:clip-a", "Different workspace");
  const snapshot = drafts.begin(key);
  assert.equal(
    drafts.begin(key),
    null,
    "a second synchronous submit cannot duplicate admission",
  );
  const admission = deferred();
  const pending = admission.promise.then(() => {
    drafts.submitted(snapshot);
    drafts.finish(key);
  });
  drafts.write(key, "New question typed while waiting");
  admission.resolve();
  await pending;
  assert.equal(drafts.read(key), "New question typed while waiting");
  assert.equal(drafts.read("workspace-b:clip-a"), "Different workspace");
  drafts.submitted(drafts.begin(key));
  drafts.finish(key);
  assert.equal(drafts.read(key), "");
  drafts.write(key, "Same text");
  const next = drafts.begin(key);
  drafts.write(key, "Edited");
  drafts.write(key, "Same text");
  drafts.submitted(next);
  drafts.finish(key);
  assert.equal(
    drafts.read(key),
    "Same text",
    "returning to the same text is still a newer draft",
  );
});

class Player extends EventTarget {
  readyState = 0;
  duration = NaN;
  currentTime = 0;
  load(duration = 90) {
    this.duration = duration;
    this.readyState = 1;
    this.dispatchEvent(new Event("loadedmetadata"));
  }
}

test("citations wait for metadata, newest seek wins, and navigation cancels queued seeks", () => {
  const seeker = createVideoSeeker();
  const player = new Player();
  let current = true;
  seeker.seek(player, 12, () => current);
  assert.equal(player.currentTime, 0);
  seeker.seek(player, 35, () => current);
  player.load();
  assert.equal(player.currentTime, 35);
  const next = new Player();
  seeker.seek(next, 28, () => current);
  current = false;
  next.load();
  assert.equal(next.currentTime, 0);
  current = true;
  const removed = new Player();
  seeker.seek(removed, 22, () => current);
  seeker.clear();
  removed.load();
  assert.equal(removed.currentTime, 0);
});

test("seek rejects invalid and out-of-source timestamps without coercing them", () => {
  const seeker = createVideoSeeker();
  const player = new Player();
  player.load(30);
  for (const time of [-1, Infinity, NaN])
    assert.equal(
      seeker.seek(player, time, () => true),
      false,
    );
  seeker.seek(player, 31, () => true);
  assert.equal(player.currentTime, 0);
  seeker.seek(player, 12.5, () => true);
  assert.equal(player.currentTime, 12.5);
});

test("citation controls require evidence identity and timestamp, not text or invented URLs", () => {
  const valid = { frameId: "frame1", timeSeconds: 3.25, label: "F001" };
  const answer = {
    text: "See [F999] or file:///private/video.mp4",
    evidence: [{ frameId: "frame1", timeSeconds: 3.25 }],
    citations: [
      valid,
      { ...valid, timeSeconds: 5 },
      { ...valid, frameId: "forged" },
      { ...valid, timeSeconds: NaN },
    ],
  };
  assert.deepEqual(validatedVideoCitations(answer), [valid]);
  assert.deepEqual(validatedVideoCitations({ citations: [valid] }), []);
});

test("media stays on scoped local capability URLs", () => {
  assert.equal(
    scopedMediaURL("/api/media/opaque_handle-9"),
    "/api/media/opaque_handle-9",
  );
  for (const path of [
    "file:///tmp/clip.mp4",
    "https://example.com/clip.mp4",
    "//evil.test/api/media/a",
    "data:video/mp4;base64,a",
    "/api/media/../secret",
    "/api/media/%2e%2e/secret",
    "/workspace/clip.mp4",
  ]) {
    assert.throws(() => scopedMediaURL(path), /invalid playback URL/);
  }
});

test("encoder cannot become chat default while model metadata is pending; vision selection requires evidence", () => {
  const encoder = { name: VIDEO_EMBEDDING_MODEL };
  assert.equal(isCompletionModel(encoder), false);
  assert.equal(isCompletionModel({ name: "embeddinggemma:latest" }), false);
  assert.equal(
    isCompletionModel({ name: "local/embeddinggemma-2:latest" }),
    false,
  );
  assert.equal(
    isCompletionModel({ name: "vision-only", capabilities: ["vision"] }),
    false,
  );
  assert.equal(
    isCompletionModel({
      name: "other-encoder",
      capabilities: ["embedding", "vision"],
    }),
    false,
  );
  assert.equal(
    isVisionAnswerModel({
      name: "other-encoder",
      capabilities: ["embedding", "vision"],
    }),
    false,
  );
  const known = {
    name: "gemma4:12b-mlx",
    capabilities: ["completion", "vision"],
  };
  assert.equal(isCompletionModel(known), true);
  assert.equal(isVisionAnswerModel(known), true);
  assert.equal(isVisionAnswerModel({ name: known.name }), false);
  assert.equal(isCompletionModel({ name: known.name }), true);
  assert.equal(isCompletionModel({ ...known, remote: true }), false);
});
