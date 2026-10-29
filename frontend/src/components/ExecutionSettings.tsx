import { useEffect, useState } from "react";
import { Box, RefreshCw } from "lucide-react";
import { api } from "../bridge";
import type { Action } from "../Management";
import { Button, Field, formatBytes } from "./ui";

interface StorageUsage {
  observedBytes: number;
  entries: number;
  complete: boolean;
  error?: string;
  observedAt: string;
}

interface ExecutionStatus {
  image: string;
  configured: boolean;
  error?: string;
  runtime?: Record<string, unknown> & { storage?: StorageUsage };
  recovery?: { status: string; error?: string };
}
interface Toolchain {
  id: string;
  repoTags: string[];
  size: number;
}

export default function ExecutionSettings({ action }: { action: Action }) {
  const [status, setStatus] = useState<ExecutionStatus>();
  const [image, setImage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [toolchains, setToolchains] = useState<Toolchain[]>([]);
  async function refresh() {
    setBusy(true);
    try {
      const next = await api<ExecutionStatus>("execution.status");
      setStatus(next);
      setImage(next.image);
      setError("");
      try {
        const found = await api<{ items: Toolchain[] }>("execution.images");
        setToolchains(found.items || []);
      } catch {
        setToolchains([]);
      }
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Could not inspect execution runtime",
      );
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void refresh();
  }, []);
  return (
    <section className="settings-section">
      <div className="settings-label">
        <Box size={20} />
        <h3>Coding executor</h3>
        <p>Run builds and subprocess tests in a private Linux environment.</p>
      </div>
      <div className="form-stack">
        {!!toolchains.length && (
          <Field label="Installed coding toolchain">
            <select
              value={toolchains.some((t) => t.id === image) ? image : ""}
              onChange={(e) => {
                if (e.target.value) setImage(e.target.value);
              }}
            >
              <option value="">Choose an installed toolchain</option>
              {toolchains.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.repoTags?.[0] || "Gemma Desktop toolchain"} ·{" "}
                  {t.id.slice(7, 19)}
                </option>
              ))}
            </select>
          </Field>
        )}
        <Field
          label="Pinned Docker toolchain image"
          hint="Use an installed image digest. Running a command never downloads an image."
        >
          <input
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="repository@sha256:…"
            spellCheck={false}
          />
        </Field>
        <p className="small muted">
          One command at a time · 4 CPUs · 8 GiB memory · 256 processes ·
          offline
        </p>
        {status?.runtime?.storage && (
          <div className="small muted">
            <p>
              Execution storage:{" "}
              {formatBytes(status.runtime.storage.observedBytes)}
              {" · "}
              {status.runtime.storage.entries.toLocaleString()} entries
              {!status.runtime.storage.complete && " · partial measurement"}
            </p>
            <p>
              Logical file sizes in snapshots, journals and private caches. Soft
              monitor: 2 GiB / 100,000 entries, with possible overshoot. Docker
              images and VM allocation are excluded. Nothing is pruned
              automatically.
            </p>
            {status.runtime.storage.error && (
              <p>Measurement limit: {status.runtime.storage.error}</p>
            )}
          </div>
        )}
        <p className="small muted">
          File edits appear directly in your project. Build commands synchronize
          permitted source changes after completion. Dependencies and the
          profile’s configured private output directories stay in the command
          cache.
        </p>
        <div className="row-actions">
          <Button
            type="button"
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                const result = await action("execution.configure", { image });
                if (result !== undefined) await refresh();
              } finally {
                setBusy(false);
              }
            }}
          >
            Save toolchain
          </Button>
          <Button type="button" onClick={refresh} disabled={busy}>
            <RefreshCw size={14} />
            Inspect runtime
          </Button>
        </div>
        {(error || status?.error) && (
          <p className="error-text" role="alert">
            {error || status?.error}
          </p>
        )}
        {status?.recovery?.status === "blocked" && (
          <Button
            type="button"
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await action("execution.reconcile");
                await refresh();
              } finally {
                setBusy(false);
              }
            }}
          >
            Retry command recovery
          </Button>
        )}
        {status?.runtime && (
          <details>
            <summary>Runtime details</summary>
            <pre className="json-view">
              {JSON.stringify(status.runtime, null, 2)}
            </pre>
          </details>
        )}
      </div>
    </section>
  );
}
