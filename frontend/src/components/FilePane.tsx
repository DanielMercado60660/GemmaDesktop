import { lazy, Suspense, useEffect, useState } from "react";
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
import { Button, Empty, formatBytes } from "./ui";
const CodeEditor = lazy(() => import("./CodeEditor"));
interface Entry {
  name: string;
  path: string;
  isDir: boolean;
  size?: number;
}
export default function FilePane({
  workspaceId,
  theme,
  diff = false,
  onDirtyChange,
}: {
  workspaceId: string;
  theme: string;
  diff?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const [directory, setDirectory] = useState(""),
    [entries, setEntries] = useState<Entry[]>([]),
    [file, setFile] = useState<{
      path: string;
      content: string;
      hash: string;
    } | null>(null),
    [content, setContent] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [diffText, setDiff] = useState("");
  useEffect(() => {
    onDirtyChange?.(!!file && file.content !== content);
  }, [file, content, onDirtyChange]);
  useEffect(() => () => onDirtyChange?.(false), [onDirtyChange]);
  async function list() {
    setBusy(true);
    setError("");
    try {
      if (diff) {
        const r = await api<{ diff: string }>("git.diff", { workspaceId });
        setDiff(r.diff || "");
      } else {
        const r = await api<{ entries: Entry[] }>("file.list", {
          workspaceId,
          path: directory,
        });
        setEntries(r.entries || []);
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    setFile(null);
    void list();
  }, [workspaceId, directory, diff]);
  async function open(entry: Entry) {
    if (
      file &&
      content !== file.content &&
      !confirm("Discard your unsaved changes?")
    )
      return;
    if (entry.isDir) {
      setDirectory(entry.path);
      return;
    }
    setBusy(true);
    setError("");
    try {
      const r = await api<{ path: string; content: string; hash: string }>(
        "file.read",
        { workspaceId, path: entry.path },
      );
      setFile({ ...r, path: entry.path });
      setContent(r.content);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!file) return;
    setBusy(true);
    setError("");
    try {
      const r = await api<{ hash?: string }>("file.write", {
        workspaceId,
        path: file.path,
        content,
        expectedHash: file.hash,
      });
      const updated = await api<{
        path: string;
        content: string;
        hash: string;
      }>("file.read", { workspaceId, path: file.path });
      setFile({
        ...updated,
        path: file.path,
        hash: updated.hash || r.hash || file.hash,
      });
      setContent(updated.content);
    } catch (e) {
      setError((e as Error).message);
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
            Working tree changes
          </span>
          <Button
            size="icon"
            variant="ghost"
            onClick={list}
            disabled={busy}
            aria-label="Refresh changes"
          >
            <RefreshCw size={15} className={busy ? "spin" : ""} />
          </Button>
        </div>
        {error && <div className="inline-error">{error}</div>}
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
            title={busy ? "Reading changes…" : "No working tree changes"}
            description="Changes made in this workspace will appear here for review."
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
              disabled={content === file.content}
            >
              <Save size={13} />
              Save changes
            </Button>
          )}
          <Button
            size="icon"
            variant="ghost"
            onClick={list}
            aria-label="Refresh files"
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
      <div className="file-layout">
        <div className="file-list">
          {directory && (
            <button
              className="file-row"
              onClick={() => {
                if (
                  file &&
                  content !== file.content &&
                  !confirm("Discard your unsaved changes?")
                )
                  return;
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
                onChange={setContent}
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
