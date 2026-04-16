import Link from "next/link";

import { APIError, listPipelines, PipelineSummary } from "@/lib/api";

export default async function PipelinesPage() {
  let pipelines: PipelineSummary[] = [];
  let error: string | null = null;
  try {
    pipelines = await listPipelines();
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Pipelines</h1>
      <p className="page-lede">
        Multi-agent topologies loaded from <code>kind: Pipeline</code> YAML
        documents in the agents directory. Click any row to see the DAG.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Server error.</strong> {error}
        </div>
      )}

      {pipelines.length === 0 && !error ? (
        <div className="empty">
          No pipelines loaded. Drop a <code>kind: Pipeline</code> YAML into
          your agents directory and restart the server.
        </div>
      ) : (
        <div className="card-grid">
          {pipelines.map((p) => (
            <Link
              key={p.name}
              href={`/pipelines/${encodeURIComponent(p.name)}`}
              className="card"
            >
              <div className="card-head">
                <h2>{p.name}</h2>
                <span className="badge">{p.topology}</span>
              </div>
              {p.description && <p className="card-desc">{p.description}</p>}
              <dl className="stats">
                <div>
                  <dt>Agents</dt>
                  <dd>{p.agent_count}</dd>
                </div>
                <div>
                  <dt>Edges</dt>
                  <dd>{p.edge_count}</dd>
                </div>
              </dl>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
