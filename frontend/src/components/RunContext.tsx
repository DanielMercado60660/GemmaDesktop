import { useEffect, useState } from "react";
import { BookOpen, ListChecks } from "lucide-react";
import { api } from "../bridge";
import type { Run } from "../types";

interface ContextManifest {
  version: number;
  digest?: string;
  notice?: string;
  sources: Array<{
    kind: string;
    path: string;
    sha256: string;
    scope: string;
    content?: string;
    trustClass?: string;
  }>;
  estimatedTokens?: number;
  estimateMethod?: string;
  omissions?: string[];
}
interface Plan {
  revision: number;
  qualification?: string;
  observations?: Record<
    string,
    {
      receiptId: string;
      tool?: string;
      status: string;
      path?: string;
      destination?: string;
      executable?: string;
      workingDirectory?: string;
      exitCode?: number;
      publication?: string;
      beforeHash?: string;
      afterHash?: string;
      detailsLimited?: boolean;
      note?: string;
    }
  >;
  observationsOmitted?: number;
  items: Array<{
    id: string;
    title: string;
    status: string;
    receiptIds?: string[];
  }>;
}
export default function RunContext({
  run,
  view,
}: {
  run?: Run;
  view: "context" | "plan";
}) {
  const [manifest, setManifest] = useState<ContextManifest>(),
    [plan, setPlan] = useState<Plan>(),
    [error, setError] = useState("");
  useEffect(() => {
    let current = true;
    setManifest(undefined);
    setPlan(undefined);
    setError("");
    if (!run) return;
    const id = run.id;
    let busy = false;
    async function load() {
      if (busy) return;
      busy = true;
      try {
        if (view === "context") {
          const r = await api<ContextManifest>("context.get", { runId: id });
          if (current) setManifest(r);
        } else {
          const r = await api<Plan>("plan.get", { runId: id });
          if (current) setPlan(r);
        }
        if (current) setError("");
      } catch (e) {
        if (current) setError(e instanceof Error ? e.message : String(e));
      } finally {
        busy = false;
      }
    }
    void load();
    const timer = window.setInterval(load, 5000);
    return () => {
      current = false;
      window.clearInterval(timer);
    };
  }, [run?.id, view]);
  return (
    <div className="inspector-content">
      <div className="inspector-heading">
        <span className="eyebrow">
          {view === "context" ? "FROZEN INPUT SOURCES" : "DURABLE TASK PLAN"}
        </span>
      </div>
      {!run && (
        <div className="inspector-empty">
          {view === "context" ? (
            <BookOpen size={26} />
          ) : (
            <ListChecks size={26} />
          )}
          <h3>
            {view === "context" ? "Inspect what Gemma sees" : "Follow the plan"}
          </h3>
          <p>
            Start a run to view its recorded{" "}
            {view === "context" ? "context" : "plan"}.
          </p>
        </div>
      )}
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      {manifest && (
        <>
          <p className="inspector-note">
            Sources are captured for this run. Project files, attachments, and
            skill text do not grant permissions.
          </p>
          <div className="detail-list">
            <div>
              <span>Sources</span>
              <span>{manifest.sources.length}</span>
            </div>
            {manifest.estimatedTokens !== undefined && (
              <div>
                <span>Estimated source tokens</span>
                <span>{manifest.estimatedTokens.toLocaleString()}</span>
              </div>
            )}
            {run?.usage?.prompt_eval_count !== undefined && (
              <div>
                <span>Measured input across calls</span>
                <span>{run.usage.prompt_eval_count.toLocaleString()}</span>
              </div>
            )}
          </div>
          {manifest.notice && (
            <p className="inspector-note">{manifest.notice}</p>
          )}
          {manifest.estimateMethod && (
            <p className="inspector-note">{manifest.estimateMethod}</p>
          )}
          {manifest.omissions?.map((o) => (
            <p key={o} className="inspector-note">
              {o}
            </p>
          ))}
          {manifest.sources.map((s, i) => (
            <details
              className="run-context-source"
              key={`${s.kind}-${s.path}-${i}`}
            >
              <summary>
                {s.kind.replaceAll("_", " ")} · {s.path}
              </summary>
              <p className="small muted">
                {s.scope} · {s.trustClass || "Source material"}
              </p>
              <code className="small break-all">{s.sha256}</code>
              {s.content && <pre>{s.content}</pre>}
            </details>
          ))}
          {manifest.digest && (
            <p className="inspector-note mono break-all">
              Manifest {manifest.digest}
            </p>
          )}
        </>
      )}
      {plan && (
        <>
          <p className="inspector-note">
            Revision {plan.revision ?? 0}. Completion labels describe progress
            claims; verification comes from recorded checks.
          </p>
          {(plan.items || []).map((item) => (
            <div className="plan-item" key={item.id}>
              <span className="tag">{item.status.replaceAll("_", " ")}</span>
              <div>
                <p>{item.title}</p>
                {[...new Set(item.receiptIds || [])].map((id) => {
                  const observation = plan.observations?.[id];
                  return (
                    <details className="run-context-source" key={id}>
                      <summary>
                        {observation?.tool?.replaceAll("_", " ") ||
                          "Linked observation"}{" "}
                        · {observation?.status || "summary omitted"}
                      </summary>
                      {observation ? (
                        <>
                          <div className="detail-list">
                            {observation.path && (
                              <div>
                                <span>File</span>
                                <code className="break-all">
                                  {observation.path}
                                </code>
                              </div>
                            )}
                            {observation.destination && (
                              <div>
                                <span>Destination</span>
                                <code className="break-all">
                                  {observation.destination}
                                </code>
                              </div>
                            )}
                            {observation.executable && (
                              <div>
                                <span>Program</span>
                                <code className="break-all">
                                  {observation.executable}
                                </code>
                              </div>
                            )}
                            {observation.workingDirectory && (
                              <div>
                                <span>Directory</span>
                                <code>{observation.workingDirectory}</code>
                              </div>
                            )}
                            {observation.exitCode !== undefined && (
                              <div>
                                <span>Exit code</span>
                                <span>{observation.exitCode}</span>
                              </div>
                            )}
                            {observation.publication && (
                              <div>
                                <span>Source publication</span>
                                <span>{observation.publication}</span>
                              </div>
                            )}
                            {observation.beforeHash && (
                              <div>
                                <span>Before</span>
                                <code className="break-all">
                                  {observation.beforeHash}
                                </code>
                              </div>
                            )}
                            {observation.afterHash && (
                              <div>
                                <span>After</span>
                                <code className="break-all">
                                  {observation.afterHash}
                                </code>
                              </div>
                            )}
                          </div>
                          <p className="small muted">{observation.note}</p>
                          {observation.detailsLimited && (
                            <p className="small muted">
                              Some metadata exceeds this view’s limits;
                              displayed values are prefixes.
                            </p>
                          )}
                        </>
                      ) : (
                        <p className="small muted">
                          This plan exceeds the bounded observation view.
                        </p>
                      )}
                      <code className="small break-all">{id}</code>
                    </details>
                  );
                })}
              </div>
            </div>
          ))}
          {!plan.items?.length && (
            <p className="muted">
              No task plan has been recorded for this run.
            </p>
          )}
          <p className="inspector-note">{plan.qualification}</p>
          {!!plan.observationsOmitted && (
            <p className="inspector-note">
              {plan.observationsOmitted} linked observations omitted from this
              bounded view.
            </p>
          )}
        </>
      )}
    </div>
  );
}
