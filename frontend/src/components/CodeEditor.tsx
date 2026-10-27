import { useEffect, useLayoutEffect, useRef, useState } from "react";
import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/editor/editor.worker.js?worker&inline";

const workerErrorEvent = "gemma:editor-worker-error";
self.MonacoEnvironment = {
  // WKWebView cannot reliably load Worker scripts through wails://. Bundle
  // each worker into a Blob, and load language-specific bundles on demand.
  getWorker: async (_id, label) => {
    try {
      const WorkerClass =
        label === "typescript" || label === "javascript"
          ? (
              await import(
                "monaco-editor/language/typescript/ts.worker.js?worker&inline"
              )
            ).default
          : label === "json"
            ? (
                await import(
                  "monaco-editor/language/json/json.worker.js?worker&inline"
                )
              ).default
            : ["css", "scss", "less"].includes(label)
              ? (
                  await import(
                    "monaco-editor/language/css/css.worker.js?worker&inline"
                  )
                ).default
              : ["html", "handlebars", "razor"].includes(label)
                ? (
                    await import(
                      "monaco-editor/language/html/html.worker.js?worker&inline"
                    )
                  ).default
                : EditorWorker;
      const worker = new WorkerClass();
      worker.addEventListener("error", (event) =>
        window.dispatchEvent(
          new CustomEvent(workerErrorEvent, {
            detail: event.message || "A language worker could not start.",
          }),
        ),
      );
      return worker;
    } catch (error) {
      window.dispatchEvent(
        new CustomEvent(workerErrorEvent, {
          detail: error instanceof Error ? error.message : String(error),
        }),
      );
      throw error;
    }
  },
};
monaco.editor.defineTheme("gemma-dark", {
  base: "vs-dark",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#10141c",
    "editor.foreground": "#d9e2f1",
    "editorLineNumber.foreground": "#526078",
    "editorLineNumber.activeForeground": "#a4b8d8",
    "editor.selectionBackground": "#23457a",
    "editorGutter.background": "#10141c",
    "editor.lineHighlightBackground": "#151c28",
    "diffEditor.insertedTextBackground": "#1d6a4928",
    "diffEditor.removedTextBackground": "#99485028",
  },
});
function language(path: string) {
  const e = path.split(".").pop() || "";
  return (
    (
      {
        ts: "typescript",
        tsx: "typescript",
        js: "javascript",
        jsx: "javascript",
        json: "json",
        go: "go",
        py: "python",
        rs: "rust",
        css: "css",
        html: "html",
        md: "markdown",
        sh: "shell",
        yaml: "yaml",
        yml: "yaml",
        swift: "swift",
        sql: "sql",
        diff: "diff",
      } as Record<string, string>
    )[e] || "plaintext"
  );
}
export default function CodeEditor({
  value,
  path,
  original,
  onChange,
  readOnly = false,
  theme = "dark",
}: {
  value: string;
  path: string;
  original?: string;
  onChange?: (v: string) => void;
  readOnly?: boolean;
  theme?: string;
}) {
  const host = useRef<HTMLDivElement>(null),
    editor = useRef<
      | monaco.editor.IStandaloneCodeEditor
      | monaco.editor.IStandaloneDiffEditor
      | null
    >(null),
    callback = useRef(onChange);
  const [error, setError] = useState("");
  callback.current = onChange;
  useLayoutEffect(() => {
    if (!host.current) return;
    setError("");
    const surface = host.current;
    const id = crypto.randomUUID();
    const uri = monaco.Uri.parse(
      `gemma://editor/${id}/${encodeURIComponent(path)}`,
    );
    const model = monaco.editor.createModel(value, language(path), uri);
    const opts = {
      automaticLayout: false,
      // Use Monaco's established textarea path in WKWebView. The newer
      // native EditContext implementation differs across WebKit releases.
      editContext: false,
      experimentalGpuAcceleration: "off" as const,
      fontSize: 13,
      fontFamily: '"SFMono-Regular", Menlo, Consolas, monospace',
      lineHeight: 22,
      minimap: { enabled: false },
      padding: { top: 16, bottom: 16 },
      scrollBeyondLastLine: false,
      readOnly,
      theme: theme === "light" ? "vs" : "gemma-dark",
      wordWrap: "off" as const,
      accessibilitySupport: "auto" as const,
      renderWhitespace: "selection" as const,
    };
    let originalModel: monaco.editor.ITextModel | undefined;
    const reportWorkerError = (event: globalThis.Event) =>
      setError((event as CustomEvent<string>).detail);
    window.addEventListener(workerErrorEvent, reportWorkerError);
    try {
      if (original !== undefined) {
        const ed = monaco.editor.createDiffEditor(surface, {
          ...opts,
          renderSideBySide: true,
          originalEditable: false,
        });
        originalModel = monaco.editor.createModel(original, language(path));
        ed.setModel({ original: originalModel, modified: model });
        editor.current = ed;
      } else {
        editor.current = monaco.editor.create(surface, { ...opts, model });
      }
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
      model.dispose();
      originalModel?.dispose();
      window.removeEventListener(workerErrorEvent, reportWorkerError);
      return;
    }
    const instance = editor.current;
    let frame = 0;
    const layout = () => {
      const width = surface.clientWidth;
      const height = surface.clientHeight;
      if (width <= 0 || height <= 0) return;
      instance.layout({ width, height });
      if ("getModifiedEditor" in instance) {
        instance.getOriginalEditor().render(true);
        instance.getModifiedEditor().render(true);
      } else instance.render(true);
    };
    const scheduleLayout = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(layout);
    };
    const observer = new ResizeObserver(scheduleLayout);
    observer.observe(surface);
    layout();
    scheduleLayout();
    const subscription = model.onDidChangeContent(() =>
      callback.current?.(model.getValue()),
    );
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener(workerErrorEvent, reportWorkerError);
      subscription.dispose();
      editor.current?.dispose();
      editor.current = null;
      model.dispose();
      originalModel?.dispose();
    };
    // File identity controls model lifetime; edits are held by this model.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, original !== undefined]);
  useEffect(() => {
    const ed = editor.current;
    if (!ed) return;
    const model =
      "getModifiedEditor" in ed
        ? ed.getModifiedEditor().getModel()
        : ed.getModel();
    if (model && model.getValue() !== value) model.setValue(value);
  }, [value]);
  useEffect(() => {
    monaco.editor.setTheme(theme === "light" ? "vs" : "gemma-dark");
    editor.current?.updateOptions({ readOnly });
  }, [theme, readOnly]);
  return (
    <div className="code-editor-frame">
      {error && (
        <div className="code-editor-error" role="alert">
          Editor service: {error}
        </div>
      )}
      <div
        className="code-editor"
        ref={host}
        aria-label={
          original !== undefined ? "Code comparison" : `Editor for ${path}`
        }
      />
    </div>
  );
}
