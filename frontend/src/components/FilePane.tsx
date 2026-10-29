import { lazy, Suspense, useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  ChevronRight,
  FileCode2,
  Folder,
  RefreshCw,
  Save,
  LoaderCircle,
  AlertTriangle,
  GitCompareArrows,
} from "lucide-react";
import { api } from "../bridge";
import {
  reconcileSavedFile,
  type FileVersion,
  type SavedChange,
} from "../lib/conversation";
import { Button, Empty, formatBytes } from "./ui";
import { useConfirmation } from "./Confirmation";
import { confirmUnchanged } from "../lib/confirmation";
const CodeEditor = lazy(() => import("./CodeEditor"));
interface Entry {
  name: string;
  path: string;
  isDir: boolean;
  size?: number;
}
interface GitChange {
  path: string;
  index: string;
  worktree: string;
  untracked: boolean;
}
const gitState: Record<string, string> = {
  M: "modified",
  A: "added",
  D: "deleted",
  R: "renamed",
  C: "copied",
  U: "conflicted",
  T: "type changed",
};
export default function FilePane({
  workspaceId,
  theme,
  diff = false,
  onDirtyChange,
  revision = 0,
  saveDisabledReason = "",
}: {
  workspaceId: string;
  theme: string;
  diff?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  revision?: number;
  saveDisabledReason?: string;
}) {
  const confirmAction = useConfirmation();
  const [directory, setDirectory] = useState(""),
    [entries, setEntries] = useState<Entry[]>([]),
    [file, setFile] = useState<FileVersion | null>(null),
    [content, setContent] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [conflict, setConflict] = useState(false),
    [diffScope, setDiffScope] = useState(
      "Unstaged tracked changes. This server does not report a broader scope.",
    ),
    [diffTruncated, setDiffTruncated] = useState(false),
    [changedFiles, setChangedFiles] = useState<GitChange[]>([]),
    [diffText, setDiff] = useState("");
  const fileRef = useRef(file),
    contentRef = useRef(content),
    fileRequest = useRef(0),
    listRequest = useRef(0),
    saving = useRef(false),
    opening = useRef(false),
    previousRevision = useRef(revision);
  fileRef.current = file;
  contentRef.current = content;
  function updateContent(value: string) {
    contentRef.current = value;
    setContent(value);
  }
  function showFile(value: FileVersion) {
    fileRef.current = value;
    setFile(value);
    updateContent(value.content);
    setConflict(false);
  }
  async function mayDiscard(reload = false) {
    const snapshot = { file: fileRef.current, content: contentRef.current };
    if (!snapshot.file || snapshot.content === snapshot.file.content)
      return true;
    return confirmUnchanged(
      snapshot,
      (captured) =>
        fileRef.current === captured.file &&
        contentRef.current === captured.content,
      () =>
        confirmAction({
          title: reload
            ? "Reload and discard edits?"
            : "Discard unsaved edits?",
          description: `Your unsaved changes to ${snapshot.file!.path} will be discarded${reload ? " and replaced with the current file on disk" : ""}.`,
          confirmLabel: reload ? "Reload from disk" : "Discard changes",
          cancelLabel: "Keep editing",
          danger: true,
        }),
    );
  }
  useEffect(() => {
    onDirtyChange?.(!!file && file.content !== content);
  }, [file, content, onDirtyChange]);
  useEffect(
    () => () => {
      fileRequest.current++;
      listRequest.current++;
      onDirtyChange?.(false);
    },
    [onDirtyChange],
  );
  async function refreshFile(discard = false) {
    const opened = fileRef.current;
    if (!opened || opening.current) return;
    const draftAtStart = contentRef.current;
    const request = ++fileRequest.current;
    const disk = await api<FileVersion>("file.read", {
      workspaceId,
      path: opened.path,
    });
    if (
      request !== fileRequest.current ||
      fileRef.current?.path !== opened.path
    )
      return;
    const changedDuringRead = contentRef.current !== draftAtStart;
    const dirty = contentRef.current !== opened.content;
    if (changedDuringRead || (dirty && !discard)) {
      if (disk.hash !== opened.hash) setConflict(true);
      return;
    }
    showFile({ ...disk, path: opened.path });
  }
  async function list(refreshOpen = false) {
    const request = ++listRequest.current;
    setBusy(true);
    setError("");
    try {
      if (diff) {
        const result = await api<{
          diff: string;
          scope?: string;
          truncated?: boolean;
          files?: GitChange[];
        }>("git.diff", { workspaceId });
        if (request === listRequest.current) {
          setDiff(result.diff || "");
          setChangedFiles(result.files || []);
          setDiffScope(
            result.scope ||
              "Unstaged tracked changes. This server does not report a broader scope.",
          );
          setDiffTruncated(!!result.truncated);
        }
      } else {
        const result = await api<{ entries: Entry[] }>("file.list", {
          workspaceId,
          path: directory,
        });
        if (request === listRequest.current) {
          setEntries(result.entries || []);
          if (refreshOpen && !saving.current) await refreshFile();
        }
      }
    } catch (error) {
      if (request === listRequest.current) setError((error as Error).message);
    } finally {
      if (request === listRequest.current) setBusy(false);
    }
  }
  useEffect(() => {
    fileRequest.current++;
    opening.current = false;
    setFile(null);
    fileRef.current = null;
    setConflict(false);
    void list();
  }, [workspaceId, directory, diff]);
  useEffect(() => {
    if (previousRevision.current !== revision) {
      previousRevision.current = revision;
      void list(true);
    }
  }, [revision]);
  async function open(entry: Entry) {
    if (saving.current) return;
    if (!(await mayDiscard())) return;
    if (entry.isDir) {
      setDirectory(entry.path);
      return;
    }
    const request = ++fileRequest.current;
    opening.current = true;
    const previousPath = fileRef.current?.path;
    const previousDraft = contentRef.current;
    setBusy(true);
    setError("");
    try {
      const result = await api<FileVersion>("file.read", {
        workspaceId,
        path: entry.path,
      });
      if (request === fileRequest.current) {
        if (
          fileRef.current?.path === previousPath &&
          contentRef.current !== previousDraft
        ) {
          setError(
            "The current file has newer unsaved edits. They were preserved; select the other file again when ready.",
          );
          return;
        }
        showFile({ ...result, path: entry.path });
      }
    } catch (error) {
      if (request === fileRequest.current) setError((error as Error).message);
    } finally {
      if (request === fileRequest.current) {
        opening.current = false;
        setBusy(false);
      }
    }
  }
  async function save() {
    const opened = fileRef.current;
    if (!opened || saving.current || saveDisabledReason) return;
    saving.current = true;
    setBusy(true);
    setError("");
    const submitted = contentRef.current;
    try {
      const change = await api<SavedChange>("file.write", {
        workspaceId,
        path: opened.path,
        content: submitted,
        expectedHash: opened.hash,
      });
      if (fileRef.current?.path === opened.path) {
        const updated = reconcileSavedFile(submitted, contentRef.current, {
          ...change,
          path: opened.path,
        });
        fileRef.current = updated.file;
        setFile(updated.file);
        updateContent(updated.content);
        setConflict(false);
      }
    } catch (error) {
      setError((error as Error).message);
    } finally {
      saving.current = false;
      setBusy(false);
    }
  }
  async function reloadFile() {
    if (!(await mayDiscard(true))) return;
    setBusy(true);
    setError("");
    try {
      await refreshFile(true);
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (diff)
    return (
      <div className="files-pane">
        <div className="pane-toolbar">
          <span>
            <GitCompareArrows size={16} />
            Workspace changes
          </span>
          <Button
            size="icon"
            variant="ghost"
            onClick={() => void list(true)}
            disabled={busy}
            aria-label="Refresh changes"
          >
            <RefreshCw size={15} className={busy ? "spin" : ""} />
          </Button>
        </div>
        {error && <div className="inline-error">{error}</div>}
        <div className="pane-notice">
          {diffScope}
          {diffTruncated &&
            " The displayed diff is truncated; inspect individual files for the complete changes."}
        </div>
        {!!changedFiles.length && (
          <details className="git-change-list" open>
            <summary>{changedFiles.length} changed paths</summary>
            <div>
              {changedFiles.map((change) => (
                <div className="git-change-row" key={change.path}>
                  <code>{change.path}</code>
                  {change.untracked ? (
                    <span className="tag">Untracked</span>
                  ) : (
                    <>
                      {change.index !== " " && (
                        <span className="tag">
                          Staged · {gitState[change.index] || change.index}
                        </span>
                      )}
                      {change.worktree !== " " && (
                        <span className="tag">
                          Unstaged ·{" "}
                          {gitState[change.worktree] || change.worktree}
                        </span>
                      )}
                    </>
                  )}
                </div>
              ))}
            </div>
          </details>
        )}
        {diffText ? (
          <Suspense fallback={<LoaderCircle className="spin" />}>
            <CodeEditor
              value={diffText}
              path="working-tree.diff"
              readOnly
              theme={theme}
            />
          </Suspense>
        ) : (
          <Empty
            icon={<GitCompareArrows size={28} />}
            title={
              error
                ? "Changes could not be read"
                : busy
                  ? "Reading changes…"
                  : changedFiles.length > 0
                    ? "No net patch against HEAD"
                    : "No changes in this scope"
            }
            description={
              error
                ? "Resolve the reported error and refresh. An unsuccessful read does not mean the workspace is clean."
                : changedFiles.length > 0
                  ? "Staged and unstaged states are listed above. Their combined patch currently cancels out."
                  : "Changes made in this workspace will appear here for review."
            }
          />
        )}
      </div>
    );
  return (
    <div className="files-pane">
      <div className="pane-toolbar">
        <span>
          <Folder size={15} />
          {file ? file.path : directory || "Workspace files"}
        </span>
        <div className="row-actions">
          {file && (
            <Button
              size="small"
              onClick={save}
              loading={busy}
              disabled={content === file.content || !!saveDisabledReason}
              title={saveDisabledReason || undefined}
            >
              <Save size={13} />
              Save changes
            </Button>
          )}
          <Button
            size="icon"
            variant="ghost"
            onClick={() => void list(true)}
            disabled={busy}
            aria-label="Refresh files and open file"
          >
            <RefreshCw size={15} />
          </Button>
        </div>
      </div>
      {error && (
        <div className="inline-error">
          <AlertTriangle size={15} />
          {error}
        </div>
      )}
      {saveDisabledReason && (
        <div className="pane-notice">
          {saveDisabledReason} Draft edits are kept in this editor.
        </div>
      )}
      {conflict && (
        <div className="notice" role="status">
          <span>
            This file changed on disk. Your unsaved draft is preserved; saving
            will check the original revision.
          </span>
          <Button size="small" onClick={reloadFile} disabled={busy}>
            Reload from disk
          </Button>
        </div>
      )}
      <div className="file-layout">
        <div className="file-list">
          {directory && (
            <button
              className="file-row"
              onClick={async () => {
                if (!(await mayDiscard())) return;
                setDirectory(directory.split("/").slice(0, -1).join("/"));
              }}
            >
              <ArrowLeft size={14} />
              Parent folder
            </button>
          )}
          {entries.map((e) => (
            <button
              className={`file-row ${file?.path === e.path ? "selected" : ""}`}
              key={e.path}
              onClick={() => open(e)}
            >
              {e.isDir ? <Folder size={15} /> : <FileCode2 size={15} />}
              <span>{e.name}</span>
              {e.isDir ? (
                <ChevronRight size={13} />
              ) : (
                <small>{formatBytes(e.size)}</small>
              )}
            </button>
          ))}
          {!entries.length && (
            <p className="muted small pad">
              {busy ? "Reading files…" : "This folder is empty."}
            </p>
          )}
        </div>
        <div className="file-content">
          {file ? (
            <Suspense
              fallback={
                <div className="loading-pane">
                  <LoaderCircle className="spin" />
                  Opening editor
                </div>
              }
            >
              <CodeEditor
                value={content}
                path={`${workspaceId}/${file.path}`}
                onChange={updateContent}
                theme={theme}
              />
            </Suspense>
          ) : (
            <Empty
              icon={<FileCode2 size={29} />}
              title="A closer look at your work"
              description="Choose a file to read or edit it. Changes are checked against the version you opened."
            />
          )}
        </div>
      </div>
    </div>
  );
}
