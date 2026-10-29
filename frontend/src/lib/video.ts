export interface VideoClip {
  id: string;
  workspaceId: string;
  name: string;
  sha256: string;
  bytes: number;
  durationSeconds: number;
  status: string;
  frameCount?: number;
  embeddingModel?: string;
  indexId?: string;
  createdAt: string;
  error?: string;
}
export interface VideoJob {
  id: string;
  kind?: string;
  status: string;
  stage: string;
  completed?: number;
  total?: number;
  error?: string;
}
export interface VideoEvidence {
  frameId: string;
  timeSeconds: number;
  score: number;
  sha256: string;
  label?: string;
}
export interface VideoCitation {
  frameId: string;
  timeSeconds: number;
  label?: string;
}
export interface VideoAnswer {
  id: string;
  workspaceId: string;
  clipId: string;
  jobId: string;
  question: string;
  text: string;
  model: string;
  indexId: string;
  status: string;
  error?: string;
  createdAt: string;
  evidence?: VideoEvidence[];
  citations?: VideoCitation[];
}
export interface VideoDetails {
  clip: VideoClip;
  jobs: VideoJob[];
  recentAnswers: VideoAnswer[];
}
export function videoJobActive(status: string) {
  return ["queued", "running", "cancelling"].includes(status);
}
export function visibleVideoJobs(jobs: VideoJob[]) {
  // video.get returns newest first. Keep every active job actionable, then
  // show the newest finished outcome without repeating the stored history.
  const active = jobs.filter((job) => videoJobActive(job.status));
  const latestFinished = jobs.find((job) => !videoJobActive(job.status));
  return latestFinished ? [...active, latestFinished] : active;
}
export function videoJobMessage(job: VideoJob) {
  if (job.status !== "cancelled") return job.error || "";
  return job.kind === "index"
    ? "Indexing was cancelled."
    : job.kind === "import"
      ? "Import was cancelled."
      : job.kind === "answer"
        ? "Answer generation was cancelled."
        : "The video job was cancelled.";
}
export function videoClipMessage(clip: VideoClip, jobs: VideoJob[]) {
  if (!clip.error) return "";
  // Inspect the full history: newer answer jobs must not obscure the index
  // cancellation. A newer failed index still retains its genuine error.
  const latestIndex = jobs.find((job) => job.kind === "index");
  if (latestIndex?.status !== "cancelled" || latestIndex.error !== clip.error)
    return clip.error;
  return clip.indexId || clip.status === "ready"
    ? "Indexing was cancelled. The previous index is still available."
    : "Indexing was cancelled. You can retry indexing this clip.";
}
export function videoTime(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return "—";
  const whole = Math.floor(seconds);
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}

// A monotonic owner protects against A → B → A navigation, not just matching
// the current ID. Callers may finish work without taking over a newer view.
export function createVideoSelectionGuard() {
  let revision = 0;
  let key = "";
  return {
    select(next: string) {
      key = next;
      revision++;
    },
    capture() {
      return { key, revision };
    },
    current(owner: { key: string; revision: number }) {
      return owner.key === key && owner.revision === revision;
    },
  };
}

export function scopedMediaURL(value: string): string {
  // Media URLs are local capabilities, never filesystem paths or model links.
  if (
    !/^\/api\/media\/[A-Za-z0-9_/-]+(?:\?[A-Za-z0-9_=&%.-]+)?$/.test(value) ||
    value.includes("..")
  ) {
    throw new Error("The media service returned an invalid playback URL.");
  }
  return value;
}

type SeekablePlayer = Pick<
  HTMLVideoElement,
  | "readyState"
  | "duration"
  | "currentTime"
  | "addEventListener"
  | "removeEventListener"
>;
export function createVideoSeeker() {
  let cancel = () => {};
  return {
    clear() {
      cancel();
      cancel = () => {};
    },
    seek(player: SeekablePlayer, seconds: number, isCurrent: () => boolean) {
      cancel();
      if (!Number.isFinite(seconds) || seconds < 0) return false;
      const apply = () => {
        if (
          !isCurrent() ||
          !Number.isFinite(player.duration) ||
          seconds > player.duration
        )
          return;
        player.currentTime = seconds;
      };
      if (player.readyState >= 1) {
        apply();
        cancel = () => {};
      } else {
        const loaded = () => {
          player.removeEventListener("loadedmetadata", loaded);
          apply();
        };
        player.addEventListener("loadedmetadata", loaded);
        cancel = () => player.removeEventListener("loadedmetadata", loaded);
      }
      return true;
    },
  };
}

export function validatedVideoCitations(answer: VideoAnswer) {
  return (answer.citations || []).filter(
    (citation) =>
      Number.isFinite(citation.timeSeconds) &&
      citation.timeSeconds >= 0 &&
      answer.evidence?.some(
        (frame) =>
          frame.frameId === citation.frameId &&
          frame.timeSeconds === citation.timeSeconds,
      ),
  );
}

export function createVideoDrafts() {
  const drafts = new Map<
    string,
    { text: string; revision: number; pending: boolean }
  >();
  const get = (key: string) => {
    let value = drafts.get(key);
    if (!value) {
      value = { text: "", revision: 0, pending: false };
      drafts.set(key, value);
    }
    return value;
  };
  return {
    read(key: string) {
      return get(key).text;
    },
    write(key: string, text: string) {
      const value = get(key);
      value.text = text;
      value.revision++;
    },
    begin(key: string) {
      const value = get(key);
      if (value.pending || !value.text.trim()) return null;
      value.pending = true;
      return { key, text: value.text, revision: value.revision };
    },
    submitted(snapshot: { key: string; text: string; revision: number }) {
      const value = get(snapshot.key);
      if (value.revision === snapshot.revision) {
        value.text = "";
        value.revision++;
      }
    },
    finish(key: string) {
      get(key).pending = false;
    },
  };
}
