import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { TerminalSquare, Square, Play, AlertTriangle } from "lucide-react";
import { api } from "../bridge";
import { Button, Empty } from "./ui";
export default function TerminalPane({ workspaceId }: { workspaceId: string }) {
  const host = useRef<HTMLDivElement>(null),
    term = useRef<Terminal | null>(null),
    fit = useRef<FitAddon | null>(null),
    session = useRef(""),
    mounted = useRef(false);
  const [id, setId] = useState(""),
    [error, setError] = useState(""),
    [starting, setStarting] = useState(false),
    [exited, setExited] = useState(false);
  useEffect(() => {
    if (!host.current) return;
    mounted.current = true;
    const t = new Terminal({
      fontFamily: '"SFMono-Regular", Menlo, monospace',
      fontSize: 12,
      lineHeight: 1.45,
      cursorBlink: true,
      scrollback: 5000,
      theme: {
        background: "#10141c",
        foreground: "#dce5f4",
        cursor: "#73a3ff",
        selectionBackground: "#2855a066",
      },
      allowProposedApi: false,
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.open(host.current);
    term.current = t;
    fit.current = f;
    const input = t.onData((data) => {
      if (session.current)
        void api("terminal.write", { id: session.current, data }).catch((e) =>
          setError(String(e.message)),
        );
    });
    const resize = t.onResize(({ cols, rows }) => {
      if (session.current)
        void api("terminal.resize", { id: session.current, cols, rows }).catch(
          (e) => setError(e.message),
        );
    });
    const observer = new ResizeObserver(() => {
      if (host.current?.clientWidth && host.current.clientHeight) f.fit();
    });
    observer.observe(host.current);
    f.fit();
    return () => {
      mounted.current = false;
      observer.disconnect();
      input.dispose();
      resize.dispose();
      t.dispose();
      term.current = null;
      fit.current = null;
      const old = session.current;
      session.current = "";
      if (old) void api("terminal.stop", { id: old }).catch(() => {});
    };
  }, [workspaceId]);
  useEffect(() => {
    if (!id) return;
    let cancelled = false,
      offset = 0,
      timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const out = await api<{
          data: string;
          offset: number;
          exited?: boolean;
          truncated?: boolean;
          exitCode?: number;
          error?: string;
        }>("terminal.read", { id, offset });
        if (cancelled) return;
        offset = out.offset;
        if (out.truncated)
          setError(
            "Some earlier terminal output was dropped because the output buffer filled.",
          );
        if (out.data && term.current)
          await new Promise<void>((resolve) =>
            term.current!.write(out.data, resolve),
          );
        if (out.error) setError(out.error);
        if (out.exited) {
          setExited(true);
          return;
        }
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      }
      if (!cancelled) timer = setTimeout(poll, 180);
    }
    void poll();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [id]);
  async function start() {
    setStarting(true);
    setError("");
    try {
      const r = await api<{ id: string }>("terminal.start", { workspaceId });
      if (!mounted.current) {
        await api("terminal.stop", { id: r.id });
        return;
      }
      session.current = r.id;
      setId(r.id);
      setExited(false);
      term.current?.clear();
      fit.current?.fit();
      if (term.current)
        await api("terminal.resize", {
          id: r.id,
          cols: term.current.cols,
          rows: term.current.rows,
        });
      term.current?.focus();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setStarting(false);
    }
  }
  async function stop() {
    try {
      await api("terminal.stop", { id });
      session.current = "";
      setId("");
      setExited(true);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  return (
    <div className="terminal-pane">
      <div className="pane-toolbar">
        <span>
          <TerminalSquare size={15} />
          Your host terminal
        </span>
        <Button
          size="small"
          onClick={id && !exited ? stop : start}
          loading={starting}
        >
          {id && !exited ? (
            <>
              <Square size={13} />
              Stop
            </>
          ) : (
            <>
              <Play size={13} />
              Start terminal
            </>
          )}
        </Button>
      </div>
      {error && (
        <div className="inline-error">
          <AlertTriangle size={15} />
          {error}
        </div>
      )}
      <div className="terminal-surface" ref={host} />
      {!id && !error && (
        <div className="terminal-explainer">
          <Empty
            icon={<TerminalSquare size={27} />}
            title="A terminal for this workspace"
            description="Start a shell on your Mac in this workspace. You control its input; agents cannot type here. This shell can change files outside the agent read-only policy."
            action={
              <Button onClick={start} loading={starting}>
                <Play size={15} />
                Start terminal
              </Button>
            }
          />
        </div>
      )}
      {id && (
        <div className="pane-notice">
          User-operated host shell · Agents cannot send input · Agent read-only
          mode does not restrict this shell
        </div>
      )}
      {exited && <div className="pane-notice">Terminal process has ended.</div>}
    </div>
  );
}
