import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ArrowLeft,
  ArrowUp,
  ChevronRight,
  Clock3,
  Download,
  Film,
  Folder,
  FolderOpen,
  Image,
  LoaderCircle,
  Play,
  RefreshCw,
  Search,
  Square,
} from "lucide-react";
import { api } from "../bridge";
import type { Model, Runtime } from "../types";
import {
  isVisionAnswerModel,
  VIDEO_EMBEDDING_MODEL,
} from "../lib/modelCapabilities";
import {
  createVideoDrafts,
  createVideoSelectionGuard,
  createVideoSeeker,
  scopedMediaURL,
  validatedVideoCitations,
  videoJobActive,
  videoTime,
  visibleVideoJobs,
  videoJobMessage,
  videoClipMessage,
  type VideoAnswer,
  type VideoClip,
  type VideoDetails,
  type VideoEvidence,
} from "../lib/video";
import { Button, Empty, Field, Modal, formatBytes, friendlyStatus } from "./ui";

const drafts = createVideoDrafts();
const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

function FrameImage({
  workspaceId,
  clipId,
  frame,
  onSeek,
}: {
  workspaceId: string;
  clipId: string;
  frame: VideoEvidence;
  onSeek: () => void;
}) {
  const [url, setURL] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let current = true;
    setURL("");
    setError("");
    api<{ url: string }>("video.media.open", {
      workspaceId,
      clipId,
      kind: "frame",
      frameId: frame.frameId,
    })
      .then((result) => {
        if (current) setURL(scopedMediaURL(result.url));
      })
      .catch((reason) => {
        if (current) setError(errorText(reason));
      });
    return () => {
      current = false;
    };
  }, [workspaceId, clipId, frame.frameId]);
  return (
    <button
      className="video-evidence"
      onClick={onSeek}
      aria-label={`Seek to ${videoTime(frame.timeSeconds)}, evidence ${frame.label || frame.frameId}`}
    >
      <span className="video-thumbnail">
        {url && !error ? (
          <img
            src={url}
            alt={`Retrieved frame at ${videoTime(frame.timeSeconds)}`}
            loading="lazy"
            onError={() => setError("Thumbnail could not be loaded")}
          />
        ) : error ? (
          <span>{error}</span>
        ) : (
          <LoaderCircle size={18} className="spin" />
        )}
        <span className="video-time">
          <Play size={10} />
          {videoTime(frame.timeSeconds)}
        </span>
      </span>
      <span className="video-evidence-caption">
        <strong>{frame.label || "Frame"}</strong>
        <small>
          {Number.isFinite(frame.score)
            ? `Similarity ${frame.score.toFixed(3)}`
            : "Retrieved evidence"}
        </small>
      </span>
    </button>
  );
}

function ImportClip({
  workspaceId,
  open,
  onClose,
  onImport,
}: {
  workspaceId: string;
  open: boolean;
  onClose: () => void;
  onImport: (path: string) => Promise<void>;
}) {
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<
    Array<{
      name: string;
      path: string;
      isDir: boolean;
      size: number;
      symlink?: boolean;
    }>
  >([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!open) return;
    let current = true;
    setLoading(true);
    setError("");
    setEntries([]);
    api<{ entries: typeof entries }>("file.list", { workspaceId, path })
      .then((value) => {
        if (current) setEntries(value.entries || []);
      })
      .catch((reason) => {
        if (current) setError(errorText(reason));
      })
      .finally(() => {
        if (current) setLoading(false);
      });
    return () => {
      current = false;
    };
  }, [workspaceId, open, path, revision]);
  const visible = entries.filter((e) => e.isDir || /\.mp4$/i.test(e.name));
  return (
    <Modal
      open={open}
      onClose={() => {
        if (!busy) onClose();
      }}
      title="Import a workspace clip"
      description="An immutable copy is saved in the app. Indexing is a separate action."
    >
      <p className="video-scope">
        Visual MP4 · H.264 · up to 90 seconds · 256 MiB · 1080p · constant frame
        rate, up to 60 fps. Audio is not analyzed.
      </p>
      <div className="video-browser-toolbar">
        <Button
          size="icon"
          aria-label="Parent folder"
          disabled={!path || !!busy}
          onClick={() => setPath(path.split("/").slice(0, -1).join("/"))}
        >
          <ArrowLeft size={15} />
        </Button>
        <code>{path || "Workspace root"}</code>
        <Button
          size="icon"
          aria-label="Refresh workspace files"
          disabled={loading || !!busy}
          onClick={() => setRevision((n) => n + 1)}
        >
          <RefreshCw size={14} />
        </Button>
      </div>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      <div className="video-file-list" aria-label="Workspace MP4 files">
        {loading ? (
          <div className="loading-pane">
            <LoaderCircle className="spin" size={20} />
            Reading workspace
          </div>
        ) : visible.length ? (
          visible.map((entry) => (
            <button
              key={entry.path}
              disabled={
                !!busy ||
                entry.symlink ||
                (!entry.isDir && entry.size > 256 * 1024 * 1024)
              }
              onClick={async () => {
                if (entry.isDir) {
                  setPath(entry.path);
                  return;
                }
                setBusy(entry.path);
                setError("");
                try {
                  await onImport(entry.path);
                } catch (reason) {
                  setError(errorText(reason));
                } finally {
                  setBusy("");
                }
              }}
            >
              {busy === entry.path ? (
                <LoaderCircle className="spin" size={17} />
              ) : entry.isDir ? (
                <Folder size={17} />
              ) : (
                <Film size={17} />
              )}
              <span>
                {entry.name}
                <small>
                  {entry.symlink
                    ? "Symlinks cannot be imported"
                    : entry.isDir
                      ? "Folder"
                      : formatBytes(entry.size)}
                </small>
              </span>
              {entry.isDir ? (
                <ChevronRight size={14} />
              ) : (
                <span className="video-import-label">Import</span>
              )}
            </button>
          ))
        ) : (
          <p className="muted">
            No MP4 clips in this folder. Choose another folder or place a
            supported clip in your workspace.
          </p>
        )}
      </div>
    </Modal>
  );
}

export default function VideoPane({
  workspaceId,
  models,
  runtime,
  defaultModel,
}: {
  workspaceId: string;
  models: Model[];
  runtime: Runtime;
  defaultModel: string;
}) {
  const [clips, setClips] = useState<VideoClip[]>([]);
  const [clipId, setClipId] = useState("");
  const [details, setDetails] = useState<VideoDetails | null>(null);
  const [listLoading, setListLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [listError, setListError] = useState("");
  const [error, setError] = useState("");
  const [mediaError, setMediaError] = useState("");
  const [mediaURL, setMediaURL] = useState("");
  const [importOpen, setImportOpen] = useState(false);
  const [listRevision, refreshList] = useState(0);
  const [detailRevision, refreshDetails] = useState(0);
  const [mediaRevision, refreshMedia] = useState(0);
  const [busy, setBusy] = useState("");
  const [answerId, setAnswerId] = useState("");
  const [answer, setAnswer] = useState<VideoAnswer | null>(null);
  const [answerLoading, setAnswerLoading] = useState(false);
  const [answerError, setAnswerError] = useState("");
  const [answerRevision, refreshAnswer] = useState(0);
  const [model, setModel] = useState("");
  const [, refreshDraft] = useState(0);
  const [guard] = useState(createVideoSelectionGuard);
  const [seeker] = useState(createVideoSeeker);
  const player = useRef<HTMLVideoElement>(null);
  const selected = useRef("");
  const mounted = useRef(true);
  const draftKey = `${workspaceId}:${clipId}`;
  const question = drafts.read(draftKey);
  const visionModels = models.filter(isVisionAnswerModel);
  const answerModel =
    model ||
    (visionModels.some((m) => m.name === defaultModel)
      ? defaultModel
      : visionModels[0]?.name || "");
  const answerModelValid = visionModels.some((m) => m.name === answerModel);
  const embedding = models.find(
    (m) => m.name === VIDEO_EMBEDDING_MODEL && !m.remote,
  );
  const pull = runtime.pulls?.find(
    (p) => (p.name || p.model) === VIDEO_EMBEDDING_MODEL,
  );
  const pulling =
    !!pull &&
    !["success", "completed", "failed", "cancelled"].includes(
      pull.status || "",
    ) &&
    !pull.error;
  const activeJobs = (details?.jobs || []).filter((job) =>
    videoJobActive(job.status),
  );
  const clip = details?.clip;
  const indexBusy = activeJobs.some(
    (job) => !["retrieving", "answering"].includes(job.stage),
  );

  function selectClip(id: string) {
    guard.select(id);
    selected.current = id;
    seeker.clear();
    setClipId(id);
    setDetails(null);
    setAnswerId("");
    setAnswer(null);
    setError("");
    setAnswerError("");
    setMediaURL("");
    setMediaError("");
    setBusy("");
  }
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      guard.select("");
      seeker.clear();
    };
  }, [guard, seeker]);

  useEffect(() => {
    let current = true;
    setListLoading(true);
    setListError("");
    api<{ clips: VideoClip[] }>("video.list", { workspaceId })
      .then((result) => {
        if (!current) return;
        const next = result.clips || [];
        setClips(next);
        if (!selected.current && next.length) selectClip(next[0].id);
      })
      .catch((reason) => {
        if (current) setListError(errorText(reason));
      })
      .finally(() => {
        if (current) setListLoading(false);
      });
    return () => {
      current = false;
    };
  }, [workspaceId, listRevision]);

  useEffect(() => {
    if (!clipId) return;
    let current = true;
    let timer: ReturnType<typeof setTimeout>;
    setDetailLoading(true);
    async function load() {
      try {
        const result = await api<VideoDetails>("video.get", {
          workspaceId,
          clipId,
        });
        if (!current) return;
        if (
          result.clip.id !== clipId ||
          result.clip.workspaceId !== workspaceId
        )
          throw new Error("The service returned a different clip.");
        setDetails(result);
        setError("");
        setClips((previous) =>
          previous.some((item) => item.id === clipId)
            ? previous.map((item) => (item.id === clipId ? result.clip : item))
            : [result.clip, ...previous],
        );
        if (
          result.jobs?.some((job) => videoJobActive(job.status)) ||
          ["importing", "indexing"].includes(result.clip.status)
        )
          timer = setTimeout(load, 1200);
      } catch (reason) {
        if (current) setError(errorText(reason));
      } finally {
        if (current) setDetailLoading(false);
      }
    }
    void load();
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [workspaceId, clipId, detailRevision]);

  // Acquire one short local URL per selection, not a new capability on every poll.
  const canPlay =
    !!clip && clip.status !== "importing" && clip.durationSeconds > 0;
  useEffect(() => {
    if (!clipId || !canPlay) return;
    let current = true;
    setMediaError("");
    api<{ url: string }>("video.media.open", {
      workspaceId,
      clipId,
      kind: "original",
    })
      .then((result) => {
        if (current) setMediaURL(scopedMediaURL(result.url));
      })
      .catch((reason) => {
        if (current) setMediaError(errorText(reason));
      });
    return () => {
      current = false;
      seeker.clear();
    };
  }, [workspaceId, clipId, canPlay, mediaRevision, seeker]);

  useEffect(() => {
    if (!answerId || !clipId) return;
    let current = true;
    let timer: ReturnType<typeof setTimeout>;
    setAnswer(null);
    setAnswerError("");
    setAnswerLoading(true);
    async function load() {
      try {
        const result = await api<VideoAnswer>("video.answer.get", {
          workspaceId,
          id: answerId,
        });
        if (!current) return;
        if (
          result.id !== answerId ||
          result.clipId !== clipId ||
          result.workspaceId !== workspaceId
        )
          throw new Error("The service returned a different answer.");
        setAnswer(result);
        setAnswerError("");
        if (videoJobActive(result.status)) timer = setTimeout(load, 1000);
      } catch (reason) {
        if (current) setAnswerError(errorText(reason));
      } finally {
        if (current) setAnswerLoading(false);
      }
    }
    void load();
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [workspaceId, clipId, answerId, answerRevision]);

  async function perform(method: string, params: Record<string, unknown>) {
    const owner = guard.capture();
    setBusy(method);
    setError("");
    try {
      await api(method, { workspaceId, ...params });
      if (guard.current(owner) && mounted.current) {
        refreshDetails((n) => n + 1);
        refreshList((n) => n + 1);
      }
    } catch (reason) {
      if (guard.current(owner) && mounted.current) setError(errorText(reason));
    } finally {
      if (guard.current(owner) && mounted.current) setBusy("");
    }
  }

  async function ask(event: FormEvent) {
    event.preventDefault();
    if (
      !question.trim() ||
      !clipId ||
      clip?.status !== "ready" ||
      activeJobs.length ||
      busy ||
      !answerModelValid
    )
      return;
    const owner = guard.capture();
    const snapshot = drafts.begin(draftKey);
    if (!snapshot) return;
    setBusy("video.ask");
    setError("");
    try {
      const result = await api<{ answerId: string; jobId: string }>(
        "video.ask",
        { workspaceId, clipId, question: snapshot.text, model: answerModel },
      );
      drafts.submitted(snapshot);
      if (guard.current(owner) && mounted.current) {
        setAnswerId(result.answerId);
        refreshAnswer((n) => n + 1);
        refreshDraft((n) => n + 1);
        refreshDetails((n) => n + 1);
      }
    } catch (reason) {
      if (guard.current(owner) && mounted.current) setError(errorText(reason));
    } finally {
      drafts.finish(snapshot.key);
      if (guard.current(owner) && mounted.current) setBusy("");
    }
  }
  function seek(time: number) {
    if (!player.current) return;
    const owner = guard.capture();
    seeker.seek(
      player.current,
      time,
      () => mounted.current && guard.current(owner),
    );
  }
  const citations = answer ? validatedVideoCitations(answer) : [];

  return (
    <div className="video-pane">
      <header className="video-heading">
        <div>
          <span className="eyebrow">VISUAL MEMORY</span>
          <h2>Your clips, with evidence.</h2>
          <p>
            Search sampled frames. Ask what is visible. Return to the source.
          </p>
        </div>
        <div className="row-actions">
          <Button
            size="icon"
            aria-label="Refresh video library"
            onClick={() => {
              refreshList((n) => n + 1);
              refreshDetails((n) => n + 1);
            }}
          >
            <RefreshCw size={16} />
          </Button>
          <Button variant="default" onClick={() => setImportOpen(true)}>
            <FolderOpen size={15} />
            Import clip
          </Button>
        </div>
      </header>
      <p className="video-scope">
        MP4 / H.264 · ≤ 90 seconds · ≤ 256 MiB · ≤ 1080p · CFR ≤ 60 fps. Visual
        frames only; audio and unsampled motion are not analyzed.
      </p>
      {!embedding && (
        <section className="video-setup" aria-label="Embedding model setup">
          <span className="entity-icon">
            <Image size={21} />
          </span>
          <div>
            <strong>Give your clips a visual index</strong>
            <p>
              Download {VIDEO_EMBEDDING_MODEL} (~778 MB). Your installed vision
              model answers questions separately.
            </p>
            {pull && (
              <p role="status">
                {pull.error || pull.status}
                {typeof pull.completed === "number" && !!pull.total
                  ? ` · ${Math.min(100, Math.round((pull.completed / pull.total) * 100))}%`
                  : ""}
              </p>
            )}
          </div>
          <Button
            size="small"
            disabled={!runtime.available || !!busy}
            loading={busy === "model.pull"}
            onClick={() =>
              void perform(pulling ? "model.cancelPull" : "model.pull", {
                name: VIDEO_EMBEDDING_MODEL,
              })
            }
          >
            {pulling ? (
              <>
                <Square size={13} />
                Cancel download
              </>
            ) : (
              <>
                <Download size={14} />
                Download encoder
              </>
            )}
          </Button>
        </section>
      )}
      {!runtime.available && (
        <p className="error-text" role="alert">
          Local inference is unavailable.{" "}
          {runtime.error || "Start Ollama to index or ask questions."}
        </p>
      )}
      {listError && (
        <p className="error-text" role="alert">
          Could not load clips: {listError}
        </p>
      )}
      {error && (
        <div className="video-error" role="alert">
          <span>{error}</span>
          <Button size="small" onClick={() => refreshDetails((n) => n + 1)}>
            Refresh
          </Button>
        </div>
      )}
      <div className="video-layout">
        <aside className="video-library" aria-label="Imported clips">
          <div className="video-library-label">
            <Film size={13} />
            <span>CLIPS</span>
            <small>{clips.length}</small>
          </div>
          {listLoading && !clips.length ? (
            <p className="video-loading">
              <LoaderCircle className="spin" size={16} />
              Loading clips
            </p>
          ) : (
            clips.map((item) => (
              <button
                className={`video-clip ${item.id === clipId ? "selected" : ""}`}
                key={item.id}
                onClick={() => selectClip(item.id)}
                aria-pressed={item.id === clipId}
              >
                <span className="video-clip-icon">
                  <Film size={18} />
                </span>
                <span>
                  <strong>{item.name}</strong>
                  <small>
                    {videoTime(item.durationSeconds)} ·{" "}
                    {friendlyStatus(item.status)}
                  </small>
                </span>
                <ChevronRight size={13} />
              </button>
            ))
          )}
          {!listLoading && !clips.length && (
            <p className="video-library-empty">
              Imported clips stay with this workspace.
            </p>
          )}
        </aside>
        <main className="video-content">
          {!clipId ? (
            <Empty
              icon={<Film size={30} />}
              title="A new way to look through your work"
              description="Import a short workspace clip, build its visual index, then ask a question and inspect the frames behind the answer."
              action={
                <Button onClick={() => setImportOpen(true)}>
                  <FolderOpen size={15} />
                  Choose an MP4
                </Button>
              }
            />
          ) : !clip ? (
            <div className="loading-pane">
              {detailLoading ? (
                <>
                  <LoaderCircle className="spin" />
                  Opening clip
                </>
              ) : (
                "Clip details are unavailable. Refresh to retry."
              )}
            </div>
          ) : (
            <>
              <div className="video-clip-heading">
                <div>
                  <h3>{clip.name}</h3>
                  <p>
                    {videoTime(clip.durationSeconds)} ·{" "}
                    {formatBytes(clip.bytes)}
                    {typeof clip.frameCount === "number"
                      ? ` · ${clip.frameCount} indexed frames`
                      : ""}
                  </p>
                </div>
                <span className="tag">{friendlyStatus(clip.status)}</span>
              </div>
              <div className="video-player">
                {mediaURL ? (
                  <video
                    key={clip.id}
                    ref={player}
                    src={mediaURL}
                    controls
                    preload="metadata"
                    playsInline
                    aria-label={`Source video: ${clip.name}`}
                    onError={() =>
                      setMediaError(
                        "Playback could not load this clip. Refresh playback to request a new local URL.",
                      )
                    }
                  />
                ) : (
                  <div className="video-player-placeholder">
                    <Film size={35} />
                    <span>
                      {clip.status === "importing"
                        ? "Copying and checking your clip…"
                        : canPlay
                          ? "Opening source playback…"
                          : "Playback is available after a successful import."}
                    </span>
                  </div>
                )}
              </div>
              {mediaError && (
                <div className="video-error" role="alert">
                  <span>{mediaError}</span>
                  <Button
                    size="small"
                    onClick={() => refreshMedia((n) => n + 1)}
                  >
                    Refresh playback
                  </Button>
                </div>
              )}
              {clip.error && (
                <p className="error-text" role="alert">
                  {videoClipMessage(clip, details?.jobs || [])}
                </p>
              )}
              <div className="video-index-bar">
                <div>
                  <Search size={17} />
                  <span>
                    {clip.status === "ready"
                      ? "Visual index ready"
                      : "Build a searchable view of this clip"}
                    <small>
                      {clip.status === "ready"
                        ? clip.embeddingModel || VIDEO_EMBEDDING_MODEL
                        : "Extract and embed sampled frames. The original clip is preserved."}
                    </small>
                  </span>
                </div>
                <Button
                  size="small"
                  disabled={
                    !runtime.available ||
                    !embedding ||
                    !!busy ||
                    activeJobs.length > 0 ||
                    !["imported", "ready", "interrupted", "failed"].includes(
                      clip.status,
                    ) ||
                    !clip.durationSeconds
                  }
                  loading={busy === "video.index"}
                  onClick={() => void perform("video.index", { clipId })}
                >
                  {clip.status === "ready" ? "Rebuild index" : "Index clip"}
                </Button>
              </div>
              {visibleVideoJobs(details?.jobs || []).map((job) => (
                <div
                  className={`video-job ${videoJobActive(job.status) ? "active" : ""}`}
                  key={job.id}
                >
                  <span>
                    {videoJobActive(job.status) ? (
                      <LoaderCircle size={14} className="spin" />
                    ) : (
                      <Clock3 size={14} />
                    )}
                    {job.stage || "Job"} · {friendlyStatus(job.status)}
                  </span>
                  <small>
                    {typeof job.completed === "number" &&
                    typeof job.total === "number" &&
                    job.total > 0
                      ? `${job.completed} / ${job.total}`
                      : ""}
                  </small>
                  {videoJobActive(job.status) && (
                    <Button
                      size="small"
                      disabled={job.status === "cancelling" || !!busy}
                      onClick={() =>
                        void perform("video.job.cancel", { id: job.id })
                      }
                    >
                      <Square size={11} />
                      Cancel
                    </Button>
                  )}
                  {videoJobMessage(job) && (
                    <p className="error-text">{videoJobMessage(job)}</p>
                  )}
                </div>
              ))}
              <section className="video-question-section">
                <div className="video-section-title">
                  <span className="eyebrow">ASK THE CLIP</span>
                  <small>Answers cite visible frames</small>
                </div>
                <form onSubmit={ask} className="video-question">
                  <textarea
                    aria-label="Question about the clip"
                    value={question}
                    onChange={(event) => {
                      drafts.write(draftKey, event.target.value);
                      refreshDraft((n) => n + 1);
                    }}
                    placeholder="What changes on the screen? Where does the red object appear?"
                    rows={3}
                    maxLength={4000}
                  />
                  <div>
                    <select
                      aria-label="Vision model for video answer"
                      value={answerModel}
                      onChange={(event) => setModel(event.target.value)}
                    >
                      {!answerModel && (
                        <option value="">
                          No local vision model available
                        </option>
                      )}
                      {answerModel && !answerModelValid && (
                        <option value={answerModel} disabled>
                          {answerModel} (unavailable)
                        </option>
                      )}
                      {visionModels.map((item) => (
                        <option key={item.name} value={item.name}>
                          {item.name}
                        </option>
                      ))}
                    </select>
                    <Button
                      type="submit"
                      variant="default"
                      size="small"
                      disabled={
                        !runtime.available ||
                        !embedding ||
                        clip.status !== "ready" ||
                        !!busy ||
                        activeJobs.length > 0 ||
                        !question.trim() ||
                        !answerModelValid
                      }
                      loading={busy === "video.ask"}
                    >
                      <ArrowUp size={16} />
                      Ask
                    </Button>
                  </div>
                </form>
                {clip.status !== "ready" ? (
                  <p className="video-hint">
                    Finish indexing before asking a question.
                  </p>
                ) : !answerModelValid ? (
                  <p className="video-hint">
                    A local completion model with declared vision capability is
                    required.
                  </p>
                ) : indexBusy ? (
                  <p className="video-hint">Wait for the current video job.</p>
                ) : (
                  <p className="video-hint">
                    Sampled frames can miss motion between images. Speech and
                    sound are outside this view.
                  </p>
                )}
              </section>
              {!!details?.recentAnswers?.length && (
                <div className="video-answer-history">
                  <Field label="Saved questions">
                    <select
                      value={answerId}
                      onChange={(event) => setAnswerId(event.target.value)}
                    >
                      <option value="">Choose a previous question</option>
                      {details.recentAnswers.map((item) => (
                        <option value={item.id} key={item.id}>
                          {item.question}
                        </option>
                      ))}
                    </select>
                  </Field>
                </div>
              )}
              {answerError && (
                <div className="video-error" role="alert">
                  <span>{answerError}</span>
                  <Button
                    size="small"
                    onClick={() => refreshAnswer((n) => n + 1)}
                  >
                    Retry answer
                  </Button>
                </div>
              )}
              {answerLoading && !answer && (
                <p className="video-loading">
                  <LoaderCircle size={15} className="spin" />
                  Loading answer
                </p>
              )}
              {answer && (
                <section className="video-answer">
                  <div className="video-section-title">
                    <span className="eyebrow">
                      {videoJobActive(answer.status)
                        ? "WORKING WITH THE EVIDENCE"
                        : "ANSWER"}
                    </span>
                    <span className="tag">{friendlyStatus(answer.status)}</span>
                  </div>
                  <h4>{answer.question}</h4>
                  <p className="video-answer-model">
                    {answer.model}
                    {answer.indexId
                      ? ` · Index ${answer.indexId.slice(0, 8)}`
                      : ""}
                  </p>
                  {answer.text ? (
                    <p className="video-answer-text">{answer.text}</p>
                  ) : videoJobActive(answer.status) ? (
                    <p className="video-loading">
                      <LoaderCircle size={15} className="spin" />
                      Retrieving frames and preparing an answer…
                    </p>
                  ) : (
                    <p className="muted">No answer text was recorded.</p>
                  )}
                  {answer.error && (
                    <p className="error-text" role="alert">
                      {answer.error}
                    </p>
                  )}
                  {citations.length > 0 && (
                    <div
                      className="video-citations"
                      aria-label="Answer citations"
                    >
                      {citations.map((citation, i) => (
                        <Button
                          key={`${citation.frameId}-${i}`}
                          size="small"
                          disabled={!mediaURL}
                          onClick={() => seek(citation.timeSeconds)}
                        >
                          <Play size={12} />
                          {citation.label || "Source"} ·{" "}
                          {videoTime(citation.timeSeconds)}
                        </Button>
                      ))}
                    </div>
                  )}
                  {!!answer.evidence?.length && (
                    <>
                      <div className="video-section-title">
                        <span className="eyebrow">RETRIEVED FRAMES</span>
                        <small>
                          Similarity is a retrieval score, not confidence
                        </small>
                      </div>
                      <div className="video-evidence-grid">
                        {answer.evidence.map((frame) => (
                          <FrameImage
                            key={`${answer.id}-${frame.frameId}`}
                            workspaceId={workspaceId}
                            clipId={clipId}
                            frame={frame}
                            onSeek={() => seek(frame.timeSeconds)}
                          />
                        ))}
                      </div>
                    </>
                  )}
                </section>
              )}
            </>
          )}
        </main>
      </div>
      <ImportClip
        workspaceId={workspaceId}
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onImport={async (path) => {
          const owner = guard.capture();
          const result = await api<{ clipId: string; jobId: string }>(
            "video.import",
            { workspaceId, path },
          );
          if (!mounted.current) return;
          refreshList((n) => n + 1);
          if (guard.current(owner)) {
            setImportOpen(false);
            selectClip(result.clipId);
          }
        }}
      />
    </div>
  );
}
